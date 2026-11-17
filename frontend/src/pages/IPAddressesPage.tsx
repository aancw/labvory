import { useState } from "react"
import { Link, useSearchParams } from "react-router-dom"
import { ChevronLeftIcon, ChevronRightIcon, NetworkIcon, ShieldAlertIcon } from "lucide-react"

import { useIPs } from "@/api/hooks"
import { EmptyState } from "@/components/EmptyState"
import { PageHeader } from "@/components/PageHeader"
import { StatusBadge } from "@/components/StatusBadge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Select } from "@/components/ui/select"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { formatRelative } from "@/lib/utils"

const PAGE_SIZE = 100

export function IPAddressesPage() {
  const [searchParams] = useSearchParams()
  const [qDraft, setQDraft] = useState("")
  const [q, setQ] = useState("")
  // Filters seed from the URL so dashboard alerts (e.g. /ips?status=conflict
  // or /ips?observed_conflict=1) land on the pre-filtered view.
  const [status, setStatus] = useState(searchParams.get("status") ?? "")
  const [observedConflict, setObservedConflict] = useState(
    searchParams.get("observed_conflict") === "1" || searchParams.get("observed_conflict") === "true",
  )
  const [offset, setOffset] = useState(0)

  const { data, isLoading } = useIPs({
    q,
    status,
    ...(observedConflict ? { observed_conflict: true } : {}),
    limit: PAGE_SIZE,
    offset,
  })
  const addresses = data?.addresses ?? []
  const total = data?.total ?? 0
  const filtersActive = q || status || observedConflict

  const clearFilters = () => {
    setQ("")
    setQDraft("")
    setStatus("")
    setObservedConflict(false)
    setOffset(0)
  }

  return (
    <>
      <PageHeader
        title="IP Addresses"
        description="Every tracked address across all subnets. Allocate new addresses from a subnet's page."
      />

      <div className="mb-4 flex flex-wrap items-center gap-2">
        <div className="w-64">
          <Input
            aria-label="Search addresses"
            placeholder="Search IP or hostname…"
            value={qDraft}
            onChange={(e) => setQDraft(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                setQ(qDraft.trim())
                setOffset(0)
              }
            }}
            onBlur={() => {
              setQ(qDraft.trim())
              setOffset(0)
            }}
          />
        </div>
        <Select aria-label="Status filter" className="w-40" value={status} onChange={(e) => { setStatus(e.target.value); setOffset(0) }}>
          <option value="">All statuses</option>
          <option value="assigned">Assigned</option>
          <option value="reserved">Reserved</option>
          <option value="excluded">Excluded</option>
          <option value="conflict">Conflict</option>
          <option value="available">Available (tracked)</option>
        </Select>
        <label className="inline-flex cursor-pointer items-center gap-2 text-sm">
          <input
            type="checkbox"
            checked={observedConflict}
            onChange={(e) => { setObservedConflict(e.target.checked); setOffset(0) }}
          />
          MAC mismatches only
        </label>
        {filtersActive ? (
          <Button variant="ghost" size="sm" onClick={clearFilters}>
            Clear filters
          </Button>
        ) : null}
      </div>

      {isLoading ? (
        <div className="h-64 animate-pulse rounded-lg bg-accent" />
      ) : addresses.length === 0 ? (
        <EmptyState
          icon={NetworkIcon}
          title={filtersActive ? "No addresses match" : "No tracked addresses yet"}
          description={
            filtersActive
              ? "Try clearing the search or filters."
              : "Create a subnet and allocate addresses from its page."
          }
        />
      ) : (
        <>
          <div className="overflow-hidden rounded-lg border">
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead>Address</TableHead>
                  <TableHead>Subnet</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Method</TableHead>
                  <TableHead>Hostname</TableHead>
                  <TableHead>Asset</TableHead>
                  <TableHead>Last seen</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {addresses.map((ip) => (
                  <TableRow key={ip.id}>
                    <TableCell className="font-mono text-xs">
                      <span className="inline-flex items-center gap-2">
                        {ip.address}
                        {ip.observed_conflict ? (
                          <span
                            title="The ARP sweep last saw a different MAC answering at this address than the interface it is assigned to."
                            className="inline-flex items-center gap-1 rounded border border-red-500/40 bg-red-500/10 px-1.5 py-0.5 text-xs text-red-600 dark:text-red-400"
                          >
                            <ShieldAlertIcon className="size-3" aria-hidden />MAC mismatch
                          </span>
                        ) : null}
                      </span>
                    </TableCell>
                    <TableCell>
                      <Link to={`/subnets/${ip.subnet_id}`} className="font-mono text-xs hover:underline underline-offset-2">
                        {ip.subnet_cidr}
                      </Link>
                    </TableCell>
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
                    <TableCell className="text-muted-foreground" title={ip.last_observed_at ?? undefined}>
                      {ip.last_observed_at ? formatRelative(ip.last_observed_at) : "—"}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>

          {total > PAGE_SIZE ? (
            <div className="mt-4 flex items-center justify-between text-sm text-muted-foreground">
              <span className="tabular-nums">
                {offset + 1}–{Math.min(offset + PAGE_SIZE, total)} of {total}
              </span>
              <div className="flex gap-2">
                <Button variant="outline" size="sm" disabled={offset === 0}
                  onClick={() => setOffset(Math.max(0, offset - PAGE_SIZE))}>
                  <ChevronLeftIcon className="size-4" aria-hidden />Previous
                </Button>
                <Button variant="outline" size="sm" disabled={offset + PAGE_SIZE >= total}
                  onClick={() => setOffset(offset + PAGE_SIZE)}>
                  Next<ChevronRightIcon className="size-4" aria-hidden />
                </Button>
              </div>
            </div>
          ) : null}
        </>
      )}
    </>
  )
}
