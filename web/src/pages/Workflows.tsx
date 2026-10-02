import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { AlertTriangle, CalendarClock, Pause, Play, Pencil, Plus, Trash2 } from 'lucide-react'
import { api } from '../lib/api'
import type { WorkflowDTO } from '../lib/types'
import { Badge, Button, StatCard } from '../ui'
import WorkflowModal from '../modals/WorkflowModal'

export function describeCron(expr: string): string {
  const parts = expr.trim().split(/\s+/)
  if (parts.length !== 5) return expr
  const [m, h, dom, mon, dow] = parts
  if (m !== '*' && h === '*' && dom === '*' && mon === '*' && dow === '*') return `Hourly at :${m.padStart(2, '0')}`
  if (m !== '*' && h !== '*' && dom === '*' && mon === '*' && dow === '*') {
    return `Daily at ${h.padStart(2, '0')}:${m.padStart(2, '0')}`
  }
  if (m !== '*' && h !== '*' && dom === '*' && mon === '*' && dow !== '*') {
    const days = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat']
    return `Weekly on ${days[Number(dow)] ?? dow} at ${h.padStart(2, '0')}:${m.padStart(2, '0')}`
  }
  return expr
}

function describeTrigger(w: WorkflowDTO): string {
  if (w.triggerKind === 'manual') return 'Manual'
  if (w.triggerKind === 'once') return `Once — ${new Date(w.runAt).toLocaleString()}`
  return describeCron(w.cron)
}

export default function Workflows() {
  const qc = useQueryClient()
  const navigate = useNavigate()
  const [showAdd, setShowAdd] = useState(false)
  const [editing, setEditing] = useState<WorkflowDTO | undefined>(undefined)

  const workflows = useQuery({
    queryKey: ['workflows'],
    queryFn: () => api.get<WorkflowDTO[]>('/api/workflows'),
    refetchInterval: 10000,
  })

  function invalidate() {
    void qc.invalidateQueries({ queryKey: ['workflows'] })
  }

  const toggle = useMutation({
    mutationFn: (w: WorkflowDTO) =>
      api.put<WorkflowDTO>(`/api/workflows/${w.id}`, {
        name: w.name,
        databaseId: w.databaseId,
        destId: w.destId,
        triggerKind: w.triggerKind,
        runAt: w.runAt || undefined,
        cron: w.cron || undefined,
        enabled: !w.enabled,
      }),
    onSuccess: invalidate,
  })

  const run = useMutation({
    mutationFn: (w: WorkflowDTO) => api.post<{ jobId: string }>(`/api/workflows/${w.id}/run`),
    onSuccess: (res) => {
      invalidate()
      navigate(`/jobs?job=${res.jobId}`)
    },
  })

  const remove = useMutation({
    mutationFn: (id: string) => api.del(`/api/workflows/${id}`),
    onSuccess: invalidate,
  })

  const rows = workflows.data ?? []
  const active = rows.filter((w) => w.enabled && w.triggerKind !== 'manual').length
  const failing = rows.filter((w) => w.lastError !== '').length

  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-white">Dump Workflows</h1>
          <p className="mt-1 text-xs text-slate-400">
            Reusable dump recipes: which database, when it runs, where it lands.
          </p>
        </div>
        <Button onClick={() => setShowAdd(true)} size="md" className="shrink-0 shadow-lg shadow-indigo-600/25">
          <Plus size={16} /> New Workflow
        </Button>
      </div>

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
        <StatCard title="Total Workflows" value={rows.length} icon={CalendarClock} tone="indigo" />
        <StatCard title="Scheduled Active" value={active} icon={Play} tone="emerald" />
        <StatCard title="With Errors" value={failing} icon={AlertTriangle} tone="amber" />
      </div>

      <div className="overflow-hidden rounded-2xl border border-slate-800 bg-slate-900/80 shadow-2xl backdrop-blur-md">
        <div className="overflow-x-auto">
          <table className="w-full text-left text-sm text-slate-300">
            <thead className="border-b border-slate-800 bg-slate-950/60 text-xs font-semibold uppercase tracking-wider text-slate-400">
              <tr>
                <th className="px-5 py-3.5">Name</th>
                <th className="px-5 py-3.5">Database</th>
                <th className="px-5 py-3.5">Destination</th>
                <th className="px-5 py-3.5">Trigger</th>
                <th className="px-5 py-3.5">Next Run</th>
                <th className="px-5 py-3.5">Last Run</th>
                <th className="px-5 py-3.5">Status</th>
                <th className="px-5 py-3.5 text-right">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-800/60">
              {rows.map((w) => (
                <tr key={w.id} className="transition-colors hover:bg-slate-800/40">
                  <td className="px-5 py-4 font-semibold text-slate-100">{w.name}</td>
                  <td className="px-5 py-4 text-slate-300">{w.databaseName}</td>
                  <td className="px-5 py-4 text-slate-400">{w.destName}</td>
                  <td className="px-5 py-4 text-slate-300">{describeTrigger(w)}</td>
                  <td className="px-5 py-4 text-xs text-slate-400">
                    {w.nextRunAt ? new Date(w.nextRunAt).toLocaleString() : '—'}
                  </td>
                  <td className="px-5 py-4 text-xs text-slate-400">
                    {w.lastRunAt ? new Date(w.lastRunAt).toLocaleString() : '—'}
                    {w.lastError && <div className="mt-0.5 text-[11px] text-rose-400">{w.lastError}</div>}
                  </td>
                  <td className="px-5 py-4">
                    {w.lastError !== '' ? (
                      <Badge tone="red" pulse>
                        error
                      </Badge>
                    ) : w.enabled ? (
                      <Badge tone="green">active</Badge>
                    ) : (
                      <Badge tone="slate">paused</Badge>
                    )}
                  </td>
                  <td className="px-5 py-4 text-right">
                    <div className="flex items-center justify-end gap-1.5">
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() => run.mutate(w)}
                        disabled={run.isPending && run.variables?.id === w.id}
                        title="Run now"
                      >
                        <Play size={14} />
                      </Button>
                      <Button
                        size="sm"
                        variant="ghost"
                        onClick={() => toggle.mutate(w)}
                        title={w.enabled ? 'Pause' : 'Resume'}
                      >
                        {w.enabled ? <Pause size={14} /> : <Play size={14} />}
                      </Button>
                      <Button size="sm" variant="ghost" onClick={() => setEditing(w)} title="Edit">
                        <Pencil size={14} />
                      </Button>
                      <Button
                        size="sm"
                        variant="ghost"
                        className="hover:text-rose-400"
                        onClick={() => {
                          if (confirm(`Delete workflow "${w.name}"? Scheduled runs will stop.`)) remove.mutate(w.id)
                        }}
                        title="Delete"
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
                    <CalendarClock size={32} className="mx-auto mb-2 opacity-40" />
                    No workflows yet — create one to schedule your first dump.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </div>

      <WorkflowModal open={showAdd} onClose={() => setShowAdd(false)} onSaved={invalidate} />
      {editing && (
        <WorkflowModal open editing={editing} onClose={() => setEditing(undefined)} onSaved={invalidate} />
      )}
    </div>
  )
}
