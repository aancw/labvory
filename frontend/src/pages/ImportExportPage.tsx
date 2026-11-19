import { useMemo, useRef, useState } from "react"
import { toast } from "sonner"
import {
  CircleCheckIcon,
  CircleXIcon,
  CopyPlusIcon,
  DownloadIcon,
  FileUpIcon,
  Table2Icon,
} from "lucide-react"

import { isApiError } from "@/api/client"
import {
  useImportCommit,
  useImportPreview,
  useNetworkContexts,
  useRestoreInventory,
  useSubnets,
} from "@/api/hooks"
import type { ImportRowResult } from "@/api/types"
import { EmptyState } from "@/components/EmptyState"
import { PageHeader } from "@/components/PageHeader"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Label } from "@/components/ui/label"
import { Select } from "@/components/ui/select"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { cn } from "@/lib/utils"

type Entity = "assets" | "subnets" | "ip_addresses"

const ENTITY_FIELDS: Record<Entity, string[]> = {
  assets: ["name", "hostname", "type_slug", "status", "environment", "manufacturer", "model", "serial_number", "os_name", "os_version"],
  subnets: ["cidr", "name", "description", "gateway", "environment"],
  ip_addresses: ["address", "status", "assignment_method", "hostname", "description"],
}

// Mirrors the server-side importer requirements (store/importer.go) — keep in sync.
const REQUIRED_FIELDS: Record<Entity, string[]> = {
  assets: ["name", "type_slug"],
  subnets: ["cidr", "name"],
  ip_addresses: ["address"],
}

// Placeholder examples per entity — required fields first.
const EXAMPLE_CSV: Record<Entity, string> = {
  assets: "name,hostname,type_slug,status\npve-02,pve-02.lab,server,online\nsw-core,,switch,online",
  subnets: "cidr,name,description,gateway\n10.0.0.0/24,lab,Lab network,10.0.0.1\n192.168.1.0/24,home,,192.168.1.1",
  ip_addresses: "address,status,assignment_method,hostname\n10.0.0.10,assigned,dhcp,pve-02\n10.0.0.11,reserved,static,",
}

/** Minimal RFC 4180 CSV parser (quoted fields, escaped quotes, CRLF). */
function parseCSV(text: string): string[][] {
  const rows: string[][] = []
  let row: string[] = []
  let field = ""
  let inQuotes = false
  for (let i = 0; i < text.length; i++) {
    const ch = text[i]
    if (inQuotes) {
      if (ch === '"') {
        if (text[i + 1] === '"') {
          field += '"'
          i++
        } else {
          inQuotes = false
        }
      } else {
        field += ch
      }
    } else if (ch === '"') {
      inQuotes = true
    } else if (ch === ",") {
      row.push(field)
      field = ""
    } else if (ch === "\n" || ch === "\r") {
      if (ch === "\r" && text[i + 1] === "\n") i++
      row.push(field)
      field = ""
      if (row.length > 1 || row[0] !== "") rows.push(row)
      row = []
    } else {
      field += ch
    }
  }
  row.push(field)
  if (row.length > 1 || row[0] !== "") rows.push(row)
  return rows
}

export function ImportExportPage() {
  const [entity, setEntity] = useState<Entity>("assets")
  const { data: contextData } = useNetworkContexts()
  const [contextId, setContextId] = useState("")
  const { data: subnetData } = useSubnets()
  const [subnetId, setSubnetId] = useState("")

  const [csvText, setCsvText] = useState("")
  const fileRef = useRef<HTMLInputElement>(null)
  const [mapping, setMapping] = useState<Record<number, string>>({})
  const [preview, setPreview] = useState<ImportRowResult[] | null>(null)
  const [parsedRows, setParsedRows] = useState<Record<string, unknown>[]>([])

  const previewImport = useImportPreview()
  const commitImport = useImportCommit()
  const restore = useRestoreInventory()
  const restoreRef = useRef<HTMLInputElement>(null)

  const parsed = useMemo(() => {
    const table = parseCSV(csvText)
    if (table.length < 2) return { headers: [] as string[], rows: [] as string[][] }
    return { headers: table[0], rows: table.slice(1) }
  }, [csvText])

  // Default mapping: header name → entity field when names match.
  const effectiveMapping = useMemo(() => {
    const out: Record<number, string> = {}
    parsed.headers.forEach((h, idx) => {
      const match = ENTITY_FIELDS[entity].find((f) => f === h.trim().toLowerCase())
      out[idx] = mapping[idx] ?? match ?? ""
    })
    return out
  }, [parsed.headers, mapping, entity])

  const buildRows = (): Record<string, unknown>[] =>
    parsed.rows.map((cells) => {
      const row: Record<string, unknown> = {}
      cells.forEach((value, idx) => {
        const field = effectiveMapping[idx]
        if (field) row[field] = value.trim()
      })
      return row
    })

  const runPreview = () => {
    const rows = buildRows()
    if (rows.length === 0) {
      toast.error("Nothing to preview — paste CSV data with a header row.")
      return
    }
    const unmapped = Object.values(effectiveMapping).some((f) => f === "")
    if (unmapped) {
      toast.error("Map every CSV column to a field first.")
      return
    }
    setParsedRows(rows)
    previewImport.mutate(
      { entity, rows, context_id: contextId, subnet_id: subnetId },
      {
        onSuccess: (res) => setPreview(res.rows),
        onError: (err) => toast.error(isApiError(err) ? err.message : "Preview failed."),
      },
    )
  }

  const runCommit = () => {
    commitImport.mutate(
      { entity, rows: parsedRows, context_id: contextId, subnet_id: subnetId, skip_duplicates: true },
      {
        onSuccess: (summary) => {
          toast.success(`Imported ${summary.created} row(s), skipped ${summary.skipped}.`)
          setPreview(null)
          setCsvText("")
          setMapping({})
        },
        onError: (err) => toast.error(isApiError(err) ? err.message : "Import failed."),
      },
    )
  }

  const downloadJSON = async () => {
    try {
      const res = await fetch("/api/v1/export/inventory.json", { credentials: "include" })
      if (!res.ok) throw new Error(String(res.status))
      const blob = await res.blob()
      triggerDownload(blob, "labvory-inventory.json")
      toast.success("Inventory exported.")
    } catch {
      toast.error("Export failed.")
    }
  }

  const exportEntityCSV = async (e: Entity) => {
    // Assets and addresses export server-side (complete rows, no pagination
    // caps, proper CSV escaping); subnets stay client-side from the list API.
    if (e === "assets" || e === "ip_addresses") {
      try {
        const path = e === "assets" ? "/api/v1/export/assets.csv" : "/api/v1/export/ip-addresses.csv"
        const res = await fetch(path, { credentials: "include" })
        if (!res.ok) throw new Error(String(res.status))
        triggerDownload(await res.blob(), e === "assets" ? "labvory-assets.csv" : "labvory-ip-addresses.csv")
        toast.success("Exported — the download has every tracked row.")
      } catch {
        toast.error("Export failed.")
      }
      return
    }
    // Client-side CSV export from the same list endpoint the UI uses.
    const res = await fetch("/api/v1/subnets", { credentials: "include" })
    const data = await res.json()
    const rows: Record<string, unknown>[] = data.subnets ?? []
    const fields = ENTITY_FIELDS[e]
    const lines = [fields.join(",")]
    for (const r of rows) {
      lines.push(fields.map((f) => csvEscape(String(r[f] ?? ""))).join(","))
    }
    triggerDownload(new Blob([lines.join("\n")], { type: "text/csv" }), `labvory-${e}.csv`)
    toast.success(`Exported ${rows.length} row(s).`)
  }

  const onRestoreFile = (file: File) => {
    const reader = new FileReader()
    reader.onload = () => {
      try {
        const doc = JSON.parse(String(reader.result))
        restore.mutate(doc, {
          onSuccess: (summary) =>
            toast.success(`Restored: ${summary.total_created} item(s) created (existing data untouched).`),
          onError: (err) => toast.error(isApiError(err) ? err.message : "Restore failed."),
        })
      } catch {
        toast.error("That file is not valid JSON.")
      }
    }
    reader.readAsText(file)
  }

  return (
    <>
      <PageHeader
        title="Import & Export"
        description="Bring your inventory in from spreadsheets, take it with you as JSON. Imports are transactional and never overwrite existing rows."
      >
        <Button variant="outline" size="sm" onClick={downloadJSON}>
          <DownloadIcon className="size-4" aria-hidden />Export inventory (JSON)
        </Button>
      </PageHeader>

      <div className="grid gap-4 lg:grid-cols-2">
        {/* Export CSVs */}
        <Card>
          <CardHeader><CardTitle className="text-sm">Quick CSV exports</CardTitle></CardHeader>
          <CardContent className="flex flex-wrap gap-2">
            {(["assets", "subnets", "ip_addresses"] as Entity[]).map((e) => (
              <Button key={e} variant="outline" size="sm" onClick={() => exportEntityCSV(e)}>
                <Table2Icon className="size-4" aria-hidden />{e.replaceAll("_", " ")}
              </Button>
            ))}
          </CardContent>
        </Card>

        {/* JSON restore */}
        <Card>
          <CardHeader><CardTitle className="text-sm">Restore from JSON</CardTitle></CardHeader>
          <CardContent>
            <p className="mb-3 text-sm text-muted-foreground">
              Re-imports a LabVory inventory export. Existing entities (matched by natural keys) are never overwritten.
            </p>
            <input
              ref={restoreRef}
              type="file"
              accept=".json,application/json"
              className="hidden"
              onChange={(e) => {
                const f = e.target.files?.[0]
                if (f) onRestoreFile(f)
                e.target.value = ""
              }}
            />
            <Button variant="outline" size="sm" onClick={() => restoreRef.current?.click()} disabled={restore.isPending}>
              <FileUpIcon className="size-4" aria-hidden />
              {restore.isPending ? "Restoring…" : "Choose inventory JSON…"}
            </Button>
          </CardContent>
        </Card>
      </div>

      {/* CSV import workflow */}
      <Card className="mt-6">
        <CardHeader><CardTitle className="text-sm">CSV import</CardTitle></CardHeader>
        <CardContent>
          <div className="mb-3 flex flex-wrap items-center gap-3">
            <div className="w-48 space-y-2">
              <Label htmlFor="imp-entity">Entity</Label>
              <Select id="imp-entity" value={entity} onChange={(e) => {
                setEntity(e.target.value as Entity)
                setMapping({})
                setPreview(null)
              }}>
                <option value="assets">Assets</option>
                <option value="subnets">Subnets</option>
                <option value="ip_addresses">IP addresses</option>
              </Select>
            </div>
            {entity === "subnets" ? (
              <div className="w-56 space-y-2">
                <Label htmlFor="imp-context">Into context</Label>
                <Select id="imp-context" value={contextId} onChange={(e) => setContextId(e.target.value)}>
                  <option value="">Select context…</option>
                  {(contextData?.contexts ?? []).map((c) => (
                    <option key={c.id} value={c.id}>{c.name}</option>
                  ))}
                </Select>
              </div>
            ) : null}
            {entity === "ip_addresses" ? (
              <div className="w-56 space-y-2">
                <Label htmlFor="imp-subnet">Into subnet</Label>
                <Select id="imp-subnet" value={subnetId} onChange={(e) => setSubnetId(e.target.value)}>
                  <option value="">Select subnet…</option>
                  {(subnetData?.subnets ?? []).map((s) => (
                    <option key={s.id} value={s.id}>{s.cidr} · {s.name}</option>
                  ))}
                </Select>
              </div>
            ) : null}
            <input
              ref={fileRef}
              type="file"
              accept=".csv,text/csv"
              className="hidden"
              onChange={(e) => {
                const f = e.target.files?.[0]
                if (!f) return
                const reader = new FileReader()
                reader.onload = () => setCsvText(String(reader.result))
                reader.readAsText(f)
                e.target.value = ""
              }}
            />
            <Button variant="outline" size="sm" className="mt-4" onClick={() => fileRef.current?.click()}>
              <FileUpIcon className="size-4" aria-hidden />Load .csv file
            </Button>
          </div>

          <textarea
            aria-label="CSV data"
            rows={6}
            placeholder={EXAMPLE_CSV[entity]}
            className="w-full rounded-md border border-input bg-transparent px-3 py-2 font-mono text-xs outline-none focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/40"
            value={csvText}
            onChange={(e) => {
              setCsvText(e.target.value)
              setPreview(null)
            }}
          />

          {parsed.headers.length > 0 ? (
            <div className="mt-4">
              <p className="mb-2 text-xs font-medium uppercase tracking-wide text-muted-foreground">Column mapping</p>
              <p className="mb-2 text-xs text-muted-foreground">
                Required fields:{" "}
                {REQUIRED_FIELDS[entity].map((f, i) => (
                  <span key={f}>
                    {i > 0 ? ", " : ""}
                    <code className="font-mono text-foreground">{f}</code>
                  </span>
                ))}
                {" — rows missing them fail validation. Unmapped columns are ignored; header names don't need to match."}
              </p>
              <div className="grid gap-2 sm:grid-cols-3 lg:grid-cols-4">
                {parsed.headers.map((header, idx) => (
                  <div key={idx} className="flex items-center gap-2 text-sm">
                    <span className="w-28 truncate font-mono text-xs text-muted-foreground">{header}</span>
                    <span aria-hidden>→</span>
                    <select
                      aria-label={`Target field for ${header}`}
                      className="h-8 flex-1 rounded-md border border-input bg-transparent px-2 text-xs"
                      value={effectiveMapping[idx] ?? ""}
                      onChange={(e) => setMapping((prev) => ({ ...prev, [idx]: e.target.value }))}
                    >
                      <option value="">— ignore —</option>
                      {ENTITY_FIELDS[entity].map((f) => (
                        <option key={f} value={f}>
                          {REQUIRED_FIELDS[entity].includes(f) ? `${f} (required)` : f}
                        </option>
                      ))}
                    </select>
                  </div>
                ))}
              </div>
              <div className="mt-3">
                <Button size="sm" onClick={runPreview} disabled={previewImport.isPending}>
                  {previewImport.isPending ? "Validating…" : "Preview import"}
                </Button>
              </div>
            </div>
          ) : null}

          {preview ? (
            <div className="mt-5">
              <div className="mb-2 flex items-center justify-between">
                <p className="text-sm font-medium">
                  Preview:{" "}
                  <span className="text-emerald-600 dark:text-emerald-400">
                    {preview.filter((r) => r.status === "ok").length} ready
                  </span>{" "}
                  · <span>{preview.filter((r) => r.status === "duplicate").length} duplicates</span> ·{" "}
                  <span className="text-destructive">{preview.filter((r) => r.status === "error").length} errors</span>
                </p>
                <Button size="sm" onClick={runCommit} disabled={commitImport.isPending}>
                  <CopyPlusIcon className="size-4" aria-hidden />
                  {commitImport.isPending ? "Importing…" : "Commit import (skip duplicates)"}
                </Button>
              </div>
              <div className="max-h-72 overflow-auto rounded-lg border">
                <Table>
                  <TableHeader>
                    <TableRow className="hover:bg-transparent">
                      <TableHead className="w-14">#</TableHead>
                      <TableHead className="w-28">Status</TableHead>
                      <TableHead>Detail</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {preview.map((row) => (
                      <TableRow key={row.index}>
                        <TableCell className="tabular-nums text-muted-foreground">{row.index + 1}</TableCell>
                        <TableCell>
                          <span className={cn(
                            "inline-flex items-center gap-1.5 text-xs font-medium",
                            row.status === "ok" && "text-emerald-600 dark:text-emerald-400",
                            row.status === "duplicate" && "text-muted-foreground",
                            row.status === "error" && "text-destructive",
                          )}>
                            {row.status === "ok" ? <CircleCheckIcon className="size-3.5" aria-hidden /> : null}
                            {row.status === "error" ? <CircleXIcon className="size-3.5" aria-hidden /> : null}
                            {row.status}
                          </span>
                        </TableCell>
                        <TableCell className="max-w-md truncate text-xs text-muted-foreground" title={row.detail}>
                          {row.detail ?? JSON.stringify(row.data)}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>
            </div>
          ) : null}
        </CardContent>
      </Card>

      {csvText === "" && parsedRows.length === 0 ? (
        <EmptyState
          className="mt-6"
          icon={FileUpIcon}
          title="No CSV loaded"
          description="Paste rows or load a file, map the columns, preview, then commit. Validation errors never write anything."
        />
      ) : null}
    </>
  )
}

function csvEscape(value: string): string {
  if (/[",\n\r]/.test(value)) {
    return `"${value.replaceAll('"', '""')}"`
  }
  return value
}

function triggerDownload(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob)
  const a = document.createElement("a")
  a.href = url
  a.download = filename
  a.click()
  URL.revokeObjectURL(url)
}
