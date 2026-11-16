import { useEffect, useState } from "react"
import { Outlet, useNavigate } from "react-router-dom"
import { toast } from "sonner"
import { LogOutIcon, MenuIcon, PlusIcon, SearchIcon, UserCircleIcon } from "lucide-react"

import { CommandPalette } from "@/components/CommandPalette"
import { LogoMark, Sidebar } from "@/components/layout/Sidebar"
import { SessionInvalidBridge } from "@/components/SessionInvalidBridge"
import { ThemeToggle } from "@/components/theme-toggle"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { isApiError } from "@/api/client"
import { useLogout, useMe } from "@/api/hooks"

export function Topbar() {
  const navigate = useNavigate()
  const { data: me } = useMe()
  const logout = useLogout()

  const user = me?.user

  const handleLogout = () => {
    logout.mutate(undefined, {
      onSuccess: () => navigate("/login"),
      onError: (err) => toast.error(isApiError(err) ? err.message : "Logout failed."),
    })
  }

  return (
    <header className="sticky top-0 z-30 flex h-14 items-center gap-3 border-b bg-background/80 px-4 backdrop-blur-sm">
      <Button
        variant="ghost"
        size="icon"
        className="lg:hidden"
        aria-label="Open navigation"
        onClick={() => window.dispatchEvent(new CustomEvent("lanvory:toggle-sidebar"))}
      >
        <MenuIcon className="size-4" />
      </Button>

      <CommandPalette>
        <button
          type="button"
          className="flex h-9 w-full max-w-sm items-center gap-2 rounded-md border border-input bg-muted/40 px-3 text-sm text-muted-foreground transition-colors hover:bg-muted/70 focus-visible:ring-[3px] focus-visible:ring-ring/40 focus-visible:outline-none"
          aria-label="Open search"
        >
          <SearchIcon className="size-4" aria-hidden />
          <span className="flex-1 truncate text-left">Search assets, IPs, services…</span>
          <kbd className="rounded border bg-background px-1.5 font-mono text-[11px] text-muted-foreground">
            ⌘K
          </kbd>
        </button>
      </CommandPalette>

      <div className="flex-1" />

      <Button
        variant="outline"
        size="sm"
        className="hidden sm:inline-flex"
        disabled
        title="Asset registration arrives in Phase 2"
      >
        <PlusIcon className="size-4" aria-hidden />
        Quick add
      </Button>

      <ThemeToggle />

      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button variant="ghost" size="icon" aria-label="Account menu">
            <UserCircleIcon className="size-5" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="w-56">
          <DropdownMenuLabel className="text-foreground">
            <div className="flex items-center gap-2">
              <LogoMark className="size-6 rounded" />
              <div className="min-w-0">
                <p className="truncate text-sm font-medium">
                  {user?.display_name || user?.username}
                </p>
                <p className="truncate font-mono text-[11px] font-normal text-muted-foreground">
                  {user?.username} · {user?.role}
                </p>
              </div>
            </div>
          </DropdownMenuLabel>
          <DropdownMenuSeparator />
          <DropdownMenuItem onClick={() => navigate("/settings")}>Settings</DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem onClick={handleLogout}>
            <LogOutIcon className="size-4" aria-hidden />
            Sign out
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </header>
  )
}

/** Responsive shell: persistent sidebar on desktop, slide-over on mobile. */
export function AppShell() {
  const [mobileOpen, setMobileOpen] = useState(false)

  useEffect(() => {
    const toggle = () => setMobileOpen((v) => !v)
    window.addEventListener("lanvory:toggle-sidebar", toggle)
    return () => window.removeEventListener("lanvory:toggle-sidebar", toggle)
  }, [])

  return (
    <div className="min-h-screen">
      <SessionInvalidBridge />
      {/* Desktop sidebar */}
      <div className="fixed inset-y-0 left-0 z-40 hidden lg:block">
        <Sidebar />
      </div>

      {/* Mobile slide-over */}
      {mobileOpen ? (
        <div className="fixed inset-0 z-50 lg:hidden" role="dialog" aria-modal="true">
          <div
            className="absolute inset-0 bg-black/50"
            onClick={() => setMobileOpen(false)}
            aria-hidden
          />
          <div className="absolute inset-y-0 left-0 animate-in slide-in-from-left duration-200">
            <Sidebar onNavigate={() => setMobileOpen(false)} />
          </div>
        </div>
      ) : null}

      <div className="flex min-h-screen flex-col lg:pl-60">
        <Topbar />
        <main className="flex-1 px-4 py-6 lg:px-6">
          <Outlet />
        </main>
      </div>
    </div>
  )
}
