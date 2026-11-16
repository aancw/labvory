import { useMemo, useState, useCallback, useEffect } from "react"
import { useNavigate } from "react-router-dom"
import {
  Background,
  Controls,
  ReactFlow,
  useNodesState,
  useEdgesState,
  getSmoothStepPath,
  BaseEdge,
  type Edge,
  type EdgeProps,
  type Node,
} from "@xyflow/react"
import "@xyflow/react/dist/style.css"
import { WaypointsIcon } from "lucide-react"

import { useTopology } from "@/api/hooks"
import type { TopologyNode, TopologyEdge } from "@/api/types"
import { EmptyState } from "@/components/EmptyState"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { cn } from "@/lib/utils"

type ViewMode = "all" | "physical" | "virtualization" | "dependency"

const VIEW_LABELS: Record<ViewMode, string> = {
  all: "All links",
  physical: "Physical connections",
  virtualization: "Virtualization",
  dependency: "Dependencies",
}

/** Simple layered layout: hosts at top, guests below, dependencies to the side. */
function computeLayout(nodes: TopologyNode[], edges: TopologyEdge[], view: ViewMode): { nodes: Node[]; edges: Edge[] } {
  // Depth = longest parent chain (for the vertical axis).
  const parentOf = new Map(nodes.map((n) => [n.id, n.parent_id ?? ""]))
  const depth = new Map<string, number>()
  const depthOf = (id: string, guard = 0): number => {
    if (depth.has(id)) return depth.get(id)!
    if (guard > 16) return 0
    const p = parentOf.get(id)
    const d = p ? depthOf(p, guard + 1) + 1 : 0
    depth.set(id, d)
    return d
  }
  nodes.forEach((n) => depthOf(n.id))

  const columns = new Map<string, number>() // depth -> running column index
  const positioned = nodes.map((n) => {
    const d = depth.get(n.id) ?? 0
    const col = columns.get(String(d)) ?? 0
    columns.set(String(d), col + 1)
    return {
      id: n.id,
      type: "asset",
      data: { asset: n },
      position: { x: col * 230, y: d * 160 },
    } satisfies Node
  })

  const keptEdges = edges.filter((e) => {
    if (view === "physical") return e.kind === "connected_to"
    if (view === "virtualization") return e.kind === "hosts" || e.kind === "runs_on"
    if (view === "dependency") return e.kind === "depends_on"
    return true
  })

  const flowEdges: Edge[] = keptEdges.map((e) => ({
    id: e.id,
    source: e.source,
    target: e.target,
    type: "lanvory",
    label: e.kind === "hosts" ? "" : e.kind + (e.source_port || e.target_port ? ` (${e.source_port || "?"}↔${e.target_port || "?"})` : ""),
    style: edgeStyle(e.kind),
    labelStyle: { fontSize: 10, fill: "var(--color-muted-foreground)" },
    labelBgStyle: { fill: "var(--color-card)", fillOpacity: 0.9 },
  }))

  return { nodes: positioned, edges: flowEdges }
}

function edgeStyle(kind: TopologyEdge["kind"]) {
  switch (kind) {
    case "hosts":
      return { stroke: "var(--color-border)", strokeWidth: 1.5 }
    case "connected_to":
      return { stroke: "#0ea5e9", strokeWidth: 2 }
    case "depends_on":
      return { stroke: "#f59e0b", strokeWidth: 2 }
    default:
      return { stroke: "#8b5cf6", strokeWidth: 2 }
  }
}

function AssetNode({ data, selected }: { data: { asset: TopologyNode }; selected?: boolean }) {
  const asset = data.asset
  const navigate = useNavigate()
  return (
    <button
      type="button"
      onClick={() => navigate(`/assets/${asset.id}`)}
      className={cn(
        "w-52 rounded-lg border bg-card px-3 py-2 text-left shadow-xs transition-colors",
        selected ? "border-primary ring-2 ring-ring/40" : "border-border hover:border-primary/40",
      )}
    >
      <p className="truncate text-sm font-medium">{asset.name}</p>
      <p className="truncate text-xs text-muted-foreground">
        {asset.type_label}
        {asset.hostname ? <span className="ml-1 font-mono">{asset.hostname}</span> : null}
      </p>
      <div className="mt-1.5 flex items-center justify-between">
        <span
          className={cn(
            "inline-block size-2 rounded-full",
            asset.status === "online" ? "bg-emerald-500" : asset.status === "maintenance" ? "bg-amber-500" : "bg-zinc-400",
          )}
          aria-hidden
        />
        {asset.primary_ip ? <span className="font-mono text-[10px] text-muted-foreground">{asset.primary_ip}</span> : null}
      </div>
    </button>
  )
}

const nodeTypes = { asset: AssetNode }

function LanvoryEdge(props: EdgeProps) {
  const [edgePath, labelX, labelY] = getSmoothStepPath({
    sourceX: props.sourceX,
    sourceY: props.sourceY,
    targetX: props.targetX,
    targetY: props.targetY,
  })
  const label = typeof props.label === "string" ? props.label : ""
  return (
    <>
      <BaseEdge id={props.id} path={edgePath} style={props.style as React.CSSProperties | undefined} />
      {label ? (
        <g>
          <rect x={labelX - 40} y={labelY - 9} width={80} height={18} rx={4} fill="var(--color-card)" opacity={0.9} />
          <text x={labelX} y={labelY} textAnchor="middle" dominantBaseline="middle" fontSize={10} fill="var(--color-muted-foreground)">
            {label}
          </text>
        </g>
      ) : null}
    </>
  )
}

const edgeTypes = { lanvory: LanvoryEdge }

const LAYOUT_STORAGE = "lanvory-topology-positions"

export function TopologyPage() {
  const { data, isLoading, isError } = useTopology()
  const [view, setView] = useState<ViewMode>("all")
  const [categoryFilter, setCategoryFilter] = useState<"all" | "physical" | "virtual">("all")

  const { nodes: layoutNodes, edges: layoutEdges } = useMemo(() => {
    if (!data) return { nodes: [] as Node[], edges: [] as Edge[] }
    const filteredNodes = data.nodes.filter((n) => categoryFilter === "all" || n.category === categoryFilter)
    const ids = new Set(filteredNodes.map((n) => n.id))
    const filteredEdges = data.edges.filter((e) => ids.has(e.source) && ids.has(e.target))
    return computeLayout(filteredNodes, filteredEdges, view)
  }, [data, view, categoryFilter])

  const [nodes, setNodes, onNodesChange] = useNodesState(layoutNodes)
  const [edges, setEdges, onEdgesChange] = useEdgesState(layoutEdges)

  // Re-layout when filters change, restoring any saved drag positions.
  useEffect(() => {
    let saved: Record<string, { x: number; y: number }> = {}
    try {
      saved = JSON.parse(localStorage.getItem(LAYOUT_STORAGE) ?? "{}")
    } catch {
      saved = {}
    }
    setNodes(layoutNodes.map((n) => (saved[n.id] ? { ...n, position: saved[n.id] } : n)))
    setEdges(layoutEdges)
  }, [layoutNodes, layoutEdges, setNodes, setEdges])

  const onNodeDragStop = useCallback(
    (_event: unknown, node: Node) => {
      let saved: Record<string, { x: number; y: number }> = {}
      try {
        saved = JSON.parse(localStorage.getItem(LAYOUT_STORAGE) ?? "{}")
      } catch {
        saved = {}
      }
      saved[node.id] = node.position
      localStorage.setItem(LAYOUT_STORAGE, JSON.stringify(saved))
    },
    [],
  )

  const resetLayout = () => {
    localStorage.removeItem(LAYOUT_STORAGE)
    setNodes(layoutNodes)
  }

  const nodeCount = nodes.length

  return (
    <>
      <div className="mb-4 flex flex-wrap items-center gap-2">
        <div className="flex rounded-md border p-0.5" role="radiogroup" aria-label="Topology view">
          {(Object.keys(VIEW_LABELS) as ViewMode[]).map((v) => (
            <button
              key={v}
              type="button"
              role="radio"
              aria-checked={view === v}
              onClick={() => setView(v)}
              className={cn(
                "rounded px-2.5 py-1 text-xs transition-colors",
                view === v ? "bg-primary text-primary-foreground" : "text-muted-foreground hover:text-foreground",
              )}
            >
              {VIEW_LABELS[v]}
            </button>
          ))}
        </div>
        <div className="flex rounded-md border p-0.5" role="radiogroup" aria-label="Category filter">
          {(["all", "physical", "virtual"] as const).map((c) => (
            <button
              key={c}
              type="button"
              role="radio"
              aria-checked={categoryFilter === c}
              onClick={() => setCategoryFilter(c)}
              className={cn(
                "rounded px-2.5 py-1 text-xs capitalize transition-colors",
                categoryFilter === c ? "bg-secondary text-secondary-foreground" : "text-muted-foreground hover:text-foreground",
              )}
            >
              {c}
            </button>
          ))}
        </div>
        <div className="flex-1" />
        <span className="text-xs text-muted-foreground tabular-nums">{nodeCount} nodes</span>
        <Button variant="outline" size="sm" onClick={resetLayout}>Reset layout</Button>
      </div>

      <p className="mb-3 text-xs text-muted-foreground">
        Only explicit links are drawn — parent-child and manually defined connections with interface/port metadata.
        Physical links are never inferred from shared subnets. Drag positions persist locally.
      </p>

      <Card className="overflow-hidden p-0">
        <CardContent className="h-[600px] p-0">
          {isLoading ? (
            <div className="h-full animate-pulse bg-accent" />
          ) : isError ? (
            <EmptyState icon={WaypointsIcon} title="Topology unavailable" description="Could not load the graph." className="m-6" />
          ) : nodeCount === 0 ? (
            <EmptyState
              icon={WaypointsIcon}
              title="Nothing to draw yet"
              description="Register assets and define relationships (hosting, physical connections, dependencies) to populate the topology."
              className="m-6"
            />
          ) : (
            <ReactFlow
              nodes={nodes}
              edges={edges}
              onNodesChange={onNodesChange}
              onEdgesChange={onEdgesChange}
              onNodeDragStop={onNodeDragStop}
              nodeTypes={nodeTypes}
              edgeTypes={edgeTypes}
              fitView
              minZoom={0.2}
              proOptions={{ hideAttribution: false }}
            >
              <Background gap={24} />
              <Controls showInteractive={false} />
            </ReactFlow>
          )}
        </CardContent>
      </Card>
    </>
  )
}
