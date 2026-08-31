package meta

import (
	"context"
	"errors"
	"testing"
)

func TestDumpLifecycle(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	db := sampleDB()
	dbID, _ := st.CreateDatabase(ctx, &db)
	d := Dump{DatabaseID: dbID, Engine: "postgres", Label: "nightly", Storage: "local",
		SourceDB: "appdb", Status: "pending", CreatedBy: 7}
	id, err := st.CreateDump(ctx, &d)
	if err != nil {
		t.Fatal(err)
	}
	got, err := st.GetDump(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Label != "nightly" || got.Status != "pending" || got.SourceDB != "appdb" || got.SizeBytes != 0 {
		t.Fatalf("got = %+v", got)
	}
	if err := st.UpdateDumpResult(ctx, id, "ready", "postgres/prod-pg/20260831-010101.dump", 4096); err != nil {
		t.Fatal(err)
	}
	got, _ = st.GetDump(ctx, id)
	if got.Status != "ready" || got.Location == "" || got.SizeBytes != 4096 {
		t.Fatalf("after update = %+v", got)
	}
}

func TestListDumpsJoinsDatabaseName(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	db := sampleDB()
	dbID, _ := st.CreateDatabase(ctx, &db)
	st.CreateDump(ctx, &Dump{DatabaseID: dbID, Engine: "postgres", Label: "a", Storage: "local", Status: "ready", CreatedBy: 1})
	st.CreateDump(ctx, &Dump{DatabaseID: 0, Engine: "mongodb", Label: "uploaded", Storage: "local", Status: "uploaded", CreatedBy: 1})
	rows, err := st.ListDumps(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("len = %d", len(rows))
	}
	if rows[0].Label != "uploaded" || rows[0].DatabaseName != "" {
		t.Fatalf("rows[0] = %+v", rows[0])
	}
	if rows[1].DatabaseName != "prod-pg" {
		t.Fatalf("rows[1] = %+v", rows[1])
	}
	st.DeleteDatabase(ctx, dbID)
	rows, _ = st.ListDumps(ctx)
	if rows[1].DatabaseName != "" {
		t.Fatalf("deleted db name should be empty, got %+v", rows[1])
	}
	if err := st.DeleteDump(ctx, rows[1].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetDump(ctx, rows[1].ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}
