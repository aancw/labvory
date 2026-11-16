// Typed fetch client: same-origin cookies, double-submit CSRF, uniform error envelope.

export class ApiError extends Error {
  constructor(
    public code: string,
    public message: string,
    public status: number,
    public details?: unknown,
  ) {
    super(message)
    this.name = "ApiError"
  }
}

let csrfToken: string | null = null

/** Fired when the server reports the session itself is unusable (expired,
 *  revoked, or the session store failed) so the UI can force a re-login. */
export const SESSION_INVALID_EVENT = "lanvory:session-invalid"

/** True when an error means "this session cannot be used anymore". */
export function isSessionInvalid(error: unknown): boolean {
  if (!(error instanceof ApiError)) return false
  if (error.status === 401) return true
  // The session store failing (e.g. DB unavailable mid-request) is not
  // recoverable client-side either — treat it like an expired session.
  return error.status >= 500 && /session lookup failed/i.test(error.message)
}

export function resetCsrf(): void {
  csrfToken = null
}

function notifySessionInvalid(message: string): void {
  if (typeof window === "undefined") return
  window.dispatchEvent(new CustomEvent(SESSION_INVALID_EVENT, { detail: { message } }))
}

async function ensureCsrf(): Promise<string> {
  if (csrfToken) return csrfToken
  const res = await fetch("/api/v1/csrf", { credentials: "include" })
  if (!res.ok) {
    throw new ApiError("csrf", "Could not obtain CSRF token.", res.status)
  }
  const data = (await res.json()) as { token: string }
  csrfToken = data.token
  return csrfToken
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = {}
  if (body !== undefined) headers["Content-Type"] = "application/json"
  if (method !== "GET" && method !== "HEAD") {
    headers["X-CSRF-Token"] = await ensureCsrf()
  }

  const init: RequestInit = { method, headers, credentials: "include" }
  if (body !== undefined) init.body = JSON.stringify(body)

  const res = await fetch(path, init)

  if (!res.ok) {
    let code = "error"
    let message = `Request failed (${res.status})`
    let details: unknown
    try {
      const data = (await res.json()) as {
        error?: { code?: string; message?: string; details?: unknown }
      }
      if (data.error) {
        code = data.error.code ?? code
        message = data.error.message ?? message
        details = data.error.details
      }
    } catch {
      // Non-JSON error body; keep defaults.
    }
    // A rejected CSRF token means our cached token is stale — clear it for the next call.
    if (res.status === 403 && code === "csrf") {
      csrfToken = null
    }
    const apiError = new ApiError(code, message, res.status, details)
    if (isSessionInvalid(apiError)) {
      csrfToken = null
      notifySessionInvalid(message)
    }
    throw apiError
  }

  if (res.status === 204) return undefined as T
  return (await res.json()) as T
}

export const api = {
  get: <T>(path: string) => request<T>("GET", path),
  post: <T>(path: string, body?: unknown) => request<T>("POST", path, body),
  put: <T>(path: string, body?: unknown) => request<T>("PUT", path, body),
  delete: <T>(path: string) => request<T>("DELETE", path),
}

export function isApiError(value: unknown): value is ApiError {
  return value instanceof ApiError
}
