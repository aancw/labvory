import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { api } from "./client"
import type {
  ActivityPage,
  AddressGrid,
  Asset,
  AssetDetail,
  AssetPage,
  AssetPayload,
  AssetRelationship,
  AssetType,
  BulkResult,
  DashboardStats,
  ApiToken,
  ApiTokenCreated,
  Domain,
  DomainDetail,
  ResolveDomainResult,
  Subdomain,
  SubdomainResolveResult,
  InterfacePage,
  IPHistoryRow,
  IPPage,
  Location,
  NetworkContext,
  NetworkInterface,
  SearchResults,
  Service,
  ServiceDetail,
  ServiceDeployment,
  ServiceEndpoint,
  SessionList,
  SettingsMap,
  SetupStatus,
  Subnet,
  SubnetDetail,
  SyncRun,
  Tag,
  TOTPSetup,
  TopologyGraph,
  User,
  Vlan,
  IPAddress,
  ImportRowResult,
  ImportSummary,
  Integration,
  RestoreSummary,
} from "./types"

// ---------- auth ----------

export function useMe() {
  return useQuery({
    queryKey: ["auth", "me"],
    queryFn: () => api.get<{ user: User }>("/api/v1/auth/me"),
    retry: false,
    staleTime: Number.POSITIVE_INFINITY,
  })
}

export function useSetupStatus() {
  return useQuery({
    queryKey: ["setup", "status"],
    queryFn: () => api.get<SetupStatus>("/api/v1/setup/status"),
    retry: false,
    staleTime: 10_000,
  })
}

export function useLogin() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: { username: string; password: string; totp_code?: string }) =>
      api.post<{ user: User }>("/api/v1/auth/login", input),
    onSuccess: () => qc.invalidateQueries(),
  })
}

// ---------- two-factor (TOTP) ----------

export function useTOTPSetup() {
  return useMutation({
    mutationFn: () => api.post<TOTPSetup>("/api/v1/auth/2fa/setup"),
  })
}

export function useTOTPEnable() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (code: string) => api.post<{ ok: boolean }>("/api/v1/auth/2fa/enable", { code }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["me"] }),
  })
}

export function useTOTPDisable() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: { password: string; code: string }) =>
      api.post<{ ok: boolean }>("/api/v1/auth/2fa/disable", input),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["me"] }),
  })
}

// ---------- sessions ----------

export function useSessions() {
  return useQuery({
    queryKey: ["sessions"],
    queryFn: () => api.get<SessionList>("/api/v1/auth/sessions"),
  })
}

export function useRevokeOtherSessions() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () => api.delete<{ ok: boolean; revoked: number }>("/api/v1/auth/sessions/others"),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["sessions"] }),
  })
}

export function useSetupAdmin() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: { username: string; display_name: string; password: string }) =>
      api.post<{ user: User }>("/api/v1/setup/admin", input),
    onSuccess: () => qc.invalidateQueries(),
  })
}

export function useLogout() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () => api.post<{ ok: boolean }>("/api/v1/auth/logout"),
    onSuccess: () => qc.clear(),
  })
}

export function useChangePassword() {
  return useMutation({
    mutationFn: (input: { current_password: string; new_password: string }) =>
      api.post<{ ok: boolean }>("/api/v1/auth/change-password", input),
  })
}

// ---------- application ----------

export function useDashboard() {
  return useQuery({
    queryKey: ["dashboard"],
    queryFn: () => api.get<DashboardStats>("/api/v1/dashboard"),
  })
}

export function useActivity(limit: number, offset: number) {
  return useQuery({
    queryKey: ["activity", limit, offset],
    queryFn: () =>
      api.get<ActivityPage>(`/api/v1/activity?limit=${limit}&offset=${offset}`),
  })
}

export function useSettings() {
  return useQuery({
    queryKey: ["settings"],
    queryFn: () => api.get<{ settings: SettingsMap }>("/api/v1/settings"),
  })
}

export function useTestNotification() {
  return useMutation({
    mutationFn: () => api.post<{ ok: boolean }>("/api/v1/settings/notify-test"),
  })
}

export function useUpdateSettings() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: { settings: SettingsMap }) =>
      api.put<{ settings: SettingsMap }>("/api/v1/settings", input),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["settings"] }),
  })
}

export function useSearch(query: string, enabled: boolean) {
  return useQuery({
    queryKey: ["search", query],
    queryFn: () =>
      api.get<SearchResults>(
        `/api/v1/search?q=${encodeURIComponent(query)}`,
      ),
    enabled: enabled && query.trim().length >= 2,
    placeholderData: (prev) => prev,
  })
}

// ---------- inventory (Phase 2) ----------

export interface AssetQuery {
  q?: string
  category?: string
  type_slugs?: string
  status?: string
  environment?: string
  location_id?: string
  parent_id?: string
  sort?: string
  order?: string
  limit?: number
  offset?: number
}

function assetQueryParams(query: AssetQuery): string {
  const params = new URLSearchParams()
  for (const [key, value] of Object.entries(query)) {
    if (value !== undefined && value !== "" && value !== null) {
      params.set(key, String(value))
    }
  }
  const s = params.toString()
  return s ? `?${s}` : ""
}

export function useAssets(query: AssetQuery) {
  return useQuery({
    queryKey: ["assets", query],
    queryFn: () => api.get<AssetPage>(`/api/v1/assets${assetQueryParams(query)}`),
    placeholderData: (prev) => prev,
  })
}

// Lightweight search for pickers (parent/relationship targets).
export function useAssetOptions(query: string, enabled: boolean, limit = 10) {
  return useQuery({
    queryKey: ["asset-options", query, limit],
    queryFn: () =>
      api.get<AssetPage>(`/api/v1/assets?q=${encodeURIComponent(query)}&limit=${limit}&sort=name&order=asc`),
    enabled: enabled,
    placeholderData: (prev) => prev,
  })
}

export function useAsset(id: string | undefined) {
  return useQuery({
    queryKey: ["asset", id],
    queryFn: () => api.get<AssetDetail>(`/api/v1/assets/${id}`),
    enabled: !!id,
  })
}

export function useAssetTypes() {
  return useQuery({
    queryKey: ["asset-types"],
    queryFn: () => api.get<{ types: AssetType[] }>("/api/v1/asset-types"),
    staleTime: Number.POSITIVE_INFINITY,
  })
}

function invalidateAssets(qc: ReturnType<typeof useQueryClient>) {
  void qc.invalidateQueries({ queryKey: ["assets"] })
  void qc.invalidateQueries({ queryKey: ["asset"] })
  void qc.invalidateQueries({ queryKey: ["dashboard"] })
  void qc.invalidateQueries({ queryKey: ["search"] })
}

export function useCreateAsset() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: AssetPayload) => api.post<{ asset: Asset }>("/api/v1/assets", input),
    onSuccess: () => invalidateAssets(qc),
  })
}

export function useUpdateAsset() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: { id: string; patch: Partial<AssetPayload> }) =>
      api.put<{ asset: Asset }>(`/api/v1/assets/${input.id}`, input.patch),
    onSuccess: () => invalidateAssets(qc),
  })
}

export function useDeleteAsset() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.delete<{ ok: boolean }>(`/api/v1/assets/${id}`),
    onSuccess: () => invalidateAssets(qc),
  })
}

export function useBulkAssets() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: {
      action: "delete" | "set_status" | "set_environment"
      ids: string[]
      status?: string
      environment?: string
    }) => api.post<BulkResult>("/api/v1/assets/bulk", input),
    onSuccess: () => invalidateAssets(qc),
  })
}

export function useInterfaces(assetId?: string) {
  return useQuery({
    queryKey: ["interfaces", assetId ?? "all"],
    queryFn: () =>
      assetId
        ? api.get<AssetDetail>(`/api/v1/assets/${assetId}`).then((d) => d.interfaces)
        : api.get<InterfacePage>("/api/v1/interfaces?limit=200").then((p) => p.interfaces),
  })
}

export function useCreateInterface() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: { assetId: string; payload: Omit<NetworkInterface, "id" | "asset_id" | "asset_name" | "ip_count" | "created_at" | "updated_at"> }) =>
      api.post<NetworkInterface>(`/api/v1/assets/${input.assetId}/interfaces`, input.payload),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["asset"] })
      void qc.invalidateQueries({ queryKey: ["interfaces"] })
    },
  })
}

export function useUpdateInterface() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: { id: string; payload: Record<string, unknown> }) =>
      api.put<NetworkInterface>(`/api/v1/interfaces/${input.id}`, input.payload),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["asset"] })
      void qc.invalidateQueries({ queryKey: ["interfaces"] })
    },
  })
}

export function useDeleteInterface() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.delete<{ ok: boolean }>(`/api/v1/interfaces/${id}`),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["asset"] })
      void qc.invalidateQueries({ queryKey: ["interfaces"] })
    },
  })
}

export function useCreateRelationship() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: { assetId: string; target_asset_id: string; kind: string; note?: string }) =>
      api.post<AssetRelationship>(`/api/v1/assets/${input.assetId}/relationships`, {
        target_asset_id: input.target_asset_id,
        kind: input.kind,
        note: input.note ?? "",
      }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["asset"] })
    },
  })
}

export function useDeleteRelationship() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.delete<{ ok: boolean }>(`/api/v1/relationships/${id}`),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["asset"] })
    },
  })
}

export function useLocations() {
  return useQuery({
    queryKey: ["locations"],
    queryFn: () => api.get<{ locations: Location[] }>("/api/v1/locations"),
  })
}

export function useCreateLocation() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: { name: string; description: string; parent_id: string | null }) =>
      api.post<Location>("/api/v1/locations", input),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["locations"] }),
  })
}

export function useUpdateLocation() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: { id: string; name: string; description: string; parent_id: string | null }) =>
      api.put<Location>(`/api/v1/locations/${input.id}`, input),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["locations"] })
      void qc.invalidateQueries({ queryKey: ["assets"] })
    },
  })
}

export function useDeleteLocation() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.delete<{ ok: boolean }>(`/api/v1/locations/${id}`),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["locations"] }),
  })
}

export function useTags() {
  return useQuery({
    queryKey: ["tags"],
    queryFn: () => api.get<{ tags: Tag[] }>("/api/v1/tags"),
  })
}

export function useCreateTag() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: { name: string; color: string }) => api.post<Tag>("/api/v1/tags", input),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["tags"] }),
  })
}

export function useUpdateTag() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: { id: string; name: string; color: string }) =>
      api.put<Tag>(`/api/v1/tags/${input.id}`, input),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["tags"] })
      void qc.invalidateQueries({ queryKey: ["assets"] })
    },
  })
}

export function useDeleteTag() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.delete<{ ok: boolean }>(`/api/v1/tags/${id}`),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["tags"] })
      void qc.invalidateQueries({ queryKey: ["assets"] })
    },
  })
}

// ---------- IPAM (Phase 3) ----------

export function useSubnets(filters: { q?: string; context_id?: string; vlan_id?: string } = {}) {
  const params = new URLSearchParams()
  for (const [k, v] of Object.entries(filters)) if (v) params.set(k, v)
  const s = params.toString()
  return useQuery({
    queryKey: ["subnets", filters],
    queryFn: () => api.get<{ subnets: Subnet[] }>(`/api/v1/subnets${s ? `?${s}` : ""}`),
  })
}

export function useSubnet(id: string | undefined) {
  return useQuery({
    queryKey: ["subnet", id],
    queryFn: () => api.get<SubnetDetail>(`/api/v1/subnets/${id}`),
    enabled: !!id,
  })
}

export function useAddressGrid(id: string | undefined) {
  return useQuery({
    queryKey: ["subnet-grid", id],
    queryFn: () => api.get<AddressGrid>(`/api/v1/subnets/${id}/grid`),
    enabled: !!id,
  })
}

export function useNextAvailable(id: string | undefined, count = 10) {
  return useQuery({
    queryKey: ["subnet-next", id, count],
    queryFn: () => api.get<{ addresses: string[] }>(`/api/v1/subnets/${id}/next-available?count=${count}`),
    enabled: !!id,
  })
}

export function useCreateSubnet() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (payload: Record<string, unknown>) => api.post<{ subnet: Subnet }>("/api/v1/subnets", payload),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["subnets"] })
      void qc.invalidateQueries({ queryKey: ["dashboard"] })
      void qc.invalidateQueries({ queryKey: ["search"] })
    },
  })
}

export function useUpdateSubnet() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: { id: string; payload: Record<string, unknown> }) =>
      api.put<{ subnet: Subnet }>(`/api/v1/subnets/${input.id}`, input.payload),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["subnets"] })
      void qc.invalidateQueries({ queryKey: ["subnet"] })
    },
  })
}

export function useDeleteSubnet() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.delete<{ ok: boolean }>(`/api/v1/subnets/${id}`),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["subnets"] })
      void qc.invalidateQueries({ queryKey: ["dashboard"] })
    },
  })
}

export function useAllocateIP() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: { subnetId: string; payload: Record<string, unknown> }) =>
      api.post<IPAddress>(`/api/v1/subnets/${input.subnetId}/addresses`, input.payload),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["subnet"] })
      void qc.invalidateQueries({ queryKey: ["subnet-grid"] })
      void qc.invalidateQueries({ queryKey: ["subnets"] })
      void qc.invalidateQueries({ queryKey: ["ips"] })
      void qc.invalidateQueries({ queryKey: ["dashboard"] })
      void qc.invalidateQueries({ queryKey: ["search"] })
    },
  })
}

export function useBulkIP() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: { subnetId: string; payload: Record<string, unknown> }) =>
      api.post<{ affected: number }>(`/api/v1/subnets/${input.subnetId}/addresses/bulk`, input.payload),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["subnet"] })
      void qc.invalidateQueries({ queryKey: ["subnet-grid"] })
      void qc.invalidateQueries({ queryKey: ["subnets"] })
      void qc.invalidateQueries({ queryKey: ["ips"] })
    },
  })
}

export function useUpdateIP() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: { id: string; payload: Record<string, unknown> }) =>
      api.put<IPAddress>(`/api/v1/ip-addresses/${input.id}`, input.payload),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["subnet"] })
      void qc.invalidateQueries({ queryKey: ["subnet-grid"] })
      void qc.invalidateQueries({ queryKey: ["subnets"] })
      void qc.invalidateQueries({ queryKey: ["ips"] })
    },
  })
}

export function useReleaseIP() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.post<IPAddress>(`/api/v1/ip-addresses/${id}/release`),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["subnet"] })
      void qc.invalidateQueries({ queryKey: ["subnet-grid"] })
      void qc.invalidateQueries({ queryKey: ["subnets"] })
      void qc.invalidateQueries({ queryKey: ["ips"] })
    },
  })
}

export function useReleaseIPsBulk() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (ids: string[]) =>
      api.post<{ ok: boolean; released: number }>(`/api/v1/ip-addresses/release-bulk`, { ids }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["subnet"] })
      void qc.invalidateQueries({ queryKey: ["subnet-grid"] })
      void qc.invalidateQueries({ queryKey: ["subnets"] })
      void qc.invalidateQueries({ queryKey: ["ips"] })
    },
  })
}

export function useDeleteIPsBulk() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (ids: string[]) =>
      api.post<{ ok: boolean; deleted: number }>(`/api/v1/ip-addresses/delete-bulk`, { ids }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["subnet"] })
      void qc.invalidateQueries({ queryKey: ["subnet-grid"] })
      void qc.invalidateQueries({ queryKey: ["subnets"] })
      void qc.invalidateQueries({ queryKey: ["ips"] })
    },
  })
}

export function useDeleteIP() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.delete<{ ok: boolean }>(`/api/v1/ip-addresses/${id}`),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["subnet"] })
      void qc.invalidateQueries({ queryKey: ["subnet-grid"] })
      void qc.invalidateQueries({ queryKey: ["subnets"] })
      void qc.invalidateQueries({ queryKey: ["ips"] })
    },
  })
}

export function useIPHistory(ipID: string | undefined, open: boolean) {
  return useQuery({
    queryKey: ["ip-history", ipID],
    queryFn: () => api.get<{ history: IPHistoryRow[] }>(`/api/v1/ip-addresses/${ipID}/history`),
    enabled: !!ipID && open,
  })
}

export function useIPs(filters: { status?: string; q?: string; observed_conflict?: boolean; limit?: number; offset?: number } = {}) {
  const params = new URLSearchParams()
  for (const [k, v] of Object.entries(filters)) {
    if (v !== undefined && v !== "") params.set(k, String(v))
  }
  const s = params.toString()
  return useQuery({
    queryKey: ["ips", filters],
    queryFn: () => api.get<IPPage>(`/api/v1/ip-addresses${s ? `?${s}` : ""}`),
    placeholderData: (prev) => prev,
  })
}

export function useVlans() {
  return useQuery({
    queryKey: ["vlans"],
    queryFn: () => api.get<{ vlans: Vlan[] }>("/api/v1/vlans"),
  })
}

export function useCreateVlan() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (payload: { context_id: string; vlan_id: number; name: string; purpose: string; description: string }) =>
      api.post<Vlan>("/api/v1/vlans", payload),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["vlans"] })
      void qc.invalidateQueries({ queryKey: ["contexts"] })
    },
  })
}

export function useUpdateVlan() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: { id: string; payload: { vlan_id: number; name: string; purpose: string; description: string } }) =>
      api.put<Vlan>(`/api/v1/vlans/${input.id}`, input.payload),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["vlans"] })
      void qc.invalidateQueries({ queryKey: ["subnets"] })
    },
  })
}

export function useDeleteVlan() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.delete<{ ok: boolean }>(`/api/v1/vlans/${id}`),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["vlans"] })
      void qc.invalidateQueries({ queryKey: ["subnets"] })
    },
  })
}

export function useNetworkContexts() {
  return useQuery({
    queryKey: ["contexts"],
    queryFn: () => api.get<{ contexts: NetworkContext[] }>("/api/v1/network-contexts"),
  })
}

export function useCreateNetworkContext() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (payload: { name: string; description: string }) =>
      api.post<NetworkContext>("/api/v1/network-contexts", payload),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["contexts"] }),
  })
}

export function useUpdateNetworkContext() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: { id: string; payload: { name: string; description: string } }) =>
      api.put<NetworkContext>(`/api/v1/network-contexts/${input.id}`, input.payload),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["contexts"] }),
  })
}

// ---------- user management (admin) ----------

export function useUsers() {
  return useQuery({
    queryKey: ["users"],
    queryFn: () => api.get<{ users: User[] }>("/api/v1/users"),
  })
}

export function useCreateUser() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (payload: { username: string; display_name: string; password: string; role: string }) =>
      api.post<{ user: User }>("/api/v1/users", payload),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["users"] }),
  })
}

export function useUpdateUser() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: { id: string; payload: { display_name?: string; role?: string; password?: string } }) =>
      api.put<{ ok: boolean }>(`/api/v1/users/${input.id}`, input.payload),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["users"] })
      void qc.invalidateQueries({ queryKey: ["me"] })
    },
  })
}

export function useDeleteUser() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.delete<{ ok: boolean }>(`/api/v1/users/${id}`),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["users"] }),
  })
}

// ---------- asset reachability check ----------

export function useCheckAsset() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) =>
      api.post<{ ok: boolean; reachable: boolean; target: string; status: string }>(
        `/api/v1/assets/${id}/check`,
      ),
    onSuccess: (_data, id) => void qc.invalidateQueries({ queryKey: ["asset", id] }),
  })
}

// ---------- services (Phase 4) ----------

export function useServices(filters: { q?: string; category?: string; environment?: string; deployment_type?: string } = {}) {
  const params = new URLSearchParams()
  for (const [k, v] of Object.entries(filters)) if (v) params.set(k, v)
  const s = params.toString()
  return useQuery({
    queryKey: ["services", filters],
    queryFn: () => api.get<{ services: Service[] }>(`/api/v1/services${s ? `?${s}` : ""}`),
    placeholderData: (prev) => prev,
  })
}

export function useService(id: string | undefined) {
  return useQuery({
    queryKey: ["service", id],
    queryFn: () => api.get<ServiceDetail>(`/api/v1/services/${id}`),
    enabled: !!id,
  })
}

export function useCreateService() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (payload: Record<string, unknown>) => api.post<{ service: Service }>("/api/v1/services", payload),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["services"] })
      void qc.invalidateQueries({ queryKey: ["dashboard"] })
      void qc.invalidateQueries({ queryKey: ["search"] })
    },
  })
}

export function useUpdateService() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: { id: string; payload: Record<string, unknown> }) =>
      api.put<{ service: Service }>(`/api/v1/services/${input.id}`, input.payload),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["service"] })
      void qc.invalidateQueries({ queryKey: ["services"] })
    },
  })
}

export function useDeleteService() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.delete<{ ok: boolean }>(`/api/v1/services/${id}`),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["services"] })
      void qc.invalidateQueries({ queryKey: ["dashboard"] })
    },
  })
}

export function useCreateServiceEndpoint() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: { serviceId: string; payload: Record<string, unknown> }) =>
      api.post<ServiceEndpoint>(`/api/v1/services/${input.serviceId}/endpoints`, input.payload),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["service"] }),
  })
}

export function useDeleteServiceEndpoint() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.delete<{ ok: boolean }>(`/api/v1/service-endpoints/${id}`),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["service"] }),
  })
}

export function useToggleEndpointHealth() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: { id: string; enabled: boolean }) =>
      api.put<{ ok: boolean; health_enabled: boolean }>(`/api/v1/service-endpoints/${input.id}/health`, {
        enabled: input.enabled,
      }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["service"] })
      void qc.invalidateQueries({ queryKey: ["stats"] })
    },
  })
}

export function useCreateServiceDeployment() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: { serviceId: string; payload: { asset_id: string; deployment_type: string; notes?: string } }) =>
      api.post<ServiceDeployment>(`/api/v1/services/${input.serviceId}/deployments`, input.payload),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["service"] })
      void qc.invalidateQueries({ queryKey: ["services"] })
    },
  })
}

export function useDeleteServiceDeployment() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.delete<{ ok: boolean }>(`/api/v1/service-deployments/${id}`),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["service"] })
      void qc.invalidateQueries({ queryKey: ["services"] })
    },
  })
}

export function useActivityFiltered(
  limit: number,
  offset: number,
  filters: { entity_type?: string | undefined; action?: string | undefined },
) {
  const params = new URLSearchParams({ limit: String(limit), offset: String(offset) })
  for (const [k, v] of Object.entries(filters)) if (v) params.set(k, v)
  return useQuery({
    queryKey: ["activity-filtered", limit, offset, filters],
    queryFn: () => api.get<ActivityPage>(`/api/v1/activity?${params.toString()}`),
    placeholderData: (prev) => prev,
  })
}

// ---------- topology + portability (Phase 5) ----------

export function useTopology() {
  return useQuery({
    queryKey: ["topology"],
    queryFn: () => api.get<TopologyGraph>("/api/v1/topology"),
  })
}

export function useImportPreview() {
  return useMutation({
    mutationFn: (payload: { entity: string; rows: Record<string, unknown>[]; context_id?: string; subnet_id?: string }) =>
      api.post<{ rows: ImportRowResult[] }>("/api/v1/import/preview", payload),
  })
}

export function useImportCommit() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (payload: {
      entity: string
      rows: Record<string, unknown>[]
      context_id?: string
      subnet_id?: string
      skip_duplicates: boolean
    }) => api.post<ImportSummary>("/api/v1/import/commit", payload),
    onSuccess: () => {
      void qc.invalidateQueries()
    },
  })
}

export function useRestoreInventory() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (inventory: Record<string, unknown>) =>
      api.post<RestoreSummary>("/api/v1/import/restore", { inventory }),
    onSuccess: () => void qc.invalidateQueries(),
  })
}

// ---------- integrations (Phase 6) ----------

export function useIntegrations() {
  return useQuery({
    queryKey: ["integrations"],
    queryFn: () =>
      api.get<{
        integrations: Integration[]
        implemented: string[]
        planned: string[]
        secrets_ready: boolean
      }>("/api/v1/integrations"),
  })
}

export function useCreateIntegration() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (payload: { kind: string; name: string; description: string; enabled: boolean; config: Record<string, unknown>; secrets?: Record<string, string>; sync_interval_seconds?: number | null }) =>
      api.post<Integration>("/api/v1/integrations", payload),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["integrations"] }),
  })
}

export function useUpdateIntegration() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: { id: string; payload: { name: string; description: string; enabled: boolean; config: Record<string, unknown>; secrets?: Record<string, string>; sync_interval_seconds?: number | null } }) =>
      api.put<Integration>(`/api/v1/integrations/${input.id}`, input.payload),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["integrations"] }),
  })
}

export function useDeleteIntegration() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.delete<{ ok: boolean }>(`/api/v1/integrations/${id}`),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["integrations"] }),
  })
}

export function useSyncIntegration() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.post<{ summary: Record<string, unknown> }>(`/api/v1/integrations/${id}/sync`),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["integrations"] })
      void qc.invalidateQueries({ queryKey: ["sync-runs"] })
      void qc.invalidateQueries({ queryKey: ["assets"] })
      void qc.invalidateQueries({ queryKey: ["dashboard"] })
    },
  })
}

export function useSyncRuns(integrationId: string | undefined, enabled: boolean) {
  return useQuery({
    queryKey: ["sync-runs", integrationId],
    queryFn: () => api.get<{ runs: SyncRun[] }>(`/api/v1/integrations/${integrationId}/sync-runs`),
    enabled: !!integrationId && enabled,
  })
}

// ---------- API tokens (bearer credentials for agents / scripts) ----------

export function useApiTokens() {
  return useQuery({
    queryKey: ["api-tokens"],
    queryFn: () => api.get<{ tokens: ApiToken[] }>("/api/v1/tokens"),
  })
}

export function useCreateApiToken() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: { name: string; purpose: "mcp" | "api" }) =>
      api.post<ApiTokenCreated>("/api/v1/tokens", input),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["api-tokens"] }),
  })
}

export function useRevokeApiToken() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) =>
      api.delete<{ revoked: boolean }>(`/api/v1/tokens/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["api-tokens"] }),
  })
}

// ---------- DNS inventory (domains & subdomains) ----------

export function useDomains() {
  return useQuery({
    queryKey: ["domains"],
    queryFn: () => api.get<{ domains: Domain[] }>("/api/v1/domains"),
  })
}

export function useDomain(id: string | undefined) {
  return useQuery({
    queryKey: ["domains", id],
    queryFn: () => api.get<DomainDetail>(`/api/v1/domains/${id}`),
    enabled: !!id,
  })
}

export function useCreateDomain() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: { name: string; registrar?: string; notes?: string; expires_on?: string | null }) =>
      api.post<Domain>("/api/v1/domains", input),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["domains"] }),
  })
}

export function useUpdateDomain() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: { id: string; name: string; registrar?: string; notes?: string; expires_on?: string | null }) =>
      api.put<Domain>(`/api/v1/domains/${input.id}`, input),
    onSuccess: (_data, vars) => {
      void qc.invalidateQueries({ queryKey: ["domains"] })
      void qc.invalidateQueries({ queryKey: ["domains", vars.id] })
    },
  })
}

export function useDeleteDomain() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.delete<{ deleted: boolean }>(`/api/v1/domains/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["domains"] }),
  })
}

export function useCreateSubdomain() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: {
      domainId: string
      label: string
      record_type: string
      value: string
      ttl?: number | null
    }) => api.post<Subdomain>(`/api/v1/domains/${input.domainId}/subdomains`, input),
    onSuccess: (_data, vars) => {
      void qc.invalidateQueries({ queryKey: ["domains", vars.domainId] })
      void qc.invalidateQueries({ queryKey: ["domains"] })
    },
  })
}

export function useUpdateSubdomain() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: {
      id: string
      domainId: string
      label: string
      record_type: string
      value: string
      ttl?: number | null
    }) => api.put<Subdomain>(`/api/v1/subdomains/${input.id}`, input),
    onSuccess: (_data, vars) => {
      void qc.invalidateQueries({ queryKey: ["domains", vars.domainId] })
    },
  })
}

export function useDeleteSubdomain() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: { id: string; domainId: string }) =>
      api.delete<{ deleted: boolean }>(`/api/v1/subdomains/${input.id}`),
    onSuccess: (_data, vars) => {
      void qc.invalidateQueries({ queryKey: ["domains", vars.domainId] })
      void qc.invalidateQueries({ queryKey: ["domains"] })
    },
  })
}

export function useResolveSubdomain() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: { id: string; domainId: string }) =>
      api.post<SubdomainResolveResult>(`/api/v1/subdomains/${input.id}/resolve`),
    onSuccess: (_data, vars) => {
      void qc.invalidateQueries({ queryKey: ["domains", vars.domainId] })
    },
  })
}

export function useResolveDomain() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (domainId: string) =>
      api.post<ResolveDomainResult>(`/api/v1/domains/${domainId}/resolve`),
    onSuccess: (_data, domainId) => {
      void qc.invalidateQueries({ queryKey: ["domains", domainId] })
    },
  })
}
