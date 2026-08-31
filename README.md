# ku-dump

Dump and restore PostgreSQL and MongoDB databases from a single web app.
Register databases at runtime, run `pg_dump`/`pg_restore`/`mongodump`/
`mongorestore` jobs with live logs, store dumps on the server or any
S3-compatible object storage, and restore to any registered database of
the same engine (e.g. copy prod → dev).

One Go process serves the JSON API (`/api/*`) and an embedded React SPA.
Metadata (users, databases, dump history, jobs) lives in a local SQLite
file. ku-dump never reads or writes your data outside dump/restore
operations.

## Requirements

- Go 1.26+, Node 22+ (build only)
- CLI tools on the host: `pg_dump`, `pg_restore` (postgresql-client) and
  `mongodump`, `mongorestore` (mongodb-database-tools)
- Binary overrides via env if the tools are not on PATH, e.g.
  `KUDUMP_PG_DUMP="docker run --rm --network host -i postgres:16 pg_dump"`

## Quick start

    make build
    ./ku-dump    # serves on :8080, metadata in ./ku-dump.db, dumps in ./dumps

Open http://localhost:8080 — first run asks you to create the admin user.
Then register a database (engine, host, port, db, user, password), press
Dump, and watch the job log live.

## Configuration

| Env | Default | Purpose |
|---|---|---|
| KUDUMP_ADDR | :8080 | listen address |
| KUDUMP_DB | ku-dump.db | SQLite metadata path |
| KUDUMP_DUMPS_DIR | dumps | local dump storage root |
| KUDUMP_CRYPT_KEY | (auto) | hex 32-byte AES key; else generated `.ku-dump-key` |
| KUDUMP_PG_DUMP / KUDUMP_PG_RESTORE / KUDUMP_MONGODUMP / KUDUMP_MONGORESTORE | PATH lookup | tool command overrides (may include args) |

## Usage

- **Databases** — register/edit/test databases. Dump and Restore are
  disabled while another job is running on the same database.
- **Dumps** — history with download/delete. Upload a file dump
  (`pg_dump -Fc` or gzipped mongo archive) to restore it.
- **Jobs** — live log tail, cancel while running.
- **Settings** — S3-compatible storage (endpoint/bucket/credentials,
  MinIO works) and CLI tool availability.

Restores are destructive: target objects are dropped first
(`pg_restore --clean --if-exists`, `mongorestore --drop`). The UI and the
API both require typing the target database name to confirm.

## Development

    make test          # unit tests
    make integration   # docker-based e2e (postgres, mongo, minio)
    make dev           # build web + run backend

## Security notes

- Database passwords and the S3 secret are encrypted at rest (AES-256-GCM)
- HMAC-signed session cookies, login rate limiting (5 failures / 15 min)
- External IDs are opaque; uploads are size-capped and magic-byte checked
