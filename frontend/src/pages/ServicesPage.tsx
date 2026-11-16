import { useState } from "react"
import { Link } from "react-router-dom"
import { toast } from "sonner"
import { CircuitBoardIcon, PlusIcon, Trash2Icon } from "lucide-react"

import { isApiError } from "@/api/client"
import { useCreateService, useDeleteService, useServices } from "@/api/hooks"
import { ConfirmDialog } from "@/components/ConfirmDialog"
import { EmptyState } from "@/components/EmptyState"
import { PageHeader } from "@/components/PageHeader"
import { StatusBadge } from "@/components/StatusBadge"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select } from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"

const CATEGORIES = [
  "media", "network", "monitoring", "home-automation", "storage", "development",
  "security", "productivity", "database", "proxy", "other",
]

export interface ServiceFormState {
  name: string
  description: string
  category: string
  deployment_type: string
  environment: string
  status: string
  repo_url: string
  docs_url: string
  notes: string
}

export const emptyServiceForm = (): ServiceFormState => ({
  name: "", description: "", category: "other", deployment_type: "native",
  environment: "production", status: "unknown", repo_url: "", docs_url: "", notes: "",
})

export function servicePayload(f: ServiceFormState): Record<string, unknown> {
  return {
    name: f.name.trim(), description: f.description.trim(), category: f.category,
    deployment_type: f.deployment_type, environment: f.environment, status: f.status,
    repo_url: f.repo_url.trim(), docs_url: f.docs_url.trim(), notes: f.notes.trim(),
  }
}

export function ServiceFormFields({ form, setForm }: {
  form: ServiceFormState
  setForm: (patch: Partial<ServiceFormState>) => void
}) {
  return (
    <div className="space-y-3">
      <div className="grid gap-3 sm:grid-cols-2">
        <div className="space-y-1.5">
          <Label htmlFor="svc-name">Name *</Label>
          <Input id="svc-name" value={form.name} onChange={(e) => setForm({ name: e.target.value })} autoFocus />
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="svc-category">Category</Label>
          <Select id="svc-category" value={form.category} onChange={(e) => setForm({ category: e.target.value })}>
            {CATEGORIES.map((c) => <option key={c} value={c}>{c}</option>)}
          </Select>
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="svc-deploy">Deployment type</Label>
          <Select id="svc-deploy" value={form.deployment_type} onChange={(e) => setForm({ deployment_type: e.target.value })}>
            <option value="native">Native</option>
            <option value="docker">Docker</option>
            <option value="vm">VM</option>
            <option value="lxc">LXC</option>
            <option value="kubernetes">Kubernetes</option>
          </Select>
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="svc-env">Environment</Label>
          <Select id="svc-env" value={form.environment} onChange={(e) => setForm({ environment: e.target.value })}>
            <option value="production">Production</option>
            <option value="development">Development</option>
            <option value="testing">Testing</option>
            <option value="lab">Lab</option>
          </Select>
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="svc-status">Status</Label>
          <Select id="svc-status" value={form.status} onChange={(e) => setForm({ status: e.target.value })}>
            <option value="unknown">Unknown</option>
            <option value="online">Online</option>
            <option value="offline">Offline</option>
            <option value="maintenance">Maintenance</option>
            <option value="retired">Retired</option>
          </Select>
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="svc-repo">Repository URL</Label>
          <Input id="svc-repo" className="font-mono text-xs" value={form.repo_url} onChange={(e) => setForm({ repo_url: e.target.value })} />
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="svc-docs">Documentation URL</Label>
          <Input id="svc-docs" className="font-mono text-xs" value={form.docs_url} onChange={(e) => setForm({ docs_url: e.target.value })} />
        </div>
      </div>
      <div className="space-y-1.5">
        <Label htmlFor="svc-desc">Description</Label>
        <Input id="svc-desc" value={form.description} onChange={(e) => setForm({ description: e.target.value })} />
      </div>
    </div>
  )
}

export function ServicesPage() {
  const [qDraft, setQDraft] = useState("")
  const [q, setQ] = useState("")
  const [category, setCategory] = useState("")
  const [deploymentType, setDeploymentType] = useState("")
  const { data, isLoading } = useServices({ q, category, deployment_type: deploymentType })
  const [createOpen, setCreateOpen] = useState(false)
  const [form, setFormState] = useState<ServiceFormState>(emptyServiceForm)
  const setForm = (patch: Partial<ServiceFormState>) => setFormState((prev) => ({ ...prev, ...patch }))
  const createService = useCreateService()
  const deleteService = useDeleteService()

  const submitCreate = () => {
    createService.mutate(servicePayload(form), {
      onSuccess: () => {
        toast.success("Service registered.")
        setCreateOpen(false)
      },
      onError: (err) => toast.error(isApiError(err) ? err.message : "Could not create service."),
    })
  }

  const services = data?.services ?? []

  return (
    <>
      <PageHeader
        title="Services"
        description="Applications running across your infrastructure, mapped to their hosts."
      >
        <Button size="sm" onClick={() => { setFormState(emptyServiceForm()); setCreateOpen(true) }}>
          <PlusIcon className="size-4" aria-hidden />Register service
        </Button>
      </PageHeader>

      <div className="mb-4 flex flex-wrap gap-2">
        <div className="w-64">
          <Input
            aria-label="Search services"
            placeholder="Search services…"
            value={qDraft}
            onChange={(e) => setQDraft(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && setQ(qDraft.trim())}
            onBlur={() => setQ(qDraft.trim())}
          />
        </div>
        <Select aria-label="Category filter" className="w-44" value={category} onChange={(e) => setCategory(e.target.value)}>
          <option value="">All categories</option>
          {CATEGORIES.map((c) => <option key={c} value={c}>{c}</option>)}
        </Select>
        <Select aria-label="Deployment type filter" className="w-44" value={deploymentType} onChange={(e) => setDeploymentType(e.target.value)}>
          <option value="">All deployment types</option>
          <option value="native">Native</option>
          <option value="docker">Docker</option>
          <option value="vm">VM</option>
          <option value="lxc">LXC</option>
          <option value="kubernetes">Kubernetes</option>
        </Select>
      </div>

      {isLoading ? (
        <div className="space-y-2">{Array.from({ length: 4 }).map((_, i) => (
          <Skeleton key={i} className="h-12 w-full" />
        ))}</div>
      ) : services.length === 0 ? (
        <EmptyState
          icon={CircuitBoardIcon}
          title="No services registered"
          description="Track your homelab applications — Pi-hole, Home Assistant, Jellyfin — and where they run."
        />
      ) : (
        <div className="overflow-hidden rounded-lg border">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b bg-muted/40 text-left">
                <th className="px-4 py-2.5 text-xs font-medium uppercase text-muted-foreground">Name</th>
                <th className="px-4 py-2.5 text-xs font-medium uppercase text-muted-foreground">Category</th>
                <th className="px-4 py-2.5 text-xs font-medium uppercase text-muted-foreground">Deployment</th>
                <th className="px-4 py-2.5 text-xs font-medium uppercase text-muted-foreground">Environment</th>
                <th className="px-4 py-2.5 text-xs font-medium uppercase text-muted-foreground">Status</th>
                <th className="px-4 py-2.5 text-xs font-medium uppercase text-muted-foreground">Hosts</th>
                <th className="w-10" />
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
                  <td className="px-4 py-2.5 text-muted-foreground">{svc.deployment_type}</td>
                  <td className="px-4 py-2.5 text-muted-foreground">{svc.environment}</td>
                  <td className="px-4 py-2.5"><StatusBadge status={svc.status} /></td>
                  <td className="px-4 py-2.5 tabular-nums text-muted-foreground">{svc.host_count}</td>
                  <td>
                    <ConfirmDialog
                      trigger={
                        <Button variant="ghost" size="icon" className="size-7" aria-label={`Delete ${svc.name}`}>
                          <Trash2Icon className="size-3.5" aria-hidden />
                        </Button>
                      }
                      title={`Delete ${svc.name}?`}
                      description="Endpoints and deployment mappings are removed with the service."
                      confirmLabel="Delete service"
                      onConfirm={() =>
                        deleteService.mutate(svc.id, {
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
            <DialogTitle>Register service</DialogTitle>
            <DialogDescription>Services exist independently of assets — map them to hosts afterwards.</DialogDescription>
          </DialogHeader>
          <ServiceFormFields form={form} setForm={setForm} />
          <DialogFooter>
            <Button variant="outline" size="sm" onClick={() => setCreateOpen(false)}>Cancel</Button>
            <Button size="sm" onClick={submitCreate} disabled={createService.isPending}>
              {createService.isPending ? "Creating…" : "Register service"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}
