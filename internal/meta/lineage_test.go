package meta

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

// lineageFixture drives dumps and restores the way the runner does: a job
// per event, finished before the next one starts.
type lineageFixture struct {
	t   *testing.T
	st  *Store
	ctx context.Context
}

func (f lineageFixture) db(name string) int64 {
	d := sampleDB()
	d.Name = name
	id, err := f.st.CreateDatabase(f.ctx, &d)
	if err != nil {
		f.t.Fatal(err)
	}
	return id
}

func (f lineageFixture) dump(dbID int64, status string) int64 {
	id, err := f.st.CreateDump(f.ctx, &Dump{DatabaseID: dbID, Engine: "postgres", Label: "d", Storage: "local", Status: "pending", CreatedBy: 1})
	if err != nil {
		f.t.Fatal(err)
	}
	jobID, err := f.st.CreateJobGuarded(f.ctx, &Job{Type: "dump", DatabaseID: dbID, DumpID: &id})
	if err != nil {
		f.t.Fatal(err)
	}
	if err := f.st.SetDumpParent(f.ctx, id, dbID, jobID); err != nil {
		f.t.Fatal(err)
	}
	jobStatus := "success"
	if status == "failed" {
		jobStatus = "failed"
	}
	_ = f.st.UpdateDumpResult(f.ctx, id, status, "k", 1)
	_ = f.st.SetJobFinished(f.ctx, jobID, jobStatus, "")
	return id
}

func (f lineageFixture) restore(dumpID, dbID int64, status string) int64 {
	jobID, err := f.st.CreateJobGuarded(f.ctx, &Job{Type: "restore", DatabaseID: dbID, DumpID: &dumpID})
	if err != nil {
		f.t.Fatal(err)
	}
	_ = f.st.SetJobFinished(f.ctx, jobID, status, "")
	return jobID
}

func (f lineageFixture) parent(dumpID int64) (int64, int64) {
	d, err := f.st.GetDump(f.ctx, dumpID)
	if err != nil {
		f.t.Fatal(err)
	}
	return d.ParentDumpID, d.ParentJobID
}

func TestLineageParents(t *testing.T) {
	f := lineageFixture{t, testStore(t), context.Background()}
	prod, dev := f.db("prod"), f.db("dev")

	p1 := f.dump(prod, "ready")
	if pd, pj := f.parent(p1); pd != 0 || pj != 0 {
		t.Fatalf("first dump parent = %d/%d, want root", pd, pj)
	}
	f.dump(prod, "failed")
	p2 := f.dump(prod, "ready")
	if pd, pj := f.parent(p2); pd != p1 || pj != 0 {
		t.Fatalf("p2 parent = %d/%d, want p1 (failed dump skipped)", pd, pj)
	}

	r1 := f.restore(p2, dev, "success")
	a1 := f.dump(dev, "ready")
	if pd, pj := f.parent(a1); pd != p2 || pj != r1 {
		t.Fatalf("a1 parent = %d/%d, want p2 via restore %d", pd, pj, r1)
	}
	a2 := f.dump(dev, "ready")
	if pd, pj := f.parent(a2); pd != a1 || pj != 0 {
		t.Fatalf("a2 parent = %d/%d, want a1", pd, pj)
	}

	// reset dev to a1; a failed restore afterwards changes nothing
	r2 := f.restore(a1, dev, "success")
	f.restore(p1, dev, "failed")
	a3 := f.dump(dev, "ready")
	if pd, pj := f.parent(a3); pd != a1 || pj != r2 {
		t.Fatalf("a3 parent = %d/%d, want a1 via restore %d", pd, pj, r2)
	}
}

func TestLineageDeleteReparents(t *testing.T) {
	f := lineageFixture{t, testStore(t), context.Background()}
	prod := f.db("prod")
	p1 := f.dump(prod, "ready")
	p2 := f.dump(prod, "ready")
	p3 := f.dump(prod, "ready")
	if err := f.st.DeleteDump(f.ctx, p2); err != nil {
		t.Fatal(err)
	}
	if pd, pj := f.parent(p3); pd != p1 || pj != 0 {
		t.Fatalf("p3 parent after delete = %d/%d, want p1", pd, pj)
	}
}

func TestLineageBackfill(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	f := lineageFixture{t, st, context.Background()}
	prod, dev := f.db("prod"), f.db("dev")
	p1 := f.dump(prod, "ready")
	r1 := f.restore(p1, dev, "success")
	a1 := f.dump(dev, "ready")
	st.Close()

	// simulate a database from before lineage tracking
	raw, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{`ALTER TABLE dumps DROP COLUMN parent_dump_id`, `ALTER TABLE dumps DROP COLUMN parent_job_id`} {
		if _, err := raw.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	raw.Close()

	st, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	f.st = st
	if pd, pj := f.parent(a1); pd != p1 || pj != r1 {
		t.Fatalf("backfilled a1 parent = %d/%d, want p1 via restore %d", pd, pj, r1)
	}
	if pd, pj := f.parent(p1); pd != 0 || pj != 0 {
		t.Fatalf("backfilled p1 parent = %d/%d, want root", pd, pj)
	}
}
