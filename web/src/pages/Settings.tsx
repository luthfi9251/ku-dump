import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AlertTriangle, CheckCircle2, Cloud, KeyRound, Pencil, Plus, Terminal, Trash2 } from 'lucide-react'
import { api, ApiError } from '../lib/api'
import type { StorageDestinationDTO, TestResultDTO, ToolsDTO } from '../lib/types'
import { Badge, Button, Spinner } from '../ui'
import DestinationModal from '../modals/DestinationModal'

export default function Settings() {
  const qc = useQueryClient()
  const [editing, setEditing] = useState<StorageDestinationDTO | undefined>(undefined)
  const [showAdd, setShowAdd] = useState(false)
  const [testMsg, setTestMsg] = useState<{ id: string; ok: boolean; text: string } | null>(null)
  const [deleteErr, setDeleteErr] = useState('')

  const dests = useQuery({
    queryKey: ['storage-destinations'],
    queryFn: () => api.get<StorageDestinationDTO[]>('/api/storage/destinations'),
  })
  const tools = useQuery({ queryKey: ['tools'], queryFn: () => api.get<ToolsDTO>('/api/tools') })

  const invalidate = () => void qc.invalidateQueries({ queryKey: ['storage-destinations'] })

  const remove = useMutation({
    mutationFn: (id: string) => api.del(`/api/storage/destinations/${id}`),
    onSuccess: () => {
      setDeleteErr('')
      invalidate()
    },
    onError: (err) => setDeleteErr(err instanceof ApiError ? err.message : 'delete failed'),
  })

  async function testDest(d: StorageDestinationDTO) {
    setTestMsg({ id: d.id, ok: true, text: 'testing…' })
    try {
      const res = await api.post<TestResultDTO>(`/api/storage/destinations/${d.id}/test`, {})
      setTestMsg({ id: d.id, ok: res.ok, text: res.ok ? 'Connection OK' : `Failed: ${res.error ?? 'unknown'}` })
    } catch (err) {
      setTestMsg({ id: d.id, ok: false, text: err instanceof ApiError ? err.message : 'test failed' })
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

      {/* Storage Destinations Section */}
      <section className="rounded-2xl border border-slate-800 bg-slate-900/80 p-6 shadow-2xl backdrop-blur-md space-y-6">
        <div className="flex items-center justify-between gap-3 border-b border-slate-800 pb-4">
          <div className="flex items-center gap-3">
            <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-sky-500/10 text-sky-400 border border-sky-500/20">
              <Cloud size={20} />
            </div>
            <div>
              <h2 className="text-base font-bold text-slate-100">Storage Destinations</h2>
              <p className="text-xs text-slate-400">
                Manage S3-compatible offsite destinations (AWS S3, MinIO, R2, Spaces). Local disk is always available.
              </p>
            </div>
          </div>
          <Button size="sm" onClick={() => setShowAdd(true)} className="shrink-0">
            <Plus size={14} /> Add Destination
          </Button>
        </div>

        {deleteErr && (
          <div className="rounded-lg border border-rose-800/60 bg-rose-950/40 p-3 text-xs text-rose-300">{deleteErr}</div>
        )}

        {dests.isLoading && (
          <div className="flex justify-center py-6">
            <Spinner className="h-6 w-6" />
          </div>
        )}

        {!dests.isLoading && (dests.data ?? []).length === 0 && (
          <div className="rounded-xl border border-dashed border-slate-800 bg-slate-950/40 p-8 text-center text-xs text-slate-500">
            No S3 destinations yet. Add one to store dumps offsite.
          </div>
        )}

        <div className="space-y-3">
          {(dests.data ?? []).map((d) => (
            <div key={d.id} className="rounded-xl border border-slate-800 bg-slate-950/60 p-4">
              <div className="flex items-start justify-between gap-3">
                <div className="min-w-0">
                  <div className="flex items-center gap-2">
                    <Cloud size={15} className="shrink-0 text-sky-400" />
                    <span className="truncate text-sm font-semibold text-slate-100">{d.name}</span>
                    <Badge tone="slate">{d.kind}</Badge>
                    {d.secretSet ? (
                      <Badge tone="green">
                        <KeyRound size={12} className="inline mr-1" /> secret set
                      </Badge>
                    ) : (
                      <Badge tone="amber">no secret</Badge>
                    )}
                  </div>
                  <div className="mt-1 truncate font-mono text-xs text-slate-400">
                    {d.endpoint} / {d.bucket}
                    {d.prefix ? ` (${d.prefix})` : ''}
                  </div>
                  <div className="mt-0.5 truncate text-[11px] text-slate-500">AK: {d.accessKey}</div>
                </div>
                <div className="flex shrink-0 items-center gap-1">
                  <Button size="sm" variant="ghost" onClick={() => testDest(d)} title="Test Connection">
                    {testMsg?.id === d.id && testMsg.text === 'testing…' ? <Spinner className="h-3.5 w-3.5" /> : <CheckCircle2 size={14} />}
                  </Button>
                  <Button size="sm" variant="ghost" onClick={() => setEditing(d)} title="Edit Destination">
                    <Pencil size={14} />
                  </Button>
                  <Button
                    size="sm"
                    variant="ghost"
                    className="hover:text-rose-400"
                    onClick={() => {
                      if (confirm(`Delete destination "${d.name}"? Destinations with stored dumps cannot be deleted.`))
                        remove.mutate(d.id)
                    }}
                    title="Delete Destination"
                  >
                    <Trash2 size={14} />
                  </Button>
                </div>
              </div>
              {testMsg?.id === d.id && testMsg.text !== 'testing…' && (
                <div
                  className={`mt-3 rounded-lg border p-2.5 text-xs ${
                    testMsg.ok ? 'border-emerald-800/60 bg-emerald-950/40 text-emerald-300' : 'border-rose-800/60 bg-rose-950/40 text-rose-300'
                  }`}
                >
                  {testMsg.text}
                </div>
              )}
            </div>
          ))}
        </div>
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

      <DestinationModal open={showAdd} onClose={() => setShowAdd(false)} onSaved={invalidate} />
      <DestinationModal open={editing !== undefined} onClose={() => setEditing(undefined)} dest={editing} onSaved={invalidate} />
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
