import { useState } from "react"
import { LoaderCircleIcon, SearchIcon } from "lucide-react"

import { useAssetOptions } from "@/api/hooks"
import type { Asset } from "@/api/types"
import { cn } from "@/lib/utils"

interface AssetPickerProps {
  value: string | null
  onChange: (asset: Asset | null) => void
  placeholder?: string
  /** Exclude one asset (e.g. the one being edited) from results. */
  excludeId?: string | undefined
  disabled?: boolean
  /**
   * Show the asset list as soon as the field is focused, without requiring a
   * typed query (used when the whole inventory is a plausible choice).
   */
  listOnFocus?: boolean
}

/**
 * Searchable asset selector used for parent-host and relationship targets.
 * Debounced query against the live inventory API; keyboard accessible.
 */
export function AssetPicker({ value, onChange, placeholder, excludeId, disabled, listOnFocus }: AssetPickerProps) {
  const [query, setQuery] = useState("")
  const [debounced, setDebounced] = useState("")
  const [open, setOpen] = useState(false)
  const [selected, setSelected] = useState<Asset | null>(null)

  const { data, isFetching } = useAssetOptions(debounced, open, listOnFocus ? 50 : 10)

  const handleQueryChange = (q: string) => {
    setQuery(q)
    window.clearTimeout((window as unknown as Record<string, number>).__gwPickerDebounce)
    ;(window as unknown as Record<string, number>).__gwPickerDebounce = window.setTimeout(() => {
      setDebounced(q.trim())
    }, 220)
  }

  const options = (data?.assets ?? []).filter((a) => a.id !== excludeId)

  const pick = (asset: Asset) => {
    setSelected(asset)
    onChange(asset)
    setOpen(false)
    setQuery("")
    setDebounced("")
  }

  return (
    <div className="relative">
      <div className="relative">
        <SearchIcon
          className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground"
          aria-hidden
        />
        <input
          role="combobox"
          aria-expanded={open}
          aria-label={placeholder ?? "Select asset"}
          className={cn(
            "h-9 w-full rounded-md border border-input bg-transparent pl-9 pr-3 text-sm shadow-xs outline-none",
            "focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/40",
            "disabled:cursor-not-allowed disabled:opacity-50",
          )}
          placeholder={placeholder ?? "Search assets by name or hostname…"}
          value={selected && !open ? selected.name : query}
          disabled={disabled}
          onFocus={() => setOpen(true)}
          onChange={(e) => {
            if (selected) setSelected(null)
            handleQueryChange(e.target.value)
            setOpen(true)
          }}
          onBlur={() => window.setTimeout(() => setOpen(false), 150)}
        />
        {isFetching ? (
          <LoaderCircleIcon className="absolute right-3 top-1/2 size-4 -translate-y-1/2 animate-spin text-muted-foreground" aria-hidden />
        ) : null}
      </div>

      {open ? (
        <div className="absolute z-40 mt-1 max-h-60 w-full overflow-y-auto rounded-md border bg-popover p-1 shadow-md">
          {!listOnFocus && query.length < 2 && !selected ? (
            <p className="px-3 py-3 text-sm text-muted-foreground">Type at least 2 characters.</p>
          ) : options.length === 0 ? (
            <p className="px-3 py-3 text-sm text-muted-foreground">No matching assets.</p>
          ) : (
            options.map((asset) => (
              <button
                key={asset.id}
                type="button"
                className="flex w-full items-center gap-2 rounded-sm px-2.5 py-1.5 text-left text-sm hover:bg-accent hover:text-accent-foreground"
                onMouseDown={(e) => e.preventDefault()}
                onClick={() => pick(asset)}
              >
                <span className="min-w-0 flex-1 truncate">
                  <span className="font-medium">{asset.name}</span>
                  {asset.hostname ? (
                    <span className="ml-1.5 font-mono text-xs text-muted-foreground">{asset.hostname}</span>
                  ) : null}
                </span>
                <span className="shrink-0 text-xs text-muted-foreground">{asset.type_label}</span>
              </button>
            ))
          )}
        </div>
      ) : null}

      {value && !selected && !open ? (
        <p className="mt-1 font-mono text-xs text-muted-foreground">
          Current: {value.slice(0, 8)}… — search to replace
        </p>
      ) : null}
    </div>
  )
}
