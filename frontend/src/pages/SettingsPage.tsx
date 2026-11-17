import { useEffect, useState } from "react"
import { zodResolver } from "@hookform/resolvers/zod"
import { useForm } from "react-hook-form"
import { toast } from "sonner"
import { z } from "zod"

import { isApiError } from "@/api/client"
import { useApiTokens, useChangePassword, useCreateApiToken, useMe, useRevokeApiToken, useSettings, useTestNotification, useUpdateSettings } from "@/api/hooks"
import type { ApiToken, ApiTokenCreated } from "@/api/types"
import { PageHeader } from "@/components/PageHeader"
import { useTheme } from "@/components/theme-provider"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
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
        <AppearanceCard />
        <SecurityCard />
        <AccountCard username={user?.username ?? ""} role={user?.role ?? ""} />
        <div className="lg:col-span-2">
          <ApiTokensCard />
        </div>
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
