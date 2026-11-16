import { useState } from "react"
import { toast } from "sonner"
import { PencilIcon, PlusIcon, TagIcon, Trash2Icon } from "lucide-react"

import { isApiError } from "@/api/client"
import { useCreateTag, useDeleteTag, useTags, useUpdateTag } from "@/api/hooks"
import type { Tag } from "@/api/types"
import { ConfirmDialog } from "@/components/ConfirmDialog"
import { EmptyState } from "@/components/EmptyState"
import { PageHeader } from "@/components/PageHeader"
import { TAG_COLORS, tagDotClass } from "@/components/tag-color"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select } from "@/components/ui/select"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"

export function TagsPage() {
  const { data, isLoading } = useTags()
  const tags = data?.tags ?? []

  const [createOpen, setCreateOpen] = useState(false)
  const [name, setName] = useState("")
  const [color, setColor] = useState("zinc")
  const [editing, setEditing] = useState<Tag | null>(null)

  const createTag = useCreateTag()
  const updateTag = useUpdateTag()
  const deleteTag = useDeleteTag()

  const submitCreate = () => {
    createTag.mutate(
      { name: name.trim(), color },
      {
        onSuccess: () => {
          toast.success("Tag created.")
          setName("")
          setColor("zinc")
          setCreateOpen(false)
        },
        onError: (err) => toast.error(isApiError(err) ? err.message : "Could not create tag."),
      },
    )
  }

  const submitEdit = () => {
    if (!editing) return
    updateTag.mutate(
      { id: editing.id, name: name.trim(), color },
      {
        onSuccess: () => {
          toast.success("Tag updated.")
          setEditing(null)
        },
        onError: (err) => toast.error(isApiError(err) ? err.message : "Could not update tag."),
      },
    )
  }

  return (
    <>
      <PageHeader
        title="Tags"
        description="Reusable labels attached to assets (and, later, subnets and services)."
      >
        <Button size="sm" onClick={() => setCreateOpen(true)}>
          <PlusIcon className="size-4" aria-hidden />Add tag
        </Button>
      </PageHeader>

      {isLoading ? (
        <div className="h-32 animate-pulse rounded-lg bg-accent" />
      ) : tags.length === 0 ? (
        <EmptyState
          icon={TagIcon}
          title="No tags yet"
          description="Create tags to categorize assets — e.g. critical, backup-target, vm-host."
        />
      ) : (
        <div className="overflow-hidden rounded-lg border">
          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead>Name</TableHead>
                <TableHead>Color</TableHead>
                <TableHead>Usage</TableHead>
                <TableHead className="w-24" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {tags.map((tag) => (
                <TableRow key={tag.id}>
                  <TableCell className="font-medium">{tag.name}</TableCell>
                  <TableCell>
                    <span className="inline-flex items-center gap-1.5 text-xs text-muted-foreground">
                      <span className={`size-2.5 rounded-full ${tagDotClass(tag.color)}`} aria-hidden />
                      {tag.color}
                    </span>
                  </TableCell>
                  <TableCell className="tabular-nums text-muted-foreground">
                    {tag.usage ?? 0} entit{(tag.usage ?? 0) === 1 ? "y" : "ies"}
                  </TableCell>
                  <TableCell>
                    <div className="flex justify-end gap-1">
                      <Button
                        variant="ghost"
                        size="icon"
                        className="size-7"
                        aria-label={`Edit ${tag.name}`}
                        onClick={() => {
                          setEditing(tag)
                          setName(tag.name)
                          setColor(tag.color)
                        }}
                      >
                        <PencilIcon className="size-3.5" aria-hidden />
                      </Button>
                      <ConfirmDialog
                        trigger={
                          <Button variant="ghost" size="icon" className="size-7" aria-label={`Delete ${tag.name}`}>
                            <Trash2Icon className="size-3.5" aria-hidden />
                          </Button>
                        }
                        title={`Delete ${tag.name}?`}
                        description="The tag is removed from every entity that uses it."
                        confirmLabel="Delete tag"
                        onConfirm={() =>
                          deleteTag.mutate(tag.id, {
                            onError: (err) => toast.error(isApiError(err) ? err.message : "Delete failed."),
                          })
                        }
                      />
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}

      <Dialog open={createOpen || !!editing} onOpenChange={(v) => {
        if (!v) {
          setCreateOpen(false)
          setEditing(null)
        }
      }}>
        <DialogContent className="max-w-sm">
          <DialogHeader>
            <DialogTitle>{editing ? "Edit tag" : "Add tag"}</DialogTitle>
            <DialogDescription>Tags attach to assets from the asset form.</DialogDescription>
          </DialogHeader>
          <div className="space-y-3">
            <div className="space-y-1.5">
              <Label htmlFor="tag-name">Name *</Label>
              <Input id="tag-name" value={name} onChange={(e) => setName(e.target.value)} autoFocus />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="tag-color">Color</Label>
              <Select id="tag-color" value={color} onChange={(e) => setColor(e.target.value)}>
                {TAG_COLORS.map((c) => (
                  <option key={c} value={c}>{c}</option>
                ))}
              </Select>
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" size="sm" onClick={() => {
              setCreateOpen(false)
              setEditing(null)
            }}>Cancel</Button>
            <Button
              size="sm"
              onClick={editing ? submitEdit : submitCreate}
              disabled={(createTag.isPending || updateTag.isPending) || name.trim() === ""}
            >
              {createTag.isPending || updateTag.isPending ? "Saving…" : editing ? "Save changes" : "Create tag"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}
