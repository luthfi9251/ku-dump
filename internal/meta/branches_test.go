package meta

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

func (f lineageFixture) branch(dumpID int64) int64 {
	d, err := f.st.GetDump(f.ctx, dumpID)
	if err != nil {
		f.t.Fatal(err)
	}
	return d.BranchID
}

func (f lineageFixture) restoreBranch(jobID int64) int64 {
	var b int64
	if err := f.st.db.QueryRow(`SELECT branch_id FROM jobs WHERE id = ?`, jobID).Scan(&b); err != nil {
		f.t.Fatal(err)
	}
	return b
}

func TestBranchRules(t *testing.T) {
	f := lineageFixture{t, testStore(t), context.Background()}
	prod, dev := f.db("prod"), f.db("dev")

	a1 := f.dump(dev, "ready")
	a2 := f.dump(dev, "ready")
	main := f.branch(a1)
	if main == 0 || f.branch(a2) != main {
		t.Fatalf("first dumps should share one branch: %d %d", main, f.branch(a2))
	}

	// rule 2: an older point of the same database starts a new branch
	r1 := f.restore(a1, dev, "success")
	fork := f.restoreBranch(r1)
	if fork == 0 || fork == main {
		t.Fatalf("restore of older point: branch %d, want new (main %d)", fork, main)
	}
	b1 := f.dump(dev, "ready")
	if f.branch(b1) != fork {
		t.Fatalf("dump after fork on branch %d, want %d", f.branch(b1), fork)
	}

	// preview, then rule 1: the tip of main continues main
	if b, err := f.st.PreviewRestoreBranch(f.ctx, a2, dev); err != nil || b == nil || b.ID != main {
		t.Fatalf("preview tip of main = %+v, %v", b, err)
	}
	if b, err := f.st.PreviewRestoreBranch(f.ctx, a1, dev); err != nil || b != nil {
		t.Fatalf("preview older point = %+v, %v; want new branch", b, err)
	}
	r2 := f.restore(a2, dev, "success")
	if f.restoreBranch(r2) != main {
		t.Fatalf("restore of main tip on branch %d, want main %d", f.restoreBranch(r2), main)
	}
	a3 := f.dump(dev, "ready")
	if f.branch(a3) != main {
		t.Fatalf("dump after checkout on branch %d, want main", f.branch(a3))
	}

	// a failed restore changes nothing
	f.restore(b1, dev, "failed")
	if a4 := f.dump(dev, "ready"); f.branch(a4) != main {
		t.Fatalf("failed restore moved the branch")
	}

	// rule 3: a dump of another database always starts a new branch
	p1 := f.dump(prod, "ready")
	r3 := f.restore(p1, dev, "success")
	if b := f.restoreBranch(r3); b == 0 || b == main || b == fork {
		t.Fatalf("cross-database restore on branch %d, want new", b)
	}
	r4 := f.restore(p1, dev, "success")
	if f.restoreBranch(r4) == f.restoreBranch(r3) {
		t.Fatal("restoring the same foreign dump twice should give two branches")
	}
}

func TestBranchRename(t *testing.T) {
	f := lineageFixture{t, testStore(t), context.Background()}
	dev := f.db("dev")
	a1 := f.dump(dev, "ready")
	r := f.restore(a1, dev, "success") // a1 is the tip: continues
	f.dump(dev, "ready")
	f.restore(a1, dev, "success") // a1 is now an older point: new branch
	_ = r
	bs, err := f.st.ListBranches(f.ctx)
	if err != nil || len(bs) != 2 {
		t.Fatalf("branches = %+v, %v", bs, err)
	}
	if bs[0].Name == "" || bs[0].Name == bs[1].Name {
		t.Fatalf("random names = %q %q", bs[0].Name, bs[1].Name)
	}
	if err := f.st.RenameBranch(f.ctx, bs[0].ID, "  feature-payment "); err != nil {
		t.Fatal(err)
	}
	if b, _ := f.st.GetBranch(f.ctx, bs[0].ID); b.Name != "feature-payment" {
		t.Fatalf("renamed = %q", b.Name)
	}
	if err := f.st.RenameBranch(f.ctx, bs[1].ID, "feature-payment"); !errors.Is(err, ErrBranchNameTaken) {
		t.Fatalf("duplicate rename err = %v", err)
	}
}

func TestBranchBackfill(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	f := lineageFixture{t, st, context.Background()}
	dev := f.db("dev")
	a1 := f.dump(dev, "ready")
	a2 := f.dump(dev, "ready")
	r1 := f.restore(a1, dev, "success")
	b1 := f.dump(dev, "ready")
	r2 := f.restore(a2, dev, "success")
	st.Close()

	raw, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{`ALTER TABLE dumps DROP COLUMN branch_id`, `ALTER TABLE jobs DROP COLUMN branch_id`, `DELETE FROM branches`} {
		if _, err := raw.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	raw.Close()

	if st, err = Open(path); err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	f.st = st
	main := f.branch(a1)
	if main == 0 || f.branch(a2) != main {
		t.Fatalf("backfilled main = %d/%d", main, f.branch(a2))
	}
	if fork := f.restoreBranch(r1); fork == 0 || fork == main || f.branch(b1) != fork {
		t.Fatalf("backfilled fork = %d, b1 on %d", fork, f.branch(b1))
	}
	if f.restoreBranch(r2) != main {
		t.Fatalf("backfilled checkout of main tip = %d, want %d", f.restoreBranch(r2), main)
	}
}
