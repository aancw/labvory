import { useState } from "react"
import { toast } from "sonner"
import { LayersIcon, PencilIcon, PlusIcon, Trash2Icon } from "lucide-react"

import { isApiError } from "@/api/client"
import {
  useCreateNetworkContext,
  useCreateVlan,
  useDeleteVlan,
  useNetworkContexts,
  useUpdateNetworkContext,
  useUpdateVlan,
  useVlans,
} from "@/api/hooks"
import type { NetworkContext, Vlan } from "@/api/types"
import { ConfirmDialog } from "@/components/ConfirmDialog"
import { EmptyState } from "@/components/EmptyState"
import { PageHeader } from "@/components/PageHeader"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select } from "@/components/ui/select"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"

export function VlansPage() {
  const { data, isLoading } = useVlans()
  const vlans = data?.vlans ?? []
  const [formOpen, setFormOpen] = useState(false)
  const [editing, setEditing] = useState<Vlan | null>(null)
  const [contextId, setContextId] = useState("")
  const [vlanNumber, setVlanNumber] = useState("")
  const [name, setName] = useState("")
  const [purpose, setPurpose] = useState("")
  const [description, setDescription] = useState("")

  const createVlan = useCreateVlan()
  const updateVlan = useUpdateVlan()
  const deleteVlan = useDeleteVlan()
  const { data: contextData } = useNetworkContexts()

  const openCreate = () => {
    setEditing(null)
    setContextId(contextData?.contexts[0]?.id ?? "")
    setVlanNumber("")
    setName("")
    setPurpose("")
    setDescription("")
    setFormOpen(true)
  }

  const openEdit = (v: Vlan) => {
    setEditing(v)
    setContextId(v.context_id)
    setVlanNumber(String(v.vlan_id))
    setName(v.name)
    setPurpose(v.purpose)
    setDescription(v.description)
    setFormOpen(true)
  }

  const submit = () => {
    const num = Number(vlanNumber)
    if (!Number.isInteger(num) || num < 1 || num > 4094) {
      toast.error("VLAN ID must be an integer between 1 and 4094.")
      return
    }
    if (editing) {
      updateVlan.mutate(
        { id: editing.id, payload: { vlan_id: num, name: name.trim(), purpose: purpose.trim(), description: description.trim() } },
        {
          onSuccess: () => {
            toast.success("VLAN updated.")
            setFormOpen(false)
          },
          onError: (err) => toast.error(isApiError(err) ? err.message : "Update failed."),
        },
      )
    } else {
      createVlan.mutate(
        { context_id: contextId, vlan_id: num, name: name.trim(), purpose: purpose.trim(), description: description.trim() },
        {
          onSuccess: () => {
            toast.success("VLAN created.")
            setFormOpen(false)
          },
          onError: (err) => toast.error(isApiError(err) ? err.message : "Create failed."),
        },
      )
    }
  }

  return (
    <>
      <PageHeader
        title="VLANs & Contexts"
        description="Logical segments per network context. The same VLAN ID may exist in separate contexts."
      >
        <Button size="sm" onClick={openCreate}>
          <PlusIcon className="size-4" aria-hidden />Add VLAN
        </Button>
      </PageHeader>

      <ContextsCard contexts={contextData?.contexts ?? []} />

      <div className="mt-6">
        {isLoading ? (
          <div className="h-40 animate-pulse rounded-lg bg-accent" />
        ) : vlans.length === 0 ? (
          <EmptyState
            icon={LayersIcon}
            title="No VLANs yet"
            description="Create VLANs to group your subnets — Management, Servers, Guest, IoT, Lab…"
          />
        ) : (
          <div className="overflow-hidden rounded-lg border">
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead className="w-20">VLAN ID</TableHead>
                  <TableHead>Name</TableHead>
                  <TableHead>Context</TableHead>
                  <TableHead>Purpose</TableHead>
                  <TableHead>Subnets</TableHead>
                  <TableHead className="w-24" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {vlans.map((v) => (
                  <TableRow key={v.id}>
                    <TableCell className="font-mono tabular-nums">{v.vlan_id}</TableCell>
                    <TableCell className="font-medium">{v.name}</TableCell>
                    <TableCell className="text-muted-foreground">{v.context_name}</TableCell>
                    <TableCell className="text-muted-foreground">{v.purpose || "—"}</TableCell>
                    <TableCell className="tabular-nums text-muted-foreground">{v.subnet_count}</TableCell>
                    <TableCell>
                      <div className="flex justify-end gap-1">
                        <Button variant="ghost" size="icon" className="size-7" aria-label={`Edit ${v.name}`} onClick={() => openEdit(v)}>
                          <PencilIcon className="size-3.5" aria-hidden />
                        </Button>
                        <ConfirmDialog
                          trigger={
                            <Button variant="ghost" size="icon" className="size-7" aria-label={`Delete ${v.name}`}>
                              <Trash2Icon className="size-3.5" aria-hidden />
                            </Button>
                          }
                          title={`Delete VLAN ${v.vlan_id}?`}
                          description="Subnets mapped to this VLAN are unlinked, not deleted."
                          confirmLabel="Delete VLAN"
                          onConfirm={() =>
                            deleteVlan.mutate(v.id, {
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
      </div>

      <Dialog open={formOpen} onOpenChange={setFormOpen}>
        <DialogContent className="max-w-sm">
          <DialogHeader>
            <DialogTitle>{editing ? `Edit VLAN ${editing.vlan_id}` : "Add VLAN"}</DialogTitle>
            <DialogDescription>VLAN IDs are unique within a context.</DialogDescription>
          </DialogHeader>
          <div className="space-y-3">
            <div className="grid grid-cols-2 gap-3">
              <div className="space-y-1.5">
                <Label htmlFor="vlan-num">VLAN ID *</Label>
                <Input id="vlan-num" type="number" min={1} max={4094} value={vlanNumber}
                  onChange={(e) => setVlanNumber(e.target.value)} autoFocus />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="vlan-context">Context</Label>
                <Select id="vlan-context" value={contextId} disabled={!!editing}
                  onChange={(e) => setContextId(e.target.value)}>
                  {(contextData?.contexts ?? []).map((c) => (
                    <option key={c.id} value={c.id}>{c.name}</option>
                  ))}
                </Select>
              </div>
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="vlan-name">Name *</Label>
              <Input id="vlan-name" value={name} onChange={(e) => setName(e.target.value)} />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="vlan-purpose">Purpose</Label>
              <Input id="vlan-purpose" placeholder="Management, Servers, Guest…" value={purpose} onChange={(e) => setPurpose(e.target.value)} />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="vlan-desc">Description</Label>
              <Input id="vlan-desc" value={description} onChange={(e) => setDescription(e.target.value)} />
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" size="sm" onClick={() => setFormOpen(false)}>Cancel</Button>
            <Button size="sm" onClick={submit} disabled={createVlan.isPending || updateVlan.isPending}>
              {createVlan.isPending || updateVlan.isPending ? "Saving…" : editing ? "Save changes" : "Create VLAN"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}

function ContextsCard({ contexts }: { contexts: NetworkContext[] }) {
  const create = useCreateNetworkContext()
  const update = useUpdateNetworkContext()
  const [name, setName] = useState("")
  const [description, setDescription] = useState("")

  const submit = () => {
    if (!name.trim()) return
    create.mutate(
      { name: name.trim(), description: description.trim() },
      {
        onSuccess: () => {
          toast.success("Context created.")
          setName("")
          setDescription("")
        },
        onError: (err) => toast.error(isApiError(err) ? err.message : "Create failed."),
      },
    )
  }

  const rename = (c: NetworkContext) => {
    const next = window.prompt(`Rename context "${c.name}"`, c.name)
    if (next && next.trim() && next.trim() !== c.name) {
      update.mutate(
        { id: c.id, payload: { name: next.trim(), description: c.description } },
        {
          onSuccess: () => toast.success("Context renamed."),
          onError: (err) => toast.error(isApiError(err) ? err.message : "Rename failed."),
        },
      )
    }
  }

  return (
    <Card>
      <CardHeader><CardTitle className="text-sm">Network contexts (VRF-style separation)</CardTitle></CardHeader>
      <CardContent>
        <div className="mb-3 flex flex-wrap gap-2">
          {contexts.map((c) => (
            <button
              key={c.id}
              type="button"
              onClick={() => rename(c)}
              title={`${c.subnet_count} subnets · ${c.vlan_count} VLANs — click to rename`}
              className="inline-flex items-center gap-2 rounded-md border bg-muted/40 px-2.5 py-1 text-sm hover:bg-accent"
            >
              <span className="font-medium">{c.name}</span>
              <span className="text-xs text-muted-foreground tabular-nums">{c.subnet_count}/{c.vlan_count}</span>
            </button>
          ))}
        </div>
        <div className="flex max-w-md gap-2">
          <Input placeholder="New context name…" value={name} onChange={(e) => setName(e.target.value)} className="h-8" />
          <Input placeholder="Description" value={description} onChange={(e) => setDescription(e.target.value)}
            className="h-8 flex-1"
            onKeyDown={(e) => e.key === "Enter" && submit()} />
          <Button variant="outline" size="sm" onClick={submit} disabled={create.isPending || !name.trim()}>
            Add
          </Button>
        </div>
      </CardContent>
    </Card>
  )
}
