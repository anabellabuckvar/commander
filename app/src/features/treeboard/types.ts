// The board tree cmdrd serves at GET /api/tree (internal/livedata/tree.go).
// Every text the view shows about state comes from these fields; the view
// never works a state out for itself, so it reads exactly what the Commander reads.

export interface TreeLine {
  tone: 'you' | 'agent' | 'review' | 'person' | 'none' | 'bad' | 'sum'
  text: string
}

export interface TreeRow {
  id: string
  key: string
  project: string
  parent?: string
  label: string
  depth: number
  title: string
  tix?: string[]
  ac?: string[]
  sum?: string
  note?: string
  done: boolean
  pct: number
  of: string
  needs_you: number
  state: string
  status: TreeLine[]
  line: TreeLine
  now?: string
}

export interface TreeStep {
  id: string
  row: string
  title: string
  type: 'you' | 'agent' | 'review' | 'person'
  owner: string
  state: 'active' | 'notstarted' | 'done'
  col: number
  run?: string
  model?: string
  asleep?: boolean
  own: string
  wt?: string
  ask?: string
  text?: string
  since?: number
  gate?: boolean
  ac?: string
}

export interface Tree {
  version: number
  asof: number
  done: number
  total: number
  needs_you: number
  rows: TreeRow[]
  steps: TreeStep[]
  edges: [string, string][]
}

/** Selection is what the right panel shows: it rides along with each message to the Commander. */
export interface Selection {
  kind: 'workstream' | 'step'
  id: string
  key: string // project:bead for a workstream; the row's key for a step
  label: string // the workstream's place, e.g. "3.2"
  title: string
  row?: string // a step's workstream title
}
