import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type KeyboardEvent as ReactKeyboardEvent,
  type ReactNode,
} from "react"
import { useNavigate } from "react-router-dom"
import { BoxIcon, CornerDownLeftIcon, GlobeIcon, LayersIcon, MapPinIcon, NetworkIcon, SearchIcon } from "lucide-react"

import { Dialog, DialogContent, DialogTitle } from "@/components/ui/dialog"
import { useSearch } from "@/api/hooks"
import type { SearchHit } from "@/api/types"
import { cn } from "@/lib/utils"

const HIT_ICON: Record<SearchHit["entity_type"], typeof BoxIcon> = {
  asset: BoxIcon,
  ip: NetworkIcon,
  subnet: GlobeIcon,
  vlan: LayersIcon,
  service: BoxIcon,
  location: MapPinIcon,
}

// Asset hits jump straight to the detail page; other entity kinds land on their
// module route (detail views arrive with each module's phase).
const HIT_TARGET: Record<SearchHit["entity_type"], string> = {
  asset: "/assets",
  ip: "/ips",
  subnet: "/subnets",
  vlan: "/vlans",
  service: "/services",
  location: "/admin/locations",
}

interface CommandPaletteProps {
  children: ReactNode
}

export function CommandPalette({ children }: CommandPaletteProps) {
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState("")
  const [debounced, setDebounced] = useState("")
  const [activeIndex, setActiveIndex] = useState(0)
  const inputRef = useRef<HTMLInputElement>(null)
  const navigate = useNavigate()

  // Global ⌘K / Ctrl+K.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault()
        setOpen((v) => !v)
      }
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  }, [])

  useEffect(() => {
    if (!open) {
      setQuery("")
      setDebounced("")
      setActiveIndex(0)
      return
    }
    const t = setTimeout(() => inputRef.current?.focus(), 20)
    return () => clearTimeout(t)
  }, [open])

  useEffect(() => {
    const t = setTimeout(() => setDebounced(query.trim()), 220)
    return () => clearTimeout(t)
  }, [query])

  const { data, isFetching } = useSearch(debounced, open)

  const hits = useMemo(() => {
    if (!data) return [] as SearchHit[]
    return [
      ...data.assets,
      ...data.ips,
      ...data.subnets,
      ...data.vlans,
      ...data.services,
      ...data.locations,
    ]
  }, [data])

  useEffect(() => setActiveIndex(0), [debounced])

  const select = useCallback(
    (hit: SearchHit) => {
      setOpen(false)
      // Asset detail pages exist since Phase 2 — deep-link those directly.
      const target =
        hit.entity_type === "asset"
          ? `/assets/${hit.id}`
          : hit.entity_type === "subnet"
            ? `/subnets/${hit.id}`
            : hit.entity_type === "service"
              ? `/services/${hit.id}`
              : HIT_TARGET[hit.entity_type]
      navigate(target)
    },
    [navigate],
  )

  const onKeyDown = (e: ReactKeyboardEvent) => {
    if (e.key === "ArrowDown") {
      e.preventDefault()
      setActiveIndex((i) => Math.min(i + 1, hits.length - 1))
    } else if (e.key === "ArrowUp") {
      e.preventDefault()
      setActiveIndex((i) => Math.max(i - 1, 0))
    } else if (e.key === "Enter" && hits[activeIndex]) {
      e.preventDefault()
      select(hits[activeIndex])
    }
  }

  return (
    <>
      <div onClick={() => setOpen(true)}>{children}</div>

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent
          className="top-[18%] max-w-xl translate-y-0 gap-0 overflow-hidden p-0"
          showCloseButton={false}
        >
          <DialogTitle className="sr-only">Global search</DialogTitle>
          <div className="flex items-center gap-2.5 border-b px-4">
            <SearchIcon className="size-4 shrink-0 text-muted-foreground" aria-hidden />
            <input
              ref={inputRef}
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              onKeyDown={onKeyDown}
              placeholder="Search assets, IP addresses, MACs, serials, services…"
              className="h-12 flex-1 bg-transparent text-sm outline-none placeholder:text-muted-foreground"
              role="combobox"
              aria-expanded={hits.length > 0}
              aria-label="Search query"
            />
            {isFetching ? (
              <span className="font-mono text-[11px] text-muted-foreground">…</span>
            ) : (
              <kbd className="rounded border bg-muted px-1.5 font-mono text-[11px] text-muted-foreground">
                esc
              </kbd>
            )}
          </div>

          <div className="max-h-80 overflow-y-auto p-1.5" role="listbox" aria-label="Search results">
            {debounced.length < 2 ? (
              <p className="px-3 py-6 text-center text-sm text-muted-foreground">
                Type at least 2 characters to search.
              </p>
            ) : hits.length === 0 ? (
              <p className="px-3 py-6 text-center text-sm text-muted-foreground">
                No matches found.
              </p>
            ) : (
              hits.map((hit, i) => (
                <button
                  key={`${hit.entity_type}-${hit.id}`}
                  type="button"
                  role="option"
                  aria-selected={i === activeIndex}
                  onMouseEnter={() => setActiveIndex(i)}
                  onClick={() => select(hit)}
                  className={cn(
                    "flex w-full items-center gap-3 rounded-md px-3 py-2 text-left outline-none",
                    i === activeIndex && "bg-accent text-accent-foreground",
                  )}
                >
                  <HitIconFor entity={hit.entity_type} />
                  <span className="min-w-0 flex-1">
                    <span className="block truncate text-sm font-medium font-mono">{hit.title}</span>
                    <span className="block truncate text-xs text-muted-foreground">
                      {hit.subtitle}
                    </span>
                  </span>
                  {i === activeIndex ? (
                    <CornerDownLeftIcon className="size-3.5 shrink-0 text-muted-foreground" aria-hidden />
                  ) : null}
                </button>
              ))
            )}
          </div>
        </DialogContent>
      </Dialog>
    </>
  )
}

function HitIconFor({ entity }: { entity: SearchHit["entity_type"] }) {
  const Icon = HIT_ICON[entity] ?? BoxIcon
  return <Icon className="size-4 shrink-0 text-muted-foreground" aria-hidden />
}
