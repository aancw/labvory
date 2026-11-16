import { useState } from "react"
import { Link } from "react-router-dom"
import { toast } from "sonner"
import {
  BoxesIcon,
  CloudIcon,
  ContainerIcon,
  GlobeIcon,
  KeyRoundIcon,
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
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { formatRelative } from "@/lib/utils"

const KIND_META: Record<string, { label: string; icon: typeof ServerIcon; description: string }> = {
  proxmox: { label: "Proxmox VE", icon: ServerIcon, description: "Read-only API token: imports nodes, VMs and LXC guests with source attribution." },
  docker: { label: "Docker Engine", icon: ContainerIcon, description: "Discovers containers on one Docker host via the Engine API (unix:// or tcp://)." },
  icmp: { label: "ICMP reachability", icon: RadioIcon, description: "Opt-in ping checks against an explicit asset allowlist. Results are observed state only." },
  dns: { label: "DNS enumeration", icon: GlobeIcon, description: "Enumerates a domain's records (AXFR zone transfer for internal zones, crt.name for public ones) and stores them as observed state." },
  cloudflare: { label: "Cloudflare", icon: CloudIcon, description: "Imports the account's zones as domains and syncs their DNS records. Needs an API token with Zone:Read + DNS:Read." },
  kubernetes: { label: "Kubernetes", icon: BoxesIcon, description: "Imports cluster nodes and pods as assets and Services into the service registry. Uses kubeconfig, an API token, or the in-cluster service account." },
  adguard: { label: "AdGuard Home", icon: ShieldCheckIcon, description: "Imports DNS rewrites for local addresses as observed records (declared values are never overwritten) and marks client IPs as seen on matching IP allocations." },
  arp: { label: "ARP scan", icon: RadarIcon, description: "Reads Layer-2 neighbors (ip ↔ mac) from the local ARP table or an arp-scan sweep. IPs are marked as seen on matching allocations; MACs matching an asset interface record the sighting on the asset." },
}

export function IntegrationsPage() {
  const { data, isLoading } = useIntegrations()
  const sync = useSyncIntegration()
  const remove = useDeleteIntegration()
  const [editing, setEditing] = useState<Integration | null>(null)
  const [creatingKind, setCreatingKind] = useState<string | null>(null)
  const [historyFor, setHistoryFor] = useState<string | null>(null)

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
        <div className="grid gap-4 lg:grid-cols-2">
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
              onDelete={() =>
                remove.mutate(integ.id, {
                  onError: (err) => toast.error(isApiError(err) ? err.message : "Delete failed."),
                })
              }
              onToggleHistory={() => setHistoryFor(historyFor === integ.id ? null : integ.id)}
              historyOpen={historyFor === integ.id}
              syncing={sync.isPending}
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
    </>
  )
}

function IntegrationCard({
  integ, onEdit, onSync, onDelete, onToggleHistory, historyOpen, syncing,
}: {
  integ: Integration
  onEdit: () => void
  onSync: () => void
  onDelete: () => void
  onToggleHistory: () => void
  historyOpen: boolean
  syncing: boolean
}) {
  const meta = KIND_META[integ.kind]
  return (
    <Card>
      <CardHeader className="flex-row items-start justify-between">
        <div className="flex items-center gap-2.5">
          {meta ? <KindIconFromMeta meta={integ.kind} /> : <PlugIcon className="size-5 text-muted-foreground" aria-hidden />}
          <div>
            <CardTitle className="text-sm flex items-center gap-2">
              {integ.name}
              {integ.enabled ? (
                <Badge className="bg-emerald-500/10 text-emerald-600 dark:text-emerald-400">enabled</Badge>
              ) : (
                <Badge variant="outline">disabled</Badge>
              )}
            </CardTitle>
            <p className="mt-0.5 text-xs text-muted-foreground">
              {meta?.label ?? integ.kind} · last sync {integ.last_sync_at ? formatRelative(integ.last_sync_at) : "never"}
            </p>
          </div>
        </div>
        <div className="flex gap-1.5">
          <Button variant="outline" size="sm" onClick={onSync} disabled={syncing || !integ.implemented || !integ.enabled}>
            <RefreshCwIcon className={cn2(syncing ? "animate-spin" : "", "size-4")} aria-hidden />
            {syncing ? "Syncing…" : "Sync now"}
          </Button>
          <Button variant="ghost" size="sm" onClick={onEdit}>Edit</Button>
          <ConfirmDialog
            trigger={
              <Button variant="ghost" size="icon" className="size-7" aria-label={`Delete ${integ.name}`}>
                <Trash2Icon className="size-3.5" aria-hidden />
              </Button>
            }
            title={`Delete ${integ.name}?`}
            description="Imported assets stay in the inventory — only the connector and its sync history are removed."
            confirmLabel="Delete connector"
            onConfirm={onDelete}
          />
        </div>
      </CardHeader>
      <CardContent>
        {meta ? <p className="mb-3 text-xs text-muted-foreground">{meta.description}</p> : null}

        {/* Source config summary */}
        <dl className="mb-3 grid grid-cols-2 gap-x-4 gap-y-1 text-xs">
          {Object.entries(integ.config)
            .filter(([key]) => !key.startsWith("secrets"))
            .map(([key, value]) => (
              <div key={key} className="flex justify-between gap-2 overflow-hidden">
                <dt className="shrink-0 text-muted-foreground">{key}</dt>
                <dd className="truncate font-mono">{key === "asset_id" && typeof value === "string" ? (
                  <Link to={`/assets/${value}`} className="hover:underline underline-offset-2">host asset</Link>
                ) : String(value)}</dd>
              </div>
            ))}
          {integ.config.secrets_configured ? (
            <div className="col-span-2 flex justify-between gap-2">
              <dt className="text-muted-foreground">secrets</dt>
              <dd className="font-mono">
                {Object.entries(integ.config.secrets_configured).map(([k, v]) => (
                  <span key={k} className={cn2(v ? "text-emerald-600 dark:text-emerald-400" : "text-muted-foreground")}>
                    {k}: {v ? "configured ✓" : "missing"}
                  </span>
                ))}
              </dd>
            </div>
          ) : null}
        </dl>

        <Button variant="ghost" size="sm" className="h-7 px-2 text-xs" onClick={onToggleHistory}>
          {historyOpen ? "Hide sync history" : "Show sync history"}
        </Button>
        {historyOpen ? <SyncHistory integrationId={integ.id} /> : null}
      </CardContent>
    </Card>
  )
}

function KindIconFromMeta({ meta }: { meta: string }) {
  const meta_ = KIND_META[meta]
  const Icon = meta_?.icon ?? PlugIcon
  return <Icon className="size-5 text-muted-foreground" aria-hidden />
}

function SyncHistory({ integrationId }: { integrationId: string }) {
  const { data, isLoading } = useSyncRuns(integrationId, true)
  if (isLoading) return <p className="mt-2 text-xs text-muted-foreground">Loading…</p>
  const runs = data?.runs ?? []
  if (runs.length === 0) {
    return <p className="mt-2 text-xs text-muted-foreground">No sync runs yet.</p>
  }
  return (
    <ul className="mt-2 space-y-1.5">
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
  const [arpMode, setArpMode] = useState<"table" | "scan">("table")
  const [arpInterface, setArpInterface] = useState("")
  const [arpCidr, setArpCidr] = useState("")
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
      setArpMode(((editing.config.mode as "table" | "scan") ?? "table"))
      setArpInterface((editing.config.interface as string) ?? "")
      setArpCidr((editing.config.cidr as string) ?? "")
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
      setArpMode("table")
      setArpInterface("")
      setArpCidr("")
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
              : {}

    if (editing) {
      update.mutate(
        { id: editing.id, payload: { name: name.trim(), description: description.trim(), enabled, config, secrets } },
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
        { kind, name: name.trim(), description: description.trim(), enabled, config, secrets },
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
                  <option value="table">ARP table — read the local cache (no privileges)</option>
                  <option value="scan">arp-scan — active sweep (needs the binary + NET_RAW)</option>
                </select>
              </div>
              {arpMode === "scan" ? (
                <div className="grid gap-3 sm:grid-cols-2">
                  <div className="space-y-1.5">
                    <Label htmlFor="int-arp-iface">Interface</Label>
                    <Input id="int-arp-iface" className="font-mono text-xs" placeholder="eth0 (empty = default route)"
                      value={arpInterface} onChange={(e) => setArpInterface(e.target.value)} />
                  </div>
                  <div className="space-y-1.5">
                    <Label htmlFor="int-arp-cidr">CIDR</Label>
                    <Input id="int-arp-cidr" className="font-mono text-xs" placeholder="192.168.1.0/24 (empty = local subnet)"
                      value={arpCidr} onChange={(e) => setArpCidr(e.target.value)} />
                  </div>
                  <p className="col-span-2 text-xs text-muted-foreground">
                    arp-scan needs root/NET_RAW on the server host. When Lanvory runs in Docker, scanning the LAN
                    additionally requires <span className="font-mono">network_mode: host</span> — the compose file already
                    grants NET_RAW. The table mode works everywhere but only shows neighbors the host has talked to.
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

// tiny local helper to avoid importing cn twice with different names
function cn2(...classes: (string | false | undefined)[]) {
  return classes.filter(Boolean).join(" ")
}
