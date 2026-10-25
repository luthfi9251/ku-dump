import { memo, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  BaseEdge,
  Controls,
  Handle,
  Position,
  ReactFlow,
  ReactFlowProvider,
  getBezierPath,
  useReactFlow,
  type Edge,
  type EdgeProps,
  type Node,
  type NodeProps,
  type Viewport,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { ArrowDown, ArrowUp, GitBranch } from 'lucide-react'
import { api } from '../lib/api'
import type { DumpDTO, DumpGraphDTO, Engine, GraphNodeDTO } from '../lib/types'
import { Spinner } from '../ui'
import { COL, NODE, ROW_H, buildLayout, chainOf, type Layout } from './layout'
import { BranchPanel, NodePanel, fmtTime } from './panels'

// Lineage graph of one database, drawn with React Flow. The canvas only pans
// and zooms; rows, day separators and lane tails are painted on a background
// layer that follows the viewport, and row names stay pinned on the left.

const LABEL_W = 176
const MAX_H = 620
const RESTORE_PENDING = '#fbbf24'
const RESTORE_FAILED = '#fb7185'

type PointData = {
  kind: 'dump' | 'restore' | 'upload' | 'stub'
  color: string
  status: string
  title: string
  label?: string
  selected: boolean
  dim: boolean
  onActivate?: () => void
}
type PointNode = Node<PointData, 'point'>
type LinkData = { kind: 'continue' | 'restore' }
type LinkEdge = Edge<LinkData, 'link'>

const hidden = { left: '50%', top: '50%', opacity: 0, width: 1, height: 1, minWidth: 0, minHeight: 0, border: 0 }

const PointView = memo(function PointView({ data }: NodeProps<PointNode>) {
  const restoreStroke = data.status === 'success' ? data.color : data.status === 'pending' || data.status === 'running' ? RESTORE_PENDING : RESTORE_FAILED
  const handles = (
    <>
      <Handle type="target" position={Position.Left} style={hidden} isConnectable={false} />
      <Handle type="source" position={Position.Right} style={hidden} isConnectable={false} />
    </>
  )
  if (data.kind === 'stub') {
    return (
      <div className="pointer-events-none relative" style={{ width: NODE, height: NODE }}>
        {handles}
        <span className="absolute bottom-full right-0 whitespace-nowrap font-mono text-[9.5px] text-slate-400">{data.label}</span>
      </div>
    )
  }
  return (
    <div
      role="button"
      tabIndex={0}
      aria-label={data.title}
      aria-pressed={data.selected}
      title={data.title}
      onKeyDown={(e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault()
          data.onActivate?.()
        }
      }}
      className="relative flex cursor-pointer items-center justify-center rounded-full outline-none focus-visible:ring-2 focus-visible:ring-indigo-300"
      style={{ width: NODE, height: NODE, opacity: data.dim ? 0.35 : 1 }}
    >
      {handles}
      {data.selected && <span className="absolute inset-0 rounded-full border-2 border-indigo-400" />}
      {data.kind === 'upload' ? (
        <span className="h-4 w-4 rounded-[3px]" style={{ background: data.color }} />
      ) : data.kind === 'dump' ? (
        <span className={`h-[17px] w-[17px] rounded-full ${data.status === 'pending' ? 'animate-pulse' : ''}`} style={{ background: data.color }} />
      ) : (
        <span
          className="h-[13px] w-[13px] rotate-45 bg-slate-950"
          style={{ border: `2.5px ${data.status === 'success' ? 'solid' : 'dashed'} ${restoreStroke}` }}
        />
      )}
      {data.label && <span className="pointer-events-none absolute top-full mt-0.5 max-w-[52px] truncate whitespace-nowrap font-mono text-[9.5px] text-slate-400">{data.label}</span>}
    </div>
  )
})

function LinkView({ id, sourceX, sourceY, targetX, targetY, data, style }: EdgeProps<LinkEdge>) {
  let path: string
  if (Math.abs(sourceY - targetY) < 1) {
    path =
      data?.kind === 'restore' && targetX - sourceX > COL * 1.5
        ? `M${sourceX} ${sourceY} C ${sourceX} ${sourceY - 30}, ${targetX - 12} ${targetY - 30}, ${targetX - 4} ${targetY - 9}`
        : `M${sourceX} ${sourceY} L${targetX} ${targetY}`
  } else {
    ;[path] = getBezierPath({ sourceX, sourceY, targetX, targetY, sourcePosition: Position.Right, targetPosition: Position.Left, curvature: 0.35 })
  }
  return <BaseEdge id={id} path={path} style={style} />
}

const nodeTypes = { point: PointView }
const edgeTypes = { link: LinkView }

export default function DumpGraph(props: {
  engine: Engine
  databaseId: string
  selectedId: string | null
  onSelect: (id: string | null) => void
  onRestore: (dump: DumpDTO) => void
  onDelete: (dump: DumpDTO) => void
  onOpenJob: (jobId: string) => void
}) {
  return (
    <ReactFlowProvider>
      <Graph {...props} />
    </ReactFlowProvider>
  )
}

function Graph({
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
  const layout = useMemo(() => (graph.data ? buildLayout(graph.data, focus) : null), [graph.data, focus])
  const chain = useMemo(() => (layout ? chainOf(layout.byId, selectedId) : []), [layout, selectedId])

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
  if (layout.placed.size === 0) {
    return (
      <div className="rounded-2xl border border-slate-800 bg-slate-900/80 px-6 py-12 text-center text-sm text-slate-500">
        <GitBranch size={32} className="mx-auto mb-2 opacity-40" />
        No dumps or restores for this database yet. Run a workflow to create the first point.
      </div>
    )
  }

  const selected = selectedId && layout.placed.has(selectedId) ? layout.byId.get(selectedId) : undefined
  return (
    <div className="grid grid-cols-1 items-start gap-4 xl:grid-cols-[minmax(0,1fr)_340px]">
      <div className="min-w-0 overflow-hidden rounded-2xl border border-slate-800 bg-slate-900/80 shadow-2xl backdrop-blur-md">
        <Canvas layout={layout} focus={focus} chain={chain} selectedId={selected?.id ?? null} onSelect={onSelect} />
        <div className="flex flex-wrap gap-x-5 gap-y-1.5 border-t border-slate-800 px-4 py-2.5 text-[11px] text-slate-400">
          <span className="inline-flex items-center gap-1.5"><span className="h-2.5 w-2.5 rounded-full bg-slate-400" />dump</span>
          <span className="inline-flex items-center gap-1.5"><span className="h-2.5 w-2.5 rounded-sm bg-slate-500" />uploaded file</span>
          <span className="inline-flex items-center gap-1.5"><span className="h-2.5 w-2.5 rotate-45 border-2 border-slate-400" />restore</span>
          <span className="inline-flex items-center gap-1.5"><span className="h-2.5 w-2.5 rotate-45 border-2 border-dashed border-rose-400" />failed / cancelled restore</span>
          <span>Drag or scroll to move back in time; newest is on the right.</span>
        </div>
      </div>
      {selected ? (
        <NodePanel node={selected} layout={layout} focus={focus} chain={chain} onSelect={onSelect} onRestore={onRestore} onDelete={onDelete} onOpenJob={onOpenJob} />
      ) : (
        <BranchPanel layout={layout} focus={focus} onSelect={(id) => onSelect(id)} />
      )}
    </div>
  )
}

function Canvas({
  layout,
  focus,
  chain,
  selectedId,
  onSelect,
}: {
  layout: Layout
  focus: string
  chain: GraphNodeDTO[]
  selectedId: string | null
  onSelect: (id: string | null) => void
}) {
  const rf = useReactFlow()
  const box = useRef<HTMLDivElement>(null)
  const [boxW, setBoxW] = useState(0)
  const [vp, setVp] = useState<Viewport>({ x: 0, y: 0, zoom: 1 })
  // pinned to the newest (right) edge until the user moves away from it
  const pinned = useRef(true)
  const height = Math.min(Math.max(layout.height + 16, 200), MAX_H)

  useLayoutEffect(() => {
    const el = box.current
    if (!el) return
    const ro = new ResizeObserver(() => setBoxW(el.clientWidth))
    ro.observe(el)
    return () => ro.disconnect()
  }, [])

  useEffect(() => {
    pinned.current = true
  }, [focus])

  const rightX = useCallback((zoom: number) => boxW - layout.width * zoom, [boxW, layout.width])

  useEffect(() => {
    if (!boxW || !pinned.current) return
    const { y, zoom } = rf.getViewport()
    const next = { x: rightX(zoom), y, zoom }
    void rf.setViewport(next)
    setVp(next)
  }, [boxW, layout, rightX, rf])

  const onChain = useMemo(() => new Set(chain.map((n) => n.id)), [chain])
  const dimmed = (id: string) => selectedId !== null && !onChain.has(id)

  const nodes: PointNode[] = useMemo(() => {
    const out: PointNode[] = []
    for (const p of layout.placed.values()) {
      const n = p.node
      const kind = n.kind === 'restore' ? 'restore' : n.lane === 'upload' ? 'upload' : 'dump'
      const title =
        n.kind === 'dump'
          ? `${n.dump?.label ?? 'dump'} · ${fmtTime(n.at)} · ${n.status}`
          : `restore ${n.source ? layout.byId.get(n.source)?.dump?.label : '(source deleted)'} → ${layout.laneName(n.lane)} · ${n.status}`
      out.push({
        id: n.id,
        type: 'point',
        position: { x: p.x - NODE / 2, y: p.y - NODE / 2 },
        draggable: false,
        selectable: false,
        data: {
          kind, color: p.row.color, status: n.status, title,
          label: n.kind === 'dump' && p.row.kind === 'branch' ? n.dump?.label : undefined,
          selected: n.id === selectedId, dim: dimmed(n.id),
          onActivate: () => onSelect(n.id === selectedId ? null : n.id),
        },
      })
    }
    for (const s of layout.stubs) {
      out.push({
        id: s.id, type: 'point', position: { x: s.x - NODE / 2, y: s.y - NODE / 2 }, draggable: false, selectable: false, focusable: false,
        data: { kind: 'stub', color: s.color, status: 'success', title: s.label, label: s.label, selected: false, dim: false },
      })
    }
    return out
  }, [layout, selectedId, onChain, onSelect])

  const edges: LinkEdge[] = useMemo(
    () =>
      layout.links.map((l) => {
        const on = onChain.has(l.to) && (onChain.has(l.from) || l.from.startsWith('stub-'))
        const color = l.kind === 'continue' || l.status === 'success' ? l.color : l.status === 'pending' || l.status === 'running' ? RESTORE_PENDING : RESTORE_FAILED
        return {
          id: l.id,
          source: l.from,
          target: l.to,
          type: 'link',
          selectable: false,
          animated: l.status === 'running' || l.status === 'pending',
          data: { kind: l.kind },
          style: {
            stroke: color,
            strokeWidth: l.kind === 'continue' ? (on ? 5 : 3) : on ? 3.5 : 2,
            strokeDasharray: l.from.startsWith('stub-') ? '2 3' : l.kind === 'restore' && l.status !== 'success' ? '4 3' : undefined,
            opacity: selectedId !== null && !on ? 0.3 : 1,
          },
        }
      }),
    [layout, onChain, selectedId],
  )

  const tf = `translate(${vp.x} ${vp.y}) scale(${vp.zoom})`
  return (
    <div ref={box} className="relative" style={{ height }}>
      {/* background: rows, day separators, tails; follows the viewport */}
      <svg className="pointer-events-none absolute inset-0 h-full w-full" aria-hidden="true">
        <g transform={tf}>
          {layout.rows.map((r) => (
            <g key={r.id}>
              {r.kind === 'branch' && r.branch?.active && (
                <rect x={-1e5} y={r.y - ROW_H / 2} width={2e5} height={ROW_H} fill="#6366f1" opacity={0.07} />
              )}
              <line x1={-1e5} y1={r.y} x2={1e5} y2={r.y} stroke="#1e293b" strokeWidth={1 / vp.zoom} />
            </g>
          ))}
          {layout.rows.some((r) => r.kind !== 'branch') &&
            layout.rows.map((r, i) =>
              i > 0 && r.kind !== layout.rows[i - 1].kind ? (
                <line key={'sep' + r.id} x1={-1e5} y1={r.y - ROW_H / 2} x2={1e5} y2={r.y - ROW_H / 2} stroke="#334155" strokeWidth={1 / vp.zoom} />
              ) : null,
            )}
          {layout.days.map((d) => (
            <g key={d.x}>
              <line x1={d.x} y1={14} x2={d.x} y2={layout.height} stroke="#1e293b" strokeDasharray="2 4" strokeWidth={1 / vp.zoom} />
              <text x={d.x + 6} y={26} fontSize={11} fill="#64748b" className="font-mono">{d.label}</text>
            </g>
          ))}
          {layout.tails.map((t) => (
            <line key={'tail' + t.row.id} x1={t.x} y1={t.row.y} x2={t.x + COL * 0.7} y2={t.row.y} stroke={t.row.color} strokeWidth={3} strokeDasharray="3 4" opacity={0.6} />
          ))}
        </g>
      </svg>

      <ReactFlow
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        edgeTypes={edgeTypes}
        colorMode="dark"
        className="!bg-transparent"
        defaultViewport={vp}
        minZoom={0.35}
        maxZoom={1.6}
        nodesDraggable={false}
        nodesConnectable={false}
        nodesFocusable={false}
        edgesFocusable={false}
        elementsSelectable={false}
        panOnScroll
        zoomOnScroll={false}
        zoomOnDoubleClick={false}
        proOptions={{ hideAttribution: true }}
        onNodeClick={(_, n) => {
          if (!n.id.startsWith('stub-')) onSelect(n.id === selectedId ? null : n.id)
        }}
        onPaneClick={() => onSelect(null)}
        onMove={(_, v) => setVp(v)}
        onMoveEnd={(e, v) => {
          // only user gestures change the pin; programmatic moves have no event
          if (e) pinned.current = Math.abs(v.x - rightX(v.zoom)) < 6
        }}
      >
        <Controls position="bottom-right" showInteractive={false} fitViewOptions={{ padding: 0.15 }} />
      </ReactFlow>

      {/* row names, pinned left */}
      <div
        className="pointer-events-none absolute inset-y-0 left-0 z-10 border-r border-slate-800 bg-[#0a0f1d] shadow-[8px_0_12px_-6px_rgba(0,0,0,0.6)]"
        style={{ width: LABEL_W }}
      >
        <div className="relative h-full overflow-hidden">
          {layout.rows.map((r) => {
            const top = vp.y + r.y * vp.zoom
            if (top < -20 || top > height + 20) return null
            return (
              <div key={r.id} className="absolute inset-x-0 flex -translate-y-1/2 items-center gap-1.5 px-3 text-xs" style={{ top }} title={r.label}>
                {r.kind === 'upstream' ? (
                  <ArrowUp size={12} className="shrink-0 text-slate-500" />
                ) : r.kind === 'downstream' ? (
                  <ArrowDown size={12} className="shrink-0 text-slate-500" />
                ) : (
                  <span className="h-2.5 w-2.5 shrink-0 rounded-full" style={{ background: r.color }} />
                )}
                <span className={`truncate font-mono ${r.kind === 'branch' ? 'font-semibold text-slate-100' : 'italic text-slate-400'}`}>{r.label}</span>
                {r.branch?.active && <span className="shrink-0 rounded-full bg-indigo-500/20 px-1.5 text-[9px] font-semibold uppercase tracking-wider text-indigo-300">active</span>}
              </div>
            )
          })}
        </div>
      </div>
    </div>
  )
}
