import { useEffect, useMemo, useState } from "react"
import { Link, useLocation, useNavigate, useSearchParams } from "react-router-dom"
import { toast } from "sonner"
import {
  ChevronDownIcon,
  ChevronLeftIcon,
  ChevronRightIcon,
  ChevronUpIcon,
  Columns3Icon,
  MoreHorizontalIcon,
  MonitorIcon,
  MonitorDotIcon,
  PlusIcon,
  Trash2Icon,
} from "lucide-react"

import { isApiError } from "@/api/client"
import { useAssets, useBulkAssets, useLocations } from "@/api/hooks"
import { AssetFormDialog } from "@/components/AssetFormDialog"
import { ConfirmDialog } from "@/components/ConfirmDialog"
import { EmptyState } from "@/components/EmptyState"
import { PageHeader } from "@/components/PageHeader"
import { StatusBadge } from "@/components/StatusBadge"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { BookmarkPlusIcon, BookmarkIcon } from "lucide-react"
import { Input } from "@/components/ui/input"
import { Select } from "@/components/ui/select"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { formatRelative } from "@/lib/utils"

export const PAGE_SIZE = 50

/** Pre-filter presets behind the sidebar entries. */
export function assetViewPreset(pathname: string): { title: string; description: string; category?: string; type_slugs?: string; chips?: RuntimeChip[] } {
  if (pathname === "/assets/physical") {
    return { title: "Physical Devices", description: "Servers, network gear, SBCs and other hardware.", category: "physical" }
  }
  if (pathname === "/assets/virtual-machines") {
    return { title: "Virtual Machines", description: "KVM/QEMU guests across your hosts.", type_slugs: "vm" }
  }
  if (pathname === "/assets/containers") {
    return { title: "Containers", description: "LXC, Docker and Kubernetes workloads — filter by runtime below.", chips: CONTAINER_CHIPS }
  }
  return { title: "All Assets", description: "Every registered device, VM, and container." }
}

/** Runtime chips on the containers view. "All" deliberately excludes pods: synced
 *  pods are ephemeral and can flood the list — they are one click away under Pods. */
export interface RuntimeChip {
  label: string
  typeSlugs: string
  hint?: string
}

export const CONTAINER_CHIPS: RuntimeChip[] = [
  { label: "All", typeSlugs: "lxc,docker,k8s-node", hint: "Containers + Kubernetes nodes" },
  { label: "LXC", typeSlugs: "lxc" },
  { label: "Docker", typeSlugs: "docker" },
  { label: "K8s", typeSlugs: "k8s-node,pod", hint: "Nodes and pods" },
  { label: "Pods", typeSlugs: "pod", hint: "Kubernetes pods" },
]

const ENVIRONMENT_BADGES: Record<string, string> = {
  production: "text-emerald-600 dark:text-emerald-400",
  development: "text-sky-600 dark:text-sky-400",
  testing: "text-amber-600 dark:text-amber-400",
  lab: "text-violet-600 dark:text-violet-400",
}

export function AssetListPage() {
  const navigate = useNavigate()
  const { pathname } = useLocation()
  const [searchParams, setSearchParams] = useSearchParams()
  const preset = assetViewPreset(pathname)

  // Containers view: runtime chips drive the `type_slugs` URL param. An absent
  // param means the All chip, which deliberately excludes pods.
  const chips = preset.chips ?? null
  const urlTypeSlugs = searchParams.get("type_slugs") ?? ""
  const activeChip = chips ? chips.find((c) => c.typeSlugs === urlTypeSlugs) ?? chips[0] : undefined

  // The Register button follows the current section: it preselects the type and
  // narrows the picker to what this view is about.
  const registerScope = (() => {
    const slugs = (activeChip?.typeSlugs ?? preset.type_slugs)
      ?.split(",").map((s) => s.trim()).filter(Boolean) ?? []
    if (slugs.length > 0) {
      return { restrictTo: { slugs }, defaultTypeSlug: slugs[0] }
    }
    if (preset.category) {
      return { restrictTo: { category: preset.category } }
    }
    return {}
  })()

  // Quick add from the topbar lands here via ?new=1 — open the register dialog once.
  const [registerOpen, setRegisterOpen] = useState(false)
  useEffect(() => {
    if (searchParams.get("new") === "1") {
      setRegisterOpen(true)
      searchParams.delete("new")
      setSearchParams(searchParams, { replace: true })
    }
  }, [searchParams, setSearchParams])

  // URL is the source of truth for filters/sort/page so views are shareable.
  const q = searchParams.get("q") ?? ""
  const status = searchParams.get("status") ?? ""
  const environment = searchParams.get("environment") ?? ""
  const category = preset.category ?? searchParams.get("category") ?? ""
  const typeSlugs = chips
    ? (urlTypeSlugs || chips[0].typeSlugs)
    : preset.type_slugs ?? searchParams.get("type_slugs") ?? ""
  const locationId = searchParams.get("location") ?? ""
  const sort = searchParams.get("sort") ?? "updated_at"
  const order = searchParams.get("order") ?? "desc"
  const offset = Number(searchParams.get("offset") ?? "0") || 0

  const [searchDraft, setSearchDraft] = useState(q)
  const [selected, setSelected] = useState<Set<string>>(new Set())

  // Configurable columns (persisted locally).
  const COLUMN_STORAGE = "labvory-asset-columns"
  const [columns, setColumns] = useState<Record<string, boolean>>(() => {
    try {
      const stored = localStorage.getItem(COLUMN_STORAGE)
      if (stored) return JSON.parse(stored) as Record<string, boolean>
    } catch {
      // fall through to defaults
    }
    return { primary_ip: true, mac: true, os: true, parent: true, env: true }
  })
  const toggleColumn = (key: string) => {
    setColumns((prev) => {
      const next = { ...prev, [key]: !prev[key] }
      localStorage.setItem(COLUMN_STORAGE, JSON.stringify(next))
      return next
    })
  }

  // Saved views (filters per page variant, persisted locally).
  const VIEW_STORAGE = `labvory-views${pathname}`
  const [views, setViews] = useState<Record<string, string>>(() => {
    try {
      return JSON.parse(localStorage.getItem(VIEW_STORAGE) ?? "{}") as Record<string, string>
    } catch {
      return {}
    }
  })
  const saveCurrentView = () => {
    const name = window.prompt("Name this view", "")
    if (!name || !name.trim()) return
    const next = { ...views, [name.trim()]: searchParams.toString() }
    setViews(next)
    localStorage.setItem(VIEW_STORAGE, JSON.stringify(next))
    toast.success(`View "${name.trim()}" saved.`)
  }
  const applyView = (name: string) => {
    setSearchParams(new URLSearchParams(views[name] ?? ""))
  }
  const deleteView = (name: string) => {
    const next = { ...views }
    delete next[name]
    setViews(next)
    localStorage.setItem(VIEW_STORAGE, JSON.stringify(next))
  }

  const { data, isLoading, isError } = useAssets({
    q, status, environment, category, type_slugs: typeSlugs,
    location_id: locationId, sort, order, limit: PAGE_SIZE, offset,
  })
  const { data: locationData } = useLocations()
  const bulk = useBulkAssets()

  const setParam = (key: string, value: string) => {
    const next = new URLSearchParams(searchParams)
    if (value === "") next.delete(key)
    else next.set(key, value)
    if (key !== "offset") next.delete("offset")
    setSearchParams(next)
    if (key === "q" || key === "status" || key === "environment" || key === "location") {
      setSelected(new Set())
    }
  }

  const assets = data?.assets ?? []
  const total = data?.total ?? 0

  const allPageSelected = assets.length > 0 && assets.every((a) => selected.has(a.id))
  const toggleAll = () => {
    setSelected((prev) => {
      const next = new Set(prev)
      if (allPageSelected) {
        assets.forEach((a) => next.delete(a.id))
      } else {
        assets.forEach((a) => next.add(a.id))
      }
      return next
    })
  }
  const toggleOne = (id: string) => {
    setSelected((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const runBulk = (action: "delete" | "set_status", params?: { status?: string }) => {
    bulk.mutate(
      params?.status !== undefined
        ? { action, ids: [...selected], status: params.status }
        : { action, ids: [...selected] },
      {
        onSuccess: (result) => {
          toast.success(`${result.affected ?? selected.size} asset(s) updated.`)
          setSelected(new Set())
        },
        onError: (err) => toast.error(isApiError(err) ? err.message : "Bulk operation failed."),
      },
    )
  }

  const selectedAssets = useMemo(
    () => assets.filter((a) => selected.has(a.id)),
    [assets, selected],
  )
  const anyHostWithChildrenSelected = selectedAssets.some((a) => a.child_count > 0)

  if (isError) {
    return (
      <>
        <PageHeader title={preset.title} />
        <EmptyState
          icon={MonitorDotIcon}
          title="Inventory unavailable"
          description="Could not load assets. Check the API connection and reload."
        />
      </>
    )
  }

  return (
    <>
      <PageHeader
        title={preset.title}
        description={total > 0 ? `${total} asset${total > 1 ? "s" : ""} · ${preset.description}` : preset.description}
      >
        <AssetFormDialog
          {...registerScope}
          open={registerOpen}
          onOpenChange={setRegisterOpen}
          trigger={<Button size="sm"><PlusIcon className="size-4" aria-hidden />Register asset</Button>}
        />
      </PageHeader>

      {/* Runtime chips (containers view): presets above the filter bar. */}
      {chips ? (
        <Tabs
          value={activeChip?.label}
          onValueChange={(v) => {
            const chip = chips.find((c) => c.label === v)
            if (chip) setParam("type_slugs", chip === chips[0] ? "" : chip.typeSlugs)
          }}
        >
          <TabsList className="mb-4" aria-label="Container runtime filter">
            {chips.map((c) => (
              <TabsTrigger key={c.label} value={c.label} title={c.hint}>{c.label}</TabsTrigger>
            ))}
          </TabsList>
        </Tabs>
      ) : null}

      {/* Filter bar */}
      <div className="mb-4 flex flex-wrap items-center gap-2">
        <form
          className="relative w-full max-w-xs"
          onSubmit={(e) => {
            e.preventDefault()
            setParam("q", searchDraft.trim())
          }}
        >
          <Input
            aria-label="Search assets"
            placeholder="Search name, hostname, serial…"
            value={searchDraft}
            onChange={(e) => setSearchDraft(e.target.value)}
            className="pl-3"
          />
        </form>

        <Select aria-label="Status filter" className="w-36" value={status} onChange={(e) => setParam("status", e.target.value)}>
          <option value="">All statuses</option>
          <option value="online">Online</option>
          <option value="offline">Offline</option>
          <option value="maintenance">Maintenance</option>
          <option value="retired">Retired</option>
          <option value="unknown">Unknown</option>
        </Select>

        <Select aria-label="Environment filter" className="w-44" value={environment} onChange={(e) => setParam("environment", e.target.value)}>
          <option value="">All environments</option>
          <option value="production">Production</option>
          <option value="development">Development</option>
          <option value="testing">Testing</option>
          <option value="lab">Lab</option>
        </Select>

        {!preset.category && !preset.type_slugs && !chips ? (
          <Select aria-label="Category filter" className="w-44" value={category} onChange={(e) => setParam("category", e.target.value)}>
            <option value="">Physical + virtual</option>
            <option value="physical">Physical only</option>
            <option value="virtual">Virtual only</option>
          </Select>
        ) : null}

        <Select aria-label="Location filter" className="w-44" value={locationId} onChange={(e) => setParam("location", e.target.value)}>
          <option value="">All locations</option>
          {(locationData?.locations ?? []).map((l) => (
            <option key={l.id} value={l.id}>{l.name}</option>
          ))}
        </Select>

        <div className="flex-1" />

        {/* Column visibility */}
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="outline" size="sm">
              <Columns3Icon className="size-4" aria-hidden />Columns
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuLabel>Visible columns</DropdownMenuLabel>
            <DropdownMenuSeparator />
            <DropdownMenuCheckboxItem checked={columns.primary_ip ?? true} onCheckedChange={() => toggleColumn("primary_ip")}>
              Primary IP
            </DropdownMenuCheckboxItem>
            <DropdownMenuCheckboxItem checked={columns.mac ?? true} onCheckedChange={() => toggleColumn("mac")}>
              MAC
            </DropdownMenuCheckboxItem>
            <DropdownMenuCheckboxItem checked={columns.os ?? true} onCheckedChange={() => toggleColumn("os")}>
              OS
            </DropdownMenuCheckboxItem>
            <DropdownMenuCheckboxItem checked={columns.parent ?? true} onCheckedChange={() => toggleColumn("parent")}>
              Parent host
            </DropdownMenuCheckboxItem>
            <DropdownMenuCheckboxItem checked={columns.env ?? true} onCheckedChange={() => toggleColumn("env")}>
              Environment
            </DropdownMenuCheckboxItem>
          </DropdownMenuContent>
        </DropdownMenu>

        {/* Saved views */}
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="outline" size="sm">
              <BookmarkIcon className="size-4" aria-hidden />Views
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-56">
            <DropdownMenuLabel>Saved views</DropdownMenuLabel>
            <DropdownMenuSeparator />
            {Object.keys(views).length === 0 ? (
              <p className="px-2 py-2 text-xs text-muted-foreground">No saved views on this page yet.</p>
            ) : (
              Object.keys(views).map((name) => (
                <DropdownMenuItem key={name} onClick={() => applyView(name)}>
                  <span className="flex-1">{name}</span>
                  <button
                    type="button"
                    className="text-muted-foreground hover:text-destructive"
                    aria-label={`Delete view ${name}`}
                    onClick={(e) => {
                      e.stopPropagation()
                      deleteView(name)
                    }}
                  >
                    <Trash2Icon className="size-3.5" aria-hidden />
                  </button>
                </DropdownMenuItem>
              ))
            )}
            <DropdownMenuSeparator />
            <DropdownMenuItem onClick={saveCurrentView}>
              <BookmarkPlusIcon className="size-4" aria-hidden />Save current filters
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>

      {/* Bulk action bar */}
      {selected.size > 0 ? (
        <div className="mb-3 flex flex-wrap items-center gap-2 rounded-md border border-primary/30 bg-primary/5 px-3 py-2 text-sm">
          <span className="font-medium">{selected.size} selected</span>
          <div className="flex-1" />
          <Button variant="outline" size="sm" onClick={() => runBulk("set_status", { status: "offline" })}>
            Mark offline
          </Button>
          <Button variant="outline" size="sm" onClick={() => runBulk("set_status", { status: "online" })}>
            Mark online
          </Button>
          <ConfirmDialog
            trigger={
              <Button variant="destructive" size="sm">
                <Trash2Icon className="size-4" aria-hidden />Delete
              </Button>
            }
            title={`Delete ${selected.size} asset(s)?`}
            description={
              anyHostWithChildrenSelected
                ? "Some selected assets host other assets — those will be blocked until moved."
                : "This permanently removes the selected assets and their interfaces. Relationships are cleaned up."
            }
            confirmLabel="Delete selected"
            onConfirm={() => {
              if (anyHostWithChildrenSelected) {
                toast.error("Selection includes hosts with child assets. Remove those guests first.")
                return
              }
              runBulk("delete")
            }}
          />
        </div>
      ) : null}

      {/* Table */}
      {isLoading ? (
        <div className="space-y-2">{Array.from({ length: 6 }).map((_, i) => (
          <div key={i} className="h-10 animate-pulse rounded-md bg-accent" />
        ))}</div>
      ) : assets.length === 0 ? (
        <EmptyState
          icon={MonitorIcon}
          title={q || status || environment || locationId ? "No assets match these filters" : "No assets registered yet"}
          description={
            q || status || environment || locationId
              ? "Try clearing a filter or changing the search."
              : "Register your first physical device — VMs and containers attach to it afterwards."
          }
        />
      ) : (
        <div className="overflow-hidden rounded-lg border">
          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead className="w-10">
                  <Checkbox
                    aria-label="Select all on page"
                    checked={allPageSelected || (assets.some((a) => selected.has(a.id)) ? "indeterminate" : false)}
                    onCheckedChange={toggleAll}
                  />
                </TableHead>
                <SortableHead label="Name" param="name" sort={sort} order={order} onSort={setParam} />
                <TableHead>Type</TableHead>
                {(columns.primary_ip ?? true) ? <TableHead>Primary IP</TableHead> : null}
                {(columns.mac ?? true) ? <TableHead>MAC</TableHead> : null}
                {(columns.os ?? true) ? <TableHead>OS</TableHead> : null}
                {(columns.parent ?? true) ? <TableHead>Parent host</TableHead> : null}
                <SortableHead label="Status" param="status" sort={sort} order={order} onSort={setParam} />
                {(columns.env ?? true) ? <TableHead>Env</TableHead> : null}
                <SortableHead label="Updated" param="updated_at" sort={sort} order={order} onSort={setParam} />
                <TableHead className="w-10" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {assets.map((asset) => (
                <TableRow key={asset.id}>
                  <TableCell>
                    <Checkbox
                      aria-label={`Select ${asset.name}`}
                      checked={selected.has(asset.id)}
                      onCheckedChange={() => toggleOne(asset.id)}
                    />
                  </TableCell>
                  <TableCell>
                    <Link
                      to={`/assets/${asset.id}`}
                      className="font-medium hover:underline underline-offset-2"
                    >
                      {asset.name}
                    </Link>
                    {asset.hostname ? (
                      <p className="font-mono text-xs text-muted-foreground">{asset.hostname}</p>
                    ) : null}
                  </TableCell>
                  <TableCell className="text-muted-foreground">
                    {asset.type_label}
                    {asset.child_count > 0 ? (
                      <span className="ml-1.5 rounded bg-muted px-1.5 py-0.5 text-xs tabular-nums">
                        {asset.child_count}
                      </span>
                    ) : null}
                  </TableCell>
                  {(columns.primary_ip ?? true) ? (
                    <TableCell className="font-mono text-xs">{asset.primary_ip ?? "—"}</TableCell>
                  ) : null}
                  {(columns.mac ?? true) ? (
                    <TableCell className="font-mono text-xs">{asset.mac_addresses || "—"}</TableCell>
                  ) : null}
                  {(columns.os ?? true) ? (
                    <TableCell className="text-muted-foreground">
                      {asset.os_name || "—"}
                      {asset.os_version ? <span className="ml-1 text-xs">{asset.os_version}</span> : null}
                    </TableCell>
                  ) : null}
                  {(columns.parent ?? true) ? (
                    <TableCell className="text-muted-foreground">{asset.parent_name ?? "—"}</TableCell>
                  ) : null}
                  <TableCell><StatusBadge status={asset.status} /></TableCell>
                  {(columns.env ?? true) ? (
                    <TableCell className={`text-xs font-medium ${ENVIRONMENT_BADGES[asset.environment] ?? ""}`}>
                      {asset.environment}
                    </TableCell>
                  ) : null}
                  <TableCell className="text-xs text-muted-foreground tabular-nums" title={asset.updated_at}>
                    {formatRelative(asset.updated_at)}
                  </TableCell>
                  <TableCell>
                    <DropdownMenu>
                      <DropdownMenuTrigger asChild>
                        <Button variant="ghost" size="icon" className="size-7" aria-label={`Actions for ${asset.name}`}>
                          <MoreHorizontalIcon className="size-4" aria-hidden />
                        </Button>
                      </DropdownMenuTrigger>
                      <DropdownMenuContent align="end">
                        <DropdownMenuItem onClick={() => navigate(`/assets/${asset.id}`)}>
                          Open details
                        </DropdownMenuItem>
                      </DropdownMenuContent>
                    </DropdownMenu>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}

      {/* Pagination */}
      {total > PAGE_SIZE ? (
        <div className="mt-4 flex items-center justify-between text-sm text-muted-foreground">
          <span className="tabular-nums">
            {offset + 1}–{Math.min(offset + PAGE_SIZE, total)} of {total}
          </span>
          <div className="flex gap-2">
            <Button variant="outline" size="sm" disabled={offset === 0}
              onClick={() => setParam("offset", String(Math.max(0, offset - PAGE_SIZE)))}>
              <ChevronLeftIcon className="size-4" aria-hidden />Previous
            </Button>
            <Button variant="outline" size="sm" disabled={offset + PAGE_SIZE >= total}
              onClick={() => setParam("offset", String(offset + PAGE_SIZE))}>
              Next<ChevronRightIcon className="size-4" aria-hidden />
            </Button>
          </div>
        </div>
      ) : null}
    </>
  )
}

function SortableHead({
  label, param, sort, order, onSort,
}: {
  label: string
  param: string
  sort: string
  order: string
  onSort: (key: string, value: string) => void
}) {
  const active = sort === param
  const toggleSort = () => {
    onSort("sort", param)
    onSort("order", active && order === "desc" ? "asc" : "desc")
  }
  return (
    <TableHead>
      <button
        type="button"
        className="inline-flex items-center gap-1 uppercase hover:text-foreground"
        onClick={toggleSort}
      >
        {label}
        {active ? (
          order === "desc"
            ? <ChevronDownIcon className="size-3" aria-hidden />
            : <ChevronUpIcon className="size-3" aria-hidden />
        ) : null}
      </button>
    </TableHead>
  )
}
