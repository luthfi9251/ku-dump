import { useLayoutEffect, useMemo, useRef, useState } from 'react'
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
// Only the focused database and the databases related to it are drawn: where
// its data came from (upstream) and where its data went (downstream).

const LANE_COLORS = ['#f59e0b', '#a78bfa', '#2dd4bf', '#60a5fa', '#f472b6', '#a3e635', '#fb923c', '#38bdf8']
const MUTED = '#64748b'
const COL = 56
const LANE_H = 64
const PAD = 40 // room left of the first node for incoming restore stubs
const TAIL = COL // room right of the last node for the dashed lane tail
const TOP = 40

const isReady = (n: GraphNodeDTO) => n.kind === 'dump' && (n.status === 'ready' || n.status === 'uploaded')
const isState = (n: GraphNodeDTO) => (n.kind === 'dump' ? n.status !== 'failed' : n.status === 'success')

function fmtDay(iso: string) {
  return new Date(iso).toLocaleDateString(undefined, { month: 'short', day: 'numeric' })
}

function fmtTime(iso: string) {
  return new Date(iso).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' })
}

// relatedLanes walks restore edges from the focused lane: upstream to the
// lanes its data came from, downstream to the lanes its data went to. Siblings
// (other targets of an upstream lane) are left out. Returns lane -> depth,
// negative upstream, 0 for the focus, positive downstream.
function relatedLanes(data: DumpGraphDTO, focus: string): Map<string, number> {
  const byId = new Map(data.nodes.map((n) => [n.id, n]))
  const edges: [string, string][] = []
  for (const n of data.nodes) {
    const src = n.kind === 'restore' && n.source ? byId.get(n.source) : undefined
    if (src && src.lane !== n.lane) edges.push([src.lane, n.lane])
  }
  const depth = new Map<string, number>([[focus, 0]])
  const walk = (dir: 1 | -1) => {
    let frontier = [focus]
    for (let d = dir; frontier.length > 0; d += dir) {
      const next: string[] = []
      for (const lane of frontier) {
        for (const [from, to] of edges) {
          const other = dir === -1 ? (to === lane ? from : null) : from === lane ? to : null
          if (other && !depth.has(other)) {
            depth.set(other, d)
            next.push(other)
          }
        }
      }
      frontier = next
    }
  }
  walk(-1)
  walk(1)
  return depth
}

export default function DumpGraph({
  engine,
  databaseId,
  selectedId,
  onSelect,
  onRestore,
  onDelete,
  onOpenJob,
}: {
  engine: Engine
  databaseId: string
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
  const focus = `db-${databaseId}`
  const scroller = useRef<HTMLDivElement>(null)
  const [viewWidth, setViewWidth] = useState(0)
  // the graph opens at its right edge (newest) and stays pinned there while
  // new events arrive, unless the user scrolled back in time
  const pinnedRight = useRef(true)

  const layout = useMemo(() => {
    const data = graph.data
    if (!data) return null
    const laneColor = new Map<string, string>()
    data.lanes.forEach((l, i) => laneColor.set(l.id, l.kind === 'database' ? LANE_COLORS[i % LANE_COLORS.length] : MUTED))
    const depth = relatedLanes(data, focus)
    const order = new Map(data.lanes.map((l, i) => [l.id, i]))
    const lanes = data.lanes
      .filter((l) => depth.has(l.id))
      .sort((a, b) => depth.get(a.id)! - depth.get(b.id)! || order.get(a.id)! - order.get(b.id)!)
    const laneIndex = new Map(lanes.map((l, i) => [l.id, i]))
    const byId = new Map(data.nodes.map((n) => [n.id, n]))
    const nodes = data.nodes.filter((n) => laneIndex.has(n.lane))
    const contentWidth = PAD + nodes.length * COL + TAIL
    const width = Math.max(contentWidth, viewWidth)
    const offset = width - contentWidth // few events: keep them right-aligned too
    const pos = new Map<string, { x: number; y: number }>()
    nodes.forEach((n, i) => {
      pos.set(n.id, { x: offset + PAD + i * COL + COL / 2, y: TOP + laneIndex.get(n.lane)! * LANE_H + LANE_H / 2 })
    })
    // current state of each lane, for the dashed tail
    const laneState = new Map<string, string>()
    for (const n of nodes) if (isState(n)) laneState.set(n.lane, n.id)
    const height = TOP + lanes.length * LANE_H + 8
    return { data, lanes, nodes, laneColor, byId, pos, laneState, width, height, offset }
  }, [graph.data, focus, viewWidth])

  useLayoutEffect(() => {
    const el = scroller.current
    if (!el) return
    const ro = new ResizeObserver(() => setViewWidth(el.clientWidth))
    ro.observe(el)
    return () => ro.disconnect()
  }, [layout !== null])

  useLayoutEffect(() => {
    pinnedRight.current = true
  }, [focus])

  useLayoutEffect(() => {
    const el = scroller.current
    if (el && pinnedRight.current) el.scrollLeft = el.scrollWidth
  }, [layout])

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

  const { data, lanes, nodes, laneColor, byId, pos, laneState, width, height, offset } = layout
  if (nodes.length === 0) {
    return (
      <div className="rounded-2xl border border-slate-800 bg-slate-900/80 px-6 py-12 text-center text-sm text-slate-500">
        <GitBranch size={32} className="mx-auto mb-2 opacity-40" />
        No dumps or restores for this database yet. Run a workflow to create the first point.
      </div>
    )
  }

  const selected = selectedId ? byId.get(selectedId) : undefined
  const visible = (id: string) => pos.has(id)
  const dim = (id: string) => (selected && !onChain.has(id) ? 0.35 : 1)
  const laneName = (id: string) => data.lanes.find((l) => l.id === id)?.name ?? '?'

  // day separators
  const days: { x: number; label: string }[] = []
  let lastDay = ''
  nodes.forEach((n, i) => {
    const d = new Date(n.at).toDateString()
    if (d !== lastDay) {
      days.push({ x: offset + PAD + i * COL, label: fmtDay(n.at) })
      lastDay = d
    }
  })

  function select(id: string) {
    onSelect(id === selectedId ? null : id)
  }

  return (
    <div className="grid grid-cols-1 gap-4 xl:grid-cols-[minmax(0,1fr)_340px]">
      <div className="min-w-0 overflow-hidden rounded-2xl border border-slate-800 bg-slate-900/80 shadow-2xl backdrop-blur-md">
        <div className="flex">
          {/* lane names stay in place while the timeline scrolls */}
          <div className="relative w-32 shrink-0 border-r border-slate-800 bg-slate-950/40" style={{ height }}>
            {lanes.map((l: GraphLaneDTO, i) => (
              <div
                key={l.id}
                className="absolute inset-x-0 flex -translate-y-1/2 items-center gap-1.5 px-3 font-mono text-xs"
                style={{ top: TOP + i * LANE_H + LANE_H / 2 }}
                title={l.name}
              >
                <span className="h-2 w-2 shrink-0 rounded-full" style={{ background: laneColor.get(l.id) }} />
                <span
                  className={`truncate ${l.id === focus ? 'font-bold text-slate-100' : 'font-semibold'} ${l.kind === 'database' ? '' : 'italic'}`}
                  style={l.id === focus ? undefined : { color: laneColor.get(l.id) }}
                >
                  {l.name}
                </span>
              </div>
            ))}
          </div>
          <div
            ref={scroller}
            className="min-w-0 flex-1 overflow-x-auto"
            onScroll={(e) => {
              const el = e.currentTarget
              pinnedRight.current = el.scrollLeft + el.clientWidth >= el.scrollWidth - 4
            }}
          >
            <svg width={width} height={height} viewBox={`0 0 ${width} ${height}`} className="block font-mono" role="img" aria-label="Dump lineage graph">
              {lanes.map((l, i) => {
                const y = TOP + i * LANE_H + LANE_H / 2
                return (
                  <g key={l.id}>
                    {l.id === focus && <rect x={0} y={y - LANE_H / 2} width={width} height={LANE_H} fill="#6366f1" opacity={0.06} />}
                    <line x1={0} y1={y} x2={width} y2={y} stroke="#1e293b" />
                  </g>
                )
              })}
              {days.map((d) => (
                <g key={d.x}>
                  <line x1={d.x} y1={14} x2={d.x} y2={height} stroke="#1e293b" strokeDasharray="2 4" />
                  <text x={d.x + 6} y={24} fontSize={11} fill="#64748b">{d.label}</text>
                </g>
              ))}
              {/* lane segments: parent state -> dump */}
              {nodes.map((n) => {
                if (n.kind !== 'dump' || !n.parent || !visible(n.parent)) return null
                const a = pos.get(n.parent)!
                const b = pos.get(n.id)!
                const on = onChain.has(n.id) && onChain.has(n.parent)
                return <line key={'s' + n.id} x1={a.x} y1={a.y} x2={b.x} y2={b.y} stroke={laneColor.get(n.lane)} strokeWidth={on ? 5 : 3} opacity={selected && !on ? 0.35 : 1} />
              })}
              {[...laneState.entries()].map(([lane, id]) => {
                const p = pos.get(id)!
                return <line key={'t' + lane} x1={p.x} y1={p.y} x2={p.x + COL * 0.7} y2={p.y} stroke={laneColor.get(lane)} strokeWidth={3} strokeDasharray="3 4" opacity={0.6} />
              })}
              {/* restore edges: source dump -> restore; a stub when the source is not drawn */}
              {nodes.map((n) => {
                if (n.kind !== 'restore') return null
                const b = pos.get(n.id)!
                const src = n.source ? byId.get(n.source) : undefined
                const statusColor = n.status === 'pending' || n.status === 'running' ? '#fbbf24' : '#fb7185'
                if (!src || !visible(src.id)) {
                  return (
                    <g key={'r' + n.id} opacity={dim(n.id)}>
                      <line x1={b.x - COL * 0.75} y1={b.y} x2={b.x - 10} y2={b.y} stroke={src ? laneColor.get(src.lane) : MUTED} strokeWidth={2} strokeDasharray="2 3" />
                      <text x={b.x - 12} y={b.y - 8} fontSize={9.5} textAnchor="end" fill="#94a3b8">
                        {src ? `from ${laneName(src.lane)}` : 'source deleted'}
                      </text>
                    </g>
                  )
                }
                const a = pos.get(src.id)!
                const on = onChain.has(n.id) && onChain.has(src.id)
                const color = n.status === 'success' ? laneColor.get(src.lane) : statusColor
                const d =
                  a.y === b.y
                    ? `M${a.x} ${a.y} C ${a.x} ${a.y - 28}, ${b.x - 10} ${b.y - 28}, ${b.x - 6} ${b.y - 8}`
                    : `M${a.x} ${a.y} C ${a.x} ${(a.y + b.y) / 2}, ${b.x - COL * 0.6} ${b.y}, ${b.x - 10} ${b.y}`
                return (
                  <path key={'r' + n.id} d={d} fill="none" stroke={color} strokeWidth={on ? 3.5 : 2}
                    strokeDasharray={n.status === 'success' ? undefined : '4 3'} opacity={selected && !on ? 0.35 : 1} />
                )
              })}
              {nodes.map((n) => {
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
        node={selected && visible(selected.id) ? selected : undefined}
        chain={chain}
        byId={byId}
        visible={visible}
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
  visible,
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
  visible: (id: string) => boolean
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
              disabled={!visible(c.id)}
              title={visible(c.id) ? undefined : `${laneName(c.lane)} is not related to the selected database`}
              className={`inline-flex items-center gap-1.5 rounded-full border px-2 py-0.5 disabled:cursor-default disabled:opacity-60 ${c.id === node.id ? 'border-indigo-500/60 bg-indigo-950/60 text-indigo-200' : 'border-slate-700 bg-slate-800/60 text-slate-300 enabled:hover:border-slate-500'}`}
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
