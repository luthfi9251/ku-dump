package meta

import (
	"database/sql"
	"time"

	_ "modernc.org/sqlite"
)

var ErrNotFound = sql.ErrNoRows

type Store struct {
	db *sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS users (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  username TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE TABLE IF NOT EXISTS databases (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL UNIQUE,
  engine TEXT NOT NULL CHECK (engine IN ('postgres','mongodb')),
  host TEXT NOT NULL,
  port INTEGER NOT NULL,
  db_name TEXT NOT NULL,
  username TEXT NOT NULL,
  password_enc TEXT NOT NULL,
  options TEXT NOT NULL DEFAULT '{}',
  last_test_at TEXT,
  last_test_ok INTEGER,
  created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE TABLE IF NOT EXISTS dumps (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  database_id INTEGER NOT NULL DEFAULT 0,
  engine TEXT NOT NULL,
  label TEXT NOT NULL,
  storage TEXT NOT NULL,
  location TEXT NOT NULL DEFAULT '',
  source_db TEXT NOT NULL DEFAULT '',
  size_bytes INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','ready','uploaded','failed')),
  created_by INTEGER NOT NULL,
  created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE TABLE IF NOT EXISTS jobs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  type TEXT NOT NULL CHECK (type IN ('dump','restore')),
  database_id INTEGER NOT NULL,
  dump_id INTEGER,
  storage TEXT,
  status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','running','success','failed','cancelled')),
  log_path TEXT NOT NULL DEFAULT '',
  error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  started_at TEXT,
  finished_at TEXT
);
CREATE TABLE IF NOT EXISTS settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS storage_destinations (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL UNIQUE,
  kind TEXT NOT NULL CHECK (kind IN ('s3')),
  endpoint TEXT NOT NULL,
  region TEXT NOT NULL DEFAULT '',
  bucket TEXT NOT NULL,
  prefix TEXT NOT NULL DEFAULT '',
  access_key TEXT NOT NULL,
  secret_enc TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE TABLE IF NOT EXISTS dump_workflows (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  name         TEXT NOT NULL,
  database_id  INTEGER NOT NULL REFERENCES databases(id) ON DELETE CASCADE,
  dest_id      INTEGER NOT NULL DEFAULT 0,
  trigger_kind TEXT NOT NULL CHECK (trigger_kind IN ('manual','once','cron')),
  run_at       TEXT,
  cron         TEXT NOT NULL DEFAULT '',
  enabled      INTEGER NOT NULL DEFAULT 1,
  last_run_at  TEXT,
  last_error   TEXT NOT NULL DEFAULT '',
  next_run_at  TEXT,
  created_at   TEXT NOT NULL DEFAULT (datetime('now'))
);
`

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	if err := ensureColumn(db, "dumps", "dest_id", `ALTER TABLE dumps ADD COLUMN dest_id INTEGER NOT NULL DEFAULT 0`); err != nil {
		db.Close()
		return nil, err
	}
	if err := ensureColumn(db, "dumps", "workflow_id", `ALTER TABLE dumps ADD COLUMN workflow_id INTEGER NOT NULL DEFAULT 0`); err != nil {
		db.Close()
		return nil, err
	}
	if err := ensureColumn(db, "dump_workflows", "storage_path", `ALTER TABLE dump_workflows ADD COLUMN storage_path TEXT NOT NULL DEFAULT ''`); err != nil {
		db.Close()
		return nil, err
	}
	if err := ensureColumn(db, "dump_workflows", "filename_pattern", `ALTER TABLE dump_workflows ADD COLUMN filename_pattern TEXT NOT NULL DEFAULT ''`); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func ensureColumn(db *sql.DB, table, column, ddl string) error {
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`, table, column).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		_, err := db.Exec(ddl)
		return err
	}
	return nil
}

func (s *Store) Close() error { return s.db.Close() }

func parseTime(v string) time.Time {
	t, _ := time.Parse("2006-01-02 15:04:05", v)
	return t.UTC()
}

func parseTimePtr(ns sql.NullString) *time.Time {
	if !ns.Valid {
		return nil
	}
	t := parseTime(ns.String)
	return &t
}
