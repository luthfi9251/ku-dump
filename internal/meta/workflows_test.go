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
		StoragePath: "backups/prod", FilenamePattern: "{db}-{date}",
	})
	if err != nil {
		t.Fatal(err)
	}
	wf, err := st.GetWorkflow(ctx, wfID)
	if err != nil || wf.Name != "nightly" || wf.TriggerKind != "once" || !wf.Enabled ||
		wf.NextRunAt == nil || wf.NextRunAt.Unix() != runAt.Unix() {
		t.Fatalf("get = %+v, %v", wf, err)
	}
	if wf.StoragePath != "backups/prod" || wf.FilenamePattern != "{db}-{date}" {
		t.Fatalf("storage config = %q / %q", wf.StoragePath, wf.FilenamePattern)
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
	wf.StoragePath = ""
	wf.FilenamePattern = ""
	if err := st.UpdateWorkflow(ctx, wf); err != nil {
		t.Fatal(err)
	}
	got, _ := st.GetWorkflow(ctx, wfID)
	if got.Cron != "0 2 * * *" || got.RunAt != nil || got.NextRunAt == nil || got.NextRunAt.Unix() != next.Unix() {
		t.Fatalf("after update = %+v", got)
	}
	if got.StoragePath != "" || got.FilenamePattern != "" {
		t.Fatalf("storage config not cleared: %q / %q", got.StoragePath, got.FilenamePattern)
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
