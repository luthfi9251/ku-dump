import { useEffect, useMemo, useRef } from 'react'
import { useQuery } from '@tanstack/react-query'
import { ArrowLeftRight, Download, GitBranch, Play, Terminal, Trash2 } from 'lucide-react'
import { api } from './lib/api'
import { fmtSize } from './lib/format'
import type { DumpDTO, DumpGraphDTO, Engine, GraphLaneDTO, GraphNodeDTO } from './lib/types'
import { Badge, Button, Spinner } from './ui'
import { statusTone } from './JobDrawer'

// Lineage graph of dumps: one lane per database, time flows to the right.
// ● dump, ■ uploaded file, ◇ restore. A dump links to its parent state in the
// same lane; a restore links from its source dump, possibly in another lane.

const LANE_COLORS = ['#f59e0b', '#a78bfa', '#2dd4bf', '#60a5fa', '#f472b6', '#a3e635', '#fb923c', '#38bdf8']
const MUTED = '#64748b'
const COL = 56
const LANE_H = 64
const LEFT = 150
const TOP = 40

const isReady = (n: GraphNodeDTO) => n.kind === 'dump' && (n.status === 'ready' || n.status === 'uploaded')
const isState = (n: GraphNodeDTO) => (n.kind === 'dump' ? n.status !== 'failed' : n.status === 'success')

function fmtDay(iso: string) {
  return new Date(iso).toLocaleDateString(undefined, { month: 'short', day: 'numeric' })
}

function fmtTime(iso: string) {
  return new Date(iso).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' })
}

export default function DumpGraph({
  engine,
  selectedId,
  onSelect,
  onRestore,
  onDelete,
  onOpenJob,
}: {
  engine: Engine
  selectedId: string | null
  onSelect: (id: string | null) => void
  onRestore: (dump: DumpDTO) => void
  onDelete: (dump: DumpDTO) => void
  onOpenJob: (jobId: string) => void
}) {
  const graph = useQuery({
    queryKey: ['dump-graph', engine],
    queryFn: () => api.get<DumpGraphDTO>(`/api/dumps/graph?engine=${engine}`),
    refetchInterval: 10000,
  })
  const scroller = useRef<HTMLDivElement>(null)
  const scrolledFor = useRef<string | null>(null)

  const layout = useMemo(() => {
    const data = graph.data
    if (!data) return null
    const laneIndex = new Map<string, number>()
    const laneColor = new Map<string, string>()
    data.lanes.forEach((l, i) => {
      laneIndex.set(l.id, i)
      laneColor.set(l.id, l.kind === 'database' ? LANE_COLORS[i % LANE_COLORS.length] : MUTED)
    })
    const byId = new Map<string, GraphNodeDTO>()
    const pos = new Map<string, { x: number; y: number }>()
    data.nodes.forEach((n, i) => {
      byId.set(n.id, n)
      pos.set(n.id, { x: LEFT + i * COL + COL / 2, y: TOP + (laneIndex.get(n.lane) ?? 0) * LANE_H + LANE_H / 2 })
    })
    // current state of each lane, for the dashed tail
    const laneState = new Map<string, string>()
    for (const n of data.nodes) if (isState(n)) laneState.set(n.lane, n.id)
    const width = LEFT + Math.max(data.nodes.length, 8) * COL + COL
    const height = TOP + data.lanes.length * LANE_H + 8
    return { data, laneIndex, laneColor, byId, pos, laneState, width, height }
  }, [graph.data])

  // open on the newest events
  useEffect(() => {
    if (layout && scroller.current && scrolledFor.current !== engine) {
      scroller.current.scrollLeft = scroller.current.scrollWidth
      scrolledFor.current = engine
    }
  }, [layout, engine])

  const chain = useMemo(() => {
    const out: GraphNodeDTO[] = []
    if (!layout || !selectedId) return out
    let cur = layout.byId.get(selectedId)
    while (cur && out.length < 500) {
      out.push(cur)
      const next = cur.kind === 'dump' ? cur.parent : cur.source
      cur = next ? layout.byId.get(next) : undefined
    }
    return out
  }, [layout, selectedId])
  const onChain = new Set(chain.map((n) => n.id))

  if (graph.isLoading) {
    return (
      <div className="flex justify-center rounded-2xl border border-slate-800 bg-slate-900/80 p-12">
        <Spinner />
      </div>
    )
  }
  if (!layout) {
    return <div className="rounded-2xl border border-rose-800/60 bg-rose-950/40 p-4 text-sm text-rose-300">Could not load the dump graph.</div>
  }
  if (layout.data.nodes.length === 0) {
    return (
      <div className="rounded-2xl border border-slate-800 bg-slate-900/80 px-6 py-12 text-center text-sm text-slate-500">
        <GitBranch size={32} className="mx-auto mb-2 opacity-40" />
        No {engine === 'postgres' ? 'PostgreSQL' : 'MongoDB'} dumps or restores yet. Run a workflow to create the first point.
      </div>
    )
  }

  const { data, laneColor, byId, pos, laneState, width, height } = layout
  const selected = selectedId ? byId.get(selectedId) : undefined
  const dim = (id: string) => (selected && !onChain.has(id) ? 0.35 : 1)
  const laneName = (id: string) => data.lanes.find((l) => l.id === id)?.name ?? '?'

  // day separators
  const days: { x: number; label: string }[] = []
  let lastDay = ''
  data.nodes.forEach((n, i) => {
    const d = new Date(n.at).toDateString()
    if (d !== lastDay) {
      days.push({ x: LEFT + i * COL, label: fmtDay(n.at) })
      lastDay = d
    }
  })

  function select(id: string) {
    onSelect(id === selectedId ? null : id)
  }

  return (
    <div className="grid grid-cols-1 gap-4 xl:grid-cols-[minmax(0,1fr)_340px]">
      <div className="min-w-0 overflow-hidden rounded-2xl border border-slate-800 bg-slate-900/80 shadow-2xl backdrop-blur-md">
        <div ref={scroller} className="overflow-x-auto">
          <svg width={width} height={height} viewBox={`0 0 ${width} ${height}`} className="block font-mono" role="img" aria-label="Dump lineage graph">
            {days.map((d) => (
              <g key={d.x}>
                <line x1={d.x} y1={14} x2={d.x} y2={height} stroke="#1e293b" strokeDasharray="2 4" />
                <text x={d.x + 6} y={24} fontSize={11} fill="#64748b">{d.label}</text>
              </g>
            ))}
            {data.lanes.map((l: GraphLaneDTO, i) => {
              const y = TOP + i * LANE_H + LANE_H / 2
              return (
                <g key={l.id}>
                  <line x1={LEFT - 8} y1={y} x2={width} y2={y} stroke="#1e293b" />
                  <text x={16} y={y + 4} fontSize={12} fontWeight={600} fill={laneColor.get(l.id)} fontStyle={l.kind === 'database' ? 'normal' : 'italic'}>
                    {l.name.length > 18 ? l.name.slice(0, 17) + '…' : l.name}
                    <title>{l.name}</title>
                  </text>
                </g>
              )
            })}
            {/* lane segments: parent state -> dump */}
            {data.nodes.map((n) => {
              if (n.kind !== 'dump' || !n.parent) return null
              const a = pos.get(n.parent)!
              const b = pos.get(n.id)!
              const on = onChain.has(n.id) && onChain.has(n.parent)
              return <line key={'s' + n.id} x1={a.x} y1={a.y} x2={b.x} y2={b.y} stroke={laneColor.get(n.lane)} strokeWidth={on ? 5 : 3} opacity={selected && !on ? 0.35 : 1} />
            })}
            {[...laneState.entries()].map(([lane, id]) => {
              const p = pos.get(id)!
              return <line key={'t' + lane} x1={p.x} y1={p.y} x2={p.x + COL * 0.7} y2={p.y} stroke={laneColor.get(lane)} strokeWidth={3} strokeDasharray="3 4" opacity={0.6} />
            })}
            {/* restore edges: source dump -> restore */}
            {data.nodes.map((n) => {
              if (n.kind !== 'restore' || !n.source) return null
              const src = byId.get(n.source)!
              const a = pos.get(src.id)!
              const b = pos.get(n.id)!
              const on = onChain.has(n.id) && onChain.has(src.id)
              const color = n.status === 'success' ? laneColor.get(src.lane) : n.status === 'pending' || n.status === 'running' ? '#fbbf24' : '#fb7185'
              const d =
                a.y === b.y
                  ? `M${a.x} ${a.y} C ${a.x} ${a.y - 28}, ${b.x - 10} ${b.y - 28}, ${b.x - 6} ${b.y - 8}`
                  : `M${a.x} ${a.y} C ${a.x} ${(a.y + b.y) / 2}, ${b.x - COL * 0.6} ${b.y}, ${b.x - 10} ${b.y}`
              return (
                <path key={'r' + n.id} d={d} fill="none" stroke={color} strokeWidth={on ? 3.5 : 2}
                  strokeDasharray={n.status === 'success' ? undefined : '4 3'} opacity={selected && !on ? 0.35 : 1} />
              )
            })}
            {data.nodes.map((n) => {
              const { x, y } = pos.get(n.id)!
              const color = laneColor.get(n.lane)!
              const isSel = n.id === selectedId
              const title =
                n.kind === 'dump'
                  ? `${n.dump?.label ?? 'dump'} · ${fmtTime(n.at)} · ${n.status}`
                  : `restore ${n.source ? byId.get(n.source)?.dump?.label : '(source deleted)'} → ${laneName(n.lane)} · ${n.status}`
              const restoreStroke = n.status === 'success' ? color : n.status === 'pending' || n.status === 'running' ? '#fbbf24' : '#fb7185'
              return (
                <g
                  key={n.id}
                  className="cursor-pointer focus:outline-none [&:focus-visible>.ring]:stroke-indigo-300"
                  tabIndex={0}
                  role="button"
                  aria-label={title}
                  aria-pressed={isSel}
                  opacity={dim(n.id)}
                  onClick={() => select(n.id)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' || e.key === ' ') {
                      e.preventDefault()
                      select(n.id)
                    }
                  }}
                >
                  <title>{title}</title>
                  <circle className="ring" cx={x} cy={y} r={15} fill="transparent" stroke={isSel ? '#818cf8' : 'transparent'} strokeWidth={2} />
                  {n.kind === 'dump' && n.lane === 'upload' ? (
                    <rect x={x - 8} y={y - 8} width={16} height={16} rx={3} fill={color} />
                  ) : n.kind === 'dump' ? (
                    <circle cx={x} cy={y} r={8.5} fill={color} className={n.status === 'pending' ? 'animate-pulse' : undefined} />
                  ) : (
                    <path d={`M${x} ${y - 9} L${x + 9} ${y} L${x} ${y + 9} L${x - 9} ${y} Z`} fill="#0f172a" stroke={restoreStroke} strokeWidth={2.5}
                      strokeDasharray={n.status === 'success' ? undefined : '3 2'} />
                  )}
                </g>
              )
            })}
          </svg>
        </div>
        <div className="flex flex-wrap gap-x-5 gap-y-1.5 border-t border-slate-800 px-4 py-2.5 text-[11px] text-slate-400">
          <span className="inline-flex items-center gap-1.5"><span className="h-2.5 w-2.5 rounded-full bg-slate-400" />dump</span>
          <span className="inline-flex items-center gap-1.5"><span className="h-2.5 w-2.5 rounded-sm bg-slate-500" />uploaded file</span>
          <span className="inline-flex items-center gap-1.5"><span className="h-2.5 w-2.5 rotate-45 border-2 border-slate-400" />restore</span>
          <span className="inline-flex items-center gap-1.5"><span className="h-2.5 w-2.5 rotate-45 border-2 border-dashed border-rose-400" />failed / cancelled restore</span>
          <span>A break in a lane before ◇ means the restore replaced the database contents.</span>
        </div>
      </div>

      <NodePanel
        node={selected}
        chain={chain}
        byId={byId}
        laneName={laneName}
        laneColor={(id) => laneColor.get(id) ?? MUTED}
        onSelect={onSelect}
        onRestore={onRestore}
        onDelete={onDelete}
        onOpenJob={onOpenJob}
      />
    </div>
  )
}

function NodePanel({
  node,
  chain,
  byId,
  laneName,
  laneColor,
  onSelect,
  onRestore,
  onDelete,
  onOpenJob,
}: {
  node: GraphNodeDTO | undefined
  chain: GraphNodeDTO[]
  byId: Map<string, GraphNodeDTO>
  laneName: (id: string) => string
  laneColor: (id: string) => string
  onSelect: (id: string | null) => void
  onRestore: (dump: DumpDTO) => void
  onDelete: (dump: DumpDTO) => void
  onOpenJob: (jobId: string) => void
}) {
  const box = 'rounded-2xl border border-slate-800 bg-slate-900/80 p-5 shadow-2xl backdrop-blur-md'
  if (!node) {
    return (
      <div className={`${box} text-sm text-slate-400`}>
        <p className="font-semibold text-slate-200">Pick a point</p>
        <p className="mt-1 text-xs leading-relaxed">
          Click a dump to see where its data came from and restore it to any
          database of the same engine. Click a restore to read its log.
        </p>
      </div>
    )
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
              className={`inline-flex items-center gap-1.5 rounded-full border px-2 py-0.5 ${c.id === node.id ? 'border-indigo-500/60 bg-indigo-950/60 text-indigo-200' : 'border-slate-700 bg-slate-800/60 text-slate-300 hover:border-slate-500'}`}
            >
              <span className="h-2 w-2 rounded-full" style={{ background: laneColor(c.lane) }} />
              {c.kind === 'dump' ? c.dump?.label : `restore → ${laneName(c.lane)}`}
            </button>
          </li>
        ))}
      </ol>
    </div>
  )
  const row = (k: string, v: React.ReactNode) => (
    <>
      <dt className="text-slate-500">{k}</dt>
      <dd className="min-w-0 break-words text-slate-200">{v}</dd>
    </>
  )

  if (node.kind === 'restore') {
    const src = node.source ? byId.get(node.source) : undefined
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
          {row('Source', src ? `${src.dump?.label} (${laneName(src.lane)})` : <span className="italic text-slate-500">dump deleted</span>)}
          {row('Started', fmtTime(node.at))}
          {node.finishedAt && row('Finished', fmtTime(node.finishedAt))}
        </dl>
        <p className="text-xs leading-relaxed text-slate-400">
          {node.status === 'success'
            ? 'A restore is an event, not a file. To bring a database back to this state, restore its source dump.'
            : 'This restore did not complete, so it did not change the database. Later dumps of this lane keep the earlier state as parent.'}
        </p>
        <div className="flex flex-wrap gap-2">
          {node.jobId && (
            <Button size="sm" variant="outline" onClick={() => onOpenJob(node.jobId!)}>
              <Terminal size={14} /> View log
            </Button>
          )}
          {src && (
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
      : parent.source
        ? `${byId.get(parent.source)?.dump?.label} (restored from ${laneName(byId.get(parent.source)!.lane)})`
        : 'a restore whose source dump was deleted'
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
        {row('Database', d.databaseName || <span className="italic text-slate-500">uploaded file</span>)}
        {row('Created', fmtTime(d.createdAt))}
        {row('Workflow', d.workflowName || '—')}
        {row('Storage', d.destName || 'local')}
        {row('Size', fmtSize(d.sizeBytes))}
        {row('Parent', parentText)}
      </dl>
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
