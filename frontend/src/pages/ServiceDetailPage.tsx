import { useState } from "react"
import { Link, useNavigate, useParams } from "react-router-dom"
import { toast } from "sonner"
import {
  ActivityIcon,
  ArrowLeftIcon,
  ExternalLinkIcon,
  GlobeIcon,
  PencilIcon,
  PlusIcon,
  ScrollTextIcon,
  Trash2Icon,
} from "lucide-react"

import { isApiError } from "@/api/client"
import {
  useToggleEndpointHealth,
  useCreateServiceDeployment,
  useCreateServiceEndpoint,
  useDeleteService,
  useDeleteServiceDeployment,
  useDeleteServiceEndpoint,
  useService,
  useUpdateService,
} from "@/api/hooks"
import type { ServiceEndpoint } from "@/api/types"
import { formatRelative } from "@/lib/utils"

import { AssetPicker } from "@/components/AssetPicker"
import { ConfirmDialog } from "@/components/ConfirmDialog"
import { EmptyState } from "@/components/EmptyState"
import { StatusBadge } from "@/components/StatusBadge"
import { Spinner } from "@/components/Spinner"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select } from "@/components/ui/select"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { formatDateTime } from "@/lib/utils"
import { ServiceFormFields, servicePayload, type ServiceFormState } from "./ServicesPage"

export function ServiceDetailPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const { data, isLoading, isError } = useService(id)
  const deleteService = useDeleteService()
  const updateService = useUpdateService()

  const [editOpen, setEditOpen] = useState(false)
  const [form, setFormState] = useState<ServiceFormState | null>(null)
  const setForm = (patch: Partial<ServiceFormState>) =>
    setFormState((prev) => (prev ? { ...prev, ...patch } : prev))

  if (isLoading) {
    return <div className="flex justify-center pt-16"><Spinner className="size-6 text-muted-foreground" /></div>
  }
  if (isError || !data) {
    return <EmptyState icon={GlobeIcon} title="Service unavailable" description="Could not load this service." />
  }

  const { service, endpoints, deployments, activity } = data

  const openEdit = () => {
    setFormState({
      name: service.name, description: service.description, category: service.category,
      deployment_type: service.deployment_type, environment: service.environment,
      status: service.status, repo_url: service.repo_url, docs_url: service.docs_url,
      notes: service.notes,
    })
    setEditOpen(true)
  }

  return (
    <>
      <button
        type="button"
        onClick={() => navigate("/services")}
        className="mb-3 inline-flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground"
      >
        <ArrowLeftIcon className="size-4" aria-hidden /> All services
      </button>

      <div className="mb-6 flex flex-wrap items-start justify-between gap-4">
        <div>
          <div className="flex flex-wrap items-center gap-2.5">
            <h1 className="text-xl font-semibold tracking-tight">{service.name}</h1>
            <StatusBadge status={service.status} />
          </div>
          <div className="mt-1.5 flex flex-wrap items-center gap-x-3 gap-y-1 text-sm text-muted-foreground">
            <span>{service.category}</span>
            <span>{service.deployment_type}</span>
            <span>{service.environment}</span>
            {service.repo_url ? (
              <a href={service.repo_url} target="_blank" rel="noreferrer"
                className="inline-flex items-center gap-1 font-mono text-xs hover:text-foreground">
                repository <ExternalLinkIcon className="size-3" aria-hidden />
              </a>
            ) : null}
            {service.docs_url ? (
              <a href={service.docs_url} target="_blank" rel="noreferrer"
                className="inline-flex items-center gap-1 font-mono text-xs hover:text-foreground">
                docs <ExternalLinkIcon className="size-3" aria-hidden />
              </a>
            ) : null}
          </div>
          {service.description ? (
            <p className="mt-2 max-w-2xl text-sm text-muted-foreground">{service.description}</p>
          ) : null}
        </div>
        <div className="flex items-center gap-2">
          <Button variant="outline" size="sm" onClick={openEdit}>
            <PencilIcon className="size-4" aria-hidden />Edit
          </Button>
          <ConfirmDialog
            trigger={
              <Button variant="destructive" size="sm">
                <Trash2Icon className="size-4" aria-hidden />Delete
              </Button>
            }
            title={`Delete ${service.name}?`}
            description="Endpoints and deployment mappings are removed with the service."
            confirmLabel="Delete service"
            onConfirm={() =>
              deleteService.mutate(service.id, {
                onSuccess: () => navigate("/services"),
                onError: (err) => toast.error(isApiError(err) ? err.message : "Delete failed."),
              })
            }
          />
        </div>
      </div>

      <div className="grid gap-4 lg:grid-cols-2">
        <EndpointsCard serviceId={service.id} endpoints={endpoints} />
        <DeploymentsCard serviceId={service.id} deployments={deployments} />
      </div>

      <Card className="mt-4">
        <CardHeader className="flex-row items-center justify-between">
          <CardTitle className="text-sm">Activity</CardTitle>
          <ScrollTextIcon className="size-4 text-muted-foreground" aria-hidden />
        </CardHeader>
        <CardContent>
          {activity.length === 0 ? (
            <p className="py-4 text-center text-sm text-muted-foreground">No recorded changes.</p>
          ) : (
            <ul className="space-y-1.5">
              {activity.map((event) => (
                <li key={event.id} className="flex items-center gap-3 text-sm">
                  <span className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs">{event.action}</span>
                  <span className="text-muted-foreground">{event.actor_label || "system"}</span>
                  <span className="flex-1" />
                  <span className="text-xs text-muted-foreground tabular-nums">{formatDateTime(event.occurred_at)}</span>
                </li>
              ))}
            </ul>
          )}
        </CardContent>
      </Card>

      {form ? (
        <Dialog open={editOpen} onOpenChange={setEditOpen}>
          <DialogContent className="max-w-xl">
            <DialogHeader>
              <DialogTitle>Edit {service.name}</DialogTitle>
              <DialogDescription>Changes are recorded in the activity log.</DialogDescription>
            </DialogHeader>
            <ServiceFormFields form={form} setForm={setForm} />
            <DialogFooter>
              <Button variant="outline" size="sm" onClick={() => setEditOpen(false)}>Cancel</Button>
              <Button
                size="sm"
                disabled={updateService.isPending}
                onClick={() =>
                  updateService.mutate(
                    { id: service.id, payload: servicePayload(form) },
                    {
                      onSuccess: () => {
                        toast.success("Service updated.")
                        setEditOpen(false)
                      },
                      onError: (err) => toast.error(isApiError(err) ? err.message : "Update failed."),
                    },
                  )
                }
              >
                {updateService.isPending ? "Saving…" : "Save changes"}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      ) : null}
    </>
  )
}

function endpointHealth(ep: ServiceEndpoint): "up" | "down" | "unknown" | null {
  if (!ep.health_enabled) return null
  if (ep.last_checked_at == null) return "unknown"
  if (ep.last_error || ep.last_status == null || ep.last_status >= 400) return "down"
  return "up"
}

const HEALTH_DOT: Record<"up" | "down" | "unknown", string> = {
  up: "bg-emerald-500",
  down: "bg-red-500",
  unknown: "bg-zinc-400",
}

function EndpointsCard({
  serviceId, endpoints,
}: {
  serviceId: string
  endpoints: ServiceEndpoint[]
}) {
  const create = useCreateServiceEndpoint()
  const remove = useDeleteServiceEndpoint()
  const toggleHealth = useToggleEndpointHealth()
  const [open, setOpen] = useState(false)
  const [label, setLabel] = useState("")
  const [protocol, setProtocol] = useState("http")
  const [port, setPort] = useState("")
  const [url, setUrl] = useState("")
  const [isExternal, setIsExternal] = useState(false)
  const [healthEnabled, setHealthEnabled] = useState(false)

  const submit = () => {
    const portNum = port.trim() === "" ? null : Number(port)
    create.mutate(
      { serviceId, payload: { label: label.trim(), protocol, port: portNum, url: url.trim(), is_external: isExternal, health_enabled: healthEnabled } },
      {
        onSuccess: () => {
          toast.success(healthEnabled ? "Endpoint added — health checks start within a minute." : "Endpoint added.")
          setOpen(false)
          setLabel("")
          setPort("")
          setUrl("")
          setHealthEnabled(false)
        },
        onError: (err) => toast.error(isApiError(err) ? err.message : "Could not add endpoint."),
      },
    )
  }

  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between">
        <CardTitle className="text-sm">Endpoints</CardTitle>
        <Button variant="outline" size="sm" onClick={() => setOpen(true)}>
          <PlusIcon className="size-4" aria-hidden />Add endpoint
        </Button>
      </CardHeader>
      <CardContent>
        {endpoints.length === 0 ? (
          <p className="py-6 text-center text-sm text-muted-foreground">
            No endpoints — add the URL you use to reach this service.
          </p>
        ) : (
          <ul className="space-y-2">
            {endpoints.map((e) => (
              <li key={e.id} className="flex items-center gap-3 rounded-md border px-3 py-2">
                <span className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs uppercase">{e.protocol}</span>
                {e.url ? (
                  <a href={e.url} target="_blank" rel="noreferrer"
                    className="min-w-0 flex-1 truncate font-mono text-xs hover:underline underline-offset-2">
                    {e.url}
                  </a>
                ) : (
                  <span className="min-w-0 flex-1 truncate font-mono text-xs">
                    {e.label || "—"}{e.port != null ? ` :${e.port}` : ""}
                  </span>
                )}
                {e.is_external ? <span className="text-xs text-muted-foreground">external</span> : null}
                {(() => {
                  const state = endpointHealth(e)
                  if (!state) return null
                  const tip = state === "unknown"
                    ? "Health check enabled — first result pending"
                    : state === "up"
                      ? `Up · HTTP ${e.last_status} · ${e.last_latency_ms} ms · ${formatRelative(e.last_checked_at!)}`
                      : `Down · ${e.last_error || `HTTP ${e.last_status}`} · ${formatRelative(e.last_checked_at!)}`
                  return (
                    <span
                      title={tip}
                      className={`size-2 shrink-0 rounded-full ${HEALTH_DOT[state]}`}
                      aria-label={`Health: ${state}`}
                    />
                  )
                })()}
                {e.protocol === "http" || e.protocol === "https" ? (
                  <Button
                    variant="ghost"
                    size="icon"
                    className="size-7"
                    title={e.health_enabled ? "Disable health checks" : "Enable health checks"}
                    aria-label={e.health_enabled ? `Disable health checks for ${e.label || e.url}` : `Enable health checks for ${e.label || e.url}`}
                    disabled={toggleHealth.isPending}
                    onClick={() =>
                      toggleHealth.mutate(
                        { id: e.id, enabled: !e.health_enabled },
                        {
                          onSuccess: () => toast.success(e.health_enabled ? "Health checks disabled." : "Health checks enabled — first check runs within a minute."),
                          onError: (err) => toast.error(isApiError(err) ? err.message : "Update failed."),
                        },
                      )
                    }
                  >
                    <ActivityIcon className="size-3.5" aria-hidden />
                  </Button>
                ) : null}
                <ConfirmDialog
                  trigger={
                    <Button variant="ghost" size="icon" className="size-7" aria-label="Remove endpoint">
                      <Trash2Icon className="size-3.5" aria-hidden />
                    </Button>
                  }
                  title="Remove endpoint?"
                  description="The service itself is unchanged."
                  confirmLabel="Remove"
                  onConfirm={() =>
                    remove.mutate(e.id, {
                      onError: (err) => toast.error(isApiError(err) ? err.message : "Remove failed."),
                    })
                  }
                />
              </li>
            ))}
          </ul>
        )}

        <Dialog open={open} onOpenChange={setOpen}>
          <DialogContent className="max-w-sm">
            <DialogHeader>
              <DialogTitle>Add endpoint</DialogTitle>
              <DialogDescription>Quick links to the service web interface.</DialogDescription>
            </DialogHeader>
            <div className="space-y-3">
              <div className="space-y-1.5">
                <Label htmlFor="ep-label">Label</Label>
                <Input id="ep-label" placeholder="Web UI" value={label} onChange={(e) => setLabel(e.target.value)} autoFocus />
              </div>
              <div className="grid grid-cols-2 gap-3">
                <div className="space-y-1.5">
                  <Label htmlFor="ep-protocol">Protocol</Label>
                  <Select id="ep-protocol" value={protocol} onChange={(e) => setProtocol(e.target.value)}>
                    <option value="http">HTTP</option>
                    <option value="https">HTTPS</option>
                    <option value="tcp">TCP</option>
                    <option value="udp">UDP</option>
                  </Select>
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="ep-port">Port</Label>
                  <Input id="ep-port" type="number" min={1} max={65535} value={port} onChange={(e) => setPort(e.target.value)} />
                </div>
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="ep-url">URL</Label>
                <Input id="ep-url" className="font-mono text-xs" placeholder="http://jellyfin.lab:8096"
                  value={url} onChange={(e) => setUrl(e.target.value)} />
              </div>
              <label className="flex items-center gap-2 text-sm">
                <input type="checkbox" checked={isExternal} onChange={(e) => setIsExternal(e.target.checked)} />
                Externally reachable (published through a reverse proxy)
              </label>
              {(protocol === "http" || protocol === "https") && url.trim() !== "" ? (
                <label className="flex items-center gap-2 text-sm">
                  <input type="checkbox" checked={healthEnabled} onChange={(e) => setHealthEnabled(e.target.checked)} />
                  Health checks — probe this URL every minute and alert on downtime
                </label>
              ) : null}
            </div>
            <DialogFooter>
              <Button variant="outline" size="sm" onClick={() => setOpen(false)}>Cancel</Button>
              <Button size="sm" onClick={submit} disabled={create.isPending}>
                {create.isPending ? "Adding…" : "Add endpoint"}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </CardContent>
    </Card>
  )
}

function DeploymentsCard({
  serviceId, deployments,
}: {
  serviceId: string
  deployments: { id: string; asset_id: string; asset_name: string; asset_hostname: string; deployment_type: string; notes: string }[]
}) {
  const create = useCreateServiceDeployment()
  const remove = useDeleteServiceDeployment()
  const [open, setOpen] = useState(false)
  const [assetId, setAssetId] = useState<string | null>(null)
  const [deployType, setDeployType] = useState("native")
  const [notes, setNotes] = useState("")

  const submit = () => {
    if (!assetId) return
    create.mutate(
      { serviceId, payload: { asset_id: assetId, deployment_type: deployType, notes: notes.trim() } },
      {
        onSuccess: () => {
          toast.success("Deployment mapped.")
          setOpen(false)
          setAssetId(null)
          setNotes("")
        },
        onError: (err) => toast.error(isApiError(err) ? err.message : "Could not add deployment."),
      },
    )
  }

  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between">
        <CardTitle className="text-sm">Runs on</CardTitle>
        <Button variant="outline" size="sm" onClick={() => setOpen(true)}>
          <PlusIcon className="size-4" aria-hidden />Map to host
        </Button>
      </CardHeader>
      <CardContent>
        {deployments.length === 0 ? (
          <p className="py-6 text-center text-sm text-muted-foreground">
            Not mapped to any host yet.
          </p>
        ) : (
          <div className="overflow-hidden rounded-lg border">
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead>Host</TableHead>
                  <TableHead>Type</TableHead>
                  <TableHead className="w-10" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {deployments.map((d) => (
                  <TableRow key={d.id}>
                    <TableCell>
                      <Link to={`/assets/${d.asset_id}`} className="font-medium hover:underline underline-offset-2">
                        {d.asset_name}
                      </Link>
                      {d.asset_hostname ? (
                        <p className="font-mono text-xs text-muted-foreground">{d.asset_hostname}</p>
                      ) : null}
                    </TableCell>
                    <TableCell className="text-muted-foreground">{d.deployment_type}</TableCell>
                    <TableCell>
                      <ConfirmDialog
                        trigger={
                          <Button variant="ghost" size="icon" className="size-7" aria-label={`Unmap ${d.asset_name}`}>
                            <Trash2Icon className="size-3.5" aria-hidden />
                          </Button>
                        }
                        title={`Unmap from ${d.asset_name}?`}
                        description="The host asset remains; only the service mapping is removed."
                        confirmLabel="Unmap"
                        onConfirm={() =>
                          remove.mutate(d.id, {
                            onError: (err) => toast.error(isApiError(err) ? err.message : "Remove failed."),
                          })
                        }
                      />
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}

        <Dialog open={open} onOpenChange={setOpen}>
          <DialogContent className="max-w-sm">
            <DialogHeader>
              <DialogTitle>Map to host</DialogTitle>
              <DialogDescription>One service may run on multiple assets.</DialogDescription>
            </DialogHeader>
            <div className="space-y-3">
              <div className="space-y-1.5">
                <Label>Host asset *</Label>
                <AssetPicker value={assetId} onChange={(a) => setAssetId(a?.id ?? null)} />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="dep-type">Deployment type</Label>
                <Select id="dep-type" value={deployType} onChange={(e) => setDeployType(e.target.value)}>
                  <option value="native">Native</option>
                  <option value="docker">Docker</option>
                  <option value="vm">VM</option>
                  <option value="lxc">LXC</option>
                  <option value="kubernetes">Kubernetes</option>
                </Select>
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="dep-notes">Notes</Label>
                <Input id="dep-notes" value={notes} onChange={(e) => setNotes(e.target.value)} />
              </div>
            </div>
            <DialogFooter>
              <Button variant="outline" size="sm" onClick={() => setOpen(false)}>Cancel</Button>
              <Button size="sm" onClick={submit} disabled={create.isPending || !assetId}>
                {create.isPending ? "Mapping…" : "Map service"}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </CardContent>
    </Card>
  )
}
