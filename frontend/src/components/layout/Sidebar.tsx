import { NavLink } from "react-router-dom"

import { NAV_SECTIONS } from "@/components/nav"
import { Badge } from "@/components/ui/badge"
import { cn } from "@/lib/utils"

interface SidebarProps {
  onNavigate?: () => void
}

export function Sidebar({ onNavigate }: SidebarProps) {
  return (
    <aside className="flex h-full w-60 flex-col border-r border-sidebar-border bg-sidebar">
      <div className="flex h-14 items-center gap-2.5 border-b border-sidebar-border px-4">
        <LogoMark />
        <div className="leading-tight">
          <p className="text-sm font-semibold">Lanvory</p>
          <p className="text-[11px] text-muted-foreground">Homelab Infrastructure</p>
        </div>
      </div>

      <nav className="flex-1 overflow-y-auto px-2 py-3">
        {NAV_SECTIONS.map((section) => (
          <div key={section.heading} className="mb-4">
            <p className="mb-1 px-2 text-[11px] font-medium tracking-wider text-muted-foreground/80 uppercase">
              {section.heading}
            </p>
            <ul className="space-y-0.5">
              {section.items.map((item) => (
                <li key={item.path}>
                  <NavLink
                    to={item.path}
                    end={item.path === "/"}
                    onClick={onNavigate}
                    className={({ isActive }) =>
                      cn(
                        "group flex items-center gap-2.5 rounded-md px-2 py-1.5 text-sm text-sidebar-foreground/85 transition-colors",
                        "hover:bg-sidebar-accent hover:text-sidebar-accent-foreground",
                        "focus-visible:ring-[3px] focus-visible:ring-ring/40 focus-visible:outline-none",
                        isActive &&
                          "bg-sidebar-accent font-medium text-sidebar-accent-foreground",
                      )
                    }
                  >
                    <item.icon className="size-4 shrink-0 text-muted-foreground" aria-hidden />
                    <span className="flex-1 truncate">{item.label}</span>
                    {item.phase ? (
                      <Badge
                        variant="outline"
                        className="h-5 border-border/70 px-1.5 text-[10px] font-medium text-muted-foreground"
                      >
                        P{item.phase}
                      </Badge>
                    ) : null}
                  </NavLink>
                </li>
              ))}
            </ul>
          </div>
        ))}
      </nav>

      <div className="border-t border-sidebar-border px-4 py-3">
        <p className="text-[11px] text-muted-foreground">
          Self-hosted ·{" "}
          <span className="font-mono text-[11px]">v0.1.0</span>
        </p>
      </div>
    </aside>
  )
}

export function LogoMark({ className }: { className?: string }) {
  return (
    <div
      className={cn(
        "flex size-7 shrink-0 items-center justify-center rounded-md bg-primary text-primary-foreground",
        className,
      )}
    >
      <svg viewBox="0 0 32 32" className="size-4" fill="none" aria-hidden>
        <path d="M6 10h20M6 16h20M6 22h12" stroke="currentColor" strokeWidth="3" strokeLinecap="round" />
        <circle cx="25" cy="22" r="3" fill="currentColor" />
      </svg>
    </div>
  )
}
