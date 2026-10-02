# Dump Workflows — Design Spec

Date: 2026-10-02
Status: Approved (brainstorming session)

## Problem

Dumping today is one-shot and manual only: `POST /api/databases/{id}/dump`
creates a job that runs immediately. There is no way to schedule backups
(recurring or one-time) or to treat "dump this database, on this schedule,
to this destination" as a reusable entity.

## Goal

Replace the one-shot dump flow with **dump workflows**. A workflow binds
three components:

1. **Database** to dump
2. **Trigger** — one of:
   - `manual` — runs when the user presses Run
   - `once` — runs a single time at a given datetime, then disables itself
   - `cron` — runs on a recurring schedule (5-field standard cron)
3. **Storage destination** — local (`dest_id = 0`) or any S3 destination

## Non-goals

- Per-workflow timezones (server local time everywhere)
- Catch-up of runs missed while the app was down (skip; recompute next run)
- Retention / automatic cleanup of old dumps
- Restore workflows (restore flow unchanged)

## Data Model

New table in `internal/meta/meta.go` schema:

```sql
CREATE TABLE IF NOT EXISTS dump_workflows (
  id           INTEGER PRIMARY KEY,
  name         TEXT NOT NULL,
  database_id  INTEGER NOT NULL REFERENCES databases(id) ON DELETE CASCADE,
  dest_id      INTEGER NOT NULL DEFAULT 0,
  trigger_kind TEXT NOT NULL CHECK (trigger_kind IN ('manual','once','cron')),
  run_at       TEXT,
  cron         TEXT,
  enabled      INTEGER NOT NULL DEFAULT 1,
  last_run_at  TEXT,
  last_error   TEXT,
  next_run_at  TEXT,
  created_at   TEXT NOT NULL
);
```

- `name`: optional in the UI; backend default `"<database> (<engine>)"`.
- `run_at`: RFC3339, only for `once`.
- `cron`: only for `cron`. Validated with `cron.ParseStandard` (robfig/cron/v3).
- `next_run_at`: NULL for `manual`; otherwise the next scheduled run.
- `last_error`: last failure reason ("database busy", "tools missing",
  "store unavailable", "missed"); cleared on a successful trigger.

Migration: `dumps.workflow_id INTEGER NULL` added via `ensureColumn`
(provenance; the Dumps page shows the workflow name when set).
No other existing tables change.

Cron library: `github.com/robfig/cron/v3` (validation + `Next`; correct on
DST / month boundaries — not hand-rolled).

## Scheduler — `internal/scheduler/scheduler.go`

Single goroutine, tick every **30 seconds**. The tick body is extracted as
`RunOnce(ctx, now)` so unit tests call it directly with an explicit time —
no sleeps, no fake-clock framework.

Per tick, for every workflow with `enabled = 1 AND next_run_at <= now`:

1. Resolve the store via `newStore(ctx, dest_id)`; on failure set
   `last_error` and leave `next_run_at` unchanged — the run is retried on
   the next tick (a backup runs once its storage comes back).
2. Create the dump + job through the shared `startDump` path (below).
3. Set `last_run_at = now`, clear `last_error`, then:
   - `once` → `enabled = 0`, `next_run_at = NULL`
   - `cron` → `next_run_at = schedule.Next(now)` (always from *now* —
     missed runs are never replayed)

Boot recovery (`scheduler.Recover(ctx, now)`):

- `cron` workflow with `next_run_at IS NULL` → recompute from now.
- `once` workflow whose `run_at` already passed while down →
  `enabled = 0`, `last_error = 'missed'` (never auto-runs at boot).
- Panics inside the loop are recovered; the loop keeps ticking.

Timezone: server local time (`time.Now()`, `time.Local`).

## Shared Run Path

The logic currently in the dump handler (validate tools → pre-create store
→ create `dumps` row `pending` → `CreateJobGuarded` → `Runner.Start`) is
extracted into a single closure **`startDump` defined in `cmd/ku-dump/main.go`**
and injected into both `api.Deps` and the scheduler. Both the
`POST /api/workflows/{id}/run` endpoint and scheduler ticks use it — there is
exactly one way to start a dump run.

Guard failure (another job active on that database): the dump row is marked
`failed` (never left pending) and the caller reports the conflict — HTTP 409
for the endpoint, `last_error = 'database busy'` for the scheduler.

## API — `internal/api/workflows.go`

| Route | Behavior |
|---|---|
| `GET /api/workflows` | list, joined with database + destination names |
| `POST /api/workflows` | create; validates cron parses and `run_at` is in the future; computes `next_run_at` (manual → NULL, once → run_at, cron → Next(now)) |
| `PUT /api/workflows/{id}` | edit; trigger change recomputes `next_run_at`; `enabled` toggle |
| `DELETE /api/workflows/{id}` | delete |
| `POST /api/workflows/{id}/run` | run now; works regardless of `enabled`; never shifts the schedule (a `once` workflow still fires at its `run_at` afterwards) |

Removed: `POST /api/databases/{id}/dump` (full replacement). Destinations may
not be deleted while referenced by an existing workflow (extend the existing
409 delete-guard). All IDs stay opaque base64 (`encID`/`decID`).

## Frontend

- **New page `/workflows`**: table of workflows — name, database,
  destination, trigger description ("Manual" / "Once — 5 Oct 03:00" /
  "Daily 02:00"), enabled toggle, next run, last run / last_error, actions
  Run / Edit / Delete. Sidebar entry with a badge (like Jobs).
- **`WorkflowModal`** replaces `DumpModal`; one form, three sections:
  1. Database select (preselected when opened from the Databases page)
  2. Trigger — segmented control: Manual | Once (datetime-local) |
     Recurring (preset dropdown hourly/daily/weekly + time picker +
     day-of-week for weekly, plus "Advanced: cron" text input).
     **Presets are converted to cron in the frontend**; the backend only
     ever sees a cron string.
  3. Destination select (local + S3 destinations)
- **Databases.tsx**: the Dump button opens `WorkflowModal` with the database
  preselected. For a manual trigger the frontend creates the workflow then
  immediately calls `/run` — the "runs right away" UX is preserved with no
  backend special-casing.
- **Dumps page**: small "via workflow" column from `workflow_id`.

## Error Handling

- Invalid cron / past `run_at` → 400 at save time (trust-boundary validation).
- Busy database / missing tools / store failure → no job created where
  possible, `last_error` recorded, schedule still advances; visible in UI.
- Database deleted → workflow cascade-deleted.
- Scheduler panic → recovered, loop continues.

## Testing

- **Unit**: workflow meta CRUD; scheduler via `RunOnce(ctx, now)`
  (cron advance, once→disable, busy→`last_error`, missed-on-boot); API
  handler CRUD + run (including 409 on busy and delete-guard on referenced
  destination); cron validation.
- **Integration**: `TestAPIEndToEnd` switches to the workflow path
  (create manual workflow + `/run`) instead of the removed dump endpoint.
- Frontend preset→cron conversion has no automated tests (consistent with
  the repo — no web test infra).

## Dependencies

- Add: `github.com/robfig/cron/v3` (only; scheduler loop is hand-rolled).
