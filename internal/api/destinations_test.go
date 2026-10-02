package api

import (
	"encoding/json"
	"net/http"
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
