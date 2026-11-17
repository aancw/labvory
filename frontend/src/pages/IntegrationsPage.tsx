import { useState, type ReactNode } from "react"
import { Link } from "react-router-dom"
import { toast } from "sonner"
import {
  BoxesIcon,
  CloudIcon,
  ContainerIcon,
  EllipsisVerticalIcon,
  GlobeIcon,
  HistoryIcon,
  KeyRoundIcon,
  PencilIcon,
  PlugIcon,
  RadarIcon,
  RadioIcon,
  RefreshCwIcon,
  ServerIcon,
  ShieldCheckIcon,
  Trash2Icon,
} from "lucide-react"

import { isApiError } from "@/api/client"
import {
  useAssets,
  useCreateIntegration,
  useDomains,
  useDeleteIntegration,
  useIntegrations,
  useSyncIntegration,
  useSyncRuns,
  useUpdateIntegration,
} from "@/api/hooks"
import type { Integration } from "@/api/types"
import { AssetPicker } from "@/components/AssetPicker"
import { ConfirmDialog } from "@/components/ConfirmDialog"
import { EmptyState } from "@/components/EmptyState"
import { PageHeader } from "@/components/PageHeader"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { cn, formatRelative } from "@/lib/utils"

const KIND_META: Record<string, { label: string; icon: typeof ServerIcon; description: string }> = {
  proxmox: { label: "Proxmox VE", icon: ServerIcon, description: "Read-only API token: imports nodes, VMs and LXC guests with source attribution." },
  docker: { label: "Docker Engine", icon: ContainerIcon, description: "Discovers containers on one Docker host via the Engine API (unix:// or tcp://)." },
  icmp: { label: "ICMP reachability", icon: RadioIcon, description: "Opt-in ping checks against an explicit asset allowlist. Results are observed state only." },
  dns: { label: "DNS enumeration", icon: GlobeIcon, description: "Enumerates a domain's records (AXFR zone transfer for internal zones, crt.name for public ones) and stores them as observed state." },
  cloudflare: { label: "Cloudflare", icon: CloudIcon, description: "Imports the account's zones as domains and syncs their DNS records. Needs an API token with Zone:Read + DNS:Read." },
  kubernetes: { label: "Kubernetes", icon: BoxesIcon, description: "Imports cluster nodes and pods as assets and Services into the service registry. Uses kubeconfig, an API token, or the in-cluster service account." },
  adguard: { label: "AdGuard Home", icon: ShieldCheckIcon, description: "Imports DNS rewrites for local addresses as observed records (declared values are never overwritten) and marks client IPs as seen on matching IP allocations." },
  arp: { label: "ARP scan", icon: RadarIcon, description: "Sweeps the active interface's subnet in-process (gopacket) — discovered devices are imported automatically as Discovered Device assets and their IPs registered (subnet auto-created when none matches). Table mode only reads the local ARP cache." },
  opnsense: { label: "OPNsense", icon: KeyRoundIcon, description: "Imports DHCP lease state over the REST API: matching IP allocations are marked as observed with the lease hostname and MAC recorded under observed metadata. Read-only — declared values are never overwritten and no assets are created." },
}

export function IntegrationsPage() {
  const { data, isLoading } = useIntegrations()
  const sync = useSyncIntegration()
  const remove = useDeleteIntegration()
  const [editing, setEditing] = useState<Integration | null>(null)
  const [creatingKind, setCreatingKind] = useState<string | null>(null)
  const [historyFor, setHistoryFor] = useState<Integration | null>(null)
  const [deleting, setDeleting] = useState<Integration | null>(null)

  if (isLoading) {
    return <div className="h-64 animate-pulse rounded-lg bg-accent" />
  }

  const integrations = data?.integrations ?? []
  const implemented = data?.implemented ?? []
  const planned = data?.planned ?? []

  return (
    <>
      <PageHeader
        title="Integrations"
        description="Read-only discovery connectors. Syncs never delete manually maintained assets and never overwrite operator-declared fields."
      />

      {!data?.secrets_ready ? (
        <div className="mb-4 rounded-md border border-amber-500/30 bg-amber-500/10 px-3 py-2.5 text-sm text-amber-700 dark:text-amber-400">
          <KeyRoundIcon className="mr-1.5 inline size-4" aria-hidden />
          <span className="font-medium">LV_SECRET_KEY is not configured.</span> Integration secrets cannot be stored
          (and therefore credential-based syncs cannot run) until the server has one. Set it in{" "}
          <code className="font-mono text-xs">deploy/.env</code> and restart the stack.
        </div>
      ) : null}

      {integrations.length === 0 ? (
        <EmptyState
          icon={PlugIcon}
          title="No integrations configured"
          description="Add a connector below. Discovery is always opt-in per connector."
        />
      ) : (
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 2xl:grid-cols-5">
          {integrations.map((integ) => (
            <IntegrationCard
              key={integ.id}
              integ={integ}
              onEdit={() => setEditing(integ)}
              onSync={() =>
                sync.mutate(integ.id, {
                  onSuccess: (res) => toast.success(`Sync complete: ${JSON.stringify(res.summary)}`),
                  onError: (err) => toast.error(isApiError(err) ? err.message : "Sync failed."),
                })
              }
              onDelete={() => setDeleting(integ)}
              onHistory={() => setHistoryFor(integ)}
              // isPending is shared across the page's single mutation instance —
              // key it to the clicked integration so only its card shows progress.
              syncing={sync.isPending && sync.variables === integ.id}
            />
          ))}
        </div>
      )}

      {/* Add connector */}
      <Card className="mt-6">
        <CardHeader><CardTitle className="text-sm">Add a connector</CardTitle></CardHeader>
        <CardContent>
          <div className="grid gap-3 sm:grid-cols-3">
            {implemented.map((kind) => {
              const KindIcon = KIND_META[kind]?.icon ?? PlugIcon
              const kindMeta = KIND_META[kind]
              return (
                <button
                  key={kind}
                  type="button"
                  onClick={() => setCreatingKind(kind)}
                  className="flex items-start gap-3 rounded-lg border p-3 text-left transition-colors hover:border-primary/40 hover:bg-accent/40"
                >
                  <KindIcon className="mt-0.5 size-5 shrink-0 text-muted-foreground" aria-hidden />
                  <span>
                    <span className="block text-sm font-medium">{kindMeta?.label ?? kind}</span>
                    <span className="mt-0.5 block text-xs text-muted-foreground">{kindMeta?.description}</span>
                  </span>
                </button>
              )
            })}
          </div>
          <p className="mt-4 text-xs text-muted-foreground">
            Designed but not implemented yet: {planned.join(", ")}. The schema and UI accept them as disabled entries —
            no fake functionality.
          </p>
        </CardContent>
      </Card>

      <IntegrationDialog
        creatingKind={creatingKind}
        editing={editing}
        onClose={() => {
          setCreatingKind(null)
          setEditing(null)
        }}
      />

      <SyncHistoryDialog integ={historyFor} onClose={() => setHistoryFor(null)} />

      <ConfirmDialog
        open={!!deleting}
        onOpenChange={(v) => { if (!v) setDeleting(null) }}
        title={deleting ? `Delete ${deleting.name}?` : "Delete integration?"}
        description="Imported assets stay in the inventory — only the connector and its sync history are removed."
        confirmLabel="Delete connector"
        onConfirm={() => {
          if (!deleting) return
          remove.mutate(deleting.id, {
            onError: (err) => toast.error(isApiError(err) ? err.message : "Delete failed."),
          })
        }}
      />
    </>
  )
}

// Auto-sync schedule options (seconds; mirrors the API's 300…604800 bound).
const SYNC_INTERVAL_OPTIONS = [
  { value: "", label: "Manual only" },
  { value: "900", label: "Every 15 minutes" },
  { value: "3600", label: "Hourly" },
  { value: "21600", label: "Every 6 hours" },
  { value: "86400", label: "Daily" },
  { value: "604800", label: "Weekly" },
]

function formatSyncInterval(seconds: number | null | undefined): string {
  if (!seconds) return ""
  if (seconds % 604800 === 0) return `auto every ${seconds / 604800}w`
  if (seconds % 86400 === 0) return `auto every ${seconds / 86400}d`
  if (seconds % 3600 === 0) return `auto every ${seconds / 3600}h`
  return `auto every ${Math.round(seconds / 60)}m`
}

// Config values that identify a connector's source, in display priority order.
// Everything empty/false is skipped — the card shows facts, not a raw config dump.
const CONFIG_KEY_ORDER = [
  "endpoint", "host", "server", "api_server",
  "mode", "interface", "cidr",
  "username", "token_id", "context", "kubeconfig",
] as const

function configChips(integ: Integration): { title: string; node: ReactNode }[] {
  const cfg = integ.config
  const chips: { title: string; node: ReactNode }[] = []
  const add = (title: string, node: ReactNode) => chips.push({ title, node })

  for (const key of CONFIG_KEY_ORDER) {
    const value = cfg[key]
    if (typeof value === "string" && value.trim()) {
      add(`${key}: ${value.trim()}`, <span className="font-mono">{value.trim()}</span>)
    }
  }
  if (typeof cfg.asset_id === "string" && cfg.asset_id) {
    add(
      "host asset",
      <Link to={`/assets/${cfg.asset_id}`} className="font-mono underline-offset-2 hover:underline">host asset</Link>,
    )
  }
  if (Array.isArray(cfg.zone_names) && cfg.zone_names.length > 0) {
    const n = cfg.zone_names.length
    add("zone_names", <span className="font-mono">{n} zone{n === 1 ? "" : "s"}</span>)
  }
  if (Array.isArray(cfg.asset_ids) && cfg.asset_ids.length > 0) {
    const n = cfg.asset_ids.length
    add("scope", <span className="font-mono">{n} asset{n === 1 ? "" : "s"}</span>)
  }
  if (cfg.in_cluster === true) {
    add("in_cluster", <span className="font-mono">in-cluster</span>)
  }
  if (integ.sync_interval_seconds) {
    add("sync_interval_seconds", <span className="font-mono">{formatSyncInterval(integ.sync_interval_seconds)}</span>)
  }
  const secrets = Object.entries(cfg.secrets_configured ?? {})
  if (secrets.length > 0) {
    const allSet = secrets.every(([, v]) => v)
    add(
      `secrets: ${secrets.map(([k, v]) => `${k} ${v ? "configured" : "missing"}`).join(", ")}`,
      <span className={cn("flex items-center gap-1", allSet ? "text-emerald-600 dark:text-emerald-400" : "text-amber-600 dark:text-amber-400")}>
        <KeyRoundIcon className="size-3" aria-hidden />
        {allSet ? "secrets" : "secret missing"}
      </span>,
    )
  }
  return chips
}

function IntegrationCard({
  integ, onEdit, onSync, onDelete, onHistory, syncing,
}: {
  integ: Integration
  onEdit: () => void
  onSync: () => void
  onDelete: () => void
  onHistory: () => void
  syncing: boolean
}) {
  const meta = KIND_META[integ.kind]
  const Icon = meta?.icon ?? PlugIcon
  const chips = configChips(integ)

  return (
    <Card className="gap-3 p-4">
      <div className="flex items-start gap-2.5">
        <div className="flex size-8 shrink-0 items-center justify-center rounded-md border bg-muted/40">
          <Icon className="size-4 text-muted-foreground" aria-hidden />
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-1.5">
            <CardTitle className="truncate text-sm leading-tight">{integ.name}</CardTitle>
            {integ.enabled ? (
              <Badge className="shrink-0 border-transparent bg-emerald-500/10 px-1.5 py-0 text-[10px] text-emerald-600 dark:text-emerald-400">
                enabled
              </Badge>
            ) : (
              <Badge variant="outline" className="shrink-0 px-1.5 py-0 text-[10px]">disabled</Badge>
            )}
          </div>
          <p className="mt-1 truncate text-xs text-muted-foreground">{meta?.label ?? integ.kind}</p>
        </div>
      </div>

      {chips.length > 0 ? (
        <div className="flex flex-wrap gap-1">
          {chips.map((chip, i) => (
            <span
              key={i}
              title={chip.title}
              className="inline-flex max-w-full items-center gap-1 rounded-md border bg-muted/30 px-1.5 py-0.5 text-[11px] leading-tight text-muted-foreground"
            >
              <span className="truncate">{chip.node}</span>
            </span>
          ))}
        </div>
      ) : null}

      <div className="mt-auto flex items-center justify-between gap-2 border-t pt-3">
        <span className="truncate text-xs text-muted-foreground">
          {integ.last_sync_at ? formatRelative(integ.last_sync_at) : "never synced"}
        </span>
        <div className="flex shrink-0 items-center gap-1">
          <Button
            variant="outline"
            size="sm"
            className="h-7 gap-1.5 px-2.5 text-xs"
            onClick={onSync}
            disabled={syncing || !integ.implemented || !integ.enabled}
          >
            <RefreshCwIcon className={cn(syncing ? "animate-spin" : "", "size-3.5")} aria-hidden />
            {syncing ? "Syncing…" : "Sync"}
          </Button>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="ghost" size="icon" className="size-7" aria-label={`More actions for ${integ.name}`}>
                <EllipsisVerticalIcon className="size-4" aria-hidden />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-44">
              <DropdownMenuItem onClick={onEdit}>
                <PencilIcon className="size-3.5" aria-hidden />Edit
              </DropdownMenuItem>
              <DropdownMenuItem onClick={onHistory}>
                <HistoryIcon className="size-3.5" aria-hidden />Sync history
              </DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuItem onClick={onDelete} className="text-destructive focus:text-destructive">
                <Trash2Icon className="size-3.5" aria-hidden />Delete
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </div>
    </Card>
  )
}

function SyncHistoryDialog({ integ, onClose }: { integ: Integration | null; onClose: () => void }) {
  return (
    <Dialog open={!!integ} onOpenChange={(v) => !v && onClose()}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>{integ ? `${integ.name} — sync history` : "Sync history"}</DialogTitle>
          <DialogDescription>The most recent runs for this connector.</DialogDescription>
        </DialogHeader>
        {integ ? <SyncHistory integrationId={integ.id} /> : null}
      </DialogContent>
    </Dialog>
  )
}

function SyncHistory({ integrationId }: { integrationId: string }) {
  const { data, isLoading } = useSyncRuns(integrationId, true)
  if (isLoading) return <p className="text-xs text-muted-foreground">Loading…</p>
  const runs = data?.runs ?? []
  if (runs.length === 0) {
    return <p className="text-xs text-muted-foreground">No sync runs yet.</p>
  }
  return (
    <ul className="max-h-80 space-y-1.5 overflow-y-auto">
      {runs.map((run) => (
        <li key={run.id} className="flex items-center gap-2.5 rounded border px-2.5 py-1.5 text-xs">
          <span
            className={
              run.status === "success"
                ? "text-emerald-600 dark:text-emerald-400"
                : run.status === "failed"
                  ? "text-destructive"
                  : "text-muted-foreground"
            }
          >
            ●
          </span>
          <span className="font-medium capitalize">{run.status}</span>
          <span className="min-w-0 flex-1 truncate font-mono text-muted-foreground" title={run.message}>
            {run.message || JSON.stringify(run.summary ?? {})}
          </span>
          <span className="shrink-0 text-muted-foreground">{formatRelative(run.started_at)}</span>
        </li>
      ))}
    </ul>
  )
}

function IntegrationDialog({
  creatingKind, editing, onClose,
}: {
  creatingKind: string | null
  editing: Integration | null
  onClose: () => void
}) {
  const kind = creatingKind ?? editing?.kind ?? ""
  const meta = KIND_META[kind]
  const create = useCreateIntegration()
  const update = useUpdateIntegration()

  const [name, setName] = useState("")
  const [description, setDescription] = useState("")
  const [endpoint, setEndpoint] = useState("")
  const [tokenId, setTokenId] = useState("")
  const [tokenSecret, setTokenSecret] = useState("")
  const [verifyTLS, setVerifyTLS] = useState(false)
  const [dockerHost, setDockerHost] = useState("")
  const [dockerAsset, setDockerAsset] = useState<string | null>(null)
  const [icmpAssets, setIcmpAssets] = useState<string[]>([])
  const [dnsDomainId, setDnsDomainId] = useState("")
  const [dnsMode, setDnsMode] = useState<"axfr" | "crtname">("axfr")
  const [dnsServer, setDnsServer] = useState("")
  const [cfToken, setCfToken] = useState("")
  const [cfZones, setCfZones] = useState("")
  const [k8sKubeconfig, setK8sKubeconfig] = useState("")
  const [k8sContext, setK8sContext] = useState("")
  const [k8sAPIServer, setK8sAPIServer] = useState("")
  const [k8sInCluster, setK8sInCluster] = useState(false)
  const [k8sToken, setK8sToken] = useState("")
  const [adguardUsername, setAdguardUsername] = useState("")
  const [adguardPassword, setAdguardPassword] = useState("")
  const [opnsenseKey, setOpnsenseKey] = useState("")
  const [opnsenseSecret, setOpnsenseSecret] = useState("")
  const [arpMode, setArpMode] = useState<"table" | "scan">("scan")
  const [arpInterface, setArpInterface] = useState("")
  const [arpCidr, setArpCidr] = useState("")
  const [syncInterval, setSyncInterval] = useState("")
  const [enabled, setEnabled] = useState(true)
  const [loadedFor, setLoadedFor] = useState<string | null>(null)

  const { data: assetData } = useAssets({ limit: 100, sort: "name", order: "asc" })
  const { data: domainData } = useDomains()

  // Load values when the dialog opens for an existing integration.
  const openKey = editing ? `edit-${editing.id}` : creatingKind ? `create-${creatingKind}` : ""
  if (openKey && loadedFor !== openKey) {
    setLoadedFor(openKey)
    if (editing) {
      setName(editing.name)
      setDescription(editing.description)
      setEndpoint(editing.config.endpoint ?? "")
      setTokenId(editing.config.token_id ?? "")
      setVerifyTLS(editing.config.verify_tls ?? false)
      setDockerHost(editing.config.host ?? "")
      setDnsDomainId((editing.config.domain_id as string) ?? "")
      setDnsMode(((editing.config.mode as "axfr" | "crtname") ?? "axfr"))
      setDnsServer((editing.config.server as string) ?? "")
      setCfZones(((editing.config.zone_names as string[]) ?? []).join(", "))
      setK8sKubeconfig((editing.config.kubeconfig as string) ?? "")
      setK8sContext((editing.config.context as string) ?? "")
      setK8sAPIServer((editing.config.api_server as string) ?? "")
      setK8sInCluster(Boolean(editing.config.in_cluster))
      setAdguardUsername((editing.config.username as string) ?? "")
      setAdguardPassword("")
      setOpnsenseKey("")
      setOpnsenseSecret("")
      setArpMode(((editing.config.mode as "table" | "scan") ?? "table"))
      setArpInterface((editing.config.interface as string) ?? "")
      setArpCidr((editing.config.cidr as string) ?? "")
      setSyncInterval(editing.sync_interval_seconds ? String(editing.sync_interval_seconds) : "")
      setEnabled(editing.enabled)
    } else {
      setName("")
      setDescription("")
      setEndpoint("")
      setTokenId("")
      setTokenSecret("")
      setVerifyTLS(false)
      setDockerHost("")
      setDockerAsset(null)
      setIcmpAssets([])
      setDnsDomainId("")
      setDnsMode("axfr")
      setDnsServer("")
      setCfToken("")
      setCfZones("")
      setK8sKubeconfig("")
      setK8sContext("")
      setK8sAPIServer("")
      setK8sInCluster(false)
      setK8sToken("")
      setAdguardUsername("")
      setAdguardPassword("")
      setOpnsenseKey("")
      setOpnsenseSecret("")
      setArpMode("table")
      setArpInterface("")
      setArpCidr("")
      setSyncInterval("")
      setEnabled(true)
    }
  }
  if (!openKey && loadedFor) setLoadedFor(null)

  const submit = () => {
    if (kind === "proxmox" && (!endpoint.trim() || !tokenId.trim())) {
      toast.error("Endpoint and token ID are required.")
      return
    }
    if (kind === "docker" && !dockerHost.trim()) {
      toast.error("Docker host is required (unix:// or tcp://).")
      return
    }
    if (kind === "icmp" && icmpAssets.length === 0) {
      toast.error("Select at least one asset for the ICMP scope (opt-in by design).")
      return
    }
    if (kind === "dns" && !dnsDomainId) {
      toast.error("Pick the domain to enumerate (create it under Domains first).")
      return
    }
    if (kind === "dns" && dnsMode === "axfr" && !dnsServer.trim()) {
      toast.error("AXFR needs the nameserver address (host:port).")
      return
    }
    if (kind === "cloudflare" && !editing && !cfToken.trim()) {
      toast.error("A Cloudflare API token is required.")
      return
    }
    if (kind === "kubernetes" && !k8sKubeconfig.trim() && !k8sAPIServer.trim() && !k8sInCluster) {
      toast.error("Pick one auth mode: kubeconfig path, API server (+ token), or in-cluster.")
      return
    }
    if (kind === "adguard") {
      if (!endpoint.trim()) {
        toast.error("The AdGuard endpoint is required (e.g. http://192.168.1.11:80).")
        return
      }
      if (!adguardUsername.trim()) {
        toast.error("The AdGuard username is required.")
        return
      }
      if (!editing && !adguardPassword) {
        toast.error("The AdGuard password is required.")
        return
      }
    }

    if (kind === "opnsense") {
      if (!endpoint.trim()) {
        toast.error("The OPNsense endpoint is required (e.g. https://opnsense.lan).")
        return
      }
      if (!editing && (!opnsenseKey.trim() || !opnsenseSecret.trim())) {
        toast.error("An OPNsense API key pair (key + secret) is required.")
        return
      }
    }

    const config: Record<string, unknown> =
      kind === "proxmox"
        ? { endpoint: endpoint.trim(), token_id: tokenId.trim(), verify_tls: verifyTLS }
        : kind === "docker"
          ? { host: dockerHost.trim(), asset_id: dockerAsset ?? "" }
          : kind === "dns"
            ? { domain_id: dnsDomainId, mode: dnsMode, server: dnsServer.trim() }
            : kind === "cloudflare"
              ? { zone_names: cfZones.split(",").map((z) => z.trim()).filter(Boolean) }
              : kind === "kubernetes"
                ? {
                    kubeconfig: k8sKubeconfig.trim(),
                    context: k8sContext.trim(),
                    api_server: k8sAPIServer.trim(),
                    in_cluster: k8sInCluster,
                    verify_tls: true,
                  }
                : kind === "adguard"
                  ? { endpoint: endpoint.trim(), username: adguardUsername.trim(), verify_tls: verifyTLS }
                  : kind === "opnsense"
                    ? { endpoint: endpoint.trim(), verify_tls: verifyTLS }
                    : kind === "arp"
                      ? { mode: arpMode, interface: arpInterface.trim(), cidr: arpCidr.trim() }
                      : { asset_ids: icmpAssets, timeout_seconds: 2 }
    const secrets: Record<string, string> =
      kind === "proxmox" && tokenSecret
        ? { token_secret: tokenSecret }
        : kind === "cloudflare" && cfToken
          ? { api_token: cfToken }
          : kind === "kubernetes" && k8sToken
            ? { token: k8sToken }
            : kind === "adguard" && adguardPassword
              ? { password: adguardPassword }
              : kind === "opnsense" && (opnsenseKey || opnsenseSecret)
                ? Object.fromEntries(
                    [
                      ["api_key", opnsenseKey],
                      ["api_secret", opnsenseSecret],
                    ].filter(([, v]) => v),
                  )
                : {}

    const intervalSeconds = syncInterval === "" ? null : Number(syncInterval)

    if (editing) {
      update.mutate(
        { id: editing.id, payload: { name: name.trim(), description: description.trim(), enabled, config, secrets, sync_interval_seconds: intervalSeconds } },
        {
          onSuccess: () => {
            toast.success("Integration updated.")
            onClose()
          },
          onError: (err) => toast.error(isApiError(err) ? err.message : "Update failed."),
        },
      )
    } else {
      create.mutate(
        { kind, name: name.trim(), description: description.trim(), enabled, config, secrets, sync_interval_seconds: intervalSeconds },
        {
          onSuccess: () => {
            toast.success("Integration created.")
            onClose()
          },
          onError: (err) => toast.error(isApiError(err) ? err.message : "Create failed."),
        },
      )
    }
  }

  const busy = create.isPending || update.isPending

  return (
    <Dialog open={!!openKey} onOpenChange={(v) => !v && onClose()}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>
            {editing ? `Edit ${editing.name}` : `Add ${meta?.label ?? kind} connector`}
          </DialogTitle>
          <DialogDescription>{meta?.description}</DialogDescription>
        </DialogHeader>

        <div className="space-y-3">
          <div className="grid gap-3 sm:grid-cols-2">
            <div className="space-y-1.5">
              <Label htmlFor="int-name">Name *</Label>
              <Input id="int-name" value={name} onChange={(e) => setName(e.target.value)} autoFocus />
            </div>
            <div className="flex items-end gap-2 pb-1">
              <input
                id="int-enabled"
                type="checkbox"
                checked={enabled}
                onChange={(e) => setEnabled(e.target.checked)}
                className="size-4"
              />
              <Label htmlFor="int-enabled" className="cursor-pointer">Enabled</Label>
            </div>
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="int-sync-interval">Auto-sync schedule</Label>
            <select
              id="int-sync-interval"
              className="h-9 w-full rounded-md border border-input bg-transparent px-2 text-sm"
              value={syncInterval}
              onChange={(e) => setSyncInterval(e.target.value)}
            >
              {SYNC_INTERVAL_OPTIONS.map((opt) => (
                <option key={opt.value} value={opt.value}>{opt.label}</option>
              ))}
            </select>
            <p className="text-xs text-muted-foreground">
              The server runs the sync in the background when the interval elapses. Manual syncs stay available.
            </p>
          </div>

          {kind === "proxmox" ? (
            <>
              <div className="space-y-1.5">
                <Label htmlFor="int-endpoint">API endpoint *</Label>
                <Input id="int-endpoint" className="font-mono text-xs" placeholder="https://pve.local:8006"
                  value={endpoint} onChange={(e) => setEndpoint(e.target.value)} />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="int-token">API token ID *</Label>
                <Input id="int-token" className="font-mono text-xs" placeholder="root@pam!lanvory"
                  value={tokenId} onChange={(e) => setTokenId(e.target.value)} />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="int-secret">API token secret</Label>
                <Input id="int-secret" type="password" className="font-mono text-xs"
                  placeholder={editing?.config.secrets_configured?.token_secret ? "configured — leave blank to keep" : "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"}
                  value={tokenSecret} onChange={(e) => setTokenSecret(e.target.value)} />
                <p className="text-xs text-muted-foreground">
                  Create a read-only API token in Proxmox (Datacenter → Permissions → API Tokens).
                  Stored encrypted with AES-256-GCM, never returned by the API.
                </p>
              </div>
              <label className="flex items-center gap-2 text-sm">
                <input type="checkbox" checked={verifyTLS} onChange={(e) => setVerifyTLS(e.target.checked)} className="size-4" />
                Verify TLS certificate (disable for self-signed clusters)
              </label>
            </>
          ) : null}

          {kind === "docker" ? (
            <>
              <div className="space-y-1.5">
                <Label htmlFor="int-docker">Docker host *</Label>
                <Input id="int-docker" className="font-mono text-xs" placeholder="unix:///var/run/docker.sock or tcp://host:2375"
                  value={dockerHost} onChange={(e) => setDockerHost(e.target.value)} />
              </div>
              <div className="space-y-1.5">
                <Label>Host asset *</Label>
                <AssetPicker
                  value={dockerAsset}
                  onChange={(a) => setDockerAsset(a?.id ?? null)}
                  placeholder="Host that runs these containers…"
                  listOnFocus
                />
                <p className="text-xs text-muted-foreground">Discovered containers become child assets of this host.</p>
              </div>
            </>
          ) : null}

          {kind === "dns" ? (
            <>
              <div className="space-y-1.5">
                <Label htmlFor="int-dns-domain">Domain *</Label>
                <select
                  id="int-dns-domain"
                  className="h-9 w-full rounded-md border border-input bg-transparent px-2 text-sm"
                  value={dnsDomainId}
                  onChange={(e) => setDnsDomainId(e.target.value)}
                >
                  <option value="">Select domain…</option>
                  {(domainData?.domains ?? []).map((d) => (
                    <option key={d.id} value={d.id}>{d.name}</option>
                  ))}
                </select>
                <p className="text-xs text-muted-foreground">Add the zone under Domains first; the sync fills its records.</p>
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="int-dns-mode">Mode</Label>
                <select
                  id="int-dns-mode"
                  className="h-9 w-full rounded-md border border-input bg-transparent px-2 text-sm"
                  value={dnsMode}
                  onChange={(e) => setDnsMode(e.target.value as "axfr" | "crtname")}
                >
                  <option value="axfr">AXFR — zone transfer (internal zones)</option>
                  <option value="crtname">crt.name — certificate transparency (public zones)</option>
                </select>
              </div>
              {dnsMode === "axfr" ? (
                <div className="space-y-1.5">
                  <Label htmlFor="int-dns-server">Nameserver *</Label>
                  <Input id="int-dns-server" className="font-mono text-xs" placeholder="192.168.1.1:53"
                    value={dnsServer} onChange={(e) => setDnsServer(e.target.value)} />
                  <p className="text-xs text-muted-foreground">
                    The server must allow zone transfers from this host (BIND allow-transfer, dnsmasq auth-zone,
                    PowerDNS allow-axfr-ips, Technitium).
                  </p>
                </div>
              ) : null}
            </>
          ) : null}

          {kind === "cloudflare" ? (
            <>
              <div className="space-y-1.5">
                <Label htmlFor="int-cf-token">API token *</Label>
                <Input id="int-cf-token" type="password" className="font-mono text-xs"
                  placeholder={editing?.config.secrets_configured?.api_token ? "configured — leave blank to keep" : "Cloudflare API token"}
                  value={cfToken} onChange={(e) => setCfToken(e.target.value)} />
                <p className="text-xs text-muted-foreground">
                  Create a token in Cloudflare (My Profile → API Tokens) with <span className="font-mono">Zone:Read</span> and{" "}
                  <span className="font-mono">DNS:Read</span>. Stored encrypted with AES-256-GCM, never returned by the API.
                </p>
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="int-cf-zones">Zone allowlist</Label>
                <Input id="int-cf-zones" className="font-mono text-xs" placeholder="example.com, lab.example.net (empty = all zones)"
                  value={cfZones} onChange={(e) => setCfZones(e.target.value)} />
                <p className="text-xs text-muted-foreground">
                  Zones become domains automatically; their DNS records sync as observed state. Declared values are never overwritten.
                </p>
              </div>
            </>
          ) : null}

          {kind === "kubernetes" ? (
            <>
              <div className="space-y-1.5">
                <Label htmlFor="int-k8s-kubeconfig">Kubeconfig path</Label>
                <Input id="int-k8s-kubeconfig" className="font-mono text-xs" placeholder="/home/you/.kube/config"
                  value={k8sKubeconfig} onChange={(e) => setK8sKubeconfig(e.target.value)} />
                <p className="text-xs text-muted-foreground">
                  Read server-side. Leave empty when using an API token or in-cluster auth.
                </p>
              </div>
              <div className="grid gap-3 sm:grid-cols-2">
                <div className="space-y-1.5">
                  <Label htmlFor="int-k8s-context">Context</Label>
                  <Input id="int-k8s-context" placeholder="current context" value={k8sContext}
                    onChange={(e) => setK8sContext(e.target.value)} />
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="int-k8s-server">API server</Label>
                  <Input id="int-k8s-server" className="font-mono text-xs" placeholder="https://10.0.0.5:6443"
                    value={k8sAPIServer} onChange={(e) => setK8sAPIServer(e.target.value)} />
                </div>
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="int-k8s-token">Bearer token</Label>
                <Input id="int-k8s-token" type="password" className="font-mono text-xs"
                  placeholder={editing?.config.secrets_configured?.token ? "configured — leave blank to keep" : "optional (needed with API server mode)"}
                  value={k8sToken} onChange={(e) => setK8sToken(e.target.value)} />
                <p className="text-xs text-muted-foreground">
                  Needs <span className="font-mono">list</span> on nodes, pods and services. Stored encrypted with AES-256-GCM.
                </p>
              </div>
              <label className="flex items-center gap-2 text-sm">
                <input type="checkbox" checked={k8sInCluster} onChange={(e) => setK8sInCluster(e.target.checked)} className="size-4" />
                Use the in-cluster service account (when Lanvory runs inside the cluster)
              </label>
            </>
          ) : null}

          {kind === "adguard" ? (
            <>
              <div className="space-y-1.5">
                <Label htmlFor="int-adguard-endpoint">Endpoint *</Label>
                <Input id="int-adguard-endpoint" className="font-mono text-xs" placeholder="http://192.168.1.11:80"
                  value={endpoint} onChange={(e) => setEndpoint(e.target.value)} />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="int-adguard-user">Username *</Label>
                <Input id="int-adguard-user" className="font-mono text-xs" placeholder="admin"
                  value={adguardUsername} onChange={(e) => setAdguardUsername(e.target.value)} />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="int-adguard-pass">Password</Label>
                <Input id="int-adguard-pass" type="password" className="font-mono text-xs"
                  placeholder={editing?.config.secrets_configured?.password ? "configured — leave blank to keep" : "AdGuard login password"}
                  value={adguardPassword} onChange={(e) => setAdguardPassword(e.target.value)} />
                <p className="text-xs text-muted-foreground">
                  The same credentials you use for the AdGuard Home dashboard (basic auth).
                  Stored encrypted with AES-256-GCM, never returned by the API.
                </p>
              </div>
              <label className="flex items-center gap-2 text-sm">
                <input type="checkbox" checked={verifyTLS} onChange={(e) => setVerifyTLS(e.target.checked)} className="size-4" />
                Verify TLS certificate (disable for self-signed instances)
              </label>
              <p className="text-xs text-muted-foreground">
                Rewrites whose answer is a local address (private, loopback, link-local, ULA, Tailscale CGNAT) become
                observed DNS records under auto-created domains; public answers are skipped. Client IPs are recorded as
                observations on matching IP allocations only.
              </p>
            </>
          ) : null}

          {kind === "opnsense" ? (
            <>
              <div className="space-y-1.5">
                <Label htmlFor="int-opnsense-endpoint">Endpoint *</Label>
                <Input id="int-opnsense-endpoint" className="font-mono text-xs" placeholder="https://opnsense.lan"
                  value={endpoint} onChange={(e) => setEndpoint(e.target.value)} />
              </div>
              <div className="grid gap-3 sm:grid-cols-2">
                <div className="space-y-1.5">
                  <Label htmlFor="int-opnsense-key">API key *</Label>
                  <Input id="int-opnsense-key" className="font-mono text-xs"
                    placeholder={editing?.config.secrets_configured?.api_key ? "configured — leave blank to keep" : "key from the API key pair"}
                    value={opnsenseKey} onChange={(e) => setOpnsenseKey(e.target.value)} />
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="int-opnsense-secret">API secret *</Label>
                  <Input id="int-opnsense-secret" type="password" className="font-mono text-xs"
                    placeholder={editing?.config.secrets_configured?.api_secret ? "configured — leave blank to keep" : "secret from the API key pair"}
                    value={opnsenseSecret} onChange={(e) => setOpnsenseSecret(e.target.value)} />
                </div>
              </div>
              <label className="flex items-center gap-2 text-sm">
                <input type="checkbox" checked={verifyTLS} onChange={(e) => setVerifyTLS(e.target.checked)} className="size-4" />
                Verify TLS certificate (disable for self-signed instances)
              </label>
              <p className="text-xs text-muted-foreground">
                Create the key pair under <span className="font-mono">System › Access › Users › API keys</span>. The sync reads the
                DHCP lease list and marks matching IP allocations as observed — the lease hostname and MAC are stored as
                observed metadata, never overwriting declared values.
              </p>
            </>
          ) : null}

          {kind === "arp" ? (
            <>
              <div className="space-y-1.5">
                <Label htmlFor="int-arp-mode">Mode</Label>
                <select
                  id="int-arp-mode"
                  className="h-9 w-full rounded-md border border-input bg-transparent px-2 text-sm"
                  value={arpMode}
                  onChange={(e) => setArpMode(e.target.value as "table" | "scan")}
                >
                  <option value="scan">ARP sweep — active in-process scan (gopacket)</option>
                  <option value="table">ARP table — read the local cache (no privileges)</option>
                </select>
              </div>
              {arpMode === "scan" ? (
                <div className="grid gap-3 sm:grid-cols-2">
                  <div className="space-y-1.5">
                    <Label htmlFor="int-arp-iface">Interface</Label>
                    <Input id="int-arp-iface" className="font-mono text-xs" placeholder="en0 / eth0 (empty = active interface via default route)"
                      value={arpInterface} onChange={(e) => setArpInterface(e.target.value)} />
                  </div>
                  <div className="space-y-1.5">
                    <Label htmlFor="int-arp-cidr">CIDR</Label>
                    <Input id="int-arp-cidr" className="font-mono text-xs" placeholder="192.168.1.0/24 (empty = interface subnet)"
                      value={arpCidr} onChange={(e) => setArpCidr(e.target.value)} />
                  </div>
                  <p className="col-span-2 text-xs text-muted-foreground">
                    The sweep runs in-process (gopacket) — no external binary — and needs raw packet access:
                    Linux <span className="font-mono">NET_RAW</span> (compose grants it — for LAN reachability use the
                    macvlan override <span className="font-mono">docker-compose.arp.yml</span>); macOS needs root or BPF
                    access (<span className="font-mono">sudo chmod g+rw /dev/bpf*</span>). Discovered devices become
                    Discovered Device assets and their IPs are registered automatically (the subnet is auto-created
                    when none matches). In Docker, set the interface to the LAN-facing one (macvlan: <span className="font-mono">eth1</span>).
                  </p>
                </div>
              ) : null}
            </>
          ) : null}

          {kind === "icmp" ? (
            <div className="space-y-2">
              <Label>Scope (explicit allowlist)</Label>
              <div className="max-h-40 space-y-1 overflow-y-auto rounded-md border p-2">
                {(assetData?.assets ?? []).map((a) => (
                  <label key={a.id} className="flex items-center gap-2 text-sm">
                    <input
                      type="checkbox"
                      className="size-4"
                      checked={icmpAssets.includes(a.id)}
                      onChange={(e) =>
                        setIcmpAssets((prev) => (e.target.checked ? [...prev, a.id] : prev.filter((id) => id !== a.id)))
                      }
                    />
                    <span className="font-medium">{a.name}</span>
                    <span className="font-mono text-xs text-muted-foreground">{a.hostname || a.primary_ip || ""}</span>
                  </label>
                ))}
                {(assetData?.assets ?? []).length === 0 ? (
                  <p className="text-xs text-muted-foreground">Register assets first.</p>
                ) : null}
              </div>
              <p className="text-xs text-muted-foreground">
                Results are recorded as observed state — the declared status field is never modified.
              </p>
            </div>
          ) : null}

          <div className="space-y-1.5">
            <Label htmlFor="int-desc">Description</Label>
            <Input id="int-desc" value={description} onChange={(e) => setDescription(e.target.value)} />
          </div>
        </div>

        <DialogFooter>
          <Button variant="outline" size="sm" onClick={onClose}>Cancel</Button>
          <Button size="sm" onClick={submit} disabled={busy || !name.trim()}>
            {busy ? "Saving…" : editing ? "Save changes" : "Create connector"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
