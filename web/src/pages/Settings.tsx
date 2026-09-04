import { useEffect, useState } from 'react'
import type { FormEvent } from 'react'
import { useQuery } from '@tanstack/react-query'
import { CheckCircle2, Cloud, Eye, EyeOff, Terminal, AlertTriangle } from 'lucide-react'
import { api, ApiError } from '../lib/api'
import type { StorageSettingsDTO, TestResultDTO, ToolsDTO } from '../lib/types'
import { Badge, Button, Field, Input, Spinner } from '../ui'

export default function Settings() {
  const [endpoint, setEndpoint] = useState('')
  const [region, setRegion] = useState('')
  const [bucket, setBucket] = useState('')
  const [prefix, setPrefix] = useState('')
  const [accessKey, setAccessKey] = useState('')
  const [secretKey, setSecretKey] = useState('')
  const [showSecret, setShowSecret] = useState(false)
  const [secretSet, setSecretSet] = useState(false)
  const [msg, setMsg] = useState<{ type: 'success' | 'error'; text: string } | null>(null)
  const [testMsg, setTestMsg] = useState<{ type: 'success' | 'error'; text: string } | null>(null)
  const [busy, setBusy] = useState(false)

  const settings = useQuery({ queryKey: ['storage-settings'], queryFn: () => api.get<StorageSettingsDTO>('/api/settings/storage') })
  const tools = useQuery({ queryKey: ['tools'], queryFn: () => api.get<ToolsDTO>('/api/tools') })

  useEffect(() => {
    const s = settings.data
    if (!s) return
    setEndpoint(s.endpoint)
    setRegion(s.region)
    setBucket(s.bucket)
    setPrefix(s.prefix)
    setAccessKey(s.accessKey)
    setSecretSet(s.secretSet)
  }, [settings.data])

  async function onSave(e: FormEvent) {
    e.preventDefault()
    setMsg(null)
    setBusy(true)
    try {
      await api.put('/api/settings/storage', { endpoint, region, bucket, prefix, accessKey, secretKey })
      setSecretKey('')
      setMsg({ type: 'success', text: 'Storage settings saved successfully.' })
      await settings.refetch()
    } catch (err) {
      setMsg({ type: 'error', text: err instanceof ApiError ? err.message : 'Save failed' })
    } finally {
      setBusy(false)
    }
  }

  async function onTest() {
    setTestMsg(null)
    setBusy(true)
    try {
      const res = await api.post<TestResultDTO>('/api/settings/storage/test')
      setTestMsg(
        res.ok
          ? { type: 'success', text: 'S3 Storage connection verified successfully!' }
          : { type: 'error', text: `Connection Failed: ${res.error ?? 'unknown error'}` },
      )
    } catch (err) {
      setTestMsg({ type: 'error', text: err instanceof ApiError ? err.message : 'Test failed' })
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="max-w-3xl space-y-8">
      {/* Page Title */}
      <div>
        <h1 className="text-2xl font-bold tracking-tight text-white">System Settings</h1>
        <p className="mt-1 text-xs text-slate-400">
          Configure remote cloud storage providers and inspect host binary execution requirements.
        </p>
      </div>

      {/* S3 Storage Section */}
      <section className="rounded-2xl border border-slate-800 bg-slate-900/80 p-6 shadow-2xl backdrop-blur-md space-y-6">
        <div className="flex items-center gap-3 border-b border-slate-800 pb-4">
          <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-sky-500/10 text-sky-400 border border-sky-500/20">
            <Cloud size={20} />
          </div>
          <div>
            <h2 className="text-base font-bold text-slate-100">S3-Compatible Remote Storage</h2>
            <p className="text-xs text-slate-400">
              Configure AWS S3, MinIO, Cloudflare R2, or DigitalOcean Spaces to enable offsite backup copies.
            </p>
          </div>
        </div>

        <form onSubmit={onSave} className="space-y-4">
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <Field label="Endpoint URL" hint="e.g. http://127.0.0.1:9000 or https://s3.amazonaws.com">
              <Input
                value={endpoint}
                onChange={(e) => setEndpoint(e.target.value)}
                placeholder="https://s3.us-east-1.amazonaws.com"
              />
            </Field>
            <Field label="Region">
              <Input value={region} onChange={(e) => setRegion(e.target.value)} placeholder="us-east-1" />
            </Field>
            <Field label="Bucket Name">
              <Input value={bucket} onChange={(e) => setBucket(e.target.value)} placeholder="my-db-backups" />
            </Field>
            <Field label="Key Prefix (Optional)">
              <Input value={prefix} onChange={(e) => setPrefix(e.target.value)} placeholder="ku-dump/production" />
            </Field>
            <Field label="Access Key ID">
              <Input value={accessKey} onChange={(e) => setAccessKey(e.target.value)} placeholder="AKIAIOSFODNN7EXAMPLE" />
            </Field>
            <Field label={secretSet ? 'Secret Access Key (Set — leave blank to keep)' : 'Secret Access Key'}>
              <div className="relative">
                <Input
                  type={showSecret ? 'text' : 'password'}
                  value={secretKey}
                  onChange={(e) => setSecretKey(e.target.value)}
                  placeholder={secretSet ? '••••••••••••••••' : 'wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY'}
                  className="pr-10"
                />
                <button
                  type="button"
                  onClick={() => setShowSecret(!showSecret)}
                  className="absolute right-3 top-1/2 -translate-y-1/2 text-slate-500 hover:text-slate-300"
                >
                  {showSecret ? <EyeOff size={16} /> : <Eye size={16} />}
                </button>
              </div>
            </Field>
          </div>

          {msg && (
            <div
              className={`rounded-lg border p-3 text-xs ${
                msg.type === 'success' ? 'border-emerald-800/60 bg-emerald-950/40 text-emerald-300' : 'border-rose-800/60 bg-rose-950/40 text-rose-300'
              }`}
            >
              {msg.text}
            </div>
          )}

          {testMsg && (
            <div
              className={`rounded-lg border p-3 text-xs ${
                testMsg.type === 'success'
                  ? 'border-emerald-800/60 bg-emerald-950/40 text-emerald-300'
                  : 'border-rose-800/60 bg-rose-950/40 text-rose-300'
              }`}
            >
              {testMsg.text}
            </div>
          )}

          <div className="flex items-center gap-3 pt-2">
            <Button type="submit" disabled={busy}>
              {busy ? <Spinner /> : 'Save Storage Settings'}
            </Button>
            <Button type="button" variant="outline" onClick={onTest} disabled={busy || !settings.data?.configured}>
              {busy ? <Spinner /> : 'Test Storage Connection'}
            </Button>
          </div>
        </form>
      </section>

      {/* CLI Tools Section */}
      <section className="rounded-2xl border border-slate-800 bg-slate-900/80 p-6 shadow-2xl backdrop-blur-md space-y-4">
        <div className="flex items-center gap-3 border-b border-slate-800 pb-4">
          <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-violet-500/10 text-violet-400 border border-violet-500/20">
            <Terminal size={20} />
          </div>
          <div>
            <h2 className="text-base font-bold text-slate-100">Host CLI Tool Diagnostics</h2>
            <p className="text-xs text-slate-400">
              `ku-dump` delegates execution to native engine CLI tools installed on the host system.
            </p>
          </div>
        </div>

        <div className="space-y-3">
          <ToolRow name="PostgreSQL Tools" missing={tools.data?.postgres} pkgs="postgresql-client (pg_dump, pg_restore)" />
          <ToolRow name="MongoDB Tools" missing={tools.data?.mongodb} pkgs="mongodb-database-tools (mongodump, mongorestore)" />
        </div>
      </section>
    </div>
  )
}

function ToolRow({ name, missing, pkgs }: { name: string; missing?: string[]; pkgs: string }) {
  const ok = (missing?.length ?? 0) === 0
  const isLoading = missing === undefined

  return (
    <div className="flex items-center justify-between rounded-xl border border-slate-800 bg-slate-950/60 p-4">
      <div>
        <div className="font-semibold text-slate-100 text-sm">{name}</div>
        <div className="text-xs text-slate-400 mt-0.5">{pkgs}</div>
      </div>
      <div>
        {isLoading ? (
          <Spinner />
        ) : ok ? (
          <Badge tone="green">
            <CheckCircle2 size={12} className="inline mr-1" /> Installed & Ready
          </Badge>
        ) : (
          <Badge tone="red">
            <AlertTriangle size={12} className="inline mr-1" /> Missing: {missing?.join(', ')}
          </Badge>
        )}
      </div>
    </div>
  )
}

