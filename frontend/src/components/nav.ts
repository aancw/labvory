import {
  ArrowLeftRightIcon,
  BoxesIcon,
  CircuitBoardIcon,
  ContainerIcon,
  CpuIcon,
  GlobeIcon,
  HardDriveIcon,
  LayersIcon,
  LayoutDashboardIcon,
  MapPinIcon,
  MonitorIcon,
  NetworkIcon,
  PlugIcon,
  ScrollTextIcon,
  ServerIcon,
  SettingsIcon,
  ShipWheelIcon,
  TagsIcon,
  WaypointsIcon,
  type LucideIcon,
} from "lucide-react"

export interface NavItem {
  label: string
  path: string
  icon: LucideIcon
  /** Phase when this module ships; undefined for Phase 1 features. */
  phase?: number
}

export interface NavSection {
  heading: string
  items: NavItem[]
}

// Navigation mirrors the spec's sidebar layout. Items without `phase` are live;
// the rest render honest phase placeholders until their phase lands.
export const NAV_SECTIONS: NavSection[] = [
  {
    heading: "Overview",
    items: [
      { label: "Dashboard", path: "/", icon: LayoutDashboardIcon },
      { label: "Activity", path: "/activity", icon: ScrollTextIcon },
    ],
  },
  {
    heading: "Infrastructure",
    items: [
      { label: "All Assets", path: "/assets", icon: BoxesIcon },
      { label: "Physical Devices", path: "/assets/physical", icon: ServerIcon },
      { label: "Virtual Machines", path: "/assets/virtual-machines", icon: MonitorIcon },
      { label: "Containers", path: "/assets/containers", icon: ContainerIcon },
      { label: "Kubernetes", path: "/kubernetes", icon: ShipWheelIcon },
      { label: "Services", path: "/services", icon: CircuitBoardIcon },
    ],
  },
  {
    heading: "Networking",
    items: [
      { label: "IP Addresses", path: "/ips", icon: NetworkIcon },
      { label: "Subnets", path: "/subnets", icon: HardDriveIcon },
      { label: "VLANs", path: "/vlans", icon: LayersIcon },
      { label: "Network Interfaces", path: "/interfaces", icon: WaypointsIcon },
      { label: "Topology", path: "/topology", icon: CpuIcon },
      { label: "Domains", path: "/domains", icon: GlobeIcon },
    ],
  },
  {
    heading: "Administration",
    items: [
      { label: "Locations", path: "/admin/locations", icon: MapPinIcon },
      { label: "Tags", path: "/admin/tags", icon: TagsIcon },
      { label: "Integrations", path: "/admin/integrations", icon: PlugIcon },
      { label: "Import & Export", path: "/admin/import", icon: ArrowLeftRightIcon },
      { label: "Settings", path: "/settings", icon: SettingsIcon },
    ],
  },
]

/** Flattened nav items with their section, used for breadcrumbs and lookup. */
export const NAV_ITEMS: NavItem[] = NAV_SECTIONS.flatMap((s) => s.items)

export function findNavItem(path: string): NavItem | undefined {
  return NAV_ITEMS.find((item) => item.path === path)
}
