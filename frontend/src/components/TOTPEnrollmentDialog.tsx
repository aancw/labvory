import { useEffect, useState } from "react"
import { toast } from "sonner"

import { isApiError } from "@/api/client"
import { useTOTPEnable, useTOTPSetup } from "@/api/hooks"
import type { TOTPSetup } from "@/api/types"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"

interface TOTPEnrollmentDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

/**
 * TOTP enrollment dialog: fetches a fresh setup secret when opened and
 * confirms with one valid 6-digit code (manual key entry, no QR dependency).
 * Shared by the settings page and the instance-wide 2FA enforcement gate.
 */
export function TOTPEnrollmentDialog({ open, onOpenChange }: TOTPEnrollmentDialogProps) {
  const setup = useTOTPSetup()
  const enable = useTOTPEnable()
  const [enroll, setEnroll] = useState<TOTPSetup | null>(null)
  const [code, setCode] = useState("")

  useEffect(() => {
    if (!open) {
      setEnroll(null)
      setCode("")
      return
    }
    setCode("")
    setup.mutate(undefined, {
      onSuccess: (res) => setEnroll(res),
      onError: (err) => {
        toast.error(isApiError(err) ? err.message : "Could not start enrollment.")
        onOpenChange(false)
      },
    })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open])

  const confirmEnable = () => {
    enable.mutate(code.trim(), {
      onSuccess: () => {
        toast.success("Two-factor enabled — you will need a code at next sign-in.")
        onOpenChange(false)
      },
      onError: (err) =>
        toast.error(isApiError(err) ? err.message : "Could not confirm the code."),
    })
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Set up an authenticator</DialogTitle>
          <DialogDescription>
            Add a new TOTP entry in your authenticator app (Aegis, 1Password, Google
            Authenticator, …) — paste the key below or use &ldquo;enter setup key&rdquo;.
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-3">
          <div>
            <p className="mb-1 text-xs font-medium text-muted-foreground">Setup key</p>
            <p className="break-all rounded-md border bg-muted px-3 py-2 font-mono text-sm tracking-wider">
              {enroll?.secret ?? "…"}
            </p>
          </div>
          <div>
            <p className="mb-1 text-xs font-medium text-muted-foreground">otpauth URI (optional)</p>
            <p className="break-all rounded-md border bg-muted px-3 py-2 font-mono text-xs">
              {enroll?.otpauth_uri ?? "…"}
            </p>
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="totp-confirm">Confirm with a 6-digit code</Label>
            <Input
              id="totp-confirm"
              className="text-center font-mono tracking-[0.4em]"
              inputMode="numeric"
              maxLength={6}
              placeholder="000000"
              value={code}
              onChange={(e) => setCode(e.target.value)}
              onKeyDown={(e) => e.key === "Enter" && confirmEnable()}
              autoFocus
            />
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" size="sm" onClick={() => onOpenChange(false)}>Cancel</Button>
          <Button
            size="sm"
            onClick={confirmEnable}
            disabled={enable.isPending || setup.isPending || code.trim().length !== 6}
          >
            {enable.isPending ? "Confirming…" : "Confirm"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
