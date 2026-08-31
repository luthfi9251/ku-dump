package meta

import (
	"context"
	"errors"
	"testing"
)

func sampleDB() Database {
	return Database{Name: "prod-pg", Engine: "postgres", Host: "10.0.0.1", Port: 5432,
		DBName: "appdb", Username: "app", PasswordEnc: "ENC", Options: `{"sslmode":"require"}`}
}

func TestDatabaseCRUD(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	in := sampleDB()
	id, err := st.CreateDatabase(ctx, &in)
	if err != nil {
		t.Fatal(err)
	}
	got, err := st.GetDatabase(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "prod-pg" || got.Engine != "postgres" || got.Port != 5432 ||
		got.DBName != "appdb" || got.Username != "app" || got.PasswordEnc != "ENC" ||
		got.Options != `{"sslmode":"require"}` || got.LastTestAt != nil || got.LastTestOK != nil {
		t.Fatalf("got = %+v", got)
	}
	byName, err := st.GetDatabaseByName(ctx, "prod-pg")
	if err != nil || byName.ID != id {
		t.Fatalf("byName = %+v, %v", byName, err)
	}
	if _, err := st.GetDatabaseByName(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestDatabaseListOrderedByName(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	for _, n := range []string{"zeta", "alpha", "mid"} {
		d := sampleDB()
		d.Name = n
		if _, err := st.CreateDatabase(ctx, &d); err != nil {
			t.Fatal(err)
		}
	}
	list, err := st.ListDatabases(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 || list[0].Name != "alpha" || list[2].Name != "zeta" {
		t.Fatalf("list = %+v", list)
	}
}

func TestDatabaseUpdateAndDelete(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	in := sampleDB()
	id, _ := st.CreateDatabase(ctx, &in)
	got, _ := st.GetDatabase(ctx, id)
	got.Host = "10.9.9.9"
	got.Port = 6543
	got.PasswordEnc = "ENC2"
	got.Options = ""
	if err := st.UpdateDatabase(ctx, got); err != nil {
		t.Fatal(err)
	}
	after, _ := st.GetDatabase(ctx, id)
	if after.Host != "10.9.9.9" || after.Port != 6543 || after.PasswordEnc != "ENC2" {
		t.Fatalf("after = %+v", after)
	}
	if after.Options != "{}" {
		t.Fatalf("Options = %q, want default {}", after.Options)
	}
	if err := st.DeleteDatabase(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetDatabase(ctx, id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestSetDatabaseTestResult(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	in := sampleDB()
	id, _ := st.CreateDatabase(ctx, &in)
	if err := st.SetDatabaseTestResult(ctx, id, true); err != nil {
		t.Fatal(err)
	}
	got, _ := st.GetDatabase(ctx, id)
	if got.LastTestAt == nil || got.LastTestOK == nil || !*got.LastTestOK {
		t.Fatalf("test result = %+v", got)
	}
	if err := st.SetDatabaseTestResult(ctx, id, false); err != nil {
		t.Fatal(err)
	}
	got, _ = st.GetDatabase(ctx, id)
	if *got.LastTestOK {
		t.Fatal("LastTestOK should be false")
	}
}
