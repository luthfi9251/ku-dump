package meta

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestOpenMigratesIdempotently(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.db")
	st1, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	st1.Close()
	st2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	st2.Close()
}

func TestDestinationSchemaRebuild(t *testing.T) {
	dir := t.TempDir()
	dbPath := dir + "/legacy.db"
	db, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		t.Fatal(err)
	}
	old := `CREATE TABLE storage_destinations (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL UNIQUE,
		kind TEXT NOT NULL CHECK (kind IN ('s3')),
		endpoint TEXT NOT NULL,
		region TEXT NOT NULL DEFAULT '',
		bucket TEXT NOT NULL,
		prefix TEXT NOT NULL DEFAULT '',
		access_key TEXT NOT NULL,
		secret_enc TEXT NOT NULL,
		created_at TEXT NOT NULL DEFAULT (datetime('now')))`
	if _, err := db.Exec(old); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO storage_destinations
		(name, kind, endpoint, region, bucket, prefix, access_key, secret_enc)
		VALUES ('legacy', 's3', 'http://old', '', 'oldbucket', '', 'ak', 'sec')`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	st, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open with legacy table: %v", err)
	}
	defer st.Close()

	ctx := context.Background()
	d, err := st.GetDestination(ctx, 1)
	if err != nil || d.Name != "legacy" || d.Kind != "s3" || d.Bucket != "oldbucket" || d.SecretEnc != "sec" {
		t.Fatalf("legacy row = %+v, %v", d, err)
	}
	if _, err := st.CreateDestination(ctx, &Destination{Name: "loc", Kind: "local", RootPath: "/tmp/x"}); err != nil {
		t.Fatalf("new kind rejected after rebuild: %v", err)
	}
}

func TestUserCRUD(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	if n, err := st.CountUsers(ctx); err != nil || n != 0 {
		t.Fatalf("CountUsers = %d, %v", n, err)
	}
	id, err := st.CreateUser(ctx, "admin", "hashed")
	if err != nil {
		t.Fatal(err)
	}
	if id == 0 {
		t.Fatal("id not assigned")
	}
	if n, _ := st.CountUsers(ctx); n != 1 {
		t.Fatalf("CountUsers = %d", n)
	}
	u, err := st.GetUserByUsername(ctx, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if u.ID != id || u.PasswordHash != "hashed" {
		t.Fatalf("user = %+v", u)
	}
	if u.CreatedAt.IsZero() || time.Since(u.CreatedAt) > time.Minute {
		t.Fatalf("CreatedAt = %v", u.CreatedAt)
	}
	if _, err := st.GetUserByUsername(ctx, "ghost"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	byID, err := st.GetUserByID(ctx, id)
	if err != nil || byID.Username != "admin" {
		t.Fatalf("GetUserByID = %+v, %v", byID, err)
	}
}
