package api

import (
	"encoding/json"
	"net/http"
	"os"
	"testing"
)

func destPayload() map[string]any {
	return map[string]any{"name": "minio", "endpoint": "http://127.0.0.1:9000", "region": "us-east-1",
		"bucket": "ku", "prefix": "pre", "accessKey": "ak", "secretKey": "sk"}
}

func createDest(t *testing.T, h http.Handler, cookie *http.Cookie, name string) string {
	t.Helper()
	p := destPayload()
	p["name"] = name
	rec := doJSON(t, h, "POST", "/api/storage/destinations", p, cookie)
	if rec.Code != 201 {
		t.Fatalf("create dest = %d: %s", rec.Code, rec.Body.String())
	}
	var d destinationDTO
	json.NewDecoder(rec.Body).Decode(&d)
	return d.ID
}

func TestDestinationCRUD(t *testing.T) {
	h := newTestServer(t)
	cookie := setupAndLogin(t, h)
	rec := doJSON(t, h, "GET", "/api/storage/destinations", nil, nil)
	if rec.Code != 401 {
		t.Fatalf("unauth = %d", rec.Code)
	}
	id := createDest(t, h, cookie, "minio")
	rec = doJSON(t, h, "GET", "/api/storage/destinations", nil, cookie)
	var list []destinationDTO
	json.NewDecoder(rec.Body).Decode(&list)
	if len(list) != 1 || list[0].ID != id || !list[0].SecretSet || list[0].Kind != "s3" {
		t.Fatalf("list = %+v", list)
	}
	upd := destPayload()
	upd["name"] = "minio"
	upd["prefix"] = "other"
	delete(upd, "secretKey")
	rec = doJSON(t, h, "PUT", "/api/storage/destinations/"+id, upd, cookie)
	if rec.Code != 200 {
		t.Fatalf("update = %d: %s", rec.Code, rec.Body.String())
	}
	var got destinationDTO
	json.NewDecoder(rec.Body).Decode(&got)
	if got.Prefix != "other" || !got.SecretSet {
		t.Fatalf("updated = %+v", got)
	}
	rec = doJSON(t, h, "POST", "/api/storage/destinations", destPayload(), cookie)
	if rec.Code != 409 {
		t.Fatalf("duplicate = %d", rec.Code)
	}
	rec = doJSON(t, h, "POST", "/api/storage/destinations", map[string]any{"name": "x"}, cookie)
	if rec.Code != 400 {
		t.Fatalf("validation = %d", rec.Code)
	}
	rec = doJSON(t, h, "DELETE", "/api/storage/destinations/"+id, nil, cookie)
	if rec.Code != 200 {
		t.Fatalf("delete = %d", rec.Code)
	}
	rec = doJSON(t, h, "GET", "/api/storage/destinations", nil, cookie)
	list = nil
	json.NewDecoder(rec.Body).Decode(&list)
	if len(list) != 0 {
		t.Fatalf("list after delete = %+v", list)
	}
}

func TestDestinationDeleteGuard(t *testing.T) {
	h := newTestServer(t)
	cookie := setupAndLogin(t, h)
	id := createDest(t, h, cookie, "minio")
	db := createDatabase(t, h, cookie)
	rec := doJSON(t, h, "POST", "/api/workflows", map[string]any{
		"name": "x", "databaseId": db.ID, "destId": id, "triggerKind": "manual",
	}, cookie)
	if rec.Code != 201 {
		t.Fatalf("create wf = %d: %s", rec.Code, rec.Body.String())
	}
	var wf struct {
		ID string `json:"id"`
	}
	json.NewDecoder(rec.Body).Decode(&wf)
	rec = doJSON(t, h, "POST", "/api/workflows/"+wf.ID+"/run", nil, cookie)
	if rec.Code != 201 {
		t.Fatalf("run wf = %d: %s", rec.Code, rec.Body.String())
	}
	var created struct {
		DumpID string `json:"dumpId"`
	}
	json.NewDecoder(rec.Body).Decode(&created)
	d := waitDumpReady(t, h, cookie, created.DumpID)
	if d.DestName != "minio" || d.DestID != id {
		t.Fatalf("dump = %+v", d)
	}
	rec = doJSON(t, h, "DELETE", "/api/storage/destinations/"+id, nil, cookie)
	if rec.Code != 409 {
		t.Fatalf("delete in-use = %d", rec.Code)
	}
	rec = doJSON(t, h, "DELETE", "/api/dumps/"+d.ID, nil, cookie)
	if rec.Code != 200 {
		t.Fatalf("delete dump = %d", rec.Code)
	}
	rec = doJSON(t, h, "DELETE", "/api/storage/destinations/"+id, nil, cookie)
	if rec.Code != 409 {
		t.Fatalf("delete with workflow = %d", rec.Code)
	}
	rec = doJSON(t, h, "DELETE", "/api/workflows/"+wf.ID, nil, cookie)
	if rec.Code != 200 {
		t.Fatalf("delete wf = %d", rec.Code)
	}
	rec = doJSON(t, h, "DELETE", "/api/storage/destinations/"+id, nil, cookie)
	if rec.Code != 200 {
		t.Fatalf("delete after refs gone = %d", rec.Code)
	}
}

func TestDestinationTestUnreachable(t *testing.T) {
	h := newTestServer(t)
	cookie := setupAndLogin(t, h)
	rec := doJSON(t, h, "POST", "/api/storage/destinations/test", map[string]any{
		"name": "t", "endpoint": "http://127.0.0.1:1", "bucket": "b",
		"accessKey": "a", "secretKey": "s",
	}, cookie)
	if rec.Code != 200 {
		t.Fatalf("code = %d", rec.Code)
	}
	var res struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	json.NewDecoder(rec.Body).Decode(&res)
	if res.OK || res.Error == "" {
		t.Fatalf("res = %+v", res)
	}
}

func TestLocalDestinationLifecycle(t *testing.T) {
	h := newTestServer(t)
	cookie := setupAndLogin(t, h)
	root := t.TempDir()

	// relative path rejected
	rec := doJSON(t, h, "POST", "/api/storage/destinations", map[string]any{
		"name": "bad", "kind": "local", "rootPath": "relative/path",
	}, cookie)
	if rec.Code != 400 {
		t.Fatalf("relative rootPath = %d: %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, h, "POST", "/api/storage/destinations", map[string]any{
		"name": "nfs", "kind": "local", "rootPath": root + "/pg",
	}, cookie)
	if rec.Code != 201 {
		t.Fatalf("create local = %d: %s", rec.Code, rec.Body.String())
	}
	var d struct {
		ID        string `json:"id"`
		Kind      string `json:"kind"`
		RootPath  string `json:"rootPath"`
		SecretSet bool   `json:"secretSet"`
	}
	json.Unmarshal(rec.Body.Bytes(), &d)
	if d.Kind != "local" || d.RootPath != root+"/pg" || d.SecretSet {
		t.Fatalf("created = %+v", d)
	}
	if _, err := os.Stat(root + "/pg"); err != nil {
		t.Fatalf("rootPath not created on save: %v", err)
	}

	// test endpoint probes the folder
	rec = doJSON(t, h, "POST", "/api/storage/destinations/"+d.ID+"/test", nil, cookie)
	var tr struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	json.Unmarshal(rec.Body.Bytes(), &tr)
	if !tr.OK {
		t.Fatalf("local test failed: %s", tr.Error)
	}

	// kind is immutable on update
	rec = doJSON(t, h, "PUT", "/api/storage/destinations/"+d.ID, map[string]any{
		"name": "nfs", "kind": "s3", "endpoint": "http://e", "bucket": "b", "accessKey": "k", "secretKey": "s",
	}, cookie)
	if rec.Code != 400 {
		t.Fatalf("kind change = %d", rec.Code)
	}

	// delete still works while unused
	rec = doJSON(t, h, "DELETE", "/api/storage/destinations/"+d.ID, nil, cookie)
	if rec.Code != 200 {
		t.Fatalf("delete local = %d", rec.Code)
	}
}

func TestSFTPDestinationValidation(t *testing.T) {
	h := newTestServer(t)
	cookie := setupAndLogin(t, h)

	// missing authType
	rec := doJSON(t, h, "POST", "/api/storage/destinations", map[string]any{
		"name": "off", "kind": "sftp", "host": "h", "username": "u", "secretKey": "s",
	}, cookie)
	if rec.Code != 400 {
		t.Fatalf("missing authType = %d", rec.Code)
	}

	// create ok (test endpoint fails gracefully against a dead host)
	rec = doJSON(t, h, "POST", "/api/storage/destinations", map[string]any{
		"name": "off", "kind": "sftp", "host": "127.0.0.1", "port": 1,
		"username": "u", "authType": "password", "secretKey": "s", "remoteDir": "dumps",
	}, cookie)
	if rec.Code != 201 {
		t.Fatalf("create sftp = %d: %s", rec.Code, rec.Body.String())
	}
	var d struct {
		ID       string `json:"id"`
		Port     int    `json:"port"`
		AuthType string `json:"authType"`
	}
	json.Unmarshal(rec.Body.Bytes(), &d)
	if d.Port != 1 || d.AuthType != "password" {
		t.Fatalf("created = %+v", d)
	}

	// update keeps secret when blank
	rec = doJSON(t, h, "PUT", "/api/storage/destinations/"+d.ID, map[string]any{
		"name": "off2", "kind": "sftp", "host": "127.0.0.1", "port": 2,
		"username": "u", "authType": "key",
	}, cookie)
	if rec.Code != 200 {
		t.Fatalf("update sftp = %d: %s", rec.Code, rec.Body.String())
	}

	// test endpoint returns ok:false (dead host), not a 5xx
	rec = doJSON(t, h, "POST", "/api/storage/destinations/"+d.ID+"/test", nil, cookie)
	var tr struct {
		OK bool `json:"ok"`
	}
	json.Unmarshal(rec.Body.Bytes(), &tr)
	if tr.OK {
		t.Fatal("sftp test unexpectedly ok")
	}
}

func TestDestinationKindDefaultsToS3(t *testing.T) {
	h := newTestServer(t)
	cookie := setupAndLogin(t, h)
	// no kind field -> s3 validation applies
	rec := doJSON(t, h, "POST", "/api/storage/destinations", map[string]any{
		"name": "legacy-client", "endpoint": "http://e", "bucket": "b", "accessKey": "k", "secretKey": "s",
	}, cookie)
	if rec.Code != 201 {
		t.Fatalf("kindless payload = %d: %s", rec.Code, rec.Body.String())
	}
	var d struct {
		Kind string `json:"kind"`
	}
	json.Unmarshal(rec.Body.Bytes(), &d)
	if d.Kind != "s3" {
		t.Fatalf("kind = %q", d.Kind)
	}
}
