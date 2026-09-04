import { useEffect, useState } from 'react'
import type { FormEvent } from 'react'
import { api, ApiError } from '../lib/api'
import type { StorageDestinationDTO, TestResultDTO } from '../lib/types'
import { Button, Field, Input, Modal, Spinner } from '../ui'

interface Props {
  open: boolean
  onClose: () => void
  dest?: StorageDestinationDTO
  onSaved: () => void
}

interface FormState {
  name: string
  endpoint: string
  region: string
  bucket: string
  prefix: string
  accessKey: string
  secretKey: string
}

const emptyForm: FormState = {
  name: '', endpoint: '', region: '', bucket: '', prefix: '', accessKey: '', secretKey: '',
}

export default function DestinationModal({ open, onClose, dest, onSaved }: Props) {
  const [form, setForm] = useState<FormState>(emptyForm)
  const [error, setError] = useState('')
  const [testMsg, setTestMsg] = useState<{ ok: boolean; text: string } | null>(null)
  const [testing, setTesting] = useState(false)
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    if (!open) return
    setError('')
    setTestMsg(null)
    if (dest) {
      setForm({
        name: dest.name, endpoint: dest.endpoint, region: dest.region,
        bucket: dest.bucket, prefix: dest.prefix, accessKey: dest.accessKey, secretKey: '',
      })
    } else {
      setForm(emptyForm)
    }
  }, [open, dest])

  function set<K extends keyof FormState>(key: K, value: string) {
    setForm((f) => ({ ...f, [key]: value }))
  }

  async function onTest() {
    setTesting(true)
    setTestMsg(null)
    try {
      const res = await api.post<TestResultDTO>('/api/storage/destinations/test', form)
      setTestMsg(res.ok ? { ok: true, text: 'Connection OK' } : { ok: false, text: `Failed: ${res.error ?? 'unknown'}` })
    } catch (err) {
      setTestMsg({ ok: false, text: err instanceof ApiError ? err.message : 'test failed' })
    } finally {
      setTesting(false)
    }
  }

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    setError('')
    setSaving(true)
    try {
      if (dest) await api.put(`/api/storage/destinations/${dest.id}`, form)
      else await api.post('/api/storage/destinations', form)
      onSaved()
      onClose()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'save failed')
    } finally {
      setSaving(false)
    }
  }

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={dest ? `Edit ${dest.name}` : 'Add S3 Destination'}
      subtitle="Register an S3-compatible bucket (AWS S3, MinIO, R2, Spaces) as a dump destination."
    >
      <form onSubmit={onSubmit} className="space-y-4">
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <Field label="Display Name">
            <Input value={form.name} onChange={(e) => set('name', e.target.value)} placeholder="MinIO Offsite" required />
          </Field>
          <Field label="Endpoint URL" hint="e.g. http://127.0.0.1:9000">
            <Input value={form.endpoint} onChange={(e) => set('endpoint', e.target.value)} placeholder="https://s3.us-east-1.amazonaws.com" required />
          </Field>
          <Field label="Region">
            <Input value={form.region} onChange={(e) => set('region', e.target.value)} placeholder="us-east-1" />
          </Field>
          <Field label="Bucket Name">
            <Input value={form.bucket} onChange={(e) => set('bucket', e.target.value)} placeholder="my-db-backups" required />
          </Field>
          <Field label="Key Prefix (Optional)">
            <Input value={form.prefix} onChange={(e) => set('prefix', e.target.value)} placeholder="ku-dump/production" />
          </Field>
          <Field label="Access Key ID">
            <Input value={form.accessKey} onChange={(e) => set('accessKey', e.target.value)} required />
          </Field>
        </div>
        <Field label={dest ? 'Secret Access Key (leave blank to keep existing)' : 'Secret Access Key'}>
          <Input type="password" value={form.secretKey} onChange={(e) => set('secretKey', e.target.value)} required={!dest} />
        </Field>

        {error && (
          <div className="rounded-lg border border-rose-800/60 bg-rose-950/40 p-2.5 text-xs text-rose-300">
            {error}
          </div>
        )}
        {testMsg && (
          <div
            className={`rounded-lg border p-2.5 text-xs ${
              testMsg.ok ? 'border-emerald-800/60 bg-emerald-950/40 text-emerald-300' : 'border-rose-800/60 bg-rose-950/40 text-rose-300'
            }`}
          >
            {testMsg.text}
          </div>
        )}

        <div className="flex items-center justify-between border-t border-slate-800 pt-4">
          {!dest && (
            <Button type="button" variant="outline" onClick={onTest} disabled={testing}>
              {testing ? <Spinner /> : 'Test Connection'}
            </Button>
          )}
          {dest && <span className="text-[11px] text-slate-500">Test saved config from the row button.</span>}
          <div className="flex items-center gap-2">
            <Button type="button" variant="ghost" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" disabled={saving}>
              {saving ? <Spinner /> : 'Save Destination'}
            </Button>
          </div>
        </div>
      </form>
    </Modal>
  )
}
