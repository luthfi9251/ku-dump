import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useSearchParams } from 'react-router-dom'
import { AlertCircle, CheckCircle2, Clock, ListChecks, PlayCircle } from 'lucide-react'
import { api } from '../lib/api'
import type { JobDTO } from '../lib/types'
import { Badge, StatCard } from '../ui'
import JobDrawer, { statusTone } from '../JobDrawer'

type StatusFilter = 'all' | 'active' | 'success' | 'failed'

export default function Jobs() {
  const [params, setParams] = useSearchParams()
  const [drawerId, setDrawerId] = useState<string | null>(params.get('job'))
  const [filter, setFilter] = useState<StatusFilter>('all')

  const jobs = useQuery({
    queryKey: ['jobs'],
    queryFn: () => api.get<JobDTO[]>('/api/jobs?limit=200'),
    refetchInterval: 2000,
  })

  function openDrawer(id: string) {
    setDrawerId(id)
    if (params.get('job') !== id) {
      params.set('job', id)
      setParams(params, { replace: true })
    }
  }

  function closeDrawer() {
    setDrawerId(null)
    params.delete('job')
    setParams(params, { replace: true })
  }

  const allJobs = jobs.data ?? []
  const activeCount = allJobs.filter((j) => j.status === 'pending' || j.status === 'running').length
  const successCount = allJobs.filter((j) => j.status === 'success').length
  const failedCount = allJobs.filter((j) => j.status === 'failed').length

  const rows = allJobs.filter((j) => {
    if (filter === 'active') return j.status === 'pending' || j.status === 'running'
    if (filter === 'success') return j.status === 'success'
    if (filter === 'failed') return j.status === 'failed'
    return true
  })

  return (
    <div className="space-y-6">
      {/* Page Title */}
      <div className="flex flex-col gap-1 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-white">Job History & Operations</h1>
          <p className="mt-1 text-xs text-slate-400">
            Monitor real-time backup and restore job executions, inspect logs, or cancel active tasks.
          </p>
        </div>
      </div>

      {/* Overview Stat Cards */}
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <StatCard title="Total Jobs" value={allJobs.length} icon={ListChecks} tone="indigo" />
        <StatCard title="Active Running" value={activeCount} icon={PlayCircle} tone="amber" />
        <StatCard title="Succeeded" value={successCount} icon={CheckCircle2} tone="emerald" />
        <StatCard title="Failed" value={failedCount} icon={AlertCircle} tone="violet" />
      </div>

      {/* Filter Tabs */}
      <div className="flex items-center gap-2 border-b border-slate-800 pb-3">
        {(
          [
            { id: 'all', label: 'All Jobs', count: allJobs.length },
            { id: 'active', label: 'Active', count: activeCount },
            { id: 'success', label: 'Succeeded', count: successCount },
            { id: 'failed', label: 'Failed', count: failedCount },
          ] as const
        ).map((t) => (
          <button
            key={t.id}
            onClick={() => setFilter(t.id)}
            className={`flex items-center gap-2 rounded-lg px-3.5 py-1.5 text-xs font-semibold tracking-wide transition-all ${
              filter === t.id
                ? 'bg-indigo-600 text-white shadow-md shadow-indigo-600/25'
                : 'text-slate-400 hover:bg-slate-800 hover:text-slate-200'
            }`}
          >
            {t.label}
            <span
              className={`rounded-full px-1.5 py-0.2 text-[10px] ${
                filter === t.id ? 'bg-indigo-700 text-indigo-100' : 'bg-slate-800 text-slate-400'
              }`}
            >
              {t.count}
            </span>
          </button>
        ))}
      </div>

      {/* Table Container */}
      <div className="overflow-hidden rounded-2xl border border-slate-800 bg-slate-900/80 shadow-2xl backdrop-blur-md">
        <div className="overflow-x-auto">
          <table className="w-full text-left text-sm text-slate-300">
            <thead className="border-b border-slate-800 bg-slate-950/60 text-xs font-semibold uppercase tracking-wider text-slate-400">
              <tr>
                <th className="px-5 py-3.5">Type</th>
                <th className="px-5 py-3.5">Target Database</th>
                <th className="px-5 py-3.5">Dump Archive</th>
                <th className="px-5 py-3.5">Status</th>
                <th className="px-5 py-3.5">Started At</th>
                <th className="px-5 py-3.5">Finished At</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-800/60">
              {rows.map((j) => {
                const isActive = drawerId === j.id
                return (
                  <tr
                    key={j.id}
                    onClick={() => openDrawer(j.id)}
                    className={`cursor-pointer transition-colors hover:bg-slate-800/50 ${
                      isActive ? 'bg-indigo-950/40 text-white font-medium' : ''
                    }`}
                  >
                    <td className="px-5 py-4 font-semibold text-slate-100 uppercase tracking-wider text-xs">
                      {j.type}
                    </td>
                    <td className="px-5 py-4 font-medium text-slate-200">{j.databaseName}</td>
                    <td className="px-5 py-4 text-xs font-mono text-slate-400">{j.dumpLabel || '—'}</td>
                    <td className="px-5 py-4">
                      <Badge tone={statusTone(j.status)} pulse={j.status === 'pending' || j.status === 'running'}>
                        {j.status}
                      </Badge>
                    </td>
                    <td className="px-5 py-4 text-xs text-slate-400">{new Date(j.createdAt).toLocaleString()}</td>
                    <td className="px-5 py-4 text-xs text-slate-400">
                      {j.finishedAt ? new Date(j.finishedAt).toLocaleString() : <span className="italic text-slate-500">running…</span>}
                    </td>
                  </tr>
                )
              })}
              {rows.length === 0 && (
                <tr>
                  <td colSpan={6} className="px-6 py-12 text-center text-slate-500">
                    <Clock size={32} className="mx-auto mb-2 opacity-40" />
                    No jobs match the selected status filter.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </div>

      {drawerId && <JobDrawer jobId={drawerId} onClose={closeDrawer} />}
    </div>
  )
}

