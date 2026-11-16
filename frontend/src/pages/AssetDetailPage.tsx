import { useState, type ReactNode } from "react"
import { Link, useNavigate, useParams } from "react-router-dom"
import { toast } from "sonner"
import {
  ArrowLeftIcon,
  BoxesIcon,
  MapPinIcon,
  PencilIcon,
  PlusIcon,
  ScrollTextIcon,
  TagIcon,
  Trash2Icon,
  WaypointsIcon,
} from "lucide-react"

import { isApiError } from "@/api/client"
import {
  useAsset,
  useCreateInterface,
  useCreateRelationship,
  useDeleteAsset,
  useDeleteInterface,
  useDeleteRelationship,
} from "@/api/hooks"
import type { NetworkInterface } from "@/api/types"
import { AssetFormDialog } from "@/components/AssetFormDialog"
import { AssetPicker } from "@/components/AssetPicker"
import { ConfirmDialog } from "@/components/ConfirmDialog"
import { EmptyState } from "@/components/EmptyState"
import { StatusBadge, statusLabel } from "@/components/StatusBadge"
import { Spinner } from "@/components/Spinner"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select } from "@/components/ui/select"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { formatDateTime, formatRelative } from "@/lib/utils"

const RELATIONSHIP_KINDS = [
  { value: "runs_on", label: "Runs on" },
  { value: "connected_to", label: "Connected to" },
  { value: "depends_on", label: "Depends on" },
]

export function AssetDetailPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const { data, isLoading, isError, error } = useAsset(id)
  const deleteAsset = useDeleteAsset()

  if (isLoading) {
    return <div className="flex justify-center pt-16"><Spinner className="size-6 text-muted-foreground" /></div>
  }
  if (isError || !data) {
    return (
      <EmptyState
        icon={BoxesIcon}
        title="Asset unavailable"
        description={isApiError(error) && error.status === 404 ? "This asset does not exist." : "Could not load the asset. Reload to retry."}
      />
    )
  }

  const { asset, interfaces, relationships, children, activity } = data

  return (
    <>
      <div className="mb-6">
        <button
          type="button"
          onClick={() => navigate("/assets")}
          className="mb-3 inline-flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground"
        >
          <ArrowLeftIcon className="size-4" aria-hidden /> All assets
        </button>

        <div className="flex flex-wrap items-start justify-between gap-4">
          <div>
            <div className="flex flex-wrap items-center gap-2.5">
              <h1 className="text-xl font-semibold tracking-tight">{asset.name}</h1>
              <StatusBadge status={asset.status} />
            </div>
            <div className="mt-1.5 flex flex-wrap items-center gap-x-3 gap-y-1 text-sm text-muted-foreground">
              <span>{asset.type_label}</span>
              {asset.hostname ? <span className="font-mono text-xs">{asset.hostname}</span> : null}
              <span className={environmentClass(asset.environment)}>{asset.environment}</span>
              {asset.location_name ? (
                <span className="inline-flex items-center gap-1">
                  <MapPinIcon className="size-3.5" aria-hidden />
                  <Link to={`/assets?location=${asset.location_id ?? ""}`} className="hover:underline underline-offset-2">
                    {asset.location_name}
                  </Link>
                </span>
              ) : null}
              <span className="text-xs">source: {asset.source}</span>
            </div>
          </div>

          <div className="flex items-center gap-2">
            <AssetFormDialog asset={asset} trigger={
              <Button variant="outline" size="sm"><PencilIcon className="size-4" aria-hidden />Edit</Button>
            } />
            <ConfirmDialog
              trigger={
                <Button variant="destructive" size="sm">
                  <Trash2Icon className="size-4" aria-hidden />Delete
                </Button>
              }
              title={`Delete ${asset.name}?`}
              description={
                asset.child_count > 0
                  ? `This asset hosts ${asset.child_count} child asset(s). Move or delete them first.`
                  : "This permanently removes the asset and its interfaces, and is recorded in the audit log."
              }
              confirmLabel="Delete asset"
              onConfirm={() => {
                deleteAsset.mutate(asset.id, {
                  onSuccess: () => {
                    toast.success("Asset deleted.")
                    navigate("/assets")
                  },
                  onError: (err) => toast.error(isApiError(err) ? err.message : "Delete failed."),
                })
              }}
            />
          </div>
        </div>
      </div>

      <Tabs defaultValue="overview">
        <TabsList className="mb-4 flex-wrap">
          <TabsTrigger value="overview">Overview</TabsTrigger>
          <TabsTrigger value="hardware">Hardware</TabsTrigger>
          <TabsTrigger value="network">Network</TabsTrigger>
          <TabsTrigger value="hosted">Hosted Resources{asset.child_count > 0 ? ` (${asset.child_count})` : ""}</TabsTrigger>
          <TabsTrigger value="relationships">Relationships{relationships.length > 0 ? ` (${relationships.length})` : ""}</TabsTrigger>
          <TabsTrigger value="activity">Activity</TabsTrigger>
        </TabsList>

        <TabsContent value="overview">
          <div className="grid gap-4 lg:grid-cols-2">
            <Card>
              <CardHeader><CardTitle className="text-sm">Identity</CardTitle></CardHeader>
              <CardContent>
                <InfoRow label="Name" value={asset.name} />
                <InfoRow label="Hostname" value={asset.hostname || "—"} mono />
                <InfoRow label="Type" value={asset.type_label} />
                <InfoRow label="Status" value={statusLabel(asset.status)} />
                <InfoRow label="Lifecycle" value={asset.lifecycle} />
                <InfoRow label="Environment" value={asset.environment} />
                <InfoRow
                  label="Parent host"
                  value={
                    asset.parent_id && asset.parent_name ? (
                      <Link to={`/assets/${asset.parent_id}`} className="text-primary hover:underline underline-offset-2">
                        {asset.parent_name}
                      </Link>
                    ) : "—"
                  }
                />
                <InfoRow label="Location" value={asset.location_name ?? "—"} />
              </CardContent>
            </Card>

            <Card>
              <CardHeader><CardTitle className="text-sm">Tags & notes</CardTitle></CardHeader>
              <CardContent>
                <div className="mb-4 flex flex-wrap gap-1.5">
                  {asset.tags.length === 0 ? (
                    <p className="text-sm text-muted-foreground">No tags attached.</p>
                  ) : (
                    asset.tags.map((t) => (
                      <span key={t.id} className="inline-flex items-center gap-1.5 rounded-md border bg-muted/50 px-2 py-0.5 text-xs">
                        <TagIcon className="size-3" aria-hidden />
                        {t.name}
                      </span>
                    ))
                  )}
                </div>
                <p className="whitespace-pre-wrap text-sm text-muted-foreground">
                  {asset.notes || "No notes recorded."}
                </p>
              </CardContent>
            </Card>
          </div>
        </TabsContent>

        <TabsContent value="hardware">
          <Card>
            <CardContent className="pt-2">
              <div className="grid gap-x-8 sm:grid-cols-2">
                <div>
                  <InfoRow label="Manufacturer" value={asset.manufacturer || "—"} />
                  <InfoRow label="Model" value={asset.model || "—"} />
                  <InfoRow label="Serial number" value={asset.serial_number || "—"} mono />
                  <InfoRow label="OS" value={asset.os_name ? `${asset.os_name} ${asset.os_version}`.trim() : "—"} />
                </div>
                <div>
                  <InfoRow label="CPU" value={asset.cpu_model || "—"} />
                  <InfoRow label="CPU cores" value={asset.cpu_cores != null ? String(asset.cpu_cores) : "—"} mono />
                  <InfoRow label="RAM" value={asset.ram_gb != null ? `${asset.ram_gb} GB` : "—"} mono />
                  <InfoRow label="Storage" value={asset.storage_gb != null ? `${asset.storage_gb} GB` : "—"} mono />
                </div>
              </div>
              <div className="mt-6 border-t pt-4">
                <div className="grid gap-x-8 sm:grid-cols-3">
                  <InfoRow label="Purchased" value={asset.purchase_date ?? "—"} mono />
                  <InfoRow label="Cost" value={asset.purchase_cost != null ? asset.purchase_cost.toFixed(2) : "—"} mono />
                  <InfoRow label="Warranty expires" value={asset.warranty_expires ?? "—"} mono />
                </div>
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="network">
          <InterfacesPanel assetId={asset.id} interfaces={interfaces} />
        </TabsContent>

        <TabsContent value="hosted">
          {children.length === 0 ? (
            <EmptyState
              icon={BoxesIcon}
              title="No hosted resources"
              description="Virtual machines and containers parented to this asset appear here."
            />
          ) : (
            <div className="overflow-hidden rounded-lg border">
              <Table>
                <TableHeader>
                  <TableRow className="hover:bg-transparent">
                    <TableHead>Name</TableHead>
                    <TableHead>Type</TableHead>
                    <TableHead>Primary IP</TableHead>
                    <TableHead>Status</TableHead>
                    <TableHead>Updated</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {children.map((child) => (
                    <TableRow key={child.id}>
                      <TableCell>
                        <Link to={`/assets/${child.id}`} className="font-medium hover:underline underline-offset-2">
                          {child.name}
                        </Link>
                      </TableCell>
                      <TableCell className="text-muted-foreground">{child.type_label}</TableCell>
                      <TableCell className="font-mono text-xs">{child.primary_ip ?? "—"}</TableCell>
                      <TableCell><StatusBadge status={child.status} /></TableCell>
                      <TableCell className="text-xs text-muted-foreground">{formatRelative(child.updated_at)}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          )}
        </TabsContent>

        <TabsContent value="relationships">
          <RelationshipsPanel assetId={asset.id} excludeSelfId={asset.id} relationships={relationships} />
        </TabsContent>

        <TabsContent value="activity">
          {activity.length === 0 ? (
            <EmptyState
              icon={ScrollTextIcon}
              title="No recorded changes"
              description="Edits to this asset appear here with actor, time, and before/after values."
            />
          ) : (
            <div className="overflow-hidden rounded-lg border">
              <Table>
                <TableHeader>
                  <TableRow className="hover:bg-transparent">
                    <TableHead className="w-44">When</TableHead>
                    <TableHead className="w-32">Actor</TableHead>
                    <TableHead className="w-40">Action</TableHead>
                    <TableHead>Changes</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {activity.map((event) => (
                    <TableRow key={event.id}>
                      <TableCell className="text-muted-foreground tabular-nums">{formatDateTime(event.occurred_at)}</TableCell>
                      <TableCell className="font-medium">{event.actor_label || "system"}</TableCell>
                      <TableCell>
                        <span className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs">{event.action}</span>
                      </TableCell>
                      <TableCell className="max-w-md truncate font-mono text-xs text-muted-foreground" title={JSON.stringify(event.changes)}>
                        {event.changes && Object.keys(event.changes).length > 0 ? JSON.stringify(event.changes) : "—"}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          )}
        </TabsContent>
      </Tabs>
    </>
  )
}

// ---------- network tab ----------

function InterfacesPanel({ assetId, interfaces }: { assetId: string; interfaces: NetworkInterface[] }) {
  const createInterface = useCreateInterface()
  const deleteInterface = useDeleteInterface()
  const [open, setOpen] = useState(false)
  const [name, setName] = useState("")
  const [kind, setKind] = useState("ethernet")
  const [mac, setMac] = useState("")

  const submit = () => {
    createInterface.mutate(
      { assetId, payload: { name, kind, mac_address: mac || null, mtu: null, speed_mbps: null, notes: "" } },
      {
        onSuccess: () => {
          toast.success("Interface added.")
          setOpen(false)
          setName("")
          setMac("")
        },
        onError: (err) => toast.error(isApiError(err) ? err.message : "Could not add interface."),
      },
    )
  }

  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between">
        <CardTitle className="text-sm">Network interfaces</CardTitle>
        <Button variant="outline" size="sm" onClick={() => setOpen(true)}>
          <PlusIcon className="size-4" aria-hidden />Add interface
        </Button>
      </CardHeader>
      <CardContent>
        {interfaces.length === 0 ? (
          <EmptyState
            icon={WaypointsIcon}
            title="No interfaces"
            description="Add interfaces to record MACs and prepare IP assignments (Phase 3)."
            className="border-0 bg-transparent py-6"
          />
        ) : (
          <div className="overflow-hidden rounded-lg border">
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead>Name</TableHead>
                  <TableHead>Kind</TableHead>
                  <TableHead>MAC address</TableHead>
                  <TableHead>IPs</TableHead>
                  <TableHead className="w-12" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {interfaces.map((ni) => (
                  <TableRow key={ni.id}>
                    <TableCell className="font-mono text-sm font-medium">{ni.name}</TableCell>
                    <TableCell className="text-muted-foreground">{ni.kind}</TableCell>
                    <TableCell className="font-mono text-xs">{ni.mac_address ?? "—"}</TableCell>
                    <TableCell className="text-muted-foreground tabular-nums">{ni.ip_count}</TableCell>
                    <TableCell>
                      <ConfirmDialog
                        trigger={
                          <Button variant="ghost" size="icon" className="size-7" aria-label={`Delete ${ni.name}`}>
                            <Trash2Icon className="size-3.5" aria-hidden />
                          </Button>
                        }
                        title={`Delete ${ni.name}?`}
                        description="Assigned IP rows are kept but detached (full IP management arrives in Phase 3)."
                        confirmLabel="Delete interface"
                        onConfirm={() =>
                          deleteInterface.mutate(ni.id, {
                            onError: (err) => toast.error(isApiError(err) ? err.message : "Delete failed."),
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
              <DialogTitle>Add interface</DialogTitle>
              <DialogDescription>Interfaces anchor IP allocations once IPAM lands.</DialogDescription>
            </DialogHeader>
            <div className="space-y-3">
              <div className="space-y-1.5">
                <Label htmlFor="iface-name">Name *</Label>
                <Input id="iface-name" className="font-mono" placeholder="eth0" value={name} onChange={(e) => setName(e.target.value)} autoFocus />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="iface-kind">Kind</Label>
                <Select id="iface-kind" value={kind} onChange={(e) => setKind(e.target.value)}>
                  <option value="ethernet">Ethernet</option>
                  <option value="wifi">Wi-Fi</option>
                  <option value="vlan">VLAN</option>
                  <option value="bond">Bond</option>
                  <option value="loopback">Loopback</option>
                  <option value="other">Other</option>
                </Select>
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="iface-mac">MAC address</Label>
                <Input id="iface-mac" className="font-mono" placeholder="AA:BB:CC:DD:EE:FF" value={mac} onChange={(e) => setMac(e.target.value)} />
              </div>
            </div>
            <DialogFooter>
              <Button variant="outline" size="sm" onClick={() => setOpen(false)}>Cancel</Button>
              <Button size="sm" onClick={submit} disabled={createInterface.isPending || name.trim() === ""}>
                {createInterface.isPending ? "Adding…" : "Add interface"}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </CardContent>
    </Card>
  )
}

// ---------- relationships tab ----------

function RelationshipsPanel({
  assetId, excludeSelfId, relationships,
}: {
  assetId: string
  excludeSelfId: string
  relationships: NonNullable<ReturnType<typeof useAsset>["data"]>["relationships"]
}) {
  const createRelationship = useCreateRelationship()
  const deleteRelationship = useDeleteRelationship()
  const [open, setOpen] = useState(false)
  const [kind, setKind] = useState("runs_on")
  const [target, setTarget] = useState<string | null>(null)
  const [note, setNote] = useState("")

  const submit = () => {
    if (!target) return
    createRelationship.mutate(
      { assetId, target_asset_id: target, kind, note },
      {
        onSuccess: () => {
          toast.success("Relationship created.")
          setOpen(false)
          setTarget(null)
          setNote("")
        },
        onError: (err) => toast.error(isApiError(err) ? err.message : "Could not create relationship."),
      },
    )
  }

  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between">
        <CardTitle className="text-sm">Relationships</CardTitle>
        <Button variant="outline" size="sm" onClick={() => setOpen(true)}>
          <PlusIcon className="size-4" aria-hidden />Add relationship
        </Button>
      </CardHeader>
      <CardContent>
        {relationships.length === 0 ? (
          <EmptyState
            icon={WaypointsIcon}
            title="No relationships"
            description="Link this asset to hosts, peers, or dependencies. Cycles are rejected automatically."
            className="border-0 bg-transparent py-6"
          />
        ) : (
          <div className="overflow-hidden rounded-lg border">
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead className="w-36">Kind</TableHead>
                  <TableHead className="w-24">Direction</TableHead>
                  <TableHead>Related asset</TableHead>
                  <TableHead>Note</TableHead>
                  <TableHead className="w-12" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {relationships.map((rel) => (
                  <TableRow key={rel.id}>
                    <TableCell>
                      <span className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs">{rel.kind}</span>
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">{rel.direction}</TableCell>
                    <TableCell>
                      <Link to={`/assets/${rel.other_asset.id}`} className="font-medium hover:underline underline-offset-2">
                        {rel.other_asset.name}
                      </Link>
                      <span className="ml-1.5 text-xs text-muted-foreground">{rel.other_asset.type_label}</span>
                    </TableCell>
                    <TableCell className="max-w-xs truncate text-muted-foreground">{rel.note || "—"}</TableCell>
                    <TableCell>
                      <ConfirmDialog
                        trigger={
                          <Button variant="ghost" size="icon" className="size-7" aria-label="Delete relationship">
                            <Trash2Icon className="size-3.5" aria-hidden />
                          </Button>
                        }
                        title="Remove relationship?"
                        description="The two assets remain; only the link is removed."
                        confirmLabel="Remove"
                        onConfirm={() =>
                          deleteRelationship.mutate(rel.id, {
                            onError: (err) => toast.error(isApiError(err) ? err.message : "Delete failed."),
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
              <DialogTitle>Add relationship</DialogTitle>
              <DialogDescription>
                <code>runs_on</code> and <code>depends_on</code> edges may not form cycles.
              </DialogDescription>
            </DialogHeader>
            <div className="space-y-3">
              <div className="space-y-1.5">
                <Label htmlFor="rel-kind">Kind</Label>
                <Select id="rel-kind" value={kind} onChange={(e) => setKind(e.target.value)}>
                  {RELATIONSHIP_KINDS.map((k) => (
                    <option key={k.value} value={k.value}>{k.label}</option>
                  ))}
                </Select>
              </div>
              <div className="space-y-1.5">
                <Label>Target asset *</Label>
                <AssetPicker value={target} onChange={(a) => setTarget(a?.id ?? null)} excludeId={excludeSelfId} />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="rel-note">Note</Label>
                <Input id="rel-note" value={note} onChange={(e) => setNote(e.target.value)} />
              </div>
            </div>
            <DialogFooter>
              <Button variant="outline" size="sm" onClick={() => setOpen(false)}>Cancel</Button>
              <Button size="sm" onClick={submit} disabled={createRelationship.isPending || !target}>
                {createRelationship.isPending ? "Creating…" : "Create relationship"}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </CardContent>
    </Card>
  )
}

function InfoRow({ label, value, mono }: { label: string; value: ReactNode; mono?: boolean }) {
  return (
    <div className="flex items-baseline justify-between gap-4 border-b py-1.5 last:border-0">
      <span className="shrink-0 text-xs text-muted-foreground">{label}</span>
      <span className={`text-right text-sm ${mono ? "font-mono text-xs" : ""}`}>{value}</span>
    </div>
  )
}

function environmentClass(environment: string): string {
  const map: Record<string, string> = {
    production: "text-emerald-600 dark:text-emerald-400",
    development: "text-sky-600 dark:text-sky-400",
    testing: "text-amber-600 dark:text-amber-400",
    lab: "text-violet-600 dark:text-violet-400",
  }
  return map[environment] ?? ""
}
