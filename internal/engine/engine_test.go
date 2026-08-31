package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/luthfi9251/ku-dump/internal/cryptx"
)

func writeTool(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func mustCrypt(t *testing.T) *cryptx.Cryptx {
	t.Helper()
	key := make([]byte, 32)
	c, err := cryptx.New(key)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestResolveToolsDefaultLookup(t *testing.T) {
	dir := t.TempDir()
	for _, n := range ToolNames {
		writeTool(t, dir, n)
	}
	t.Setenv("PATH", dir)
	ts := ResolveTools(nil)
	for _, n := range ToolNames {
		if ts[n] == nil {
			t.Fatalf("tool %s not resolved", n)
		}
		if ts[n].Args[0] != filepath.Join(dir, n) {
			t.Fatalf("args = %v", ts[n].Args)
		}
	}
	if got := ts.Missing("pg_dump", "mongodump"); got != nil {
		t.Fatalf("missing = %v", got)
	}
}

func TestResolveToolsMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	ts := ResolveTools(nil)
	got := ts.Missing(ToolNames...)
	if len(got) != len(ToolNames) {
		t.Fatalf("missing = %v", got)
	}
}

func TestResolveToolsOverrideWithArgs(t *testing.T) {
	dir := t.TempDir()
	wrapper := writeTool(t, dir, "pg-dump-wrapper")
	ts := ResolveTools(map[string]string{"pg_dump": wrapper + " --verbose"})
	if ts["pg_dump"] == nil {
		t.Fatal("override not resolved")
	}
	want := []string{wrapper, "--verbose"}
	if len(ts["pg_dump"].Args) != 2 || ts["pg_dump"].Args[0] != want[0] || ts["pg_dump"].Args[1] != want[1] {
		t.Fatalf("args = %v", ts["pg_dump"].Args)
	}
}

func TestResolveToolsOverrideMissingBinary(t *testing.T) {
	ts := ResolveTools(map[string]string{"pg_dump": "/nonexistent/pg_dump"})
	if ts["pg_dump"] != nil {
		t.Fatal("nonexistent override resolved")
	}
}

func TestBuildEngines(t *testing.T) {
	engines := BuildEngines(nil, mustCrypt(t))
	if _, ok := engines["postgres"]; !ok {
		t.Fatal("postgres engine missing")
	}
	if _, ok := engines["mongodb"]; !ok {
		t.Fatal("mongodb engine missing")
	}
}
