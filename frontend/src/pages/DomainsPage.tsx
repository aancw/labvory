import { useState } from "react"
import { toast } from "sonner"
import {
  CircleCheckIcon,
  GlobeIcon,
  PlusIcon,
  RefreshCwIcon,
  TriangleAlertIcon,
  Trash2Icon,
} from "lucide-react"

import { isApiError } from "@/api/client"
import {
  useCreateDomain,
  useCreateSubdomain,
  useDeleteDomain,
  useDeleteSubdomain,
  useDomain,
  useDomains,
  useResolveDomain,
  useResolveSubdomain,
} from "@/api/hooks"
import type { Subdomain } from "@/api/types"
import { ConfirmDialog } from "@/components/ConfirmDialog"
import { EmptyState } from "@/components/EmptyState"
import { PageHeader } from "@/components/PageHeader"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select } from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { formatDateTime } from "@/lib/utils"

const RECORD_TYPES = ["A", "AAAA", "CNAME", "MX", "TXT", "SRV", "CAA", "NS"]

/** Drift badge: declared value vs. last DNS answer. */
function DriftBadge({ record }: { record: Subdomain }) {
  if (!record.observed_at) {
    return <Badge variant="secondary">Unresolved</Badge>
  }
  const declared = record.value.trim()
  const observed = record.observed_value.trim()
  if (!declared) {
    return <Badge variant="outline">Observed</Badge>
  }
  const matches = observed
    .split(",")
    .some((part) => part.trim().toLowerCase() === declared.toLowerCase())
  return matches ? (
    <Badge variant="outline" className="gap-1">
      <CircleCheckIcon className="size-3" aria-hidden /> Match
    </Badge>
  ) : (
    <Badge variant="destructive" className="gap-1">
      <TriangleAlertIcon className="size-3" aria-hidden /> Drift
    </Badge>
  )
}

export function DomainsPage() {
  const { data, isLoading } = useDomains()
  const domains = data?.domains ?? []

  const createDomain = useCreateDomain()
  const deleteDomain = useDeleteDomain()

  const [name, setName] = useState("")
  const [registrar, setRegistrar] = useState("")
  const [expires, setExpires] = useState("")
  const [selectedId, setSelectedId] = useState<string | undefined>(undefined)

  const submitDomain = () => {
    if (!name.trim()) return
    createDomain.mutate(
      {
        name: name.trim(),
        registrar: registrar.trim(),
        expires_on: expires || null,
      },
      {
        onSuccess: (d) => {
          toast.success(`Domain ${d.name} added.`)
          setName("")
          setRegistrar("")
          setExpires("")
          setSelectedId(d.id)
        },
        onError: (err) => toast.error(isApiError(err) ? err.message : "Could not add domain."),
      },
    )
  }

  return (
    <>
      <PageHeader
        title="Domains"
        description="DNS zones and records. Declared values are yours; observed values come from real DNS lookups."
      />

      <Card>
        <CardHeader>
          <CardTitle className="text-sm">Add a domain</CardTitle>
          <CardDescription>
            Track a zone manually here, or let a <code className="font-mono text-xs">dns</code>{" "}
            integration (AXFR / crt.sh) enumerate its records for you.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <form
            className="flex flex-wrap items-end gap-3"
            onSubmit={(e) => {
              e.preventDefault()
              submitDomain()
            }}
          >
            <div className="w-64 space-y-2">
              <Label htmlFor="dom-name">Domain</Label>
              <Input
                id="dom-name"
                placeholder="example.com"
                value={name}
                onChange={(e) => setName(e.target.value)}
              />
            </div>
            <div className="w-52 space-y-2">
              <Label htmlFor="dom-registrar">Registrar</Label>
              <Input
                id="dom-registrar"
                placeholder="optional"
                value={registrar}
                onChange={(e) => setRegistrar(e.target.value)}
              />
            </div>
            <div className="w-44 space-y-2">
              <Label htmlFor="dom-expires">Expires on</Label>
              <Input
                id="dom-expires"
                type="date"
                value={expires}
                onChange={(e) => setExpires(e.target.value)}
              />
            </div>
            <Button type="submit" size="sm" disabled={createDomain.isPending || !name.trim()}>
              <PlusIcon className="size-4" aria-hidden />
              {createDomain.isPending ? "Adding…" : "Add domain"}
            </Button>
          </form>
        </CardContent>
      </Card>

      <Card className="mt-4">
        <CardHeader>
          <CardTitle className="text-sm">Zones</CardTitle>
          <CardDescription>Select a domain to manage its records.</CardDescription>
        </CardHeader>
        <CardContent>
          {isLoading ? (
            <Skeleton className="h-24 w-full" />
          ) : domains.length === 0 ? (
            <EmptyState
              icon={GlobeIcon}
              title="No domains yet"
              description="Add your first zone above to start tracking DNS records."
            />
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Domain</TableHead>
                  <TableHead>Registrar</TableHead>
                  <TableHead>Records</TableHead>
                  <TableHead>Expires</TableHead>
                  <TableHead className="w-0 text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {domains.map((d) => (
                  <TableRow
                    key={d.id}
                    className="cursor-pointer"
                    data-state={d.id === selectedId ? "selected" : undefined}
                    onClick={() => setSelectedId((prev) => (prev === d.id ? undefined : d.id))}
                  >
                    <TableCell className="font-mono font-medium">{d.name}</TableCell>
                    <TableCell className="text-muted-foreground">{d.registrar || "—"}</TableCell>
                    <TableCell className="text-muted-foreground">{d.subdomain_count}</TableCell>
                    <TableCell className="text-muted-foreground">{d.expires_on || "—"}</TableCell>
                    <TableCell className="text-right">
                      <ConfirmDialog
                        trigger={
                          <Button variant="ghost" size="icon" className="size-7" aria-label={`Delete ${d.name}`}>
                            <Trash2Icon className="size-3.5" aria-hidden />
                          </Button>
                        }
                        title={`Delete ${d.name}?`}
                        description="The domain and every record under it are removed. This cannot be undone."
                        confirmLabel="Delete domain"
                        onConfirm={() => {
                          deleteDomain.mutate(d.id, {
                            onSuccess: () => {
                              toast.success("Domain deleted.")
                              if (selectedId === d.id) setSelectedId(undefined)
                            },
                            onError: (err) =>
                              toast.error(isApiError(err) ? err.message : "Could not delete domain."),
                          })
                        }}
                      />
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      {selectedId ? <DomainRecords domainId={selectedId} /> : null}
    </>
  )
}

function DomainRecords({ domainId }: { domainId: string }) {
  const { data, isLoading } = useDomain(domainId)
  const createSubdomain = useCreateSubdomain()
  const deleteSubdomain = useDeleteSubdomain()
  const resolveSubdomain = useResolveSubdomain()
  const resolveDomain = useResolveDomain()

  const [label, setLabel] = useState("")
  const [recordType, setRecordType] = useState("A")
  const [value, setValue] = useState("")
  const [ttl, setTtl] = useState("")

  const domain = data?.domain
  const records = data?.subdomains ?? []

  const addRecord = () => {
    if (!domain) return
    createSubdomain.mutate(
      {
        domainId: domain.id,
        label: label.trim(),
        record_type: recordType,
        value: value.trim(),
        ttl: ttl.trim() ? Number(ttl) : null,
      },
      {
        onSuccess: () => {
          toast.success("Record added.")
          setLabel("")
          setValue("")
          setTtl("")
        },
        onError: (err) => toast.error(isApiError(err) ? err.message : "Could not add record."),
      },
    )
  }

  return (
    <Card className="mt-4">
      <CardHeader>
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <CardTitle className="text-sm">
              Records — <span className="font-mono">{domain?.name ?? "…"}</span>
            </CardTitle>
            <CardDescription>
              Empty label = apex record. “Resolve all” queries DNS for every record and flags drift.
            </CardDescription>
          </div>
          <Button
            variant="outline"
            size="sm"
            disabled={resolveDomain.isPending || records.length === 0}
            onClick={() =>
              resolveDomain.mutate(domainId, {
                onSuccess: (r) =>
                  toast.success(
                    `Resolved ${r.resolved}/${r.total} records` +
                      (r.drifted ? ` · ${r.drifted} drifted` : "") +
                      (r.failed ? ` · ${r.failed} failed` : ""),
                  ),
                onError: (err) => toast.error(isApiError(err) ? err.message : "Resolve failed."),
              })
            }
          >
            <RefreshCwIcon className={resolveDomain.isPending ? "size-4 animate-spin" : "size-4"} aria-hidden />
            Resolve all
          </Button>
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        <form
          className="flex flex-wrap items-end gap-3"
          onSubmit={(e) => {
            e.preventDefault()
            addRecord()
          }}
        >
          <div className="w-44 space-y-2">
            <Label htmlFor="rec-label">Label</Label>
            <Input
              id="rec-label"
              placeholder="www (blank = apex)"
              value={label}
              onChange={(e) => setLabel(e.target.value)}
            />
          </div>
          <div className="w-28 space-y-2">
            <Label htmlFor="rec-type">Type</Label>
            <Select id="rec-type" value={recordType} onChange={(e) => setRecordType(e.target.value)}>
              {RECORD_TYPES.map((t) => (
                <option key={t} value={t}>{t}</option>
              ))}
            </Select>
          </div>
          <div className="w-64 space-y-2">
            <Label htmlFor="rec-value">Declared value</Label>
            <Input
              id="rec-value"
              placeholder={recordType === "A" ? "192.168.1.10" : "target"}
              value={value}
              onChange={(e) => setValue(e.target.value)}
            />
          </div>
          <div className="w-28 space-y-2">
            <Label htmlFor="rec-ttl">TTL</Label>
            <Input
              id="rec-ttl"
              placeholder="auto"
              inputMode="numeric"
              value={ttl}
              onChange={(e) => setTtl(e.target.value.replace(/[^0-9]/g, ""))}
            />
          </div>
          <Button type="submit" size="sm" disabled={createSubdomain.isPending}>
            <PlusIcon className="size-4" aria-hidden />
            {createSubdomain.isPending ? "Adding…" : "Add record"}
          </Button>
        </form>

        {isLoading ? (
          <Skeleton className="h-24 w-full" />
        ) : records.length === 0 ? (
          <EmptyState
            icon={GlobeIcon}
            title="No records in this zone"
            description="Add records manually, or run a dns integration to enumerate them."
          />
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Type</TableHead>
                <TableHead>Declared</TableHead>
                <TableHead>Observed</TableHead>
                <TableHead>State</TableHead>
                <TableHead>Last checked</TableHead>
                <TableHead className="w-0 text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {records.map((rec) => (
                <TableRow key={rec.id}>
                  <TableCell className="font-mono text-xs">{rec.fqdn}</TableCell>
                  <TableCell>
                    <Badge variant="secondary">{rec.record_type}</Badge>
                  </TableCell>
                  <TableCell className="font-mono text-xs text-muted-foreground">
                    {rec.value || "—"}
                  </TableCell>
                  <TableCell className="max-w-56 truncate font-mono text-xs" title={rec.observed_value}>
                    {rec.observed_value || "—"}
                  </TableCell>
                  <TableCell>
                    <DriftBadge record={rec} />
                  </TableCell>
                  <TableCell className="text-muted-foreground">
                    {rec.observed_at ? formatDateTime(rec.observed_at) : "Never"}
                  </TableCell>
                  <TableCell className="text-right">
                    <div className="flex items-center justify-end gap-2">
                      <Button
                        variant="outline"
                        size="sm"
                        disabled={resolveSubdomain.isPending}
                        onClick={() =>
                          resolveSubdomain.mutate(
                            { id: rec.id, domainId },
                            {
                              onSuccess: (r) =>
                                r.error
                                  ? toast.error(r.error)
                                  : toast.success(r.drift ? "Resolved — drift detected." : "Resolved — matches."),
                              onError: (err) =>
                                toast.error(isApiError(err) ? err.message : "Resolve failed."),
                            },
                          )
                        }
                      >
                        <RefreshCwIcon className="size-3.5" aria-hidden />
                        Resolve
                      </Button>
                      <ConfirmDialog
                        trigger={
                          <Button variant="ghost" size="icon" className="size-7" aria-label={`Delete ${rec.fqdn}`}>
                            <Trash2Icon className="size-3.5" aria-hidden />
                          </Button>
                        }
                        title={`Delete ${rec.fqdn}?`}
                        description="The record is removed from the inventory."
                        confirmLabel="Delete record"
                        onConfirm={() =>
                          deleteSubdomain.mutate(
                            { id: rec.id, domainId },
                            {
                              onSuccess: () => toast.success("Record deleted."),
                              onError: (err) =>
                                toast.error(isApiError(err) ? err.message : "Could not delete record."),
                            },
                          )
                        }
                      />
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  )
}
