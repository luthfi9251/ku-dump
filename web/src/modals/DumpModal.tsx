import { useState } from 'react'
import type { FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { Cloud, HardDrive } from 'lucide-react'
import { api, ApiError } from '../lib/api'
import type { DatabaseDTO, StorageDestinationDTO } from '../lib/types'
import { Button, Field, Input, Modal, Spinner } from '../ui'

export default function DumpModal({
  open,
  onClose,
  db,
}: {
  open: boolean
  onClose: () => void
  db: DatabaseDTO
}) {
  const [label, setLabel] = useState('')
  const [destId, setDestId] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const navigate = useNavigate()

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
      const res = await api.post<{ jobId: string }>(`/api/databases/${db.id}/dump`, { label, destId })
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
          <div className="grid gap-3 pt-1 sm:grid-cols-2">
            <DestCard
              active={destId === ''}
              title="Local Storage"
              subtitle="Save on server disk"
              icon={<HardDrive size={18} className={destId === '' ? 'text-indigo-400' : 'text-slate-400'} />}
              onClick={() => setDestId('')}
            />
            {(dests.data ?? []).map((d) => (
              <DestCard
                key={d.id}
                active={destId === d.id}
                title={d.name}
                subtitle={d.bucket}
                icon={<Cloud size={18} className={destId === d.id ? 'text-indigo-400' : 'text-slate-400'} />}
                onClick={() => setDestId(d.id)}
              />
            ))}
          </div>
          {dests.data && dests.data.length === 0 && (
            <p className="mt-2 text-[11px] text-slate-500">
              No S3 destinations configured — add one in Settings, or dump to local disk.
            </p>
          )}
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

function DestCard({
  active,
  title,
  subtitle,
  icon,
  onClick,
}: {
  active: boolean
  title: string
  subtitle: string
  icon: React.ReactNode
  onClick: () => void
}) {
  return (
    <label
      className={`flex cursor-pointer flex-col rounded-xl border p-4 transition-all ${
        active
          ? 'border-indigo-500 bg-indigo-950/40 text-indigo-200 shadow-md shadow-indigo-600/10'
          : 'border-slate-800 bg-slate-950/60 text-slate-400 hover:border-slate-700 hover:text-slate-200'
      }`}
      onClick={onClick}
    >
      <div className="flex items-center gap-2 font-semibold text-sm">
        <input type="radio" checked={active} onChange={onClick} className="hidden" />
        {icon}
        {title}
      </div>
      <span className="mt-1 truncate text-[11px] text-slate-500">{subtitle}</span>
    </label>
  )
}
