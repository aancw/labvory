import { Link } from "react-router-dom"
import { BoxesIcon } from "lucide-react"

import { findNavItem } from "@/components/nav"
import { EmptyState } from "@/components/EmptyState"
import { PageHeader } from "@/components/PageHeader"
import { Badge } from "@/components/ui/badge"

const PHASE_SUMMARY: Record<number, string> = {
  2: "Asset management: full CRUD for physical and virtual assets, interfaces, relationships, locations, tags, and the inventory data table.",
  3: "IPAM: subnets, VLANs, network contexts, IP allocation workflows, and the interactive address grid.",
  4: "Services & dashboard: service inventory with endpoints and deployments, plus the enriched operational dashboard.",
  5: "Topology & portability: the React Flow network canvas, CSV import with validation, and JSON export.",
  6: "Integrations: read-only Proxmox and Docker discovery with sync history and source attribution.",
}

/**
 * Honest placeholder for modules that ship in a later phase. Clearly labeled as such —
 * never presented as functional.
 */
export function PlaceholderPage({ path }: { path: string }) {
  const item = findNavItem(path)
  const phase = item?.phase ?? 2

  return (
    <>
      <PageHeader
        title={item?.label ?? "Module"}
        description={`Planned for Phase ${phase} of the roadmap.`}
      >
        <Badge variant="outline" className="font-mono">
          Phase {phase}
        </Badge>
      </PageHeader>
      <EmptyState
        icon={item?.icon ?? BoxesIcon}
        title="Not implemented yet — by design"
        description={
          PHASE_SUMMARY[phase] ??
          "This module ships in a later phase of the implementation plan."
        }
      />
      <p className="mt-4 text-sm text-muted-foreground">
        The database schema and API surface for this module already exist — see{" "}
        <Link to="/activity" className="underline underline-offset-2">
          the activity log
        </Link>{" "}
        for what has happened so far, or{" "}
        <Link to="/" className="underline underline-offset-2">
          return to the dashboard
        </Link>
        .
      </p>
    </>
  )
}
