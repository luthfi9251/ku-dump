import { useState } from 'react'
import type { FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { Cloud, HardDrive } from 'lucide-react'
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
