import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Database, HardDriveDownload, Pencil, Play, Plus, Trash2 } from 'lucide-react'
import { api } from '../lib/api'
import type { DatabaseDTO, StorageSettingsDTO, TestResultDTO } from '../lib/types'
import { Badge, Button, Spinner } from '../ui'
import DatabaseModal from '../modals/DatabaseModal'
import DumpModal from '../modals/DumpModal'
import RestoreModal from '../modals/RestoreModal'

export default function Databases() {
  const qc = useQueryClient()
  const [editing, setEditing] = useState<DatabaseDTO | undefined>(undefined)
  const [showAdd, setShowAdd] = useState(false)
  const [dumpTarget, setDumpTarget] = useState<DatabaseDTO | null>(null)
  const [restoreTarget, setRestoreTarget] = useState<DatabaseDTO | null>(null)

  const dbs = useQuery({
    queryKey: ['databases'],
    queryFn: () => api.get<DatabaseDTO[]>('/api/databases'),
    refetchInterval: 3000,
  })
  const storage = useQuery({
    queryKey: ['storage-settings'],
    queryFn: () => api.get<StorageSettingsDTO>('/api/settings/storage'),
  })

  const remove = useMutation({
    mutationFn: (id: string) => api.del(`/api/databases/${id}`),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['databases'] }),
  })

  const test = useMutation({
    mutationFn: (id: string) => api.post<TestResultDTO>(`/api/databases/${id}/test`),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['databases'] }),
  })

  function invalidate() {
    void qc.invalidateQueries({ queryKey: ['databases'] })
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-bold">Databases</h1>
        <Button onClick={() => setShowAdd(true)}>
          <Plus size={16} /> Add database
        </Button>
      </div>
      {dbs.isLoading && <Spinner />}
      {dbs.data && dbs.data.length === 0 && (
        <p className="rounded border border-dashed border-slate-300 p-8 text-center text-slate-500">
          No databases registered yet. Add one to start dumping.
        </p>
      )}
      <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
        {dbs.data?.map((db) => (
          <div key={db.id} className="space-y-3 rounded-lg border border-slate-200 bg-white p-4 shadow-sm">
            <div className="flex items-start justify-between">
              <div>
                <div className="font-semibold">{db.name}</div>
                <div className="text-sm text-slate-500">
                  {db.host}:{db.port}/{db.dbName}
                </div>
              </div>
              <Badge tone={db.engine === 'postgres' ? 'blue' : 'green'}>{db.engine}</Badge>
            </div>
            <div className="flex items-center gap-2 text-xs text-slate-500">
              {db.toolsMissing.length > 0 ? (
                <Badge tone="red">missing tools: {db.toolsMissing.join(', ')}</Badge>
              ) : db.lastTestOk === null ? (
                <span>not tested yet</span>
              ) : db.lastTestOk ? (
                <Badge tone="green">connection OK</Badge>
              ) : (
                <Badge tone="red">connection failed</Badge>
              )}
              {db.hasActiveJob && <Badge tone="amber">job running</Badge>}
            </div>
            <div className="flex flex-wrap gap-2">
              <Button
                onClick={() => setDumpTarget(db)}
                disabled={db.hasActiveJob || db.toolsMissing.length > 0}
                title={db.hasActiveJob ? 'a job is already running' : undefined}
              >
                <HardDriveDownload size={15} /> Dump
              </Button>
              <Button
                variant="outline"
                onClick={() => setRestoreTarget(db)}
                disabled={db.hasActiveJob || db.toolsMissing.length > 0}
              >
                <Play size={15} /> Restore
              </Button>
              <Button
                variant="outline"
                onClick={() => test.mutate(db.id)}
                disabled={test.isPending && test.variables === db.id}
              >
                {test.isPending && test.variables === db.id ? <Spinner /> : <Database size={15} />} Test
              </Button>
              <Button variant="outline" onClick={() => setEditing(db)}>
                <Pencil size={15} /> Edit
              </Button>
              <Button
                variant="danger"
                onClick={() => {
                  if (confirm(`Delete registration "${db.name}"? Dump files are kept.`)) remove.mutate(db.id)
                }}
              >
                <Trash2 size={15} />
              </Button>
            </div>
          </div>
        ))}
      </div>
      <DatabaseModal open={showAdd} onClose={() => setShowAdd(false)} onSaved={invalidate} />
      <DatabaseModal open={editing !== undefined} onClose={() => setEditing(undefined)} db={editing} onSaved={invalidate} />
      {dumpTarget && (
        <DumpModal
          open
          onClose={() => setDumpTarget(null)}
          db={dumpTarget}
          storageConfigured={storage.data?.configured ?? false}
        />
      )}
      {restoreTarget && (
        <RestoreModal open onClose={() => setRestoreTarget(null)} target={restoreTarget} onStarted={() => invalidate()} />
      )}
    </div>
  )
}
