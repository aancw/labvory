import { zodResolver } from "@hookform/resolvers/zod"
import { useForm } from "react-hook-form"
import { useState } from "react"
import type { ReactNode } from "react"
import { Link, useNavigate } from "react-router-dom"
import { z } from "zod"
import { KeyRoundIcon } from "lucide-react"

import { isApiError } from "@/api/client"
import { useLogin, useSetupStatus } from "@/api/hooks"
import { LogoMark } from "@/components/layout/Sidebar"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"

const loginSchema = z.object({
  username: z.string().min(1, "Username is required."),
  password: z.string().min(1, "Password is required."),
  totp_code: z.string().max(6).optional(),
})

type LoginForm = z.infer<typeof loginSchema>

export function LoginPage() {
  const navigate = useNavigate()
  const login = useLogin()
  const setupStatus = useSetupStatus()
  const [needsTotp, setNeedsTotp] = useState(false)

  const form = useForm<LoginForm>({
    resolver: zodResolver(loginSchema),
    defaultValues: { username: "", password: "", totp_code: "" },
  })

  const onSubmit = form.handleSubmit((values) => {
    login.mutate(
      {
        username: values.username,
        password: values.password,
        ...(values.totp_code ? { totp_code: values.totp_code } : {}),
      },
      {
        onSuccess: () => navigate("/", { replace: true }),
        onError: (err) => {
          if (isApiError(err) && err.code === "totp_required") {
            setNeedsTotp(true)
            form.setError("root", {
              message: "Two-factor is enabled — enter the 6-digit code from your authenticator.",
            })
            return
          }
          if (isApiError(err) && err.code === "invalid_totp") {
            form.setError("root", { message: "Invalid or expired verification code." })
            return
          }
          form.setError("root", {
            message: isApiError(err) ? err.message : "Login failed. Try again.",
          })
        },
      },
    )
  })

  return (
    <AuthLayout>
      <Card className="w-full max-w-sm">
        <CardHeader>
          <CardTitle className="text-lg">{needsTotp ? "Two-factor required" : "Sign in"}</CardTitle>
          <CardDescription>
            {needsTotp
              ? "Your password was accepted — confirm with your authenticator app."
              : "Access your infrastructure inventory."}
          </CardDescription>
        </CardHeader>
        <CardContent>
          {setupStatus.data?.needs_setup && !needsTotp ? (
            <div className="mb-4 rounded-md border border-amber-500/30 bg-amber-500/10 px-3 py-2.5 text-sm text-amber-700 dark:text-amber-400">
              First run detected —{" "}
              <Link to="/setup" className="font-medium underline underline-offset-2">
                create your administrator account
              </Link>
              .
            </div>
          ) : null}

          <form onSubmit={onSubmit} className="space-y-4" noValidate>
            {form.formState.errors.root ? (
              <p className="rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive">
                {form.formState.errors.root.message}
              </p>
            ) : null}
            {needsTotp ? (
              <div className="space-y-2">
                <Label htmlFor="totp-code" className="inline-flex items-center gap-1.5">
                  <KeyRoundIcon className="size-3.5" aria-hidden /> Verification code
                </Label>
                <Input
                  id="totp-code"
                  className="text-center font-mono text-lg tracking-[0.4em]"
                  inputMode="numeric"
                  autoComplete="one-time-code"
                  maxLength={6}
                  autoFocus
                  placeholder="000000"
                  {...form.register("totp_code")}
                />
              </div>
            ) : (
              <>
                <div className="space-y-2">
                  <Label htmlFor="username">Username</Label>
                  <Input
                    id="username"
                    autoComplete="username"
                    autoFocus
                    {...form.register("username")}
                  />
                  {form.formState.errors.username ? (
                    <p className="text-xs text-destructive">
                      {form.formState.errors.username.message}
                    </p>
                  ) : null}
                </div>
                <div className="space-y-2">
                  <Label htmlFor="password">Password</Label>
                  <Input
                    id="password"
                    type="password"
                    autoComplete="current-password"
                    {...form.register("password")}
                  />
                  {form.formState.errors.password ? (
                    <p className="text-xs text-destructive">
                      {form.formState.errors.password.message}
                    </p>
                  ) : null}
                </div>
              </>
            )}
            <Button type="submit" className="w-full" disabled={login.isPending}>
              {login.isPending ? (needsTotp ? "Verifying…" : "Signing in…") : needsTotp ? "Verify" : "Sign in"}
            </Button>
          </form>
        </CardContent>
      </Card>
    </AuthLayout>
  )
}

export function AuthLayout({ children }: { children: ReactNode }) {
  return (
    <div className="flex min-h-screen items-center justify-center bg-muted/30 px-4">
      <div className="w-full max-w-sm">
        <div className="mb-6 flex items-center justify-center gap-2.5">
          <LogoMark className="size-9 rounded-lg" />
          <div className="leading-tight">
            <p className="text-lg font-semibold tracking-tight">Lanvory</p>
            <p className="text-xs text-muted-foreground">Homelab Infrastructure Manager</p>
          </div>
        </div>
        {children}
        <p className="mt-6 text-center text-xs text-muted-foreground">
          Local-first · Self-hosted · Session cookies are required
        </p>
      </div>
    </div>
  )
}
