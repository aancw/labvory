import { Link } from "react-router-dom"
import { WaypointsIcon } from "lucide-react"

import { useInterfaces } from "@/api/hooks"
import { EmptyState } from "@/components/EmptyState"
import { PageHeader } from "@/components/PageHeader"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Skeleton } from "@/components/ui/skeleton"

export function InterfacesPage() {
  const { data, isLoading, isError } = useInterfaces()

  return (
    <>
      <PageHeader
        title="Network Interfaces"
        description="Every interface across the inventory, with MACs, IP counts, and owning assets."
      />

      {isError ? (
        <EmptyState
          icon={WaypointsIcon}
          title="Could not load interfaces"
          description="Check the API connection and reload."
        />
      ) : isLoading ? (
        <div className="space-y-2">{Array.from({ length: 6 }).map((_, i) => (
          <Skeleton key={i} className="h-10 w-full" />
        ))}</div>
      ) : (data?.length ?? 0) === 0 ? (
        <EmptyState
          icon={WaypointsIcon}
          title="No interfaces registered"
          description="Interfaces are added from an asset's Network tab."
        />
      ) : (
        <div className="overflow-hidden rounded-lg border">
          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead>Asset</TableHead>
                <TableHead>Interface</TableHead>
                <TableHead>Kind</TableHead>
                <TableHead>MAC address</TableHead>
                <TableHead>IPs</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {(data ?? []).map((ni) => (
                <TableRow key={ni.id}>
                  <TableCell>
                    <Link to={`/assets/${ni.asset_id}`} className="font-medium hover:underline underline-offset-2">
                      {ni.asset_name}
                    </Link>
                  </TableCell>
                  <TableCell className="font-mono text-sm">{ni.name}</TableCell>
                  <TableCell className="text-muted-foreground">{ni.kind}</TableCell>
                  <TableCell className="font-mono text-xs">{ni.mac_address ?? "—"}</TableCell>
                  <TableCell className="tabular-nums text-muted-foreground">{ni.ip_count}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </>
  )
}
