import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { useEffect, type ReactNode } from "react"
import {
  createBrowserRouter,
  Navigate,
  RouterProvider,
  useLocation,
} from "react-router-dom"

import { isApiError, isSessionInvalid } from "@/api/client"
import { useMe } from "@/api/hooks"
import { ThemeProvider } from "@/components/theme-provider"
import { AppShell } from "@/components/layout/Topbar"
import { Toaster } from "@/components/ui/sonner"
import { Spinner } from "@/components/Spinner"
import { ActivityPage } from "@/pages/ActivityPage"
import { AssetDetailPage } from "@/pages/AssetDetailPage"
import { AssetListPage } from "@/pages/AssetListPage"
import { DashboardPage } from "@/pages/DashboardPage"
import { InterfacesPage } from "@/pages/InterfacesPage"
import { IPAddressesPage } from "@/pages/IPAddressesPage"
import { LocationsPage } from "@/pages/LocationsPage"
import { ImportExportPage } from "@/pages/ImportExportPage"
import { IntegrationsPage } from "@/pages/IntegrationsPage"
import { KubernetesPage } from "@/pages/KubernetesPage"
import { LoginPage } from "@/pages/LoginPage"
import { ServiceDetailPage } from "@/pages/ServiceDetailPage"
import { ServicesPage } from "@/pages/ServicesPage"
import { NotFoundPage } from "@/pages/NotFoundPage"
import { SettingsPage } from "@/pages/SettingsPage"
import { SetupPage } from "@/pages/SetupPage"
import { TopologyPage } from "@/pages/TopologyPage"
import { SubnetDetailPage } from "@/pages/SubnetDetailPage"
import { SubnetsPage } from "@/pages/SubnetsPage"
import { TagsPage } from "@/pages/TagsPage"
import { DomainsPage } from "@/pages/DomainsPage"
import { VlansPage } from "@/pages/VlansPage"

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 30_000,
      retry: (failureCount, error) => {
        // Never retry client errors (4xx) — including auth failures.
        if (isApiError(error) && error.status >= 400 && error.status < 500) return false
        return failureCount < 2
      },
    },
  },
})

/** Blocks rendering until the session probe resolves; redirects when unauthenticated. */
function RequireAuth({ children }: { children: ReactNode }) {
  const me = useMe()
  const location = useLocation()

  if (me.isPending) return <FullScreenSpinner />

  if (me.isError) {
    // 401 (signed out) and "session lookup failed" both mean the session is
    // unusable — send the user to the login screen instead of spinning forever.
    if (isSessionInvalid(me.error)) {
      return <Navigate to="/login" replace state={{ from: location.pathname }} />
    }
    if (isApiError(me.error) && me.error.status >= 500) {
      // Server problem, not an auth problem — surface it on the login screen.
      return <Navigate to="/login" replace state={{ from: location.pathname }} />
    }
    return <FullScreenSpinner />
  }
  return <>{children}</>
}

function RequireNoAuth({ children }: { children: ReactNode }) {
  const me = useMe()
  if (me.isPending) return <FullScreenSpinner />
  if (me.isSuccess) return <Navigate to="/" replace />
  return <>{children}</>
}

function FullScreenSpinner() {
  return (
    <div className="flex min-h-screen items-center justify-center">
      <Spinner className="size-6 text-muted-foreground" />
    </div>
  )
}

const router = createBrowserRouter([
  {
    path: "/login",
    element: (
      <RequireNoAuth>
        <LoginPage />
      </RequireNoAuth>
    ),
  },
  {
    path: "/setup",
    element: (
      <RequireNoAuth>
        <SetupPage />
      </RequireNoAuth>
    ),
  },
  {
    element: (
      <RequireAuth>
        <AppShell />
      </RequireAuth>
    ),
    children: [
      { index: true, element: <DashboardPage /> },
      { path: "activity", element: <ActivityPage /> },
      { path: "settings", element: <SettingsPage /> },

      // Inventory & modules.
      { path: "assets", element: <AssetListPage /> },
      { path: "assets/physical", element: <AssetListPage /> },
      { path: "assets/virtual-machines", element: <AssetListPage /> },
      { path: "assets/containers", element: <AssetListPage /> },
      { path: "assets/:id", element: <AssetDetailPage /> },
      { path: "kubernetes", element: <KubernetesPage /> },
      { path: "interfaces", element: <InterfacesPage /> },
      { path: "subnets", element: <SubnetsPage /> },
      { path: "subnets/:id", element: <SubnetDetailPage /> },
      { path: "ips", element: <IPAddressesPage /> },
      { path: "vlans", element: <VlansPage /> },
      { path: "services", element: <ServicesPage /> },
      { path: "services/:id", element: <ServiceDetailPage /> },
      { path: "topology", element: <TopologyPage /> },
      { path: "domains", element: <DomainsPage /> },
      { path: "admin/integrations", element: <IntegrationsPage /> },
      { path: "admin/import", element: <ImportExportPage /> },
      { path: "admin/locations", element: <LocationsPage /> },
      { path: "admin/tags", element: <TagsPage /> },

      { path: "*", element: <NotFoundPage /> },
    ],
  },
])

export default function App() {
  // Keep the query cache fresh across tabs/refreshes of long-lived sessions.
  useEffect(() => {
    const onVisible = () => {
      if (document.visibilityState === "visible") {
        void queryClient.invalidateQueries({ queryKey: ["auth", "me"] })
      }
    }
    document.addEventListener("visibilitychange", onVisible)
    return () => document.removeEventListener("visibilitychange", onVisible)
  }, [])

  return (
    <ThemeProvider>
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
        <Toaster />
      </QueryClientProvider>
    </ThemeProvider>
  )
}
