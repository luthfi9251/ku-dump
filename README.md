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

- **Databases** — register/edit/test databases. The Dump button opens the
  workflow creator with the database preselected.
- **Workflows** — reusable dump recipes: pick a database, when it runs
  (manually, once at a time, or recurring hourly/daily/weekly/advanced cron),
  and the storage destination. Optionally set a storage path (folder inside
  the destination) and a file name pattern with variables `{workflow}` `{db}`
  `{engine}` `{date}` `{time}` `{timestamp}`; empty keeps the default
  `<engine>/<db>/<timestamp>.dump` layout. A timestamp is always part of the
  file name, and dumps are never overwritten (existing objects refuse to be
  replaced). Run any workflow now with the play button, pause/resume it, and
  see next/last run plus errors.
- **Dumps** — history with download/delete, grouped by storage destination.
  Upload a file dump (`pg_dump -Fc` or gzipped mongo archive) to restore it.
- **Jobs** — live log tail, cancel while running.
- **Destinations** — reusable storage targets on their own page: local
  folders (any absolute path), S3-compatible buckets, or remote servers
  over SFTP (password or key). Test each with one click; destinations
  referenced by dumps or workflows cannot be deleted. Assign any of them
  per workflow.
- **Settings** — CLI tool availability.

When a workflow runs it stores the dump under the workflow's chosen
destination, and the Dumps page shows which workflow produced it. Scheduled
runs missed while ku-dump was down are skipped (the next occurrence is
computed at startup). Destinations referenced by a workflow cannot be
deleted until the workflow is updated or removed.

Restores are destructive: target objects are dropped first
(`pg_restore --clean --if-exists`, `mongorestore --drop`). The UI and the
API both require typing the target database name to confirm. The restore
dialog lets you filter dumps per destination and shows file size and date.

## Development

    make test          # unit tests
    make integration   # docker-based e2e (postgres, mongo, minio)
    make dev           # build web + run backend

## Security notes

- Database passwords and S3 destination secrets are encrypted at rest
  (AES-256-GCM)
- HMAC-signed session cookies, login rate limiting (5 failures / 15 min)
- External IDs are opaque; uploads are size-capped and magic-byte checked
