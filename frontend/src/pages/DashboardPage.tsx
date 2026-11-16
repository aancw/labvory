import { Link } from "react-router-dom"
import {
  ArrowRightIcon,
  BoxesIcon,
  ShieldAlertIcon,
  CircuitBoardIcon,
  ContainerIcon,
  GlobeIcon,
  HardDriveIcon,
  MonitorIcon,
  NetworkIcon,
  ScrollTextIcon,
  ShieldCheckIcon,
} from "lucide-react"

import { useDashboard, useMe } from "@/api/hooks"
import type { DashboardStatsV2 } from "@/api/types"
import { EmptyState } from "@/components/EmptyState"
import { PageHeader } from "@/components/PageHeader"
import { StatusBadge } from "@/components/StatusBadge"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { cn, formatRelative } from "@/lib/utils"

export function DashboardPage() {
  const { data: me } = useMe()
  const { data, isLoading, isError } = useDashboard()

  const displayName = me?.user.display_name || me?.user.username || "operator"

  if (isError) {
    return (
      <>
        <PageHeader title="Dashboard" />
        <EmptyState
          icon={ShieldCheckIcon}
          title="Dashboard unavailable"
          description="Could not load infrastructure statistics. Check that the API is reachable and reload."
        />
      </>
    )
  }

  return (
    <>
      <PageHeader
        title={`Welcome, ${displayName}`}
        description="Operational overview of your homelab infrastructure."
      />

      {isLoading || !data ? (
        <StatsSkeleton />
      ) : (
        <>
          {/* Stat cards — each navigates to its filtered inventory view. */}
          <div className="grid grid-cols-2 gap-4 md:grid-cols-4">
            <StatCard
              to="/assets"
              icon={BoxesIcon}
              label="Total assets"
              value={data.assets.total}
              hint={`${data.assets.physical} physical · ${data.assets.virtual} virtual`}
            />
            <StatCard
              to="/assets/physical"
              icon={HardDriveIcon}
              label="Physical devices"
              value={data.assets.physical}
              hint="Servers, network gear, SBCs"
            />
            <StatCard
              to="/assets/virtual-machines"
              icon={MonitorIcon}
              label="Virtual machines"
              value={data.assets.vms}
              hint={`${data.assets.containers} LXC/K8s guests`}
            />
            <StatCard
              to="/assets/containers"
              icon={ContainerIcon}
              label="Docker hosts"
              value={data.assets.docker_hosts}
              hint="Registered Docker workloads"
            />
            <StatCard
              to="/services"
              icon={CircuitBoardIcon}
              label="Services"
              value={data.services}
              hint="Tracked applications"
            />
            <StatCard
              to="/subnets"
              icon={GlobeIcon}
              label="Subnets"
              value={data.network.subnets}
              hint={`${data.network.vlans} VLANs mapped`}
            />
            <StatCard
              to="/ips"
              icon={NetworkIcon}
              label="IPs assigned"
              value={data.network.ip_assigned}
              hint={`${data.network.ip_available} available tracked`}
            />
            <StatCard
              to="/ips"
              icon={ShieldCheckIcon}
              label="Reserved (unassigned)"
              value={data.network.ip_reserved}
              hint={`${data.network.ip_conflicts} conflicts`}
            />
          </div>

          {/* Device status strip */}
          <Card className="mt-6">
            <CardHeader>
              <CardTitle className="text-sm">Assets by status</CardTitle>
            </CardHeader>
            <CardContent>
              {data.assets.total === 0 ? (
                <EmptyState
                  icon={BoxesIcon}
                  title="No infrastructure registered yet"
                  description="Asset registration ships in Phase 2. Until then, this dashboard reflects live database state — everything you see here is real."
                />
              ) : (
                <div className="flex flex-wrap gap-x-8 gap-y-3">
                  <StatusCount status="online" count={data.assets.online} />
                  <StatusCount status="offline" count={data.assets.offline} />
                  <StatusCount status="maintenance" count={data.assets.maintenance} />
                  <StatusCount status="retired" count={data.assets.retired} />
                  <StatusCount status="unknown" count={data.assets.unknown} />
                </div>
              )}
            </CardContent>
          </Card>

          {/* Alerts */}
          {(() => {
            const stats = data as DashboardStatsV2
            const alertItems = [
              { label: "IP conflicts", count: stats.alerts?.ip_conflicts ?? 0, to: "/ips?status=conflict" },
              { label: "Warranty expiring (60d)", count: stats.alerts?.warranty_expiring ?? 0, to: "/assets" },
              { label: "Services without host", count: stats.alerts?.services_without_host ?? 0, to: "/services" },
            ]
            const total = alertItems.reduce((acc, a) => acc + a.count, 0)
            if (total === 0) return null
            return (
              <Card className="mt-6 border-amber-500/30">
                <CardHeader className="flex-row items-center justify-between">
                  <CardTitle className="text-sm">Attention needed</CardTitle>
                  <ShieldAlertIcon className="size-4 text-amber-500" aria-hidden />
                </CardHeader>
                <CardContent>
                  <div className="flex flex-wrap gap-2">
                    {alertItems.filter((a) => a.count > 0).map((a) => (
                      <Link key={a.label} to={a.to}
                        className="inline-flex items-center gap-2 rounded-md border border-amber-500/30 bg-amber-500/10 px-3 py-1.5 text-sm text-amber-700 hover:bg-amber-500/20 dark:text-amber-400">
                        <span className="font-semibold tabular-nums">{a.count}</span> {a.label}
                      </Link>
                    ))}
                  </div>
                </CardContent>
              </Card>
            )
          })()}

          {/* IP utilization by subnet + assets by OS + services by host */}
          {(() => {
            const stats = data as DashboardStatsV2
            const util = stats.subnet_utilization ?? []
            const byOS = stats.by_os ?? []
            const byHost = stats.services_by_host ?? []
            if (util.length === 0 && byOS.length === 0 && byHost.length === 0) return null
            return (
              <div className="mt-6 grid gap-4 lg:grid-cols-3">
                {util.length > 0 ? (
                  <Card>
                    <CardHeader><CardTitle className="text-sm">IP utilization by subnet</CardTitle></CardHeader>
                    <CardContent>
                      <ul className="space-y-2.5">
                        {util.map((s) => (
                          <li key={s.id} className="flex items-center gap-3 text-sm">
                            <Link to={`/subnets/${s.id}`} className="min-w-0 flex-1 truncate font-mono text-xs hover:underline underline-offset-2">
                              {s.cidr}
                              <span className="ml-1.5 font-sans text-muted-foreground">{s.name}</span>
                            </Link>
                            <UtilizationMini value={s.utilization} />
                          </li>
                        ))}
                      </ul>
                    </CardContent>
                  </Card>
                ) : null}
                {byOS.length > 0 ? (
                  <Card>
                    <CardHeader><CardTitle className="text-sm">Assets by OS</CardTitle></CardHeader>
                    <CardContent>
                      <ul className="space-y-2 text-sm">
                        {byOS.map((o) => (
                          <li key={o.os} className="flex items-center justify-between gap-2">
                            <Link to={`/assets?q=${encodeURIComponent(o.os)}`} className="min-w-0 truncate hover:underline underline-offset-2">
                              {o.os}
                            </Link>
                            <span className="tabular-nums text-muted-foreground">{o.count}</span>
                          </li>
                        ))}
                      </ul>
                    </CardContent>
                  </Card>
                ) : null}
                {byHost.length > 0 ? (
                  <Card>
                    <CardHeader><CardTitle className="text-sm">Services by host</CardTitle></CardHeader>
                    <CardContent>
                      <ul className="space-y-2 text-sm">
                        {byHost.map((h) => (
                          <li key={h.asset_id} className="flex items-center justify-between gap-2">
                            <Link to={`/assets/${h.asset_id}`} className="min-w-0 truncate hover:underline underline-offset-2">
                              {h.asset_name}
                            </Link>
                            <span className="tabular-nums text-muted-foreground">{h.services}</span>
                          </li>
                        ))}
                      </ul>
                    </CardContent>
                  </Card>
                ) : null}
              </div>
            )
          })()}

          {/* Recent activity + recently updated */}
          <div className="mt-6 grid gap-4 lg:grid-cols-2">
            <Card>
              <CardHeader className="flex-row items-center justify-between">
                <CardTitle className="text-sm">Recent activity</CardTitle>
                <Link
                  to="/activity"
                  className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
                >
                  View all
                  <ArrowRightIcon className="size-3" aria-hidden />
                </Link>
              </CardHeader>
              <CardContent>
                {data.recent_activity.length === 0 ? (
                  <p className="py-6 text-center text-sm text-muted-foreground">
                    No activity recorded yet.
                  </p>
                ) : (
                  <ul className="space-y-2.5">
                    {data.recent_activity.slice(0, 6).map((event) => (
                      <li key={event.id} className="flex items-center gap-3 text-sm">
                        <ScrollTextIcon
                          className="size-3.5 shrink-0 text-muted-foreground"
                          aria-hidden
                        />
                        <span className="min-w-0 flex-1 truncate">
                          <span className="font-medium">{event.actor_label || "system"}</span>{" "}
                          <span className="text-muted-foreground">{humanAction(event.action)}</span>{" "}
                          <span className="font-mono text-xs">{event.entity_label}</span>
                        </span>
                        <span className="shrink-0 text-xs text-muted-foreground tabular-nums">
                          {formatRelative(event.occurred_at)}
                        </span>
                      </li>
                    ))}
                  </ul>
                )}
              </CardContent>
            </Card>

            <Card>
              <CardHeader>
                <CardTitle className="text-sm">Recently updated assets</CardTitle>
              </CardHeader>
              <CardContent>
                {data.recently_updated_assets.length === 0 ? (
                  <p className="py-6 text-center text-sm text-muted-foreground">
                    No assets registered yet.
                  </p>
                ) : (
                  <ul className="space-y-2.5">
                    {data.recently_updated_assets.slice(0, 6).map((asset) => (
                      <li key={asset.id} className="flex items-center gap-3 text-sm">
                        <Link
                          to={`/assets/${asset.id}`}
                          className="min-w-0 flex-1 truncate font-medium hover:underline underline-offset-2"
                        >
                          {asset.name}
                          {asset.hostname ? (
                            <span className="ml-1.5 font-mono text-xs font-normal text-muted-foreground">
                              {asset.hostname}
                            </span>
                          ) : null}
                        </Link>
                        <span className="shrink-0 text-xs text-muted-foreground">
                          {asset.type_label}
                        </span>
                        <StatusBadge status={asset.status} className="shrink-0" />
                      </li>
                    ))}
                  </ul>
                )}
              </CardContent>
            </Card>
          </div>
        </>
      )}
    </>
  )
}

function StatCard({
  to,
  icon: Icon,
  label,
  value,
  hint,
}: {
  to: string
  icon: typeof BoxesIcon
  label: string
  value: number
  hint: string
}) {
  return (
    <Link
      to={to}
      className="group rounded-lg border bg-card p-4 shadow-xs transition-colors hover:border-primary/40 hover:bg-accent/40 focus-visible:ring-[3px] focus-visible:ring-ring/40 focus-visible:outline-none"
    >
      <div className="flex items-center gap-2 text-muted-foreground">
        <Icon className="size-4" aria-hidden />
        <span className="text-xs font-medium tracking-wide uppercase">{label}</span>
      </div>
      <p className="mt-2 text-2xl font-semibold tabular-nums">{value}</p>
      <p className="mt-1 truncate text-xs text-muted-foreground">{hint}</p>
    </Link>
  )
}

function UtilizationMini({ value }: { value: number }) {
  const tone = value >= 90 ? "bg-red-500" : value >= 70 ? "bg-amber-500" : "bg-sky-500"
  return (
    <span className="inline-flex items-center gap-1.5">
      <span className="h-1.5 w-16 overflow-hidden rounded-full bg-muted">
        <span className={cn("block h-full rounded-full", tone)} style={{ width: `${value}%` }} />
      </span>
      <span className="w-8 text-right text-xs tabular-nums text-muted-foreground">{value}%</span>
    </span>
  )
}

function StatusCount({ status, count }: { status: string; count: number }) {
  return (
    <div className="flex items-center gap-2">
      <StatusBadge status={status} />
      <span className="text-lg font-semibold tabular-nums">{count}</span>
    </div>
  )
}

export function humanAction(action: string): string {
  return action.replaceAll(".", " ")
}

function StatsSkeleton() {
  return (
    <div className="grid grid-cols-2 gap-4 md:grid-cols-4">
      {Array.from({ length: 8 }).map((_, i) => (
        <Skeleton key={i} className="h-24 rounded-lg" />
      ))}
    </div>
  )
}
