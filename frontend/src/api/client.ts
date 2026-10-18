// HTTP client — wraps fetch with auth header, base URL, and error parsing.
// Auth token read from localStorage key 'aml_id_token'.
// All errors throw an ApiError-shaped object so callers can pattern-match.

import type { ApiError } from './types'
import { getToken, clearSession } from '../lib/session'

// API base URL.
//
// Default: same origin, which covers the Vite dev proxy and the Firebase
// Hosting `/api/**` rewrite to a Cloud Run backend.
//
// When the backend lives on another origin (any non-GCP container host), set
// VITE_API_BASE_URL to that origin at build time — e.g.
// VITE_API_BASE_URL=https://fcrt-api.fly.dev npm run build
const API_ORIGIN = (import.meta.env.VITE_API_BASE_URL ?? '').replace(/\/+$/, '')
const BASE = `${API_ORIGIN}/api/v1`

function token(): string {
  return getToken()
}

/** Called when the server rejects our token so the UI can return to /login. */
function onUnauthorized(): void {
  clearSession()
  if (typeof window !== 'undefined' && window.location.pathname !== '/login') {
    window.location.assign('/login')
  }
}

export class FetchError extends Error {
  status: number
  code: string
  constructor(status: number, message: string, code = 'UNKNOWN') {
    super(message)
    this.status = status
    this.code = code
    this.name = 'FetchError'
  }
}

async function request<T>(
  method: string,
  path: string,
  body?: unknown,
): Promise<T> {
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
  }
  const tok = token()
  if (tok) headers['Authorization'] = `Bearer ${tok}`

  const res = await fetch(`${BASE}${path}`, {
    method,
    headers,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  })

  if (!res.ok) {
    let msg = res.statusText
    let code = 'HTTP_ERROR'
    try {
      const err = await res.json() as ApiError
      msg = err.error ?? err.message ?? msg
      code = err.code ?? code
    } catch { /* ignore parse failure */ }
    // Expired/invalid session → drop it and return to the login screen.
    if (res.status === 401) onUnauthorized()
    throw new FetchError(res.status, msg, code)
  }

  // 204 No Content
  if (res.status === 204) return undefined as unknown as T

  let json: unknown
  try {
    json = await res.json()
  } catch {
    // A 2xx that is not JSON means the API did not answer us — typically a static
    // host serving index.html for an unrouted /api/** path because no backend is
    // deployed at this origin. Surface that instead of a raw parse error.
    throw new FetchError(
      res.status,
      'The API is not reachable from this site. No backend is deployed for this origin.',
      'BAD_RESPONSE',
    )
  }
  // Success responses are wrapped as {"data": ...} by internal/api/response.go.
  // Unwrap a single-key {data} envelope; pass anything else through unchanged
  // (a bare array, or a raw document like the regression pack).
  if (
    json !== null &&
    typeof json === 'object' &&
    !Array.isArray(json) &&
    'data' in (json as Record<string, unknown>) &&
    Object.keys(json as Record<string, unknown>).length === 1
  ) {
    return (json as { data: T }).data
  }
  return json as T
}

/** Downloads a file from the API with the auth header attached. */
async function download(path: string, filename: string): Promise<void> {
  const headers: Record<string, string> = {}
  const tok = token()
  if (tok) headers['Authorization'] = `Bearer ${tok}`

  const res = await fetch(`${BASE}${path}`, { headers })
  if (!res.ok) {
    if (res.status === 401) onUnauthorized()
    throw new FetchError(res.status, res.statusText, 'DOWNLOAD_FAILED')
  }

  const blob = await res.blob()
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  a.remove()
  URL.revokeObjectURL(url)
}

export const api = {
  get:      <T>(path: string)               => request<T>('GET',   path),
  post:     <T>(path: string, body: unknown) => request<T>('POST',  path, body),
  patch:    <T>(path: string, body?: unknown)=> request<T>('PATCH', path, body),
  delete:   <T>(path: string)               => request<T>('DELETE', path),
  download,
}
