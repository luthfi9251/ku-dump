package api

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func createDatabase(t *testing.T, h http.Handler, cookie *http.Cookie) databaseDTO {
	t.Helper()
	rec := doJSON(t, h, "POST", "/api/databases", validPayload(), cookie)
	if rec.Code != 201 {
		t.Fatalf("create db = %d: %s", rec.Code, rec.Body.String())
	}
	var d databaseDTO
	json.NewDecoder(rec.Body).Decode(&d)
	return d
}

// dumpViaWorkflow creates a manual workflow for the database and runs it —
// the direct POST /api/databases/{id}/dump endpoint was removed.
func dumpViaWorkflow(t *testing.T, h http.Handler, cookie *http.Cookie, dbID, name string) (string, dumpDTO) {
	t.Helper()
	rec := doJSON(t, h, "POST", "/api/workflows", map[string]any{
		"name": name, "databaseId": dbID, "destId": "", "triggerKind": "manual",
	}, cookie)
	if rec.Code != 201 {
		t.Fatalf("create workflow = %d: %s", rec.Code, rec.Body.String())
	}
	var wf struct {
		ID string `json:"id"`
	}
	json.NewDecoder(rec.Body).Decode(&wf)
	rec = doJSON(t, h, "POST", "/api/workflows/"+wf.ID+"/run", nil, cookie)
	if rec.Code != 201 {
		t.Fatalf("run workflow = %d: %s", rec.Code, rec.Body.String())
	}
	var created struct {
		JobID  string `json:"jobId"`
		DumpID string `json:"dumpId"`
	}
	json.NewDecoder(rec.Body).Decode(&created)
	return created.JobID, waitDumpReady(t, h, cookie, created.DumpID)
}

func waitDumpReady(t *testing.T, h http.Handler, cookie *http.Cookie, dumpID string) dumpDTO {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		rec := doJSON(t, h, "GET", "/api/dumps", nil, cookie)
		if rec.Code == 200 {
			var dumps []dumpDTO
			json.NewDecoder(rec.Body).Decode(&dumps)
			for _, d := range dumps {
				if d.ID == dumpID && (d.Status == "ready" || d.Status == "failed") {
					return d
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("dump not ready in time")
	return dumpDTO{}
}

func TestDumpFlow(t *testing.T) {
	h := newTestServer(t)
	cookie := setupAndLogin(t, h)
	db := createDatabase(t, h, cookie)
	_, d := dumpViaWorkflow(t, h, cookie, db.ID, "nightly")
	if d.Status != "ready" {
		t.Fatalf("dump = %+v", d)
	}
	if d.DestID != "" || d.DestName != "local" {
		t.Fatalf("dest = %q %q", d.DestID, d.DestName)
	}
	if d.Label != "nightly" || d.Engine != "postgres" || d.SizeBytes == 0 || d.SourceDB != "app" {
		t.Fatalf("dump = %+v", d)
	}
	rec := doJSON(t, h, "GET", "/api/dumps/"+d.ID+"/download", nil, cookie)
	if rec.Code != 200 {
		t.Fatalf("download = %d", rec.Code)
	}
	if rec.Body.String() != "DUMPDATA:app" {
		t.Fatalf("download body = %q", rec.Body.String())
	}
	cd := rec.Header().Get("Content-Disposition")
	if !strings.Contains(cd, "attachment") {
		t.Fatalf("disposition = %q", cd)
	}
	rec = doJSON(t, h, "DELETE", "/api/dumps/"+d.ID, nil, cookie)
	if rec.Code != 200 {
		t.Fatalf("delete = %d", rec.Code)
	}
	rec = doJSON(t, h, "GET", "/api/dumps/"+d.ID+"/download", nil, cookie)
	if rec.Code != 404 {
		t.Fatalf("download after delete = %d", rec.Code)
	}
}

func TestDumpValidation(t *testing.T) {
	h := newTestServer(t)
	cookie := setupAndLogin(t, h)
	db := createDatabase(t, h, cookie)
	rec := doJSON(t, h, "POST", "/api/workflows",
		map[string]any{"name": "x", "databaseId": db.ID, "destId": "!!!", "triggerKind": "manual"}, cookie)
	if rec.Code != 400 {
		t.Fatalf("bad destId = %d", rec.Code)
	}
	rec = doJSON(t, h, "POST", "/api/workflows",
		map[string]any{"name": "x", "databaseId": db.ID, "destId": encID(999), "triggerKind": "manual"}, cookie)
	if rec.Code != 400 {
		t.Fatalf("unknown destId = %d", rec.Code)
	}
	rec = doJSON(t, h, "POST", "/api/workflows",
		map[string]any{"name": "x", "databaseId": "AAAA", "triggerKind": "manual"}, cookie)
	if rec.Code != 404 {
		t.Fatalf("bad db = %d", rec.Code)
	}
}

func TestDumpDefaultLabel(t *testing.T) {
	h := newTestServer(t)
	cookie := setupAndLogin(t, h)
	db := createDatabase(t, h, cookie)
	_, d := dumpViaWorkflow(t, h, cookie, db.ID, "")
	if d.Label == "" {
		t.Fatal("default label missing")
	}
}

func TestDumpEngineFilter(t *testing.T) {
	h := newTestServer(t)
	cookie := setupAndLogin(t, h)
	createDatabase(t, h, cookie)
	rec := doJSON(t, h, "GET", "/api/dumps?engine=mongodb", nil, cookie)
	if rec.Code != 200 {
		t.Fatalf("code = %d", rec.Code)
	}
	var dumps []dumpDTO
	json.NewDecoder(rec.Body).Decode(&dumps)
	if len(dumps) != 0 {
		t.Fatalf("len = %d", len(dumps))
	}
}

func uploadRestore(t *testing.T, h http.Handler, cookie *http.Cookie, engine, content string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "backup.bin")
	fw.Write([]byte(content))
	mw.WriteField("engine", engine)
	mw.Close()
	req := httptest.NewRequest("POST", "/api/restores/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestUploadRestorePostgres(t *testing.T) {
	h := newTestServer(t)
	cookie := setupAndLogin(t, h)
	rec := uploadRestore(t, h, cookie, "postgres", "PGDMP-fake-archive-content")
	if rec.Code != 201 {
		t.Fatalf("upload = %d: %s", rec.Code, rec.Body.String())
	}
	var d dumpDTO
	json.NewDecoder(rec.Body).Decode(&d)
	if d.Status != "uploaded" || d.Engine != "postgres" || d.SourceDB != "" {
		t.Fatalf("dto = %+v", d)
	}
	rec = doJSON(t, h, "GET", "/api/dumps/"+d.ID+"/download", nil, cookie)
	if rec.Code != 200 || rec.Body.String() != "PGDMP-fake-archive-content" {
		t.Fatalf("download = %d %q", rec.Code, rec.Body.String())
	}
}

func TestUploadRestoreMagicMismatch(t *testing.T) {
	h := newTestServer(t)
	cookie := setupAndLogin(t, h)
	rec := uploadRestore(t, h, cookie, "mongodb", "PGDMP-not-gzip")
	if rec.Code != 400 {
		t.Fatalf("code = %d: %s", rec.Code, rec.Body.String())
	}
	var e struct {
		Error string `json:"error"`
	}
	json.NewDecoder(rec.Body).Decode(&e)
	if e.Error != "ENGINE_MISMATCH" {
		t.Fatalf("error = %s", e.Error)
	}
	rec = uploadRestore(t, h, cookie, "postgres", "\x1f\x8b-gzip-data")
	if rec.Code != 400 {
		t.Fatalf("code = %d", rec.Code)
	}
}

func TestListDumpsExposesStorageKind(t *testing.T) {
	h := newTestServer(t)
	cookie := setupAndLogin(t, h)
	dbID := createDBViaAPI(t, h, cookie)
	// seed a dump via workflow run (fake engine, local storage)
	rec := doJSON(t, h, "POST", "/api/workflows", map[string]any{
		"name": "w", "databaseId": dbID, "triggerKind": "manual",
	}, cookie)
	var wf struct {
		ID string `json:"id"`
	}
	json.Unmarshal(rec.Body.Bytes(), &wf)
	rec = doJSON(t, h, "POST", "/api/workflows/"+wf.ID+"/run", nil, cookie)
	if rec.Code != 201 {
		t.Fatalf("run = %d: %s", rec.Code, rec.Body.String())
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		rec = doJSON(t, h, "GET", "/api/dumps", nil, cookie)
		var dumps []map[string]any
		json.Unmarshal(rec.Body.Bytes(), &dumps)
		if len(dumps) > 0 {
			if dumps[0]["storageKind"] != "local" {
				t.Fatalf("storageKind = %v", dumps[0]["storageKind"])
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("dump never appeared")
}

func TestUploadRestoreBadEngine(t *testing.T) {
	h := newTestServer(t)
	cookie := setupAndLogin(t, h)
	rec := uploadRestore(t, h, cookie, "mysql", "whatever")
	if rec.Code != 400 {
		t.Fatalf("code = %d", rec.Code)
	}
}
