// API contract types mirroring backend JSON payloads.

export interface User {
  id: string
  username: string
  display_name: string
  role: string
  created_at: string
  updated_at: string
}

export interface SetupStatus {
  needs_setup: boolean
}

export interface AssetSummary {
  total: number
  physical: number
  virtual: number
  vms: number
  containers: number
  docker_hosts: number
  online: number
  offline: number
  maintenance: number
  retired: number
  unknown: number
}

export interface NetworkSummary {
  subnets: number
  vlans: number
  ip_tracked: number
  ip_assigned: number
  ip_reserved: number
  ip_available: number
  ip_conflicts: number
}

export interface AuditEvent {
  id: string
  occurred_at: string
  actor_user_id?: string
  actor_label: string
  entity_type: string
  entity_id?: string
  entity_label: string
  action: string
  changes?: Record<string, unknown>
  source_ip?: string
}

export interface AssetStub {
  id: string
  name: string
  hostname: string
  type_label: string
  category: string
  status: string
  updated_at: string
}

export interface DashboardStats {
  assets: AssetSummary
  network: NetworkSummary
  services: number
  locations: number
  tags: number
  recent_activity: AuditEvent[]
  recently_updated_assets: AssetStub[]
}

export interface ActivityPage {
  events: AuditEvent[]
  total: number
  limit: number
  offset: number
}

export type SettingsMap = Record<string, unknown>

export interface SearchHit {
  id: string
  title: string
  subtitle: string
  entity_type: "asset" | "ip" | "subnet" | "vlan" | "service" | "location"
  icon: string
}

export interface SearchResults {
  assets: SearchHit[]
  ips: SearchHit[]
  subnets: SearchHit[]
  vlans: SearchHit[]
  services: SearchHit[]
  locations: SearchHit[]
}

// ---------- Phase 2: inventory ----------

export interface AssetType {
  id: string
  slug: string
  label: string
  category: "physical" | "virtual"
  icon: string
}

export interface Asset {
  id: string
  name: string
  hostname: string
  type_id: string
  type_label: string
  type_slug: string
  category: "physical" | "virtual"
  icon: string
  status: string
  lifecycle: string
  environment: string
  location_id?: string | null
  location_name?: string | null
  parent_id?: string | null
  parent_name?: string | null
  manufacturer: string
  model: string
  serial_number: string
  os_name: string
  os_version: string
  cpu_model: string
  cpu_cores?: number | null
  ram_gb?: number | null
  storage_gb?: number | null
  purchase_date?: string | null
  purchase_cost?: number | null
  warranty_expires?: string | null
  notes: string
  source: string
  /** Integration-synced observed state; connectors own the shape (e.g. metadata.kubernetes). */
  metadata?: {
    kubernetes?: {
      roles?: string[]
      kubelet?: string
      runtime?: string
      namespace?: string
      node?: string
      images?: string[]
      phase?: string
    }
    observed?: { status?: string; checked_at?: string }
  } & Record<string, unknown>
  primary_ip?: string | null
  child_count: number
  tags: Tag[]
  created_at: string
  updated_at: string
}

export interface AssetPage {
  assets: Asset[]
  total: number
  limit: number
  offset: number
}

export interface Tag {
  id: string
  name: string
  color: string
  usage?: number
}

export interface Location {
  id: string
  name: string
  slug: string
  description: string
  parent_id?: string | null
  asset_count: number
  child_count: number
  created_at: string
  updated_at: string
}

export interface NetworkInterface {
  id: string
  asset_id: string
  asset_name?: string
  name: string
  kind: string
  mac_address?: string | null
  mtu?: number | null
  speed_mbps?: number | null
  notes: string
  ip_count: number
  created_at: string
  updated_at: string
}

export interface InterfacePage {
  interfaces: NetworkInterface[]
  total: number
  limit: number
  offset: number
}

export interface AssetRelationship {
  id: string
  source_asset_id: string
  target_asset_id: string
  kind: string
  note: string
  direction: "outgoing" | "incoming"
  other_asset: {
    id: string
    name: string
    hostname: string
    type_label: string
    category: string
    status: string
  }
  created_at: string
}

export interface AssetDetail {
  asset: Asset
  interfaces: NetworkInterface[]
  relationships: AssetRelationship[]
  children: Asset[]
  activity: AuditEvent[]
}

export interface AssetPayload {
  name: string
  hostname: string
  type_id: string
  status: string
  lifecycle: string
  environment: string
  location_id: string | null
  parent_id: string | null
  manufacturer: string
  model: string
  serial_number: string
  os_name: string
  os_version: string
  cpu_model: string
  cpu_cores: number | null
  ram_gb: number | null
  storage_gb: number | null
  purchase_date: string | null
  purchase_cost: number | null
  warranty_expires: string | null
  notes: string
  tag_ids: string[]
}

export interface BulkResult {
  ok: boolean
  affected?: number
}

// ---------- Phase 3: IPAM ----------

export interface NetworkContext {
  id: string
  name: string
  slug: string
  description: string
  subnet_count: number
  vlan_count: number
  created_at: string
}

export interface Vlan {
  id: string
  context_id: string
  context_name: string
  vlan_id: number
  name: string
  purpose: string
  description: string
  subnet_count: number
  created_at: string
}

export interface Subnet {
  id: string
  cidr: string
  name: string
  description: string
  context_id: string
  context_name: string
  vlan_id?: string | null
  vlan_name?: string | null
  vlan_number?: number | null
  gateway?: string | null
  dns_servers: string[]
  dhcp_start?: string | null
  dhcp_end?: string | null
  location_id?: string | null
  location_name?: string | null
  environment: string
  usable_count: number
  assigned: number
  reserved: number
  excluded: number
  conflicts: number
  available: number
  utilization: number
  created_at: string
  updated_at: string
}

export interface IPAddress {
  id: string
  subnet_id: string
  subnet_cidr?: string
  address: string
  status: string
  assignment_method?: string | null
  hostname: string
  description: string
  interface_id?: string | null
  asset_id?: string | null
  asset_name?: string | null
  last_observed_at?: string | null
  created_at: string
  updated_at: string
}

export interface SubnetDetail {
  subnet: Subnet
  addresses: IPAddress[]
  history?: AuditEvent[]
  grid_limit: number
}

export interface GridCell {
  address: string
  status: string
  ip_id?: string | null
  hostname?: string
}

export interface AddressGrid {
  cidr: string
  usable: number
  first: string
  last: string
  cells: GridCell[]
  truncated: boolean
  next_available: string[]
}

export interface IPHistoryRow {
  id: string
  assigned_at: string
  released_at?: string | null
  method?: string | null
  note?: string | null
  asset_id?: string | null
  asset_name?: string | null
  assigned_by?: string | null
}

export interface IPPage {
  addresses: IPAddress[]
  total: number
  limit: number
  offset: number
}

// ---------- Phase 4: services ----------

export interface Service {
  id: string
  name: string
  slug: string
  description: string
  category: string
  deployment_type: string
  environment: string
  status: string
  repo_url: string
  docs_url: string
  notes: string
  host_count: number
  created_at: string
  updated_at: string
}

export interface ServiceEndpoint {
  id: string
  service_id: string
  label: string
  protocol: string
  port?: number | null
  url: string
  is_external: boolean
}

export interface ServiceDeployment {
  id: string
  service_id: string
  asset_id: string
  asset_name: string
  asset_hostname: string
  asset_category: string
  deployment_type: string
  notes: string
  created_at: string
}

export interface ServiceDetail {
  service: Service
  endpoints: ServiceEndpoint[]
  deployments: ServiceDeployment[]
  activity: AuditEvent[]
}

export interface DashboardStatsV2 extends DashboardStats {
  by_os: { os: string; count: number }[]
  services_by_host: { asset_id: string; asset_name: string; services: number }[]
  subnet_utilization: { id: string; cidr: string; name: string; used: number; usable: number; utilization: number }[]
  alerts: {
    ip_conflicts: number
    warranty_expiring: number
    services_without_host: number
  }
}

// ---------- Phase 5: topology + portability ----------

export interface TopologyNode {
  id: string
  name: string
  hostname: string
  category: string
  type_slug: string
  type_label: string
  status: string
  parent_id?: string
  primary_ip?: string
}

export interface TopologyEdge {
  id: string
  source: string
  target: string
  kind: "hosts" | "runs_on" | "connected_to" | "depends_on"
  note?: string
  source_port?: string
  target_port?: string
}

export interface TopologyGraph {
  nodes: TopologyNode[]
  edges: TopologyEdge[]
}

export interface ImportRowResult {
  index: number
  status: "ok" | "error" | "duplicate"
  detail?: string
  data: Record<string, unknown>
}

export interface ImportSummary {
  created: number
  skipped: number
  errors?: string[]
}

export interface RestoreSummary {
  created: Record<string, number>
  total_created: number
}

// ---------- Phase 6: integrations ----------

export interface Integration {
  id: string
  kind: string
  name: string
  description: string
  enabled: boolean
  config: {
    endpoint?: string
    token_id?: string
    verify_tls?: boolean
    host?: string
    asset_id?: string
    asset_ids?: string[]
    timeout_seconds?: number
    secrets_configured?: Record<string, boolean>
    secrets_encrypted?: boolean
    [key: string]: unknown
  }
  last_sync_at?: string | null
  created_at: string
  updated_at: string
  implemented: boolean
}

export interface SyncRun {
  id: string
  integration_id: string
  started_at: string
  finished_at?: string | null
  status: "running" | "success" | "failed" | "partial"
  summary?: Record<string, unknown>
  message: string
}

export interface ApiToken {
  id: string
  user_id: string
  name: string
  // "mcp" (MCP endpoint only) or "api" (REST API bearer only).
  purpose: "mcp" | "api" | string
  created_at: string
  last_used_at?: string | null
  revoked_at?: string | null
}

export interface ApiTokenCreated extends ApiToken {
  // Plaintext bearer value (prefix "lv_"), returned exactly once at creation.
  token: string
}

// ---------- DNS inventory (domains & subdomains) ----------

export interface Domain {
  id: string
  name: string
  registrar: string
  expires_on?: string | null
  notes: string
  subdomain_count: number
  created_at: string
  updated_at: string
}

export interface Subdomain {
  id: string
  domain_id: string
  label: string
  fqdn: string
  record_type: string
  value: string
  ttl?: number | null
  ip_address_id?: string | null
  service_id?: string | null
  observed_value: string
  observed_at?: string | null
  created_at: string
  updated_at: string
}

export interface DomainDetail {
  domain: Domain
  subdomains: Subdomain[]
}

export interface SubdomainResolveResult {
  subdomain: Subdomain
  drift: boolean
  error?: string
}

export interface ResolveDomainResult {
  resolved: number
  drifted: number
  failed: number
  total: number
}
