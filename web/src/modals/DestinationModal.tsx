import { useEffect, useState } from 'react'
import type { FormEvent } from 'react'
import { Cloud, HardDrive, Server } from 'lucide-react'
import { api, ApiError } from '../lib/api'
import type { DestinationKind, StorageDestinationDTO, TestResultDTO } from '../lib/types'
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
  rootPath: string
  host: string
  port: string
  username: string
  authType: 'password' | 'key'
  remoteDir: string
}

const emptyForm: FormState = {
  name: '', endpoint: '', region: '', bucket: '', prefix: '', accessKey: '', secretKey: '',
  rootPath: '', host: '', port: '22', username: '', authType: 'password', remoteDir: '',
}

const kinds: { id: DestinationKind; label: string; blurb: string; icon: typeof Cloud }[] = [
  { id: 'local', label: 'Local Folder', blurb: 'Folder di server ini', icon: HardDrive },
  { id: 's3', label: 'S3 Bucket', blurb: 'S3-compatible (MinIO, R2, S3)', icon: Cloud },
  { id: 'sftp', label: 'Remote Server', blurb: 'Kirim via SFTP/SSH', icon: Server },
]

export default function DestinationModal({ open, onClose, dest, onSaved }: Props) {
  const [kind, setKind] = useState<DestinationKind>('local')
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
      setKind(dest.kind)
      setForm({
        name: dest.name, endpoint: dest.endpoint, region: dest.region,
        bucket: dest.bucket, prefix: dest.prefix, accessKey: dest.accessKey, secretKey: '',
        rootPath: dest.rootPath, host: dest.host, port: String(dest.port || 22),
        username: dest.username, authType: (dest.authType as FormState['authType']) || 'password',
        remoteDir: dest.remoteDir,
      })
    } else {
      setKind('local')
      setForm(emptyForm)
    }
  }, [open, dest])

  function set<K extends keyof FormState>(key: K, value: FormState[K]) {
    setForm((f) => ({ ...f, [key]: value }))
  }

  function body(): Record<string, unknown> {
    const base = { name: form.name.trim(), kind }
    if (kind === 's3')
      return { ...base, endpoint: form.endpoint, region: form.region, bucket: form.bucket,
        prefix: form.prefix, accessKey: form.accessKey, secretKey: form.secretKey }
    if (kind === 'local') return { ...base, rootPath: form.rootPath.trim() }
    return { ...base, host: form.host, port: Number(form.port) || 22, username: form.username,
      authType: form.authType, secretKey: form.secretKey, remoteDir: form.remoteDir }
  }

  async function onTest() {
    setTesting(true)
    setTestMsg(null)
    try {
      const res = await api.post<TestResultDTO>('/api/storage/destinations/test', body())
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
      if (dest) await api.put(`/api/storage/destinations/${dest.id}`, body())
      else await api.post('/api/storage/destinations', body())
      onSaved()
      onClose()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'save failed')
    } finally {
      setSaving(false)
    }
  }

  const secretLabel =
    kind === 'sftp' ? (form.authType === 'key' ? 'Private Key' : 'Password')
    : 'Secret Access Key'

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={dest ? `Edit Destination — ${dest.name}` : 'New Destination'}
      subtitle="Destinations are reusable: assign them to any workflow."
      wide
    >
      <form onSubmit={onSubmit} className="space-y-4">
        {!dest && (
          <Field label="Type">
            <div className="grid gap-3 sm:grid-cols-3">
              {kinds.map((k) => {
                const Icon = k.icon
                const active = kind === k.id
                return (
                  <button
                    key={k.id}
                    type="button"
                    onClick={() => { setKind(k.id); setTestMsg(null); setError('') }}
                    className={`flex flex-col items-start gap-1 rounded-xl border p-3 text-left transition-all ${
                      active
                        ? 'border-indigo-500 bg-indigo-950/40 text-indigo-200'
                        : 'border-slate-800 bg-slate-950/60 text-slate-400 hover:border-slate-700'
                    }`}
                  >
                    <Icon size={18} className={active ? 'text-indigo-400' : 'text-slate-500'} />
                    <span className="text-sm font-semibold">{k.label}</span>
                    <span className="text-[11px] text-slate-500">{k.blurb}</span>
                  </button>
                )
              })}
            </div>
          </Field>
        )}

        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <Field label="Display Name">
            <Input value={form.name} onChange={(e) => set('name', e.target.value)} placeholder="offsite-backups" required />
          </Field>

          {kind === 's3' && (
            <>
              <Field label="Endpoint URL" hint="e.g. http://127.0.0.1:9000">
                <Input value={form.endpoint} onChange={(e) => set('endpoint', e.target.value)} required />
              </Field>
              <Field label="Region">
                <Input value={form.region} onChange={(e) => set('region', e.target.value)} placeholder="us-east-1" />
              </Field>
              <Field label="Bucket Name">
                <Input value={form.bucket} onChange={(e) => set('bucket', e.target.value)} required />
              </Field>
              <Field label="Key Prefix (Optional)">
                <Input value={form.prefix} onChange={(e) => set('prefix', e.target.value)} placeholder="ku-dump/production" />
              </Field>
              <Field label="Access Key ID">
                <Input value={form.accessKey} onChange={(e) => set('accessKey', e.target.value)} required />
              </Field>
            </>
          )}

          {kind === 'local' && (
            <Field label="Root Folder" hint="Absolute path, e.g. /var/backups/pg — created if missing">
              <Input value={form.rootPath} onChange={(e) => set('rootPath', e.target.value)} placeholder="/var/backups/pg" className="font-mono" required />
            </Field>
          )}

          {kind === 'sftp' && (
            <>
              <Field label="Host">
                <Input value={form.host} onChange={(e) => set('host', e.target.value)} placeholder="backup.example.com" required />
              </Field>
              <Field label="Port">
                <Input type="number" value={form.port} onChange={(e) => set('port', e.target.value)} placeholder="22" />
              </Field>
              <Field label="Username">
                <Input value={form.username} onChange={(e) => set('username', e.target.value)} required />
              </Field>
              <Field label="Auth Type">
                <div className="flex gap-2">
                  {(['password', 'key'] as const).map((a) => (
                    <button key={a} type="button" onClick={() => set('authType', a)}
                      className={`flex-1 rounded-lg border px-3 py-2 text-sm transition-all ${
                        form.authType === a
                          ? 'border-indigo-500 bg-indigo-950/40 text-indigo-200'
                          : 'border-slate-800 bg-slate-950/60 text-slate-400 hover:border-slate-700'
                      }`}>
                      {a === 'password' ? 'Password' : 'Private Key'}
                    </button>
                  ))}
                </div>
              </Field>
              <Field label="Remote Dir (Optional)" hint="Created on the server if missing">
                <Input value={form.remoteDir} onChange={(e) => set('remoteDir', e.target.value)} placeholder="/srv/dumps" className="font-mono" />
              </Field>
            </>
          )}
        </div>

        {kind !== 'local' && (
          <Field label={`${secretLabel}${dest ? ' (leave blank to keep existing)' : ''}`}>
            {kind === 'sftp' && form.authType === 'key' ? (
              <textarea
                className="min-h-28 w-full rounded-lg border border-slate-700 bg-slate-950 px-3 py-2 font-mono text-xs text-slate-200 focus:border-indigo-500 focus:outline-none"
                value={form.secretKey}
                onChange={(e) => set('secretKey', e.target.value)}
                placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
                required={!dest}
              />
            ) : (
              <Input
                type={kind === 'sftp' && form.authType === 'password' ? 'password' : 'password'}
                value={form.secretKey}
                onChange={(e) => set('secretKey', e.target.value)}
                required={!dest}
              />
            )}
          </Field>
        )}

        {error && (
          <div className="rounded-lg border border-rose-800/60 bg-rose-950/40 p-2.5 text-xs text-rose-300">{error}</div>
        )}
        {testMsg && (
          <div className={`rounded-lg border p-2.5 text-xs ${
            testMsg.ok ? 'border-emerald-800/60 bg-emerald-950/40 text-emerald-300' : 'border-rose-800/60 bg-rose-950/40 text-rose-300'
          }`}>
            {testMsg.text}
          </div>
        )}

        <div className="flex items-center justify-between border-t border-slate-800 pt-4">
          <Button type="button" variant="outline" onClick={onTest} disabled={testing}>
            {testing ? <Spinner /> : 'Test Connection'}
          </Button>
          <div className="flex items-center gap-2">
            <Button type="button" variant="ghost" onClick={onClose}>Cancel</Button>
            <Button type="submit" disabled={saving}>
              {saving ? <Spinner /> : dest ? 'Save Changes' : 'Create Destination'}
            </Button>
          </div>
        </div>
      </form>
    </Modal>
  )
}
