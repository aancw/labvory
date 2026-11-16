import { useEffect, useState } from "react"
import { Link, useSearchParams } from "react-router-dom"
import { toast } from "sonner"
import { GlobeIcon, PlusIcon, Trash2Icon } from "lucide-react"

import { isApiError } from "@/api/client"
import {
  useCreateSubnet,
  useDeleteSubnet,
  useLocations,
  useNetworkContexts,
  useSubnets,
  useVlans,
} from "@/api/hooks"
import { ConfirmDialog } from "@/components/ConfirmDialog"
import { EmptyState } from "@/components/EmptyState"
import { PageHeader } from "@/components/PageHeader"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select } from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import { cn } from "@/lib/utils"

export function UtilizationBar({ value, className }: { value: number; className?: string }) {
  const tone =
    value >= 90 ? "bg-red-500" : value >= 70 ? "bg-amber-500" : value >= 40 ? "bg-sky-500" : "bg-emerald-500"
  return (
    <div className={cn("flex items-center gap-2", className)}>
      <div className="h-1.5 w-24 overflow-hidden rounded-full bg-muted">
        <div className={cn("h-full rounded-full transition-all", tone)} style={{ width: `${value}%` }} />
      </div>
      <span className="w-8 text-right text-xs tabular-nums text-muted-foreground">{value}%</span>
    </div>
  )
}

export function SubnetFormFields({
  form, setForm, editing,
}: {
  form: SubnetFormState
  setForm: (patch: Partial<SubnetFormState>) => void
  editing: boolean
}) {
  const { data: contextData } = useNetworkContexts()
  const { data: vlanData } = useVlans()
  const { data: locationData } = useLocations()

  return (
    <div className="space-y-3">
      <div className="grid gap-3 sm:grid-cols-2">
        <div className="space-y-1.5">
          <Label htmlFor="sn-cidr">CIDR *</Label>
          <Input id="sn-cidr" className="font-mono" placeholder="10.10.0.0/24"
            value={form.cidr} onChange={(e) => setForm({ cidr: e.target.value })} autoFocus />
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="sn-name">Name *</Label>
          <Input id="sn-name" placeholder="Servers LAN"
            value={form.name} onChange={(e) => setForm({ name: e.target.value })} />
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="sn-context">Network context *</Label>
          <Select id="sn-context" value={form.context_id}
            disabled={editing}
            onChange={(e) => setForm({ context_id: e.target.value, vlan_id: "" })}>
            {(contextData?.contexts ?? []).map((c) => (
              <option key={c.id} value={c.id}>{c.name}</option>
            ))}
          </Select>
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="sn-vlan">VLAN</Label>
          <Select id="sn-vlan" value={form.vlan_id} onChange={(e) => setForm({ vlan_id: e.target.value })}>
            <option value="">None</option>
            {(vlanData?.vlans ?? []).filter((v) => v.context_id === form.context_id).map((v) => (
              <option key={v.id} value={v.id}>{v.vlan_id} · {v.name}</option>
            ))}
          </Select>
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="sn-gateway">Gateway</Label>
          <Input id="sn-gateway" className="font-mono" placeholder="10.10.0.1"
            value={form.gateway} onChange={(e) => setForm({ gateway: e.target.value })} />
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="sn-dns">DNS servers (comma-separated)</Label>
          <Input id="sn-dns" className="font-mono" placeholder="10.10.0.53, 1.1.1.1"
            value={form.dns} onChange={(e) => setForm({ dns: e.target.value })} />
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="sn-dhcp-start">DHCP range start</Label>
          <Input id="sn-dhcp-start" className="font-mono" value={form.dhcp_start}
            onChange={(e) => setForm({ dhcp_start: e.target.value })} />
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="sn-dhcp-end">DHCP range end</Label>
          <Input id="sn-dhcp-end" className="font-mono" value={form.dhcp_end}
            onChange={(e) => setForm({ dhcp_end: e.target.value })} />
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="sn-location">Location</Label>
          <Select id="sn-location" value={form.location_id}
            onChange={(e) => setForm({ location_id: e.target.value })}>
            <option value="">None</option>
            {(locationData?.locations ?? []).map((l) => (
              <option key={l.id} value={l.id}>{l.name}</option>
            ))}
          </Select>
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="sn-env">Environment</Label>
          <Select id="sn-env" value={form.environment} onChange={(e) => setForm({ environment: e.target.value })}>
            <option value="production">Production</option>
            <option value="development">Development</option>
            <option value="testing">Testing</option>
            <option value="lab">Lab</option>
          </Select>
        </div>
      </div>
      <div className="space-y-1.5">
        <Label htmlFor="sn-desc">Description</Label>
        <Input id="sn-desc" value={form.description} onChange={(e) => setForm({ description: e.target.value })} />
      </div>
    </div>
  )
}

export interface SubnetFormState {
  cidr: string
  name: string
  description: string
  context_id: string
  vlan_id: string
  gateway: string
  dns: string
  dhcp_start: string
  dhcp_end: string
  location_id: string
  environment: string
}

export function emptySubnetForm(contextId: string): SubnetFormState {
  return {
    cidr: "", name: "", description: "", context_id: contextId, vlan_id: "",
    gateway: "", dns: "", dhcp_start: "", dhcp_end: "", location_id: "",
    environment: "production",
  }
}

export function subnetPayload(f: SubnetFormState): Record<string, unknown> {
  const dns = f.dns.split(",").map((s) => s.trim()).filter(Boolean)
  return {
    cidr: f.cidr.trim(),
    name: f.name.trim(),
    description: f.description.trim(),
    context_id: f.context_id,
    vlan_id: f.vlan_id || null,
    gateway: f.gateway.trim() || null,
    dns_servers: dns,
    dhcp_start: f.dhcp_start.trim() || null,
    dhcp_end: f.dhcp_end.trim() || null,
    location_id: f.location_id || null,
    environment: f.environment,
  }
}

export function SubnetsPage() {
  const [qDraft, setQDraft] = useState("")
  const [q, setQ] = useState("")
  const { data, isLoading } = useSubnets({ q })
  const { data: contextData } = useNetworkContexts()
  const [createOpen, setCreateOpen] = useState(false)

  // Quick add from the topbar lands here via ?new=1 — open the create dialog once.
  const [searchParams, setSearchParams] = useSearchParams()
  useEffect(() => {
    if (searchParams.get("new") === "1") {
      setCreateOpen(true)
      searchParams.delete("new")
      setSearchParams(searchParams, { replace: true })
    }
  }, [searchParams, setSearchParams])
  const defaultContext = contextData?.contexts[0]?.id ?? ""
  const [form, setFormState] = useState<SubnetFormState>(() => emptySubnetForm(defaultContext))
  const setForm = (patch: Partial<SubnetFormState>) => setFormState((prev) => ({ ...prev, ...patch }))

  const createSubnet = useCreateSubnet()
  const deleteSubnet = useDeleteSubnet()

  const submitCreate = () => {
    createSubnet.mutate(subnetPayload(form), {
      onSuccess: () => {
        toast.success("Subnet created.")
        setCreateOpen(false)
      },
      onError: (err) => toast.error(isApiError(err) ? err.message : "Could not create subnet."),
    })
  }

  const subnets = data?.subnets ?? []

  return (
    <>
      <PageHeader
        title="Subnets"
        description="Routed segments with live utilization. Overlapping ranges require separate contexts."
      >
        <Button size="sm" onClick={() => {
          setForm(emptySubnetForm(defaultContext))
          setCreateOpen(true)
        }}>
          <PlusIcon className="size-4" aria-hidden />Add subnet
        </Button>
      </PageHeader>

      <div className="mb-4 max-w-xs">
        <Input
          aria-label="Search subnets"
          placeholder="Search name or CIDR…"
          value={qDraft}
          onChange={(e) => setQDraft(e.target.value)}
          onKeyDown={(e) => e.key === "Enter" && setQ(qDraft.trim())}
          onBlur={() => setQ(qDraft.trim())}
        />
      </div>

      {isLoading ? (
        <div className="space-y-2">{Array.from({ length: 4 }).map((_, i) => (
          <Skeleton key={i} className="h-12 w-full" />
        ))}</div>
      ) : subnets.length === 0 ? (
        <EmptyState
          icon={GlobeIcon}
          title="No subnets yet"
          description="Create your first subnet to start tracking IP allocations."
        />
      ) : (
        <div className="overflow-hidden rounded-lg border">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b bg-muted/40 text-left">
                <th className="px-4 py-2.5 text-xs font-medium uppercase text-muted-foreground">CIDR</th>
                <th className="px-4 py-2.5 text-xs font-medium uppercase text-muted-foreground">Name</th>
                <th className="px-4 py-2.5 text-xs font-medium uppercase text-muted-foreground">VLAN</th>
                <th className="px-4 py-2.5 text-xs font-medium uppercase text-muted-foreground">Context</th>
                <th className="px-4 py-2.5 text-xs font-medium uppercase text-muted-foreground">Gateway</th>
                <th className="px-4 py-2.5 text-xs font-medium uppercase text-muted-foreground">Utilization</th>
                <th className="w-10" />
              </tr>
            </thead>
            <tbody>
              {subnets.map((sn) => (
                <tr key={sn.id} className="border-b last:border-0 hover:bg-muted/40">
                  <td className="px-4 py-2.5">
                    <Link to={`/subnets/${sn.id}`} className="font-mono text-sm font-medium hover:underline underline-offset-2">
                      {sn.cidr}
                    </Link>
                  </td>
                  <td className="px-4 py-2.5">{sn.name}</td>
                  <td className="px-4 py-2.5 text-muted-foreground tabular-nums">
                    {sn.vlan_number != null ? `VLAN ${sn.vlan_number}` : "—"}
                  </td>
                  <td className="px-4 py-2.5 text-muted-foreground">{sn.context_name}</td>
                  <td className="px-4 py-2.5 font-mono text-xs">{sn.gateway ?? "—"}</td>
                  <td className="px-4 py-2.5">
                    <UtilizationBar value={sn.utilization} />
                  </td>
                  <td>
                    <ConfirmDialog
                      trigger={
                        <Button variant="ghost" size="icon" className="size-7" aria-label={`Delete ${sn.name}`}>
                          <Trash2Icon className="size-3.5" aria-hidden />
                        </Button>
                      }
                      title={`Delete ${sn.cidr}?`}
                      description="Subnets with active allocations (assigned/reserved/excluded) cannot be deleted."
                      confirmLabel="Delete subnet"
                      onConfirm={() =>
                        deleteSubnet.mutate(sn.id, {
                          onError: (err) => toast.error(isApiError(err) ? err.message : "Delete failed."),
                        })
                      }
                    />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <Dialog open={createOpen} onOpenChange={setCreateOpen}>
        <DialogContent className="max-w-xl">
          <DialogHeader>
            <DialogTitle>Add subnet</DialogTitle>
            <DialogDescription>Network and broadcast addresses are reserved automatically; /31 and /32 are handled per RFC 3021.</DialogDescription>
          </DialogHeader>
          <SubnetFormFields form={form} setForm={setForm} editing={false} />
          <DialogFooter>
            <Button variant="outline" size="sm" onClick={() => setCreateOpen(false)}>Cancel</Button>
            <Button size="sm" onClick={submitCreate} disabled={createSubnet.isPending}>
              {createSubnet.isPending ? "Creating…" : "Create subnet"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}
