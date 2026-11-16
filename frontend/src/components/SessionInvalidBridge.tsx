import { useEffect } from "react"
import { useQueryClient } from "@tanstack/react-query"
import { useLocation, useNavigate } from "react-router-dom"
import { toast } from "sonner"

import { resetCsrf, SESSION_INVALID_EVENT } from "@/api/client"

/**
 * Global bridge: when any API call reports the session is unusable (401, or the
 * server's session lookup failing), drop cached state and send the user back to
 * the login screen instead of leaving stale pages on screen.
 */
export function SessionInvalidBridge() {
  const navigate = useNavigate()
  const location = useLocation()
  const queryClient = useQueryClient()

  useEffect(() => {
    const onInvalid = () => {
      resetCsrf()
      queryClient.clear()
      if (location.pathname === "/login" || location.pathname === "/setup") return
      toast.error("Session expired. Please sign in again.")
      navigate("/login", { replace: true, state: { from: location.pathname } })
    }
    window.addEventListener(SESSION_INVALID_EVENT, onInvalid)
    return () => window.removeEventListener(SESSION_INVALID_EVENT, onInvalid)
  }, [navigate, location.pathname, queryClient])

  return null
}
