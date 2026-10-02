# Dump Workflows Implementation Plan — Chunk 3: Frontend + Integration + Docs

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the one-shot dump UI with workflow-based UI (new Workflows page + WorkflowModal), move the e2e integration tests onto the workflow API, and update the README.

**Architecture:** React 19 SPA following existing page/modal patterns. Preset schedules (hourly/daily/weekly) are converted to cron **in the frontend** — the backend only ever sees a cron string. Integration tests switch from `POST /api/databases/{id}/dump` (removed in Chunk 2) to create-manual-workflow + run.

**Tech Stack:** React 19, TypeScript, Vite, Tailwind, `@tanstack/react-query`, `react-router-dom`.

**Spec:** `docs/superpowers/specs/2026-10-02-dump-workflows-design.md`
**Depends on:** Chunks 1–2 complete.

## Global Constraints

- UI primitives live in `web/src/ui.tsx` (`Button`, `Input`, `Select`, `Field`, `Modal`, `Badge`, `StatCard`, `Spinner`, `cn`).
- Dark theme classes: slate-950 background, indigo accents — copy existing pages.
- Frontend has no test infra: verification = `npm --prefix web run build` (tsc strict catches type errors) — every task ends with it.
- `runAt` sent as `new Date(x).toISOString()` (RFC3339 UTC).
- No new npm dependencies.

---

### Task 6: Frontend — types, WorkflowModal, Workflows page, route, sidebar

**Files:**
- Modify: `web/src/lib/types.ts`
- Create: `web/src/pages/Workflows.tsx`
- Create: `web/src/modals/WorkflowModal.tsx`
- Modify: `web/src/App.tsx`
- Modify: `web/src/Layout.tsx`

**Interfaces:**
- Consumes: REST endpoints from Chunk 2 Task 4.
- Produces: `WorkflowDTO`, `TriggerKind` types; `describeCron(expr)` helper; `WorkflowModal` used by Task 7.

- [ ] **Step 1: Add types** — append to `web/src/lib/types.ts`:

```ts
export type TriggerKind = 'manual' | 'once' | 'cron'

export interface WorkflowDTO {
  id: string
  name: string
  databaseId: string
  databaseName: string
  engine: Engine
  destId: string
  destName: string
  triggerKind: TriggerKind
  runAt: string
  cron: string
  enabled: boolean
  lastRunAt: string
  lastError: string
  nextRunAt: string
  createdAt: string
}
```

And add `workflowName: string` to `DumpDTO` (after `destName`).

- [ ] **Step 2: Create `web/src/modals/WorkflowModal.tsx`**

Three sections matching the user flow: ① database, ② trigger, ③ destination. Reuses the `DestCard` visual from the old DumpModal.

```tsx
import { useState } from 'react'
import type { FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { CalendarClock, Cloud, HardDrive } from 'lucide-react'
import { api, ApiError } from '../lib/api'
import type { DatabaseDTO, StorageDestinationDTO, TriggerKind, WorkflowDTO } from '../lib/types'
import { Button, Field, Input, Modal, Select, Spinner } from '../ui'

type Preset = 'hourly' | 'daily' | 'weekly'

export function presetToCron(preset: Preset, time: string, dow: number): string {
  const [h, m] = time.split(':').map(Number)
  switch (preset) {
    case 'hourly':
      return `${m || 0} * * * *`
    case 'weekly':
      return `${m || 0} ${h || 0} * * ${dow}`
    default:
      return `${m || 0} ${h || 0} * * *`
  }
}

function toLocalInput(iso: string): string {
  const d = new Date(iso)
  return new Date(d.getTime() - d.getTimezoneOffset() * 60000).toISOString().slice(0, 16)
}

export default function WorkflowModal({
  open,
  onClose,
  db,
  editing,
  onSaved,
}: {
  open: boolean
  onClose: () => void
  db?: DatabaseDTO
  editing?: WorkflowDTO
  onSaved?: () => void
}) {
  const [name, setName] = useState(editing?.name ?? '')
  const [dbId, setDbId] = useState(editing?.databaseId ?? db?.id ?? '')
  const [destId, setDestId] = useState(editing?.destId ?? '')
  const [kind, setKind] = useState<TriggerKind>(editing?.triggerKind ?? 'manual')
  const [runAt, setRunAt] = useState(editing?.runAt ? toLocalInput(editing.runAt) : '')
  const [preset, setPreset] = useState<Preset>('daily')
  const [time, setTime] = useState('02:00')
  const [dow, setDow] = useState(1)
  const [advanced, setAdvanced] = useState(editing ? editing.triggerKind === 'cron' : false)
  const [cronExpr, setCronExpr] = useState(editing?.cron ?? '0 2 * * *')
  const [runNow, setRunNow] = useState(true)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const navigate = useNavigate()

  const dbs = useQuery({
    queryKey: ['databases'],
    queryFn: () => api.get<DatabaseDTO[]>('/api/databases'),
    enabled: open && !db && !editing,
  })
  const dests = useQuery({
    queryKey: ['storage-destinations'],
    queryFn: () => api.get<StorageDestinationDTO[]>('/api/storage/destinations'),
    enabled: open,
  })

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    setError('')
    setBusy(true)
    try {
      const body: Record<string, unknown> = {
        name,
        databaseId: dbId,
        destId,
        triggerKind: kind,
      }
      if (editing) body.enabled = editing.enabled
      if (kind === 'once') body.runAt = new Date(runAt).toISOString()
      if (kind === 'cron') body.cron = advanced ? cronExpr : presetToCron(preset, time, dow)
      if (editing) {
        await api.put<WorkflowDTO>(`/api/workflows/${editing.id}`, body)
      } else {
        const wf = await api.post<WorkflowDTO>('/api/workflows', body)
        if (kind === 'manual' && runNow) {
          const res = await api.post<{ jobId: string }>(`/api/workflows/${wf.id}/run`)
          onClose()
          onSaved?.()
          navigate(`/jobs?job=${res.jobId}`)
          return
        }
      }
      onClose()
      onSaved?.()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'failed to save workflow')
    } finally {
      setBusy(false)
    }
  }

  const dbsList = db ? [db] : (dbs.data ?? [])

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={editing ? `Edit Workflow — ${editing.name}` : 'New Dump Workflow'}
      subtitle="Pick the database, when it runs, and where the dump is stored."
      wide
    >
      <form onSubmit={onSubmit} className="space-y-5">
        {/* 1. Database */}
        <Field label="1 · Database">
          {db ? (
            <div className="rounded-lg border border-slate-700 bg-slate-950/60 px-3 py-2 text-sm text-slate-200">
              {db.name} <span className="text-xs text-slate-500">({db.engine} · {db.dbName})</span>
            </div>
          ) : (
            <Select value={dbId} onChange={(e) => setDbId(e.target.value)} required>
              <option value="" disabled>
                Select database…
              </option>
              {dbsList.map((d) => (
                <option key={d.id} value={d.id}>
                  {d.name} ({d.engine})
                </option>
              ))}
            </Select>
          )}
        </Field>

        {/* 2. Trigger */}
        <Field label="2 · When to run">
          <div className="flex gap-2">
            {(['manual', 'once', 'cron'] as TriggerKind[]).map((k) => (
              <button
                key={k}
                type="button"
                onClick={() => setKind(k)}
                className={`flex flex-1 items-center justify-center gap-1.5 rounded-lg border px-3 py-2 text-sm transition-all ${
                  kind === k
                    ? 'border-indigo-500 bg-indigo-950/40 text-indigo-200'
                    : 'border-slate-800 bg-slate-950/60 text-slate-400 hover:border-slate-700'
                }`}
              >
                {k === 'manual' ? 'Run manually' : k === 'once' ? 'Once at time' : 'Recurring'}
              </button>
            ))}
          </div>
          <div className="mt-2 space-y-2">
            {kind === 'once' && (
              <Input type="datetime-local" value={runAt} onChange={(e) => setRunAt(e.target.value)} required />
            )}
            {kind === 'cron' && !advanced && (
              <div className="flex flex-wrap items-center gap-2">
                <Select value={preset} onChange={(e) => setPreset(e.target.value as Preset)} className="w-36">
                  <option value="hourly">Every hour</option>
                  <option value="daily">Daily</option>
                  <option value="weekly">Weekly</option>
                </Select>
                {preset !== 'hourly' && (
                  <Select value={String(dow)} onChange={(e) => setDow(Number(e.target.value))} className="w-32" disabled={preset === 'daily'}>
                    {['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'].map((d, i) => (
                      <option key={d} value={i}>
                        {preset === 'weekly' ? `on ${d}` : d}
                      </option>
                    ))}
                  </Select>
                )}
                <Input type="time" value={time} onChange={(e) => setTime(e.target.value)} className="w-32" />
              </div>
            )}
            {kind === 'cron' && (
              <div className="flex items-center gap-2">
                <Input
                  value={advanced ? cronExpr : presetToCron(preset, time, dow)}
                  onChange={(e) => setCronExpr(e.target.value)}
                  readOnly={!advanced}
                  className="flex-1 font-mono text-xs"
                />
                <label className="flex shrink-0 items-center gap-1.5 text-xs text-slate-400">
                  <input type="checkbox" checked={advanced} onChange={(e) => setAdvanced(e.target.checked)} />
                  Advanced
                </label>
              </div>
            )}
            {kind === 'manual' && !editing && (
              <label className="flex items-center gap-2 text-xs text-slate-300">
                <input type="checkbox" checked={runNow} onChange={(e) => setRunNow(e.target.checked)} />
                Run immediately after creating
              </label>
            )}
          </div>
        </Field>

        {/* 3. Destination */}
        <Field label="3 · Destination">
          <div className="grid gap-3 pt-1 sm:grid-cols-2">
            <label
              className={`flex cursor-pointer flex-col rounded-xl border p-4 transition-all ${
                destId === ''
                  ? 'border-indigo-500 bg-indigo-950/40 text-indigo-200 shadow-md shadow-indigo-600/10'
                  : 'border-slate-800 bg-slate-950/60 text-slate-400 hover:border-slate-700 hover:text-slate-200'
              }`}
              onClick={() => setDestId('')}
            >
              <div className="flex items-center gap-2 text-sm font-semibold">
                <input type="radio" checked={destId === ''} onChange={() => setDestId('')} className="hidden" />
                <HardDrive size={18} className={destId === '' ? 'text-indigo-400' : 'text-slate-400'} />
                Local Storage
              </div>
              <span className="mt-1 truncate text-[11px] text-slate-500">Save on server disk</span>
            </label>
            {(dests.data ?? []).map((d) => (
              <label
                key={d.id}
                className={`flex cursor-pointer flex-col rounded-xl border p-4 transition-all ${
                  destId === d.id
                    ? 'border-indigo-500 bg-indigo-950/40 text-indigo-200 shadow-md shadow-indigo-600/10'
                    : 'border-slate-800 bg-slate-950/60 text-slate-400 hover:border-slate-700 hover:text-slate-200'
                }`}
                onClick={() => setDestId(d.id)}
              >
                <div className="flex items-center gap-2 text-sm font-semibold">
                  <input type="radio" checked={destId === d.id} onChange={() => setDestId(d.id)} className="hidden" />
                  <Cloud size={18} className={destId === d.id ? 'text-indigo-400' : 'text-slate-400'} />
                  {d.name}
                </div>
                <span className="mt-1 truncate text-[11px] text-slate-500">{d.bucket}</span>
              </label>
            ))}
          </div>
          {dests.data && dests.data.length === 0 && (
            <p className="mt-2 text-[11px] text-slate-500">
              No S3 destinations configured — add one in Settings, or store locally.
            </p>
          )}
        </Field>

        <Field label="Name (optional)" hint="Defaults to database name">
          <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="nightly-backup" />
        </Field>

        {error && (
          <div className="rounded-lg border border-rose-800/60 bg-rose-950/40 p-2.5 text-xs text-rose-300">{error}</div>
        )}

        <div className="flex justify-end gap-2 border-t border-slate-800 pt-4">
          <Button type="button" variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" disabled={busy || !dbId} className="shadow-lg shadow-indigo-600/20">
            {busy ? <Spinner /> : editing ? 'Save Changes' : 'Create Workflow'}
          </Button>
        </div>
      </form>
    </Modal>
  )
}
```

Note on the awkward `enabled` expression in `onSubmit`: simplify to what it
actually means — the payload's `enabled` is only sent when editing (preserve
the row's current state; toggling happens from the list):

```ts
if (editing) body.enabled = editing.enabled
```

Apply that instead of the inline ternary in the `body` literal.

- [ ] **Step 3: Create `web/src/pages/Workflows.tsx`**

```tsx
import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { AlertTriangle, CalendarClock, Pause, Play, Pencil, Plus, Trash2 } from 'lucide-react'
import { api } from '../lib/api'
import type { TriggerKind, WorkflowDTO } from '../lib/types'
import { Badge, Button, StatCard } from '../ui'
import WorkflowModal from '../modals/WorkflowModal'

export function describeCron(expr: string): string {
  const parts = expr.trim().split(/\s+/)
  if (parts.length !== 5) return expr
  const [m, h, dom, mon, dow] = parts
  if (m !== '*' && h === '*' && dom === '*' && mon === '*' && dow === '*') return `Hourly at :${m.padStart(2, '0')}`
  if (m !== '*' && h !== '*' && dom === '*' && mon === '*' && dow === '*') {
    return `Daily at ${h.padStart(2, '0')}:${m.padStart(2, '0')}`
  }
  if (m !== '*' && h !== '*' && dom === '*' && mon === '*' && dow !== '*') {
    const days = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat']
    return `Weekly on ${days[Number(dow)] ?? dow} at ${h.padStart(2, '0')}:${m.padStart(2, '0')}`
  }
  return expr
}

function describeTrigger(w: WorkflowDTO): string {
  if (w.triggerKind === 'manual') return 'Manual'
  if (w.triggerKind === 'once') return `Once — ${new Date(w.runAt).toLocaleString()}`
  return describeCron(w.cron)
}

export default function Workflows() {
  const qc = useQueryClient()
  const navigate = useNavigate()
  const [showAdd, setShowAdd] = useState(false)
  const [editing, setEditing] = useState<WorkflowDTO | undefined>(undefined)

  const workflows = useQuery({
    queryKey: ['workflows'],
    queryFn: () => api.get<WorkflowDTO[]>('/api/workflows'),
    refetchInterval: 10000,
  })

  function invalidate() {
    void qc.invalidateQueries({ queryKey: ['workflows'] })
  }

  const toggle = useMutation({
    mutationFn: (w: WorkflowDTO) =>
      api.put<WorkflowDTO>(`/api/workflows/${w.id}`, {
        name: w.name,
        databaseId: w.databaseId,
        destId: w.destId,
        triggerKind: w.triggerKind,
        runAt: w.runAt || undefined,
        cron: w.cron || undefined,
        enabled: !w.enabled,
      }),
    onSuccess: invalidate,
  })

  const run = useMutation({
    mutationFn: (w: WorkflowDTO) => api.post<{ jobId: string }>(`/api/workflows/${w.id}/run`),
    onSuccess: (res) => {
      invalidate()
      navigate(`/jobs?job=${res.jobId}`)
    },
  })

  const remove = useMutation({
    mutationFn: (id: string) => api.del(`/api/workflows/${id}`),
    onSuccess: invalidate,
  })

  const rows = workflows.data ?? []
  const active = rows.filter((w) => w.enabled && w.triggerKind !== 'manual').length
  const failing = rows.filter((w) => w.lastError !== '').length

  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-white">Dump Workflows</h1>
          <p className="mt-1 text-xs text-slate-400">
            Reusable dump recipes: which database, when it runs, where it lands.
          </p>
        </div>
        <Button onClick={() => setShowAdd(true)} size="md" className="shrink-0 shadow-lg shadow-indigo-600/25">
          <Plus size={16} /> New Workflow
        </Button>
      </div>

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
        <StatCard title="Total Workflows" value={rows.length} icon={CalendarClock} tone="indigo" />
        <StatCard title="Scheduled Active" value={active} icon={Play} tone="emerald" />
        <StatCard title="With Errors" value={failing} icon={AlertTriangle} tone="amber" />
      </div>

      <div className="overflow-hidden rounded-2xl border border-slate-800 bg-slate-900/80 shadow-2xl backdrop-blur-md">
        <div className="overflow-x-auto">
          <table className="w-full text-left text-sm text-slate-300">
            <thead className="border-b border-slate-800 bg-slate-950/60 text-xs font-semibold uppercase tracking-wider text-slate-400">
              <tr>
                <th className="px-5 py-3.5">Name</th>
                <th className="px-5 py-3.5">Database</th>
                <th className="px-5 py-3.5">Destination</th>
                <th className="px-5 py-3.5">Trigger</th>
                <th className="px-5 py-3.5">Next Run</th>
                <th className="px-5 py-3.5">Last Run</th>
                <th className="px-5 py-3.5">Status</th>
                <th className="px-5 py-3.5 text-right">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-800/60">
              {rows.map((w) => (
                <tr key={w.id} className="transition-colors hover:bg-slate-800/40">
                  <td className="px-5 py-4 font-semibold text-slate-100">{w.name}</td>
                  <td className="px-5 py-4 text-slate-300">{w.databaseName}</td>
                  <td className="px-5 py-4 text-slate-400">{w.destName}</td>
                  <td className="px-5 py-4 text-slate-300">{describeTrigger(w)}</td>
                  <td className="px-5 py-4 text-xs text-slate-400">
                    {w.nextRunAt ? new Date(w.nextRunAt).toLocaleString() : '—'}
                  </td>
                  <td className="px-5 py-4 text-xs text-slate-400">
                    {w.lastRunAt ? new Date(w.lastRunAt).toLocaleString() : '—'}
                    {w.lastError && <div className="mt-0.5 text-[11px] text-rose-400">{w.lastError}</div>}
                  </td>
                  <td className="px-5 py-4">
                    {w.lastError !== '' ? (
                      <Badge tone="red" pulse>
                        error
                      </Badge>
                    ) : w.enabled ? (
                      <Badge tone="green">active</Badge>
                    ) : (
                      <Badge tone="slate">paused</Badge>
                    )}
                  </td>
                  <td className="px-5 py-4 text-right">
                    <div className="flex items-center justify-end gap-1.5">
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() => run.mutate(w)}
                        disabled={run.isPending && run.variables?.id === w.id}
                        title="Run now"
                      >
                        <Play size={14} />
                      </Button>
                      <Button
                        size="sm"
                        variant="ghost"
                        onClick={() => toggle.mutate(w)}
                        title={w.enabled ? 'Pause' : 'Resume'}
                      >
                        {w.enabled ? <Pause size={14} /> : <Play size={14} />}
                      </Button>
                      <Button size="sm" variant="ghost" onClick={() => setEditing(w)} title="Edit">
                        <Pencil size={14} />
                      </Button>
                      <Button
                        size="sm"
                        variant="ghost"
                        className="hover:text-rose-400"
                        onClick={() => {
                          if (confirm(`Delete workflow "${w.name}"? Scheduled runs will stop.`)) remove.mutate(w.id)
                        }}
                        title="Delete"
                      >
                        <Trash2 size={14} />
                      </Button>
                    </div>
                  </td>
                </tr>
              ))}
              {rows.length === 0 && (
                <tr>
                  <td colSpan={8} className="px-6 py-12 text-center text-slate-500">
                    <CalendarClock size={32} className="mx-auto mb-2 opacity-40" />
                    No workflows yet — create one to schedule your first dump.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </div>

      <WorkflowModal open={showAdd} onClose={() => setShowAdd(false)} onSaved={invalidate} />
      {editing && (
        <WorkflowModal open editing={editing} onClose={() => setEditing(undefined)} onSaved={invalidate} />
      )}
    </div>
  )
}
```

Note: `TriggerKind` import is used by nothing in this file after the final
edit — remove it from the import if `tsc` flags it (noUnusedLocals).

- [ ] **Step 4: Route + sidebar with error badge**

`web/src/App.tsx` — add import and route:

```tsx
import Workflows from './pages/Workflows'
// inside the auth-guarded Routes, before /settings:
<Route path="/workflows" element={<Workflows />} />
```

`web/src/Layout.tsx` — add `CalendarClock` to the lucide-react import, add
`WorkflowDTO` to the types import, insert into `links` between Databases and
Dumps:

```ts
{ to: '/workflows', label: 'Workflows', icon: CalendarClock },
```

Then add the error badge (mirrors the Jobs active badge). Inside `Layout()`,
next to the jobs query:

```tsx
  const workflows = useQuery({
    queryKey: ['workflows-summary'],
    queryFn: () => api.get<WorkflowDTO[]>('/api/workflows'),
    refetchInterval: 10000,
  })
  const failingWorkflows = workflows.data?.filter((w) => w.lastError !== '').length ?? 0
```

And inside the `links.map` render, after the Jobs badge block:

```tsx
              {label === 'Workflows' && failingWorkflows > 0 && (
                <span className="flex items-center gap-1 rounded-full bg-rose-500/20 px-2 py-0.5 text-[11px] font-semibold text-rose-400 border border-rose-500/30">
                  <span className="h-1.5 w-1.5 rounded-full bg-rose-400" />
                  {failingWorkflows} error{failingWorkflows > 1 ? 's' : ''}
                </span>
              )}
```

- [ ] **Step 5: Build to verify**

Run: `npm --prefix web install && npm --prefix web run build`
Expected: tsc + vite build succeed.

- [ ] **Step 6: Commit**

```bash
git add web/src/lib/types.ts web/src/pages/Workflows.tsx web/src/modals/WorkflowModal.tsx web/src/App.tsx web/src/Layout.tsx
git commit -m "feat(web): workflows page and modal (manual/once/cron triggers)"
```

---

### Task 7: Frontend — Databases page swap, delete DumpModal, Dumps column

**Files:**
- Modify: `web/src/pages/Databases.tsx`
- Modify: `web/src/pages/Dumps.tsx`
- Delete: `web/src/modals/DumpModal.tsx`

**Interfaces:**
- Consumes: `WorkflowModal` from Task 6; `DumpDTO.workflowName` from Task 6.

- [ ] **Step 1: Swap Databases.tsx to WorkflowModal**

In `web/src/pages/Databases.tsx` (note: this file currently uses double quotes):

Replace import:

```tsx
import WorkflowModal from "../modals/WorkflowModal";
```

Replace the modal render block:

```tsx
      {dumpTarget && (
        <WorkflowModal
          open
          onClose={() => setDumpTarget(null)}
          db={dumpTarget}
          onSaved={() => {
            invalidate();
            void qc.invalidateQueries({ queryKey: ["workflows"] });
          }}
        />
      )}
```

The Dump button (`setDumpTarget(db)`) stays as is — it now opens the workflow
creator with the database preselected.

- [ ] **Step 2: Delete DumpModal**

```bash
git rm web/src/modals/DumpModal.tsx
```

- [ ] **Step 3: Add Workflow column to Dumps.tsx**

In `web/src/pages/Dumps.tsx`:

Header row — add after the `Database` `<th>`:

```tsx
                <th className="px-5 py-3.5">Workflow</th>
```

Body row — add after the database `<td>` (`{d.databaseName || ...}`):

```tsx
                  <td className="px-5 py-4 text-xs text-indigo-300">
                    {d.workflowName || <span className="text-slate-600">—</span>}
                  </td>
```

Empty-state colSpan: `colSpan={8}` → `colSpan={9}`.

- [ ] **Step 4: Build to verify**

Run: `npm --prefix web run build`
Expected: succeeds — proves no dangling `DumpModal` imports.

- [ ] **Step 5: Commit**

```bash
git add web/src/pages/Databases.tsx web/src/pages/Dumps.tsx
git commit -m "feat(web): dump button creates workflows, dumps page shows workflow provenance"
```

---

### Task 8: Integration tests + README

**Files:**
- Modify: `integration/integration_test.go`
- Modify: `README.md`

**Interfaces:**
- Consumes: `POST /api/workflows`, `POST /api/workflows/{id}/run`, `DELETE /api/workflows/{id}` (Chunk 2).

- [ ] **Step 1: Move `TestAPIEndToEnd` onto the workflow API**

In `integration/integration_test.go`, replace the opaque-ID probe and direct
dump call (the block starting `code, body = postJSON(t, client, srv.URL, "/api/databases/MTIz/dump", ...)`
through `waitJobSuccess(t, client, srv.URL, dumpRes.JobID)`) with:

```go
	code, body = postJSON(t, client, srv.URL, "/api/workflows/MTIz/run", nil)
	if code != 404 {
		t.Fatal("opaque id guessing must not pass")
	}
	var dbs []map[string]any
	code, body = getJSON(t, client, srv.URL, "/api/databases")
	json.Unmarshal(body, &dbs)
	var srcID, dstID string
	for _, db := range dbs {
		if db["name"] == "api-src" {
			srcID = db["id"].(string)
		}
		if db["name"] == "api-dst" {
			dstID = db["id"].(string)
		}
	}
	code, body = postJSON(t, client, srv.URL, "/api/workflows", map[string]string{
		"name": "e2e", "databaseId": srcID, "destId": "", "triggerKind": "manual",
	})
	if code != 201 {
		t.Fatalf("create workflow = %d: %s", code, body)
	}
	var wfRes struct {
		ID string `json:"id"`
	}
	json.Unmarshal(body, &wfRes)
	code, body = postJSON(t, client, srv.URL, "/api/workflows/"+wfRes.ID+"/run", nil)
	if code != 201 {
		t.Fatalf("run workflow = %d: %s", code, body)
	}
	var dumpRes struct {
		JobID  string `json:"jobId"`
		DumpID string `json:"dumpId"`
	}
	json.Unmarshal(body, &dumpRes)
	waitJobSuccess(t, client, srv.URL, dumpRes.JobID)
```

(The old `var dbs ... srcID/dstID` block that previously sat between the two
is folded into the snippet above; keep only one copy.)

The dump label is now the workflow name `"e2e"`, so the later
`d["label"] == "e2e"` lookup still matches — no change needed there.

- [ ] **Step 2: Move `TestAPIS3DestinationLifecycle` onto the workflow API**

Replace the direct dump call block:

```go
	code, body = postJSON(t, client, srv.URL, "/api/workflows", map[string]string{
		"name": "s3e2e", "databaseId": srcID, "destId": dest.ID, "triggerKind": "manual",
	})
	if code != 201 {
		t.Fatalf("create workflow = %d: %s", code, body)
	}
	var wfRes struct {
		ID string `json:"id"`
	}
	json.Unmarshal(body, &wfRes)
	code, body = postJSON(t, client, srv.URL, "/api/workflows/"+wfRes.ID+"/run", nil)
	if code != 201 {
		t.Fatalf("run workflow = %d: %s", code, body)
	}
	var dumpRes struct {
		JobID  string `json:"jobId"`
		DumpID string `json:"dumpId"`
	}
	json.Unmarshal(body, &dumpRes)
	waitJobSuccess(t, client, srv.URL, dumpRes.JobID)
```

Cleanup: the workflow now also guards destination deletion, so delete it
before the final destination delete — replace the last three statements with:

```go
	if code, _ := delJSON(t, client, srv.URL, "/api/workflows/"+wfRes.ID); code != 200 {
		t.Fatalf("delete workflow = %d", code)
	}
	if code, _ := delJSON(t, client, srv.URL, "/api/dumps/"+dumpID); code != 200 {
		t.Fatalf("delete dump = %d", code)
	}
	if code, _ := delJSON(t, client, srv.URL, "/api/storage/destinations/"+dest.ID); code != 200 {
		t.Fatalf("delete dest after workflow+dump gone = %d", code)
	}
```

(The mid-test `delJSON` 409 check on the in-use destination stays — now it is
guarded by both dump and workflow references.)

- [ ] **Step 3: Run the integration suite**

Run: `make integration`
Expected: all tests PASS (requires Docker).

- [ ] **Step 4: Update README**

In `README.md`, replace the **Usage** section bullets with:

```markdown
- **Databases** — register/edit/test databases. The Dump button opens the
  workflow creator with the database preselected.
- **Workflows** — reusable dump recipes: pick a database, when it runs
  (manually, once at a time, or recurring hourly/daily/weekly/advanced cron),
  and the storage destination. Run any workflow now with the play button,
  pause/resume it, and see next/last run plus errors.
- **Dumps** — history with download/delete, grouped by storage destination.
  Upload a file dump (`pg_dump -Fc` or gzipped mongo archive) to restore it.
- **Jobs** — live log tail, cancel while running.
- **Settings** — storage destinations: register any number of S3-compatible
  buckets (endpoint/bucket/credentials, MinIO works); local disk is always
  available. Plus CLI tool availability.

When a workflow runs it stores the dump under the workflow's chosen
destination, and the Dumps page shows which workflow produced it. Scheduled
runs missed while ku-dump was down are skipped (the next occurrence is
computed at startup). Destinations referenced by a workflow cannot be
deleted until the workflow is updated or removed.
```

- [ ] **Step 5: Full verification**

```bash
go test ./... && go vet ./... && npm --prefix web run build && make integration
```
Expected: all green.

- [ ] **Step 6: Commit**

```bash
git add integration/integration_test.go README.md
git commit -m "test(integration): dump lifecycle via workflows; docs update"
```
