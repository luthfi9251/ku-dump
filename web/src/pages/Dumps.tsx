import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Download, Play, Trash2 } from 'lucide-react'
import { api } from '../lib/api'
import type { DatabaseDTO, DumpDTO, Engine } from '../lib/types'
import { Badge, Button, Select, Spinner } from '../ui'
import RestoreModal from '../modals/RestoreModal'

function fmtSize(bytes: number): string {
  if (bytes <= 0) return '—'
  const units = ['B', 'KB', 'MB', 'GB']
  let v = bytes
  let u = 0
  while (v >= 1024 && u < units.length - 1) {
    v /= 1024
    u++
  }
  return `${v.toFixed(1)} ${units[u]}`
}

export default function Dumps() {
  const qc = useQueryClient()
  const [engineFilter, setEngineFilter] = useState('')
  const [dbFilter, setDbFilter] = useState('')
  const [restoreSource, setRestoreSource] = useState<DumpDTO | null>(null)

  const dumps = useQuery({ queryKey: ['dumps'], queryFn: () => api.get<DumpDTO[]>('/api/dumps'), refetchInterval: 3000 })
  const dbs = useQuery({ queryKey: ['databases'], queryFn: () => api.get<DatabaseDTO[]>('/api/databases') })

  const remove = useMutation({
    mutationFn: (id: string) => api.del(`/api/dumps/${id}`),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['dumps'] }),
  })

  const rows = (dumps.data ?? []).filter(
    (d) => (!engineFilter || d.engine === engineFilter) && (!dbFilter || d.databaseId === dbFilter),
  )

  return (
    <div className="space-y-4">
      <h1 className="text-xl font-bold">Dumps</h1>
      <div className="flex gap-3">
        <Select value={engineFilter} onChange={(e) => setEngineFilter(e.target.value as Engine | '')} className="w-44">
          <option value="">All engines</option>
          <option value="postgres">postgres</option>
          <option value="mongodb">mongodb</option>
        </Select>
        <Select value={dbFilter} onChange={(e) => setDbFilter(e.target.value)} className="w-56">
          <option value="">All databases</option>
          {dbs.data?.map((db) => (
            <option key={db.id} value={db.id}>
              {db.name}
            </option>
          ))}
        </Select>
      </div>
      <div className="overflow-x-auto rounded-lg border border-slate-200 bg-white">
        <table className="w-full text-sm">
          <thead className="border-b border-slate-200 text-left text-slate-500">
            <tr>
              <th className="p-3">Label</th>
              <th className="p-3">Database</th>
              <th className="p-3">Engine</th>
              <th className="p-3">Storage</th>
              <th className="p-3">Size</th>
              <th className="p-3">Status</th>
              <th className="p-3">Created</th>
              <th className="p-3"></th>
            </tr>
          </thead>
          <tbody>
            {rows.map((d) => (
              <tr key={d.id} className="border-b border-slate-100 last:border-0">
                <td className="p-3 font-medium">{d.label}</td>
                <td className="p-3">{d.databaseName || <span className="text-slate-400">upload</span>}</td>
                <td className="p-3">
                  <Badge tone={d.engine === 'postgres' ? 'blue' : 'green'}>{d.engine}</Badge>
                </td>
                <td className="p-3">{d.storage}</td>
                <td className="p-3">{fmtSize(d.sizeBytes)}</td>
                <td className="p-3">
                  {d.status === 'pending' ? (
                    <Spinner />
                  ) : (
                    <Badge tone={d.status === 'ready' || d.status === 'uploaded' ? 'green' : d.status === 'failed' ? 'red' : 'slate'}>
                      {d.status}
                    </Badge>
                  )}
                </td>
                <td className="p-3 text-slate-500">{new Date(d.createdAt).toLocaleString()}</td>
                <td className="p-3">
                  <div className="flex justify-end gap-1.5">
                    <Button
                      variant="outline"
                      onClick={() => window.open(`/api/dumps/${d.id}/download`, '_blank')}
                      disabled={d.status !== 'ready' && d.status !== 'uploaded'}
                    >
                      <Download size={15} />
                    </Button>
                    <Button
                      variant="outline"
                      onClick={() => setRestoreSource(d)}
                      disabled={d.status !== 'ready' && d.status !== 'uploaded'}
                    >
                      <Play size={15} /> Restore
                    </Button>
                    <Button
                      variant="danger"
                      disabled={d.status === 'pending'}
                      onClick={() => {
                        if (confirm(`Delete dump "${d.label}"? The stored file will be removed.`)) remove.mutate(d.id)
                      }}
                    >
                      <Trash2 size={15} />
                    </Button>
                  </div>
                </td>
              </tr>
            ))}
            {rows.length === 0 && (
              <tr>
                <td colSpan={8} className="p-6 text-center text-slate-500">
                  No dumps yet.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
      {restoreSource && (
        <RestoreModal
          open
          onClose={() => setRestoreSource(null)}
          source={restoreSource}
          onStarted={() => void qc.invalidateQueries({ queryKey: ['dumps'] })}
        />
      )}
    </div>
  )
}
