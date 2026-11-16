import { useEffect, useState, type ReactNode } from "react"
import { zodResolver } from "@hookform/resolvers/zod"
import { useForm } from "react-hook-form"
import { toast } from "sonner"
import { z } from "zod"

import { isApiError } from "@/api/client"
import {
  useAssetTypes,
  useCreateAsset,
  useLocations,
  useTags,
  useUpdateAsset,
} from "@/api/hooks"
import type { Asset, AssetPayload } from "@/api/types"
import { AssetPicker } from "@/components/AssetPicker"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select } from "@/components/ui/select"
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { ChevronDownIcon, TagIcon } from "lucide-react"
import { tagDotClass } from "@/components/tag-color"

const assetSchema = z.object({
  name: z.string().trim().min(1, "Name is required.").max(100),
  hostname: z.string().trim().max(100).optional(),
  type_id: z.string().min(1, "Pick a device type."),
  status: z.string(),
  lifecycle: z.string(),
  environment: z.string(),
  manufacturer: z.string().trim().max(100).optional(),
  model: z.string().trim().max(100).optional(),
  serial_number: z.string().trim().max(100).optional(),
  os_name: z.string().trim().max(100).optional(),
  os_version: z.string().trim().max(50).optional(),
  cpu_model: z.string().trim().max(100).optional(),
  cpu_cores: z.string().optional(),
  ram_gb: z.string().optional(),
  storage_gb: z.string().optional(),
  purchase_date: z.string().optional(),
  purchase_cost: z.string().optional(),
  warranty_expires: z.string().optional(),
  notes: z.string().max(5000).optional(),
})

type AssetForm = z.infer<typeof assetSchema>

interface AssetFormDialogProps {
  /** Present → edit mode; absent → create mode. */
  asset?: Asset
  trigger: ReactNode
  /** Defaults applied in create mode (e.g. preset parent or type). */
  defaults?: Partial<Pick<AssetPayload, "parent_id" | "type_id" | "environment">>
  /** Create-mode convenience: preselect this asset-type slug (e.g. "vm"). */
  defaultTypeSlug?: string
  /** Create-mode scope: limit the type picker to a category and/or slug list. */
  restrictTo?: { category?: string; slugs?: string[] }
  onSaved?: (asset: Asset) => void
}

const STATUS_OPTIONS = [
  { value: "unknown", label: "Unknown" },
  { value: "online", label: "Online" },
  { value: "offline", label: "Offline" },
  { value: "maintenance", label: "Maintenance" },
  { value: "retired", label: "Retired" },
]
const LIFECYCLE_OPTIONS = [
  { value: "planned", label: "Planned" },
  { value: "active", label: "Active" },
  { value: "staged", label: "Staged" },
  { value: "decommissioned", label: "Decommissioned" },
]
const ENVIRONMENT_OPTIONS = [
  { value: "production", label: "Production" },
  { value: "development", label: "Development" },
  { value: "testing", label: "Testing" },
  { value: "lab", label: "Lab" },
]

function optionalNumber(value: string | undefined): number | null {
  if (value === undefined || value.trim() === "") return null
  const n = Number(value)
  return Number.isFinite(n) ? n : null
}

function optionalDate(value: string | undefined): string | null {
  return value && value.trim() !== "" ? value : null
}

export function AssetFormDialog({
  asset,
  trigger,
  defaults,
  defaultTypeSlug,
  restrictTo,
  onSaved,
}: AssetFormDialogProps) {
  const [open, setOpen] = useState(false)
  const [locationId, setLocationId] = useState<string>(asset?.location_id ?? "")
  const [parentId, setParentId] = useState<string | null>(asset?.parent_id ?? defaults?.parent_id ?? null)
  const [tagIds, setTagIds] = useState<string[]>(asset?.tags.map((t) => t.id) ?? [])

  const { data: typeData } = useAssetTypes()
  const { data: locationData } = useLocations()
  const { data: tagData } = useTags()
  const createAsset = useCreateAsset()
  const updateAsset = useUpdateAsset()

  const form = useForm<AssetForm>({
    resolver: zodResolver(assetSchema),
    defaultValues: {},
  })

  // Reset the form whenever the dialog opens (create) or the asset changes (edit).
  useEffect(() => {
    if (!open) return
    form.reset({
      name: asset?.name ?? "",
      hostname: asset?.hostname ?? "",
      type_id: asset?.type_id ?? defaults?.type_id ?? "",
      status: asset?.status ?? "unknown",
      lifecycle: asset?.lifecycle ?? "active",
      environment: asset?.environment ?? defaults?.environment ?? "production",
      manufacturer: asset?.manufacturer ?? "",
      model: asset?.model ?? "",
      serial_number: asset?.serial_number ?? "",
      os_name: asset?.os_name ?? "",
      os_version: asset?.os_version ?? "",
      cpu_model: asset?.cpu_model ?? "",
      cpu_cores: asset?.cpu_cores != null ? String(asset.cpu_cores) : "",
      ram_gb: asset?.ram_gb != null ? String(asset.ram_gb) : "",
      storage_gb: asset?.storage_gb != null ? String(asset.storage_gb) : "",
      purchase_date: asset?.purchase_date ?? "",
      purchase_cost: asset?.purchase_cost != null ? String(asset.purchase_cost) : "",
      warranty_expires: asset?.warranty_expires ?? "",
      notes: asset?.notes ?? "",
    })
    setLocationId(asset?.location_id ?? "")
    setParentId(asset?.parent_id ?? defaults?.parent_id ?? null)
    setTagIds(asset?.tags.map((t) => t.id) ?? [])
  }, [open, asset, defaults, form])

  const types = typeData?.types ?? []

  // Apply the section's default type once the catalogue has loaded (create mode
  // only, and never over a user's own selection).
  useEffect(() => {
    if (!open || asset || !defaultTypeSlug || types.length === 0) return
    if (form.getValues("type_id")) return
    const match = types.find((t) => t.slug === defaultTypeSlug)
    if (match) form.setValue("type_id", match.id, { shouldValidate: false })
  }, [open, asset, defaultTypeSlug, types, form])

  // Section-scoped picker: keep only the types that belong to the current view
  // (VM page → virtual machine, Containers → LXC/Docker/K8s, ...).
  const scopedTypes = (() => {
    if (!restrictTo) return types
    if (restrictTo.slugs?.length) {
      const allowed = new Set(restrictTo.slugs)
      return types.filter((t) => allowed.has(t.slug))
    }
    if (restrictTo.category) {
      return types.filter((t) => t.category === restrictTo.category)
    }
    return types
  })()
  const physicalTypes = scopedTypes.filter((t) => t.category === "physical")
  const virtualTypes = scopedTypes.filter((t) => t.category === "virtual")
  const locations = locationData?.locations ?? []
  const tags = tagData?.tags ?? []
  const selectedType = types.find((t) => t.id === form.watch("type_id"))

  const onSubmit = form.handleSubmit((values) => {
    const payload: AssetPayload = {
      name: values.name.trim(),
      hostname: values.hostname?.trim() ?? "",
      type_id: values.type_id,
      status: values.status,
      lifecycle: values.lifecycle,
      environment: values.environment,
      location_id: locationId || null,
      parent_id: parentId,
      manufacturer: values.manufacturer?.trim() ?? "",
      model: values.model?.trim() ?? "",
      serial_number: values.serial_number?.trim() ?? "",
      os_name: values.os_name?.trim() ?? "",
      os_version: values.os_version?.trim() ?? "",
      cpu_model: values.cpu_model?.trim() ?? "",
      cpu_cores: optionalNumber(values.cpu_cores),
      ram_gb: optionalNumber(values.ram_gb),
      storage_gb: optionalNumber(values.storage_gb),
      purchase_date: optionalDate(values.purchase_date),
      purchase_cost: optionalNumber(values.purchase_cost),
      warranty_expires: optionalDate(values.warranty_expires),
      notes: values.notes?.trim() ?? "",
      tag_ids: tagIds,
    }

    const mutation = asset
      ? updateAsset.mutateAsync({ id: asset.id, patch: payload })
      : createAsset.mutateAsync(payload)

    mutation.then(
      (result) => {
        toast.success(asset ? "Asset updated." : "Asset registered.")
        setOpen(false)
        onSaved?.(result.asset)
      },
      (err) => {
        toast.error(isApiError(err) ? err.message : "Could not save the asset.")
      },
    )
  })

  const err = form.formState.errors

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>{trigger}</DialogTrigger>
      <DialogContent className="max-h-[90vh] max-w-2xl overflow-y-auto">
        <DialogHeader>
          <DialogTitle>{asset ? `Edit ${asset.name}` : "Register asset"}</DialogTitle>
          <DialogDescription>
            {asset
              ? "Changes are recorded in the activity log with before/after values."
              : "Add a device, VM, or container to your inventory."}
          </DialogDescription>
        </DialogHeader>

        <form onSubmit={onSubmit} className="space-y-5" noValidate>
          {err.root ? (
            <p className="rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive">
              {err.root.message}
            </p>
          ) : null}

          <fieldset className="space-y-4">
            <legend className="text-xs font-medium tracking-wider text-muted-foreground uppercase">
              Identity
            </legend>
            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-1.5">
                <Label htmlFor="asset-name">Name *</Label>
                <Input id="asset-name" {...form.register("name")} autoFocus />
                {err.name ? <p className="text-xs text-destructive">{err.name.message}</p> : null}
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="asset-hostname">Hostname</Label>
                <Input id="asset-hostname" className="font-mono" {...form.register("hostname")} />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="asset-type">Device type *</Label>
                <Select id="asset-type" {...form.register("type_id")}>
                  <option value="">Select type…</option>
                  {physicalTypes.length > 0 ? (
                    <optgroup label="Physical">{physicalTypes.map((t) => (
                      <option key={t.id} value={t.id}>{t.label}</option>
                    ))}</optgroup>
                  ) : null}
                  {virtualTypes.length > 0 ? (
                    <optgroup label="Virtual">{virtualTypes.map((t) => (
                      <option key={t.id} value={t.id}>{t.label}</option>
                    ))}</optgroup>
                  ) : null}
                </Select>
                {err.type_id ? <p className="text-xs text-destructive">{err.type_id.message}</p> : null}
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="asset-environment">Environment</Label>
                <Select id="asset-environment" {...form.register("environment")}>
                  {ENVIRONMENT_OPTIONS.map((o) => (
                    <option key={o.value} value={o.value}>{o.label}</option>
                  ))}
                </Select>
              </div>
            </div>
          </fieldset>

          <fieldset className="space-y-4">
            <legend className="text-xs font-medium tracking-wider text-muted-foreground uppercase">
              Placement
            </legend>
            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-1.5">
                <Label htmlFor="asset-location">Location</Label>
                <Select id="asset-location" value={locationId} onChange={(e) => setLocationId(e.target.value)}>
                  <option value="">No location</option>
                  {locations.map((l) => (
                    <option key={l.id} value={l.id}>{l.name}</option>
                  ))}
                </Select>
              </div>
              <div className="space-y-1.5">
                <Label>Parent host</Label>
                <AssetPicker
                  value={parentId}
                  onChange={(a) => setParentId(a?.id ?? null)}
                  excludeId={asset?.id}
                  placeholder={selectedType?.category === "virtual" ? "Host running this guest…" : "Optional parent…"}
                />
              </div>
            </div>
          </fieldset>

          <fieldset className="space-y-4">
            <legend className="text-xs font-medium tracking-wider text-muted-foreground uppercase">
              Hardware & OS
            </legend>
            <div className="grid gap-4 sm:grid-cols-3">
              <div className="space-y-1.5">
                <Label htmlFor="asset-manufacturer">Manufacturer</Label>
                <Input id="asset-manufacturer" {...form.register("manufacturer")} />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="asset-model">Model</Label>
                <Input id="asset-model" {...form.register("model")} />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="asset-serial">Serial number</Label>
                <Input id="asset-serial" className="font-mono" {...form.register("serial_number")} />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="asset-os">Operating system</Label>
                <Input id="asset-os" {...form.register("os_name")} />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="asset-os-version">OS version</Label>
                <Input id="asset-os-version" {...form.register("os_version")} />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="asset-cpu-model">CPU model</Label>
                <Input id="asset-cpu-model" {...form.register("cpu_model")} />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="asset-cpu-cores">CPU cores</Label>
                <Input id="asset-cpu-cores" type="number" min={0} {...form.register("cpu_cores")} />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="asset-ram">RAM (GB)</Label>
                <Input id="asset-ram" type="number" step="0.5" min={0} {...form.register("ram_gb")} />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="asset-storage">Storage (GB)</Label>
                <Input id="asset-storage" type="number" step="1" min={0} {...form.register("storage_gb")} />
              </div>
            </div>
          </fieldset>

          <fieldset className="space-y-4">
            <legend className="text-xs font-medium tracking-wider text-muted-foreground uppercase">
              Lifecycle
            </legend>
            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-1.5">
                <Label htmlFor="asset-status">Status</Label>
                <Select id="asset-status" {...form.register("status")}>
                  {STATUS_OPTIONS.map((o) => (
                    <option key={o.value} value={o.value}>{o.label}</option>
                  ))}
                </Select>
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="asset-lifecycle">Lifecycle</Label>
                <Select id="asset-lifecycle" {...form.register("lifecycle")}>
                  {LIFECYCLE_OPTIONS.map((o) => (
                    <option key={o.value} value={o.value}>{o.label}</option>
                  ))}
                </Select>
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="asset-purchase-date">Purchase date</Label>
                <Input id="asset-purchase-date" type="date" {...form.register("purchase_date")} />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="asset-purchase-cost">Purchase cost</Label>
                <Input id="asset-purchase-cost" type="number" step="0.01" min={0} {...form.register("purchase_cost")} />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="asset-warranty">Warranty expires</Label>
                <Input id="asset-warranty" type="date" {...form.register("warranty_expires")} />
              </div>
            </div>
          </fieldset>

          <fieldset className="space-y-4">
            <legend className="text-xs font-medium tracking-wider text-muted-foreground uppercase">
              Tags & notes
            </legend>
            <div className="space-y-1.5">
              <Label>Tags</Label>
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button variant="outline" size="sm" type="button" className="w-full justify-between font-normal">
                    <span className="flex items-center gap-1.5">
                      <TagIcon className="size-3.5 text-muted-foreground" aria-hidden />
                      {tagIds.length === 0 ? "No tags selected" : `${tagIds.length} tag${tagIds.length > 1 ? "s" : ""}`}
                    </span>
                    <ChevronDownIcon className="size-3.5 text-muted-foreground" aria-hidden />
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent className="w-64">
                  <DropdownMenuLabel>Attach tags</DropdownMenuLabel>
                  <DropdownMenuSeparator />
                  {tags.length === 0 ? (
                    <p className="px-2 py-2 text-xs text-muted-foreground">
                      No tags yet — create them under Administration → Tags.
                    </p>
                  ) : (
                    tags.map((t) => (
                      <DropdownMenuCheckboxItem
                        key={t.id}
                        checked={tagIds.includes(t.id)}
                        onCheckedChange={(checked) =>
                          setTagIds((prev) => (checked ? [...prev, t.id] : prev.filter((id) => id !== t.id)))
                        }
                        onSelect={(e) => e.preventDefault()}
                      >
                        <span className={`inline-block size-2 rounded-full ${tagDotClass(t.color)}`} aria-hidden />
                        {t.name}
                      </DropdownMenuCheckboxItem>
                    ))
                  )}
                </DropdownMenuContent>
              </DropdownMenu>
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="asset-notes">Notes</Label>
              <textarea
                id="asset-notes"
                rows={3}
                className="flex w-full rounded-md border border-input bg-transparent px-3 py-2 text-sm shadow-xs outline-none focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/40"
                {...form.register("notes")}
              />
            </div>
          </fieldset>

          <DialogFooter>
            <Button type="button" variant="outline" size="sm" onClick={() => setOpen(false)}>
              Cancel
            </Button>
            <Button type="submit" size="sm" disabled={createAsset.isPending || updateAsset.isPending}>
              {createAsset.isPending || updateAsset.isPending
                ? "Saving…"
                : asset
                  ? "Save changes"
                  : "Register asset"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
