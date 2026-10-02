package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

type restorePayload struct {
	DumpID           string `json:"dumpId"`
	TargetDatabaseID string `json:"targetDatabaseId"`
	ConfirmName      string `json:"confirmName"`
}

func dumpSomething(t *testing.T, h http.Handler, cookie *http.Cookie) dumpDTO {
	t.Helper()
	db := createDatabase(t, h, cookie)
	_, d := dumpViaWorkflow(t, h, cookie, db.ID, "")
	return d
}

func createTargetDB(t *testing.T, h http.Handler, cookie *http.Cookie, name string) databaseDTO {
	t.Helper()
	p := validPayload()
	p["name"] = name
	rec := doJSON(t, h, "POST", "/api/databases", p, cookie)
	if rec.Code != 201 {
		t.Fatalf("create target = %d: %s", rec.Code, rec.Body.String())
	}
	var d databaseDTO
	json.NewDecoder(rec.Body).Decode(&d)
	return d
}

func TestRestoreHappyPath(t *testing.T) {
	h := newTestServer(t)
	cookie := setupAndLogin(t, h)
	d := dumpSomething(t, h, cookie)
	target := createTargetDB(t, h, cookie, "dev")
	rec := doJSON(t, h, "POST", "/api/restores", restorePayload{
		DumpID: d.ID, TargetDatabaseID: target.ID, ConfirmName: "dev",
	}, cookie)
	if rec.Code != 201 {
		t.Fatalf("restore = %d: %s", rec.Code, rec.Body.String())
	}
	var created struct {
		JobID string `json:"jobId"`
	}
	json.NewDecoder(rec.Body).Decode(&created)
	if created.JobID == "" {
		t.Fatal("jobId empty")
	}
}

func TestRestoreEngineMismatch(t *testing.T) {
	h := newTestServer(t)
	cookie := setupAndLogin(t, h)
	d := dumpSomething(t, h, cookie)
	p := validPayload()
	p["name"] = "mongo-target"
	p["engine"] = "mongodb"
	rec := doJSON(t, h, "POST", "/api/databases", p, cookie)
	if rec.Code != 201 {
		t.Fatalf("mongo target = %d: %s", rec.Code, rec.Body.String())
	}
	var mongoTarget databaseDTO
	json.NewDecoder(rec.Body).Decode(&mongoTarget)
	rec = doJSON(t, h, "POST", "/api/restores", restorePayload{
		DumpID: d.ID, TargetDatabaseID: mongoTarget.ID, ConfirmName: "mongo-target",
	}, cookie)
	if rec.Code != 400 {
		t.Fatalf("code = %d", rec.Code)
	}
	var e struct {
		Error string `json:"error"`
	}
	json.NewDecoder(rec.Body).Decode(&e)
	if e.Error != "ENGINE_MISMATCH" {
		t.Fatalf("error = %s", e.Error)
	}
}

func TestRestoreConfirmMismatch(t *testing.T) {
	h := newTestServer(t)
	cookie := setupAndLogin(t, h)
	d := dumpSomething(t, h, cookie)
	target := createTargetDB(t, h, cookie, "dev")
	rec := doJSON(t, h, "POST", "/api/restores", restorePayload{
		DumpID: d.ID, TargetDatabaseID: target.ID, ConfirmName: "wrong",
	}, cookie)
	if rec.Code != 400 {
		t.Fatalf("code = %d", rec.Code)
	}
	var e struct {
		Error string `json:"error"`
	}
	json.NewDecoder(rec.Body).Decode(&e)
	if e.Error != "CONFIRM_MISMATCH" {
		t.Fatalf("error = %s", e.Error)
	}
}

func TestRestoreActiveJobBlocked(t *testing.T) {
	h := newTestServer(t)
	cookie := setupAndLogin(t, h)
	d := dumpSomething(t, h, cookie)
	p := validPayload()
	p["name"] = "slow-target"
	p["dbName"] = "slow"
	rec := doJSON(t, h, "POST", "/api/databases", p, cookie)
	if rec.Code != 201 {
		t.Fatalf("create target = %d: %s", rec.Code, rec.Body.String())
	}
	var target databaseDTO
	json.NewDecoder(rec.Body).Decode(&target)
	rec = doJSON(t, h, "POST", "/api/restores", restorePayload{
		DumpID: d.ID, TargetDatabaseID: target.ID, ConfirmName: "slow-target",
	}, cookie)
	if rec.Code != 201 {
		t.Fatalf("first restore = %d: %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, h, "POST", "/api/restores", restorePayload{
		DumpID: d.ID, TargetDatabaseID: target.ID, ConfirmName: "slow-target",
	}, cookie)
	if rec.Code != 409 {
		t.Fatalf("second restore = %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRestoreNotFound(t *testing.T) {
	h := newTestServer(t)
	cookie := setupAndLogin(t, h)
	target := createTargetDB(t, h, cookie, "dev")
	rec := doJSON(t, h, "POST", "/api/restores", restorePayload{
		DumpID: "AAAA", TargetDatabaseID: target.ID, ConfirmName: "dev",
	}, cookie)
	if rec.Code != 404 {
		t.Fatalf("code = %d", rec.Code)
	}
}
