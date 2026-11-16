import { useMemo } from "react"
import { Link } from "react-router-dom"
import { ShipWheelIcon } from "lucide-react"

import { useAssets, useServices } from "@/api/hooks"
import type { Asset } from "@/api/types"
import { EmptyState } from "@/components/EmptyState"
import { PageHeader } from "@/components/PageHeader"
import { StatusBadge } from "@/components/StatusBadge"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"

// The assets API caps limit at 100; pods can exceed that on busy clusters, so the
// section notes truncation and links to the full inventory view.
const POD_LIMIT = 100

function podName(pod: Asset): string {
  // Synced pods are named "namespace/name"; show the bare name.
  return pod.name.split("/")[1] ?? pod.name
}

function podPhase(pod: Asset): string {
  return (pod.metadata?.kubernetes?.phase ?? pod.metadata?.observed?.status ?? "").toLowerCase()
}

function nodeStatus(node: Asset): string {
  // Discovered assets keep status "unknown"; the connector's observed state is live.
  return node.metadata?.observed?.status ?? node.status
}

export function KubernetesPage() {
  const { data: nodeData, isLoading: nodesLoading } = useAssets({
    type_slugs: "k8s-node", limit: 100, sort: "name", order: "asc",
  })
  const { data: podData, isLoading: podsLoading } = useAssets({
    type_slugs: "pod", limit: POD_LIMIT, sort: "name", order: "asc",
  })
  const { data: svcData } = useServices({ deployment_type: "kubernetes" })

  const nodes = nodeData?.assets ?? []
  const pods = podData?.assets ?? []
  const services = svcData?.services ?? []

  const readyNodes = nodes.filter((n) => nodeStatus(n) === "online").length
  const runningPods = pods.filter((p) => podPhase(p) === "running").length

  const podsByNamespace = useMemo(() => {
    const groups = new Map<string, Asset[]>()
    for (const p of pods) {
      const ns = p.metadata?.kubernetes?.namespace ?? "default"
      if (!groups.has(ns)) groups.set(ns, [])
      groups.get(ns)!.push(p)
    }
    return [...groups.entries()].sort(([a], [b]) => a.localeCompare(b))
  }, [pods])

  const empty = !nodesLoading && !podsLoading && nodes.length === 0 && pods.length === 0 && services.length === 0

  return (
    <>
      <PageHeader
        title="Kubernetes"
        description="Nodes, pods and services across your clusters — synced by the Kubernetes connector."
      />

      {/* Cluster stats */}
      <div className="mb-6 grid gap-4 sm:grid-cols-3">
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm text-muted-foreground">Nodes</CardTitle>
          </CardHeader>
          <CardContent>
            {nodesLoading ? <Skeleton className="h-7 w-10" /> : (
              <p className="text-2xl font-semibold tabular-nums">{nodes.length}</p>
            )}
            <p className="text-xs text-muted-foreground">{readyNodes} ready</p>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm text-muted-foreground">Pods</CardTitle>
          </CardHeader>
          <CardContent>
            {podsLoading ? <Skeleton className="h-7 w-10" /> : (
              <p className="text-2xl font-semibold tabular-nums">{podData?.total ?? 0}</p>
            )}
            <p className="text-xs text-muted-foreground">{runningPods} running{pods.length > 0 ? " (shown)" : ""}</p>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm text-muted-foreground">Services</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-2xl font-semibold tabular-nums">{services.length}</p>
            <p className="text-xs text-muted-foreground">in the service registry</p>
          </CardContent>
        </Card>
      </div>

      {empty ? (
        <EmptyState
          icon={ShipWheelIcon}
          title="No Kubernetes resources"
          description="Set up the Kubernetes integration to sync nodes, pods and services from your cluster."
        />
      ) : (
        <div className="space-y-6">
          {/* Nodes */}
          <Card>
            <CardHeader className="pb-3">
              <CardTitle className="text-sm">Nodes</CardTitle>
            </CardHeader>
            <CardContent className="p-0">
              {nodesLoading ? (
                <div className="space-y-2 p-4">{Array.from({ length: 2 }).map((_, i) => (
                  <Skeleton key={i} className="h-9 w-full" />
                ))}</div>
              ) : nodes.length === 0 ? (
                <p className="px-4 py-3 text-sm text-muted-foreground">No nodes synced yet.</p>
              ) : (
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b bg-muted/40 text-left">
                      <th className="px-4 py-2.5 text-xs font-medium uppercase text-muted-foreground">Name</th>
                      <th className="px-4 py-2.5 text-xs font-medium uppercase text-muted-foreground">Roles</th>
                      <th className="px-4 py-2.5 text-xs font-medium uppercase text-muted-foreground">CPU</th>
                      <th className="px-4 py-2.5 text-xs font-medium uppercase text-muted-foreground">RAM</th>
                      <th className="px-4 py-2.5 text-xs font-medium uppercase text-muted-foreground">Kubelet</th>
                      <th className="px-4 py-2.5 text-xs font-medium uppercase text-muted-foreground">Runtime</th>
                      <th className="px-4 py-2.5 text-xs font-medium uppercase text-muted-foreground">Status</th>
                    </tr>
                  </thead>
                  <tbody>
                    {nodes.map((n) => (
                      <tr key={n.id} className="border-b last:border-0 hover:bg-muted/40">
                        <td className="px-4 py-2.5">
                          <Link to={`/assets/${n.id}`} className="font-medium hover:underline underline-offset-2">
                            {n.name}
                          </Link>
                        </td>
                        <td className="px-4 py-2.5 text-muted-foreground">
                          {n.metadata?.kubernetes?.roles?.join(", ") || "—"}
                        </td>
                        <td className="px-4 py-2.5 tabular-nums text-muted-foreground">{n.cpu_cores ?? "—"}</td>
                        <td className="px-4 py-2.5 tabular-nums text-muted-foreground">
                          {n.ram_gb != null ? `${Math.round(n.ram_gb)} GB` : "—"}
                        </td>
                        <td className="px-4 py-2.5 font-mono text-xs text-muted-foreground">{n.os_version || "—"}</td>
                        <td className="px-4 py-2.5 font-mono text-xs text-muted-foreground">
                          {n.metadata?.kubernetes?.runtime || "—"}
                        </td>
                        <td className="px-4 py-2.5"><StatusBadge status={nodeStatus(n)} /></td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
            </CardContent>
          </Card>

          {/* Pods, grouped by namespace */}
          <Card>
            <CardHeader className="pb-3">
              <CardTitle className="text-sm">Pods</CardTitle>
            </CardHeader>
            <CardContent className="p-0">
              {podsLoading ? (
                <div className="space-y-2 p-4">{Array.from({ length: 4 }).map((_, i) => (
                  <Skeleton key={i} className="h-9 w-full" />
                ))}</div>
              ) : pods.length === 0 ? (
                <p className="px-4 py-3 text-sm text-muted-foreground">No pods synced yet.</p>
              ) : (
                <>
                  {(podData?.total ?? 0) > POD_LIMIT && (
                    <p className="border-b bg-muted/40 px-4 py-2 text-xs text-muted-foreground">
                      Showing the first {POD_LIMIT} of {podData?.total} pods —{" "}
                      <Link to="/assets?type_slugs=pod" className="underline underline-offset-2">view all in inventory</Link>
                    </p>
                  )}
                  <div>
                    {podsByNamespace.map(([ns, nsPods]) => (
                      <div key={ns}>
                        <div className="flex items-baseline gap-2 border-b bg-muted/20 px-4 py-2">
                          <span className="font-mono text-xs font-medium">{ns}</span>
                          <span className="text-xs text-muted-foreground">{nsPods.length} pod{nsPods.length > 1 ? "s" : ""}</span>
                        </div>
                        <table className="w-full text-sm">
                          <tbody>
                            {nsPods.map((p) => {
                              const images = p.metadata?.kubernetes?.images ?? []
                              return (
                                <tr key={p.id} className="border-b last:border-0 hover:bg-muted/40">
                                  <td className="w-1/3 px-4 py-2">
                                    <Link to={`/assets/${p.id}`} className="font-medium hover:underline underline-offset-2">
                                      {podName(p)}
                                    </Link>
                                  </td>
                                  <td className="w-1/5 px-4 py-2 text-muted-foreground">{p.parent_name || "—"}</td>
                                  <td className="w-28 px-4 py-2"><StatusBadge status={podPhase(p) || "unknown"} /></td>
                                  <td className="px-4 py-2 font-mono text-xs text-muted-foreground">
                                    {images.length === 0 ? "—" : (
                                      <span title={images.join("\n")}>
                                        {images[0]}{images.length > 1 ? ` +${images.length - 1}` : ""}
                                      </span>
                                    )}
                                  </td>
                                </tr>
                              )
                            })}
                          </tbody>
                        </table>
                      </div>
                    ))}
                  </div>
                </>
              )}
            </CardContent>
          </Card>

          {/* K8s services from the registry */}
          <Card>
            <CardHeader className="pb-3">
              <CardTitle className="text-sm">Services</CardTitle>
            </CardHeader>
            <CardContent className="p-0">
              {services.length === 0 ? (
                <p className="px-4 py-3 text-sm text-muted-foreground">
                  No Kubernetes services in the registry yet — they are imported automatically on sync.
                </p>
              ) : (
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b bg-muted/40 text-left">
                      <th className="px-4 py-2.5 text-xs font-medium uppercase text-muted-foreground">Name</th>
                      <th className="px-4 py-2.5 text-xs font-medium uppercase text-muted-foreground">Category</th>
                      <th className="px-4 py-2.5 text-xs font-medium uppercase text-muted-foreground">Environment</th>
                      <th className="px-4 py-2.5 text-xs font-medium uppercase text-muted-foreground">Status</th>
                      <th className="px-4 py-2.5 text-xs font-medium uppercase text-muted-foreground">Hosts</th>
                    </tr>
                  </thead>
                  <tbody>
                    {services.map((svc) => (
                      <tr key={svc.id} className="border-b last:border-0 hover:bg-muted/40">
                        <td className="px-4 py-2.5">
                          <Link to={`/services/${svc.id}`} className="font-medium hover:underline underline-offset-2">
                            {svc.name}
                          </Link>
                        </td>
                        <td className="px-4 py-2.5 text-muted-foreground">{svc.category}</td>
                        <td className="px-4 py-2.5 text-muted-foreground">{svc.environment}</td>
                        <td className="px-4 py-2.5"><StatusBadge status={svc.status} /></td>
                        <td className="px-4 py-2.5 tabular-nums text-muted-foreground">{svc.host_count}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
            </CardContent>
          </Card>
        </div>
      )}
    </>
  )
}
