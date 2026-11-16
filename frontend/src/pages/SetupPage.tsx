import { zodResolver } from "@hookform/resolvers/zod"
import { useForm } from "react-hook-form"
import { useNavigate } from "react-router-dom"
import { z } from "zod"

import { isApiError } from "@/api/client"
import { useSetupAdmin, useSetupStatus } from "@/api/hooks"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { AuthLayout } from "./LoginPage"

const setupSchema = z
  .object({
    username: z
      .string()
      .min(3, "At least 3 characters.")
      .max(32, "At most 32 characters.")
      .regex(/^[a-zA-Z0-9][a-zA-Z0-9._-]*$/, "Letters, digits, '.', '_' and '-' only."),
    display_name: z.string().max(64, "At most 64 characters.").optional(),
    password: z.string().min(10, "Use at least 10 characters.").max(200),
    confirm: z.string().min(1, "Confirm your password."),
  })
  .refine((data) => data.password === data.confirm, {
    path: ["confirm"],
    message: "Passwords do not match.",
  })

type SetupForm = z.infer<typeof setupSchema>

export function SetupPage() {
  const navigate = useNavigate()
  const setupStatus = useSetupStatus()
  const setupAdmin = useSetupAdmin()

  const form = useForm<SetupForm>({
    resolver: zodResolver(setupSchema),
    defaultValues: { username: "", display_name: "", password: "", confirm: "" },
  })

  if (!setupStatus.isLoading && !setupStatus.data?.needs_setup) {
    return (
      <AuthLayout>
        <Card className="w-full max-w-sm">
          <CardHeader>
            <CardTitle className="text-lg">Setup already completed</CardTitle>
            <CardDescription>
              An administrator account exists. Sign in to continue.
            </CardDescription>
          </CardHeader>
          <CardContent>
            <Button className="w-full" onClick={() => navigate("/login")}>
              Go to sign in
            </Button>
          </CardContent>
        </Card>
      </AuthLayout>
    )
  }

  const onSubmit = form.handleSubmit((values) => {
    setupAdmin.mutate(
      {
        username: values.username,
        display_name: values.display_name || "",
        password: values.password,
      },
      {
        onSuccess: () => navigate("/", { replace: true }),
        onError: (err) =>
          form.setError("root", {
            message: isApiError(err) ? err.message : "Could not create the administrator.",
          }),
      },
    )
  })

  return (
    <AuthLayout>
      <Card className="w-full max-w-sm">
        <CardHeader>
          <CardTitle className="text-lg">Create your administrator</CardTitle>
          <CardDescription>
            This is the first-run setup. The first account owns the instance.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <form onSubmit={onSubmit} className="space-y-4" noValidate>
            {form.formState.errors.root ? (
              <p className="rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive">
                {form.formState.errors.root.message}
              </p>
            ) : null}
            <div className="space-y-2">
              <Label htmlFor="username">Username</Label>
              <Input id="username" autoComplete="username" autoFocus {...form.register("username")} />
              {form.formState.errors.username ? (
                <p className="text-xs text-destructive">{form.formState.errors.username.message}</p>
              ) : null}
            </div>
            <div className="space-y-2">
              <Label htmlFor="display_name">
                Display name <span className="font-normal text-muted-foreground">(optional)</span>
              </Label>
              <Input id="display_name" {...form.register("display_name")} />
            </div>
            <div className="space-y-2">
              <Label htmlFor="password">Password</Label>
              <Input
                id="password"
                type="password"
                autoComplete="new-password"
                {...form.register("password")}
              />
              <p className="text-xs text-muted-foreground">Minimum 10 characters.</p>
              {form.formState.errors.password ? (
                <p className="text-xs text-destructive">{form.formState.errors.password.message}</p>
              ) : null}
            </div>
            <div className="space-y-2">
              <Label htmlFor="confirm">Confirm password</Label>
              <Input
                id="confirm"
                type="password"
                autoComplete="new-password"
                {...form.register("confirm")}
              />
              {form.formState.errors.confirm ? (
                <p className="text-xs text-destructive">{form.formState.errors.confirm.message}</p>
              ) : null}
            </div>
            <Button type="submit" className="w-full" disabled={setupAdmin.isPending}>
              {setupAdmin.isPending ? "Creating…" : "Create administrator"}
            </Button>
          </form>
        </CardContent>
      </Card>
    </AuthLayout>
  )
}
