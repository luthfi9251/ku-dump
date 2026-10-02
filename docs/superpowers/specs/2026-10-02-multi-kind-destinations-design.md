# Multi-Kind Destinations (Local, S3, SFTP) — Design

Date: 2026-10-02
Status: Approved (brainstorming session)

## Ringkasan

Saat ini `storage_destinations` hanya mendukung S3, dikelola dari halaman
Settings, dan local storage bersifat implisit (satu folder `KUDUMP_DUMPS_DIR`,
destination id `0`). Fitur ini mengubahnya menjadi:

1. **Halaman Destinations tersendiri** (`/destinations`) untuk mengelola
   semua tipe destination; Settings hanya menyisakan Tools.
2. **Tiga tipe destination** yang sama-sama reusable (bisa banyak row,
   tinggal di-assign ke workflow):
   - **s3** — seperti sekarang (endpoint/bucket/credentials/prefix).
   - **local** — folder di disk yang sama, path absolute bebas
     (mis. `/var/backups/pg`, `/mnt/nas/dumps`).
   - **sftp** — kirim file ke server lain via SFTP/SSH (pure Go, tanpa
     binary eksternal), auth password atau private key.
3. **Perbaikan UX RestoreModal**: pemilihan dump yang ada tetap, tapi
   diperjelas — filter per destination, opsi dikelompokkan per destination,
   label menampilkan nama file (location), ukuran, dan tanggal.

Local default (`destID = 0` → `KUDUMP_DUMPS_DIR`) tetap ada dan tidak
bisa dihapus, demi kompatibilitas data lama.

## Keputusan Desain

1. **Satu tabel `storage_destinations`, kolom spesifik per tipe
   (nullable)** — bukan kolom JSON, bukan tabel per tipe. Mengikuti pola
   kolom diskrit + `secret_enc` terenkripsi yang sudah ada.
2. **`storage.Store` interface tidak berubah** (`Kind/Put/Open/Delete`).
   Tiap tipe = satu implementasi: `LocalFS` (sudah ada, param root),
   `S3` (sudah ada), `SFTP` (baru).
3. **SFTP dipilih atas rsync/HTTP** (keputusan user): pure Go
   (`golang.org/x/crypto/ssh` + `github.com/pkg/sftp`), put/fetch balik
   untuk restore, test koneksi mudah. Koneksi SSH dibuka per operasi
   (Put/Open/Delete) — frekuensi backup rendah, tidak perlu pool.
4. **Path local bebas (absolute)** (keputusan user): divalidasi saat
   simpan — harus absolute, `MkdirAll` jika belum ada, dan uji writable
   dengan probe file (tulis-baca-hapus). ku-dump single-admin; operator
   sudah mengontrol host.
5. **Anti-timpa & key layout tidak berubah**: `storage.BuildKey`
   (folder + filename pattern per workflow, timestamp otomatis) berlaku
   untuk ketiga tipe; `Put` tetap fail-if-exists.
6. **Restore dari tipe apa pun** lewat `Store.Open` — SFTP meng-stream
   file dari server remote. Tidak ada copy lokal permanen.
7. **Delete guard tetap**: destination yang masih dipakai dump/workflow
   tidak bisa dihapus (409 `DESTINATION_IN_USE`), berlaku untuk semua tipe.
8. **UX restore diperbaiki, bukan diganti ulang** (keputusan user):
   dropdown dump ada tapi kurang jelas.

## Skema

Tabel `storage_destinations` di-rebuild karena `CHECK (kind IN ('s3'))`
tidak bisa di-ALTER di SQLite. Rebuild idempoten: cek
`sqlite_master.sql`; jika belum memuat `'sftp'`, jalankan sekali
(create new → insert select → drop → rename):

```sql
CREATE TABLE storage_destinations_new (
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

- `secret_enc` dipakai ulang: S3 secret key, SFTP password, atau SFTP
  private key (terenkripsi AES-256-GCM seperti sekarang).
- Kolom lama jadi `NOT NULL DEFAULT ''` agar insert-select aman.
- Tabel lain (dumps, dump_workflows, jobs) tidak berubah.

## Storage Layer

```
storage.Store (tidak berubah)
├── LocalFS  {root}                    — local & local default
├── S3      {S3Config}                 — s3
└── SFTP    {SFTPConfig}               — sftp (baru)
```

`SFTPConfig {Host, Port, Username, AuthType password|key, Secret,
RemoteDir}`:

- `Put`: dial SSH → sftp client → `Create(RemoteDir/key)` → `io.Copy`
  dari file lokal → chmod 0644.
- `Open`: dial → `Open(RemoteDir/key)` → dibungkus `io.ReadCloser` yang
  menutup sftp client + koneksi ssh saat `Close()`.
- `Delete`: `Remove(RemoteDir/key)`; tolerir `NotExist` seperti LocalFS.
- `Test`: dial + tulis-hapus probe file di RemoteDir (MkdirAll bila
  belum ada).
- Key path: join sanitasi (`path.Join`, tolak `..`) — gaya
  `LocalFS.path`.

Factory `newStore` di `main.go` switch per `dest.Kind`:
`s3 → NewS3`, `local → NewLocalFS(RootPath)`, `sftp → NewSFTP`.

## API

Endpoint sama dengan sekarang (`/api/storage/destinations*`), payload
diperluas:

```json
{
  "name": "...", "kind": "s3|local|sftp",
  "endpoint": "...", "region": "...", "bucket": "...", "prefix": "...",
  "accessKey": "...", "secretKey": "...",
  "rootPath": "/var/backups/pg",
  "host": "backup.example.com", "port": 22, "username": "backup",
  "authType": "password|key", "secretKey": "<password atau private key>",
  "remoteDir": "/srv/dumps"
}
```

Validasi per kind:

- **s3** — seperti sekarang (endpoint, bucket, accessKey wajib; secret
  wajib saat create, opsional saat edit).
- **local** — nama + `rootPath` wajib; `filepath.IsAbs`; `MkdirAll` +
  probe tulis saat create/update/test.
- **sftp** — nama, host, username, authType wajib; port default 22;
  secret (password/key) wajib saat create, opsional saat edit;
  `remoteDir` opsional (default `.`).

`POST /api/storage/destinations/test` (payload) dan
`POST /api/storage/destinations/{id}/test` switch per kind: s3 =
BucketExists, local = probe file, sftp = dial + probe file. Respons
`{ok, error?}` seperti sekarang.

DTO menampilkan semua field non-rahasia + `secretSet` boolean.

## Frontend

- **Halaman `/destinations`** + item nav baru. Tabel/kartu daftar semua
  destination dengan badge tipe (Cloud = s3, HardDrive = local, Server =
  sftp) dan ringkasan field utama per tipe. Modal create/edit: pemilih
  tipe (3 kartu, gaya WorkflowModal) → field dinamis per tipe → tombol
  **Test** sebelum simpan.
- **Settings**: bagian Destinations dipindah ke halaman baru; Settings
  menyisakan Tools (link ke halaman Destinations).
- **WorkflowModal**: grid destination menampilkan semua tipe dengan
  ikon; opsi "Local Storage (default)" tetap paling atas (`destId = ''`).
- **Dumps**: filter storage berisi semua destination (ikon tipe); badge
  kolom Storage ikon per tipe.
- **RestoreModal (UX)**: tambah filter destination; opsi dikelompokkan
  per destination (`<optgroup>`); label opsi = `[engine] label — file
  (location) — ukuran — tanggal`. Upload tab tidak berubah.

## Error Handling

- Path local tidak absolute / tidak writable → 400 VALIDATION saat
  simpan; saat runtime dump → job failed dengan pesan jelas di log.
- SFTP gagal dial/auth/remote dir tidak writable → test endpoint
  mengembalikan `{ok:false,error}`; runtime → job failed, log job
  menampilkan tahap (dial/auth/upload).
- Destination dihapus saat masih direferensikan → 409 (guard yang sudah
  ada, kini mencakup semua tipe).
- Rebuild tabel migrasi dalam satu transaksi; gagal → startup gagal
  dengan pesan jelas (data lama tak tersentuh).

## Testing

- **Unit storage**: LocalFS dengan root custom (round-trip, fail-if-
  exists, traversal), SFTP config validation + sanitasi key path + error
  dial tanpa server; SFTP round-trip di integration test.
- **Unit meta**: CRUD destination ketiga kind; migrasi rebuild (fixture
  schema lama berisi data → rebuild → data utuh, kind baru bisa dibuat).
- **Unit api**: validasi payload per kind (create/update/test), test
  endpoint local benar-benar menulis probe, sftp gagal rapi.
- **Unit runner**: dump ke destination row `local` (bukan hanya
  `destID=0`), restore dari destination row.
- **Integration (docker)**: tambah layanan sshd (`atmoz/sftp`) ke
  compose; e2e dump → file muncul di SFTP, restore dari SFTP sukses.

## Out of Scope

- Penjadwalan sinkronisasi antar destination (mirror/replikasi backup).
- Rotasi/retensi otomatis file lama.
- Protokol remote lain (rsync, push HTTP antar ku-dump) — sudah
  diputuskan pakai SFTP.
- Multi-user / role: ku-dump tetap single-admin.
