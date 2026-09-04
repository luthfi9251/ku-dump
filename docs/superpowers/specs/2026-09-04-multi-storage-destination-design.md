# Multi Storage Destination — Design

Date: 2026-09-04
Status: Approved (brainstorming session)

## Ringkasan

Saat ini ku-dump hanya mendukung satu konfigurasi S3 (flat settings keys
`s3_*` di tabel `settings`). Fitur ini mengubahnya menjadi **banyak
storage destination** yang bisa dikelola user: saat dump, user memilih
database tujuan dump **dan** destination penyimpanan; saat restore, user
memilih dump apa pun (dari destination mana pun) dan menentukan database
target yang tidak harus sama dengan sumber.

Alur restore bebas-target sebenarnya sudah ada di v1 (RestoreModal + API
`POST /api/restores` memvalidasi hanya kesamaan engine). Fokus kerja
fitur ini adalah **multi destination**.

## Keputusan Desain

1. **Local tetap built-in tunggal** — folder `KUDUMP_DUMPS_DIR` selalu
   tersedia sebagai destination id `0`, tidak bisa diedit/dihapus.
   Yang bisa dikelola (CRUD) hanya destination S3.
2. **Tabel baru `storage_destinations`** mengikuti pola tabel
   `databases` (kolom terstruktur, secret terenkripsi per-baris), bukan
   blob JSON.
3. **Delete guard**: destination yang masih dipakai dump tidak bisa
   dihapus (HTTP 409 `DESTINATION_IN_USE`). Ini menjamin dump/restore
   tidak pernah menemui destination yang hilang.
4. **Runner membaca dest dari record dump**, bukan dari job. Kolom
   `jobs.storage` dibiarkan (legacy), berhenti ditulis. Tabel `jobs`
   tidak berubah.
5. **Migrasi otomatis**: config S3 lama (settings keys) dimigrasi jadi
   satu destination bernama nilai `bucket`-nya, sekali jalan saat
   startup. Dump lama ber-storage `s3` diarahkan ke destination hasil
   migrasi. Route settings lama dihapus (SPA & API ship bareng, tidak
   ada consumer eksternal).

## Skema

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

Idempoten (aman dipanggil ulang):

```sql
ALTER TABLE dumps ADD COLUMN dest_id INTEGER NOT NULL DEFAULT 0;
```

- `dumps.dest_id = 0` berarti local.
- `ListDumps` LEFT JOIN `storage_destinations` untuk nama destination
  (id 0 → "Local").

## API

| Route | Perilaku |
|---|---|
| `GET /api/storage/destinations` | daftar; secret dimask (`secretSet: bool`) |
| `POST /api/storage/destinations` | create; validasi field wajib |
| `PUT /api/storage/destinations/{id}` | update; `secretKey` kosong = tetap |
| `DELETE /api/storage/destinations/{id}` | 409 `DESTINATION_IN_USE` jika masih ada dump |
| `POST /api/storage/destinations/test` | test by payload (tanpa save), cover add & edit |

Route lama `GET/PUT /api/settings/storage` dan
`POST /api/settings/storage/test` dihapus.

Perubahan endpoint lama:

- `POST /api/databases/{id}/dump`: body `storage: "local"|"s3"` →
  `destId` (angka, 0 = local). Validasi destination ada.
- DTO dump bertambah `destId` + `destName`.

## Data Flow

- **Dump**: pilih DB + destination → dump row (`dest_id`) → job →
  runner `runDump` memuat record dump, resolve store via
  `newStore(ctx, dump.DestID)`, `Put` ke sana.
- **Restore**: pilih dump + target DB (engine sama, bebas) → runner
  `runRestore` resolve store dari `dump.DestID`, `Open`, restore ke
  target. Tidak ada perubahan di RestoreModal.

## Factory Store (main.go)

```go
newStore := func(ctx context.Context, destID int64) (storage.Store, error)
// destID == 0 → storage.NewLocalFS(cfg.DumpsDir)
// else        → st.GetDestination → decrypt secret → storage.NewS3
```

Menggantikan switch `"local"|"s3"` dan menghapus hack
`api.Server{Deps: api.Deps{...}}` yang ada di `main.go` untuk membaca
S3 config.

## Frontend

- **Settings**: form S3 tunggal → daftar kartu destination (nama,
  endpoint/bucket, badge secret, test/edit/hapus) + modal add/edit.
- **DumpModal**: kartu "Local" + satu kartu per destination S3,
  submit `destId`.
- **Dumps**: tampilkan nama destination.
- **RestoreModal**: tidak berubah.

## Error Handling

- Delete destination terpakai → 409, frontend menampilkan pesan.
- `destId` tidak dikenal saat dump → 400 `STORAGE_NOT_CONFIGURED`.
- Restore dump yang destination-nya sudah tidak ada tidak mungkin
  terjadi karena delete guard.

## Testing

- `meta`: CRUD destination; migrasi legacy S3 (buat destination,
  update dumps, hapus keys).
- `api`: CRUD + auth guard; delete-guard 409; dump dengan `destId`;
  test-by-payload.
- `runner`: update fake `newStore` ke signature `destID`; dump/restore
  resolve store dari dump.
- Integration (docker e2e): pindah ke route destination baru.

## Non-goals

- Multiple local folder.
- Copy/move dump antar destination.
- Health check / test terjadwal per destination.
- Destination selain `local` dan `s3` (GCS/Azure dsb.) — kind memungkinkan
  ditambah nanti.
