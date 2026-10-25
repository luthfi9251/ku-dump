import type { DumpGraphDTO, GraphBranchDTO, GraphLaneDTO, GraphNodeDTO } from '../lib/types'

// Layout of the dump graph. The focused database is split into one row per
// branch; related databases (where its data came from, above, and where its
// data went to, below) get one compact row each. Time flows to the right:
// every event takes one column, in the order the API returns them.

export const COL = 56
export const ROW_H = 60
export const TOP = 40
export const PAD = 48 // room left of the first event for incoming restore stubs
export const TAIL = COL // room right of the last event for the dashed tail
export const NODE = 30 // node box size

const BRANCH_COLORS = ['#2dd4bf', '#f59e0b', '#a78bfa', '#f472b6', '#60a5fa', '#a3e635', '#fb923c', '#38bdf8']
const LANE_COLOR = '#64748b'

export const isReady = (n: GraphNodeDTO) => n.kind === 'dump' && (n.status === 'ready' || n.status === 'uploaded')
export const isState = (n: GraphNodeDTO) => (n.kind === 'dump' ? n.status !== 'failed' : n.status === 'success')

export interface Row {
  id: string
  kind: 'branch' | 'upstream' | 'downstream'
  laneId: string
  branch?: GraphBranchDTO
  label: string
  color: string
  y: number // center, flow coordinates
}

export interface Placed {
  node: GraphNodeDTO
  row: Row
  x: number // center, flow coordinates
  y: number
}

export interface Link {
  id: string
  from: string // node id, or a stub id for a source that is not drawn
  to: string
  kind: 'continue' | 'restore'
  status: string
  color: string
}

export interface Stub {
  id: string
  x: number
  y: number
  label: string
  color: string
}

export interface Layout {
  rows: Row[]
  placed: Map<string, Placed>
  links: Link[]
  stubs: Stub[]
  tails: { row: Row; x: number }[]
  days: { x: number; label: string }[]
  byId: Map<string, GraphNodeDTO>
  branches: Map<string, GraphBranchDTO>
  laneName: (id: string) => string
  width: number
  height: number
}

// relatedLanes walks restore edges from the focused lane: upstream to the
// lanes its data came from, downstream to the lanes its data went to.
// Siblings (other targets of an upstream lane) are left out. Returns
// lane -> depth, negative upstream, 0 for the focus, positive downstream.
export function relatedLanes(data: DumpGraphDTO, focus: string): Map<string, number> {
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

function fmtDay(iso: string) {
  return new Date(iso).toLocaleDateString(undefined, { month: 'short', day: 'numeric' })
}

export function buildLayout(data: DumpGraphDTO, focus: string): Layout {
  const byId = new Map(data.nodes.map((n) => [n.id, n]))
  const branches = new Map(data.branches.map((b) => [b.id, b]))
  const lanesById = new Map<string, GraphLaneDTO>(data.lanes.map((l) => [l.id, l]))
  const laneName = (id: string) => lanesById.get(id)?.name ?? '?'
  const depth = relatedLanes(data, focus)
  const nodes = data.nodes.filter((n) => depth.has(n.lane))

  // rows: upstream lanes, then the focus branches by first appearance, then downstream lanes
  const laneOrder = new Map(data.lanes.map((l, i) => [l.id, i]))
  const related = [...depth.keys()].filter((l) => l !== focus)
  related.sort((a, b) => depth.get(a)! - depth.get(b)! || laneOrder.get(a)! - laneOrder.get(b)!)
  const branchOrder: string[] = []
  for (const n of nodes) {
    if (n.lane !== focus) continue
    const key = n.branch ?? 'none'
    if (!branchOrder.includes(key)) branchOrder.push(key)
  }
  const rows: Row[] = []
  const add = (r: Omit<Row, 'y'>) => rows.push({ ...r, y: TOP + rows.length * ROW_H + ROW_H / 2 })
  for (const lane of related.filter((l) => depth.get(l)! < 0)) {
    add({ id: `lane:${lane}`, kind: 'upstream', laneId: lane, label: laneName(lane), color: LANE_COLOR })
  }
  branchOrder.forEach((key, i) => {
    const b = branches.get(key)
    add({
      id: `branch:${key}`, kind: 'branch', laneId: focus, branch: b,
      label: b?.name ?? 'unassigned', color: BRANCH_COLORS[i % BRANCH_COLORS.length],
    })
  })
  for (const lane of related.filter((l) => depth.get(l)! > 0)) {
    add({ id: `lane:${lane}`, kind: 'downstream', laneId: lane, label: laneName(lane), color: LANE_COLOR })
  }
  const rowById = new Map(rows.map((r) => [r.id, r]))
  const rowOf = (n: GraphNodeDTO) => rowById.get(n.lane === focus ? `branch:${n.branch ?? 'none'}` : `lane:${n.lane}`)!

  const placed = new Map<string, Placed>()
  nodes.forEach((n, i) => {
    const row = rowOf(n)
    placed.set(n.id, { node: n, row, x: PAD + i * COL + COL / 2, y: row.y })
  })

  const links: Link[] = []
  const stubs: Stub[] = []
  for (const p of placed.values()) {
    const n = p.node
    if (n.kind === 'dump' && n.parent && placed.has(n.parent)) {
      links.push({ id: `c-${n.id}`, from: n.parent, to: n.id, kind: 'continue', status: 'success', color: p.row.color })
    }
    if (n.kind !== 'restore') continue
    const src = n.source ? placed.get(n.source) : undefined
    if (src) {
      links.push({ id: `r-${n.id}`, from: src.node.id, to: n.id, kind: 'restore', status: n.status, color: src.row.color })
    } else {
      const from = n.source ? byId.get(n.source) : undefined
      const stub: Stub = {
        id: `stub-${n.id}`, x: p.x - COL * 0.8, y: p.y,
        label: from ? `from ${laneName(from.lane)}` : 'source deleted', color: LANE_COLOR,
      }
      stubs.push(stub)
      links.push({ id: `r-${n.id}`, from: stub.id, to: n.id, kind: 'restore', status: n.status, color: LANE_COLOR })
    }
  }

  // dashed tail after the current state of each lane; in the focus database
  // only the active branch holds the current state
  const lastState = new Map<string, Placed>()
  for (const p of placed.values()) if (isState(p.node)) lastState.set(p.node.lane, p)
  const tails = [...lastState.values()].map((p) => ({ row: p.row, x: p.x }))

  const days: { x: number; label: string }[] = []
  let lastDay = ''
  nodes.forEach((n, i) => {
    const d = new Date(n.at).toDateString()
    if (d !== lastDay) {
      days.push({ x: PAD + i * COL, label: fmtDay(n.at) })
      lastDay = d
    }
  })

  return {
    rows, placed, links, stubs, tails, days, byId, branches, laneName,
    width: PAD + nodes.length * COL + TAIL,
    height: TOP + rows.length * ROW_H + 8,
  }
}

// chainOf walks back from a node: dump -> parent state, restore -> source.
export function chainOf(byId: Map<string, GraphNodeDTO>, id: string | null): GraphNodeDTO[] {
  const out: GraphNodeDTO[] = []
  let cur = id ? byId.get(id) : undefined
  while (cur && out.length < 1000) {
    out.push(cur)
    const next = cur.kind === 'dump' ? cur.parent : cur.source
    cur = next ? byId.get(next) : undefined
  }
  return out
}
