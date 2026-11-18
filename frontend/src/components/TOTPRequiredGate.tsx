import { useState } from "react"
import { ShieldAlert } from "lucide-react"
import { useNavigate } from "react-router-dom"
import { toast } from "sonner"

import { isApiError } from "@/api/client"
import { useLogout } from "@/api/hooks"
import { TOTPEnrollmentDialog } from "@/components/TOTPEnrollmentDialog"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"

/**
 * Full-screen wall shown while the instance requires two-factor
 * authentication and the signed-in account has none yet. The data API is
 * blocked server-side in this state; only enrollment (and sign-out) are
 * reachable, so the user can never be locked out of their own account.
 */
export function TOTPRequiredGate() {
  const navigate = useNavigate()
  const logout = useLogout()
  const [enrollOpen, setEnrollOpen] = useState(false)

  const signOut = () => {
    logout.mutate(undefined, {
      onSuccess: () => navigate("/login", { replace: true }),
      onError: (err) => toast.error(isApiError(err) ? err.message : "Could not sign out."),
    })
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-background p-4">
      <Card className="w-full max-w-md">
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-base">
            <ShieldAlert className="size-5 text-amber-500" aria-hidden />
            Two-factor authentication required
          </CardTitle>
          <CardDescription>
            This instance requires a second factor at sign-in. Set up an authenticator app
            to continue — everything else stays locked until enrollment is confirmed.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <Badge variant="outline" className="text-amber-600 dark:text-amber-400">
            Enrollment required
          </Badge>
          <div className="flex flex-col gap-2">
            <Button onClick={() => setEnrollOpen(true)}>Set up an authenticator</Button>
            <Button variant="outline" onClick={signOut} disabled={logout.isPending}>
              {logout.isPending ? "Signing out…" : "Sign out"}
            </Button>
          </div>
        </CardContent>
      </Card>
      <TOTPEnrollmentDialog open={enrollOpen} onOpenChange={setEnrollOpen} />
    </div>
  )
}
