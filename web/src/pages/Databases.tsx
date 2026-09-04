import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { CheckCircle2, Database, HardDriveDownload, Layers, AlertTriangle, Pencil, Play, Plus, Search, Trash2, XCircle } from 'lucide-react'
import { api } from '../lib/api'
import type { DatabaseDTO, TestResultDTO } from '../lib/types'
import { Badge, Button, Input, Select, Spinner, StatCard } from '../ui'
import DatabaseModal from '../modals/DatabaseModal'
import DumpModal from '../modals/DumpModal'
import RestoreModal from '../modals/RestoreModal'

export default function Databases() {
  const qc = useQueryClient()
  const [editing, setEditing] = useState<DatabaseDTO | undefined>(undefined)
  const [showAdd, setShowAdd] = useState(false)
  const [dumpTarget, setDumpTarget] = useState<DatabaseDTO | null>(null)
  const [restoreTarget, setRestoreTarget] = useState<DatabaseDTO | null>(null)
  const [search, setSearch] = useState('')
  const [engineFilter, setEngineFilter] = useState<string>('all')

  const dbs = useQuery({
    queryKey: ['databases'],
    queryFn: () => api.get<DatabaseDTO[]>('/api/databases'),
    refetchInterval: 10000,
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

  const allDbs = dbs.data ?? []
  const postgresCount = allDbs.filter((d) => d.engine === 'postgres').length
  const mongoCount = allDbs.filter((d) => d.engine === 'mongodb').length
  const healthyCount = allDbs.filter((d) => d.lastTestOk === true).length

  const filtered = allDbs.filter((db) => {
    const matchesSearch =
      db.name.toLowerCase().includes(search.toLowerCase()) ||
      db.host.toLowerCase().includes(search.toLowerCase()) ||
      db.dbName.toLowerCase().includes(search.toLowerCase())
    const matchesEngine = engineFilter === 'all' || db.engine === engineFilter
    return matchesSearch && matchesEngine
  })

  return (
    <div className="space-y-6">
      {/* Page Header */}
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-white">Databases</h1>
          <p className="mt-1 text-xs text-slate-400">
            Manage your registered database instances and trigger manual dumps or restores.
          </p>
        </div>
        <Button onClick={() => setShowAdd(true)} size="md" className="shrink-0 shadow-lg shadow-indigo-600/25">
          <Plus size={16} /> Add Database
        </Button>
      </div>

      {/* Stats Summary Bar */}
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <StatCard title="Total Instances" value={allDbs.length} icon={Database} tone="indigo" />
        <StatCard title="PostgreSQL" value={postgresCount} icon={Layers} tone="sky" />
        <StatCard title="MongoDB" value={mongoCount} icon={Layers} tone="emerald" />
        <StatCard
          title="Healthy Connections"
          value={`${healthyCount} / ${allDbs.length}`}
          icon={CheckCircle2}
          tone="emerald"
        />
      </div>

      {/* Search & Filter Bar */}
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center justify-between rounded-xl border border-slate-800 bg-slate-900/60 p-3 backdrop-blur-md">
        <div className="relative flex-1">
          <Search size={16} className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-500" />
          <Input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Search databases by name, host, or db name..."
            className="pl-9 bg-slate-950/80"
          />
        </div>
        <div className="flex items-center gap-2">
          <Select
            value={engineFilter}
            onChange={(e) => setEngineFilter(e.target.value)}
            className="w-40 bg-slate-950/80"
          >
            <option value="all">All Engines</option>
            <option value="postgres">PostgreSQL</option>
            <option value="mongodb">MongoDB</option>
          </Select>
        </div>
      </div>

      {/* Loading State */}
      {dbs.isLoading && (
        <div className="flex items-center justify-center py-12">
          <Spinner className="h-8 w-8" />
        </div>
      )}

      {/* Empty State */}
      {!dbs.isLoading && filtered.length === 0 && (
        <div className="flex flex-col items-center justify-center rounded-2xl border border-dashed border-slate-800 bg-slate-900/40 p-12 text-center backdrop-blur-md">
          <div className="flex h-12 w-12 items-center justify-center rounded-2xl bg-slate-800/80 text-slate-400 mb-3 border border-slate-700/50">
            <Database size={24} />
          </div>
          <h3 className="text-base font-semibold text-slate-200">No databases found</h3>
          <p className="mt-1 text-xs text-slate-400 max-w-sm">
            {search || engineFilter !== 'all'
              ? 'No registered database matches your search or engine criteria.'
              : 'You have not added any databases yet. Click "Add Database" to register your first connection.'}
          </p>
          {!search && engineFilter === 'all' && (
            <Button onClick={() => setShowAdd(true)} className="mt-4" size="sm">
              <Plus size={14} /> Register Database
            </Button>
          )}
        </div>
      )}

      {/* Database Cards Grid */}
      <div className="grid grid-cols-1 gap-5 md:grid-cols-2 xl:grid-cols-3">
        {filtered.map((db) => {
          const isPostgres = db.engine === 'postgres'
          return (
            <div
              key={db.id}
              className="group relative flex flex-col justify-between overflow-hidden rounded-2xl border border-slate-800/90 bg-slate-900/80 p-5 shadow-xl transition-all duration-200 hover:border-slate-700 hover:shadow-2xl hover:shadow-black/50"
            >
              {/* Card Top Engine Bar */}
              <div
                className={`absolute top-0 inset-x-0 h-1 ${
                  isPostgres ? 'bg-gradient-to-r from-sky-500 to-indigo-500' : 'bg-gradient-to-r from-emerald-500 to-teal-500'
                }`}
              />

              <div className="space-y-4">
                {/* Header Info */}
                <div className="flex items-start justify-between gap-3">
                  <div>
                    <h3 className="font-bold text-slate-100 text-base group-hover:text-indigo-300 transition-colors">
                      {db.name}
                    </h3>
                    <div className="mt-0.5 font-mono text-xs text-slate-400 truncate">
                      {db.host}:{db.port}/{db.dbName}
                    </div>
                  </div>
                  <Badge tone={isPostgres ? 'blue' : 'green'} className="shrink-0">
                    {db.engine}
                  </Badge>
                </div>

                {/* Status Badges */}
                <div className="flex flex-wrap items-center gap-2 pt-1 border-t border-slate-800/60 text-xs">
                  {db.toolsMissing.length > 0 ? (
                    <Badge tone="red">
                      <AlertTriangle size={12} className="inline mr-1" />
                      missing: {db.toolsMissing.join(', ')}
                    </Badge>
                  ) : db.lastTestOk === null ? (
                    <Badge tone="slate">Not tested</Badge>
                  ) : db.lastTestOk ? (
                    <Badge tone="green">
                      <CheckCircle2 size={12} className="inline mr-1" />
                      Connection OK
                    </Badge>
                  ) : (
                    <Badge tone="red">
                      <XCircle size={12} className="inline mr-1" />
                      Connection Failed
                    </Badge>
                  )}

                  {db.hasActiveJob && (
                    <Badge tone="amber" pulse>
                      Job Running
                    </Badge>
                  )}
                </div>
              </div>

              {/* Action Buttons Footer */}
              <div className="mt-5 flex flex-wrap items-center justify-between gap-2 border-t border-slate-800/80 pt-4">
                <div className="flex items-center gap-2 flex-1">
                  <Button
                    size="sm"
                    onClick={() => setDumpTarget(db)}
                    disabled={db.hasActiveJob || db.toolsMissing.length > 0}
                    title={db.hasActiveJob ? 'A job is currently running' : undefined}
                    className="flex-1"
                  >
                    <HardDriveDownload size={14} /> Dump
                  </Button>
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => setRestoreTarget(db)}
                    disabled={db.hasActiveJob || db.toolsMissing.length > 0}
                    className="flex-1"
                  >
                    <Play size={14} /> Restore
                  </Button>
                </div>
                <div className="flex items-center gap-1">
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={() => test.mutate(db.id)}
                    disabled={test.isPending && test.variables === db.id}
                    title="Test Connection"
                  >
                    {test.isPending && test.variables === db.id ? <Spinner className="h-3.5 w-3.5" /> : <Database size={14} />}
                  </Button>
                  <Button size="sm" variant="ghost" onClick={() => setEditing(db)} title="Edit Configuration">
                    <Pencil size={14} />
                  </Button>
                  <Button
                    size="sm"
                    variant="ghost"
                    className="hover:text-rose-400"
                    onClick={() => {
                      if (confirm(`Delete database registration "${db.name}"? Historical dump files will be kept.`))
                        remove.mutate(db.id)
                    }}
                    title="Delete Database"
                  >
                    <Trash2 size={14} />
                  </Button>
                </div>
              </div>
            </div>
          )
        })}
      </div>

      {/* Modals */}
      <DatabaseModal open={showAdd} onClose={() => setShowAdd(false)} onSaved={invalidate} />
      <DatabaseModal open={editing !== undefined} onClose={() => setEditing(undefined)} db={editing} onSaved={invalidate} />
      {dumpTarget && (
        <DumpModal open onClose={() => setDumpTarget(null)} db={dumpTarget} />
      )}
      {restoreTarget && (
        <RestoreModal open onClose={() => setRestoreTarget(null)} target={restoreTarget} onStarted={() => invalidate()} />
      )}
    </div>
  )
}

