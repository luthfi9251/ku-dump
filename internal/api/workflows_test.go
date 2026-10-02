package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func createDBViaAPI(t *testing.T, h http.Handler, cookie *http.Cookie) string {
	t.Helper()
	rec := doJSON(t, h, "POST", "/api/databases", map[string]any{
		"name": "pg1", "engine": "postgres", "host": "127.0.0.1", "port": 5432,
		"dbName": "db1", "username": "u", "password": "p",
	}, cookie)
	if rec.Code != 201 {
		t.Fatalf("create database = %d: %s", rec.Code, rec.Body.String())
	}
	var db struct {
		ID string `json:"id"`
	}
	json.Unmarshal(rec.Body.Bytes(), &db)
	return db.ID
}

func getWorkflow(t *testing.T, h http.Handler, cookie *http.Cookie, id string) map[string]any {
	t.Helper()
	rec := doJSON(t, h, "GET", "/api/workflows", nil, cookie)
	if rec.Code != 200 {
		t.Fatalf("list workflows = %d", rec.Code)
	}
	var out []map[string]any
	json.Unmarshal(rec.Body.Bytes(), &out)
	for _, wf := range out {
		if wf["id"] == id {
			return wf
		}
	}
	t.Fatalf("workflow %s not found", id)
	return nil
}

func TestWorkflowCRUDAndValidation(t *testing.T) {
	h := newTestServer(t)
	cookie := setupAndLogin(t, h)
	dbID := createDBViaAPI(t, h, cookie)

	// invalid cron rejected at save time
	rec := doJSON(t, h, "POST", "/api/workflows", map[string]any{
		"name": "bad", "databaseId": dbID, "triggerKind": "cron", "cron": "not a cron",
	}, cookie)
	if rec.Code != 400 {
		t.Fatalf("invalid cron = %d", rec.Code)
	}
	// once with past runAt rejected
	rec = doJSON(t, h, "POST", "/api/workflows", map[string]any{
		"name": "past", "databaseId": dbID, "triggerKind": "once",
		"runAt": time.Now().Add(-time.Hour).Format(time.RFC3339),
	}, cookie)
	if rec.Code != 400 {
		t.Fatalf("past runAt = %d", rec.Code)
	}
	// unknown database
	rec = doJSON(t, h, "POST", "/api/workflows", map[string]any{
		"databaseId": "MTIz", "triggerKind": "manual",
	}, cookie)
	if rec.Code != 404 {
		t.Fatalf("unknown db = %d", rec.Code)
	}

	// create cron workflow, name defaulted
	rec = doJSON(t, h, "POST", "/api/workflows", map[string]any{
		"databaseId": dbID, "destId": "", "triggerKind": "cron", "cron": "0 2 * * *",
	}, cookie)
	if rec.Code != 201 {
		t.Fatalf("create = %d: %s", rec.Code, rec.Body.String())
	}
	var wf struct {
		ID           string `json:"id"`
		Name         string `json:"name"`
		DatabaseName string `json:"databaseName"`
		DestName     string `json:"destName"`
		NextRunAt    string `json:"nextRunAt"`
	}
	json.Unmarshal(rec.Body.Bytes(), &wf)
	if wf.Name != "pg1 (postgres)" || wf.DatabaseName != "pg1" || wf.DestName != "local" || wf.NextRunAt == "" {
		t.Fatalf("created = %+v", wf)
	}

	// default triggerKind manual when created as manual, nextRunAt empty
	rec = doJSON(t, h, "POST", "/api/workflows", map[string]any{
		"name": "m", "databaseId": dbID, "triggerKind": "manual",
	}, cookie)
	if rec.Code != 201 {
		t.Fatalf("create manual = %d", rec.Code)
	}
	var manual struct {
		ID        string `json:"id"`
		NextRunAt string `json:"nextRunAt"`
	}
	json.Unmarshal(rec.Body.Bytes(), &manual)
	if manual.NextRunAt != "" {
		t.Fatalf("manual nextRunAt = %q", manual.NextRunAt)
	}

	// update: switch to once + pause
	rec = doJSON(t, h, "PUT", "/api/workflows/"+manual.ID, map[string]any{
		"name": "m2", "databaseId": dbID, "destId": "", "triggerKind": "once",
		"runAt": time.Now().Add(time.Hour).Format(time.RFC3339), "enabled": false,
	}, cookie)
	if rec.Code != 200 {
		t.Fatalf("update = %d: %s", rec.Code, rec.Body.String())
	}
	got := getWorkflow(t, h, cookie, manual.ID)
	if got["name"] != "m2" || got["triggerKind"] != "once" || got["enabled"] != false {
		t.Fatalf("updated = %+v", got)
	}

	// run a paused manual workflow is allowed, but this one is 'once' now;
	// create a dedicated paused manual one to prove run works while disabled
	rec = doJSON(t, h, "POST", "/api/workflows", map[string]any{
		"name": "paused-manual", "databaseId": dbID, "triggerKind": "manual", "enabled": false,
	}, cookie)
	var paused struct {
		ID string `json:"id"`
	}
	json.Unmarshal(rec.Body.Bytes(), &paused)
	rec = doJSON(t, h, "POST", "/api/workflows/"+paused.ID+"/run", nil, cookie)
	if rec.Code != 201 {
		t.Fatalf("run while disabled = %d: %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, h, "DELETE", "/api/workflows/"+manual.ID, nil, cookie)
	if rec.Code != 200 {
		t.Fatalf("delete = %d", rec.Code)
	}
	rec = doJSON(t, h, "GET", "/api/workflows", nil, cookie)
	var out []map[string]any
	json.Unmarshal(rec.Body.Bytes(), &out)
	for _, wf := range out {
		if wf["id"] == manual.ID {
			t.Fatal("workflow not deleted")
		}
	}
}

func TestWorkflowRunCreatesDump(t *testing.T) {
	h := newTestServer(t)
	cookie := setupAndLogin(t, h)
	dbID := createDBViaAPI(t, h, cookie)

	rec := doJSON(t, h, "POST", "/api/workflows", map[string]any{
		"name": "run-me", "databaseId": dbID, "destId": "", "triggerKind": "manual",
	}, cookie)
	var wf struct {
		ID string `json:"id"`
	}
	json.Unmarshal(rec.Body.Bytes(), &wf)

	rec = doJSON(t, h, "POST", "/api/workflows/"+wf.ID+"/run", nil, cookie)
	if rec.Code != 201 {
		t.Fatalf("run = %d: %s", rec.Code, rec.Body.String())
	}
	var runRes struct {
		JobID  string `json:"jobId"`
		DumpID string `json:"dumpId"`
	}
	json.Unmarshal(rec.Body.Bytes(), &runRes)
	if runRes.JobID == "" || runRes.DumpID == "" {
		t.Fatalf("run response = %+v", runRes)
	}

	// fake engine is fast; wait for success
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		rec := doJSON(t, h, "GET", "/api/jobs/"+runRes.JobID, nil, cookie)
		if rec.Code == 200 {
			var j struct {
				Status string `json:"status"`
				Error  string `json:"error"`
			}
			json.Unmarshal(rec.Body.Bytes(), &j)
			if j.Status == "success" {
				// dump carries workflow provenance
				w := getWorkflow(t, h, cookie, wf.ID)
				if w["lastRunAt"] == "" || w["lastError"] != "" {
					t.Fatalf("workflow bookkeeping = %+v", w)
				}
				rec = doJSON(t, h, "GET", "/api/dumps", nil, cookie)
				var dumps []map[string]any
				json.Unmarshal(rec.Body.Bytes(), &dumps)
				for _, d := range dumps {
					if d["id"] == runRes.DumpID {
						if d["workflowName"] != "run-me" {
							t.Fatalf("workflowName = %v", d["workflowName"])
						}
						return
					}
				}
				t.Fatal("dump not listed")
			}
			if j.Status == "failed" || j.Status == "cancelled" {
				t.Fatalf("job %s: %s", j.Status, j.Error)
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("job did not finish")
}

func TestDestinationDeleteGuardedByWorkflow(t *testing.T) {
	h := newTestServer(t)
	cookie := setupAndLogin(t, h)
	dbID := createDBViaAPI(t, h, cookie)

	rec := doJSON(t, h, "POST", "/api/storage/destinations", map[string]any{
		"name": "d1", "endpoint": "http://e", "bucket": "b",
		"accessKey": "k", "secretKey": "s",
	}, cookie)
	if rec.Code != 201 {
		t.Fatalf("create dest = %d: %s", rec.Code, rec.Body.String())
	}
	var dest struct {
		ID string `json:"id"`
	}
	json.Unmarshal(rec.Body.Bytes(), &dest)

	rec = doJSON(t, h, "POST", "/api/workflows", map[string]any{
		"name": "w", "databaseId": dbID, "destId": dest.ID, "triggerKind": "manual",
	}, cookie)
	if rec.Code != 201 {
		t.Fatalf("create wf = %d: %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, h, "DELETE", "/api/storage/destinations/"+dest.ID, nil, cookie)
	if rec.Code != 409 {
		t.Fatalf("delete guarded dest = %d: %s", rec.Code, rec.Body.String())
	}
}
