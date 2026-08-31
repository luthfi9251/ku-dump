import { useEffect, useState } from 'react'
import type { FormEvent } from 'react'
import { useQuery } from '@tanstack/react-query'
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
  const [secretSet, setSecretSet] = useState(false)
  const [msg, setMsg] = useState('')
  const [testMsg, setTestMsg] = useState('')
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
    setMsg('')
    setBusy(true)
    try {
      await api.put('/api/settings/storage', { endpoint, region, bucket, prefix, accessKey, secretKey })
      setSecretKey('')
      setMsg('Saved.')
      await settings.refetch()
    } catch (err) {
      setMsg(err instanceof ApiError ? err.message : 'save failed')
    } finally {
      setBusy(false)
    }
  }

  async function onTest() {
    setTestMsg('')
    setBusy(true)
    try {
      const res = await api.post<TestResultDTO>('/api/settings/storage/test')
      setTestMsg(res.ok ? 'Storage reachable.' : `Failed: ${res.error ?? 'unknown'}`)
    } catch (err) {
      setTestMsg(err instanceof ApiError ? err.message : 'test failed')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="max-w-2xl space-y-6">
      <section className="space-y-3 rounded-lg border border-slate-200 bg-white p-5">
        <h2 className="font-semibold">S3-compatible storage</h2>
        <p className="text-sm text-slate-500">
          Used when a dump is created with storage “S3”. Example endpoint: http://127.0.0.1:9000 (MinIO).
        </p>
        <form onSubmit={onSave} className="space-y-3">
          <div className="grid grid-cols-2 gap-3">
            <Field label="Endpoint">
              <Input value={endpoint} onChange={(e) => setEndpoint(e.target.value)} placeholder="https://s3.example.com" />
            </Field>
            <Field label="Region">
              <Input value={region} onChange={(e) => setRegion(e.target.value)} />
            </Field>
            <Field label="Bucket">
              <Input value={bucket} onChange={(e) => setBucket(e.target.value)} />
            </Field>
            <Field label="Key prefix (optional)">
              <Input value={prefix} onChange={(e) => setPrefix(e.target.value)} placeholder="ku-dump" />
            </Field>
            <Field label="Access key">
              <Input value={accessKey} onChange={(e) => setAccessKey(e.target.value)} />
            </Field>
            <Field label={secretSet ? 'Secret key (set — leave empty to keep)' : 'Secret key'}>
              <Input type="password" value={secretKey} onChange={(e) => setSecretKey(e.target.value)} />
            </Field>
          </div>
          {msg && <p className="text-sm text-slate-600">{msg}</p>}
          <div className="flex gap-2">
            <Button type="submit" disabled={busy}>
              {busy ? <Spinner /> : 'Save'}
            </Button>
            <Button type="button" variant="outline" onClick={onTest} disabled={busy || !settings.data?.configured}>
              Test connection
            </Button>
          </div>
          {testMsg && <p className="text-sm text-slate-600">{testMsg}</p>}
        </form>
      </section>

      <section className="space-y-2 rounded-lg border border-slate-200 bg-white p-5">
        <h2 className="font-semibold">CLI tools on this host</h2>
        <p className="text-sm text-slate-500">
          ku-dump shells out to the official tools. Install them (or set KUDUMP_* env overrides) per engine:
        </p>
        <div className="space-y-1 text-sm">
          <ToolRow name="postgres" missing={tools.data?.postgres} pkgs="postgresql-client (pg_dump, pg_restore)" />
          <ToolRow name="mongodb" missing={tools.data?.mongodb} pkgs="mongodb-database-tools (mongodump, mongorestore)" />
        </div>
      </section>
    </div>
  )
}

function ToolRow({ name, missing, pkgs }: { name: string; missing?: string[]; pkgs: string }) {
  const ok = (missing?.length ?? 0) === 0
  return (
    <div className="flex items-center justify-between gap-2">
      <div>
        <span className="font-medium">{name}</span> <span className="text-slate-400">— {pkgs}</span>
      </div>
      {toolsLoading(missing) ? <Spinner /> : ok ? <Badge tone="green">all tools available</Badge> : <Badge tone="red">missing: {missing?.join(', ')}</Badge>}
    </div>
  )
}

function toolsLoading(missing?: string[]): boolean {
  return missing === undefined
}
