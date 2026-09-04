package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func waitJobTerminal(t *testing.T, h http.Handler, cookie *http.Cookie, jobID string) jobDTO {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		rec := doJSON(t, h, "GET", "/api/jobs/"+jobID, nil, cookie)
		if rec.Code == 200 {
			var j jobDTO
			json.NewDecoder(rec.Body).Decode(&j)
			if j.Status == "success" || j.Status == "failed" || j.Status == "cancelled" {
				return j
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("job not terminal in time")
	return jobDTO{}
}

func TestJobListAndDetail(t *testing.T) {
	h := newTestServer(t)
	cookie := setupAndLogin(t, h)
	db := createDatabase(t, h, cookie)
	rec := doJSON(t, h, "POST", "/api/databases/"+db.ID+"/dump",
		map[string]string{"destId": ""}, cookie)
	var created struct {
		JobID  string `json:"jobId"`
		DumpID string `json:"dumpId"`
	}
	json.NewDecoder(rec.Body).Decode(&created)
	j := waitJobTerminal(t, h, cookie, created.JobID)
	if j.Status != "success" {
		t.Fatalf("job = %+v", j)
	}

	rec = doJSON(t, h, "GET", "/api/jobs", nil, cookie)
	if rec.Code != 200 {
		t.Fatalf("list = %d", rec.Code)
	}
	var jobs []jobDTO
	json.NewDecoder(rec.Body).Decode(&jobs)
	if len(jobs) != 1 {
		t.Fatalf("len = %d", len(jobs))
	}
	got := jobs[0]
	if got.ID != created.JobID || got.Type != "dump" || got.Status != "success" ||
		got.DatabaseName != "prod" || got.DumpLabel == "" {
		t.Fatalf("job = %+v", got)
	}
	if got.Error != "" {
		t.Fatalf("error = %q", got.Error)
	}

	rec = doJSON(t, h, "GET", "/api/jobs/"+created.JobID, nil, cookie)
	if rec.Code != 200 {
		t.Fatalf("detail = %d", rec.Code)
	}
	var detail jobDTO
	json.NewDecoder(rec.Body).Decode(&detail)
	if !strings.Contains(detail.LogTail, "dumping") {
		t.Fatalf("logTail = %q", detail.LogTail)
	}
}

func TestJobDetailNotFound(t *testing.T) {
	h := newTestServer(t)
	cookie := setupAndLogin(t, h)
	rec := doJSON(t, h, "GET", "/api/jobs/AAAA", nil, cookie)
	if rec.Code != 404 {
		t.Fatalf("code = %d", rec.Code)
	}
}

func TestJobCancelTerminalConflict(t *testing.T) {
	h := newTestServer(t)
	cookie := setupAndLogin(t, h)
	db := createDatabase(t, h, cookie)
	rec := doJSON(t, h, "POST", "/api/databases/"+db.ID+"/dump",
		map[string]string{"destId": ""}, cookie)
	var created struct {
		JobID  string `json:"jobId"`
		DumpID string `json:"dumpId"`
	}
	json.NewDecoder(rec.Body).Decode(&created)
	waitDumpReady(t, h, cookie, created.DumpID)
	rec = doJSON(t, h, "POST", "/api/jobs/"+created.JobID+"/cancel", nil, cookie)
	if rec.Code != 409 {
		t.Fatalf("cancel terminal = %d: %s", rec.Code, rec.Body.String())
	}
}
