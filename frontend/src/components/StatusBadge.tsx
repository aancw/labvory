import { Badge } from "@/components/ui/badge"
import { cn } from "@/lib/utils"

// Single source of truth for asset/service status coloring across the app.
const STATUS_STYLES: Record<string, { dot: string; className: string; label: string }> = {
  online: { dot: "bg-emerald-500", className: "text-emerald-600 dark:text-emerald-400 border-emerald-500/25 bg-emerald-500/10", label: "Online" },
  offline: { dot: "bg-zinc-400 dark:bg-zinc-500", className: "text-muted-foreground border-border bg-muted/60", label: "Offline" },
  maintenance: { dot: "bg-amber-500", className: "text-amber-600 dark:text-amber-400 border-amber-500/25 bg-amber-500/10", label: "Maintenance" },
  retired: { dot: "bg-zinc-500", className: "text-muted-foreground border-border bg-muted/60", label: "Retired" },
  unknown: { dot: "bg-zinc-400", className: "text-muted-foreground border-border bg-muted/60", label: "Unknown" },
  assigned: { dot: "bg-emerald-500", className: "text-emerald-600 dark:text-emerald-400 border-emerald-500/25 bg-emerald-500/10", label: "Assigned" },
  available: { dot: "bg-sky-500", className: "text-sky-600 dark:text-sky-400 border-sky-500/25 bg-sky-500/10", label: "Available" },
  reserved: { dot: "bg-violet-500", className: "text-violet-600 dark:text-violet-400 border-violet-500/25 bg-violet-500/10", label: "Reserved" },
  excluded: { dot: "bg-zinc-500", className: "text-muted-foreground border-border bg-muted/60", label: "Excluded" },
  conflict: { dot: "bg-red-500", className: "text-red-600 dark:text-red-400 border-red-500/25 bg-red-500/10", label: "Conflict" },
  // Kubernetes pod phases (lowercase, as synced by the kubernetes connector).
  running: { dot: "bg-emerald-500", className: "text-emerald-600 dark:text-emerald-400 border-emerald-500/25 bg-emerald-500/10", label: "Running" },
  pending: { dot: "bg-amber-500", className: "text-amber-600 dark:text-amber-400 border-amber-500/25 bg-amber-500/10", label: "Pending" },
  succeeded: { dot: "bg-sky-500", className: "text-sky-600 dark:text-sky-400 border-sky-500/25 bg-sky-500/10", label: "Succeeded" },
  failed: { dot: "bg-red-500", className: "text-red-600 dark:text-red-400 border-red-500/25 bg-red-500/10", label: "Failed" },
}

export function StatusBadge({ status, className }: { status: string; className?: string }) {
  const style = STATUS_STYLES[status] ?? STATUS_STYLES.unknown
  return (
    <Badge variant="outline" className={cn("gap-1.5 font-normal", style.className, className)}>
      <span className={cn("size-1.5 rounded-full", style.dot)} aria-hidden />
      {style.label}
    </Badge>
  )
}

export function statusLabel(status: string): string {
  return STATUS_STYLES[status]?.label ?? status
}
