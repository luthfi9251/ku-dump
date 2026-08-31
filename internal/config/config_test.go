package config

import "testing"

func TestLoadDefaults(t *testing.T) {
	cfg := Load()
	if cfg.Addr != ":8080" {
		t.Fatalf("Addr = %q, want :8080", cfg.Addr)
	}
	if cfg.DBPath != "ku-dump.db" {
		t.Fatalf("DBPath = %q, want ku-dump.db", cfg.DBPath)
	}
	if cfg.DumpsDir != "dumps" {
		t.Fatalf("DumpsDir = %q, want dumps", cfg.DumpsDir)
	}
	if cfg.CryptKey != "" {
		t.Fatalf("CryptKey = %q, want empty", cfg.CryptKey)
	}
	for _, name := range []string{"pg_dump", "pg_restore", "mongodump", "mongorestore"} {
		if _, ok := cfg.Tools[name]; !ok {
			t.Fatalf("Tools missing key %q", name)
		}
	}
}

func TestLoadOverrides(t *testing.T) {
	t.Setenv("KUDUMP_ADDR", ":9999")
	t.Setenv("KUDUMP_DB", "/tmp/x.db")
	t.Setenv("KUDUMP_DUMPS_DIR", "/data/dumps")
	t.Setenv("KUDUMP_CRYPT_KEY", "ab")
	t.Setenv("KUDUMP_PG_DUMP", "/opt/tools/pg_dump --extra-flag")
	cfg := Load()
	if cfg.Addr != ":9999" || cfg.DBPath != "/tmp/x.db" || cfg.DumpsDir != "/data/dumps" || cfg.CryptKey != "ab" {
		t.Fatalf("overrides not applied: %+v", cfg)
	}
	if cfg.Tools["pg_dump"] != "/opt/tools/pg_dump --extra-flag" {
		t.Fatalf("Tools[pg_dump] = %q", cfg.Tools["pg_dump"])
	}
}
