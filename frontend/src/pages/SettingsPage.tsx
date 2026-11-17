import { useEffect, useState } from "react"
import { zodResolver } from "@hookform/resolvers/zod"
import { useForm } from "react-hook-form"
import { toast } from "sonner"
import { z } from "zod"

import { isApiError } from "@/api/client"
import {
  useApiTokens,
  useChangePassword,
  useCreateApiToken,
  useCreateUser,
  useDeleteUser,
  useMe,
  useRevokeApiToken,
  useRevokeOtherSessions,
  useSessions,
  useSettings,
  useTestNotification,
  useTOTPDisable,
  useTOTPEnable,
  useTOTPSetup,
  useUpdateSettings,
  useUpdateUser,
  useUsers,
} from "@/api/hooks"
import type { ApiToken, ApiTokenCreated, TOTPSetup, User } from "@/api/types"
import { PageHeader } from "@/components/PageHeader"
import { useTheme } from "@/components/theme-provider"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { ConfirmDialog } from "@/components/ConfirmDialog"
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
import { Select } from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { formatDateTime } from "@/lib/utils"
import { cn } from "@/lib/utils"

export function SettingsPage() {
  const { data: me } = useMe()
  const user = me?.user
  const isAdmin = user?.role === "admin"

  return (
    <>
      <PageHeader
        title="Settings"
        description="Instance configuration, appearance, and account security."
      />
      <div className="grid gap-4 lg:grid-cols-2">
        <InstanceCard />
        <NotificationsCard />
        <RetentionCard />
        <TwoFactorCard />
        <AppearanceCard />
        <SecurityCard />
        <SessionsCard />
        {isAdmin ? <UsersCard currentUserId={user?.id ?? ""} /> : null}
        <AccountCard username={user?.username ?? ""} role={user?.role ?? ""} />
        {isAdmin ? (
          <div className="lg:col-span-2">
            <ApiTokensCard />
          </div>
        ) : null}
      </div>
    </>
  )
}

function InstanceCard() {
  const { data, isLoading } = useSettings()
  const updateSettings = useUpdateSettings()
  const [instanceName, setInstanceName] = useState("")

  useEffect(() => {
    const value = data?.settings["instance.name"]
    if (typeof value === "string") setInstanceName(value)
  }, [data])

  const save = () => {
    updateSettings.mutate(
      { settings: { "instance.name": instanceName } },
      {
        onSuccess: () => toast.success("Instance settings saved."),
        onError: (err) => toast.error(isApiError(err) ? err.message : "Could not save settings."),
      },
    )
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm">Instance</CardTitle>
        <CardDescription>Display name used across the interface.</CardDescription>
      </CardHeader>
      <CardContent>
        {isLoading ? (
          <Skeleton className="h-9 w-full" />
        ) : (
          <form
            className="space-y-3"
            onSubmit={(e) => {
              e.preventDefault()
              save()
            }}
          >
            <div className="space-y-2">
              <Label htmlFor="instance-name">Instance name</Label>
              <Input
                id="instance-name"
                value={instanceName}
                maxLength={64}
                onChange={(e) => setInstanceName(e.target.value)}
              />
            </div>
            <Button type="submit" size="sm" disabled={updateSettings.isPending}>
              {updateSettings.isPending ? "Saving…" : "Save changes"}
            </Button>
          </form>
        )}
      </CardContent>
    </Card>
  )
}

function NotificationsCard() {
  const { data, isLoading } = useSettings()
  const updateSettings = useUpdateSettings()
  const test = useTestNotification()
  const [webhookURL, setWebhookURL] = useState("")

  useEffect(() => {
    const value = data?.settings["notifications.webhook_url"]
    if (typeof value === "string") setWebhookURL(value)
  }, [data])

  const save = () => {
    updateSettings.mutate(
      { settings: { "notifications.webhook_url": webhookURL.trim() } },
      {
        onSuccess: () => toast.success("Notification settings saved."),
        onError: (err) => toast.error(isApiError(err) ? err.message : "Could not save settings."),
      },
    )
  }

  const sendTest = () => {
    test.mutate(undefined, {
      onSuccess: () => toast.success("Test notification sent — check your webhook receiver."),
      onError: (err) => toast.error(isApiError(err) ? err.message : "Test delivery failed."),
    })
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm">Notifications</CardTitle>
        <CardDescription>
          Push alerts to a webhook when a service goes down or recovers and when a scheduled sync
          fails. Works with ntfy out of the box (ntfy.sh/&lt;topic&gt;); anything accepting a POST works too.
        </CardDescription>
      </CardHeader>
      <CardContent>
        {isLoading ? (
          <Skeleton className="h-9 w-full" />
        ) : (
          <form
            className="space-y-3"
            onSubmit={(e) => {
              e.preventDefault()
              save()
            }}
          >
            <div className="space-y-2">
              <Label htmlFor="notify-webhook">Webhook URL</Label>
              <Input
                id="notify-webhook"
                className="font-mono text-xs"
                placeholder="https://ntfy.sh/your-secret-topic"
                maxLength={500}
                value={webhookURL}
                onChange={(e) => setWebhookURL(e.target.value)}
              />
              <p className="text-xs text-muted-foreground">
                Sent as a POST with the message body and an <span className="font-mono">X-Title</span> header. Empty disables notifications.
              </p>
            </div>
            <div className="flex gap-2">
              <Button type="submit" size="sm" disabled={updateSettings.isPending}>
                {updateSettings.isPending ? "Saving…" : "Save changes"}
              </Button>
              <Button type="button" variant="outline" size="sm" onClick={sendTest} disabled={test.isPending}>
                {test.isPending ? "Sending…" : "Send test"}
              </Button>
            </div>
          </form>
        )}
      </CardContent>
    </Card>
  )
}

function RetentionCard() {
  const { data, isLoading } = useSettings()
  const updateSettings = useUpdateSettings()
  const [days, setDays] = useState("")

  useEffect(() => {
    const value = data?.settings["audit.retention_days"]
    if (typeof value === "number") setDays(String(value))
  }, [data])

  const save = () => {
    const parsed = Number(days)
    const clamped = Number.isFinite(parsed) ? Math.max(0, Math.min(3650, Math.round(parsed))) : 0
    updateSettings.mutate(
      { settings: { "audit.retention_days": clamped } },
      {
        onSuccess: () => {
          setDays(String(clamped))
          toast.success("Audit retention saved.")
        },
        onError: (err) => toast.error(isApiError(err) ? err.message : "Could not save settings."),
      },
    )
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm">Audit retention</CardTitle>
        <CardDescription>
          How long change history (the Activity log) is kept before the scheduler removes it daily.
        </CardDescription>
      </CardHeader>
      <CardContent>
        {isLoading ? (
          <Skeleton className="h-9 w-full" />
        ) : (
          <form
            className="space-y-3"
            onSubmit={(e) => {
              e.preventDefault()
              save()
            }}
          >
            <div className="space-y-2">
              <Label htmlFor="audit-retention">Retention (days)</Label>
              <Input
                id="audit-retention"
                type="number"
                min={0}
                max={3650}
                value={days}
                onChange={(e) => setDays(e.target.value)}
              />
              <p className="text-xs text-muted-foreground">
                0 keeps every audit event forever (pruning disabled). Health-flap events accumulate
                over time, so a window like 180 or 365 days keeps the log fast on long-running instances.
              </p>
            </div>
            <Button type="submit" size="sm" disabled={updateSettings.isPending}>
              {updateSettings.isPending ? "Saving…" : "Save changes"}
            </Button>
          </form>
        )}
      </CardContent>
    </Card>
  )
}

const THEME_OPTIONS = [
  { value: "light", label: "Light" },
  { value: "dark", label: "Dark" },
  { value: "system", label: "System" },
] as const

function AppearanceCard() {
  const { theme, setTheme } = useTheme()

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm">Appearance</CardTitle>
        <CardDescription>Theme preference is stored locally on this device.</CardDescription>
      </CardHeader>
      <CardContent>
        <div className="inline-flex rounded-md border p-1" role="radiogroup" aria-label="Theme">
          {THEME_OPTIONS.map((option) => (
            <button
              key={option.value}
              type="button"
              role="radio"
              aria-checked={theme === option.value}
              onClick={() => setTheme(option.value)}
              className={cn(
                "rounded px-3 py-1.5 text-sm transition-colors",
                theme === option.value
                  ? "bg-primary text-primary-foreground"
                  : "text-muted-foreground hover:text-foreground",
              )}
            >
              {option.label}
            </button>
          ))}
        </div>
      </CardContent>
    </Card>
  )
}

// TwoFactorCard manages TOTP enrollment: setup → scan/paste secret → confirm
// with a code; disable requires the password plus a valid code.
function TwoFactorCard() {
  const { data: me } = useMe()
  const enabled = me?.user?.totp_enabled_at != null
  const setup = useTOTPSetup()
  const enable = useTOTPEnable()
  const disable = useTOTPDisable()
  const [enroll, setEnroll] = useState<TOTPSetup | null>(null)
  const [code, setCode] = useState("")
  const [disableOpen, setDisableOpen] = useState(false)
  const [disablePassword, setDisablePassword] = useState("")
  const [disableCode, setDisableCode] = useState("")

  const startEnroll = () => {
    setCode("")
    setup.mutate(undefined, {
      onSuccess: (res) => setEnroll(res),
      onError: (err) => toast.error(isApiError(err) ? err.message : "Could not start enrollment."),
    })
  }

  const confirmEnable = () => {
    enable.mutate(code.trim(), {
      onSuccess: () => {
        toast.success("Two-factor enabled — you will need a code at next sign-in.")
        setEnroll(null)
        setCode("")
      },
      onError: (err) =>
        toast.error(isApiError(err) ? err.message : "Could not confirm the code."),
    })
  }

  const confirmDisable = () => {
    disable.mutate(
      { password: disablePassword, code: disableCode.trim() },
      {
        onSuccess: () => {
          toast.success("Two-factor disabled.")
          setDisableOpen(false)
          setDisablePassword("")
          setDisableCode("")
        },
        onError: (err) =>
          toast.error(isApiError(err) ? err.message : "Could not disable two-factor."),
      },
    )
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm">Two-factor authentication</CardTitle>
        <CardDescription>
          Require a 6-digit code from an authenticator app (TOTP) at sign-in.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-3">
        <div className="flex items-center justify-between gap-3">
          <div className="text-sm">
            {enabled ? (
              <span className="inline-flex items-center gap-2">
                <Badge>Enabled</Badge>
                <span className="text-muted-foreground">since {formatDateTime(me!.user.totp_enabled_at!)}</span>
              </span>
            ) : (
              <span className="text-muted-foreground">Not enabled — password only.</span>
            )}
          </div>
          {enabled ? (
            <Button variant="outline" size="sm" onClick={() => setDisableOpen(true)}>
              Disable
            </Button>
          ) : (
            <Button variant="outline" size="sm" onClick={startEnroll} disabled={setup.isPending}>
              {setup.isPending ? "Preparing…" : "Enable"}
            </Button>
          )}
        </div>

        {/* Enrollment dialog: secret + manual entry (no QR dependency). */}
        <Dialog open={enroll !== null} onOpenChange={(open) => !open && setEnroll(null)}>
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
                  {enroll?.secret}
                </p>
              </div>
              <div>
                <p className="mb-1 text-xs font-medium text-muted-foreground">otpauth URI (optional)</p>
                <p className="break-all rounded-md border bg-muted px-3 py-2 font-mono text-xs">
                  {enroll?.otpauth_uri}
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
              <Button variant="outline" size="sm" onClick={() => setEnroll(null)}>Cancel</Button>
              <Button size="sm" onClick={confirmEnable} disabled={enable.isPending || code.trim().length !== 6}>
                {enable.isPending ? "Verifying…" : "Confirm & enable"}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>

        {/* Disable dialog: password + code. */}
        <Dialog open={disableOpen} onOpenChange={setDisableOpen}>
          <DialogContent className="max-w-sm">
            <DialogHeader>
              <DialogTitle>Disable two-factor?</DialogTitle>
              <DialogDescription>
                Sign-ins will again rely on the password alone. Confirm with your password and
                one last valid code.
              </DialogDescription>
            </DialogHeader>
            <div className="space-y-3">
              <div className="space-y-1.5">
                <Label htmlFor="totp-disable-password">Password</Label>
                <Input
                  id="totp-disable-password"
                  type="password"
                  autoComplete="current-password"
                  value={disablePassword}
                  onChange={(e) => setDisablePassword(e.target.value)}
                  autoFocus
                />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="totp-disable-code">Verification code</Label>
                <Input
                  id="totp-disable-code"
                  className="text-center font-mono tracking-[0.4em]"
                  inputMode="numeric"
                  maxLength={6}
                  placeholder="000000"
                  value={disableCode}
                  onChange={(e) => setDisableCode(e.target.value)}
                />
              </div>
            </div>
            <DialogFooter>
              <Button variant="outline" size="sm" onClick={() => setDisableOpen(false)}>Cancel</Button>
              <Button
                variant="destructive"
                size="sm"
                onClick={confirmDisable}
                disabled={disable.isPending || !disablePassword || disableCode.trim().length !== 6}
              >
                {disable.isPending ? "Disabling…" : "Disable 2FA"}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </CardContent>
    </Card>
  )
}

// SessionsCard lists active logins and lets the operator sign out everywhere
// except the current device.
function SessionsCard() {
  const { data, isLoading } = useSessions()
  const revoke = useRevokeOtherSessions()
  const sessions = data?.sessions ?? []
  const others = sessions.filter((s) => s.id !== data?.current_id)

  const revokeOthers = () => {
    revoke.mutate(undefined, {
      onSuccess: (res) =>
        toast.success(
          res.revoked === 1 ? "Signed out 1 other session." : `Signed out ${res.revoked} other sessions.`,
        ),
      onError: (err) => toast.error(isApiError(err) ? err.message : "Could not revoke sessions."),
    })
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm">Active sessions</CardTitle>
        <CardDescription>Signed-in browsers and clients on this account.</CardDescription>
      </CardHeader>
      <CardContent className="space-y-3">
        {isLoading ? (
          <Skeleton className="h-20 w-full" />
        ) : sessions.length === 0 ? (
          <p className="rounded-md border border-dashed px-3 py-4 text-center text-sm text-muted-foreground">
            No active sessions.
          </p>
        ) : (
          <ul className="space-y-2">
            {sessions.map((s) => (
              <li key={s.id} className="flex items-center gap-3 rounded-md border px-3 py-2 text-sm">
                <div className="min-w-0 flex-1">
                  <p className="truncate font-medium">
                    {shortenUserAgent(s.user_agent) || "Unknown client"}
                    {s.id === data?.current_id ? (
                      <Badge className="ml-2 align-middle" variant="secondary">This device</Badge>
                    ) : null}
                  </p>
                  <p className="truncate text-xs text-muted-foreground">
                    {s.source_ip || "unknown IP"} · signed in {formatDateTime(s.created_at)}
                  </p>
                </div>
                <span className="shrink-0 text-xs text-muted-foreground">
                  expires {formatDateTime(s.expires_at)}
                </span>
              </li>
            ))}
          </ul>
        )}
        <ConfirmDialog
          trigger={
            <Button variant="outline" size="sm" disabled={others.length === 0}>
              Sign out {others.length === 1 ? "1 other session" : `${others.length} other sessions`}
            </Button>
          }
          title="Sign out other sessions?"
          description="Every other signed-in device or client is logged out immediately; this device stays signed in."
          confirmLabel="Sign out others"
          onConfirm={revokeOthers}
        />
      </CardContent>
    </Card>
  )
}

// shortenUserAgent renders "Firefox 132 · macOS" style hints from a UA string.
function shortenUserAgent(ua: string): string {
  if (!ua) return ""
  const name = /Edg\//.test(ua) ? "Edge"
    : /OPR\//.test(ua) ? "Opera"
    : /Chrome\//.test(ua) ? "Chrome"
    : /Firefox\//.test(ua) ? "Firefox"
    : /Safari\//.test(ua) ? "Safari"
    : /curl/.test(ua) ? "curl"
    : null
  const os = /Windows/.test(ua) ? "Windows"
    : /Android/.test(ua) ? "Android"
    : /iPhone|iPad/.test(ua) ? "iOS"
    : /Mac OS X/.test(ua) ? "macOS"
    : /Linux/.test(ua) ? "Linux"
    : null
  if (!name && !os) return ua.length > 60 ? ua.slice(0, 57) + "…" : ua
  return [name, os].filter(Boolean).join(" · ")
}

// UsersCard is the administrator's account manager: create accounts, edit
// roles/display names, reset passwords, and delete — with last-admin and
// self-deletion guards enforced server-side.
function UsersCard({ currentUserId }: { currentUserId: string }) {
  const { data, isLoading } = useUsers()
  const createUser = useCreateUser()
  const updateUser = useUpdateUser()
  const deleteUser = useDeleteUser()
  const [addOpen, setAddOpen] = useState(false)
  const [username, setUsername] = useState("")
  const [displayName, setDisplayName] = useState("")
  const [password, setPassword] = useState("")
  const [role, setRole] = useState("viewer")
  const [editing, setEditing] = useState<User | null>(null)
  const [editName, setEditName] = useState("")
  const [editRole, setEditRole] = useState("viewer")
  const [editPassword, setEditPassword] = useState("")

  const users = data?.users ?? []

  const openAdd = () => {
    setUsername("")
    setDisplayName("")
    setPassword("")
    setRole("viewer")
    setAddOpen(true)
  }

  const submitAdd = () => {
    createUser.mutate(
      { username: username.trim(), display_name: displayName.trim(), password, role },
      {
        onSuccess: () => {
          toast.success(`Account ${username.trim()} created.`)
          setAddOpen(false)
        },
        onError: (err) => toast.error(isApiError(err) ? err.message : "Could not create account."),
      },
    )
  }

  const openEdit = (u: User) => {
    setEditing(u)
    setEditName(u.display_name)
    setEditRole(u.role)
    setEditPassword("")
  }

  const submitEdit = () => {
    if (!editing) return
    updateUser.mutate(
      {
        id: editing.id,
        payload: {
          display_name: editName.trim(),
          role: editRole,
          ...(editPassword ? { password: editPassword } : {}),
        },
      },
      {
        onSuccess: () => {
          toast.success(
            editPassword
              ? `Account updated — ${editing.username}'s sessions were signed out.`
              : "Account updated.",
          )
          setEditing(null)
        },
        onError: (err) => toast.error(isApiError(err) ? err.message : "Could not update account."),
      },
    )
  }

  const remove = (u: User) => {
    deleteUser.mutate(u.id, {
      onSuccess: () => toast.success(`Account ${u.username} deleted.`),
      onError: (err) => toast.error(isApiError(err) ? err.message : "Could not delete account."),
    })
  }

  return (
    <Card>
      <CardHeader>
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <CardTitle className="text-sm">Accounts</CardTitle>
            <CardDescription>
              Team access. Operators manage the inventory; viewers are read-only.
            </CardDescription>
          </div>
          <Button variant="outline" size="sm" onClick={openAdd}>
            Add account
          </Button>
        </div>
      </CardHeader>
      <CardContent>
        {isLoading ? (
          <Skeleton className="h-24 w-full" />
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Username</TableHead>
                <TableHead>Display name</TableHead>
                <TableHead>Role</TableHead>
                <TableHead>Created</TableHead>
                <TableHead className="w-0 text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {users.map((u) => (
                <TableRow key={u.id}>
                  <TableCell className="font-medium">
                    {u.username}
                    {u.id === currentUserId ? (
                      <Badge className="ml-2 align-middle" variant="secondary">You</Badge>
                    ) : null}
                  </TableCell>
                  <TableCell className="text-muted-foreground">{u.display_name || "—"}</TableCell>
                  <TableCell>
                    <Badge variant={u.role === "admin" ? "default" : "secondary"}>{u.role}</Badge>
                  </TableCell>
                  <TableCell className="text-muted-foreground">{formatDateTime(u.created_at)}</TableCell>
                  <TableCell className="text-right">
                    <div className="inline-flex justify-end gap-1">
                      <Button variant="ghost" size="sm" onClick={() => openEdit(u)}>
                        Edit
                      </Button>
                      {u.id === currentUserId ? null : (
                        <ConfirmDialog
                          trigger={
                            <Button variant="ghost" size="sm" className="text-destructive">
                              Delete
                            </Button>
                          }
                          title={`Delete ${u.username}?`}
                          description="The account and its sessions are removed. Inventory data is unaffected."
                          confirmLabel="Delete account"
                          onConfirm={() => remove(u)}
                        />
                      )}
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}

        {/* Add account */}
        <Dialog open={addOpen} onOpenChange={setAddOpen}>
          <DialogContent className="max-w-sm">
            <DialogHeader>
              <DialogTitle>Add account</DialogTitle>
              <DialogDescription>Sign-in credentials for a teammate or agent operator.</DialogDescription>
            </DialogHeader>
            <div className="space-y-3">
              <div className="space-y-1.5">
                <Label htmlFor="new-user-username">Username *</Label>
                <Input id="new-user-username" value={username} onChange={(e) => setUsername(e.target.value)} autoFocus />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="new-user-display">Display name</Label>
                <Input id="new-user-display" value={displayName} onChange={(e) => setDisplayName(e.target.value)} />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="new-user-password">Password *</Label>
                <Input id="new-user-password" type="password" autoComplete="new-password" value={password}
                  onChange={(e) => setPassword(e.target.value)} />
                <p className="text-xs text-muted-foreground">At least 10 characters.</p>
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="new-user-role">Role</Label>
                <Select id="new-user-role" value={role} onChange={(e) => setRole(e.target.value)}>
                  <option value="viewer">Viewer — read-only</option>
                  <option value="operator">Operator — manage inventory</option>
                  <option value="admin">Admin — everything</option>
                </Select>
              </div>
            </div>
            <DialogFooter>
              <Button variant="outline" size="sm" onClick={() => setAddOpen(false)}>Cancel</Button>
              <Button size="sm" onClick={submitAdd} disabled={createUser.isPending || !username.trim() || password.length < 10}>
                {createUser.isPending ? "Creating…" : "Create account"}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>

        {/* Edit account */}
        <Dialog open={editing !== null} onOpenChange={(v) => !v && setEditing(null)}>
          <DialogContent className="max-w-sm">
            <DialogHeader>
              <DialogTitle>Edit {editing?.username}</DialogTitle>
              <DialogDescription>
                A password reset signs the account out of every device.
              </DialogDescription>
            </DialogHeader>
            <div className="space-y-3">
              <div className="space-y-1.5">
                <Label htmlFor="edit-user-display">Display name</Label>
                <Input id="edit-user-display" value={editName} onChange={(e) => setEditName(e.target.value)} />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="edit-user-role">Role</Label>
                <Select id="edit-user-role" value={editRole} onChange={(e) => setEditRole(e.target.value)}>
                  <option value="viewer">Viewer — read-only</option>
                  <option value="operator">Operator — manage inventory</option>
                  <option value="admin">Admin — everything</option>
                </Select>
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="edit-user-password">New password (optional)</Label>
                <Input id="edit-user-password" type="password" autoComplete="new-password" value={editPassword}
                  onChange={(e) => setEditPassword(e.target.value)} placeholder="leave blank to keep" />
              </div>
            </div>
            <DialogFooter>
              <Button variant="outline" size="sm" onClick={() => setEditing(null)}>Cancel</Button>
              <Button size="sm" onClick={submitEdit} disabled={updateUser.isPending}>
                {updateUser.isPending ? "Saving…" : "Save changes"}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </CardContent>
    </Card>
  )
}

const passwordSchema = z
  .object({
    current_password: z.string().min(1, "Enter your current password."),
    new_password: z.string().min(10, "Use at least 10 characters.").max(200),
    confirm: z.string().min(1, "Confirm the new password."),
  })
  .refine((data) => data.new_password === data.confirm, {
    path: ["confirm"],
    message: "Passwords do not match.",
  })

type PasswordForm = z.infer<typeof passwordSchema>

function SecurityCard() {
  const changePassword = useChangePassword()
  const form = useForm<PasswordForm>({
    resolver: zodResolver(passwordSchema),
    defaultValues: { current_password: "", new_password: "", confirm: "" },
  })

  const onSubmit = form.handleSubmit((values) => {
    changePassword.mutate(
      { current_password: values.current_password, new_password: values.new_password },
      {
        onSuccess: () => {
          form.reset()
          toast.success("Password changed. Other sessions have been signed out.")
        },
        onError: (err) =>
          form.setError("root", {
            message: isApiError(err) ? err.message : "Could not change the password.",
          }),
      },
    )
  })

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm">Security</CardTitle>
        <CardDescription>
          Changing your password revokes every other active session.
        </CardDescription>
      </CardHeader>
      <CardContent>
        <form onSubmit={onSubmit} className="space-y-3" noValidate>
          {form.formState.errors.root ? (
            <p className="rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive">
              {form.formState.errors.root.message}
            </p>
          ) : null}
          <div className="space-y-2">
            <Label htmlFor="current-password">Current password</Label>
            <Input
              id="current-password"
              type="password"
              autoComplete="current-password"
              {...form.register("current_password")}
            />
            {form.formState.errors.current_password ? (
              <p className="text-xs text-destructive">
                {form.formState.errors.current_password.message}
              </p>
            ) : null}
          </div>
          <div className="grid gap-3 sm:grid-cols-2">
            <div className="space-y-2">
              <Label htmlFor="new-password">New password</Label>
              <Input
                id="new-password"
                type="password"
                autoComplete="new-password"
                {...form.register("new_password")}
              />
              {form.formState.errors.new_password ? (
                <p className="text-xs text-destructive">
                  {form.formState.errors.new_password.message}
                </p>
              ) : null}
            </div>
            <div className="space-y-2">
              <Label htmlFor="confirm-password">Confirm</Label>
              <Input
                id="confirm-password"
                type="password"
                autoComplete="new-password"
                {...form.register("confirm")}
              />
              {form.formState.errors.confirm ? (
                <p className="text-xs text-destructive">{form.formState.errors.confirm.message}</p>
              ) : null}
            </div>
          </div>
          <Button type="submit" size="sm" disabled={changePassword.isPending}>
            {changePassword.isPending ? "Updating…" : "Change password"}
          </Button>
        </form>
      </CardContent>
    </Card>
  )
}

function AccountCard({ username, role }: { username: string; role: string }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm">Account</CardTitle>
        <CardDescription>Your local administrator profile.</CardDescription>
      </CardHeader>
      <CardContent className="space-y-2 text-sm">
        <div className="flex justify-between gap-4">
          <span className="text-muted-foreground">Username</span>
          <span className="font-mono">{username || "—"}</span>
        </div>
        <div className="flex justify-between gap-4">
          <span className="text-muted-foreground">Role</span>
          <span className="font-mono">{role || "—"}</span>
        </div>
        <div className="flex justify-between gap-4">
          <span className="text-muted-foreground">Authentication</span>
          <span>Argon2id · local sessions</span>
        </div>
      </CardContent>
    </Card>
  )
}

function ApiTokensCard() {
  const { data, isLoading } = useApiTokens()
  const createToken = useCreateApiToken()
  const revokeToken = useRevokeApiToken()
  const [name, setName] = useState("")
  const [purpose, setPurpose] = useState<"mcp" | "api">("mcp")
  const [created, setCreated] = useState<ApiTokenCreated | null>(null)
  const [copied, setCopied] = useState(false)
  const [setupOpen, setSetupOpen] = useState(false)

  const origin = typeof window !== "undefined" ? window.location.origin : "http://localhost:8090"
  const mcpConfig = JSON.stringify(
    {
      mcpServers: {
        lanvory: {
          type: "http",
          url: `${origin}/mcp`,
          headers: { Authorization: "Bearer lv_<your-token>" },
        },
      },
    },
    null,
    2,
  )

  const create = () => {
    const trimmed = name.trim()
    if (!trimmed) return
    createToken.mutate(
      { name: trimmed, purpose },
      {
        onSuccess: (res) => {
          setCreated(res)
          setName("")
          setCopied(false)
        },
        onError: (err) => toast.error(isApiError(err) ? err.message : "Could not create token."),
      },
    )
  }

  const revoke = (id: string) => {
    revokeToken.mutate(id, {
      onSuccess: () => toast.success("Token revoked. Clients using it get 401 immediately."),
      onError: (err) => toast.error(isApiError(err) ? err.message : "Could not revoke token."),
    })
  }

  const copy = async () => {
    if (!created) return
    await navigator.clipboard.writeText(created.token)
    setCopied(true)
  }

  // A purpose-scoped token only authenticates the surface it was issued for:
  // MCP tokens work on /mcp, API tokens work on /api/v1 (bearer, no CSRF).
  const tokens: ApiToken[] = data?.tokens ?? []

  return (
    <Card>
      <CardHeader>
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <CardTitle className="text-sm">API tokens</CardTitle>
            <CardDescription>
              Bearer credentials for machine clients. Each token is scoped to one surface — MCP agents
              use <code className="font-mono text-xs">/mcp</code>, scripts use the REST API.{" "}
              The token value is shown exactly once.
            </CardDescription>
          </div>
          <Button variant="outline" size="sm" onClick={() => setSetupOpen(true)}>
            MCP setup example
          </Button>
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        <form
          className="flex flex-wrap items-end gap-2"
          onSubmit={(e) => {
            e.preventDefault()
            create()
          }}
        >
          <div className="w-44 space-y-2">
            <Label htmlFor="tok-purpose">Purpose</Label>
            <Select
              id="tok-purpose"
              value={purpose}
              onChange={(e) => setPurpose(e.target.value as "mcp" | "api")}
            >
              <option value="mcp">MCP agent</option>
              <option value="api">API / scripts</option>
            </Select>
          </div>
          <div className="flex-1 space-y-2">
            <Label htmlFor="tok-name">Name</Label>
            <Input
              id="tok-name"
              placeholder={purpose === "mcp" ? "e.g. homelab-agent" : "e.g. backup-script"}
              value={name}
              maxLength={100}
              onChange={(e) => setName(e.target.value)}
            />
          </div>
          <Button type="submit" size="sm" disabled={createToken.isPending || !name.trim()}>
            {createToken.isPending ? "Creating…" : "Create token"}
          </Button>
        </form>
        <p className="-mt-2 text-xs text-muted-foreground">
          {purpose === "mcp"
            ? "MCP tokens authenticate only the /mcp endpoint and are rejected by the REST API."
            : "API tokens authenticate REST API requests via Authorization header (no CSRF needed) and are rejected by /mcp."}
        </p>

        {isLoading ? (
          <Skeleton className="h-24 w-full" />
        ) : tokens.length === 0 ? (
          <p className="rounded-md border border-dashed px-3 py-6 text-center text-sm text-muted-foreground">
            No tokens yet. Create one to let an agent manage the inventory over MCP, or to call the
            REST API from scripts.
          </p>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Type</TableHead>
                <TableHead>Created</TableHead>
                <TableHead>Last used</TableHead>
                <TableHead>Status</TableHead>
                <TableHead className="w-0 text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {tokens.map((t) => (
                <TableRow key={t.id}>
                  <TableCell className="font-medium">{t.name}</TableCell>
                  <TableCell>
                    {t.purpose === "api" ? (
                      <Badge variant="outline">API</Badge>
                    ) : (
                      <Badge variant="secondary">MCP</Badge>
                    )}
                  </TableCell>
                  <TableCell className="text-muted-foreground">{formatDateTime(t.created_at)}</TableCell>
                  <TableCell className="text-muted-foreground">
                    {t.last_used_at ? formatDateTime(t.last_used_at) : "Never"}
                  </TableCell>
                  <TableCell>
                    {t.revoked_at ? (
                      <Badge variant="secondary">Revoked</Badge>
                    ) : (
                      <Badge>Active</Badge>
                    )}
                  </TableCell>
                  <TableCell className="text-right">
                    {t.revoked_at ? null : (
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={() => revoke(t.id)}
                        disabled={revokeToken.isPending}
                      >
                        Revoke
                      </Button>
                    )}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}

        <Dialog open={setupOpen} onOpenChange={setSetupOpen}>
          <DialogContent className="sm:max-w-xl">
            <DialogHeader>
              <DialogTitle>MCP client setup</DialogTitle>
              <DialogDescription>
                Create an <strong>MCP agent</strong> token above, then paste this configuration into
                your MCP client. The endpoint speaks streamable HTTP with Bearer auth — no local
                bridge needed.
              </DialogDescription>
            </DialogHeader>
            <div className="space-y-3">
              <div>
                <p className="mb-1 text-xs font-medium text-muted-foreground">Client config</p>
                <pre className="overflow-x-auto rounded-md border bg-muted px-3 py-2 font-mono text-xs">
                  {mcpConfig}
                </pre>
              </div>
              <div>
                <p className="mb-1 text-xs font-medium text-muted-foreground">
                  Or call it directly (smoke test)
                </p>
                <pre className="overflow-x-auto rounded-md border bg-muted px-3 py-2 font-mono text-xs">{`curl -s ${origin}/mcp \\
  -H "Authorization: Bearer lv_<your-token>" \\
  -H "Content-Type: application/json" \\
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'`}</pre>
              </div>
              <div>
                <p className="mb-1 text-xs font-medium text-muted-foreground">
                  REST API token (different purpose)
                </p>
                <pre className="overflow-x-auto rounded-md border bg-muted px-3 py-2 font-mono text-xs">{`curl -s ${origin}/api/v1/assets \\
  -H "Authorization: Bearer lv_<your-api-token>"`}</pre>
              </div>
              <p className="text-xs text-muted-foreground">
                Full tool list and payload reference: <code className="font-mono">docs/MCP.md</code>.
              </p>
            </div>
            <DialogFooter>
              <Button variant="outline" onClick={() => setSetupOpen(false)}>Close</Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>

        <Dialog open={created !== null} onOpenChange={(open) => !open && setCreated(null)}>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>Token created</DialogTitle>
              <DialogDescription>
                Copy it now — this value is never shown again. Anyone holding it can manage the
                inventory, so treat it like a password.
              </DialogDescription>
            </DialogHeader>
            <div className="space-y-3">
              <div className="rounded-md border bg-muted px-3 py-2">
                <p className="break-all font-mono text-xs">{created?.token}</p>
              </div>
              <p className="text-xs text-muted-foreground">
                {created?.purpose === "api" ? (
                  <>
                    Use it as <code className="font-mono">Authorization: Bearer &lt;token&gt;</code>{" "}
                    against the REST API (e.g. <code className="font-mono">/api/v1/assets</code>) — no
                    CSRF token needed.
                  </>
                ) : (
                  <>
                    Use it as <code className="font-mono">Authorization: Bearer &lt;token&gt;</code>{" "}
                    against the MCP endpoint <code className="font-mono">/mcp</code> — see{" "}
                    <code className="font-mono">docs/MCP.md</code>.
                  </>
                )}
              </p>
            </div>
            <DialogFooter>
              <Button variant="outline" onClick={copy}>
                {copied ? "Copied" : "Copy token"}
              </Button>
              <Button onClick={() => setCreated(null)}>Done</Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </CardContent>
    </Card>
  )
}
