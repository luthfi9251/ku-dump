import { useState } from 'react'
import type { FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { Cloud, HardDrive } from 'lucide-react'
import { api, ApiError } from '../lib/api'
import type { DatabaseDTO } from '../lib/types'
import { Button, Field, Input, Modal, Spinner } from '../ui'

export default function DumpModal({
  open,
  onClose,
  db,
  storageConfigured,
}: {
  open: boolean
  onClose: () => void
  db: DatabaseDTO
  storageConfigured: boolean
}) {
  const [label, setLabel] = useState('')
  const [storage, setStorage] = useState<'local' | 's3'>('local')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const navigate = useNavigate()

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    setError('')
    setBusy(true)
    try {
      const res = await api.post<{ jobId: string }>(`/api/databases/${db.id}/dump`, { label, storage })
      onClose()
      navigate(`/jobs?job=${res.jobId}`)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'dump failed')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={`Create Dump — ${db.name}`}
      subtitle={`Trigger manual snapshot dump for ${db.engine} instance (${db.dbName}).`}
    >
      <form onSubmit={onSubmit} className="space-y-5">
        <Field label="Dump Label (Optional)" hint="Unique tag to identify this snapshot">
          <Input value={label} onChange={(e) => setLabel(e.target.value)} placeholder={`${db.name}-backup-${new Date().toISOString().slice(0,10)}`} />
        </Field>

        <Field label="Target Destination Storage">
          <div className="grid grid-cols-2 gap-3 pt-1">
            <label
              className={`flex cursor-pointer flex-col rounded-xl border p-4 transition-all ${
                storage === 'local'
                  ? 'border-indigo-500 bg-indigo-950/40 text-indigo-200 shadow-md shadow-indigo-600/10'
                  : 'border-slate-800 bg-slate-950/60 text-slate-400 hover:border-slate-700 hover:text-slate-200'
              }`}
              onClick={() => setStorage('local')}
            >
              <div className="flex items-center gap-2 font-semibold text-sm">
                <input type="radio" checked={storage === 'local'} onChange={() => setStorage('local')} className="hidden" />
                <HardDrive size={18} className={storage === 'local' ? 'text-indigo-400' : 'text-slate-400'} />
                Local Storage
              </div>
              <span className="mt-1 text-[11px] text-slate-500">Save on server disk</span>
            </label>

            <label
              className={`flex cursor-pointer flex-col rounded-xl border p-4 transition-all ${
                !storageConfigured
                  ? 'cursor-not-allowed border-slate-800/40 bg-slate-950/20 opacity-50 text-slate-600'
                  : storage === 's3'
                  ? 'border-indigo-500 bg-indigo-950/40 text-indigo-200 shadow-md shadow-indigo-600/10'
                  : 'border-slate-800 bg-slate-950/60 text-slate-400 hover:border-slate-700 hover:text-slate-200'
              }`}
              onClick={() => storageConfigured && setStorage('s3')}
            >
              <div className="flex items-center gap-2 font-semibold text-sm">
                <input
                  type="radio"
                  checked={storage === 's3'}
                  onChange={() => storageConfigured && setStorage('s3')}
                  disabled={!storageConfigured}
                  className="hidden"
                />
                <Cloud size={18} className={storage === 's3' ? 'text-indigo-400' : 'text-slate-400'} />
                S3 Cloud Storage
              </div>
              <span className="mt-1 text-[11px] text-slate-500">
                {storageConfigured ? 'Offsite S3 Bucket' : 'S3 not configured'}
              </span>
            </label>
          </div>
        </Field>

        {error && (
          <div className="rounded-lg border border-rose-800/60 bg-rose-950/40 p-2.5 text-xs text-rose-300">
            {error}
          </div>
        )}

        <div className="flex justify-end gap-2 border-t border-slate-800 pt-4">
          <Button type="button" variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" disabled={busy} className="shadow-lg shadow-indigo-600/20">
            {busy ? <Spinner /> : 'Start Dump Process'}
          </Button>
        </div>
      </form>
    </Modal>
  )
}

