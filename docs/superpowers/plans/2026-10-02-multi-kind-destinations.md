# Multi-Kind Destinations (Local / S3 / SFTP) + Restore Picker UX — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Storage destinations menjadi halaman sendiri dengan 3 tipe reusable (S3, Local folder, SFTP remote), semua bisa di-assign ke workflow, plus perbaikan UX pemilihan file di RestoreModal.

**Architecture:** Satu tabel `storage_destinations` di-rebuild (CHECK constraint SQLite tidak bisa di-ALTER) menambah kolom per tipe. Interface `storage.Store` tidak berubah — tiap tipe adalah implementasi (LocalFS root custom, S3 existing, SFTP baru). Factory `newStore` switch per kind. Frontend: halaman `/destinations` baru, modal destination multi-tipe, RestoreModal dengan filter/group per destination.

**Tech Stack:** Go 1.26 (chi-free stdlib mux), SQLite via modernc.org/sqlite, `github.com/pkg/sftp` + `golang.org/x/crypto/ssh` (sudah direct dep), React 19 + Vite + TanStack Query + Tailwind.

**Spec:** `docs/superpowers/specs/2026-10-02-multi-kind-destinations-design.md`

## Global Constraints

- Toolchain PATH (jalankan sebelum go/npm): `export PATH=$PATH:/usr/local/go/bin:~/go/bin:~/.nvm/versions/node/v24.21.0/bin`
- Dependency baru hanya `github.com/pkg/sftp`; `golang.org/x/crypto` sudah ada di go.mod.
- Commit message gaya repo: `feat(scope): ...`, `test(scope): ...`, `chore: ...`. Satu commit per task (atau lebih jika natural), jangan menggabungkan antar task.
- Secret (password SFTP, private key, S3 secret) selalu lewat `s.Crypt.Encrypt/Decrypt`, kolom `secret_enc`. Jangan pernah mengembalikan secret di DTO.
- Local default (`destID = 0` → `KUDUMP_DUMPS_DIR`) tidak berubah dan tidak masuk tabel.
- SPA + API ship bareng: web dist di-rebuild di task terakhir; tidak ada backward-compat API untuk consumer eksternal.
- Anti-timpa tetap: `Put` fail-if-exists untuk SEMUA tipe (LocalFS sudah O_EXCL, S3 sudah StatObject, SFTP harus StatObject dulu).
- Jalankan `go test ./...` sebelum setiap commit task backend; `npm --prefix web run build` (tanpa install ulang jika node_modules ada) di task web.

---

### Task 1: meta — Rebuild schema + Destination multi-kind CRUD

**Files:**
- Modify: `internal/meta/meta.go` (schema storage_destinations + rebuild migration di `Open`)
- Modify: `internal/meta/destinations.go` (struct, cols, scan, create, update)
- Test: `internal/meta/destinations_test.go`, `internal/meta/meta_test.go`

**Interfaces:**
- Consumes: `meta.Open(path)`, `Destination`, `CreateDestination`, `GetDestination`, `UpdateDestination` (sudah ada).
- Produces: `Destination` bertambah field `RootPath string`, `Host string`, `Port int`, `Username string`, `AuthType string` (`""|"password"|"key"`), `RemoteDir string`. `CreateDestination`/`UpdateDestination` menulis semua kolom baru. Fungsi baru `rebuildDestinationsTable(db *sql.DB) error` (idempoten).

- [ ] **Step 1: Tulis test gagal — CRUD tiga kind**

Tambahkan di `internal/meta/destinations_test.go`:

```go
func TestDestinationKinds(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	s3ID, err := st.CreateDestination(ctx, &Destination{Name: "minio", Kind: "s3",
		Endpoint: "http://e", Bucket: "b", AccessKey: "k", SecretEnc: "s"})
	if err != nil {
		t.Fatal(err)
	}
	localID, err := st.CreateDestination(ctx, &Destination{Name: "nas", Kind: "local", RootPath: "/srv/backups"})
	if err != nil {
		t.Fatal(err)
	}
	sftpID, err := st.CreateDestination(ctx, &Destination{Name: "offsite", Kind: "sftp",
		Host: "backup.example.com", Port: 2222, Username: "backup", AuthType: "key",
		SecretEnc: "KEYDATA", RemoteDir: "/srv/dumps"})
	if err != nil {
		t.Fatalf("insert sftp kind rejected: %v", err)
	}

	d, err := st.GetDestination(ctx, localID)
	if err != nil || d.Kind != "local" || d.RootPath != "/srv/backups" {
		t.Fatalf("local = %+v, %v", d, err)
	}
	d, err = st.GetDestination(ctx, sftpID)
	if err != nil || d.Host != "backup.example.com" || d.Port != 2222 ||
		d.Username != "backup" || d.AuthType != "key" || d.RemoteDir != "/srv/dumps" || d.SecretEnc != "KEYDATA" {
		t.Fatalf("sftp = %+v, %v", d, err)
	}

	d.Port = 22
	if err := st.UpdateDestination(ctx, d); err != nil {
		t.Fatal(err)
	}
	d, _ = st.GetDestination(ctx, sftpID)
	if d.Port != 22 {
		t.Fatalf("port after update = %d", d.Port)
	}

	rows, err := st.ListDestinations(ctx)
	if err != nil || len(rows) != 3 {
		t.Fatalf("list = %d, %v", len(rows), err)
	}
	_ = s3ID
}
```

- [ ] **Step 2: Tulis test gagal — migrasi rebuild schema lama**

Tambahkan di `internal/meta/meta_test.go` (package `meta`; import `database/sql`, `os/exec` tidak perlu — pakai driver "sqlite" yang sudah diimport package):

```go
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
```

- [ ] **Step 3: Jalankan test, pastikan gagal**

Run: `go test ./internal/meta/ -run 'TestDestinationKinds|TestDestinationSchemaRebuild' -v`
Expected: FAIL — kolom root_path tidak ada / CHECK menolak 'sftp' / struct tidak punya field baru (compile error dihitung gagal).

- [ ] **Step 4: Implementasi**

`internal/meta/meta.go`:

1. Ganti definisi tabel di konstanta `schema`:

```sql
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
```

2. Di akhir `Open`, setelah loop ensureColumn terakhir, tambahkan:

```go
	if err := rebuildDestinationsTable(db); err != nil {
		db.Close()
		return nil, err
	}
```

3. Tambahkan fungsi (import `strings` bila belum ada):

```go
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
```

(`fmt` sudah diimport meta.go? Jika belum, tambahkan.)

`internal/meta/destinations.go`:

```go
type Destination struct {
	ID        int64
	Name      string
	Kind      string // "s3" | "local" | "sftp"
	Endpoint  string
	Region    string
	Bucket    string
	Prefix    string
	AccessKey string
	SecretEnc string
	RootPath  string // local
	Host      string // sftp
	Port      int    // sftp
	Username  string // sftp
	AuthType  string // sftp: "" | "password" | "key"
	RemoteDir string // sftp
	CreatedAt time.Time
}

const destinationCols = `id, name, kind, endpoint, region, bucket, prefix, access_key, secret_enc,
	root_path, host, port, username, auth_type, remote_dir, created_at`
```

`scanDestination` scan tambahan sesuai urutan cols. `CreateDestination`:

```go
	res, err := s.db.ExecContext(ctx, `INSERT INTO storage_destinations
		(name, kind, endpoint, region, bucket, prefix, access_key, secret_enc,
		 root_path, host, port, username, auth_type, remote_dir)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		d.Name, d.Kind, d.Endpoint, d.Region, d.Bucket, d.Prefix, d.AccessKey, d.SecretEnc,
		d.RootPath, d.Host, d.Port, d.Username, d.AuthType, d.RemoteDir)
```

`UpdateDestination` set semua kolom + `WHERE id = ?`. `MigrateLegacyS3` tidak berubah (kolom eksplisit yang di-INSERT masih ada).

- [ ] **Step 5: Jalankan test, pastikan pass**

Run: `go test ./internal/meta/ -v`
Expected: PASS semua (termasuk test lama).

- [ ] **Step 6: Commit**

```bash
git add internal/meta/
git commit -m "feat(meta): multi-kind destinations schema + sftp/local fields"
```

---

### Task 2: storage — SFTP store

**Files:**
- Create: `internal/storage/sftp.go`
- Test: `internal/storage/sftp_test.go`
- Modify: `go.mod` / `go.sum` (go get github.com/pkg/sftp)

**Interfaces:**
- Consumes: pola `Store` interface (Kind/Put/Open/Delete) + konvensi fail-if-exists.
- Produces:

```go
type SFTPConfig struct {
	Host      string
	Port      int    // 0 => 22
	Username  string
	AuthType  string // "password" | "key"
	Secret    string // password atau PEM private key
	RemoteDir string // "" => "."
}
func NewSFTP(cfg SFTPConfig) (*SFTP, error) // ErrSFTPNotConfigured bila kosong
func (s *SFTP) Kind() string                // "sftp"
func (s *SFTP) Put(ctx context.Context, key, localPath string) error
func (s *SFTP) Open(ctx context.Context, key string) (io.ReadCloser, error)
func (s *SFTP) Delete(ctx context.Context, key string) error
func (s *SFTP) Test(ctx context.Context) error
```

- [ ] **Step 1: Tambah dependency**

Run: `go get github.com/pkg/sftp@latest && go mod tidy`
Expected: go.mod berisi `github.com/pkg/sftp`.

- [ ] **Step 2: Tulis test gagal**

`internal/storage/sftp_test.go`:

```go
package storage

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func TestNewSFTPValidation(t *testing.T) {
	ok := SFTPConfig{Host: "h", Username: "u", AuthType: "password", Secret: "s"}
	if _, err := NewSFTP(ok); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	bad := []SFTPConfig{
		{},
		{Username: "u", AuthType: "password", Secret: "s"},
		{Host: "h", AuthType: "password", Secret: "s"},
		{Host: "h", Username: "u", Secret: "s"},
		{Host: "h", Username: "u", AuthType: "password"},
		{Host: "h", Username: "u", AuthType: "token", Secret: "s"},
	}
	for i, cfg := range bad {
		if _, err := NewSFTP(cfg); !errors.Is(err, ErrSFTPNotConfigured) {
			t.Fatalf("case %d: err = %v, want ErrSFTPNotConfigured", i, err)
		}
	}
	s, _ := NewSFTP(ok)
	if s.Kind() != "sftp" {
		t.Fatalf("kind = %q", s.Kind())
	}
}

func TestSFTPRemotePathRejectsTraversal(t *testing.T) {
	s, _ := NewSFTP(SFTPConfig{Host: "h", Username: "u", AuthType: "password", Secret: "s", RemoteDir: "/srv"})
	if _, err := s.remotePath("../escape.dump"); err == nil {
		t.Fatal("traversal key accepted")
	}
	p, err := s.remotePath("postgres/db/x.dump")
	if err != nil || p != "/srv/postgres/db/x.dump" {
		t.Fatalf("path = %q, %v", p, err)
	}
}

func TestSFTPOpsFailWithoutServer(t *testing.T) {
	// port 1 on localhost is closed; every op must fail cleanly, not hang.
	s, _ := NewSFTP(SFTPConfig{Host: "127.0.0.1", Port: 1, Username: "u",
		AuthType: "password", Secret: "s"})
	ctx := context.Background()
	done := make(chan error, 1)
	go func() { done <- s.Test(ctx) }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Test succeeded without a server")
		}
	case <-time.After(20 * time.Second):
		t.Fatal("Test hung without a server")
	}
	if _, err := s.Open(ctx, "k"); err == nil {
		t.Fatal("Open succeeded without a server")
	}
	src := putFile(t, "x")
	if err := s.Put(ctx, "k", src); err == nil {
		t.Fatal("Put succeeded without a server")
	}
	if err := s.Delete(ctx, "k"); err == nil {
		t.Fatal("Delete succeeded without a server")
	}
	_ = io.Discard
	_ = strings.TrimSpace
}
```

(Hapus baris `_ = io.Discard` / `_ = strings.TrimSpace` beserta import `io`/`strings` jika tidak terpakai — placeholder agar import tidak error saat test masih merah; setelah implementasi bersihkan.)

- [ ] **Step 3: Jalankan test, pastikan gagal (compile error)**

Run: `go test ./internal/storage/ -run TestSFTP -v`
Expected: FAIL — `NewSFTP` / `remotePath` undefined.

- [ ] **Step 4: Implementasi `internal/storage/sftp.go`**

```go
package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

var ErrSFTPNotConfigured = errors.New("sftp storage not configured")

type SFTPConfig struct {
	Host      string
	Port      int
	Username  string
	AuthType  string // "password" | "key"
	Secret    string // password or PEM private key
	RemoteDir string
}

type SFTP struct {
	cfg SFTPConfig
}

func NewSFTP(cfg SFTPConfig) (*SFTP, error) {
	if cfg.Host == "" || cfg.Username == "" || cfg.AuthType == "" || cfg.Secret == "" {
		return nil, ErrSFTPNotConfigured
	}
	if cfg.AuthType != "password" && cfg.AuthType != "key" {
		return nil, fmt.Errorf("%w: invalid authType %q", ErrSFTPNotConfigured, cfg.AuthType)
	}
	if cfg.Port == 0 {
		cfg.Port = 22
	}
	if cfg.RemoteDir == "" {
		cfg.RemoteDir = "."
	}
	return &SFTP{cfg: cfg}, nil
}

func (s *SFTP) Kind() string { return "sftp" }

// dial opens one SSH connection with an SFTP session over it. The returned
// func closes both and must be called exactly once.
func (s *SFTP) dial() (*sftp.Client, func(), error) {
	var auth ssh.AuthMethod
	switch s.cfg.AuthType {
	case "password":
		auth = ssh.Password(s.cfg.Secret)
	default: // "key", guarded by NewSFTP
		signer, err := ssh.ParsePrivateKey([]byte(s.cfg.Secret))
		if err != nil {
			return nil, nil, fmt.Errorf("parse private key: %w", err)
		}
		auth = ssh.PublicKeys(signer)
	}
	// ponytail: host key not pinned; add known_hosts verification before
	// pointing this at an untrusted network.
	conn, err := ssh.Dial("tcp",
		net.JoinHostPort(s.cfg.Host, fmt.Sprintf("%d", s.cfg.Port)),
		&ssh.ClientConfig{
			User:            s.cfg.Username,
			Auth:            []ssh.AuthMethod{auth},
			HostKeyCallback: ssh.InsecureIgnoreHostKey(),
			Timeout:         15 * time.Second,
		})
	if err != nil {
		return nil, nil, err
	}
	cl, err := sftp.NewClient(conn)
	if err != nil {
		conn.Close()
		return nil, nil, err
	}
	return cl, func() { cl.Close(); conn.Close() }, nil
}

func (s *SFTP) remotePath(key string) (string, error) {
	if key == "" || strings.Contains(key, "..") {
		return "", errors.New("invalid storage key")
	}
	return path.Join(s.cfg.RemoteDir, key), nil
}

func (s *SFTP) Put(ctx context.Context, key, localPath string) error {
	p, err := s.remotePath(key)
	if err != nil {
		return err
	}
	cl, closeConn, err := s.dial()
	if err != nil {
		return err
	}
	defer closeConn()
	if dir := path.Dir(p); dir != "." && dir != "/" {
		if err := cl.MkdirAll(dir); err != nil {
			return err
		}
	}
	if _, err := cl.Stat(p); err == nil {
		return fmt.Errorf("object already exists: %s", p)
	}
	in, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := cl.Create(p)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return cl.Chmod(p, 0o644)
}

func (s *SFTP) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	p, err := s.remotePath(key)
	if err != nil {
		return nil, err
	}
	cl, closeConn, err := s.dial()
	if err != nil {
		return nil, err
	}
	f, err := cl.Open(p)
	if err != nil {
		closeConn()
		return nil, err
	}
	return sftpReadCloser{ReadCloser: f, closeConn: closeConn}, nil
}

type sftpReadCloser struct {
	io.ReadCloser
	closeConn func()
}

func (r sftpReadCloser) Close() error {
	err := r.ReadCloser.Close()
	r.closeConn()
	return err
}

func (s *SFTP) Delete(ctx context.Context, key string) error {
	p, err := s.remotePath(key)
	if err != nil {
		return err
	}
	cl, closeConn, err := s.dial()
	if err != nil {
		return err
	}
	defer closeConn()
	if err := cl.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (s *SFTP) Test(ctx context.Context) error {
	cl, closeConn, err := s.dial()
	if err != nil {
		return err
	}
	defer closeConn()
	if s.cfg.RemoteDir != "." && s.cfg.RemoteDir != "/" {
		if err := cl.MkdirAll(s.cfg.RemoteDir); err != nil {
			return err
		}
	}
	probe := path.Join(s.cfg.RemoteDir, ".kudump-probe")
	w, err := cl.Create(probe)
	if err != nil {
		return err
	}
	if _, err := w.Write([]byte("ok")); err != nil {
		w.Close()
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	if _, err := cl.Stat(probe); err != nil {
		return err
	}
	return cl.Remove(probe)
}
```

- [ ] **Step 5: Jalankan test, pastikan pass**

Run: `go test ./internal/storage/ -v`
Expected: PASS semua (SFTP ops tanpa server harus gagal < 20s — ssh.Dial timeout 15s).

- [ ] **Step 6: Commit**

```bash
git add internal/storage/sftp.go internal/storage/sftp_test.go go.mod go.sum
git commit -m "feat(storage): SFTP destination store (password/key auth, fail-if-exists)"
```

---

### Task 3: storage — LocalFS.Test probe

**Files:**
- Modify: `internal/storage/localfs.go`
- Test: `internal/storage/localfs_test.go`

**Interfaces:**
- Produces: `func (l *LocalFS) Test() error` — MkdirAll root + tulis/baca/hapus probe file `._kudump-probe`. Dipakai API test handler (Task 4). Catatan: `NewLocalFS` sudah MkdirAll; `Test` boleh dipanggil pada instance mana pun.

- [ ] **Step 1: Tulis test gagal**

Tambah di `internal/storage/localfs_test.go`:

```go
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
```

- [ ] **Step 2: Jalankan, pastikan gagal**

Run: `go test ./internal/storage/ -run TestLocalFSTestProbe -v`
Expected: FAIL — `l.Test undefined`.

- [ ] **Step 3: Implementasi**

Di `internal/storage/localfs.go`:

```go
// Test verifies the root exists (created if needed) and is writable by
// writing, reading and removing a probe file.
func (l *LocalFS) Test() error {
	if err := os.MkdirAll(l.root, 0o755); err != nil {
		return err
	}
	probe := filepath.Join(l.root, "._kudump-probe")
	if err := os.WriteFile(probe, []byte("ok"), 0o644); err != nil {
		return err
	}
	if _, err := os.ReadFile(probe); err != nil {
		return err
	}
	return os.Remove(probe)
}
```

- [ ] **Step 4: Jalankan, pastikan pass**

Run: `go test ./internal/storage/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/storage/localfs.go internal/storage/localfs_test.go
git commit -m "feat(storage): LocalFS.Test writable probe"
```

---

### Task 4: api — payload/DTO/validasi per kind + test endpoint switch

**Files:**
- Modify: `internal/api/destinations.go`
- Test: `internal/api/destinations_test.go`

**Interfaces:**
- Consumes: `meta.Destination` field baru (Task 1), `storage.NewSFTP` + `LocalFS.Test` (Task 2/3), `s.Crypt.Encrypt/Decrypt`.
- Produces: `destinationPayload` + `Kind/RootPath/Host/Port/Username/AuthType/RemoteDir` (json: `kind/rootPath/host/port/username/authType/remoteDir`); `normalize()` (kind kosong → "s3"); `validate(requireSecret)` per kind (local tidak butuh secret); helper `destDTO(d *meta.Destination)` memuat field baru; helper `s.testDestination(ctx, d *meta.Destination, secret string) error`. Kind tidak bisa diubah saat update (400 `KIND_IMMUTABLE`).

- [ ] **Step 1: Tulis test gagal**

Tambah di `internal/api/destinations_test.go` (ikuti helper `doJSON`, `setupAndLogin`, `newTestServer` yang sudah ada di package):

```go
func TestLocalDestinationLifecycle(t *testing.T) {
	h := newTestServer(t)
	cookie := setupAndLogin(t, h)
	root := t.TempDir()

	// relative path rejected
	rec := doJSON(t, h, "POST", "/api/storage/destinations", map[string]any{
		"name": "bad", "kind": "local", "rootPath": "relative/path",
	}, cookie)
	if rec.Code != 400 {
		t.Fatalf("relative rootPath = %d: %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, h, "POST", "/api/storage/destinations", map[string]any{
		"name": "nfs", "kind": "local", "rootPath": root + "/pg",
	}, cookie)
	if rec.Code != 201 {
		t.Fatalf("create local = %d: %s", rec.Code, rec.Body.String())
	}
	var d struct {
		ID       string `json:"id"`
		Kind     string `json:"kind"`
		RootPath string `json:"rootPath"`
		SecretSet bool  `json:"secretSet"`
	}
	json.Unmarshal(rec.Body.Bytes(), &d)
	if d.Kind != "local" || d.RootPath != root+"/pg" || d.SecretSet {
		t.Fatalf("created = %+v", d)
	}
	if _, err := os.Stat(root + "/pg"); err != nil {
		t.Fatalf("rootPath not created on save: %v", err)
	}

	// test endpoint probes the folder
	rec = doJSON(t, h, "POST", "/api/storage/destinations/"+d.ID+"/test", nil, cookie)
	var tr struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	json.Unmarshal(rec.Body.Bytes(), &tr)
	if !tr.OK {
		t.Fatalf("local test failed: %s", tr.Error)
	}

	// kind is immutable on update
	rec = doJSON(t, h, "PUT", "/api/storage/destinations/"+d.ID, map[string]any{
		"name": "nfs", "kind": "s3", "endpoint": "http://e", "bucket": "b", "accessKey": "k", "secretKey": "s",
	}, cookie)
	if rec.Code != 400 {
		t.Fatalf("kind change = %d", rec.Code)
	}

	// delete still works while unused
	rec = doJSON(t, h, "DELETE", "/api/storage/destinations/"+d.ID, nil, cookie)
	if rec.Code != 200 {
		t.Fatalf("delete local = %d", rec.Code)
	}
}

func TestSFTPDestinationValidation(t *testing.T) {
	h := newTestServer(t)
	cookie := setupAndLogin(t, h)

	// missing authType
	rec := doJSON(t, h, "POST", "/api/storage/destinations", map[string]any{
		"name": "off", "kind": "sftp", "host": "h", "username": "u", "secretKey": "s",
	}, cookie)
	if rec.Code != 400 {
		t.Fatalf("missing authType = %d", rec.Code)
	}

	// create ok (test endpoint fails gracefully against a dead host)
	rec = doJSON(t, h, "POST", "/api/storage/destinations", map[string]any{
		"name": "off", "kind": "sftp", "host": "127.0.0.1", "port": 1,
		"username": "u", "authType": "password", "secretKey": "s", "remoteDir": "dumps",
	}, cookie)
	if rec.Code != 201 {
		t.Fatalf("create sftp = %d: %s", rec.Code, rec.Body.String())
	}
	var d struct {
		ID       string `json:"id"`
		Port     int    `json:"port"`
		AuthType string `json:"authType"`
	}
	json.Unmarshal(rec.Body.Bytes(), &d)
	if d.Port != 1 || d.AuthType != "password" {
		t.Fatalf("created = %+v", d)
	}

	// update keeps secret when blank
	rec = doJSON(t, h, "PUT", "/api/storage/destinations/"+d.ID, map[string]any{
		"name": "off2", "kind": "sftp", "host": "127.0.0.1", "port": 2,
		"username": "u", "authType": "key",
	}, cookie)
	if rec.Code != 200 {
		t.Fatalf("update sftp = %d: %s", rec.Code, rec.Body.String())
	}

	// test endpoint returns ok:false (dead host), not a 5xx
	rec = doJSON(t, h, "POST", "/api/storage/destinations/"+d.ID+"/test", nil, cookie)
	var tr struct {
		OK bool `json:"ok"`
	}
	json.Unmarshal(rec.Body.Bytes(), &tr)
	if tr.OK {
		t.Fatal("sftp test unexpectedly ok")
	}
}

func TestDestinationKindDefaultsToS3(t *testing.T) {
	h := newTestServer(t)
	cookie := setupAndLogin(t, h)
	// no kind field -> s3 validation applies
	rec := doJSON(t, h, "POST", "/api/storage/destinations", map[string]any{
		"name": "legacy-client", "endpoint": "http://e", "bucket": "b", "accessKey": "k", "secretKey": "s",
	}, cookie)
	if rec.Code != 201 {
		t.Fatalf("kindless payload = %d: %s", rec.Code, rec.Body.String())
	}
	var d struct {
		Kind string `json:"kind"`
	}
	json.Unmarshal(rec.Body.Bytes(), &d)
	if d.Kind != "s3" {
		t.Fatalf("kind = %q", d.Kind)
	}
}
```

(Jika `os` belum diimport di file test, tambahkan.)

- [ ] **Step 2: Jalankan, pastikan gagal**

Run: `go test ./internal/api/ -run 'TestLocalDestination|TestSFTPDestination|TestDestinationKindDefaults' -v`
Expected: FAIL — payload belum punya field baru (JSON diabaikan), create local 400 (validasi s3), dsb.

- [ ] **Step 3: Implementasi `internal/api/destinations.go`**

Ganti `destinationPayload`, tambah `normalize`, `validate` baru, perluas DTO, dan switch di handler. Kode lengkap bagian yang berubah:

```go
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/luthfi9251/ku-dump/internal/meta"
	"github.com/luthfi9251/ku-dump/internal/storage"
)

type destinationPayload struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Endpoint  string `json:"endpoint"`
	Region    string `json:"region"`
	Bucket    string `json:"bucket"`
	Prefix    string `json:"prefix"`
	AccessKey string `json:"accessKey"`
	SecretKey string `json:"secretKey"`
	RootPath  string `json:"rootPath"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
	Username  string `json:"username"`
	AuthType  string `json:"authType"`
	RemoteDir string `json:"remoteDir"`
}

// normalize defaults an absent kind to s3 so pre-multi-kind clients (and old
// tests) keep working, and trims the free-form text fields.
func (p *destinationPayload) normalize() {
	if p.Kind == "" {
		p.Kind = "s3"
	}
	p.Name = strings.TrimSpace(p.Name)
	p.RootPath = strings.TrimSpace(p.RootPath)
}

func (p destinationPayload) validate(requireSecret bool) error {
	switch p.Kind {
	case "s3":
		if p.Name == "" || p.Endpoint == "" || p.Bucket == "" || p.AccessKey == "" {
			return errors.New("name, endpoint, bucket and accessKey are required")
		}
	case "local":
		if p.Name == "" || p.RootPath == "" {
			return errors.New("name and rootPath are required")
		}
		if !filepath.IsAbs(p.RootPath) {
			return errors.New("rootPath must be an absolute path")
		}
	case "sftp":
		if p.Name == "" || p.Host == "" || p.Username == "" {
			return errors.New("name, host and username are required")
		}
		if p.AuthType != "password" && p.AuthType != "key" {
			return errors.New("authType must be password or key")
		}
	default:
		return errors.New("kind must be s3, local or sftp")
	}
	if requireSecret && p.Kind != "local" && p.SecretKey == "" {
		return errors.New("secretKey is required")
	}
	return nil
}

type destinationDTO struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Endpoint  string `json:"endpoint"`
	Region    string `json:"region"`
	Bucket    string `json:"bucket"`
	Prefix    string `json:"prefix"`
	AccessKey string `json:"accessKey"`
	SecretSet bool   `json:"secretSet"`
	RootPath  string `json:"rootPath"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
	Username  string `json:"username"`
	AuthType  string `json:"authType"`
	RemoteDir string `json:"remoteDir"`
	CreatedAt string `json:"createdAt"`
}

func destDTO(d *meta.Destination) destinationDTO {
	return destinationDTO{
		ID: encID(d.ID), Name: d.Name, Kind: d.Kind, Endpoint: d.Endpoint,
		Region: d.Region, Bucket: d.Bucket, Prefix: d.Prefix, AccessKey: d.AccessKey,
		SecretSet: d.SecretEnc != "", RootPath: d.RootPath, Host: d.Host, Port: d.Port,
		Username: d.Username, AuthType: d.AuthType, RemoteDir: d.RemoteDir,
		CreatedAt: d.CreatedAt.Format(time.RFC3339),
	}
}
```

`handleCreateDestination` ganti body setelah `p.normalize()` + `validate(true)` + cek nama:

```go
	secretEnc := ""
	if p.Kind != "local" {
		enc, err := s.Crypt.Encrypt(p.SecretKey)
		if err != nil {
			log.Printf("encrypt secret: %v", err)
			fail(w, http.StatusInternalServerError, "INTERNAL", "internal error")
			return
		}
		secretEnc = enc
	}
	if p.Kind == "local" {
		if err := s.probeLocalRoot(p.RootPath); err != nil {
			fail(w, http.StatusBadRequest, "VALIDATION",
				"rootPath is not usable: "+err.Error())
			return
		}
	}
	id, err := s.Store.CreateDestination(r.Context(), &meta.Destination{
		Name: p.Name, Kind: p.Kind, Endpoint: p.Endpoint, Region: p.Region,
		Bucket: p.Bucket, Prefix: p.Prefix, AccessKey: p.AccessKey, SecretEnc: secretEnc,
		RootPath: p.RootPath, Host: p.Host, Port: p.Port,
		Username: p.Username, AuthType: p.AuthType, RemoteDir: p.RemoteDir,
	})
```

Tambahkan helper:

```go
func (s *Server) probeLocalRoot(rootPath string) error {
	l, err := storage.NewLocalFS(rootPath)
	if err != nil {
		return err
	}
	return l.Test()
}
```

`handleUpdateDestination`: setelah decode + normalize + validate(false):

```go
	if p.Kind != current.Kind {
		fail(w, http.StatusBadRequest, "KIND_IMMUTABLE",
			"destination kind cannot change; delete and create a new one")
		return
	}
	secretEnc := current.SecretEnc
	if p.SecretKey != "" && current.Kind != "local" {
		enc, err := s.Crypt.Encrypt(p.SecretKey)
		if err != nil { ...INTERNAL... }
		secretEnc = enc
	}
	if current.Kind == "local" && p.RootPath != current.RootPath {
		if err := s.probeLocalRoot(p.RootPath); err != nil {
			fail(w, http.StatusBadRequest, "VALIDATION", "rootPath is not usable: "+err.Error())
			return
		}
	}
	updated := &meta.Destination{
		ID: id, Name: p.Name, Kind: current.Kind, Endpoint: p.Endpoint, Region: p.Region,
		Bucket: p.Bucket, Prefix: p.Prefix, AccessKey: p.AccessKey, SecretEnc: secretEnc,
		RootPath: p.RootPath, Host: p.Host, Port: p.Port,
		Username: p.Username, AuthType: p.AuthType, RemoteDir: p.RemoteDir,
	}
```

Test endpoints — ganti kedua handler agar switch per kind:

```go
func (s *Server) testDestination(ctx context.Context, d *meta.Destination, secret string) error {
	switch d.Kind {
	case "local":
		l, err := storage.NewLocalFS(d.RootPath)
		if err != nil {
			return err
		}
		return l.Test()
	case "sftp":
		st, err := storage.NewSFTP(storage.SFTPConfig{
			Host: d.Host, Port: d.Port, Username: d.Username,
			AuthType: d.AuthType, Secret: secret, RemoteDir: d.RemoteDir,
		})
		if err != nil {
			return err
		}
		return st.Test(ctx)
	default:
		s3, err := storage.NewS3(storage.S3Config{
			Endpoint: d.Endpoint, Region: d.Region, Bucket: d.Bucket,
			Prefix: d.Prefix, AccessKey: d.AccessKey, SecretKey: secret,
		})
		if err != nil {
			return err
		}
		return s3.Test(ctx)
	}
}

func (s *Server) destSecret(ctx context.Context, d *meta.Destination) (string, error) {
	if d.SecretEnc == "" {
		return "", nil
	}
	return s.Crypt.Decrypt(d.SecretEnc)
}
```

`handleTestDestination`: pakai `destSecret` lalu `s3TestResponse(w, s.testDestination(r.Context(), d, secret))` (ganti isi lama; nama `s3TestResponse` dipakai apa adanya — rename ke `testResponse` opsional bila ingin rapi, update pemanggil). `handleTestDestinationPayload`: setelah normalize+validate:

```go
	d := &meta.Destination{
		Kind: p.Kind, Endpoint: p.Endpoint, Region: p.Region, Bucket: p.Bucket,
		Prefix: p.Prefix, AccessKey: p.AccessKey, RootPath: p.RootPath,
		Host: p.Host, Port: p.Port, Username: p.Username, AuthType: p.AuthType,
		RemoteDir: p.RemoteDir,
	}
	testResponse(w, s.testDestination(r.Context(), d, p.SecretKey))
```

- [ ] **Step 4: Jalankan seluruh test api**

Run: `go test ./internal/api/ -v`
Expected: PASS semua termasuk test destination lama (payload tanpa kind → s3).

- [ ] **Step 5: Commit**

```bash
git add internal/api/destinations.go internal/api/destinations_test.go
git commit -m "feat(api): multi-kind destination CRUD + per-kind test endpoint"
```

---

### Task 5: factory newStore + runner e2e local destination row

**Files:**
- Modify: `cmd/ku-dump/main.go` (factory `newStore`)
- Test: `internal/runner/destinations_test.go` (baru)

**Interfaces:**
- Consumes: `meta.Destination` (Task 1), `storage.NewSFTP` (Task 2).
- Produces: perilaku runtime — dump/restore/download/delete bekerja untuk destination row `local` dan `sftp` tanpa perubahan signature apa pun (`newStore` tetap `func(ctx, destID) (storage.Store, error)`).

- [ ] **Step 1: Tulis test gagal**

`internal/runner/destinations_test.go`:

```go
package runner

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luthfi9251/ku-dump/internal/meta"
	"github.com/luthfi9251/ku-dump/internal/storage"
)

// multiNewStore mirrors the main.go factory: destID 0 = dumps dir, rows are
// resolved per kind.
func multiNewStore(st *meta.Store, dumpsDir string) func(context.Context, int64) (storage.Store, error) {
	return func(ctx context.Context, destID int64) (storage.Store, error) {
		if destID == 0 {
			return storage.NewLocalFS(dumpsDir)
		}
		d, err := st.GetDestination(ctx, destID)
		if err != nil {
			return nil, err
		}
		switch d.Kind {
		case "local":
			return storage.NewLocalFS(d.RootPath)
		default:
			return nil, context.Canceled // not under test here
		}
	}
}

func TestDumpAndRestoreViaLocalDestinationRow(t *testing.T) {
	dir := t.TempDir()
	st, err := meta.Open(filepath.Join(dir, "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ctx := context.Background()

	enc := "ENC"
	dbID, err := st.CreateDatabase(ctx, &meta.Database{Name: "prod", Engine: "postgres",
		Host: "h", Port: 1, DBName: "appdb", Username: "u", PasswordEnc: enc})
	if err != nil {
		t.Fatal(err)
	}
	destRoot := t.TempDir()
	destID, err := st.CreateDestination(ctx, &meta.Destination{Name: "nas", Kind: "local", RootPath: destRoot})
	if err != nil {
		t.Fatal(err)
	}

	run, err := New(st, map[string]engineEngine(t), multiNewStore(st, filepath.Join(dir, "dumps")), filepath.Join(dir, "logs"))
	if err != nil {
		t.Fatal(err)
	}
	jobID, dumpID, err := run.StartDump(ctx, dbID, destID, "lbl", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	j := waitTerminal(t, st, jobID)
	if j.Status != "success" {
		t.Fatalf("dump status = %s err = %s", j.Status, j.Err)
	}
	d, _ := st.GetDump(ctx, dumpID)
	if d.Storage != "local" || d.DestID != destID {
		t.Fatalf("dump = %+v", d)
	}
	if !strings.HasPrefix(d.Location, "postgres/prod/") {
		t.Fatalf("location = %q", d.Location)
	}
	if _, err := os.Stat(filepath.Join(destRoot, filepath.FromSlash(d.Location))); err != nil {
		t.Fatalf("file not in destination root: %v", err)
	}

	// restore from the explicit local row
	targetID, err := st.CreateDatabase(ctx, &meta.Database{Name: "dev", Engine: "postgres",
		Host: "h", Port: 1, DBName: "devdb", Username: "u", PasswordEnc: enc})
	if err != nil {
		t.Fatal(err)
	}
	rJob, err := st.CreateJob(ctx, &meta.Job{Type: "restore", DatabaseID: targetID, DumpID: &dumpID})
	if err != nil {
		t.Fatal(err)
	}
	run.Start(rJob)
	rj := waitTerminal(t, st, rJob)
	if rj.Status != "success" {
		t.Fatalf("restore status = %s err = %s", rj.Status, rj.Err)
	}
}

func engineEngine(t *testing.T) engine.Engine {
	t.Helper()
	return okEngine{} // defined in startdump_test.go
}
```

 Sesuaikan import: butuh `os` dan `github.com/luthfi9251/ku-dump/internal/engine`. (Fungsi `engineEngine` hanya jembatan agar file ini mandiri; boleh langsung pakai `okEngine{}` dan hapus helper.)

- [ ] **Step 2: Jalankan, pastikan gagal**

Run: `go test ./internal/runner/ -run TestDumpAndRestoreViaLocalDestinationRow -v`
Expected: FAIL — dump sukses tapi `d.Storage` / lokasi file tidak sesuai destination row (karena factory belum ada di main — test ini memverifikasi pola factory; jika sudah pass karena pola sudah benar di closure, tetap lanjut: test mengunci perilaku).

- [ ] **Step 3: Implementasi factory di `cmd/ku-dump/main.go`**

Ganti body closure `newStore`:

```go
	newStore := func(ctx context.Context, destID int64) (storage.Store, error) {
		if destID == 0 {
			return storage.NewLocalFS(cfg.DumpsDir)
		}
		d, err := st.GetDestination(ctx, destID)
		if err != nil {
			return nil, fmt.Errorf("storage destination %d not found", destID)
		}
		secret, err := cx.Decrypt(d.SecretEnc)
		if err != nil && d.Kind != "local" {
			return nil, fmt.Errorf("decrypt destination secret: %w", err)
		}
		switch d.Kind {
		case "local":
			return storage.NewLocalFS(d.RootPath)
		case "sftp":
			return storage.NewSFTP(storage.SFTPConfig{
				Host: d.Host, Port: d.Port, Username: d.Username,
				AuthType: d.AuthType, Secret: secret, RemoteDir: d.RemoteDir,
			})
		default:
			return storage.NewS3(storage.S3Config{
				Endpoint: d.Endpoint, Region: d.Region, Bucket: d.Bucket,
				Prefix: d.Prefix, AccessKey: d.AccessKey, SecretKey: secret,
			})
		}
	}
```

- [ ] **Step 4: Jalankan semua test**

Run: `go test ./... 2>&1 | tail -12 && go build ./...`
Expected: semua PASS, build OK.

- [ ] **Step 5: Commit**

```bash
git add cmd/ku-dump/main.go internal/runner/destinations_test.go
git commit -m "feat(main): per-kind storage factory; runner e2e for local rows"
```

---

### Task 6: web — tipe data, DestinationModal multi-kind, halaman Destinations, nav

**Files:**
- Modify: `web/src/lib/types.ts` (StorageDestinationDTO + DestinationKind)
- Modify: `web/src/modals/DestinationModal.tsx` (rewrite multi-kind)
- Create: `web/src/pages/Destinations.tsx`
- Modify: `web/src/App.tsx` (route), `web/src/Layout.tsx` (nav), `web/src/pages/Settings.tsx` (hapus section destinations)

**Interfaces:**
- Consumes: API DTO baru (Task 4): `kind, rootPath, host, port, username, authType, remoteDir`.
- Produces: `DestinationKind = 's3' | 'local' | 'sftp'`; halaman `/destinations`; modal dipakai ulang halaman baru. (WorkflowModal/Dumps ikut ke Task 7.)

- [ ] **Step 1: types.ts**

Ganti `StorageDestinationDTO`:

```ts
export type DestinationKind = 's3' | 'local' | 'sftp'

export interface StorageDestinationDTO {
  id: string
  name: string
  kind: DestinationKind
  endpoint: string
  region: string
  bucket: string
  prefix: string
  accessKey: string
  secretSet: boolean
  rootPath: string
  host: string
  port: number
  username: string
  authType: string
  remoteDir: string
  createdAt: string
}
```

- [ ] **Step 2: Rewrite `DestinationModal.tsx`**

```tsx
import { useEffect, useState } from 'react'
import type { FormEvent } from 'react'
import { Cloud, HardDrive, Server } from 'lucide-react'
import { api, ApiError } from '../lib/api'
import type { DestinationKind, StorageDestinationDTO, TestResultDTO } from '../lib/types'
import { Button, Field, Input, Modal, Spinner } from '../ui'

interface Props {
  open: boolean
  onClose: () => void
  dest?: StorageDestinationDTO
  onSaved: () => void
}

interface FormState {
  name: string
  endpoint: string
  region: string
  bucket: string
  prefix: string
  accessKey: string
  secretKey: string
  rootPath: string
  host: string
  port: string
  username: string
  authType: 'password' | 'key'
  remoteDir: string
}

const emptyForm: FormState = {
  name: '', endpoint: '', region: '', bucket: '', prefix: '', accessKey: '', secretKey: '',
  rootPath: '', host: '', port: '22', username: '', authType: 'password', remoteDir: '',
}

const kinds: { id: DestinationKind; label: string; blurb: string; icon: typeof Cloud }[] = [
  { id: 'local', label: 'Local Folder', blurb: 'Folder di server ini', icon: HardDrive },
  { id: 's3', label: 'S3 Bucket', blurb: 'S3-compatible (MinIO, R2, S3)', icon: Cloud },
  { id: 'sftp', label: 'Remote Server', blurb: 'Kirim via SFTP/SSH', icon: Server },
]

export default function DestinationModal({ open, onClose, dest, onSaved }: Props) {
  const [kind, setKind] = useState<DestinationKind>('local')
  const [form, setForm] = useState<FormState>(emptyForm)
  const [error, setError] = useState('')
  const [testMsg, setTestMsg] = useState<{ ok: boolean; text: string } | null>(null)
  const [testing, setTesting] = useState(false)
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    if (!open) return
    setError('')
    setTestMsg(null)
    if (dest) {
      setKind(dest.kind)
      setForm({
        name: dest.name, endpoint: dest.endpoint, region: dest.region,
        bucket: dest.bucket, prefix: dest.prefix, accessKey: dest.accessKey, secretKey: '',
        rootPath: dest.rootPath, host: dest.host, port: String(dest.port || 22),
        username: dest.username, authType: (dest.authType as FormState['authType']) || 'password',
        remoteDir: dest.remoteDir,
      })
    } else {
      setKind('local')
      setForm(emptyForm)
    }
  }, [open, dest])

  function set<K extends keyof FormState>(key: K, value: FormState[K]) {
    setForm((f) => ({ ...f, [key]: value }))
  }

  function body(): Record<string, unknown> {
    const base = { name: form.name.trim(), kind }
    if (kind === 's3')
      return { ...base, endpoint: form.endpoint, region: form.region, bucket: form.bucket,
        prefix: form.prefix, accessKey: form.accessKey, secretKey: form.secretKey }
    if (kind === 'local') return { ...base, rootPath: form.rootPath.trim() }
    return { ...base, host: form.host, port: Number(form.port) || 22, username: form.username,
      authType: form.authType, secretKey: form.secretKey, remoteDir: form.remoteDir }
  }

  async function onTest() {
    setTesting(true)
    setTestMsg(null)
    try {
      const res = await api.post<TestResultDTO>('/api/storage/destinations/test', body())
      setTestMsg(res.ok ? { ok: true, text: 'Connection OK' } : { ok: false, text: `Failed: ${res.error ?? 'unknown'}` })
    } catch (err) {
      setTestMsg({ ok: false, text: err instanceof ApiError ? err.message : 'test failed' })
    } finally {
      setTesting(false)
    }
  }

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    setError('')
    setSaving(true)
    try {
      if (dest) await api.put(`/api/storage/destinations/${dest.id}`, body())
      else await api.post('/api/storage/destinations', body())
      onSaved()
      onClose()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'save failed')
    } finally {
      setSaving(false)
    }
  }

  const secretLabel =
    kind === 'sftp' ? (form.authType === 'key' ? 'Private Key' : 'Password')
    : 'Secret Access Key'

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={dest ? `Edit Destination — ${dest.name}` : 'New Destination'}
      subtitle="Destinations are reusable: assign them to any workflow."
      wide
    >
      <form onSubmit={onSubmit} className="space-y-4">
        {!dest && (
          <Field label="Type">
            <div className="grid gap-3 sm:grid-cols-3">
              {kinds.map((k) => {
                const Icon = k.icon
                const active = kind === k.id
                return (
                  <button
                    key={k.id}
                    type="button"
                    onClick={() => { setKind(k.id); setTestMsg(null); setError('') }}
                    className={`flex flex-col items-start gap-1 rounded-xl border p-3 text-left transition-all ${
                      active
                        ? 'border-indigo-500 bg-indigo-950/40 text-indigo-200'
                        : 'border-slate-800 bg-slate-950/60 text-slate-400 hover:border-slate-700'
                    }`}
                  >
                    <Icon size={18} className={active ? 'text-indigo-400' : 'text-slate-500'} />
                    <span className="text-sm font-semibold">{k.label}</span>
                    <span className="text-[11px] text-slate-500">{k.blurb}</span>
                  </button>
                )
              })}
            </div>
          </Field>
        )}

        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <Field label="Display Name">
            <Input value={form.name} onChange={(e) => set('name', e.target.value)} placeholder="offsite-backups" required />
          </Field>

          {kind === 's3' && (
            <>
              <Field label="Endpoint URL" hint="e.g. http://127.0.0.1:9000">
                <Input value={form.endpoint} onChange={(e) => set('endpoint', e.target.value)} required />
              </Field>
              <Field label="Region">
                <Input value={form.region} onChange={(e) => set('region', e.target.value)} placeholder="us-east-1" />
              </Field>
              <Field label="Bucket Name">
                <Input value={form.bucket} onChange={(e) => set('bucket', e.target.value)} required />
              </Field>
              <Field label="Key Prefix (Optional)">
                <Input value={form.prefix} onChange={(e) => set('prefix', e.target.value)} placeholder="ku-dump/production" />
              </Field>
              <Field label="Access Key ID">
                <Input value={form.accessKey} onChange={(e) => set('accessKey', e.target.value)} required />
              </Field>
            </>
          )}

          {kind === 'local' && (
            <Field label="Root Folder" hint="Absolute path, e.g. /var/backups/pg — created if missing">
              <Input value={form.rootPath} onChange={(e) => set('rootPath', e.target.value)} placeholder="/var/backups/pg" className="font-mono" required />
            </Field>
          )}

          {kind === 'sftp' && (
            <>
              <Field label="Host">
                <Input value={form.host} onChange={(e) => set('host', e.target.value)} placeholder="backup.example.com" required />
              </Field>
              <Field label="Port">
                <Input type="number" value={form.port} onChange={(e) => set('port', e.target.value)} placeholder="22" />
              </Field>
              <Field label="Username">
                <Input value={form.username} onChange={(e) => set('username', e.target.value)} required />
              </Field>
              <Field label="Auth Type">
                <div className="flex gap-2">
                  {(['password', 'key'] as const).map((a) => (
                    <button key={a} type="button" onClick={() => set('authType', a)}
                      className={`flex-1 rounded-lg border px-3 py-2 text-sm transition-all ${
                        form.authType === a
                          ? 'border-indigo-500 bg-indigo-950/40 text-indigo-200'
                          : 'border-slate-800 bg-slate-950/60 text-slate-400 hover:border-slate-700'
                      }`}>
                      {a === 'password' ? 'Password' : 'Private Key'}
                    </button>
                  ))}
                </div>
              </Field>
              <Field label="Remote Dir (Optional)" hint="Created on the server if missing">
                <Input value={form.remoteDir} onChange={(e) => set('remoteDir', e.target.value)} placeholder="/srv/dumps" className="font-mono" />
              </Field>
            </>
          )}
        </div>

        {kind !== 'local' && (
          <Field label={`${secretLabel}${dest ? ' (leave blank to keep existing)' : ''}`}>
            {kind === 'sftp' && form.authType === 'key' ? (
              <textarea
                className="min-h-28 w-full rounded-lg border border-slate-700 bg-slate-950 px-3 py-2 font-mono text-xs text-slate-200 focus:border-indigo-500 focus:outline-none"
                value={form.secretKey}
                onChange={(e) => set('secretKey', e.target.value)}
                placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
                required={!dest}
              />
            ) : (
              <Input
                type={kind === 'sftp' && form.authType === 'password' ? 'password' : 'password'}
                value={form.secretKey}
                onChange={(e) => set('secretKey', e.target.value)}
                required={!dest}
              />
            )}
          </Field>
        )}

        {error && (
          <div className="rounded-lg border border-rose-800/60 bg-rose-950/40 p-2.5 text-xs text-rose-300">{error}</div>
        )}
        {testMsg && (
          <div className={`rounded-lg border p-2.5 text-xs ${
            testMsg.ok ? 'border-emerald-800/60 bg-emerald-950/40 text-emerald-300' : 'border-rose-800/60 bg-rose-950/40 text-rose-300'
          }`}>
            {testMsg.text}
          </div>
        )}

        <div className="flex items-center justify-between border-t border-slate-800 pt-4">
          <Button type="button" variant="outline" onClick={onTest} disabled={testing}>
            {testing ? <Spinner /> : 'Test Connection'}
          </Button>
          <div className="flex items-center gap-2">
            <Button type="button" variant="ghost" onClick={onClose}>Cancel</Button>
            <Button type="submit" disabled={saving}>
              {saving ? <Spinner /> : dest ? 'Save Changes' : 'Create Destination'}
            </Button>
          </div>
        </div>
      </form>
    </Modal>
  )
}
```

- [ ] **Step 3: Halaman `web/src/pages/Destinations.tsx`**

```tsx
import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { CheckCircle2, Cloud, HardDrive, KeyRound, Pencil, Plus, Server, Trash2 } from 'lucide-react'
import { api, ApiError } from '../lib/api'
import type { DestinationKind, StorageDestinationDTO, TestResultDTO } from '../lib/types'
import { Badge, Button, Spinner } from '../ui'
import DestinationModal from '../modals/DestinationModal'

export function kindIcon(kind: DestinationKind) {
  return kind === 's3' ? Cloud : kind === 'sftp' ? Server : HardDrive
}

export function kindSummary(d: StorageDestinationDTO): string {
  switch (d.kind) {
    case 's3':
      return `${d.endpoint} / ${d.bucket}${d.prefix ? ` (${d.prefix})` : ''}`
    case 'local':
      return d.rootPath
    case 'sftp':
      return `sftp://${d.username}@${d.host}:${d.port}${d.remoteDir && d.remoteDir !== '.' ? '/' + d.remoteDir : ''}`
  }
}

export default function Destinations() {
  const qc = useQueryClient()
  const [editing, setEditing] = useState<StorageDestinationDTO | undefined>(undefined)
  const [showAdd, setShowAdd] = useState(false)
  const [testMsg, setTestMsg] = useState<{ id: string; ok: boolean; text: string } | null>(null)
  const [deleteErr, setDeleteErr] = useState('')

  const dests = useQuery({
    queryKey: ['storage-destinations'],
    queryFn: () => api.get<StorageDestinationDTO[]>('/api/storage/destinations'),
  })

  const invalidate = () => void qc.invalidateQueries({ queryKey: ['storage-destinations'] })

  const remove = useMutation({
    mutationFn: (id: string) => api.del(`/api/storage/destinations/${id}`),
    onSuccess: () => { setDeleteErr(''); invalidate() },
    onError: (err) => setDeleteErr(err instanceof ApiError ? err.message : 'delete failed'),
  })

  async function testDest(d: StorageDestinationDTO) {
    setTestMsg({ id: d.id, ok: true, text: 'testing…' })
    try {
      const res = await api.post<TestResultDTO>(`/api/storage/destinations/${d.id}/test`, {})
      setTestMsg({ id: d.id, ok: res.ok, text: res.ok ? 'Connection OK' : `Failed: ${res.error ?? 'unknown'}` })
    } catch (err) {
      setTestMsg({ id: d.id, ok: false, text: err instanceof ApiError ? err.message : 'test failed' })
    }
  }

  const rows = dests.data ?? []

  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-white">Backup Destinations</h1>
          <p className="mt-1 text-xs text-slate-400">
            Reusable storage targets — local folders, S3 buckets, or remote servers over SFTP. Assign them to workflows.
          </p>
        </div>
        <Button onClick={() => setShowAdd(true)} className="shrink-0">
          <Plus size={16} /> New Destination
        </Button>
      </div>

      {deleteErr && (
        <div className="rounded-lg border border-rose-800/60 bg-rose-950/40 p-3 text-xs text-rose-300">{deleteErr}</div>
      )}

      {dests.isLoading && (
        <div className="flex justify-center py-12"><Spinner className="h-6 w-6" /></div>
      )}

      {!dests.isLoading && rows.length === 0 && (
        <div className="rounded-2xl border border-dashed border-slate-800 bg-slate-900/60 p-12 text-center text-sm text-slate-500">
          No destinations yet. The built-in local dumps folder is always available; add more here.
        </div>
      )}

      <div className="grid gap-4 lg:grid-cols-2">
        {rows.map((d) => {
          const Icon = kindIcon(d.kind)
          return (
            <div key={d.id} className="rounded-2xl border border-slate-800 bg-slate-900/80 p-5 shadow-xl backdrop-blur-md">
              <div className="flex items-start justify-between gap-3">
                <div className="flex min-w-0 items-center gap-3">
                  <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-indigo-500/10 text-indigo-400 border border-indigo-500/20">
                    <Icon size={20} />
                  </div>
                  <div className="min-w-0">
                    <div className="flex items-center gap-2">
                      <span className="truncate font-semibold text-slate-100">{d.name}</span>
                      <Badge tone={d.kind === 's3' ? 'sky' as never : d.kind === 'sftp' ? 'violet' : 'blue'}>{d.kind}</Badge>
                      {d.secretSet && (
                        <Badge tone="green"><KeyRound size={12} className="inline mr-1" /> secret set</Badge>
                      )}
                    </div>
                    <div className="mt-0.5 truncate font-mono text-xs text-slate-400">{kindSummary(d)}</div>
                  </div>
                </div>
                <div className="flex shrink-0 items-center gap-1">
                  <Button size="sm" variant="ghost" onClick={() => testDest(d)} title="Test Connection">
                    {testMsg?.id === d.id && testMsg.text === 'testing…' ? <Spinner className="h-3.5 w-3.5" /> : <CheckCircle2 size={14} />}
                  </Button>
                  <Button size="sm" variant="ghost" onClick={() => setEditing(d)} title="Edit"><Pencil size={14} /></Button>
                  <Button
                    size="sm" variant="ghost" className="hover:text-rose-400"
                    onClick={() => {
                      if (confirm(`Delete destination "${d.name}"? Destinations still referenced by dumps or workflows cannot be deleted.`))
                        remove.mutate(d.id)
                    }}
                    title="Delete"
                  >
                    <Trash2 size={14} />
                  </Button>
                </div>
              </div>
              {testMsg?.id === d.id && testMsg.text !== 'testing…' && (
                <div className={`mt-3 rounded-lg border p-2.5 text-xs ${
                  testMsg.ok ? 'border-emerald-800/60 bg-emerald-950/40 text-emerald-300' : 'border-rose-800/60 bg-rose-950/40 text-rose-300'
                }`}>
                  {testMsg.text}
                </div>
              )}
            </div>
          )
        })}
      </div>

      {showAdd && <DestinationModal open onClose={() => setShowAdd(false)} onSaved={invalidate} />}
      {editing && <DestinationModal open editing-key={editing.id} dest={editing} onClose={() => setEditing(undefined)} onSaved={invalidate} />}
    </div>
  )
}
```

Catatan: `Badge tone` valid hanya `'green'|'red'|'amber'|'blue'|'slate'|'violet'|'indigo'` — jangan pakai `'sky'`; ganti baris badge kind menjadi:

```tsx
<Badge tone={d.kind === 's3' ? 'indigo' : d.kind === 'sftp' ? 'violet' : 'blue'}>{d.kind}</Badge>
```

dan hapus `as never`.

- [ ] **Step 4: Route + nav + Settings cleanup**

`web/src/App.tsx`: import `Destinations from './pages/Destinations'`, tambah `<Route path="/destinations" element={<Destinations />} />` di dalam route terproteksi.

`web/src/Layout.tsx`: import `Server` dari lucide-react; tambah di `links` sebelum Settings:

```ts
  { to: '/destinations', label: 'Destinations', icon: Server },
```

`web/src/pages/Settings.tsx`: hapus seluruh section "Storage Destinations", state/queries/mutations destinations (`dests`, `remove`, `testDest`, `testMsg`, `deleteErr`, `editing`, `showAdd`, `invalidate`), import `DestinationModal` dan ikon yang tidak terpakai. Sisakan section Tools + judul halaman (ubah subtitle jadi "Inspect host binary execution requirements."). Tambahkan di bawah judul:

```tsx
        <p className="mt-1 text-xs text-slate-400">
          Inspect host CLI tools. Storage destinations moved to their own{' '}
          <a href="/destinations" className="text-indigo-400 hover:underline">Destinations page</a>.
        </p>
```

- [ ] **Step 5: Build web**

Run: `npm --prefix web run build`
Expected: build sukses tanpa error TS.

- [ ] **Step 6: Commit**

```bash
git add web/src
git commit -m "feat(web): destinations page with local/s3/sftp modal, settings cleanup"
```

---

### Task 7: web — ikon kind di WorkflowModal/Dumps + RestoreModal UX

**Files:**
- Modify: `internal/api/dumps.go` (DTO + `storageKind`)
- Modify: `web/src/lib/types.ts` (DumpDTO.storageKind), `web/src/lib/format.ts` (baru), `web/src/pages/Dumps.tsx`, `web/src/modals/WorkflowModal.tsx`, `web/src/modals/RestoreModal.tsx`
- Test: `internal/api/dumps_test.go`

**Interfaces:**
- Consumes: `DumpRow.Storage` (kolom `dumps.storage` berisi kind).
- Produces: `dumpDTO.Storage string \`json:"storageKind"\``; `lib/format.ts` → `export function fmtSize(bytes: number): string`; RestoreModal dengan filter destination + optgroup.

- [ ] **Step 1: Test gagal — storageKind di DTO**

Tambah di `internal/api/dumps_test.go` (cek DTO list memuat `storageKind`):

```go
func TestListDumpsExposesStorageKind(t *testing.T) {
	h := newTestServer(t)
	cookie := setupAndLogin(t, h)
	dbID := createDBViaAPI(t, h, cookie)
	// seed a dump via workflow run (fake engine, local storage)
	rec := doJSON(t, h, "POST", "/api/workflows", map[string]any{
		"name": "w", "databaseId": dbID, "triggerKind": "manual",
	}, cookie)
	var wf struct {
		ID string `json:"id"`
	}
	json.Unmarshal(rec.Body.Bytes(), &wf)
	rec = doJSON(t, h, "POST", "/api/workflows/"+wf.ID+"/run", nil, cookie)
	if rec.Code != 201 {
		t.Fatalf("run = %d: %s", rec.Code, rec.Body.String())
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		rec = doJSON(t, h, "GET", "/api/dumps", nil, cookie)
		var dumps []map[string]any
		json.Unmarshal(rec.Body.Bytes(), &dumps)
		if len(dumps) > 0 {
			if dumps[0]["storageKind"] != "local" {
				t.Fatalf("storageKind = %v", dumps[0]["storageKind"])
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("dump never appeared")
}
```

- [ ] **Step 2: Jalankan, pastikan gagal**

Run: `go test ./internal/api/ -run TestListDumpsExposesStorageKind -v`
Expected: FAIL — `storageKind` absent (nil).

- [ ] **Step 3: Implementasi backend kecil**

`internal/api/dumps.go`: tambah field di `dumpDTO`:

```go
	Storage      string `json:"storageKind"`
```

dan di `dumpToDTO`: `Storage: r.Storage,`.

- [ ] **Step 4: Jalankan test api**

Run: `go test ./internal/api/ -v`
Expected: PASS.

- [ ] **Step 5: Frontend**

`web/src/lib/format.ts` (baru):

```ts
export function fmtSize(bytes: number): string {
  if (bytes <= 0) return '—'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let v = bytes
  let u = 0
  while (v >= 1024 && u < units.length - 1) {
    v /= 1024
    u++
  }
  return `${v.toFixed(1)} ${units[u]}`
}
```

`web/src/lib/types.ts`: `DumpDTO` tambah `storageKind: 'local' | 's3' | 'sftp'`.

`web/src/pages/Dumps.tsx`:
- Hapus fungsi `fmtSize` lokal; import `import { fmtSize } from '../lib/format'`.
- Ikon kolom Storage ganti berdasarkan `d.storageKind`:

```tsx
import { Server } from 'lucide-react'
...
{d.storageKind === 's3' ? <Cloud size={13} className="text-sky-400" /> : d.storageKind === 'sftp' ? <Server size={13} className="text-violet-400" /> : <HardDrive size={13} className="text-slate-400" />}
```

`web/src/modals/WorkflowModal.tsx`: pada grid destination, ganti ikon `Cloud` statis dengan kind:

```tsx
import { Cloud, HardDrive, Server } from 'lucide-react'
...
{(dests.data ?? []).map((d) => {
  const Icon = d.kind === 's3' ? Cloud : d.kind === 'sftp' ? Server : HardDrive
  return (
    <label key={d.id} className={/* existing classes */}>
      <div className="flex items-center gap-2 text-sm font-semibold">
        <input type="radio" checked={destId === d.id} onChange={() => setDestId(d.id)} className="hidden" />
        <Icon size={18} className={destId === d.id ? 'text-indigo-400' : 'text-slate-400'} />
        {d.name}
      </div>
      <span className="mt-1 truncate text-[11px] text-slate-500">{d.kind === 'local' ? d.rootPath : d.kind === 's3' ? d.bucket : `${d.host}:${d.port}`}</span>
    </label>
  )
})}
```

`web/src/modals/RestoreModal.tsx` — ganti bagian pemilihan dump (tab history):

```tsx
import { useMemo, useRef, useState } from 'react'
import { fmtSize } from '../lib/format'
...
  const [destFilter, setDestFilter] = useState('')
  const dests = useQuery({
    queryKey: ['storage-destinations'],
    queryFn: () => api.get<StorageDestinationDTO[]>('/api/storage/destinations'),
    enabled: open,
  })

  const usableDumps = useMemo(
    () =>
      (dumps.data ?? []).filter((d) => {
        if (d.status !== 'ready' && d.status !== 'uploaded') return false
        if (target && d.engine !== target.engine) return false
        if (destFilter === 'local' ? d.destId !== '' : destFilter !== '' && d.destId !== destFilter) return false
        return true
      }),
    [dumps.data, target, destFilter],
  )

  const grouped = useMemo(() => {
    const byDest = new Map<string, DumpDTO[]>()
    for (const d of usableDumps) {
      const key = d.destId || 'local'
      if (!byDest.has(key)) byDest.set(key, [])
      byDest.get(key)!.push(d)
    }
    return byDest
  }, [usableDumps])
```

dan render (menggantikan `<Select>` dump lama):

```tsx
        {tab === 'history' && (
          <>
            <Field label="Storage Destination">
              <Select value={destFilter} onChange={(e) => setDestFilter(e.target.value)}>
                <option value="">All destinations</option>
                <option value="local">Local (built-in)</option>
                {(dests.data ?? []).map((d) => (
                  <option key={d.id} value={d.id}>{d.name} ({d.kind})</option>
                ))}
              </Select>
            </Field>
            <Field label="Source Dump Archive" hint="Newest first, grouped by destination">
              <Select value={dumpId} onChange={(e) => setDumpId(e.target.value)}>
                <option value="">Select a dump from history…</option>
                {source && <option value={source.id}>{source.label} (Selected)</option>}
                {[...grouped.entries()].map(([destKey, list]) => (
                  <optgroup key={destKey} label={destKey === 'local' ? 'Local (built-in)' : dests.data?.find((d) => d.id === destKey)?.name ?? destKey}>
                    {list
                      .filter((d) => d.id !== source?.id)
                      .map((d) => (
                        <option key={d.id} value={d.id}>
                          [{d.engine}] {d.label} — {d.databaseName || 'uploaded'} · {fmtSize(d.sizeBytes)} · {new Date(d.createdAt).toLocaleString()}
                        </option>
                      ))}
                  </optgroup>
                ))}
              </Select>
            </Field>
          </>
        )}
```

Tambah import `StorageDestinationDTO` di types import list. (List dumps API sudah newest-first: `ORDER BY d.id DESC`.)

- [ ] **Step 6: Build web + test menyeluruh**

Run: `go test ./... && npm --prefix web run build`
Expected: PASS + build OK.

- [ ] **Step 7: Commit**

```bash
git add internal/api/dumps.go internal/api/dumps_test.go web/src
git commit -m "feat(web): storage kind icons + restore picker grouped by destination"
```

---

### Task 8: integration e2e SFTP + README + rebuild dist

**Files:**
- Modify: `docker-compose.test.yml` (service sftp)
- Modify: `integration/integration_test.go` (TestSFTPDestination)
- Modify: `README.md`
- Modify: `web/dist` via build

**Interfaces:**
- Consumes: semua task sebelumnya; helper integration (`composeUp`, `testCrypt`, `tools`, `pgDatabase`, `waitReady`).
- Produces: e2e bukti dump→SFTP→restore.

- [ ] **Step 1: Compose service**

Tambah di `docker-compose.test.yml`:

```yaml
  sftp:
    image: atmoz/sftp:alpine
    command: backup:pass:1001
    ports:
      - "2222:22"
```

- [ ] **Step 2: Test integration gagal dulu (container belum dipakai), tulis test**

Tambah di `integration/integration_test.go`:

```go
func TestSFTPDestination(t *testing.T) {
	cx := testCrypt(t)
	engines := tools(cx)
	ctx := context.Background()

	dir := t.TempDir()
	st, err := meta.Open(dir + "/m.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	secret, err := cx.Encrypt("pass")
	if err != nil {
		t.Fatal(err)
	}
	destID, err := st.CreateDestination(ctx, &meta.Destination{
		Name: "offsite", Kind: "sftp", Host: "127.0.0.1", Port: 2222,
		Username: "backup", AuthType: "password", SecretEnc: secret, RemoteDir: "dumps",
	})
	if err != nil {
		t.Fatal(err)
	}

	offsite, err := storage.NewSFTP(storage.SFTPConfig{
		Host: "127.0.0.1", Port: 2222, Username: "backup",
		AuthType: "password", Secret: "pass", RemoteDir: "dumps",
	})
	if err != nil {
		t.Fatal(err)
	}
	waitReady(t, "sftp", func() error { return offsite.Test(ctx) })

	newStore := func(ctx context.Context, id int64) (storage.Store, error) {
		if id == 0 {
			return storage.NewLocalFS(dir + "/dumps")
		}
		d, err := st.GetDestination(ctx, id)
		if err != nil {
			return nil, err
		}
		sec, err := cx.Decrypt(d.SecretEnc)
		if err != nil {
			return nil, err
		}
		return storage.NewSFTP(storage.SFTPConfig{
			Host: d.Host, Port: d.Port, Username: d.Username,
			AuthType: d.AuthType, Secret: sec, RemoteDir: d.RemoteDir,
		})
	}

	db := pgDatabase(t, cx, "sftpdb")
	dbID, err := st.CreateDatabase(ctx, &db)
	if err != nil {
		t.Fatal(err)
	}
	run, err := runner.New(st, engines, newStore, dir+"/logs")
	if err != nil {
		t.Fatal(err)
	}

	jobID, dumpID, err := run.StartDump(ctx, dbID, destID, "sftp-run", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(30 * time.Second)
	var dump *meta.Dump
	for time.Now().Before(deadline) {
		dump, err = st.GetDump(ctx, dumpID)
		if err == nil && dump.Status == "ready" {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if dump == nil || dump.Status != "ready" {
		t.Fatalf("dump not ready: %+v, %v", dump, err)
	}
	if _, err := offsite.Stat(ctx, dump.Location); err != nil {
		t.Fatalf("dump not on sftp server: %v", err)
	}

	// restore from sftp into the same db (fake tools accept it)
	rJob, err := st.CreateJob(ctx, &meta.Job{Type: "restore", DatabaseID: dbID, DumpID: &dumpID})
	if err != nil {
		t.Fatal(err)
	}
	run.Start(rJob)
	dl := time.Now().Add(30 * time.Second)
	for time.Now().Before(dl) {
		j, err := st.GetJob(ctx, rJob)
		if err == nil && (j.Status == "success" || j.Status == "failed") {
			if j.Status != "success" {
				t.Fatalf("restore failed: %s", j.Err)
			}
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("restore job did not finish")
}
```

Catatan: `offsite.Stat` — jika `SFTP` belum punya `Stat`, tambahkan metode kecil di Task 2 style:

```go
func (s *SFTP) Stat(ctx context.Context, key string) error {
	p, err := s.remotePath(key)
	if err != nil {
		return err
	}
	cl, closeConn, err := s.dial()
	if err != nil {
		return err
	}
	defer closeConn()
	_, err = cl.Stat(p)
	return err
}
```

(tambahkan juga di Task 2 bila memilih ini — letakkan di implementasi Task 2 agar test compile; sertakan unit test traversal-nya.)

Run: `go test -tags integration -run TestSFTPDestination -v ./integration/`
Expected: FAIL jika compose belum punya sftp (port ditolak) → tambahkan service (Step 1) dan ulangi.
Expected akhir: PASS. (Environment dev saat ini tidak bisa pull image docker; jika pull gagal karena jaringan, jalankan di mesin dengan akses registry — jangan skip test.)

- [ ] **Step 3: README**

Update bagian Usage di `README.md` — ganti bullet Settings/Dumps:

```markdown
- **Destinations** — reusable storage targets on their own page: local
  folders (any absolute path), S3-compatible buckets, or remote servers
  over SFTP (password or key). Test each with one click; destinations
  referenced by dumps or workflows cannot be deleted. Assign any of them
  per workflow.
- **Settings** — CLI tool availability.
```

dan di paragraf restore tambahkan: "The restore dialog lets you filter dumps per destination and shows file size and date."

- [ ] **Step 4: Build final + semua test**

Run: `go vet ./... && go test ./... && npm --prefix web run build && go build -o ku-dump ./cmd/ku-dump`
Expected: semua OK.

- [ ] **Step 5: Commit**

```bash
git add docker-compose.test.yml integration/integration_test.go internal/storage/sftp.go internal/storage/sftp_test.go README.md web/dist
git commit -m "test(integration): sftp destination e2e; docs + dist rebuild"
```

---

## Self-Review

1. **Spec coverage:** schema rebuild (T1), SFTP store + fail-if-exists (T2), local probe (T3), API per-kind + test endpoint (T4), factory + runner (T5), halaman Destinations + modal + nav + Settings cleanup (T6), ikon kind + RestoreModal UX (T7), integration SFTP + README + dist (T8). Semua section spec tercakup.
2. **Placeholder scan:** tidak ada TBD/TODO; semua step berisi kode konkret.
3. **Type consistency:** `Destination` fields (T1) dipakai konsisten di T4/T5/T8; `SFTPConfig` (T2) sama di T4/T5/T8; `storageKind` (T7 backend→frontend) konsisten; `fmtSize` (T7) dipakai Dumps + RestoreModal.
