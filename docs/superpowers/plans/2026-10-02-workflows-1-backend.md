# Dump Workflows Implementation Plan — Chunk 1: Backend Core

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the `dump_workflows` metadata layer, a shared `Runner.StartDump` path, and an in-process scheduler that turns due workflows into dump jobs.

**Architecture:** New SQLite table `dump_workflows` + CRUD in `internal/meta`. Dump-creation logic moves from the API handler into `Runner.StartDump` (one run path). New `internal/scheduler` package ticks every 30s, runs due workflows, and recomputes schedules on boot.

**Tech Stack:** Go 1.26, `modernc.org/sqlite`, new dep `github.com/robfig/cron/v3` (cron validation + `Next`).

**Spec:** `docs/superpowers/specs/2026-10-02-dump-workflows-design.md` — read it first.

## Global Constraints

- Go 1.26; only new dependency allowed: `github.com/robfig/cron/v3`.
- Timestamps in SQLite are TEXT `'2006-01-02 15:04:05'` UTC (existing convention, `meta.parseTime`).
- Timezone: server local (`time.Now()`, `time.Local`) everywhere outside the DB.
- All timestamps passed to queries must be UTC-formatted via `timeLayout`.
- Follow existing code style: plain `*sql.DB` methods on `meta.Store`, no interfaces, `gofmt`.
- Tests: stdlib `testing` only, no new test frameworks.

---

### Task 1: meta — `dump_workflows` table, CRUD, `dumps.workflow_id`

**Files:**
- Modify: `internal/meta/meta.go` (schema + DSN + ensureColumn)
- Create: `internal/meta/workflows.go`
- Modify: `internal/meta/dumps.go` (WorkflowID field + join)
- Test: `internal/meta/workflows_test.go`

**Interfaces (produces, used by Tasks 2–4):**
```go
type Workflow struct {
    ID int64; Name string; DatabaseID, DestID int64
    TriggerKind string            // "manual" | "once" | "cron"
    RunAt, NextRunAt, LastRunAt *time.Time
    Cron, LastError string; Enabled bool; CreatedAt time.Time
}
type WorkflowRow struct { Workflow; DatabaseName, DatabaseEngine, DestName string }

func (s *Store) CreateWorkflow(ctx context.Context, wf *Workflow) (int64, error)
func (s *Store) GetWorkflow(ctx context.Context, id int64) (*Workflow, error)
func (s *Store) ListWorkflows(ctx context.Context) ([]WorkflowRow, error)
func (s *Store) UpdateWorkflow(ctx context.Context, wf *Workflow) error // full update incl. run bookkeeping
func (s *Store) DeleteWorkflow(ctx context.Context, id int64) error
func (s *Store) DueWorkflows(ctx context.Context, now time.Time) ([]Workflow, error)
func (s *Store) SetWorkflowState(ctx context.Context, id int64, lastRunAt *time.Time, lastErr string, enabled bool, nextRunAt *time.Time) error
func (s *Store) CountWorkflowsForDestination(ctx context.Context, destID int64) (int64, error)
```
`Dump` gains `WorkflowID int64`; `DumpRow` gains `WorkflowName string`.

- [ ] **Step 1: Write the failing test**

```go
package meta

import (
	"context"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func fixtureDB(t *testing.T, st *Store, ctx context.Context) int64 {
	t.Helper()
	id, err := st.CreateDatabase(ctx, &Database{Name: "pg1", Engine: "postgres",
		Host: "h", Port: 5432, DBName: "db1", Username: "u", PasswordEnc: "x"})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestWorkflowCRUD(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	dbID := fixtureDB(t, st, ctx)

	runAt := time.Now().Add(time.Hour)
	wfID, err := st.CreateWorkflow(ctx, &Workflow{
		Name: "nightly", DatabaseID: dbID, DestID: 0,
		TriggerKind: "once", RunAt: &runAt, Enabled: true, NextRunAt: &runAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	wf, err := st.GetWorkflow(ctx, wfID)
	if err != nil || wf.Name != "nightly" || wf.TriggerKind != "once" || !wf.Enabled ||
		wf.NextRunAt == nil || wf.NextRunAt.Unix() != runAt.Unix() {
		t.Fatalf("get = %+v, %v", wf, err)
	}

	rows, err := st.ListWorkflows(ctx)
	if err != nil || len(rows) != 1 {
		t.Fatalf("list = %v, %v", rows, err)
	}
	if rows[0].DatabaseName != "pg1" || rows[0].DatabaseEngine != "postgres" || rows[0].DestName != "local" {
		t.Fatalf("joins = %+v", rows[0])
	}

	next := time.Now().Add(2 * time.Hour)
	wf.Cron = "0 2 * * *"
	wf.TriggerKind = "cron"
	wf.RunAt = nil
	wf.NextRunAt = &next
	if err := st.UpdateWorkflow(ctx, wf); err != nil {
		t.Fatal(err)
	}
	got, _ := st.GetWorkflow(ctx, wfID)
	if got.Cron != "0 2 * * *" || got.RunAt != nil || got.NextRunAt == nil || got.NextRunAt.Unix() != next.Unix() {
		t.Fatalf("after update = %+v", got)
	}

	if err := st.DeleteWorkflow(ctx, wfID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetWorkflow(ctx, wfID); err != ErrNotFound {
		t.Fatalf("deleted get = %v", err)
	}
}

func TestDueWorkflows(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	dbID := fixtureDB(t, st, ctx)
	now := time.Now()

	past := now.Add(-time.Minute)
	future := now.Add(time.Hour)
	dueID, _ := st.CreateWorkflow(ctx, &Workflow{Name: "due", DatabaseID: dbID,
		TriggerKind: "cron", Cron: "* * * * *", Enabled: true, NextRunAt: &past})
	_, _ = st.CreateWorkflow(ctx, &Workflow{Name: "future", DatabaseID: dbID,
		TriggerKind: "cron", Cron: "* * * * *", Enabled: true, NextRunAt: &future})
	_, _ = st.CreateWorkflow(ctx, &Workflow{Name: "paused", DatabaseID: dbID,
		TriggerKind: "cron", Cron: "* * * * *", Enabled: false, NextRunAt: &past})
	_, _ = st.CreateWorkflow(ctx, &Workflow{Name: "manual", DatabaseID: dbID,
		TriggerKind: "manual", Enabled: true})

	due, err := st.DueWorkflows(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 1 || due[0].ID != dueID {
		t.Fatalf("due = %+v", due)
	}
}

func TestSetWorkflowState(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	dbID := fixtureDB(t, st, ctx)
	now := time.Now()

	wfID, _ := st.CreateWorkflow(ctx, &Workflow{Name: "w", DatabaseID: dbID,
		TriggerKind: "cron", Cron: "* * * * *", Enabled: true, NextRunAt: &now})

	// storage failure: keep next_run_at + enabled, set last_error
	if err := st.SetWorkflowState(ctx, wfID, nil, "storage unavailable", true, &now); err != nil {
		t.Fatal(err)
	}
	wf, _ := st.GetWorkflow(ctx, wfID)
	if wf.LastError != "storage unavailable" || !wf.Enabled || wf.NextRunAt == nil {
		t.Fatalf("retry state = %+v", wf)
	}

	// advance: last_run set, error cleared, next moved
	next := now.Add(5 * time.Minute)
	if err := st.SetWorkflowState(ctx, wfID, &now, "", true, &next); err != nil {
		t.Fatal(err)
	}
	wf, _ = st.GetWorkflow(ctx, wfID)
	if wf.LastError != "" || wf.LastRunAt == nil || wf.NextRunAt == nil || wf.NextRunAt.Unix() != next.Unix() {
		t.Fatalf("advanced state = %+v", wf)
	}

	// missed: disable + clear next
	if err := st.SetWorkflowState(ctx, wfID, nil, "missed", false, nil); err != nil {
		t.Fatal(err)
	}
	wf, _ = st.GetWorkflow(ctx, wfID)
	if wf.Enabled || wf.NextRunAt != nil || wf.LastError != "missed" {
		t.Fatalf("missed state = %+v", wf)
	}
}

func TestWorkflowCascadeOnDatabaseDelete(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	dbID := fixtureDB(t, st, ctx)
	now := time.Now()
	if _, err := st.CreateWorkflow(ctx, &Workflow{Name: "w", DatabaseID: dbID,
		TriggerKind: "cron", Cron: "* * * * *", Enabled: true, NextRunAt: &now}); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteDatabase(ctx, dbID); err != nil {
		t.Fatal(err)
	}
	rows, _ := st.ListWorkflows(ctx)
	if len(rows) != 0 {
		t.Fatalf("workflow survived database delete: %+v", rows)
	}
}

func TestCountWorkflowsForDestination(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	dbID := fixtureDB(t, st, ctx)
	destID, err := st.CreateDestination(ctx, &Destination{Name: "s3a", Kind: "s3",
		Endpoint: "http://e", Bucket: "b", AccessKey: "k", SecretEnc: "s"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if _, err := st.CreateWorkflow(ctx, &Workflow{Name: "w", DatabaseID: dbID, DestID: destID,
		TriggerKind: "cron", Cron: "* * * * *", Enabled: true, NextRunAt: &now}); err != nil {
		t.Fatal(err)
	}
	n, err := st.CountWorkflowsForDestination(ctx, destID)
	if err != nil || n != 1 {
		t.Fatalf("count = %d, %v", n, err)
	}
}

func TestDumpWorkflowID(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	dbID := fixtureDB(t, st, ctx)
	wfID, _ := st.CreateWorkflow(ctx, &Workflow{Name: "via-wf", DatabaseID: dbID,
		TriggerKind: "manual", Enabled: true})
	dumpID, err := st.CreateDump(ctx, &Dump{DatabaseID: dbID, Engine: "postgres",
		Label: "l", Storage: "local", WorkflowID: wfID, Status: "pending"})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := st.ListDumps(ctx)
	if err != nil || len(rows) != 1 {
		t.Fatalf("list = %v, %v", rows, err)
	}
	if rows[0].WorkflowID != wfID || rows[0].WorkflowName != "via-wf" {
		t.Fatalf("workflow provenance = %+v", rows[0])
	}
	if _, err := st.GetDump(ctx, dumpID); err != nil {
		t.Fatal(err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/meta/ -run 'TestWorkflow|TestDueWorkflows|TestSetWorkflowState|TestCountWorkflowsForDestination|TestDumpWorkflowID' -v`
Expected: FAIL — `undefined: Workflow` (compile error).

- [ ] **Step 3: Implement**

`internal/meta/meta.go` — three edits.

Edit the DSN to enforce foreign keys (only `dump_workflows` declares FKs; CASCADE depends on it):

```go
db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
```

Append to `schema`:

```sql
CREATE TABLE IF NOT EXISTS dump_workflows (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  name         TEXT NOT NULL,
  database_id  INTEGER NOT NULL REFERENCES databases(id) ON DELETE CASCADE,
  dest_id      INTEGER NOT NULL DEFAULT 0,
  trigger_kind TEXT NOT NULL CHECK (trigger_kind IN ('manual','once','cron')),
  run_at       TEXT,
  cron         TEXT NOT NULL DEFAULT '',
  enabled      INTEGER NOT NULL DEFAULT 1,
  last_run_at  TEXT,
  last_error   TEXT NOT NULL DEFAULT '',
  next_run_at  TEXT,
  created_at   TEXT NOT NULL DEFAULT (datetime('now'))
);
```

After the existing `ensureColumn` call in `Open`:

```go
if err := ensureColumn(db, "dumps", "workflow_id", `ALTER TABLE dumps ADD COLUMN workflow_id INTEGER NOT NULL DEFAULT 0`); err != nil {
	db.Close()
	return nil, err
}
```

Create `internal/meta/workflows.go`:

```go
package meta

import (
	"context"
	"database/sql"
	"time"
)

const timeLayout = "2006-01-02 15:04:05"

type Workflow struct {
	ID          int64
	Name        string
	DatabaseID  int64
	DestID      int64
	TriggerKind string // "manual" | "once" | "cron"
	RunAt       *time.Time
	Cron        string
	Enabled     bool
	LastRunAt   *time.Time
	LastError   string
	NextRunAt   *time.Time
	CreatedAt   time.Time
}

type WorkflowRow struct {
	Workflow
	DatabaseName   string
	DatabaseEngine string
	DestName       string
}

func formatTimePtr(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC().Format(timeLayout)
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func scanWorkflow(row rowScanner) (*Workflow, error) {
	var wf Workflow
	var created string
	var runAt, lastRun, nextRun sql.NullString
	err := row.Scan(&wf.ID, &wf.Name, &wf.DatabaseID, &wf.DestID, &wf.TriggerKind,
		&runAt, &wf.Cron, &wf.Enabled, &lastRun, &wf.LastError, &nextRun, &created)
	if err != nil {
		return nil, err
	}
	wf.CreatedAt = parseTime(created)
	wf.RunAt = parseTimePtr(runAt)
	wf.LastRunAt = parseTimePtr(lastRun)
	wf.NextRunAt = parseTimePtr(nextRun)
	return &wf, nil
}

const workflowCols = `id, name, database_id, dest_id, trigger_kind, run_at, cron, enabled, last_run_at, last_error, next_run_at, created_at`

func (s *Store) CreateWorkflow(ctx context.Context, wf *Workflow) (int64, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO dump_workflows
		(name, database_id, dest_id, trigger_kind, run_at, cron, enabled, last_run_at, last_error, next_run_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		wf.Name, wf.DatabaseID, wf.DestID, wf.TriggerKind,
		formatTimePtr(wf.RunAt), wf.Cron, boolInt(wf.Enabled), formatTimePtr(wf.LastRunAt),
		wf.LastError, formatTimePtr(wf.NextRunAt))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) GetWorkflow(ctx context.Context, id int64) (*Workflow, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+workflowCols+` FROM dump_workflows WHERE id = ?`, id)
	return scanWorkflow(row)
}

func (s *Store) ListWorkflows(ctx context.Context) ([]WorkflowRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT w.id, w.name, w.database_id, w.dest_id, w.trigger_kind,
		w.run_at, w.cron, w.enabled, w.last_run_at, w.last_error, w.next_run_at, w.created_at,
		IFNULL(db.name, ''), IFNULL(db.engine, ''),
		CASE WHEN w.dest_id = 0 THEN 'local' ELSE IFNULL(sd.name, '') END
		FROM dump_workflows w
		LEFT JOIN databases db ON db.id = w.database_id
		LEFT JOIN storage_destinations sd ON sd.id = w.dest_id
		ORDER BY w.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WorkflowRow
	for rows.Next() {
		var r WorkflowRow
		var created string
		var runAt, lastRun, nextRun sql.NullString
		err := rows.Scan(&r.ID, &r.Name, &r.DatabaseID, &r.DestID, &r.TriggerKind,
			&runAt, &r.Cron, &r.Enabled, &lastRun, &r.LastError, &nextRun, &created,
			&r.DatabaseName, &r.DatabaseEngine, &r.DestName)
		if err != nil {
			return nil, err
		}
		r.CreatedAt = parseTime(created)
		r.RunAt = parseTimePtr(runAt)
		r.LastRunAt = parseTimePtr(lastRun)
		r.NextRunAt = parseTimePtr(nextRun)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) UpdateWorkflow(ctx context.Context, wf *Workflow) error {
	_, err := s.db.ExecContext(ctx, `UPDATE dump_workflows SET
		name = ?, database_id = ?, dest_id = ?, trigger_kind = ?, run_at = ?, cron = ?,
		enabled = ?, last_run_at = ?, last_error = ?, next_run_at = ?
		WHERE id = ?`,
		wf.Name, wf.DatabaseID, wf.DestID, wf.TriggerKind,
		formatTimePtr(wf.RunAt), wf.Cron, boolInt(wf.Enabled),
		formatTimePtr(wf.LastRunAt), wf.LastError, formatTimePtr(wf.NextRunAt), wf.ID)
	return err
}

func (s *Store) DeleteWorkflow(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM dump_workflows WHERE id = ?`, id)
	return err
}

func (s *Store) DueWorkflows(ctx context.Context, now time.Time) ([]Workflow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+workflowCols+` FROM dump_workflows
		WHERE enabled = 1 AND next_run_at IS NOT NULL AND next_run_at <= ?
		ORDER BY id`, now.UTC().Format(timeLayout))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Workflow
	for rows.Next() {
		wf, err := scanWorkflow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *wf)
	}
	return out, rows.Err()
}

// SetWorkflowState updates run bookkeeping. lastRunAt nil keeps the current
// value; nextRunAt nil clears the schedule (manual/disabled).
func (s *Store) SetWorkflowState(ctx context.Context, id int64, lastRunAt *time.Time, lastErr string, enabled bool, nextRunAt *time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE dump_workflows SET
		last_run_at = COALESCE(?, last_run_at),
		last_error = ?,
		enabled = ?,
		next_run_at = ?
		WHERE id = ?`,
		formatTimePtr(lastRunAt), lastErr, boolInt(enabled), formatTimePtr(nextRunAt), id)
	return err
}

func (s *Store) CountWorkflowsForDestination(ctx context.Context, destID int64) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM dump_workflows WHERE dest_id = ?`, destID).Scan(&n)
	return n, err
}
```

`internal/meta/dumps.go` — four edits:

1. `Dump` struct: add `WorkflowID int64` after `DestID`.
2. `DumpRow`: add `WorkflowName string`.
3. `dumpCols` and `scanDump`:

```go
const dumpCols = `id, database_id, engine, label, storage, location, source_db, size_bytes, status, created_by, created_at, dest_id, workflow_id`

func scanDump(row rowScanner) (*Dump, error) {
	var d Dump
	var created string
	err := row.Scan(&d.ID, &d.DatabaseID, &d.Engine, &d.Label, &d.Storage, &d.Location,
		&d.SourceDB, &d.SizeBytes, &d.Status, &d.CreatedBy, &created, &d.DestID, &d.WorkflowID)
	if err != nil {
		return nil, err
	}
	d.CreatedAt = parseTime(created)
	return &d, nil
}
```

4. `CreateDump`:

```go
func (s *Store) CreateDump(ctx context.Context, d *Dump) (int64, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO dumps
		(database_id, engine, label, storage, dest_id, location, source_db, size_bytes, status, created_by, workflow_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		d.DatabaseID, d.Engine, d.Label, d.Storage, d.DestID, d.Location, d.SourceDB, d.SizeBytes, d.Status, d.CreatedBy, d.WorkflowID)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}
```

5. `ListDumps` — add `d.workflow_id` to the select + scan and the workflow-name join:

```go
func (s *Store) ListDumps(ctx context.Context) ([]DumpRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT d.id, d.database_id, d.engine, d.label, d.storage, d.location,
		d.source_db, d.size_bytes, d.status, d.created_by, d.created_at, d.dest_id, d.workflow_id,
		IFNULL(db.name, ''),
		CASE WHEN d.dest_id = 0 THEN 'local' ELSE IFNULL(sd.name, '') END,
		IFNULL(wf.name, '')
		FROM dumps d
		LEFT JOIN databases db ON db.id = d.database_id
		LEFT JOIN storage_destinations sd ON sd.id = d.dest_id
		LEFT JOIN dump_workflows wf ON wf.id = d.workflow_id
		ORDER BY d.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DumpRow
	for rows.Next() {
		var r DumpRow
		var created string
		err := rows.Scan(&r.ID, &r.DatabaseID, &r.Engine, &r.Label, &r.Storage, &r.Location,
			&r.SourceDB, &r.SizeBytes, &r.Status, &r.CreatedBy, &created, &r.DestID, &r.WorkflowID,
			&r.DatabaseName, &r.DestName, &r.WorkflowName)
		if err != nil {
			return nil, err
		}
		r.CreatedAt = parseTime(created)
		out = append(out, r)
	}
	return out, rows.Err()
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/meta/ -v && go build ./...`
Expected: PASS (all existing meta tests too — scan changes are internal).

- [ ] **Step 5: Commit**

```bash
git add internal/meta/
git commit -m "feat(meta): dump_workflows table, CRUD, dumps.workflow_id provenance"
```

---

### Task 2: runner — `StartDump` shared run path

**Files:**
- Modify: `internal/runner/runner.go`
- Test: `internal/runner/startdump_test.go`

**Interfaces:**
- Consumes: `meta.Dump.WorkflowID`, `meta.CreateJobGuarded` (returns `meta.ErrJobActive`), `meta.ErrNotFound`.
- Produces (used by Tasks 3–4):
```go
var ErrToolsMissing = errors.New("missing tools")
var ErrStorageUnavailable = errors.New("storage unavailable")

// Creates the dump row, the guarded job, and starts the runner goroutine.
// Errors: meta.ErrJobActive, ErrToolsMissing, ErrStorageUnavailable, wrapped others.
func (r *Runner) StartDump(ctx context.Context, databaseID, destID int64,
	label string, workflowID, createdBy int64) (jobID, dumpID int64, err error)
```

- [ ] **Step 1: Write the failing test**

```go
package runner

import (
	"bytes"
	"context"
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/luthfi9251/ku-dump/internal/cryptx"
	"github.com/luthfi9251/ku-dump/internal/engine"
	"github.com/luthfi9251/ku-dump/internal/meta"
	"github.com/luthfi9251/ku-dump/internal/storage"
)

type okEngine struct{ cx *cryptx.Cryptx }

func (okEngine) Name() string             { return "postgres" }
func (okEngine) RequiredTools() []string  { return nil }
func (okEngine) ToolsMissing() []string   { return nil }
func (okEngine) TestConnection(context.Context, meta.Database) error { return nil }
func (okEngine) Dump(ctx context.Context, db meta.Database, out io.Writer, log io.Writer) error {
	_, err := out.Write([]byte("PGDMP-fake"))
	return err
}
func (okEngine) Restore(context.Context, meta.Database, string, io.Reader, io.Writer) error { return nil }

type missingEngine struct{ okEngine }

func (missingEngine) ToolsMissing() []string { return []string{"pg_dump"} }

func newStartDumpRunner(t *testing.T, eng engine.Engine, newStore func(context.Context, int64) (storage.Store, error)) (*Runner, *meta.Store, int64) {
	t.Helper()
	st, err := meta.Open(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ctx := context.Background()
	dbID, err := st.CreateDatabase(ctx, &meta.Database{Name: "db1", Engine: "postgres",
		Host: "h", Port: 1, DBName: "db1", Username: "u", PasswordEnc: "x"})
	if err != nil {
		t.Fatal(err)
	}
	run, err := New(st, map[string]engine.Engine{"postgres": eng}, newStore, filepath.Join(t.TempDir(), "logs"))
	if err != nil {
		t.Fatal(err)
	}
	return run, st, dbID
}

func waitDumpStatus(t *testing.T, st *meta.Store, dumpID int64, want string) meta.Dump {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		d, err := st.GetDump(context.Background(), dumpID)
		if err == nil && d.Status == want {
			return *d
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("dump %d never became %s", dumpID, want)
}

func TestStartDumpRuns(t *testing.T) {
	run, st, dbID := newStartDumpRunner(t, okEngine{}, func(_ context.Context, destID int64) (storage.Store, error) {
		return storage.NewLocalFS(t.TempDir())
	})
	ctx := context.Background()
	jobID, dumpID, err := run.StartDump(ctx, dbID, 0, "lbl", 42, 3)
	if err != nil {
		t.Fatal(err)
	}
	d := waitDumpStatus(t, st, dumpID, "ready")
	if d.WorkflowID != 42 || d.Label != "lbl" || d.Storage != "local" || d.CreatedBy != 3 {
		t.Fatalf("dump = %+v", d)
	}
	job, err := st.GetJob(ctx, jobID)
	if err != nil || job.Type != "dump" {
		t.Fatalf("job = %+v, %v", job, err)
	}
}

func TestStartDumpDefaultLabel(t *testing.T) {
	run, st, dbID := newStartDumpRunner(t, okEngine{}, func(_ context.Context, destID int64) (storage.Store, error) {
		return storage.NewLocalFS(t.TempDir())
	})
	_, dumpID, err := run.StartDump(context.Background(), dbID, 0, "", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	d := waitDumpStatus(t, st, dumpID, "ready")
	if d.Label == "" {
		t.Fatal("default label not applied")
	}
}

func TestStartDumpJobActive(t *testing.T) {
	// engine never finishes => first job stays running => guard triggers
	blocked := blockedEngine{}
	run, st, dbID := newStartDumpRunner(t, blocked, func(_ context.Context, destID int64) (storage.Store, error) {
		return storage.NewLocalFS(t.TempDir())
	})
	ctx := context.Background()
	_, dumpID, err := run.StartDump(ctx, dbID, 0, "first", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = run.StartDump(ctx, dbID, 0, "second", 0, 0)
	if err != meta.ErrJobActive {
		t.Fatalf("err = %v", err)
	}
	// the rejected run must not leave a pending dump row behind
	if _, err := st.GetDump(ctx, dumpID+1); err != meta.ErrNotFound {
		t.Fatalf("orphan dump row: %v", err)
	}
}

func TestStartDumpToolsMissing(t *testing.T) {
	run, _, dbID := newStartDumpRunner(t, missingEngine{}, nil)
	_, _, err := run.StartDump(context.Background(), dbID, 0, "", 0, 0)
	if !errors.Is(err, ErrToolsMissing) {
		t.Fatalf("err = %v", err)
	}
}

func TestStartDumpStorageUnavailable(t *testing.T) {
	run, _, dbID := newStartDumpRunner(t, okEngine{}, func(_ context.Context, destID int64) (storage.Store, error) {
		return nil, context.DeadlineExceeded
	})
	_, _, err := run.StartDump(context.Background(), dbID, 0, "", 0, 0)
	if !errors.Is(err, ErrStorageUnavailable) {
		t.Fatalf("err = %v", err)
	}
}
```

Test imports: add `"errors"`; drop the unused `"bytes"` import.

Note: `blockedEngine` — reuse the existing blocking fake if `internal/runner` tests already define one; otherwise add:

```go
type blockedEngine struct{ okEngine }

func (blockedEngine) Dump(ctx context.Context, db meta.Database, out io.Writer, log io.Writer) error {
	<-ctx.Done()
	return ctx.Err()
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runner/ -run TestStartDump -v`
Expected: FAIL — `r.StartDump undefined`.

- [ ] **Step 3: Implement** — append to `internal/runner/runner.go` (add `"errors"` to imports):

```go
var ErrToolsMissing = errors.New("missing tools")
var ErrStorageUnavailable = errors.New("storage unavailable")

// StartDump is the single path for creating a dump run: validate engine,
// tools and storage, insert the dump row, create the guarded job, start it.
func (r *Runner) StartDump(ctx context.Context, databaseID, destID int64,
	label string, workflowID, createdBy int64) (int64, int64, error) {
	db, err := r.st.GetDatabase(ctx, databaseID)
	if err != nil {
		return 0, 0, fmt.Errorf("database not found: %w", err)
	}
	eng := r.engines[db.Engine]
	if eng == nil {
		return 0, 0, fmt.Errorf("unknown engine: %s", db.Engine)
	}
	if missing := eng.ToolsMissing(); len(missing) > 0 {
		return 0, 0, fmt.Errorf("%w: %s", ErrToolsMissing, strings.Join(missing, ", "))
	}
	storageKind := "local"
	if destID != 0 {
		dest, err := r.st.GetDestination(ctx, destID)
		if err != nil {
			return 0, 0, fmt.Errorf("storage destination not found")
		}
		storageKind = dest.Kind
	}
	if _, err := r.newStore(ctx, destID); err != nil {
		return 0, 0, fmt.Errorf("%w: %s", ErrStorageUnavailable, err)
	}
	if label == "" {
		label = db.Name + " " + time.Now().Format("2006-01-02 15:04")
	}
	dumpID, err := r.st.CreateDump(ctx, &meta.Dump{
		DatabaseID: db.ID, Engine: db.Engine, Label: label, Storage: storageKind,
		DestID: destID, SourceDB: db.DBName, Status: "pending", CreatedBy: createdBy,
		WorkflowID: workflowID,
	})
	if err != nil {
		return 0, 0, err
	}
	jobID, err := r.st.CreateJobGuarded(ctx, &meta.Job{Type: "dump", DatabaseID: db.ID, DumpID: &dumpID})
	if err != nil {
		if errors.Is(err, meta.ErrJobActive) {
			_ = r.st.DeleteDump(ctx, dumpID)
		}
		return 0, 0, err
	}
	r.Start(jobID)
	return jobID, dumpID, nil
}
```

```go
import "errors"
```

(The test file already uses `errors.Is` against the sentinels; `"bytes"` is
not needed.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runner/ -v && go build ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/runner/
git commit -m "feat(runner): StartDump shared run path with typed errors"
```

---

### Task 3: scheduler package

**Files:**
- Create: `internal/scheduler/scheduler.go`
- Test: `internal/scheduler/scheduler_test.go`
- Modify: `go.mod` / `go.sum` (via `go get`)

**Interfaces:**
- Consumes: `runner.StartDump` (method value), `runner.ErrStorageUnavailable`, `meta.Workflow`, `meta.SetWorkflowState`, `meta.DueWorkflows`, `meta.ListWorkflows`.
- Produces (used by Chunk 2 / Task 5):
```go
func New(st *meta.Store, run *runner.Runner) *Scheduler
func (s *Scheduler) Run(ctx context.Context)                    // blocking loop, 30s tick
func (s *Scheduler) RunOnce(ctx context.Context, now time.Time) // one tick, test entry point
func (s *Scheduler) Recover(ctx context.Context, now time.Time) error // boot-time schedule repair
```

- [ ] **Step 1: Add the dependency**

Run: `go get github.com/robfig/cron/v3`

- [ ] **Step 2: Write the failing test**

```go
package scheduler

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/luthfi9251/ku-dump/internal/cryptx"
	"github.com/luthfi9251/ku-dump/internal/engine"
	"github.com/luthfi9251/ku-dump/internal/meta"
	"github.com/luthfi9251/ku-dump/internal/runner"
	"github.com/luthfi9251/ku-dump/internal/storage"
)

type instantEngine struct{}

func (instantEngine) Name() string             { return "postgres" }
func (instantEngine) RequiredTools() []string  { return nil }
func (instantEngine) ToolsMissing() []string   { return nil }
func (instantEngine) TestConnection(context.Context, meta.Database) error { return nil }
func (instantEngine) Dump(ctx context.Context, db meta.Database, out io.Writer, log io.Writer) error {
	return nil
}
func (instantEngine) Restore(context.Context, meta.Database, string, io.Reader, io.Writer) error {
	return nil
}

type fixture struct {
	sched *Scheduler
	st    *meta.Store
	dbID  int64
	calls *int
	fail  *error
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	st, err := meta.Open(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ctx := context.Background()
	dbID, err := st.CreateDatabase(ctx, &meta.Database{Name: "db1", Engine: "postgres",
		Host: "h", Port: 1, DBName: "db1", Username: "u", PasswordEnc: "x"})
	if err != nil {
		t.Fatal(err)
	}
	run, err := runner.New(st, map[string]engine.Engine{"postgres": instantEngine{}},
		func(_ context.Context, destID int64) (storage.Store, error) {
			return storage.NewLocalFS(t.TempDir())
		}, filepath.Join(t.TempDir(), "logs"))
	if err != nil {
		t.Fatal(err)
	}
	f := fixture{st: st, dbID: dbID, calls: new(int), fail: new(error)}
	// wrapper to count StartDump calls and inject failures
	sched := New(st, countingRunner{run, f.calls, f.fail})
	f.sched = sched
	return f
}

type countingRunner struct {
	*runner.Runner
	calls *int
	fail  *error
}

func (c countingRunner) StartDump(ctx context.Context, databaseID, destID int64,
	label string, workflowID, createdBy int64) (int64, int64, error) {
	*c.calls++
	if *c.fail != nil {
		return 0, 0, *c.fail
	}
	return c.Runner.StartDump(ctx, databaseID, destID, label, workflowID, createdBy)
}

func createWf(t *testing.T, f fixture, mut func(*meta.Workflow)) meta.Workflow {
	t.Helper()
	now := time.Now()
	wf := meta.Workflow{Name: "w", DatabaseID: f.dbID, TriggerKind: "cron",
		Cron: "* * * * *", Enabled: true, NextRunAt: &now}
	mut(&wf)
	id, err := f.st.CreateWorkflow(context.Background(), &wf)
	if err != nil {
		t.Fatal(err)
	}
	out, _ := f.st.GetWorkflow(context.Background(), id)
	return *out
}

func TestRunOnceCronAdvances(t *testing.T) {
	f := newFixture(t)
	now := time.Now().Add(-time.Hour)
	wf := createWf(t, f, func(w *meta.Workflow) { w.NextRunAt = &now })
	f.sched.RunOnce(context.Background(), time.Now())

	got, _ := f.st.GetWorkflow(context.Background(), wf.ID)
	sched, _ := cron.ParseStandard(wf.Cron)
	want := sched.Next(time.Now())
	if *f.calls != 1 || got.LastRunAt == nil || got.LastError != "" || !got.Enabled ||
		got.NextRunAt == nil || got.NextRunAt.Unix() != want.Unix() {
		t.Fatalf("calls=%d wf=%+v wantNext=%v", *f.calls, got, want)
	}
}

func TestRunOnceOnceDisables(t *testing.T) {
	f := newFixture(t)
	past := time.Now().Add(-time.Hour)
	wf := createWf(t, f, func(w *meta.Workflow) {
		w.TriggerKind = "once"
		w.Cron = ""
		w.RunAt = &past
		w.NextRunAt = &past
	})
	f.sched.RunOnce(context.Background(), time.Now())

	got, _ := f.st.GetWorkflow(context.Background(), wf.ID)
	if *f.calls != 1 || got.Enabled || got.NextRunAt != nil || got.LastError != "" {
		t.Fatalf("calls=%d wf=%+v", *f.calls, got)
	}
}

func TestRunOnceStorageFailureRetries(t *testing.T) {
	f := newFixture(t)
	*f.fail = runner.ErrStorageUnavailable
	past := time.Now().Add(-time.Hour)
	wf := createWf(t, f, func(w *meta.Workflow) { w.NextRunAt = &past })
	f.sched.RunOnce(context.Background(), time.Now())

	got, _ := f.st.GetWorkflow(context.Background(), wf.ID)
	if *f.calls != 1 || !got.Enabled || got.LastError == "" ||
		got.NextRunAt == nil || got.NextRunAt.Unix() != past.Unix() {
		t.Fatalf("retry semantics broken: calls=%d wf=%+v", *f.calls, got)
	}
}

func TestRunOnceBusyAdvances(t *testing.T) {
	f := newFixture(t)
	*f.fail = errors.New("a job is already active for this database")
	past := time.Now().Add(-time.Hour)
	wf := createWf(t, f, func(w *meta.Workflow) { w.NextRunAt = &past })
	f.sched.RunOnce(context.Background(), time.Now())

	got, _ := f.st.GetWorkflow(context.Background(), wf.ID)
	sched, _ := cron.ParseStandard(wf.Cron)
	want := sched.Next(time.Now())
	if *f.calls != 1 || !got.Enabled || got.LastError == "" ||
		got.NextRunAt == nil || got.NextRunAt.Unix() != want.Unix() {
		t.Fatalf("busy must advance schedule: calls=%d wf=%+v", *f.calls, got)
	}
}

func TestRecover(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	now := time.Now()

	// cron with stale next_run_at -> recomputed
	stale := now.Add(-2 * time.Hour)
	wf1 := createWf(t, f, func(w *meta.Workflow) { w.Name = "cron"; w.NextRunAt = &stale })
	// once in the past, still enabled -> missed
	wf2 := createWf(t, f, func(w *meta.Workflow) {
		w.Name = "once"
		w.TriggerKind = "once"
		w.Cron = ""
		w.RunAt = &stale
		w.NextRunAt = &stale
	})
	// once in the future -> untouched
	future := now.Add(time.Hour)
	wf3 := createWf(t, f, func(w *meta.Workflow) {
		w.Name = "once-future"
		w.TriggerKind = "once"
		w.Cron = ""
		w.RunAt = &future
		w.NextRunAt = &future
	})

	if err := f.sched.Recover(ctx, now); err != nil {
		t.Fatal(err)
	}
	g1, _ := f.st.GetWorkflow(ctx, wf1.ID)
	sched, _ := cron.ParseStandard(g1.Cron)
	want := sched.Next(now)
	if g1.NextRunAt == nil || g1.NextRunAt.Unix() != want.Unix() {
		t.Fatalf("cron not recomputed: %+v", g1)
	}
	g2, _ := f.st.GetWorkflow(ctx, wf2.ID)
	if g2.Enabled || g2.NextRunAt != nil || g2.LastError != "missed" {
		t.Fatalf("once not marked missed: %+v", g2)
	}
	g3, _ := f.st.GetWorkflow(ctx, wf3.ID)
	if !g3.Enabled || g3.NextRunAt == nil || g3.NextRunAt.Unix() != future.Unix() {
		t.Fatalf("future once disturbed: %+v", g3)
	}
	if *f.calls != 0 {
		t.Fatalf("recover must not run anything, calls=%d", *f.calls)
	}
}

func TestRunOnceRecoversFromPanic(t *testing.T) {
	f := newFixture(t)
	*f.fail = nil
	past := time.Now().Add(-time.Hour)
	createWf(t, f, func(w *meta.Workflow) { w.NextRunAt = &past })
	// panic inside StartDump must not escape RunOnce
	sched := New(f.st, panickyRunner{})
	sched.RunOnce(context.Background(), time.Now())
}

type panickyRunner struct{}

func (panickyRunner) StartDump(context.Context, int64, int64, string, int64, int64) (int64, int64, error) {
	panic("boom")
}
```

Both `countingRunner` (delegates to a real `*runner.Runner` and counts calls)
and `panickyRunner` satisfy the `Starter` interface directly — no embedding
required for `panickyRunner`.

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/scheduler/ -v`
Expected: FAIL — package `scheduler` does not exist.

- [ ] **Step 4: Implement** — create `internal/scheduler/scheduler.go`:

```go
package scheduler

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/luthfi9251/ku-dump/internal/meta"
	"github.com/luthfi9251/ku-dump/internal/runner"
)

const Interval = 30 * time.Second

// Starter is the slice of *runner.Runner the scheduler needs.
type Starter interface {
	StartDump(ctx context.Context, databaseID, destID int64,
		label string, workflowID, createdBy int64) (jobID, dumpID int64, err error)
}

type Scheduler struct {
	st    *meta.Store
	start Starter
}

func New(st *meta.Store, start Starter) *Scheduler {
	return &Scheduler{st: st, start: start}
}

// Run blocks, ticking every Interval. Cancel ctx to stop.
func (s *Scheduler) Run(ctx context.Context) {
	t := time.NewTicker(Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.RunOnce(ctx, time.Now())
		}
	}
}

// RunOnce executes every due workflow. Time is injected so tests are
// deterministic. Semantics per spec:
//   - storage unavailable -> keep next_run_at (retried next tick)
//   - other failures      -> record last_error, still advance the schedule
//   - once                -> disables itself after a successful run
//   - cron                -> next run computed from now (missed runs never replay)
func (s *Scheduler) RunOnce(ctx context.Context, now time.Time) {
	defer func() {
		if p := recover(); p != nil {
			log.Printf("scheduler panic: %v", p)
		}
	}()
	due, err := s.st.DueWorkflows(ctx, now)
	if err != nil {
		log.Printf("scheduler: due workflows: %v", err)
		return
	}
	for i := range due {
		s.runWorkflow(ctx, due[i], now)
	}
}

func (s *Scheduler) runWorkflow(ctx context.Context, wf meta.Workflow, now time.Time) {
	lastErr := ""
	if _, _, err := s.start.StartDump(ctx, wf.DatabaseID, wf.DestID, wf.Name, wf.ID, 0); err != nil {
		lastErr = err.Error()
		if errors.Is(err, runner.ErrStorageUnavailable) {
			if err := s.st.SetWorkflowState(ctx, wf.ID, nil, lastErr, true, wf.NextRunAt); err != nil {
				log.Printf("scheduler: workflow %d state: %v", wf.ID, err)
			}
			return
		}
	}
	var next *time.Time
	enabled := true
	switch wf.TriggerKind {
	case "once":
		if lastErr == "" {
			enabled = false
		} else {
			next = wf.RunAt // retry next tick until it actually runs
		}
	case "cron":
		sched, err := cron.ParseStandard(wf.Cron)
		if err != nil {
			lastErr = "invalid cron: " + wf.Cron
			enabled = false
		} else {
			t := sched.Next(now)
			next = &t
		}
	}
	if err := s.st.SetWorkflowState(ctx, wf.ID, &now, lastErr, enabled, next); err != nil {
		log.Printf("scheduler: workflow %d state: %v", wf.ID, err)
	}
}

// Recover repairs schedules after a restart. Missed runs are never replayed.
func (s *Scheduler) Recover(ctx context.Context, now time.Time) error {
	rows, err := s.st.ListWorkflows(ctx)
	if err != nil {
		return err
	}
	for i := range rows {
		wf := rows[i].Workflow
		if !wf.Enabled {
			continue
		}
		switch wf.TriggerKind {
		case "cron":
			if wf.NextRunAt != nil && wf.NextRunAt.After(now) {
				continue
			}
			sched, err := cron.ParseStandard(wf.Cron)
			if err != nil {
				if err := s.st.SetWorkflowState(ctx, wf.ID, nil, "invalid cron: "+wf.Cron, false, nil); err != nil {
					return err
				}
				continue
			}
			t := sched.Next(now)
			if err := s.st.SetWorkflowState(ctx, wf.ID, nil, wf.LastError, true, &t); err != nil {
				return err
			}
		case "once":
			if wf.RunAt != nil && !wf.RunAt.After(now) {
				if err := s.st.SetWorkflowState(ctx, wf.ID, nil, "missed", false, nil); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
```

`countingRunner` must forward `StartDump` to the real runner (shown in the
test above); all other `Runner` methods are irrelevant to the scheduler.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/scheduler/ -v && go build ./... && go test ./...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/scheduler/ go.mod go.sum
git commit -m "feat(scheduler): in-process 30s tick running due dump workflows"
```
