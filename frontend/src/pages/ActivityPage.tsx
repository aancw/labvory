import { useState } from "react"
import { ChevronLeftIcon, ChevronRightIcon, ScrollTextIcon } from "lucide-react"

import { useActivityFiltered } from "@/api/hooks"
import { EmptyState } from "@/components/EmptyState"
import { PageHeader } from "@/components/PageHeader"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { Skeleton } from "@/components/ui/skeleton"
import { formatDateTime } from "@/lib/utils"

const PAGE_SIZE = 50

const ACTION_VARIANT: Record<string, "default" | "secondary" | "destructive" | "outline"> = {
  "login.failed": "destructive",
  "login.success": "secondary",
  logout: "secondary",
  created: "default",
  deleted: "destructive",
}

function actionVariant(action: string): "default" | "secondary" | "destructive" | "outline" {
  return ACTION_VARIANT[action] ?? "outline"
}

export function ActivityPage() {
  const [offset, setOffset] = useState(0)
  const [entityType, setEntityType] = useState("")
  const [action, setAction] = useState("")
  const { data, isLoading, isError } = useActivityFiltered(PAGE_SIZE, offset, {
    entity_type: entityType || undefined,
    action: action || undefined,
  })

  const events = data?.events ?? []
  const total = data?.total ?? 0
  const hasNewer = offset > 0
  const hasOlder = offset + PAGE_SIZE < total

  return (
    <>
      <PageHeader
        title="Activity"
        description="Audit trail of changes and security-relevant events across the instance."
      />

      <div className="mb-4 flex flex-wrap gap-2">
        <select
          aria-label="Entity type filter"
          className="h-9 w-44 rounded-md border border-input bg-transparent px-3 text-sm shadow-xs outline-none"
          value={entityType}
          onChange={(e) => { setEntityType(e.target.value); setOffset(0) }}
        >
          <option value="">All entities</option>
          {["asset", "subnet", "ip_address", "service", "vlan", "location", "tag", "user", "auth", "settings"].map((t) => (
            <option key={t} value={t}>{t}</option>
          ))}
        </select>
        <select
          aria-label="Action filter"
          className="h-9 w-44 rounded-md border border-input bg-transparent px-3 text-sm shadow-xs outline-none"
          value={action}
          onChange={(e) => { setAction(e.target.value); setOffset(0) }}
        >
          <option value="">All actions</option>
          {["created", "updated", "deleted", "allocated", "released", "login", "logout", "bulk"].map((a) => (
            <option key={a} value={a}>{a}</option>
          ))}
        </select>
      </div>

      {isError ? (
        <EmptyState
          icon={ScrollTextIcon}
          title="Could not load activity"
          description="The audit log is unavailable right now. Reload to try again."
        />
      ) : isLoading ? (
        <div className="space-y-2">
          {Array.from({ length: 8 }).map((_, i) => (
            <Skeleton key={i} className="h-10 w-full" />
          ))}
        </div>
      ) : events.length === 0 ? (
        <EmptyState
          icon={ScrollTextIcon}
          title="No activity yet"
          description="Actions like sign-ins, settings changes, and (in later phases) asset edits will appear here."
        />
      ) : (
        <>
          <div className="overflow-hidden rounded-lg border">
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead className="w-44">Time</TableHead>
                  <TableHead className="w-36">Actor</TableHead>
                  <TableHead className="w-44">Action</TableHead>
                  <TableHead>Entity</TableHead>
                  <TableHead className="w-32">Source IP</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {events.map((event) => (
                  <TableRow key={event.id}>
                    <TableCell className="text-muted-foreground tabular-nums">
                      {formatDateTime(event.occurred_at)}
                    </TableCell>
                    <TableCell className="font-medium">
                      {event.actor_label || "system"}
                    </TableCell>
                    <TableCell>
                      <Badge variant={actionVariant(event.action)} className="font-mono font-normal">
                        {event.action}
                      </Badge>
                    </TableCell>
                    <TableCell>
                      <span className="text-muted-foreground">{event.entity_type}</span>
                      {event.entity_label ? (
                        <span className="ml-1.5 font-mono text-xs">{event.entity_label}</span>
                      ) : null}
                    </TableCell>
                    <TableCell className="font-mono text-xs text-muted-foreground">
                      {event.source_ip || "—"}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>

          <div className="mt-4 flex items-center justify-between text-sm text-muted-foreground">
            <span className="tabular-nums">
              {offset + 1}–{Math.min(offset + PAGE_SIZE, total)} of {total}
            </span>
            <div className="flex gap-2">
              <Button
                variant="outline"
                size="sm"
                disabled={!hasNewer}
                onClick={() => setOffset(Math.max(0, offset - PAGE_SIZE))}
              >
                <ChevronLeftIcon className="size-4" aria-hidden />
                Newer
              </Button>
              <Button
                variant="outline"
                size="sm"
                disabled={!hasOlder}
                onClick={() => setOffset(offset + PAGE_SIZE)}
              >
                Older
                <ChevronRightIcon className="size-4" aria-hidden />
              </Button>
            </div>
          </div>
        </>
      )}
    </>
  )
}
