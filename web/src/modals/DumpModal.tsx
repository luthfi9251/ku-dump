import { useState } from 'react'
import type { FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'
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
    <Modal open={open} onClose={onClose} title={`Dump ${db.name}`}>
      <form onSubmit={onSubmit} className="space-y-3">
        <Field label="Label (optional)">
          <Input value={label} onChange={(e) => setLabel(e.target.value)} placeholder={`${db.name} manual`} />
        </Field>
        <Field label="Storage">
          <div className="flex gap-3 text-sm">
            <label className="flex items-center gap-1.5">
              <input type="radio" checked={storage === 'local'} onChange={() => setStorage('local')} />
              Local server
            </label>
            <label className={`flex items-center gap-1.5 ${storageConfigured ? '' : 'text-slate-400'}`}>
              <input
                type="radio"
                checked={storage === 's3'}
                onChange={() => storageConfigured && setStorage('s3')}
                disabled={!storageConfigured}
              />
              S3 {storageConfigured ? '' : '(not configured)'}
            </label>
          </div>
        </Field>
        {error && <p className="text-sm text-red-600">{error}</p>}
        <div className="flex justify-end pt-2">
          <Button type="submit" disabled={busy}>
            {busy ? <Spinner /> : 'Start dump'}
          </Button>
        </div>
      </form>
    </Modal>
  )
}
