import { useState } from "react"
import { Link, useNavigate, useParams } from "react-router-dom"
import { toast } from "sonner"
import {
  ArrowLeftIcon,
  MapPinIcon,
  PencilIcon,
  ShieldAlertIcon,
  Trash2Icon,
} from "lucide-react"

import { isApiError } from "@/api/client"
import {
  useAddressGrid,
  useAllocateIP,
  useDeleteIP,
  useNextAvailable,
  useReleaseIP,
  useSubnet,
  useUpdateIP,
} from "@/api/hooks"
import type { IPAddress, Subnet } from "@/api/types"
import { ConfirmDialog } from "@/components/ConfirmDialog"
import { EmptyState } from "@/components/EmptyState"
import { StatusBadge } from "@/components/StatusBadge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select } from "@/components/ui/select"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { cn } from "@/lib/utils"
import { SubnetFormFields, subnetPayload, type SubnetFormState } from "./SubnetsPage"
import { UtilizationBar } from "./SubnetsPage"

const CELL_COLORS: Record<string, string> = {
  free: "bg-emerald-500/15 text-emerald-700 dark:text-emerald-400 hover:bg-emerald-500/30",
  assigned: "bg-blue-500/80 text-white hover:bg-blue-600",
  reserved: "bg-violet-500/80 text-white hover:bg-violet-600",
  excluded: "bg-zinc-400/60 text-zinc-700 dark:text-zinc-300 hover:bg-zinc-400/80",
  conflict: "bg-red-500/80 text-white hover:bg-red-600",
}

export function SubnetDetailPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const { data, isLoading, isError } = useSubnet(id)
  const { data: grid } = useAddressGrid(id)
  const { data: next } = useNextAvailable(id, 5)
  const [selectedCell, setSelectedCell] = useState<string | null>(null)

  if (isError || (data === undefined && !isLoading)) {
    return <EmptyState icon={ShieldAlertIcon} title="Subnet unavailable" description="Could not load this subnet." />
  }
  if (isLoading || !data) {
    return <div className="h-64 animate-pulse rounded-lg bg-accent" />
  }

  const subnet = data.subnet
  const allocatedCount = subnet.assigned + subnet.reserved + subnet.excluded + subnet.conflicts

  return (
    <>
      <button
        type="button"
        onClick={() => navigate("/subnets")}
        className="mb-3 inline-flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground"
      >
        <ArrowLeftIcon className="size-4" aria-hidden /> All subnets
      </button>

      <div className="mb-6 flex flex-wrap items-start justify-between gap-4">
        <div>
          <div className="flex flex-wrap items-center gap-2.5">
            <h1 className="font-mono text-xl font-semibold tracking-tight">{subnet.cidr}</h1>
            <span className="text-lg text-muted-foreground">{subnet.name}</span>
          </div>
          <div className="mt-1.5 flex flex-wrap items-center gap-x-3 gap-y-1 text-sm text-muted-foreground">
            <span>{subnet.context_name}</span>
            {subnet.vlan_number != null ? <span>VLAN {subnet.vlan_number}{subnet.vlan_name ? ` · ${subnet.vlan_name}` : ""}</span> : null}
            {subnet.location_name ? (
              <span className="inline-flex items-center gap-1">
                <MapPinIcon className="size-3.5" aria-hidden />{subnet.location_name}
              </span>
            ) : null}
            <span className="font-mono text-xs">gateway {subnet.gateway ?? "—"}</span>
            {subnet.dns_servers.length > 0 ? (
              <span className="font-mono text-xs">dns {subnet.dns_servers.join(", ")}</span>
            ) : null}
            {subnet.dhcp_start ? (
              <span className="font-mono text-xs">dhcp {subnet.dhcp_start}–{subnet.dhcp_end}</span>
            ) : null}
          </div>
        </div>
        <SubnetEditDialog subnet={subnet} />
      </div>

      {/* Utilization summary */}
      <div className="mb-6 grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <Card className="gap-3 py-4">
          <CardHeader className="px-4"><CardTitle className="text-xs text-muted-foreground uppercase">Utilization</CardTitle></CardHeader>
          <CardContent className="px-4">
            <UtilizationBar value={subnet.utilization} />
            <p className="mt-2 text-xs text-muted-foreground tabular-nums">
              {allocatedCount} of {subnet.usable_count} usable addresses
            </p>
          </CardContent>
        </Card>
        <Card className="gap-3 py-4">
          <CardHeader className="px-4"><CardTitle className="text-xs text-muted-foreground uppercase">Allocation states</CardTitle></CardHeader>
          <CardContent className="px-4">
            <div className="space-y-1 text-sm tabular-nums">
              <p><span className="inline-block size-2 rounded-sm bg-blue-500" aria-hidden /> Assigned: {subnet.assigned}</p>
              <p><span className="inline-block size-2 rounded-sm bg-violet-500" aria-hidden /> Reserved: {subnet.reserved}</p>
              <p><span className="inline-block size-2 rounded-sm bg-zinc-400" aria-hidden /> Excluded: {subnet.excluded}</p>
              <p><span className="inline-block size-2 rounded-sm bg-red-500" aria-hidden /> Conflicts: {subnet.conflicts}</p>
            </div>
          </CardContent>
        </Card>
        <Card className="gap-3 py-4 sm:col-span-2">
          <CardHeader className="px-4"><CardTitle className="text-xs text-muted-foreground uppercase">Next available</CardTitle></CardHeader>
          <CardContent className="px-4">
            <div className="flex flex-wrap gap-1.5">
              {(next?.addresses ?? []).length === 0 ? (
                <p className="text-sm text-muted-foreground">Subnet exhausted.</p>
              ) : (
                next?.addresses.map((a) => (
                  <button
                    key={a}
                    type="button"
                    className="rounded border bg-muted/50 px-2 py-1 font-mono text-xs hover:bg-accent"
                    onClick={() => setSelectedCell(a)}
                  >
                    {a}
                  </button>
                ))
              )}
            </div>
          </CardContent>
        </Card>
      </div>

      {/* Address grid */}
      <Card className="mb-6">
        <CardHeader className="flex-row items-center justify-between">
          <CardTitle className="text-sm">Address allocation grid</CardTitle>
          {grid?.truncated ? (
            <span className="text-xs text-muted-foreground">Too large for the grid — use the table below.</span>
          ) : (
            <p className="text-xs text-muted-foreground tabular-nums">
              {grid?.usable} usable · {grid?.first} – {grid?.last} · click an address to manage it
            </p>
          )}
        </CardHeader>
        <CardContent>
          {grid && !grid.truncated ? (
            <div className="grid max-w-3xl grid-cols-[repeat(auto-fill,minmax(5.5rem,1fr))] gap-1">
              {grid.cells.map((cell) => (
                <button
                  key={cell.address}
                  type="button"
                  onClick={() => setSelectedCell(cell.address)}
                  title={cell.hostname ? `${cell.address} · ${cell.hostname}` : cell.address}
                  className={cn(
                    "rounded px-1 py-1.5 text-center font-mono text-[10px] leading-tight transition-colors",
                    CELL_COLORS[cell.status] ?? CELL_COLORS.free,
                  )}
                >
                  {hostPart(cell.address)}
                </button>
              ))}
            </div>
          ) : (
            <p className="py-4 text-sm text-muted-foreground">
              This subnet has more than {data.grid_limit} usable addresses; the grid is hidden for performance.
            </p>
          )}
          <div className="mt-4 flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
            <span className="inline-flex items-center gap-1"><span className={cn("size-2.5 rounded", CELL_COLORS.free)} aria-hidden /> Free</span>
            <span className="inline-flex items-center gap-1"><span className={cn("size-2.5 rounded", CELL_COLORS.assigned)} aria-hidden /> Assigned</span>
            <span className="inline-flex items-center gap-1"><span className={cn("size-2.5 rounded", CELL_COLORS.reserved)} aria-hidden /> Reserved</span>
            <span className="inline-flex items-center gap-1"><span className={cn("size-2.5 rounded", CELL_COLORS.excluded)} aria-hidden /> Excluded</span>
            <span className="inline-flex items-center gap-1"><span className={cn("size-2.5 rounded", CELL_COLORS.conflict)} aria-hidden /> Conflict</span>
          </div>
        </CardContent>
      </Card>

      {/* Allocations table */}
      <AllocationsTable subnetId={subnet.id} addresses={data.addresses} />

      {selectedCell ? (
        <AddressDialog
          subnetId={subnet.id}
          address={selectedCell}
          onClose={() => setSelectedCell(null)}
        />
      ) : null}
    </>
  )
}

function hostPart(address: string): string {
  const idx = address.lastIndexOf(".")
  if (idx === -1) {
    // IPv6: show last group(s)
    const parts = address.split(":")
    return ":" + parts[parts.length - 1]
  }
  return "." + address.slice(idx + 1)
}

// ---------- allocations table ----------

function AllocationsTable({ subnetId, addresses }: { subnetId: string; addresses: IPAddress[] }) {
  const [qDraft, setQDraft] = useState("")
  const release = useReleaseIP()
  const remove = useDeleteIP()

  const [q, setQ] = useState("")
  const filtered = addresses.filter(
    (a) =>
      q === "" ||
      a.address.includes(q) ||
      a.hostname.toLowerCase().includes(q.toLowerCase()),
  )

  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between">
        <CardTitle className="text-sm">Allocations ({addresses.length})</CardTitle>
        <div className="w-56">
          <Input
            aria-label="Filter allocations"
            placeholder="Search IP or hostname…"
            value={qDraft}
            onChange={(e) => setQDraft(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && setQ(qDraft.trim())}
            onBlur={() => setQ(qDraft.trim())}
            className="h-8 text-xs"
          />
        </div>
      </CardHeader>
      <CardContent>
        {filtered.length === 0 ? (
          <p className="py-6 text-center text-sm text-muted-foreground">No tracked addresses.</p>
        ) : (
          <div className="overflow-hidden rounded-lg border">
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead>Address</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Method</TableHead>
                  <TableHead>Hostname</TableHead>
                  <TableHead>Asset</TableHead>
                  <TableHead>Description</TableHead>
                  <TableHead className="w-24" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {filtered.map((ip) => (
                  <TableRow key={ip.id}>
                    <TableCell className="font-mono text-xs">{ip.address}</TableCell>
                    <TableCell><StatusBadge status={ip.status} /></TableCell>
                    <TableCell className="text-muted-foreground">{ip.assignment_method ?? "—"}</TableCell>
                    <TableCell className="font-mono text-xs">{ip.hostname || "—"}</TableCell>
                    <TableCell>
                      {ip.asset_id && ip.asset_name ? (
                        <Link to={`/assets/${ip.asset_id}`} className="text-sm hover:underline underline-offset-2">
                          {ip.asset_name}
                        </Link>
                      ) : "—"}
                    </TableCell>
                    <TableCell className="max-w-xs truncate text-xs text-muted-foreground">{ip.description || "—"}</TableCell>
                    <TableCell>
                      <div className="flex justify-end gap-1">
                        {ip.status !== "available" ? (
                          <Button
                            variant="ghost"
                            size="sm"
                            className="h-7 px-2 text-xs"
                            onClick={() =>
                              release.mutate(ip.id, {
                                onError: (err) => toast.error(isApiError(err) ? err.message : "Release failed."),
                              })
                            }
                          >
                            Release
                          </Button>
                        ) : null}
                        <ConfirmDialog
                          trigger={
                            <Button variant="ghost" size="icon" className="size-7" aria-label={`Delete ${ip.address}`}>
                              <Trash2Icon className="size-3.5" aria-hidden />
                            </Button>
                          }
                          title={`Remove tracking for ${ip.address}?`}
                          description="The assignment history is preserved before the row is deleted."
                          confirmLabel="Delete"
                          onConfirm={() =>
                            remove.mutate(ip.id, {
                              onError: (err) => toast.error(isApiError(err) ? err.message : "Delete failed."),
                            })
                          }
                        />
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
        <p className="mt-3 text-xs text-muted-foreground">
          Subnet ID <span className="font-mono">{subnetId}</span>
        </p>
      </CardContent>
    </Card>
  )
}

// ---------- address dialog (allocate / edit / release / bulk) ----------

function AddressDialog({
  subnetId, address, onClose,
}: {
  subnetId: string
  address: string
  onClose: () => void
}) {
  const allocate = useAllocateIP()
  const update = useUpdateIP()
  const remove = useDeleteIP()

  // Load current row (if any) from the subnet detail cache via a light fetch:
  const { data: subnetDetail } = useSubnet(subnetId)
  const row = subnetDetail?.addresses.find((a) => a.address === address)

  const [status, setStatus] = useState(row?.status ?? "assigned")
  const [method, setMethod] = useState(row?.assignment_method ?? "static")
  const [hostname, setHostname] = useState(row?.hostname ?? "")
  const [description, setDescription] = useState(row?.description ?? "")

  const submit = () => {
    if (row && row.status !== "available") {
      update.mutate(
        { id: row.id, payload: { status, assignment_method: method, hostname, description, interface_id: row.interface_id ?? "" } },
        { onSuccess: () => { toast.success("Address updated."); onClose() },
          onError: (err) => toast.error(isApiError(err) ? err.message : "Update failed.") },
      )
    } else {
      allocate.mutate(
        { subnetId, payload: { address, status, assignment_method: method, hostname, description } },
        { onSuccess: () => { toast.success(`${address} allocated.`); onClose() },
          onError: (err) => toast.error(isApiError(err) ? err.message : "Allocation failed.") },
      )
    }
  }

  return (
    <Dialog open onOpenChange={(v) => !v && onClose()}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle className="font-mono">{address}</DialogTitle>
          <DialogDescription>
            {row && row.status !== "available"
              ? "This address is actively allocated. Changes are audited."
              : "Allocate this address or mark it reserved/excluded."}
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-3">
          <div className="grid gap-3 sm:grid-cols-2">
            <div className="space-y-1.5">
              <Label htmlFor="ip-status">Status</Label>
              <Select id="ip-status" value={status} onChange={(e) => setStatus(e.target.value)}>
                <option value="assigned">Assigned</option>
                <option value="reserved">Reserved</option>
                <option value="excluded">Excluded</option>
              </Select>
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="ip-method">Assignment method</Label>
              <Select id="ip-method" value={method} onChange={(e) => setMethod(e.target.value)}>
                <option value="static">Static</option>
                <option value="dhcp">DHCP</option>
                <option value="reserved">Reserved</option>
              </Select>
            </div>
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="ip-hostname">Hostname</Label>
            <Input id="ip-hostname" className="font-mono" value={hostname} onChange={(e) => setHostname(e.target.value)} />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="ip-desc">Description</Label>
            <Input id="ip-desc" value={description} onChange={(e) => setDescription(e.target.value)} />
          </div>
        </div>

        <DialogFooter className="items-center">
          {row && row.status !== "available" ? (
            <Button
              variant="outline"
              size="sm"
              onClick={() =>
                update.mutate(
                  { id: row.id, payload: { status: "available", assignment_method: "", hostname: hostname, description: description, interface_id: row.interface_id ?? "" } },
                  { onSuccess: () => { toast.success("Address released."); onClose() },
                    onError: (err) => toast.error(isApiError(err) ? err.message : "Release failed.") },
                )
              }
            >
              Release
            </Button>
          ) : null}
          {row ? (
            <ConfirmDialog
              trigger={
                <Button variant="ghost" size="sm">
                  <Trash2Icon className="size-4" aria-hidden />Remove tracking
                </Button>
              }
              title={`Remove tracking for ${address}?`}
              description="History is preserved; the address becomes free."
              confirmLabel="Remove"
              onConfirm={() =>
                remove.mutate(row.id, {
                  onSuccess: () => {
                    toast.success("Tracking removed.")
                    onClose()
                  },
                  onError: (err) => toast.error(isApiError(err) ? err.message : "Delete failed."),
                })
              }
            />
          ) : null}
          <div className="flex-1" />
          <Button variant="outline" size="sm" onClick={onClose}>Cancel</Button>
          <Button size="sm" onClick={submit} disabled={allocate.isPending || update.isPending}>
            {allocate.isPending || update.isPending ? "Saving…" : row && row.status !== "available" ? "Save changes" : "Allocate"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}


// ---------- subnet edit dialog ----------

function SubnetEditDialog({ subnet }: { subnet: Subnet }) {
  const [open, setOpen] = useState(false)
  const updateSubnet = useUpdateSubnetLocal()

  const [form, setFormState] = useState<SubnetFormState>(() => formFromSubnet(subnet))
  const setForm = (patch: Partial<SubnetFormState>) => setFormState((prev) => ({ ...prev, ...patch }))

  const openDialog = () => {
    setFormState(formFromSubnet(subnet))
    setOpen(true)
  }

  const submit = () => {
    updateSubnet.mutate(
      { id: subnet.id, payload: subnetPayload(form) },
      {
        onSuccess: () => {
          toast.success("Subnet updated.")
          setOpen(false)
        },
        onError: (err) => toast.error(isApiError(err) ? err.message : "Could not update subnet."),
      },
    )
  }

  return (
    <>
      <Button variant="outline" size="sm" onClick={openDialog}>
        <PencilIcon className="size-4" aria-hidden />Edit
      </Button>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="max-w-xl">
          <DialogHeader>
            <DialogTitle>Edit {subnet.cidr}</DialogTitle>
            <DialogDescription>Context changes are disabled once allocations exist.</DialogDescription>
          </DialogHeader>
          <SubnetFormFields form={form} setForm={setForm} editing />
          <DialogFooter>
            <Button variant="outline" size="sm" onClick={() => setOpen(false)}>Cancel</Button>
            <Button size="sm" onClick={submit} disabled={updateSubnet.isPending}>
              {updateSubnet.isPending ? "Saving…" : "Save changes"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}

import { useUpdateSubnet } from "@/api/hooks"
function useUpdateSubnetLocal() {
  return useUpdateSubnet()
}

function formFromSubnet(sn: Subnet): SubnetFormState {
  return {
    cidr: sn.cidr,
    name: sn.name,
    description: sn.description,
    context_id: sn.context_id,
    vlan_id: sn.vlan_id ?? "",
    gateway: sn.gateway ?? "",
    dns: sn.dns_servers.join(", "),
    dhcp_start: sn.dhcp_start ?? "",
    dhcp_end: sn.dhcp_end ?? "",
    location_id: sn.location_id ?? "",
    environment: sn.environment,
  }
}
