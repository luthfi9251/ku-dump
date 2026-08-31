import { useMemo, useRef, useState } from 'react'
import type { FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { api, ApiError } from '../lib/api'
import type { DatabaseDTO, DumpDTO, Engine } from '../lib/types'
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

  const usableDumps = useMemo(
    () =>
      (dumps.data ?? []).filter((d) => {
        if (d.status !== 'ready' && d.status !== 'uploaded') return false
        if (target && d.engine !== target.engine) return false
        return true
      }),
    [dumps.data, target],
  )
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
    <Modal open={open} onClose={onClose} title="Restore database" wide>
      <div className="space-y-4">
        {!source && (
          <div className="flex gap-2 border-b border-slate-200 pb-2">
            {(['history', 'upload'] as Tab[]).map((t) => (
              <button
                key={t}
                onClick={() => setTab(t)}
                className={`rounded px-3 py-1.5 text-sm ${tab === t ? 'bg-slate-900 text-white' : 'text-slate-600 hover:bg-slate-100'}`}
              >
                {t === 'history' ? 'From dump history' : 'Upload file'}
              </button>
            ))}
          </div>
        )}

        {tab === 'history' && (
          <Field label="Source dump">
            <Select value={dumpId} onChange={(e) => setDumpId(e.target.value)}>
              <option value="">Select a dump…</option>
              {source && <option value={source.id}>{source.label} (selected)</option>}
              {usableDumps
                .filter((d) => d.id !== source?.id)
                .map((d) => (
                  <option key={d.id} value={d.id}>
                    [{d.engine}] {d.label} — {d.databaseName || 'upload'} ({d.status})
                  </option>
                ))}
            </Select>
          </Field>
        )}

        {tab === 'upload' && (
          <div className="space-y-2">
            <Field label="Engine">
              <Select
                value={uploadEngine}
                onChange={(e) => setUploadEngine(e.target.value as Engine)}
                disabled={!!target}
              >
                <option value="postgres">PostgreSQL (pg_dump -Fc archive)</option>
                <option value="mongodb">MongoDB (gzipped archive)</option>
              </Select>
            </Field>
            <Field label="Dump file">
              <input
                ref={fileInput}
                type="file"
                className="text-sm"
                onChange={(e) => setFile(e.target.files?.[0] ?? null)}
              />
            </Field>
            <Button variant="outline" onClick={onUpload} disabled={!file || busy}>
              {busy ? <Spinner /> : 'Upload'}
            </Button>
            {uploadedDump && <Badge tone="green">uploaded: {uploadedDump.label}</Badge>}
          </div>
        )}

        <Field label="Target database">
          {target ? (
            <div className="rounded border border-slate-200 bg-slate-50 px-3 py-2 text-sm">
              {target.name} <Badge tone={target.engine === 'postgres' ? 'blue' : 'green'}>{target.engine}</Badge>
            </div>
          ) : (
            <Select value={targetId} onChange={(e) => setTargetId(e.target.value)}>
              <option value="">Select target…</option>
              {targets.map((db) => (
                <option key={db.id} value={db.id}>
                  {db.name} ({db.engine})
                </option>
              ))}
            </Select>
          )}
        </Field>

        {selectedTarget && (
          <div className="space-y-2 rounded border border-amber-300 bg-amber-50 p-3 text-sm text-amber-800">
            <p>
              Restoring will DROP existing objects in <b>{selectedTarget.name}</b> ({selectedTarget.dbName}) and replace
              them with the dump contents. This cannot be undone.
            </p>
            <Field label={`Type "${selectedTarget.name}" to confirm`}>
              <input
                className="w-full rounded border border-amber-300 px-2.5 py-1.5 text-sm"
                value={confirmName}
                onChange={(e) => setConfirmName(e.target.value)}
              />
            </Field>
          </div>
        )}
        {error && <p className="text-sm text-red-600">{error}</p>}
        <div className="flex justify-end">
          <Button onClick={onSubmit} disabled={busy || !effectiveDumpId || !targetId || !confirmOk}>
            {busy ? <Spinner /> : 'Restore now'}
          </Button>
        </div>
      </div>
    </Modal>
  )
}
