# Dump Workflows Implementation Plan — Chunk 2: API + Wiring

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Expose workflow CRUD + manual run over the JSON API, extend the destination delete-guard, remove the legacy direct-dump endpoint, and wire the scheduler into `main.go`.

**Architecture:** New handler file `internal/api/workflows.go` following the existing resource-file pattern. `api.Deps` is unchanged (handlers use `s.Runner.StartDump` from Chunk 1). `main.go` starts the scheduler goroutine after runner recovery.

**Tech Stack:** Go 1.26, `github.com/robfig/cron/v3`, stdlib `net/http` (Go 1.22+ pattern routing).

**Spec:** `docs/superpowers/specs/2026-10-02-dump-workflows-design.md`
**Depends on:** Chunk 1 complete (meta CRUD, `Runner.StartDump`, scheduler package).

## Global Constraints

- All external IDs are opaque base64: `encID`/`decID` (`internal/api/ids.go`).
- Response helpers: `jsonOut`, `fail`, `decodeBody` (`internal/api/respond.go`).
- Error codes match existing style: `VALIDATION` 400, `NOT_FOUND` 404, `JOB_ACTIVE` 409, `STORAGE_NOT_CONFIGURED` 400, `TOOL_MISSING` 400, `DESTINATION_IN_USE` 409.
- `runAt` travels as RFC3339; cron as 5-field standard cron string.
- Manual run works regardless of `enabled` and never shifts the schedule.

---

### Task 4: api — workflow handlers, routes, delete-guard, endpoint removal

**Files:**
- Create: `internal/api/workflows.go`
- Modify: `internal/api/server.go` (routes)
- Modify: `internal/api/dumps.go` (delete `handleCreateDump`, add `workflowName` to DTO)
- Modify: `internal/api/destinations.go` (extend delete-guard)
- Test: `internal/api/workflows_test.go`

**Interfaces:**
- Consumes: `Runner.StartDump(ctx, databaseID, destID int64, label string, workflowID, createdBy int64) (jobID, dumpID int64, err error)`, `runner.ErrToolsMissing`, `runner.ErrStorageUnavailable`, `meta.ErrJobActive`, meta workflow CRUD + `GetWorkflowRow` (Chunk 1).
- Produces: REST endpoints consumed by Chunk 3 frontend and Chunk 3 integration tests:
  - `GET /api/workflows` → `[{id, name, databaseId, databaseName, engine, destId, destName, triggerKind, runAt, cron, enabled, lastRunAt, lastError, nextRunAt, createdAt}]`
  - `POST /api/workflows` body `{name?, databaseId, destId, triggerKind, runAt?, cron?}` → 201 workflowDTO
  - `PUT /api/workflows/{id}` same body + `enabled` → 200 workflowDTO
  - `DELETE /api/workflows/{id}` → `{ok:true}`
  - `POST /api/workflows/{id}/run` → 201 `{jobId, dumpId}`
  - `dumpDTO` gains `workflowName: string`.

- [ ] **Step 1: Write the failing test**

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run 'TestWorkflow|TestDestinationDeleteGuardedByWorkflow' -v`
Expected: FAIL — 404s / compile errors (no workflows routes).

- [ ] **Step 3: Implement**

Create `internal/api/workflows.go`:

```go
package api

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/luthfi9251/ku-dump/internal/meta"
	"github.com/luthfi9251/ku-dump/internal/runner"
)

type workflowDTO struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	DatabaseID   string `json:"databaseId"`
	DatabaseName string `json:"databaseName"`
	Engine       string `json:"engine"`
	DestID       string `json:"destId"`
	DestName     string `json:"destName"`
	TriggerKind  string `json:"triggerKind"`
	RunAt        string `json:"runAt"`
	Cron         string `json:"cron"`
	Enabled      bool   `json:"enabled"`
	LastRunAt    string `json:"lastRunAt"`
	LastError    string `json:"lastError"`
	NextRunAt    string `json:"nextRunAt"`
	CreatedAt    string `json:"createdAt"`
}

func timePtrRFC3339(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(time.RFC3339)
}

func workflowToDTO(r meta.WorkflowRow) workflowDTO {
	return workflowDTO{
		ID: encID(r.ID), Name: r.Name,
		DatabaseID: encID(r.DatabaseID), DatabaseName: r.DatabaseName, Engine: r.DatabaseEngine,
		DestID: encIDOrEmpty(r.DestID), DestName: r.DestName,
		TriggerKind: r.TriggerKind, RunAt: timePtrRFC3339(r.RunAt), Cron: r.Cron,
		Enabled: r.Enabled, LastRunAt: timePtrRFC3339(r.LastRunAt),
		LastError: r.LastError, NextRunAt: timePtrRFC3339(r.NextRunAt),
		CreatedAt: r.CreatedAt.Format(time.RFC3339),
	}
}

type workflowPayload struct {
	Name        string `json:"name"`
	DatabaseID  string `json:"databaseId"`
	DestID      string `json:"destId"`
	TriggerKind string `json:"triggerKind"`
	RunAt       string `json:"runAt"`
	Cron        string `json:"cron"`
	Enabled     *bool  `json:"enabled"`
}

// validateWorkflowTrigger returns run_at, cron and next_run_at for the given
// trigger kind. next_run_at: manual -> nil, once -> run_at, cron -> Next(now).
func validateWorkflowTrigger(kind, runAt, cronExpr string, now time.Time) (*time.Time, string, *time.Time, error) {
	switch kind {
	case "manual":
		return nil, "", nil, nil
	case "once":
		if runAt == "" {
			return nil, "", nil, errors.New("runAt is required for once triggers")
		}
		t, err := time.Parse(time.RFC3339, runAt)
		if err != nil {
			return nil, "", nil, errors.New("runAt must be an RFC3339 timestamp")
		}
		if !t.After(now) {
			return nil, "", nil, errors.New("runAt must be in the future")
		}
		return &t, "", &t, nil
	case "cron":
		sched, err := cron.ParseStandard(cronExpr)
		if err != nil {
			return nil, "", nil, fmt.Errorf("invalid cron: %v", err)
		}
		t := sched.Next(now)
		return nil, cronExpr, &t, nil
	default:
		return nil, "", nil, errors.New("triggerKind must be manual, once or cron")
	}
}

func (s *Server) getWorkflowOr404(w http.ResponseWriter, r *http.Request) (*meta.Workflow, bool) {
	id, err := decID(r.PathValue("id"))
	if err != nil {
		fail(w, http.StatusNotFound, "NOT_FOUND", "workflow not found")
		return nil, false
	}
	wf, err := s.Store.GetWorkflow(r.Context(), id)
	if err != nil {
		fail(w, http.StatusNotFound, "NOT_FOUND", "workflow not found")
		return nil, false
	}
	return wf, true
}

func (s *Server) resolveWorkflowRefs(w http.ResponseWriter, r *http.Request, p workflowPayload) (dbID, destID int64, ok bool) {
	dbID, err := decID(p.DatabaseID)
	if err != nil {
		fail(w, http.StatusNotFound, "NOT_FOUND", "database not found")
		return 0, 0, false
	}
	if _, err := s.Store.GetDatabase(r.Context(), dbID); err != nil {
		fail(w, http.StatusNotFound, "NOT_FOUND", "database not found")
		return 0, 0, false
	}
	if p.DestID != "" {
		destID, err = decID(p.DestID)
		if err != nil {
			fail(w, http.StatusBadRequest, "VALIDATION", "invalid destId")
			return 0, 0, false
		}
		if _, err := s.Store.GetDestination(r.Context(), destID); err != nil {
			fail(w, http.StatusBadRequest, "STORAGE_NOT_CONFIGURED", "storage destination not found")
			return 0, 0, false
		}
	}
	return dbID, destID, true
}

func (s *Server) handleListWorkflows(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Store.ListWorkflows(r.Context())
	if err != nil {
		log.Printf("list workflows: %v", err)
		fail(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	out := make([]workflowDTO, 0, len(rows))
	for i := range rows {
		out = append(out, workflowToDTO(rows[i]))
	}
	jsonOut(w, http.StatusOK, out)
}

func (s *Server) handleCreateWorkflow(w http.ResponseWriter, r *http.Request) {
	var p workflowPayload
	if !decodeBody(w, r, &p) {
		return
	}
	dbID, destID, ok := s.resolveWorkflowRefs(w, r, p)
	if !ok {
		return
	}
	runAt, cronExpr, next, err := validateWorkflowTrigger(p.TriggerKind, p.RunAt, p.Cron, time.Now())
	if err != nil {
		fail(w, http.StatusBadRequest, "VALIDATION", err.Error())
		return
	}
	db, _ := s.Store.GetDatabase(r.Context(), dbID)
	name := p.Name
	if name == "" {
		name = db.Name + " (" + db.Engine + ")"
	}
	enabled := true
	if p.Enabled != nil {
		enabled = *p.Enabled
	}
	wfID, err := s.Store.CreateWorkflow(r.Context(), &meta.Workflow{
		Name: name, DatabaseID: dbID, DestID: destID,
		TriggerKind: p.TriggerKind, RunAt: runAt, Cron: cronExpr,
		Enabled: enabled, NextRunAt: next,
	})
	if err != nil {
		log.Printf("create workflow: %v", err)
		fail(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	row, err := s.Store.GetWorkflowRow(r.Context(), wfID)
	if err != nil {
		log.Printf("get workflow row: %v", err)
		fail(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	jsonOut(w, http.StatusCreated, workflowToDTO(*row))
}

func (s *Server) handleUpdateWorkflow(w http.ResponseWriter, r *http.Request) {
	current, ok := s.getWorkflowOr404(w, r)
	if !ok {
		return
	}
	var p workflowPayload
	if !decodeBody(w, r, &p) {
		return
	}
	dbID, destID, ok := s.resolveWorkflowRefs(w, r, p)
	if !ok {
		return
	}
	runAt, cronExpr, next, err := validateWorkflowTrigger(p.TriggerKind, p.RunAt, p.Cron, time.Now())
	if err != nil {
		fail(w, http.StatusBadRequest, "VALIDATION", err.Error())
		return
	}
	enabled := current.Enabled
	if p.Enabled != nil {
		enabled = *p.Enabled
	}
	name := p.Name
	if name == "" {
		db, _ := s.Store.GetDatabase(r.Context(), dbID)
		name = db.Name + " (" + db.Engine + ")"
	}
	updated := *current
	updated.Name = name
	updated.DatabaseID = dbID
	updated.DestID = destID
	updated.TriggerKind = p.TriggerKind
	updated.RunAt = runAt
	updated.Cron = cronExpr
	updated.Enabled = enabled
	updated.NextRunAt = next
	if err := s.Store.UpdateWorkflow(r.Context(), &updated); err != nil {
		log.Printf("update workflow %d: %v", current.ID, err)
		fail(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	row, err := s.Store.GetWorkflowRow(r.Context(), current.ID)
	if err != nil {
		log.Printf("get workflow row: %v", err)
		fail(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	jsonOut(w, http.StatusOK, workflowToDTO(*row))
}

func (s *Server) handleDeleteWorkflow(w http.ResponseWriter, r *http.Request) {
	wf, ok := s.getWorkflowOr404(w, r)
	if !ok {
		return
	}
	if err := s.Store.DeleteWorkflow(r.Context(), wf.ID); err != nil {
		log.Printf("delete workflow %d: %v", wf.ID, err)
		fail(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	jsonOut(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleRunWorkflow(w http.ResponseWriter, r *http.Request) {
	wf, ok := s.getWorkflowOr404(w, r)
	if !ok {
		return
	}
	jobID, dumpID, err := s.Runner.StartDump(r.Context(), wf.DatabaseID, wf.DestID, wf.Name, wf.ID, userID(r))
	if err != nil {
		switch {
		case errors.Is(err, meta.ErrJobActive):
			fail(w, http.StatusConflict, "JOB_ACTIVE", "a job is already running for this database")
		case errors.Is(err, runner.ErrToolsMissing):
			fail(w, http.StatusBadRequest, "TOOL_MISSING", err.Error())
		case errors.Is(err, runner.ErrStorageUnavailable):
			fail(w, http.StatusBadRequest, "STORAGE_NOT_CONFIGURED", err.Error())
		default:
			log.Printf("run workflow %d: %v", wf.ID, err)
			fail(w, http.StatusNotFound, "NOT_FOUND", "workflow target not found")
		}
		return
	}
	jsonOut(w, http.StatusCreated, map[string]string{"jobId": encID(jobID), "dumpId": encID(dumpID)})
}
```

**`GetWorkflowRow` — add to `internal/meta/workflows.go`:** the DTO needs
`DatabaseName`/`DatabaseEngine`/`DestName`, which plain `GetWorkflow` doesn't
return. `ListWorkflows` is tiny at real-world scale, so reuse its joins:

```go
func (s *Store) GetWorkflowRow(ctx context.Context, id int64) (*WorkflowRow, error) {
	rows, err := s.ListWorkflows(ctx)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		if rows[i].ID == id {
			return &rows[i], nil
		}
	}
	return nil, ErrNotFound
}
```

`internal/api/server.go` — replace the dump route with workflow routes. Delete:

```go
s.mux.HandleFunc("POST /api/databases/{id}/dump", s.auth(s.handleCreateDump))
```

Add (after the databases routes):

```go
s.mux.HandleFunc("GET /api/workflows", s.auth(s.handleListWorkflows))
s.mux.HandleFunc("POST /api/workflows", s.auth(s.handleCreateWorkflow))
s.mux.HandleFunc("PUT /api/workflows/{id}", s.auth(s.handleUpdateWorkflow))
s.mux.HandleFunc("DELETE /api/workflows/{id}", s.auth(s.handleDeleteWorkflow))
s.mux.HandleFunc("POST /api/workflows/{id}/run", s.auth(s.handleRunWorkflow))
```

`internal/api/dumps.go` — delete the whole `handleCreateDump` function and
drop now-unused imports (`errors`, `strings`). Extend `dumpDTO` and
`dumpToDTO`:

```go
type dumpDTO struct {
	// ... existing fields ...
	WorkflowName string `json:"workflowName"`
}

func dumpToDTO(r meta.DumpRow) dumpDTO {
	return dumpDTO{
		// ... existing assignments ...
		WorkflowName: r.WorkflowName,
	}
}
```

`internal/api/destinations.go` — in `handleDeleteDestination`, after the
existing dumps-count check:

```go
	wn, err := s.Store.CountWorkflowsForDestination(r.Context(), id)
	if err != nil {
		log.Printf("count workflows for destination %d: %v", id, err)
		fail(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	if wn > 0 {
		fail(w, http.StatusConflict, "DESTINATION_IN_USE",
			"destination is referenced by a workflow; update or delete the workflow first")
		return
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/api/ -v && go build ./... && go test ./...`
Expected: PASS (all existing api tests too — old dump endpoint had no direct
api unit test; integration coverage moves in Chunk 3 Task 8).

- [ ] **Step 5: Commit**

```bash
git add internal/api/ internal/meta/
git commit -m "feat(api): dump workflows CRUD + run, destination guard, drop legacy dump endpoint"
```

---

### Task 5: main.go wiring

**Files:**
- Modify: `cmd/ku-dump/main.go`

**Interfaces:**
- Consumes: `scheduler.New(st *meta.Store, start scheduler.Starter)`, `(*Scheduler).Recover(ctx, now)`, `(*Scheduler).Run(ctx)` from Chunk 1; `*runner.Runner` satisfies `scheduler.Starter` via its `StartDump` method.
- Produces: boot order — runner recovery, then schedule recovery, then serve.

- [ ] **Step 1: Wire the scheduler**

In `cmd/ku-dump/main.go`, after `run.Recover(ctx)` and before `api.NewServer`, add:

```go
	sched := scheduler.New(st, run)
	if err := sched.Recover(ctx, time.Now()); err != nil {
		log.Fatal(err)
	}
	schedCtx, stopSched := context.WithCancel(ctx)
	defer stopSched()
	go sched.Run(schedCtx)
```

Add imports `"time"` and `"github.com/luthfi9251/ku-dump/internal/scheduler"`.

- [ ] **Step 2: Verify build + full unit suite**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: PASS. (`*runner.Runner` satisfies `scheduler.Starter` — compile check.)

- [ ] **Step 3: Smoke test manually**

```bash
go run ./cmd/ku-dump &
sleep 2
curl -s localhost:8080/api/auth/status
kill %1
```
Expected: `{"needsSetup":true}` — server boots with scheduler running.

- [ ] **Step 4: Commit**

```bash
git add cmd/ku-dump/main.go
git commit -m "feat(main): start dump workflow scheduler with boot recovery"
```
