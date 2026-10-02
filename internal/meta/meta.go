package meta

import (
	"database/sql"
	"fmt"
	"strings"
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
  kind TEXT NOT NULL CHECK (kind IN ('s3','local','sftp')),
  endpoint TEXT NOT NULL DEFAULT '',
  region TEXT NOT NULL DEFAULT '',
  bucket TEXT NOT NULL DEFAULT '',
  prefix TEXT NOT NULL DEFAULT '',
  access_key TEXT NOT NULL DEFAULT '',
  secret_enc TEXT NOT NULL DEFAULT '',
  root_path TEXT NOT NULL DEFAULT '',
  host TEXT NOT NULL DEFAULT '',
  port INTEGER NOT NULL DEFAULT 0,
  username TEXT NOT NULL DEFAULT '',
  auth_type TEXT NOT NULL DEFAULT '' CHECK (auth_type IN ('','password','key')),
  remote_dir TEXT NOT NULL DEFAULT '',
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
	if err := rebuildDestinationsTable(db); err != nil {
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

// rebuildDestinationsTable migrates the pre-multi-kind table (CHECK only
// allows 's3') to the multi-kind schema. SQLite cannot ALTER a CHECK
// constraint, so the table is rebuilt in one transaction. Idempotent: a
// table whose DDL already mentions 'sftp' is left alone.
func rebuildDestinationsTable(db *sql.DB) error {
	var ddl string
	err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'storage_destinations'`).Scan(&ddl)
	if err == sql.ErrNoRows {
		return nil // fresh install; schema above already has the new shape
	}
	if err != nil {
		return err
	}
	if strings.Contains(ddl, "'sftp'") {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	steps := []string{
		`CREATE TABLE storage_destinations_new (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			kind TEXT NOT NULL CHECK (kind IN ('s3','local','sftp')),
			endpoint TEXT NOT NULL DEFAULT '',
			region TEXT NOT NULL DEFAULT '',
			bucket TEXT NOT NULL DEFAULT '',
			prefix TEXT NOT NULL DEFAULT '',
			access_key TEXT NOT NULL DEFAULT '',
			secret_enc TEXT NOT NULL DEFAULT '',
			root_path TEXT NOT NULL DEFAULT '',
			host TEXT NOT NULL DEFAULT '',
			port INTEGER NOT NULL DEFAULT 0,
			username TEXT NOT NULL DEFAULT '',
			auth_type TEXT NOT NULL DEFAULT '' CHECK (auth_type IN ('','password','key')),
			remote_dir TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`INSERT INTO storage_destinations_new
			(id, name, kind, endpoint, region, bucket, prefix, access_key, secret_enc, created_at)
			SELECT id, name, kind, endpoint, region, bucket, prefix, access_key, secret_enc, created_at
			FROM storage_destinations`,
		`DROP TABLE storage_destinations`,
		`ALTER TABLE storage_destinations_new RENAME TO storage_destinations`,
	}
	for _, q := range steps {
		if _, err := tx.Exec(q); err != nil {
			return fmt.Errorf("rebuild storage_destinations: %w", err)
		}
	}
	return tx.Commit()
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
