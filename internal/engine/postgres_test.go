package engine

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luthfi9251/ku-dump/internal/cryptx"
	"github.com/luthfi9251/ku-dump/internal/meta"
)

func pgDB(t *testing.T, cx *cryptx.Cryptx) meta.Database {
	t.Helper()
	enc, err := cx.Encrypt("s3cret")
	if err != nil {
		t.Fatal(err)
	}
	return meta.Database{ID: 1, Name: "prod", Engine: "postgres", Host: "db1", Port: 5432,
		DBName: "app", Username: "u", PasswordEnc: enc, Options: `{"sslmode":"require"}`}
}

func newPostgres(t *testing.T) (*Postgres, *cryptx.Cryptx, string) {
	t.Helper()
	dir := t.TempDir()
	pgDump := writeScript(t, dir, "pg_dump.sh", "#!/bin/sh\necho \"$@\" >&2\nprintf 'PGDMP-fake'\nexit 0\n")
	pgRestore := writeScript(t, dir, "pg_restore.sh", "#!/bin/sh\ncat >/dev/null\necho \"$@\" >&2\nexit 0\n")
	cx := mustCrypt(t)
	ts := ToolSet{
		"pg_dump":    {Name: "pg_dump", Args: []string{pgDump}},
		"pg_restore": {Name: "pg_restore", Args: []string{pgRestore}},
	}
	return NewPostgres(ts, cx), cx, dir
}

func writeScript(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPostgresDumpArgs(t *testing.T) {
	p, cx, _ := newPostgres(t)
	db := pgDB(t, cx)
	args := p.dumpArgs(db)
	joined := strings.Join(args, " ")
	for _, want := range []string{"-Fc", "--no-owner", "--no-privileges", "-h db1", "-p 5432", "-U u", "-d app"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("dumpArgs missing %q: %s", want, joined)
		}
	}
}

func TestPostgresRestoreArgs(t *testing.T) {
	p, cx, _ := newPostgres(t)
	db := pgDB(t, cx)
	joined := strings.Join(p.restoreArgs(db), " ")
	for _, want := range []string{"--clean", "--if-exists", "--no-owner", "--no-privileges", "-h db1", "-d app"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("restoreArgs missing %q: %s", want, joined)
		}
	}
}

func TestPostgresDumpAndRestoreRun(t *testing.T) {
	p, cx, _ := newPostgres(t)
	db := pgDB(t, cx)
	var out, log bytes.Buffer
	if err := p.Dump(context.Background(), db, &out, &log); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.String(), "PGDMP") {
		t.Fatalf("out = %q", out.String())
	}
	if !strings.Contains(log.String(), "-Fc") {
		t.Fatalf("log = %q", log.String())
	}
	if err := p.Restore(context.Background(), db, "app", strings.NewReader(out.String()), &log); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresDumpToolFailure(t *testing.T) {
	dir := t.TempDir()
	bad := writeScript(t, dir, "pg_dump.sh", "#!/bin/sh\necho boom >&2\nexit 3\n")
	cx := mustCrypt(t)
	ts := ToolSet{"pg_dump": {Name: "pg_dump", Args: []string{bad}}}
	p := NewPostgres(ts, cx)
	err := p.Dump(context.Background(), pgDB(t, cx), &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v", err)
	}
}

func TestPostgresToolMissing(t *testing.T) {
	p := NewPostgres(ToolSet{}, mustCrypt(t))
	if got := p.ToolsMissing(); len(got) != 2 {
		t.Fatalf("ToolsMissing = %v", got)
	}
	err := p.Dump(context.Background(), meta.Database{}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !errors.Is(err, ErrToolMissing) {
		t.Fatalf("err = %v", err)
	}
}

func TestPostgresSSLModeDefault(t *testing.T) {
	p, cx, _ := newPostgres(t)
	db := pgDB(t, cx)
	db.Options = ""
	if got := p.sslMode(db); got != "prefer" {
		t.Fatalf("sslMode = %q", got)
	}
	db.Options = `{"sslmode":"disable"}`
	if got := p.sslMode(db); got != "disable" {
		t.Fatalf("sslMode = %q", got)
	}
}
