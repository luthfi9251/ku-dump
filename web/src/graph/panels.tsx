import { useState } from 'react'
import type { FormEvent, ReactNode } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { ArrowLeftRight, Check, Download, GitBranch, Pencil, Play, Terminal, Trash2, X } from 'lucide-react'
import { api, ApiError } from '../lib/api'
import { fmtSize } from '../lib/format'
import type { DumpDTO, GraphBranchDTO, GraphNodeDTO } from '../lib/types'
import { Badge, Button } from '../ui'
import { statusTone } from '../JobDrawer'
import { isReady, type Layout } from './layout'

const box = 'rounded-2xl border border-slate-800 bg-slate-900/80 p-5 shadow-2xl backdrop-blur-md'

export function fmtTime(iso: string) {
  return new Date(iso).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' })
}

function Row({ k, children }: { k: string; children: ReactNode }) {
  return (
    <>
      <dt className="text-slate-500">{k}</dt>
      <dd className="min-w-0 break-words text-slate-200">{children}</dd>
    </>
  )
}

function rowColor(layout: Layout, id: string) {
  return layout.placed.get(id)?.row.color ?? '#64748b'
}

// branchOrigin describes how a branch started, from its first event.
function branchOrigin(layout: Layout, focus: string, branchId: string): string {
  const first = [...layout.placed.values()].find((p) => p.node.lane === focus && p.node.branch === branchId)?.node
  if (!first) return ''
  if (first.kind === 'dump') return `started with ${first.dump?.label}`
  const src = first.source ? layout.byId.get(first.source) : undefined
  if (!src) return 'restored from a deleted dump'
  if (src.lane !== focus) return `restored from ${layout.laneName(src.lane)} · ${src.dump?.label}`
  const srcBranch = src.branch ? layout.branches.get(src.branch)?.name : undefined
  return `forked from ${src.dump?.label}${srcBranch ? ` (${srcBranch})` : ''}`
}

function branchTip(layout: Layout, focus: string, branchId: string): GraphNodeDTO | undefined {
  let tip: GraphNodeDTO | undefined
  for (const p of layout.placed.values()) {
    if (p.node.lane === focus && p.node.branch === branchId && isReady(p.node)) tip = p.node
  }
  return tip
}

export function BranchName({ branch }: { branch: GraphBranchDTO }) {
  const qc = useQueryClient()
  const [editing, setEditing] = useState(false)
  const [name, setName] = useState(branch.name)
  const rename = useMutation({
    mutationFn: (n: string) => api.put(`/api/branches/${branch.id}`, { name: n }),
    onSuccess: () => {
      setEditing(false)
      void qc.invalidateQueries({ queryKey: ['dump-graph'] })
    },
  })
  if (!editing) {
    return (
      <span className="inline-flex min-w-0 items-center gap-1.5">
        <span className="truncate font-mono font-semibold text-slate-100">{branch.name}</span>
        <button
          type="button"
          onClick={() => {
            setName(branch.name)
            rename.reset()
            setEditing(true)
          }}
          className="rounded p-0.5 text-slate-500 hover:text-slate-200"
          title="Rename branch"
          aria-label={`Rename branch ${branch.name}`}
        >
          <Pencil size={12} />
        </button>
      </span>
    )
  }
  function submit(e: FormEvent) {
    e.preventDefault()
    if (name.trim()) rename.mutate(name.trim())
  }
  return (
    <form onSubmit={submit} className="w-full space-y-1">
      <div className="flex items-center gap-1">
        <input
          autoFocus
          value={name}
          maxLength={60}
          onChange={(e) => setName(e.target.value)}
          onKeyDown={(e) => e.key === 'Escape' && setEditing(false)}
          className="min-w-0 flex-1 rounded-md border border-slate-700 bg-slate-950 px-2 py-1 font-mono text-xs text-slate-100 focus:border-indigo-500 focus:outline-none"
          aria-label="Branch name"
        />
        <button type="submit" className="rounded p-1 text-emerald-400 hover:bg-slate-800" aria-label="Save name" disabled={rename.isPending}>
          <Check size={14} />
        </button>
        <button type="button" className="rounded p-1 text-slate-400 hover:bg-slate-800" aria-label="Cancel" onClick={() => setEditing(false)}>
          <X size={14} />
        </button>
      </div>
      {rename.error && <p className="text-[11px] text-rose-400">{rename.error instanceof ApiError ? rename.error.message : 'rename failed'}</p>}
    </form>
  )
}

export function BranchPanel({ layout, focus, onSelect }: { layout: Layout; focus: string; onSelect: (id: string) => void }) {
  const rows = layout.rows.filter((r) => r.kind === 'branch' && r.branch)
  const sorted = [...rows].sort((a, b) => Number(b.branch!.active) - Number(a.branch!.active))
  return (
    <div className={`${box} space-y-4`}>
      <div>
        <div className="text-[11px] font-semibold uppercase tracking-wider text-slate-500">Branches</div>
        <p className="mt-1 text-xs leading-relaxed text-slate-400">
          Restoring the newest dump of a branch continues that branch. Restoring an older point, or a dump of
          another database, starts a new branch. Click a point in the graph to restore it.
        </p>
      </div>
      <ul className="space-y-2.5">
        {sorted.map((r) => {
          const b = r.branch!
          const tip = branchTip(layout, focus, b.id)
          const count = [...layout.placed.values()].filter((p) => p.node.lane === focus && p.node.branch === b.id && p.node.kind === 'dump').length
          return (
            <li key={b.id} className={`rounded-xl border p-3 ${b.active ? 'border-indigo-500/40 bg-indigo-950/20' : 'border-slate-800 bg-slate-950/30'}`}>
              <div className="flex items-center justify-between gap-2">
                <div className="flex min-w-0 flex-1 items-center gap-2">
                  <span className="h-2.5 w-2.5 shrink-0 rounded-full" style={{ background: r.color }} />
                  <BranchName key={b.id + b.name} branch={b} />
                </div>
                {b.active && <Badge tone="indigo">active</Badge>}
              </div>
              <p className="mt-1.5 text-[11px] text-slate-400">{branchOrigin(layout, focus, b.id)}</p>
              <div className="mt-2 flex items-center justify-between text-[11px] text-slate-500">
                <span>{count} dump{count === 1 ? '' : 's'}</span>
                {tip && (
                  <button type="button" onClick={() => onSelect(tip.id)} className="text-indigo-300 hover:text-indigo-200">
                    Newest: {tip.dump?.label} →
                  </button>
                )}
              </div>
            </li>
          )
        })}
      </ul>
    </div>
  )
}

export function NodePanel({
  node,
  layout,
  focus,
  chain,
  onSelect,
  onRestore,
  onDelete,
  onOpenJob,
}: {
  node: GraphNodeDTO
  layout: Layout
  focus: string
  chain: GraphNodeDTO[]
  onSelect: (id: string | null) => void
  onRestore: (dump: DumpDTO) => void
  onDelete: (dump: DumpDTO) => void
  onOpenJob: (jobId: string) => void
}) {
  const { byId, laneName } = layout
  const visible = (id: string) => layout.placed.has(id)
  const branch = node.lane === focus && node.branch ? layout.branches.get(node.branch) : undefined
  const where = (n: GraphNodeDTO) => {
    const b = n.lane === focus && n.branch ? layout.branches.get(n.branch) : undefined
    return b ? `${laneName(n.lane)} / ${b.name}` : laneName(n.lane)
  }

  const origin = [...chain].reverse()
  const chainView = (
    <div>
      <div className="mb-1.5 text-[11px] font-semibold uppercase tracking-wider text-slate-500">Origin</div>
      <ol className="flex flex-wrap items-center gap-1.5 text-[11px]">
        {origin.map((c, i) => (
          <li key={c.id} className="flex items-center gap-1.5">
            {i > 0 && <span className="text-slate-600">→</span>}
            <button
              type="button"
              onClick={() => onSelect(c.id)}
              disabled={!visible(c.id)}
              title={visible(c.id) ? where(c) : `${laneName(c.lane)} is not related to the selected database`}
              className={`inline-flex items-center gap-1.5 rounded-full border px-2 py-0.5 disabled:cursor-default disabled:opacity-60 ${c.id === node.id ? 'border-indigo-500/60 bg-indigo-950/60 text-indigo-200' : 'border-slate-700 bg-slate-800/60 text-slate-300 enabled:hover:border-slate-500'}`}
            >
              <span className="h-2 w-2 rounded-full" style={{ background: rowColor(layout, c.id) }} />
              {c.kind === 'dump' ? c.dump?.label : `restore → ${where(c)}`}
            </button>
          </li>
        ))}
      </ol>
    </div>
  )

  if (node.kind === 'restore') {
    const src = node.source ? byId.get(node.source) : undefined
    const first = branch && [...layout.placed.values()].find((p) => p.node.lane === focus && p.node.branch === branch.id)?.node
    const result =
      node.status !== 'success' || !branch ? null : first?.id === node.id ? `started branch ${branch.name}` : `continued branch ${branch.name}`
    return (
      <div className={`${box} space-y-4`}>
        <div className="flex items-start justify-between gap-3">
          <div>
            <div className="text-[11px] font-semibold uppercase tracking-wider text-slate-500">Restore</div>
            <h3 className="mt-0.5 font-semibold text-slate-100">into {laneName(node.lane)}</h3>
          </div>
          <Badge tone={statusTone(node.status as Parameters<typeof statusTone>[0])}>{node.status}</Badge>
        </div>
        <dl className="grid grid-cols-[max-content_1fr] gap-x-4 gap-y-1.5 text-xs">
          <Row k="Source">{src ? `${src.dump?.label} (${where(src)})` : <span className="italic text-slate-500">dump deleted</span>}</Row>
          {result && <Row k="Result">{result}</Row>}
          <Row k="Started">{fmtTime(node.at)}</Row>
          {node.finishedAt && <Row k="Finished">{fmtTime(node.finishedAt)}</Row>}
        </dl>
        <p className="text-xs leading-relaxed text-slate-400">
          {node.status === 'success'
            ? 'A restore is an event, not a file. To bring a database back to this state, restore its source dump.'
            : 'This restore did not complete, so it did not change the database or its branches.'}
        </p>
        <div className="flex flex-wrap gap-2">
          {node.jobId && (
            <Button size="sm" variant="outline" onClick={() => onOpenJob(node.jobId!)}>
              <Terminal size={14} /> View log
            </Button>
          )}
          {src && visible(src.id) && (
            <Button size="sm" variant="outline" onClick={() => onSelect(src.id)}>
              <ArrowLeftRight size={14} /> Go to source dump
            </Button>
          )}
        </div>
        {chainView}
      </div>
    )
  }

  const d = node.dump!
  const parent = node.parent ? byId.get(node.parent) : undefined
  const parentText = !parent
    ? node.lane === 'upload'
      ? 'none, file uploaded from outside ku-dump'
      : 'none (first known state)'
    : parent.kind === 'dump'
      ? `${parent.dump?.label} (previous dump)`
      : parent.source && byId.get(parent.source)
        ? `${byId.get(parent.source)!.dump?.label} (restored from ${where(byId.get(parent.source)!)})`
        : 'a restore whose source dump was deleted'
  const isTip = branch && branchTip(layout, focus, branch.id)?.id === node.id
  return (
    <div className={`${box} space-y-4`}>
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="text-[11px] font-semibold uppercase tracking-wider text-slate-500">Dump</div>
          <h3 className="mt-0.5 break-words font-semibold text-slate-100">{d.label}</h3>
        </div>
        <Badge tone={isReady(node) ? 'green' : d.status === 'pending' ? 'amber' : 'slate'} pulse={d.status === 'pending'}>{d.status}</Badge>
      </div>
      <dl className="grid grid-cols-[max-content_1fr] gap-x-4 gap-y-1.5 text-xs">
        <Row k="Database">{d.databaseName || <span className="italic text-slate-500">uploaded file</span>}</Row>
        {branch && (
          <Row k="Branch">
            <span className="inline-flex items-center gap-1.5">
              <GitBranch size={12} className="text-slate-500" />
              <span className="font-mono">{branch.name}</span>
              {isTip && <span className="text-slate-500">(newest)</span>}
            </span>
          </Row>
        )}
        <Row k="Created">{fmtTime(d.createdAt)}</Row>
        <Row k="Workflow">{d.workflowName || '—'}</Row>
        <Row k="Storage">{d.destName || 'local'}</Row>
        <Row k="Size">{fmtSize(d.sizeBytes)}</Row>
        <Row k="Parent">{parentText}</Row>
      </dl>
      {branch && (
        <p className="rounded-lg border border-slate-800 bg-slate-950/40 p-2.5 text-[11px] leading-relaxed text-slate-400">
          {isTip
            ? `Restoring this dump into ${laneName(node.lane)} continues branch ${branch.name}.`
            : `Restoring this dump into ${laneName(node.lane)} starts a new branch from this point; ${branch.name} stays as it is.`}
        </p>
      )}
      <div className="flex flex-wrap gap-2">
        <Button size="sm" onClick={() => onRestore(d)} disabled={!isReady(node)}>
          <Play size={14} /> Restore to…
        </Button>
        <Button size="sm" variant="outline" onClick={() => window.open(`/api/dumps/${d.id}/download`, '_blank')} disabled={!isReady(node)} title="Download Archive">
          <Download size={14} />
        </Button>
        <Button size="sm" variant="ghost" className="hover:text-rose-400" onClick={() => onDelete(d)} disabled={d.status === 'pending'} title="Delete Dump">
          <Trash2 size={14} />
        </Button>
      </div>
      {chainView}
    </div>
  )
}
