# ku-dump v1 — Design

Date: 2026-08-31
Status: Approved (design review via brainstorming session)

## Ringkasan

ku-dump adalah aplikasi web untuk mempermudah dump dan restore database
**PostgreSQL** dan **MongoDB**. User mendaftarkan database secara runtime,
lalu memilih operasi dump atau restore dari UI. Stack: Go (backend, single
binary) + React (SPA, di-embed). Pola arsitektur mengikuti ku-crud: satu
proses serving API + SPA, metadata di SQLite.

Karena dump/restore dieksekusi via CLI tool resmi (`pg_dump`/`pg_restore`,
`mongodump`/`mongorestore`), host yang menjalankan ku-dump wajib memiliki
tool tersebut terinstall. Aplikasi mendeteksi ketersediaannya dan
menampilkannya di UI.

## Goals (v1)

1. Register/manage banyak database secara runtime (postgres & mongodb)
2. Dump database pilihan → disimpan ke **local storage server** ATAU
   **S3-compatible cloud** (dipilih user saat dump)
3. Restore dari riwayat dump ATAU dari file upload user
4. Restore dapat ditujukan ke database terdaftar mana pun dengan engine
   yang sama (mis. copy prod → dev)
5. Job berjalan asynchronous dengan log yang bisa dipantau live, dan bisa
   di-cancel
6. Simple login (user di SQLite, tanpa role) untuk memproteksi aplikasi

## Non-goals (v1)

- Scheduling / cron dump otomatis (kandidat v2)
- Dump incremental / point-in-time recovery
- MySQL dan engine lain
- Multi-user dengan role & permission granular
- Restore lintas engine (mongo → postgres, dsb.)

## Arsitektur

```
┌─────────────────────────────────────────────────┐
│              Binary ku-dump (:8080)             │
│  ┌───────────────┐  ┌────────────────────────┐  │
│  │  React SPA    │  │  Go API  /api/*        │  │
│  │  (embed.FS)   │←→│  auth · CRUD · jobs    │  │
│  └───────────────┘  └───────┬────────────────┘  │
│                             │                   │
│  ┌──────────────────────────┴────────────────┐  │
│  │            Job Runner (goroutine)         │  │
│  │  spawn pg_dump/pg_restore/                │  │
│  │  mongodump/mongorestore (exec.Command)    │  │
│  └──────┬──────────────────────┬─────────────┘  │
│         │                      │                │
│  ┌──────┴──────┐   ┌───────────┴────────────┐   │
│  │ SQLite meta │   │ Storage (interface)    │   │
│  │ users, dbs, │   │ ├ LocalFS (./dumps)    │   │
│  │ dumps, jobs │   │ └ S3-compatible        │   │
│  └─────────────┘   └────────────────────────┘   │
└─────────────────────────────────────────────────┘
```

Satu proses Go. Job dump/restore berjalan sebagai goroutine in-process
yang men-spawn CLI tool via `exec.CommandContext`. Jika proses mati saat
job berjalan, job di-mark `failed` ("interrupted by restart") saat
startup — acceptable untuk operasi manual.

### Package layout

| Package | Tanggung jawab |
|---|---|
| `cmd/` | entrypoint: flag/env config, serve |
| `internal/api` | HTTP handlers, middleware auth, routing |
| `internal/meta` | SQLite store: users, databases, dumps, jobs, settings + migrasi skema |
| `internal/engine` | interface `Engine`; implementasi `postgres`, `mongodb`: membangun argumen CLI, parse stderr, test koneksi |
| `internal/runner` | job queue in-process: lifecycle, guard 1 job aktif per database, cancel via context |
| `internal/storage` | interface `Store` (`Put/Get/Delete/List`); implementasi `localfs`, `s3` |
| `web/` | React SPA (Vite + TS + Tailwind), di-build lalu di-embed via `embed.FS` |

### Keputusan teknis

- **Format dump**: `pg_dump` custom format (`-Fc`) → satu file compressed;
  `mongodump --archive --gzip` → satu file archive compressed. Semua dump
  berbentuk **satu file** sehingga penanganan local/S3 seragam.
- **Deteksi CLI tool**: `exec.LookPath` saat startup dan saat register DB;
  UI menampilkan tool mana yang missing per engine.
- **Restore flags**: postgres memakai `pg_restore --clean --if-exists`
  (drop object dulu), mongodb memakai `mongorestore --drop`.
- **Enkripsi password**: AES-256-GCM untuk password database dan S3
  credentials. Key dari env `KUDUMP_CRYPT_KEY`; jika tidak di-set,
  auto-generate sekali dan disimpan di `.ku-dump-key` (mode 0600).
- **Config**: env/flag — `KUDUMP_ADDR` (:8080), `KUDUMP_DB` (path sqlite,
  default `./ku-dump.db`), `KUDUMP_DUMPS_DIR` (default `./dumps`),
  `KUDUMP_CRYPT_KEY`.

## Data model (SQLite)

```sql
users     (id, username UNIQUE, password_hash, created_at)
databases (id, name UNIQUE, engine /*postgres|mongodb*/, host, port,
           db_name, username, password_encrypted, options,
           created_at)
dumps     (id, database_id → databases, engine, label,
           storage /*local|s3*/, location /*path atau s3 key*/,
           size_bytes, status /*ready|uploaded*/, created_by → users,
           created_at)
jobs      (id, type /*dump|restore*/, database_id, dump_id nullable,
           status /*pending|running|success|failed|cancelled*/,
           log_path, error, started_at, finished_at, created_at)
settings  (key PRIMARY KEY, value)   -- konfigurasi S3, dsb.
```

Catatan:
- `dumps.status`: `ready` = hasil job dump; `uploaded` = hasil upload user
  untuk restore. Engine file upload ditentukan saat upload: user memilih
  engine di form upload, dan backend memverifikasi magic byte untuk
  postgres (`PGDMP` header pada format `-Fc`); file mongo archive adalah
  gzip — jika pilihan user bertentangan dengan deteksi, upload ditolak.
- `databases.options`: JSON bebas per engine (mis. sslmode postgres,
  replicaSet/authSource mongo).
- Menghapus database hanya menghapus definisi; file dump tetap ada di
  storage (dump tetap ter-list, kolom database menampilkan nama yang
  dihapus).

## API

| Method | Path | Fungsi |
|---|---|---|
| POST | `/api/auth/login` , `/api/auth/logout` | session cookie |
| GET | `/api/databases` | list + status koneksi terakhir + ketersediaan CLI tool |
| POST | `/api/databases` | register; validasi koneksi + CLI tool engine tersedia |
| PUT/DELETE | `/api/databases/{id}` | edit / hapus definisi |
| POST | `/api/databases/{id}/test` | test koneksi on-demand |
| POST | `/api/databases/{id}/dump` | buat job dump; body `{label, storage}` |
| GET | `/api/dumps` | riwayat dump (filter database/engine) |
| GET | `/api/dumps/{id}/download` | stream file dump ke browser |
| DELETE | `/api/dumps/{id}` | hapus metadata + file (local/S3) |
| POST | `/api/restores` | job restore; body `{dumpId, targetDatabaseId, confirmName}` ATAU `{uploadId, targetDatabaseId, confirmName}` |
| POST | `/api/restores/upload` | multipart upload file dump → jadi dump `status=uploaded` |
| GET | `/api/jobs` , `/api/jobs/{id}` | status job + log tail |
| POST | `/api/jobs/{id}/cancel` | kill process + context cancel |
| GET/PUT | `/api/settings/storage` | konfigurasi S3 + test koneksi |

Aturan penting:
- Validasi `engine(dump) == engine(target database)` — restore lintas
  engine ditolak.
- Backend memvalidasi `confirmName == targetDb.name` (konfirmasi
  destruktif tidak hanya di UI).
- Guard: tidak boleh ada 2 job aktif untuk database yang sama
  (`pending|running` di-check saat create job).

## Alur operasi

### Dump
1. User pilih database → Dump → isi label + pilih storage (local/S3)
2. POST `/api/databases/{id}/dump` → validasi guard → job `pending`
3. Runner: spawn CLI tool, stdout/stderr di-append ke `jobs.log_path`
4. File hasil dipindah dari temp ke storage terpilih → row `dumps`
   dibuat (status `ready`) → job `success`
5. UI memantau via polling `GET /api/jobs/{id}` (2s)

### Restore
1. User pilih sumber: riwayat dump ATAU upload file
2. Pilih target database (UI hanya menampilkan engine sama;
   cross-engine disabled dengan keterangan)
3. Konfirmasi 2 langkah + ketik nama database target
4. POST `/api/restores` → job restore → file diambil dari storage/lokasi
   upload ke temp → CLI restore dijalankan (`--clean`/`--drop`)
5. Log live & cancel tersedia sama seperti dump

## Frontend

Stack: Vite + TypeScript + Tailwind, React Query untuk polling jobs.
Tanpa state library berat.

| Halaman | Isi |
|---|---|
| `/login` | form login |
| `/databases` | halaman utama: card database (nama, engine badge, host:port, status koneksi); aksi per card: Dump, Restore, Test Connection, edit, hapus; tombol Add Database → modal field-set per engine dengan test koneksi sebelum simpan |
| `/dumps` | riwayat dump: tabel (database, engine, label, storage, size, waktu, creator); filter; aksi Download / Restore / Hapus |
| `/jobs` | daftar job + status badge; klik → drawer log live; tombol Cancel saat running |
| `/settings` | konfigurasi S3 + tombol test; info CLI tool terdeteksi/missing |

UX kunci:
- Modal Dump kecil (label + storage) → toast berisi link ke drawer log
- Modal Restore: tab sumber (Riwayat / Upload), pilih target, konfirmasi
  ketik nama target
- Tombol Dump/Restore disabled + status "job sedang berjalan" bila ada
  job aktif untuk database tersebut

## Error handling

- Job gagal (exit ≠ 0): job `failed`; 50 baris terakhir stderr disimpan di
  `jobs.error`; log lengkap tetap di file log — UI menampilkan keduanya
- Crash saat job running: saat startup, job `running` → `failed`
  ("interrupted by restart")
- CLI tool missing: job ditolak saat create (bukan gagal di tengah);
  register DB juga mem-validasi
- S3 error saat menyimpan hasil dump: job `failed` dengan pesan jelas;
  file temp lokal tetap dipertahankan untuk penyelamatan manual
- Error API: JSON `{error: "CODE", message: "..."}` (pola ku-crud)

## Keamanan

- Password DB & S3 credentials di-encrypt AES-256-GCM di SQLite
- Session cookie HttpOnly + expiry; rate limit login 5 gagal / 15 menit
  per username+IP
- ID eksternal opaque (base64), auto-increment tidak diekspos
- Upload dibatasi ukuran (default 2GB) dan nama file output di-generate
  (`{engine}/{dbSlug}/{timestamp}.dump`) → anti path-traversal
- Restore destruktif divalidasi backend via `confirmName`

## Testing

- **Unit**: engine arg-builder (argumen CLI per engine), storage localfs &
  s3 (mock/minio), meta store, crypto
- **Integration**: docker-compose (postgres + mongo) + CLI tool asli di CI
  image; test end-to-end dump → restore
- **Frontend**: build + tsc saja di v1

## Out of scope untuk plan implementasi pertama

Semua non-goals di atas; struktur kode disiapkan agar scheduling (v2)
tinggal menambah trigger job, bukan refactor.
