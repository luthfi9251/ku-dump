package meta

import (
	"context"
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
