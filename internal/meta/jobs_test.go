package meta

import (
	"context"
	"errors"
	"testing"
)

func TestJobLifecycle(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	db := sampleDB()
	dbID, _ := st.CreateDatabase(ctx, &db)
	dumpID, _ := st.CreateDump(ctx, &Dump{DatabaseID: dbID, Engine: "postgres", Label: "l", Storage: "local", Status: "pending", CreatedBy: 1})
	storage := "local"
	j := Job{Type: "dump", DatabaseID: dbID, DumpID: &dumpID, Storage: &storage}
	id, err := st.CreateJob(ctx, &j)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := st.GetJob(ctx, id)
	if got.Status != "pending" || got.DumpID == nil || *got.DumpID != dumpID || got.Storage == nil || *got.Storage != "local" {
		t.Fatalf("got = %+v", got)
	}
	if got.CreatedAt.IsZero() {
		t.Fatal("CreatedAt zero")
	}
	if err := st.SetJobRunning(ctx, id, "/tmp/logs/job-1.log"); err != nil {
		t.Fatal(err)
	}
	got, _ = st.GetJob(ctx, id)
	if got.Status != "running" || got.LogPath != "/tmp/logs/job-1.log" || got.StartedAt == nil || got.FinishedAt != nil {
		t.Fatalf("running = %+v", got)
	}
	ok, err := st.HasActiveJob(ctx, dbID)
	if err != nil || !ok {
		t.Fatalf("HasActiveJob = %v, %v", ok, err)
	}
	if err := st.SetJobFinished(ctx, id, "success", ""); err != nil {
		t.Fatal(err)
	}
	got, _ = st.GetJob(ctx, id)
	if got.Status != "success" || got.FinishedAt == nil {
		t.Fatalf("finished = %+v", got)
	}
	ok, _ = st.HasActiveJob(ctx, dbID)
	if ok {
		t.Fatal("no active job expected")
	}
}

func TestFailActiveJobsAndList(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	db := sampleDB()
	dbID, _ := st.CreateDatabase(ctx, &db)
	dumpID, _ := st.CreateDump(ctx, &Dump{DatabaseID: dbID, Engine: "postgres", Label: "l", Storage: "local", Status: "pending", CreatedBy: 1})
	id1, _ := st.CreateJob(ctx, &Job{Type: "dump", DatabaseID: dbID, DumpID: &dumpID})
	st.SetJobRunning(ctx, id1, "")
	id2, _ := st.CreateJob(ctx, &Job{Type: "restore", DatabaseID: dbID, DumpID: &dumpID})
	n, err := st.FailActiveJobs(ctx, "interrupted by restart")
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("failed %d jobs, want 2", n)
	}
	rows, err := st.ListJobs(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d", len(rows))
	}
	if rows[0].ID != id2 || rows[1].ID != id1 {
		t.Fatalf("order wrong: %+v", rows)
	}
	if rows[1].DatabaseName != "prod-pg" || rows[1].DumpLabel != "l" {
		t.Fatalf("join fields = %+v", rows[1])
	}
	for _, r := range rows {
		if r.Status != "failed" || r.Err != "interrupted by restart" {
			t.Fatalf("row = %+v", r)
		}
	}
}

func TestCreateJobGuarded(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	db := sampleDB()
	dbID, _ := st.CreateDatabase(ctx, &db)
	dumpID, _ := st.CreateDump(ctx, &Dump{DatabaseID: dbID, Engine: "postgres", Label: "l", Storage: "local", Status: "pending", CreatedBy: 1})
	storage := "local"
	id, err := st.CreateJobGuarded(ctx, &Job{Type: "dump", DatabaseID: dbID, DumpID: &dumpID, Storage: &storage})
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.CreateJobGuarded(ctx, &Job{Type: "dump", DatabaseID: dbID, DumpID: &dumpID, Storage: &storage})
	if !errors.Is(err, ErrJobActive) {
		t.Fatalf("err = %v, want ErrJobActive", err)
	}
	if _, err := st.CreateJobGuarded(ctx, &Job{Type: "dump", DatabaseID: dbID + 1}); err != nil {
		t.Fatalf("other database should be allowed: %v", err)
	}
	if err := st.SetJobFinished(ctx, id, "success", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateJobGuarded(ctx, &Job{Type: "restore", DatabaseID: dbID, DumpID: &dumpID}); err != nil {
		t.Fatalf("after finish should be allowed: %v", err)
	}
}

func TestSettings(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	if _, ok, err := st.GetSetting(ctx, "k"); err != nil || ok {
		t.Fatalf("get missing = %v, %v", ok, err)
	}
	if err := st.SetSetting(ctx, "k", "v1"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSetting(ctx, "k", "v2"); err != nil {
		t.Fatal(err)
	}
	v, ok, err := st.GetSetting(ctx, "k")
	if err != nil || !ok || v != "v2" {
		t.Fatalf("v = %q, %v, %v", v, ok, err)
	}
}
