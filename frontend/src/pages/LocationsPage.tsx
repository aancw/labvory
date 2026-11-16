import { useState } from "react"
import { Link } from "react-router-dom"
import { toast } from "sonner"
import { ChevronRightIcon, MapPinIcon, PlusIcon, Trash2Icon } from "lucide-react"

import { isApiError } from "@/api/client"
import { useCreateLocation, useDeleteLocation, useLocations, useUpdateLocation } from "@/api/hooks"
import type { Location } from "@/api/types"
import { ConfirmDialog } from "@/components/ConfirmDialog"
import { EmptyState } from "@/components/EmptyState"
import { PageHeader } from "@/components/PageHeader"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select } from "@/components/ui/select"

export function LocationsPage() {
  const { data, isLoading } = useLocations()
  const locations = data?.locations ?? []

  const [createOpen, setCreateOpen] = useState(false)
  const [name, setName] = useState("")
  const [description, setDescription] = useState("")
  const [parentId, setParentId] = useState<string>("")
  const [editing, setEditing] = useState<Location | null>(null)

  const createLocation = useCreateLocation()
  const updateLocation = useUpdateLocation()
  const deleteLocation = useDeleteLocation()

  const resetCreate = () => {
    setName("")
    setDescription("")
    setParentId("")
    setCreateOpen(false)
  }

  const submitCreate = () => {
    createLocation.mutate(
      { name, description, parent_id: parentId || null },
      {
        onSuccess: () => {
          toast.success("Location created.")
          resetCreate()
        },
        onError: (err) => toast.error(isApiError(err) ? err.message : "Could not create location."),
      },
    )
  }

  const submitEdit = () => {
    if (!editing) return
    updateLocation.mutate(
      { id: editing.id, name, description, parent_id: parentId || null },
      {
        onSuccess: () => {
          toast.success("Location updated.")
          setEditing(null)
        },
        onError: (err) => toast.error(isApiError(err) ? err.message : "Could not update location."),
      },
    )
  }

  // Assemble a simple tree for display.
  const roots = locations.filter((l) => !l.parent_id)
  const childrenOf = (id: string) => locations.filter((l) => l.parent_id === id)

  const renderRow = (loc: Location, depth: number): React.ReactNode => (
    <div key={loc.id}>
      <div
        className="flex items-center gap-3 border-b px-4 py-2.5 last:border-0"
        style={{ paddingLeft: `${1 + depth * 1.5}rem` }}
      >
        {depth > 0 ? <ChevronRightIcon className="size-3.5 text-muted-foreground" aria-hidden /> : (
          <MapPinIcon className="size-4 text-muted-foreground" aria-hidden />
        )}
        <div className="min-w-0 flex-1">
          <p className="text-sm font-medium">{loc.name}</p>
          {loc.description ? (
            <p className="truncate text-xs text-muted-foreground">{loc.description}</p>
          ) : null}
        </div>
        <span className="text-xs text-muted-foreground tabular-nums">
          {loc.asset_count} asset{loc.asset_count === 1 ? "" : "s"}
        </span>
        <div className="flex gap-1">
          <Button
            variant="ghost"
            size="sm"
            onClick={() => {
              setEditing(loc)
              setName(loc.name)
              setDescription(loc.description)
              setParentId(loc.parent_id ?? "")
            }}
          >
            Edit
          </Button>
          <ConfirmDialog
            trigger={
              <Button variant="ghost" size="icon" className="size-7" aria-label={`Delete ${loc.name}`}>
                <Trash2Icon className="size-3.5" aria-hidden />
              </Button>
            }
            title={`Delete ${loc.name}?`}
            description={
              loc.child_count > 0 || loc.asset_count > 0
                ? "Locations with child locations or assigned assets cannot be deleted."
                : "This is recorded in the audit log."
            }
            confirmLabel="Delete"
            onConfirm={() =>
              deleteLocation.mutate(loc.id, {
                onError: (err) => toast.error(isApiError(err) ? err.message : "Delete failed."),
              })
            }
          />
        </div>
      </div>
      {childrenOf(loc.id).map((child) => renderRow(child, depth + 1))}
    </div>
  )

  return (
    <>
      <PageHeader
        title="Locations"
        description="Physical placement — racks, rooms, buildings — as a tree."
      >
        <Button size="sm" onClick={() => setCreateOpen(true)}>
          <PlusIcon className="size-4" aria-hidden />Add location
        </Button>
      </PageHeader>

      {isLoading ? (
        <div className="h-40 animate-pulse rounded-lg bg-accent" />
      ) : locations.length === 0 ? (
        <EmptyState
          icon={MapPinIcon}
          title="No locations yet"
          description="Create a rack or room to start placing assets. Assets can live without a location."
        />
      ) : (
        <div className="overflow-hidden rounded-lg border">{roots.map((root) => renderRow(root, 0))}</div>
      )}

      {/* Create dialog */}
      <Dialog open={createOpen} onOpenChange={setCreateOpen}>
        <DialogContent className="max-w-sm">
          <DialogHeader>
            <DialogTitle>Add location</DialogTitle>
            <DialogDescription>Nested under an existing location if a parent is chosen.</DialogDescription>
          </DialogHeader>
          <LocationFields
            name={name} setName={setName}
            description={description} setDescription={setDescription}
            parentId={parentId} setParentId={setParentId}
            locations={locations} excludeId={null}
          />
          <DialogFooter>
            <Button variant="outline" size="sm" onClick={resetCreate}>Cancel</Button>
            <Button size="sm" onClick={submitCreate} disabled={createLocation.isPending || name.trim() === ""}>
              {createLocation.isPending ? "Creating…" : "Create location"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Edit dialog */}
      <Dialog open={!!editing} onOpenChange={(v) => !v && setEditing(null)}>
        <DialogContent className="max-w-sm">
          <DialogHeader>
            <DialogTitle>Edit location</DialogTitle>
            <DialogDescription>Parent cycles are rejected by the server.</DialogDescription>
          </DialogHeader>
          <LocationFields
            name={name} setName={setName}
            description={description} setDescription={setDescription}
            parentId={parentId} setParentId={setParentId}
            locations={locations} excludeId={editing?.id ?? null}
          />
          <DialogFooter>
            <Button variant="outline" size="sm" onClick={() => setEditing(null)}>Cancel</Button>
            <Button size="sm" onClick={submitEdit} disabled={updateLocation.isPending || name.trim() === ""}>
              {updateLocation.isPending ? "Saving…" : "Save changes"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <p className="mt-4 text-sm text-muted-foreground">
        Assets can be placed in a location from the{" "}
        <Link to="/assets" className="underline underline-offset-2">inventory</Link>.
      </p>
    </>
  )
}

function LocationFields({
  name, setName, description, setDescription, parentId, setParentId, locations, excludeId,
}: {
  name: string
  setName: (v: string) => void
  description: string
  setDescription: (v: string) => void
  parentId: string
  setParentId: (v: string) => void
  locations: Location[]
  excludeId: string | null
}) {
  return (
    <div className="space-y-3">
      <div className="space-y-1.5">
        <Label htmlFor="loc-name">Name *</Label>
        <Input id="loc-name" value={name} onChange={(e) => setName(e.target.value)} autoFocus />
      </div>
      <div className="space-y-1.5">
        <Label htmlFor="loc-desc">Description</Label>
        <Input id="loc-desc" value={description} onChange={(e) => setDescription(e.target.value)} />
      </div>
      <div className="space-y-1.5">
        <Label htmlFor="loc-parent">Parent location</Label>
        <Select id="loc-parent" value={parentId} onChange={(e) => setParentId(e.target.value)}>
          <option value="">None (top level)</option>
          {locations.filter((l) => l.id !== excludeId).map((l) => (
            <option key={l.id} value={l.id}>{l.name}</option>
          ))}
        </Select>
      </div>
    </div>
  )
}
