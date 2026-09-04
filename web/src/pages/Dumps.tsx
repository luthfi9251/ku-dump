import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Archive, Cloud, Download, HardDrive, Play, Search, Trash2 } from 'lucide-react'
import { api } from '../lib/api'
import type { DatabaseDTO, DumpDTO, Engine, StorageDestinationDTO } from '../lib/types'
import { Badge, Button, Input, Select, StatCard } from '../ui'
import RestoreModal from '../modals/RestoreModal'

function fmtSize(bytes: number): string {
  if (bytes <= 0) return '—'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
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
  const [search, setSearch] = useState('')
  const [engineFilter, setEngineFilter] = useState('')
  const [dbFilter, setDbFilter] = useState('')
  const [storageFilter, setStorageFilter] = useState('')
  const [restoreSource, setRestoreSource] = useState<DumpDTO | null>(null)

  const dumps = useQuery({ queryKey: ['dumps'], queryFn: () => api.get<DumpDTO[]>('/api/dumps'), refetchInterval: 3000 })
  const dbs = useQuery({ queryKey: ['databases'], queryFn: () => api.get<DatabaseDTO[]>('/api/databases') })
  const dests = useQuery({ queryKey: ['storage-destinations'], queryFn: () => api.get<StorageDestinationDTO[]>('/api/storage/destinations') })

  const remove = useMutation({
    mutationFn: (id: string) => api.del(`/api/dumps/${id}`),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['dumps'] }),
  })

  const allDumps = dumps.data ?? []
  const totalSizeBytes = allDumps.reduce((acc, d) => acc + (d.sizeBytes || 0), 0)
  const readyCount = allDumps.filter((d) => d.status === 'ready' || d.status === 'uploaded').length
  const s3Count = allDumps.filter((d) => d.destId !== '').length

  const rows = allDumps.filter(
    (d) =>
      (!engineFilter || d.engine === engineFilter) &&
      (!dbFilter || d.databaseId === dbFilter) &&
      (!storageFilter || (storageFilter === 'local' ? d.destId === '' : d.destId === storageFilter)) &&
      (!search || d.label.toLowerCase().includes(search.toLowerCase()) || (d.databaseName && d.databaseName.toLowerCase().includes(search.toLowerCase()))),
  )

  return (
    <div className="space-y-6">
      {/* Page Title */}
      <div className="flex flex-col gap-1 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-white">Database Dumps</h1>
          <p className="mt-1 text-xs text-slate-400">
            Historical archive of all generated database dumps stored locally or on S3 storage.
          </p>
        </div>
      </div>

      {/* Overview Stat Cards */}
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <StatCard title="Total Dumps" value={allDumps.length} icon={Archive} tone="indigo" />
        <StatCard title="Storage Used" value={fmtSize(totalSizeBytes)} icon={HardDrive} tone="violet" />
        <StatCard title="Ready Dumps" value={readyCount} icon={Archive} tone="emerald" />
        <StatCard title="S3 Offsite Copies" value={s3Count} icon={Cloud} tone="sky" />
      </div>

      {/* Filters & Search */}
      <div className="flex flex-col gap-3 md:flex-row md:items-center justify-between rounded-xl border border-slate-800 bg-slate-900/60 p-3 backdrop-blur-md">
        <div className="relative flex-1">
          <Search size={16} className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-500" />
          <Input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Search dump label or database name..."
            className="pl-9 bg-slate-950/80"
          />
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Select value={engineFilter} onChange={(e) => setEngineFilter(e.target.value as Engine | '')} className="w-36 bg-slate-950/80">
            <option value="">All Engines</option>
            <option value="postgres">PostgreSQL</option>
            <option value="mongodb">MongoDB</option>
          </Select>
          <Select value={dbFilter} onChange={(e) => setDbFilter(e.target.value)} className="w-44 bg-slate-950/80">
            <option value="">All Databases</option>
            {dbs.data?.map((db) => (
              <option key={db.id} value={db.id}>
                {db.name}
              </option>
            ))}
          </Select>
          <Select value={storageFilter} onChange={(e) => setStorageFilter(e.target.value)} className="w-32 bg-slate-950/80">
            <option value="">All Storage</option>
            <option value="local">Local</option>
            {(dests.data ?? []).map((d) => (
              <option key={d.id} value={d.id}>
                {d.name}
              </option>
            ))}
          </Select>
        </div>
      </div>

      {/* Table Container */}
      <div className="overflow-hidden rounded-2xl border border-slate-800 bg-slate-900/80 shadow-2xl backdrop-blur-md">
        <div className="overflow-x-auto">
          <table className="w-full text-left text-sm text-slate-300">
            <thead className="border-b border-slate-800 bg-slate-950/60 text-xs font-semibold uppercase tracking-wider text-slate-400">
              <tr>
                <th className="px-5 py-3.5">Label</th>
                <th className="px-5 py-3.5">Database</th>
                <th className="px-5 py-3.5">Engine</th>
                <th className="px-5 py-3.5">Storage</th>
                <th className="px-5 py-3.5">Size</th>
                <th className="px-5 py-3.5">Status</th>
                <th className="px-5 py-3.5">Created At</th>
                <th className="px-5 py-3.5 text-right">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-800/60">
              {rows.map((d) => (
                <tr key={d.id} className="transition-colors hover:bg-slate-800/40">
                  <td className="px-5 py-4 font-semibold text-slate-100">{d.label}</td>
                  <td className="px-5 py-4 text-slate-300">
                    {d.databaseName || <span className="italic text-slate-500">uploaded file</span>}
                  </td>
                  <td className="px-5 py-4">
                    <Badge tone={d.engine === 'postgres' ? 'blue' : 'green'}>{d.engine}</Badge>
                  </td>
                  <td className="px-5 py-4">
                    <span className="inline-flex items-center gap-1 text-xs font-medium uppercase tracking-wider text-slate-400">
                      {d.destId === '' ? <HardDrive size={13} className="text-slate-400" /> : <Cloud size={13} className="text-sky-400" />}
                      {d.destName || 'local'}
                    </span>
                  </td>
                  <td className="px-5 py-4 font-mono text-xs text-slate-300">{fmtSize(d.sizeBytes)}</td>
                  <td className="px-5 py-4">
                    {d.status === 'pending' ? (
                      <Badge tone="amber" pulse>
                        Pending
                      </Badge>
                    ) : (
                      <Badge tone={d.status === 'ready' || d.status === 'uploaded' ? 'green' : d.status === 'failed' ? 'red' : 'slate'}>
                        {d.status}
                      </Badge>
                    )}
                  </td>
                  <td className="px-5 py-4 text-xs text-slate-400">{new Date(d.createdAt).toLocaleString()}</td>
                  <td className="px-5 py-4 text-right">
                    <div className="flex items-center justify-end gap-1.5">
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() => window.open(`/api/dumps/${d.id}/download`, '_blank')}
                        disabled={d.status !== 'ready' && d.status !== 'uploaded'}
                        title="Download Archive"
                      >
                        <Download size={14} />
                      </Button>
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() => setRestoreSource(d)}
                        disabled={d.status !== 'ready' && d.status !== 'uploaded'}
                      >
                        <Play size={14} /> Restore
                      </Button>
                      <Button
                        size="sm"
                        variant="ghost"
                        className="hover:text-rose-400"
                        disabled={d.status === 'pending'}
                        onClick={() => {
                          if (confirm(`Delete dump "${d.label}"? The stored archive file will be permanently deleted.`))
                            remove.mutate(d.id)
                        }}
                        title="Delete Dump"
                      >
                        <Trash2 size={14} />
                      </Button>
                    </div>
                  </td>
                </tr>
              ))}
              {rows.length === 0 && (
                <tr>
                  <td colSpan={8} className="px-6 py-12 text-center text-slate-500">
                    <Archive size={32} className="mx-auto mb-2 opacity-40" />
                    No dump archives match your search or filter criteria.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
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

