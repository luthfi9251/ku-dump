import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { CheckCircle2, Cloud, HardDrive, KeyRound, Pencil, Plus, Server, Trash2 } from 'lucide-react'
import { api, ApiError } from '../lib/api'
import type { DestinationKind, StorageDestinationDTO, TestResultDTO } from '../lib/types'
import { Badge, Button, Spinner } from '../ui'
import DestinationModal from '../modals/DestinationModal'

export function kindIcon(kind: DestinationKind) {
  return kind === 's3' ? Cloud : kind === 'sftp' ? Server : HardDrive
}

export function kindSummary(d: StorageDestinationDTO): string {
  switch (d.kind) {
    case 's3':
      return `${d.endpoint} / ${d.bucket}${d.prefix ? ` (${d.prefix})` : ''}`
    case 'local':
      return d.rootPath
    case 'sftp':
      return `sftp://${d.username}@${d.host}:${d.port}${d.remoteDir && d.remoteDir !== '.' ? '/' + d.remoteDir : ''}`
  }
}

export default function Destinations() {
  const qc = useQueryClient()
  const [editing, setEditing] = useState<StorageDestinationDTO | undefined>(undefined)
  const [showAdd, setShowAdd] = useState(false)
  const [testMsg, setTestMsg] = useState<{ id: string; ok: boolean; text: string } | null>(null)
  const [deleteErr, setDeleteErr] = useState('')

  const dests = useQuery({
    queryKey: ['storage-destinations'],
    queryFn: () => api.get<StorageDestinationDTO[]>('/api/storage/destinations'),
  })

  const invalidate = () => void qc.invalidateQueries({ queryKey: ['storage-destinations'] })

  const remove = useMutation({
    mutationFn: (id: string) => api.del(`/api/storage/destinations/${id}`),
    onSuccess: () => { setDeleteErr(''); invalidate() },
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

  const rows = dests.data ?? []

  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-white">Backup Destinations</h1>
          <p className="mt-1 text-xs text-slate-400">
            Reusable storage targets — local folders, S3 buckets, or remote servers over SFTP. Assign them to workflows.
          </p>
        </div>
        <Button onClick={() => setShowAdd(true)} className="shrink-0">
          <Plus size={16} /> New Destination
        </Button>
      </div>

      {deleteErr && (
        <div className="rounded-lg border border-rose-800/60 bg-rose-950/40 p-3 text-xs text-rose-300">{deleteErr}</div>
      )}

      {dests.isLoading && (
        <div className="flex justify-center py-12"><Spinner className="h-6 w-6" /></div>
      )}

      {!dests.isLoading && rows.length === 0 && (
        <div className="rounded-2xl border border-dashed border-slate-800 bg-slate-900/60 p-12 text-center text-sm text-slate-500">
          No destinations yet. The built-in local dumps folder is always available; add more here.
        </div>
      )}

      <div className="grid gap-4 lg:grid-cols-2">
        {rows.map((d) => {
          const Icon = kindIcon(d.kind)
          return (
            <div key={d.id} className="rounded-2xl border border-slate-800 bg-slate-900/80 p-5 shadow-xl backdrop-blur-md">
              <div className="flex items-start justify-between gap-3">
                <div className="flex min-w-0 items-center gap-3">
                  <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-indigo-500/10 text-indigo-400 border border-indigo-500/20">
                    <Icon size={20} />
                  </div>
                  <div className="min-w-0">
                    <div className="flex items-center gap-2">
                      <span className="truncate font-semibold text-slate-100">{d.name}</span>
                      <Badge tone={d.kind === 's3' ? 'indigo' : d.kind === 'sftp' ? 'violet' : 'blue'}>{d.kind}</Badge>
                      {d.secretSet && (
                        <Badge tone="green"><KeyRound size={12} className="inline mr-1" /> secret set</Badge>
                      )}
                    </div>
                    <div className="mt-0.5 truncate font-mono text-xs text-slate-400">{kindSummary(d)}</div>
                  </div>
                </div>
                <div className="flex shrink-0 items-center gap-1">
                  <Button size="sm" variant="ghost" onClick={() => testDest(d)} title="Test Connection">
                    {testMsg?.id === d.id && testMsg.text === 'testing…' ? <Spinner className="h-3.5 w-3.5" /> : <CheckCircle2 size={14} />}
                  </Button>
                  <Button size="sm" variant="ghost" onClick={() => setEditing(d)} title="Edit"><Pencil size={14} /></Button>
                  <Button
                    size="sm" variant="ghost" className="hover:text-rose-400"
                    onClick={() => {
                      if (confirm(`Delete destination "${d.name}"? Destinations still referenced by dumps or workflows cannot be deleted.`))
                        remove.mutate(d.id)
                    }}
                    title="Delete"
                  >
                    <Trash2 size={14} />
                  </Button>
                </div>
              </div>
              {testMsg?.id === d.id && testMsg.text !== 'testing…' && (
                <div className={`mt-3 rounded-lg border p-2.5 text-xs ${
                  testMsg.ok ? 'border-emerald-800/60 bg-emerald-950/40 text-emerald-300' : 'border-rose-800/60 bg-rose-950/40 text-rose-300'
                }`}>
                  {testMsg.text}
                </div>
              )}
            </div>
          )
        })}
      </div>

      {showAdd && <DestinationModal open onClose={() => setShowAdd(false)} onSaved={invalidate} />}
      {editing && <DestinationModal open dest={editing} onClose={() => setEditing(undefined)} onSaved={invalidate} />}
    </div>
  )
}
