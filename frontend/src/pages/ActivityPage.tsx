import { Fragment, useState } from "react"
import { ChevronLeftIcon, ChevronRightIcon, ScrollTextIcon } from "lucide-react"

import { useActivityFiltered } from "@/api/hooks"
import type { AuditEvent } from "@/api/types"
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
import { cn, formatDateTime } from "@/lib/utils"

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

function formatChangeValue(v: unknown): string {
  if (v === null || v === undefined || v === "") return "—"
  if (typeof v === "object") return JSON.stringify(v)
  return String(v)
}

type ChangeEntry = { field: string; from?: unknown; to?: unknown }

// Audit payloads come in two shapes: update diffs ({field: {from, to}}) and
// ad-hoc context maps ({name: "…", summary: …}). Normalize to one entry list.
function changeEntries(changes: Record<string, unknown>): ChangeEntry[] {
  return Object.entries(changes).map(([field, value]) => {
    if (
      value &&
      typeof value === "object" &&
      !Array.isArray(value) &&
      "from" in (value as object) &&
      "to" in (value as object)
    ) {
      const pair = value as { from?: unknown; to?: unknown }
      return { field, from: pair.from, to: pair.to }
    }
    return { field, to: value }
  })
}

function ChangesDetail({ event }: { event: AuditEvent }) {
  const entries = changeEntries(event.changes ?? {})
  return (
    <div className="max-w-3xl space-y-1 rounded-md bg-muted/40 px-3 py-2.5">
      {entries.map((entry) => (
        <div key={entry.field} className="flex items-baseline gap-3 text-xs">
          <span className="w-40 shrink-0 truncate text-muted-foreground" title={entry.field}>
            {entry.field}
          </span>
          {entry.from !== undefined ? (
            <span className="min-w-0 truncate font-mono" title={`${formatChangeValue(entry.from)} → ${formatChangeValue(entry.to)}`}>
              {formatChangeValue(entry.from)}
              <span className="mx-1.5 text-muted-foreground">→</span>
              {formatChangeValue(entry.to)}
            </span>
          ) : (
            <span className="min-w-0 truncate font-mono" title={formatChangeValue(entry.to)}>
              {formatChangeValue(entry.to)}
            </span>
          )}
        </div>
      ))}
    </div>
  )
}

export function ActivityPage() {
  const [offset, setOffset] = useState(0)
  const [entityType, setEntityType] = useState("")
  const [action, setAction] = useState("")
  const [expanded, setExpanded] = useState<string | null>(null)
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
          description="Sign-ins, settings changes, and edits across the inventory will appear here as you use Lanvory."
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
                  <TableHead className="w-8" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {events.map((event) => {
                  const hasChanges = !!event.changes && Object.keys(event.changes).length > 0
                  const isOpen = expanded === event.id
                  return (
                    <Fragment key={event.id}>
                      <TableRow
                        className={cn(hasChanges && "cursor-pointer")}
                        onClick={hasChanges ? () => setExpanded(isOpen ? null : event.id) : undefined}
                      >
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
                        <TableCell>
                          {hasChanges ? (
                            <ChevronRightIcon
                              className={cn("size-4 text-muted-foreground transition-transform", isOpen && "rotate-90")}
                              aria-hidden
                            />
                          ) : null}
                        </TableCell>
                      </TableRow>
                      {isOpen && hasChanges ? (
                        <TableRow className="hover:bg-transparent">
                          <TableCell colSpan={6}>
                            <ChangesDetail event={event} />
                          </TableCell>
                        </TableRow>
                      ) : null}
                    </Fragment>
                  )
                })}
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
