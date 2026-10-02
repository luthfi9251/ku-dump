import { useMemo, useRef, useState } from 'react'
import type { FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { AlertTriangle, CheckCircle2, History, HardDriveUpload, UploadCloud } from 'lucide-react'
import { api, ApiError } from '../lib/api'
import { fmtSize } from '../lib/format'
import type { DatabaseDTO, DumpDTO, Engine, StorageDestinationDTO } from '../lib/types'
import { Badge, Button, Field, Modal, Select, Spinner } from '../ui'

type Tab = 'history' | 'upload'

export default function RestoreModal({
  open,
  onClose,
  target,
  source,
  onStarted,
}: {
  open: boolean
  onClose: () => void
  target?: DatabaseDTO
  source?: DumpDTO
  onStarted: () => void
}) {
  const [tab, setTab] = useState<Tab>('history')
  const [dumpId, setDumpId] = useState(source?.id ?? '')
  const [destFilter, setDestFilter] = useState('')
  const [targetId, setTargetId] = useState(target?.id ?? '')
  const [uploadEngine, setUploadEngine] = useState<Engine>(target?.engine ?? 'postgres')
  const [uploadedDump, setUploadedDump] = useState<DumpDTO | null>(null)
  const [file, setFile] = useState<File | null>(null)
  const [confirmName, setConfirmName] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const fileInput = useRef<HTMLInputElement>(null)
  const navigate = useNavigate()

  const dumps = useQuery({
    queryKey: ['dumps'],
    queryFn: () => api.get<DumpDTO[]>('/api/dumps'),
    enabled: open,
  })
  const dbs = useQuery({
    queryKey: ['databases'],
    queryFn: () => api.get<DatabaseDTO[]>('/api/databases'),
    enabled: open,
  })
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
  const engineFilter = source?.engine ?? target?.engine ?? uploadedDump?.engine
  const targets = useMemo(
    () => (dbs.data ?? []).filter((db) => !engineFilter || db.engine === engineFilter),
    [dbs.data, engineFilter],
  )
  const effectiveDumpId = tab === 'history' ? dumpId : (uploadedDump?.id ?? '')
  const selectedTarget = targets.find((db) => db.id === targetId)
  const confirmOk = !!selectedTarget && confirmName === selectedTarget.name

  async function onUpload() {
    if (!file) return
    setError('')
    setBusy(true)
    try {
      const form = new FormData()
      form.append('file', file)
      form.append('engine', uploadEngine)
      const dump = await api.upload<DumpDTO>('/api/restores/upload', form)
      setUploadedDump(dump)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'upload failed')
    } finally {
      setBusy(false)
    }
  }

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    setError('')
    if (!effectiveDumpId || !targetId || !confirmOk) return
    setBusy(true)
    try {
      const res = await api.post<{ jobId: string }>('/api/restores', {
        dumpId: effectiveDumpId,
        targetDatabaseId: targetId,
        confirmName,
      })
      onStarted()
      onClose()
      navigate(`/jobs?job=${res.jobId}`)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'restore failed')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      open={open}
      onClose={onClose}
      title="Restore Database Snapshot"
      subtitle="Overwrite target database with dump contents. Exercise caution."
      wide
    >
      <div className="space-y-5">
        {!source && (
          <div className="flex rounded-xl bg-slate-950/60 p-1 border border-slate-800">
            {(['history', 'upload'] as Tab[]).map((t) => (
              <button
                key={t}
                onClick={() => setTab(t)}
                className={`flex flex-1 items-center justify-center gap-2 rounded-lg py-2 text-xs font-semibold tracking-wide transition-all ${
                  tab === t ? 'bg-indigo-600 text-white shadow-md shadow-indigo-600/25' : 'text-slate-400 hover:text-slate-200'
                }`}
              >
                {t === 'history' ? <History size={15} /> : <HardDriveUpload size={15} />}
                {t === 'history' ? 'From Dump Archive History' : 'Upload Archive File'}
              </button>
            ))}
          </div>
        )}

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

        {tab === 'upload' && (
          <div className="space-y-4 rounded-xl border border-slate-800 bg-slate-950/40 p-4">
            <Field label="Database Engine Architecture">
              <Select
                value={uploadEngine}
                onChange={(e) => setUploadEngine(e.target.value as Engine)}
                disabled={!!target}
              >
                <option value="postgres">PostgreSQL (pg_dump -Fc archive)</option>
                <option value="mongodb">MongoDB (gzipped archive)</option>
              </Select>
            </Field>

            <Field label="Select File to Upload">
              <div
                onClick={() => fileInput.current?.click()}
                className="flex cursor-pointer flex-col items-center justify-center rounded-xl border-2 border-dashed border-slate-700/80 bg-slate-900/60 p-6 text-center transition-colors hover:border-indigo-500/80 hover:bg-slate-900"
              >
                <UploadCloud size={28} className="text-indigo-400 mb-2" />
                <span className="text-xs font-semibold text-slate-200">
                  {file ? file.name : 'Click to select dump file archive'}
                </span>
                <span className="text-[11px] text-slate-500 mt-1">Supports .dump, .tar, .gz archives</span>
                <input
                  ref={fileInput}
                  type="file"
                  className="hidden"
                  onChange={(e) => setFile(e.target.files?.[0] ?? null)}
                />
              </div>
            </Field>

            <div className="flex items-center justify-between">
              <Button variant="outline" size="sm" onClick={onUpload} disabled={!file || busy}>
                {busy ? <Spinner /> : 'Upload File Now'}
              </Button>
              {uploadedDump && (
                <Badge tone="green">
                  <CheckCircle2 size={12} className="inline mr-1" /> Uploaded: {uploadedDump.label}
                </Badge>
              )}
            </div>
          </div>
        )}

        <Field label="Target Destination Database">
          {target ? (
            <div className="flex items-center justify-between rounded-xl border border-slate-800 bg-slate-950/80 px-4 py-3 text-sm">
              <span className="font-semibold text-slate-100">{target.name}</span>
              <Badge tone={target.engine === 'postgres' ? 'blue' : 'green'}>{target.engine}</Badge>
            </div>
          ) : (
            <Select value={targetId} onChange={(e) => setTargetId(e.target.value)}>
              <option value="">Select target database instance…</option>
              {targets.map((db) => (
                <option key={db.id} value={db.id}>
                  {db.name} ({db.engine} — {db.host}:{db.port}/{db.dbName})
                </option>
              ))}
            </Select>
          )}
        </Field>

        {selectedTarget && (
          <div className="space-y-3 rounded-xl border border-amber-500/40 bg-amber-950/30 p-4 text-xs text-amber-200">
            <div className="flex items-center gap-2 font-bold text-amber-300 text-sm">
              <AlertTriangle size={18} className="shrink-0" />
              Destructive Operation Warning
            </div>
            <p className="leading-relaxed">
              Restoring will <b>DROP and OVERWRITE</b> all existing database objects in <b>{selectedTarget.name}</b> (database: <code className="bg-amber-950 px-1 py-0.5 rounded font-mono">{selectedTarget.dbName}</code>). This operation is irreversible.
            </p>
            <Field label={`Type "${selectedTarget.name}" to confirm restore`}>
              <input
                className="w-full rounded-lg border border-amber-500/50 bg-slate-950 px-3 py-2 text-sm text-amber-100 focus:border-amber-400 focus:outline-none"
                value={confirmName}
                onChange={(e) => setConfirmName(e.target.value)}
                placeholder={selectedTarget.name}
              />
            </Field>
          </div>
        )}

        {error && (
          <div className="rounded-lg border border-rose-800/60 bg-rose-950/40 p-2.5 text-xs text-rose-300">
            {error}
          </div>
        )}

        <div className="flex items-center justify-end gap-2 border-t border-slate-800 pt-4">
          <Button type="button" variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button
            variant="danger"
            onClick={onSubmit}
            disabled={busy || !effectiveDumpId || !targetId || !confirmOk}
          >
            {busy ? <Spinner /> : 'Confirm & Restore Database'}
          </Button>
        </div>
      </div>
    </Modal>
  )
}

