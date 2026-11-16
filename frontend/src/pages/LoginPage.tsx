import { zodResolver } from "@hookform/resolvers/zod"
import { useForm } from "react-hook-form"
import type { ReactNode } from "react"
import { Link, useNavigate } from "react-router-dom"
import { z } from "zod"

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
})

type LoginForm = z.infer<typeof loginSchema>

export function LoginPage() {
  const navigate = useNavigate()
  const login = useLogin()
  const setupStatus = useSetupStatus()

  const form = useForm<LoginForm>({
    resolver: zodResolver(loginSchema),
    defaultValues: { username: "", password: "" },
  })

  const onSubmit = form.handleSubmit((values) => {
    login.mutate(values, {
      onSuccess: () => navigate("/", { replace: true }),
      onError: (err) =>
        form.setError("root", {
          message: isApiError(err) ? err.message : "Login failed. Try again.",
        }),
    })
  })

  return (
    <AuthLayout>
      <Card className="w-full max-w-sm">
        <CardHeader>
          <CardTitle className="text-lg">Sign in</CardTitle>
          <CardDescription>Access your infrastructure inventory.</CardDescription>
        </CardHeader>
        <CardContent>
          {setupStatus.data?.needs_setup ? (
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
            <Button type="submit" className="w-full" disabled={login.isPending}>
              {login.isPending ? "Signing in…" : "Sign in"}
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
