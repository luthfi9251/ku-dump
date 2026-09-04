# Multi Storage Destination — Chunk 1: Backend

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Backend mendukung banyak S3 storage destination: tabel `storage_destinations`, `dumps.dest_id`, migrasi config S3 lama, endpoint CRUD, dan resolusi store per-destination di runner/main.

**Architecture:** Local = destination id `0` (built-in). Runner membaca `dest_id` dari record dump (bukan job). Secret S3 terenkripsi per-baris via `cryptx` (pola sama dengan password database).

**Tech Stack:** Go 1.26, SQLite (modernc.org/sqlite), minio-go.

**Prerequisite:** Spec `docs/superpowers/specs/2026-09-04-multi-storage-destination-design.md`.

## Global Constraints

- Local selalu tersedia sebagai `dest_id = 0`, tidak bisa di-CRUD.
- Tabel `jobs` tidak berubah; kolom `jobs.storage` berhenti ditulis.
- Kolom `dumps.storage` tetap ditulis ("local"/"s3") sebagai provenance; `dest_id` adalah sumber kebenaran.
- Secret hanya dikirim dari client, disimpan terenkripsi, tidak pernah dikembalikan ke client (`secretSet: bool`).
- Test command: `go test ./...` (unit), `go build ./...` (compile check).
- Frontend TIDAK disentuh di chunk ini; route lama `/api/settings/storage*` dihapus di sini dan SPA di-update di chunk 2.

---

### Task 1: meta — tabel `storage_destinations` + CRUD

**Files:**
- Modify: `internal/meta/meta.go` (schema)
- Create: `internal/meta/destinations.go`
- Test: `internal/meta/destinations_test.go`

**Interfaces (Produces):** `Destination` struct; `CreateDestination(ctx, *Destination) (int64, error)`, `ListDestinations(ctx) ([]Destination, error)`, `GetDestination(ctx, id int64) (*Destination, error)`, `GetDestinationByName(ctx, name string) (*Destination, error)`, `UpdateDestination(ctx, *Destination) error`, `DeleteDestination(ctx, id int64) error`.

- [ ] **Step 1: Tulis test CRUD (failing)**

```go
package meta

import (
	"context"
	"errors"
	"testing"
)

func TestDestinationCRUD(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	if rows, _ := st.ListDestinations(ctx); len(rows) != 0 {
		t.Fatalf("initial len = %d", len(rows))
	}
	id, err := st.CreateDestination(ctx, &Destination{Name: "minio", Kind: "s3",
		Endpoint: "http://127.0.0.1:9000", Region: "us-east-1", Bucket: "ku",
		Prefix: "backups", AccessKey: "ak", SecretEnc: "ENC"})
	if err != nil || id == 0 {
		t.Fatalf("create = %d, %v", id, err)
	}
	got, err := st.GetDestination(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "minio" || got.Kind != "s3" || got.Endpoint != "http://127.0.0.1:9000" ||
		got.Region != "us-east-1" || got.Bucket != "ku" || got.Prefix != "backups" ||
		got.AccessKey != "ak" || got.SecretEnc != "ENC" || got.CreatedAt.IsZero() {
		t.Fatalf("got = %+v", got)
	}
	if _, err := st.GetDestinationByName(ctx, "minio"); err != nil || got.ID != id {
		t.Fatalf("byName = %+v, %v", got, err)
	}
	if _, err := st.GetDestinationByName(ctx, "ghost"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	got.Prefix = "other"
	if err := st.UpdateDestination(ctx, got); err != nil {
		t.Fatal(err)
	}
	again, _ := st.GetDestination(ctx, id)
	if again.Prefix != "other" {
		t.Fatalf("update not applied: %+v", again)
	}
	if _, err := st.CreateDestination(ctx, &Destination{Name: "minio", Kind: "s3",
		Endpoint: "http://x", Bucket: "b", AccessKey: "a", SecretEnc: "s"}); err == nil {
		t.Fatal("duplicate name accepted")
	}
	rows, _ := st.ListDestinations(ctx)
	if len(rows) != 1 {
		t.Fatalf("len = %d", len(rows))
	}
	if err := st.DeleteDestination(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetDestination(ctx, id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}
```

- [ ] **Step 2: Jalankan test, pastikan gagal compile**

Run: `go test ./internal/meta/ -run TestDestinationCRUD`
Expected: FAIL — `undefined: Destination`

- [ ] **Step 3: Implementasi**

Tambahkan ke `schema` di `internal/meta/meta.go` (sebelum closing backtick):

```sql
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
```

Buat `internal/meta/destinations.go`:

```go
package meta

import (
	"context"
	"time"
)

type Destination struct {
	ID        int64
	Name      string
	Kind      string
	Endpoint  string
	Region    string
	Bucket    string
	Prefix    string
	AccessKey string
	SecretEnc string
	CreatedAt time.Time
}

const destinationCols = `id, name, kind, endpoint, region, bucket, prefix, access_key, secret_enc, created_at`

func scanDestination(row rowScanner) (*Destination, error) {
	var d Destination
	var created string
	err := row.Scan(&d.ID, &d.Name, &d.Kind, &d.Endpoint, &d.Region, &d.Bucket,
		&d.Prefix, &d.AccessKey, &d.SecretEnc, &created)
	if err != nil {
		return nil, err
	}
	d.CreatedAt = parseTime(created)
	return &d, nil
}

func (s *Store) CreateDestination(ctx context.Context, d *Destination) (int64, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO storage_destinations
		(name, kind, endpoint, region, bucket, prefix, access_key, secret_enc)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		d.Name, d.Kind, d.Endpoint, d.Region, d.Bucket, d.Prefix, d.AccessKey, d.SecretEnc)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) ListDestinations(ctx context.Context) ([]Destination, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+destinationCols+` FROM storage_destinations ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Destination
	for rows.Next() {
		d, err := scanDestination(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

func (s *Store) GetDestination(ctx context.Context, id int64) (*Destination, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+destinationCols+` FROM storage_destinations WHERE id = ?`, id)
	return scanDestination(row)
}

func (s *Store) GetDestinationByName(ctx context.Context, name string) (*Destination, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+destinationCols+` FROM storage_destinations WHERE name = ?`, name)
	return scanDestination(row)
}

func (s *Store) UpdateDestination(ctx context.Context, d *Destination) error {
	_, err := s.db.ExecContext(ctx, `UPDATE storage_destinations SET
		name = ?, kind = ?, endpoint = ?, region = ?, bucket = ?, prefix = ?, access_key = ?, secret_enc = ?
		WHERE id = ?`,
		d.Name, d.Kind, d.Endpoint, d.Region, d.Bucket, d.Prefix, d.AccessKey, d.SecretEnc, d.ID)
	return err
}

func (s *Store) DeleteDestination(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM storage_destinations WHERE id = ?`, id)
	return err
}
```

- [ ] **Step 4: Jalankan test sampai pass**

Run: `go test ./internal/meta/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/meta/meta.go internal/meta/destinations.go internal/meta/destinations_test.go
git commit -m "feat(meta): storage_destinations table and CRUD"
```

---

### Task 2: meta — `dumps.dest_id` + join nama destination

**Files:**
- Modify: `internal/meta/meta.go` (ensureColumn), `internal/meta/dumps.go`
- Test: `internal/meta/dumps_test.go`

**Interfaces (Produces):** `Dump.DestID int64`; `DumpRow.DestName string` ("local" untuk id 0); `CountDumpsForDestination(ctx, id int64) (int64, error)`.

- [ ] **Step 1: Tulis test (failing)** — tambahkan ke `internal/meta/dumps_test.go`:

```go
func TestDumpDestinationJoin(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	db := sampleDB()
	dbID, _ := st.CreateDatabase(ctx, &db)
	destID, err := st.CreateDestination(ctx, &Destination{Name: "minio", Kind: "s3",
		Endpoint: "http://e", Bucket: "b", AccessKey: "a", SecretEnc: "s"})
	if err != nil {
		t.Fatal(err)
	}
	st.CreateDump(ctx, &Dump{DatabaseID: dbID, Engine: "postgres", Label: "s3dump",
		Storage: "s3", DestID: destID, Status: "ready", CreatedBy: 1})
	st.CreateDump(ctx, &Dump{DatabaseID: dbID, Engine: "postgres", Label: "loc",
		Storage: "local", Status: "ready", CreatedBy: 1})
	rows, err := st.ListDumps(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("len = %d", len(rows))
	}
	if rows[0].Label != "s3dump" || rows[0].DestID != destID || rows[0].DestName != "minio" {
		t.Fatalf("rows[0] = %+v", rows[0])
	}
	if rows[1].DestID != 0 || rows[1].DestName != "local" {
		t.Fatalf("rows[1] = %+v", rows[1])
	}
	n, err := st.CountDumpsForDestination(ctx, destID)
	if err != nil || n != 1 {
		t.Fatalf("count = %d, %v", n, err)
	}
}
```

- [ ] **Step 2: Jalankan, pastikan gagal**

Run: `go test ./internal/meta/ -run TestDumpDestinationJoin`
Expected: FAIL — `d.DestID unknown field`

- [ ] **Step 3: Implementasi**

`internal/meta/meta.go` — tambahkan helper + panggil di `Open` setelah `db.Exec(schema)`:

```go
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
```

Di `Open`, setelah `CREATE TABLE` sukses, sebelum `return &Store{db: db}`:

```go
	if err := ensureColumn(db, "dumps", "dest_id", `ALTER TABLE dumps ADD COLUMN dest_id INTEGER NOT NULL DEFAULT 0`); err != nil {
		db.Close()
		return nil, err
	}
```

`internal/meta/dumps.go`:
- `Dump` struct: tambah field `DestID int64` (setelah `Storage`).
- `DumpRow`: tambah `DestName string`.
- `dumpCols` = `` `id, database_id, engine, label, storage, location, source_db, size_bytes, status, created_by, created_at, dest_id` ``.
- `scanDump`: tambah `&d.DestID` di akhir Scan.
- `CreateDump` INSERT: tambah kolom `dest_id` + placeholder + argumen `d.DestID`.
- `ListDumps` ganti query & scan:

```go
func (s *Store) ListDumps(ctx context.Context) ([]DumpRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT d.id, d.database_id, d.engine, d.label, d.storage, d.location,
		d.source_db, d.size_bytes, d.status, d.created_by, d.created_at, d.dest_id,
		IFNULL(db.name, ''),
		CASE WHEN d.dest_id = 0 THEN 'local' ELSE IFNULL(sd.name, '') END
		FROM dumps d
		LEFT JOIN databases db ON db.id = d.database_id
		LEFT JOIN storage_destinations sd ON sd.id = d.dest_id
		ORDER BY d.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DumpRow
	for rows.Next() {
		var r DumpRow
		var created string
		err := rows.Scan(&r.ID, &r.DatabaseID, &r.Engine, &r.Label, &r.Storage, &r.Location,
			&r.SourceDB, &r.SizeBytes, &r.Status, &r.CreatedBy, &created, &r.DestID,
			&r.DatabaseName, &r.DestName)
		if err != nil {
			return nil, err
		}
		r.CreatedAt = parseTime(created)
		out = append(out, r)
	}
	return out, rows.Err()
}
```

`internal/meta/destinations.go` — tambahkan:

```go
func (s *Store) CountDumpsForDestination(ctx context.Context, id int64) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM dumps WHERE dest_id = ?`, id).Scan(&n)
	return n, err
}
```

- [ ] **Step 4: Jalankan semua test meta**

Run: `go test ./internal/meta/`
Expected: PASS (termasuk test lama)

- [ ] **Step 5: Commit**

```bash
git add internal/meta/meta.go internal/meta/dumps.go internal/meta/dumps_test.go internal/meta/destinations.go
git commit -m "feat(meta): dumps.dest_id column with destination name join"
```

---

### Task 3: meta — migrasi config S3 lama

**Files:**
- Modify: `internal/meta/destinations.go`
- Test: `internal/meta/destinations_test.go`

**Interfaces (Produces):** `MigrateLegacyS3(ctx) (bool, error)` — true jika migrasi jalan. Secret lama (`s3_secret_enc`, sudah ciphertext) dipindah apa adanya — tanpa decrypt/encrypt.

- [ ] **Step 1: Tulis test (failing)** — tambahkan ke `destinations_test.go`:

```go
func TestMigrateLegacyS3(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	st.SetSetting(ctx, "s3_endpoint", "http://127.0.0.1:9000")
	st.SetSetting(ctx, "s3_region", "us-east-1")
	st.SetSetting(ctx, "s3_bucket", "oldbucket")
	st.SetSetting(ctx, "s3_prefix", "pre")
	st.SetSetting(ctx, "s3_access_key", "AK")
	st.SetSetting(ctx, "s3_secret_enc", "ENCRYPTED")
	db := sampleDB()
	dbID, _ := st.CreateDatabase(ctx, &db)
	st.CreateDump(ctx, &Dump{DatabaseID: dbID, Engine: "postgres", Label: "old", Storage: "s3", Status: "ready", CreatedBy: 1})
	st.CreateDump(ctx, &Dump{DatabaseID: dbID, Engine: "postgres", Label: "loc", Storage: "local", Status: "ready", CreatedBy: 1})

	migrated, err := st.MigrateLegacyS3(ctx)
	if err != nil || !migrated {
		t.Fatalf("migrated = %v, %v", migrated, err)
	}
	dest, err := st.GetDestinationByName(ctx, "oldbucket")
	if err != nil {
		t.Fatal(err)
	}
	if dest.Endpoint != "http://127.0.0.1:9000" || dest.Region != "us-east-1" || dest.Prefix != "pre" ||
		dest.AccessKey != "AK" || dest.SecretEnc != "ENCRYPTED" || dest.Kind != "s3" {
		t.Fatalf("dest = %+v", dest)
	}
	rows, _ := st.ListDumps(ctx)
	for _, r := range rows {
		want := dest.ID
		if r.Label == "loc" {
			want = 0
		}
		if r.DestID != want {
			t.Fatalf("dump %s dest = %d, want %d", r.Label, r.DestID, want)
		}
	}
	for _, k := range []string{"s3_endpoint", "s3_region", "s3_bucket", "s3_prefix", "s3_access_key", "s3_secret_enc"} {
		if _, ok, _ := st.GetSetting(ctx, k); ok {
			t.Fatalf("setting %s not deleted", k)
		}
	}
	if again, err := st.MigrateLegacyS3(ctx); err != nil || again {
		t.Fatalf("second run = %v, %v", again, err)
	}
}
```

- [ ] **Step 2: Jalankan, pastikan gagal**

Run: `go test ./internal/meta/ -run TestMigrateLegacyS3`
Expected: FAIL — `MigrateLegacyS3 undefined`

- [ ] **Step 3: Implementasi** — tambahkan ke `internal/meta/destinations.go`:

```go
// MigrateLegacyS3 moves the pre-multi-destination single S3 settings into a
// storage destination. Runs inside one transaction; a failure leaves the
// trigger key (s3_endpoint) in place so the next startup retries.
func (s *Store) MigrateLegacyS3(ctx context.Context) (bool, error) {
	ep, ok, err := s.GetSetting(ctx, "s3_endpoint")
	if err != nil || !ok {
		return false, err
	}
	get := func(k string) string {
		v, _, _ := s.GetSetting(ctx, k)
		return v
	}
	name := get("s3_bucket")
	if name == "" {
		name = "s3"
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `INSERT INTO storage_destinations
		(name, kind, endpoint, region, bucket, prefix, access_key, secret_enc)
		VALUES (?, 's3', ?, ?, ?, ?, ?, ?)`,
		name, ep, get("s3_region"), name, get("s3_prefix"), get("s3_access_key"), get("s3_secret_enc"))
	if err != nil {
		return false, err
	}
	destID, err := res.LastInsertId()
	if err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE dumps SET dest_id = ? WHERE storage = 's3'`, destID); err != nil {
		return false, err
	}
	for _, k := range []string{"s3_region", "s3_bucket", "s3_prefix", "s3_access_key", "s3_secret_enc", "s3_endpoint"} {
		if _, err := tx.ExecContext(ctx, `DELETE FROM settings WHERE key = ?`, k); err != nil {
			return false, err
		}
	}
	return true, tx.Commit()
}
```

Catatan: kalau sudah ada destination dengan nama sama seperti bucket lama, INSERT gagal → rollback → startup gagal dengan error jelas. Kasus ini nyaris mustahil (fitur destination belum pernah ada sebelum migrasi).

- [ ] **Step 4: Jalankan test**

Run: `go test ./internal/meta/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/meta/destinations.go internal/meta/destinations_test.go
git commit -m "feat(meta): migrate legacy single-s3 settings into a destination"
```

---

### Task 4: runner — resolusi store per destination id

**Files:**
- Modify: `internal/runner/runner.go`, `internal/runner/runner_test.go`

**Interfaces (Consumes):** `Dump.DestID` (Task 2). **Produces:** `runner.New(st, engines, newStore, logsDir)` dengan `newStore func(ctx context.Context, destID int64) (storage.Store, error)`.

- [ ] **Step 1: Update test helper (failing compile)** — di `internal/runner/runner_test.go`:

`setup()` ganti newStore:

```go
	newStore := func(ctx context.Context, destID int64) (storage.Store, error) {
		if destID != 0 {
			return nil, fmt.Errorf("storage destination %d not configured", destID)
		}
		return storage.NewLocalFS(dumpsDir)
	}
```

`seedDumpJob()`: hapus baris `storageKind := "local"` dan `Storage: &storageKind` pada `CreateJob` (Job tanpa Storage).

`TestDumpJobFailureAfterDumpMarksDumpFailed`: ganti pembuatan dump & job:

```go
	dumpID, err := st.CreateDump(ctx, &meta.Dump{DatabaseID: dbID, Engine: "postgres", Label: "l", Storage: "s3", DestID: 99, SourceDB: "appdb", Status: "pending", CreatedBy: 1})
	if err != nil {
		t.Fatal(err)
	}
	jobID, err := st.CreateJob(ctx, &meta.Job{Type: "dump", DatabaseID: dbID, DumpID: &dumpID})
```

Tujuan: dump sukses di-engine tapi store tidak tersedia (`destID 99`) → job failed dan dump ditandai failed (path `UpdateDumpResult` failed tetap teruji).

- [ ] **Step 2: Jalankan, pastikan gagal compile**

Run: `go test ./internal/runner/`
Expected: FAIL — signature `newStore` mismatch

- [ ] **Step 3: Implementasi** — `internal/runner/runner.go`:

Field & constructor:

```go
type Runner struct {
	st       *meta.Store
	engines  map[string]engine.Engine
	newStore func(ctx context.Context, destID int64) (storage.Store, error)
	logsDir  string
	mu       sync.Mutex
	cancels  map[int64]context.CancelFunc
	started  map[int64]bool
}

func New(st *meta.Store, engines map[string]engine.Engine,
	newStore func(ctx context.Context, destID int64) (storage.Store, error), logsDir string) (*Runner, error) {
```

`runDump` — muat dump di awal dan resolve store dari `dump.DestID`:

```go
func (r *Runner) runDump(ctx context.Context, job *meta.Job, db *meta.Database, eng engine.Engine, log io.Writer) (err error) {
	dump, err := r.st.GetDump(ctx, *job.DumpID)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp("", "kudump-*.dump")
	// ... (sisa body sama seperti sebelumnya sampai `if err = tmp.Close(); err != nil`)
	store, err := r.newStore(ctx, dump.DestID)
	// ... sisa sama
}
```

`runRestore` — ganti `store, err := r.storeFor(&dump.Storage)` menjadi:

```go
	store, err := r.newStore(ctx, dump.DestID)
```

Hapus fungsi `storeFor` sepenuhnya.

- [ ] **Step 4: Jalankan test runner**

Run: `go test ./internal/runner/`
Expected: PASS semua, termasuk `TestDumpJobFailureAfterDumpMarksDumpFailed`

- [ ] **Step 5: Commit**

```bash
git add internal/runner/runner.go internal/runner/runner_test.go
git commit -m "feat(runner): resolve dump store by destination id"
```

---

### Task 5: api — endpoint destinations + wiring main.go

**Files:**
- Create: `internal/api/destinations.go`, `internal/api/destinations_test.go`
- Modify: `internal/api/server.go` (routes), `internal/api/server_test.go` (fake), `cmd/ku-dump/main.go`
- Modify: `internal/api/settings.go` (buang semua S3, sisakan `handleTools`)
- Delete: `internal/api/settings_test.go` (test route lama)

**Interfaces (Consumes):** meta CRUD Task 1, `CountDumpsForDestination` Task 2. **Produces:** `api.Deps.NewStore func(ctx context.Context, destID int64) (storage.Store, error)`; routes di bawah.

- [ ] **Step 1: Tulis test (failing)** — `internal/api/destinations_test.go`:

```go
package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

func destPayload() map[string]any {
	return map[string]any{"name": "minio", "endpoint": "http://127.0.0.1:9000", "region": "us-east-1",
		"bucket": "ku", "prefix": "pre", "accessKey": "ak", "secretKey": "sk"}
}

func createDest(t *testing.T, h http.Handler, cookie *http.Cookie, name string) string {
	t.Helper()
	p := destPayload()
	p["name"] = name
	rec := doJSON(t, h, "POST", "/api/storage/destinations", p, cookie)
	if rec.Code != 201 {
		t.Fatalf("create dest = %d: %s", rec.Code, rec.Body.String())
	}
	var d destinationDTO
	json.NewDecoder(rec.Body).Decode(&d)
	return d.ID
}

func TestDestinationCRUD(t *testing.T) {
	h := newTestServer(t)
	cookie := setupAndLogin(t, h)
	rec := doJSON(t, h, "GET", "/api/storage/destinations", nil, nil)
	if rec.Code != 401 {
		t.Fatalf("unauth = %d", rec.Code)
	}
	id := createDest(t, h, cookie, "minio")
	rec = doJSON(t, h, "GET", "/api/storage/destinations", nil, cookie)
	var list []destinationDTO
	json.NewDecoder(rec.Body).Decode(&list)
	if len(list) != 1 || list[0].ID != id || !list[0].SecretSet || list[0].Kind != "s3" {
		t.Fatalf("list = %+v", list)
	}
	upd := destPayload()
	upd["name"] = "minio"
	upd["prefix"] = "other"
	delete(upd, "secretKey")
	rec = doJSON(t, h, "PUT", "/api/storage/destinations/"+id, upd, cookie)
	if rec.Code != 200 {
		t.Fatalf("update = %d: %s", rec.Code, rec.Body.String())
	}
	var got destinationDTO
	json.NewDecoder(rec.Body).Decode(&got)
	if got.Prefix != "other" || !got.SecretSet {
		t.Fatalf("updated = %+v", got)
	}
	rec = doJSON(t, h, "POST", "/api/storage/destinations", destPayload(), cookie)
	if rec.Code != 409 {
		t.Fatalf("duplicate = %d", rec.Code)
	}
	rec = doJSON(t, h, "POST", "/api/storage/destinations", map[string]any{"name": "x"}, cookie)
	if rec.Code != 400 {
		t.Fatalf("validation = %d", rec.Code)
	}
	rec = doJSON(t, h, "DELETE", "/api/storage/destinations/"+id, nil, cookie)
	if rec.Code != 200 {
		t.Fatalf("delete = %d", rec.Code)
	}
	rec = doJSON(t, h, "GET", "/api/storage/destinations", nil, cookie)
	list = nil
	json.NewDecoder(rec.Body).Decode(&list)
	if len(list) != 0 {
		t.Fatalf("list after delete = %+v", list)
	}
}

func TestDestinationDeleteGuard(t *testing.T) {
	h := newTestServer(t)
	cookie := setupAndLogin(t, h)
	id := createDest(t, h, cookie, "minio")
	db := createDatabase(t, h, cookie)
	rec := doJSON(t, h, "POST", "/api/databases/"+db.ID+"/dump", map[string]string{"label": "x", "destId": id}, cookie)
	if rec.Code != 201 {
		t.Fatalf("dump = %d: %s", rec.Code, rec.Body.String())
	}
	var created struct {
		DumpID string `json:"dumpId"`
	}
	json.NewDecoder(rec.Body).Decode(&created)
	d := waitDumpReady(t, h, cookie, created.DumpID)
	if d.Status != "ready" || d.DestName != "minio" || d.DestID != id {
		t.Fatalf("dump = %+v", d)
	}
	rec = doJSON(t, h, "DELETE", "/api/storage/destinations/"+id, nil, cookie)
	if rec.Code != 409 {
		t.Fatalf("delete in-use = %d", rec.Code)
	}
	rec = doJSON(t, h, "DELETE", "/api/dumps/"+d.ID, nil, cookie)
	if rec.Code != 200 {
		t.Fatalf("delete dump = %d", rec.Code)
	}
	rec = doJSON(t, h, "DELETE", "/api/storage/destinations/"+id, nil, cookie)
	if rec.Code != 200 {
		t.Fatalf("delete after dump gone = %d", rec.Code)
	}
}

func TestDestinationTestUnreachable(t *testing.T) {
	h := newTestServer(t)
	cookie := setupAndLogin(t, h)
	rec := doJSON(t, h, "POST", "/api/storage/destinations/test", map[string]any{
		"name": "t", "endpoint": "http://127.0.0.1:1", "bucket": "b",
		"accessKey": "a", "secretKey": "s",
	}, cookie)
	if rec.Code != 200 {
		t.Fatalf("code = %d", rec.Code)
	}
	var res struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	json.NewDecoder(rec.Body).Decode(&res)
	if res.OK || res.Error == "" {
		t.Fatalf("res = %+v", res)
	}
}
```

Catatan: test ini butuh `DumpDTO.DestID/DestName` dan `destId` di dump — datang dari Task 6. Kerjakan Task 5 dan 6 sebelum menjalankan test penuh, atau jalankan test ini hanya setelah Task 6. Eksekusi aman: selesaikan implementasi Task 5 (compile OK, test destinations skip dulu), lalu Task 6, lalu jalankan semua.

- [ ] **Step 2: Implementasi endpoint** — buat `internal/api/destinations.go`:

```go
package api

import (
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/luthfi9251/ku-dump/internal/meta"
	"github.com/luthfi9251/ku-dump/internal/storage"
)

type destinationPayload struct {
	Name      string `json:"name"`
	Endpoint  string `json:"endpoint"`
	Region    string `json:"region"`
	Bucket    string `json:"bucket"`
	Prefix    string `json:"prefix"`
	AccessKey string `json:"accessKey"`
	SecretKey string `json:"secretKey"`
}

func (p destinationPayload) validate(requireSecret bool) error {
	if strings.TrimSpace(p.Name) == "" || p.Endpoint == "" || p.Bucket == "" || p.AccessKey == "" {
		return errors.New("name, endpoint, bucket and accessKey are required")
	}
	if requireSecret && p.SecretKey == "" {
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
	CreatedAt string `json:"createdAt"`
}

func destDTO(d *meta.Destination) destinationDTO {
	return destinationDTO{
		ID: encID(d.ID), Name: d.Name, Kind: d.Kind, Endpoint: d.Endpoint,
		Region: d.Region, Bucket: d.Bucket, Prefix: d.Prefix, AccessKey: d.AccessKey,
		SecretSet: d.SecretEnc != "", CreatedAt: d.CreatedAt.Format(time.RFC3339),
	}
}

func (s *Server) handleListDestinations(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Store.ListDestinations(r.Context())
	if err != nil {
		log.Printf("list destinations: %v", err)
		fail(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	out := make([]destinationDTO, 0, len(rows))
	for i := range rows {
		out = append(out, destDTO(&rows[i]))
	}
	jsonOut(w, http.StatusOK, out)
}

func (s *Server) destNameFree(ctx context.Context, name string, excludeID int64) bool {
	existing, err := s.Store.GetDestinationByName(ctx, name)
	if err != nil {
		return true
	}
	return existing.ID == excludeID
}

func (s *Server) handleCreateDestination(w http.ResponseWriter, r *http.Request) {
	var p destinationPayload
	if !decodeBody(w, r, &p) {
		return
	}
	if err := p.validate(true); err != nil {
		fail(w, http.StatusBadRequest, "VALIDATION", err.Error())
		return
	}
	if !s.destNameFree(r.Context(), p.Name, 0) {
		fail(w, http.StatusConflict, "DUPLICATE_NAME", "destination name already used")
		return
	}
	enc, err := s.Crypt.Encrypt(p.SecretKey)
	if err != nil {
		log.Printf("encrypt secret: %v", err)
		fail(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	id, err := s.Store.CreateDestination(r.Context(), &meta.Destination{
		Name: p.Name, Kind: "s3", Endpoint: p.Endpoint, Region: p.Region,
		Bucket: p.Bucket, Prefix: p.Prefix, AccessKey: p.AccessKey, SecretEnc: enc,
	})
	if err != nil {
		log.Printf("create destination: %v", err)
		fail(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	d, _ := s.Store.GetDestination(r.Context(), id)
	jsonOut(w, http.StatusCreated, destDTO(d))
}

func (s *Server) handleUpdateDestination(w http.ResponseWriter, r *http.Request) {
	id, err := decID(r.PathValue("id"))
	if err != nil {
		fail(w, http.StatusNotFound, "NOT_FOUND", "destination not found")
		return
	}
	current, err := s.Store.GetDestination(r.Context(), id)
	if err != nil {
		fail(w, http.StatusNotFound, "NOT_FOUND", "destination not found")
		return
	}
	var p destinationPayload
	if !decodeBody(w, r, &p) {
		return
	}
	if err := p.validate(false); err != nil {
		fail(w, http.StatusBadRequest, "VALIDATION", err.Error())
		return
	}
	if !s.destNameFree(r.Context(), p.Name, id) {
		fail(w, http.StatusConflict, "DUPLICATE_NAME", "destination name already used")
		return
	}
	secretEnc := current.SecretEnc
	if p.SecretKey != "" {
		enc, err := s.Crypt.Encrypt(p.SecretKey)
		if err != nil {
			log.Printf("encrypt secret: %v", err)
			fail(w, http.StatusInternalServerError, "INTERNAL", "internal error")
			return
		}
		secretEnc = enc
	}
	updated := &meta.Destination{
		ID: id, Name: p.Name, Kind: current.Kind, Endpoint: p.Endpoint, Region: p.Region,
		Bucket: p.Bucket, Prefix: p.Prefix, AccessKey: p.AccessKey, SecretEnc: secretEnc,
	}
	if err := s.Store.UpdateDestination(r.Context(), updated); err != nil {
		log.Printf("update destination %d: %v", id, err)
		fail(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	d, _ := s.Store.GetDestination(r.Context(), id)
	jsonOut(w, http.StatusOK, destDTO(d))
}

func (s *Server) handleDeleteDestination(w http.ResponseWriter, r *http.Request) {
	id, err := decID(r.PathValue("id"))
	if err != nil {
		fail(w, http.StatusNotFound, "NOT_FOUND", "destination not found")
		return
	}
	if _, err := s.Store.GetDestination(r.Context(), id); err != nil {
		fail(w, http.StatusNotFound, "NOT_FOUND", "destination not found")
		return
	}
	n, err := s.Store.CountDumpsForDestination(r.Context(), id)
	if err != nil {
		log.Printf("count dumps for destination %d: %v", id, err)
		fail(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	if n > 0 {
		fail(w, http.StatusConflict, "DESTINATION_IN_USE",
			"destination still holds dumps; delete or move them first")
		return
	}
	if err := s.Store.DeleteDestination(r.Context(), id); err != nil {
		log.Printf("delete destination %d: %v", id, err)
		fail(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	jsonOut(w, http.StatusOK, map[string]bool{"ok": true})
}

func s3TestResponse(w http.ResponseWriter, err error) {
	if err != nil {
		jsonOut(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	jsonOut(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleTestDestinationPayload(w http.ResponseWriter, r *http.Request) {
	var p destinationPayload
	if !decodeBody(w, r, &p) {
		return
	}
	if err := p.validate(true); err != nil {
		fail(w, http.StatusBadRequest, "VALIDATION", err.Error())
		return
	}
	s3, err := storage.NewS3(storage.S3Config{
		Endpoint: p.Endpoint, Region: p.Region, Bucket: p.Bucket,
		Prefix: p.Prefix, AccessKey: p.AccessKey, SecretKey: p.SecretKey,
	})
	if err != nil {
		s3TestResponse(w, err)
		return
	}
	s3TestResponse(w, s3.Test(r.Context()))
}

func (s *Server) handleTestDestination(w http.ResponseWriter, r *http.Request) {
	id, err := decID(r.PathValue("id"))
	if err != nil {
		fail(w, http.StatusNotFound, "NOT_FOUND", "destination not found")
		return
	}
	d, err := s.Store.GetDestination(r.Context(), id)
	if err != nil {
		fail(w, http.StatusNotFound, "NOT_FOUND", "destination not found")
		return
	}
	secret, err := s.Crypt.Decrypt(d.SecretEnc)
	if err != nil {
		log.Printf("decrypt secret dest %d: %v", id, err)
		fail(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	s3, err := storage.NewS3(storage.S3Config{
		Endpoint: d.Endpoint, Region: d.Region, Bucket: d.Bucket,
		Prefix: d.Prefix, AccessKey: d.AccessKey, SecretKey: secret,
	})
	if err != nil {
		s3TestResponse(w, err)
		return
	}
	s3TestResponse(w, s3.Test(r.Context()))
}
```

(`context` dibutuhkan di signature `destNameFree` — import sesuai.)

- [ ] **Step 3: Bersihkan settings.go & routes**

`internal/api/settings.go`: hapus konstanta `keyS3*`, `storageSettingsDTO`, `storageSettingsPayload`, `S3ConfigPublic`, `s3Config`, `handleGetStorageSettings`, `handlePutStorageSettings`, `handleTestStorage`, dan import `storage`. Sisakan `handleTools` (pindahkan `handleTools` + import engines ke file ini tetap, atau pindahkan ke server.go).

`internal/api/server.go` — `Deps`:

```go
	NewStore func(ctx context.Context, destID int64) (storage.Store, error)
```

(tambah import `context`), routes: hapus tiga route `/api/settings/storage*`, tambah:

```go
	s.mux.HandleFunc("GET /api/storage/destinations", s.auth(s.handleListDestinations))
	s.mux.HandleFunc("POST /api/storage/destinations", s.auth(s.handleCreateDestination))
	s.mux.HandleFunc("POST /api/storage/destinations/test", s.auth(s.handleTestDestinationPayload))
	s.mux.HandleFunc("PUT /api/storage/destinations/{id}", s.auth(s.handleUpdateDestination))
	s.mux.HandleFunc("POST /api/storage/destinations/{id}/test", s.auth(s.handleTestDestination))
	s.mux.HandleFunc("DELETE /api/storage/destinations/{id}", s.auth(s.handleDeleteDestination))
```

- [ ] **Step 4: Update fake test & main.go**

`internal/api/server_test.go` `newTestServer`:

```go
	newStore := func(ctx context.Context, destID int64) (storage.Store, error) {
		if destID == 0 {
			return storage.NewLocalFS(filepath.Join(dir, "dumps"))
		}
		return nil, fmt.Errorf("storage destination %d not configured", destID)
	}
```

(import `context`/`fmt` sesuaikan; `storage.S3Config` tidak lagi dipakai di sini.)

`cmd/ku-dump/main.go` — setelah `meta.Open` + session secret, panggil migrasi:

```go
	if migrated, err := st.MigrateLegacyS3(ctx); err != nil {
		log.Fatal(err)
	} else if migrated {
		log.Printf("migrated legacy S3 settings into storage destination")
	}
```

Ganti factory:

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
		if err != nil {
			return nil, fmt.Errorf("decrypt destination secret: %w", err)
		}
		return storage.NewS3(storage.S3Config{
			Endpoint: d.Endpoint, Region: d.Region, Bucket: d.Bucket,
			Prefix: d.Prefix, AccessKey: d.AccessKey, SecretKey: secret,
		})
	}
```

(hack `api.Server{Deps: ...}` dan import `api` di main.go dihapus).

- [ ] **Step 5: Hapus test lama settings storage, compile**

```bash
git rm internal/api/settings_test.go
go build ./... && go vet ./...
```

Expected: compile OK. (Test destinations di server_test hijau kecuali yang butuh Task 6.)

- [ ] **Step 6: Commit**

```bash
git add -A internal/api cmd/ku-dump
git commit -m "feat(api): storage destination CRUD endpoints, legacy settings routes removed"
```

---

### Task 6: api — dump memakai destId

**Files:**
- Modify: `internal/api/dumps.go`, `internal/api/upload.go`, `internal/api/dumps_test.go`

**Interfaces (Consumes):** `NewStore(ctx, destID)` (Task 5), `GetDestination` (Task 1).

- [ ] **Step 1: Update test (failing)** — `internal/api/dumps_test.go`:

- `TestDumpFlow` & `TestDumpDefaultLabel`: body `"storage": "local"` → `"destId": ""`.
- `TestDumpValidation`: kasus `"storage": "ftp"` → `"destId": "!!!"` (400 VALIDATION); kasus `"storage": "s3"` → `"destId": encID(999)` (400 STORAGE_NOT_CONFIGURED).
- `TestDumpFlow` assertion tambahan setelah ready: `d.DestName != "local" || d.DestID != ""` → fail.

```go
	rec := doJSON(t, h, "POST", "/api/databases/"+db.ID+"/dump",
		map[string]string{"label": "nightly", "destId": ""}, cookie)
```

```go
	rec = doJSON(t, h, "POST", "/api/databases/"+db.ID+"/dump",
		map[string]string{"label": "x", "destId": "!!!"}, cookie)
	if rec.Code != 400 {
		t.Fatalf("bad destId = %d", rec.Code)
	}
	rec = doJSON(t, h, "POST", "/api/databases/"+db.ID+"/dump",
		map[string]string{"label": "x", "destId": encID(999)}, cookie)
	if rec.Code != 400 {
		t.Fatalf("unknown destId = %d", rec.Code)
	}
```

- [ ] **Step 2: Jalankan, pastikan gagal**

Run: `go test ./internal/api/ -run 'TestDump'`
Expected: FAIL (field DestID/DestName belum ada; payload lama tak dikenal)

- [ ] **Step 3: Implementasi `internal/api/dumps.go`**

`dumpDTO`: ganti `Storage string json:"storage"` dengan:

```go
	DestID   string `json:"destId"`
	DestName string `json:"destName"`
```

`dumpToDTO`:

```go
		DestID:   encIDOrEmpty(r.DestID),
		DestName: r.DestName,
```

`handleCreateDump` req & resolusi:

```go
	var req struct {
		Label  string `json:"label"`
		DestID string `json:"destId"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	destID := int64(0)
	storageKind := "local"
	if req.DestID != "" {
		var err error
		destID, err = decID(req.DestID)
		if err != nil {
			fail(w, http.StatusBadRequest, "VALIDATION", "invalid destId")
			return
		}
		dest, err := s.Store.GetDestination(r.Context(), destID)
		if err != nil {
			fail(w, http.StatusBadRequest, "STORAGE_NOT_CONFIGURED", "storage destination not found")
			return
		}
		storageKind = dest.Kind
	}
	if _, err := s.NewStore(r.Context(), destID); err != nil {
		fail(w, http.StatusBadRequest, "STORAGE_NOT_CONFIGURED", "selected storage is not available: "+err.Error())
		return
	}
```

`CreateDump`:

```go
	dumpID, err := s.Store.CreateDump(r.Context(), &meta.Dump{
		DatabaseID: db.ID, Engine: db.Engine, Label: label, Storage: storageKind,
		DestID: destID, SourceDB: db.DBName, Status: "pending", CreatedBy: userID(r),
	})
```

`CreateJobGuarded`: buang `storage := req.Storage; &storage` → `&meta.Job{Type: "dump", DatabaseID: db.ID, DumpID: &dumpID}`.

`internal/api/upload.go`: `s.NewStore("local")` → `s.NewStore(r.Context(), 0)`; `CreateDump` tetap `Storage: "local"` (DestID 0 default).

- [ ] **Step 4: Jalankan semua unit test**

Run: `go test ./...`
Expected: PASS semua package (termasuk `TestDestinationDeleteGuard` dari Task 5)

- [ ] **Step 5: Commit**

```bash
git add internal/api/dumps.go internal/api/dumps_test.go internal/api/upload.go
git commit -m "feat(api): dump requests select a storage destination"
```

---

## Verification Chunk 1

```bash
go build ./... && go vet ./... && go test ./...
```

Backend selesai. SPA masih memanggil route lama (akan 404) — lanjut Chunk 2 sebelum release.
