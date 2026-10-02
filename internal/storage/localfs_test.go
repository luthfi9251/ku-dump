package storage

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newLocal(t *testing.T) *LocalFS {
	t.Helper()
	l, err := NewLocalFS(filepath.Join(t.TempDir(), "dumps"))
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func putFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "in.dump")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLocalFSPutOpenRoundTrip(t *testing.T) {
	l := newLocal(t)
	ctx := context.Background()
	src := putFile(t, "DUMP-CONTENT")
	if err := l.Put(ctx, "postgres/prod/20260831-010101.dump", src); err != nil {
		t.Fatal(err)
	}
	rc, err := l.Open(ctx, "postgres/prod/20260831-010101.dump")
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	b, _ := io.ReadAll(rc)
	if string(b) != "DUMP-CONTENT" {
		t.Fatalf("content = %q", b)
	}
	if l.Kind() != "local" {
		t.Fatalf("kind = %q", l.Kind())
	}
}

func TestLocalFSPutRefusesOverwrite(t *testing.T) {
	l := newLocal(t)
	ctx := context.Background()
	if err := l.Put(ctx, "a/b.dump", putFile(t, "first")); err != nil {
		t.Fatal(err)
	}
	if err := l.Put(ctx, "a/b.dump", putFile(t, "second")); err == nil {
		t.Fatal("overwrite accepted")
	}
	rc, err := l.Open(ctx, "a/b.dump")
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	b, _ := io.ReadAll(rc)
	if string(b) != "first" {
		t.Fatalf("original content replaced: %q", b)
	}
}

func TestLocalFSDelete(t *testing.T) {
	l := newLocal(t)
	ctx := context.Background()
	src := putFile(t, "x")
	l.Put(ctx, "a/b.dump", src)
	if err := l.Delete(ctx, "a/b.dump"); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Open(ctx, "a/b.dump"); err == nil {
		t.Fatal("deleted key still openable")
	}
	if err := l.Delete(ctx, "a/b.dump"); err != nil {
		t.Fatalf("delete missing key should be tolerated: %v", err)
	}
}

func TestLocalFSTestProbe(t *testing.T) {
	root := filepath.Join(t.TempDir(), "nested", "root")
	l, err := NewLocalFS(root) // MkdirAll nested
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Test(); err != nil {
		t.Fatalf("probe failed: %v", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "._kudump-probe") {
			t.Fatalf("probe file left behind: %s", e.Name())
		}
	}
	if len(entries) != 0 {
		t.Fatalf("unexpected files: %v", entries)
	}
}

func TestLocalFSTraversalRejected(t *testing.T) {
	l := newLocal(t)
	src := putFile(t, "x")
	if err := l.Put(context.Background(), "../escape.dump", src); err == nil {
		t.Fatal("traversal put accepted")
	}
}

func TestSlug(t *testing.T) {
	cases := map[string]string{
		"Prod DB_2!":  "prod-db-2",
		"  ":          "db",
		"UPPER--Case": "upper-case",
	}
	for in, want := range cases {
		if got := Slug(in); got != want {
			t.Fatalf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestKeyFor(t *testing.T) {
	k := KeyFor("postgres", "prod", time.Date(2026, 8, 31, 1, 2, 3, 0, time.UTC))
	if k != "postgres/prod/20260831-010203.dump" {
		t.Fatalf("key = %q", k)
	}
	if strings.Contains(Slug("a/../b"), "/") {
		t.Fatal("slug contains slash")
	}
}

func TestBuildKey(t *testing.T) {
	ts := time.Date(2026, 10, 2, 15, 4, 5, 0, time.UTC)
	cases := []struct {
		name, path, pattern, engine, db, wf, want string
	}{
		{"empty falls back to default", "", "", "postgres", "Prod DB", "nightly", "postgres/prod-db/20261002-150405.dump"},
		{"custom path and pattern, auto timestamp", "backups/prod", "{db}-{date}", "postgres", "prod", "nightly",
			"backups/prod/prod-2026-10-02-20261002-150405.dump"},
		{"pattern with timestamp is left alone", "", "{engine}/{workflow}/{timestamp}", "postgres", "prod", "nightly",
			"postgres/nightly/20261002-150405.dump"},
		{"dirty path slugged per segment", "../etc//Weird Name!", "x", "postgres", "prod", "nightly",
			"etc/weird-name/x-20261002-150405.dump"},
		{"vars slugged", "", "{workflow} v2 ({engine})", "postgres", "prod", "Night Run", "night-run-v2-postgres-20261002-150405.dump"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := BuildKey(c.path, c.pattern, c.engine, c.db, c.wf, ts); got != c.want {
				t.Fatalf("BuildKey = %q, want %q", got, c.want)
			}
		})
	}
}
