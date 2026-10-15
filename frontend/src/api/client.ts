// HTTP client — wraps fetch with auth header, base URL, and error parsing.
// Auth token read from localStorage key 'aml_id_token'.
// All errors throw an ApiError-shaped object so callers can pattern-match.

import type { ApiError } from './types'

const BASE = '/api/v1'

function token(): string {
  return localStorage.getItem('aml_id_token') ?? ''
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
      msg = err.error ?? msg
      code = err.code ?? code
    } catch { /* ignore parse failure */ }
    throw new FetchError(res.status, msg, code)
  }

  // 204 No Content
  if (res.status === 204) return undefined as unknown as T
  return res.json() as Promise<T>
}

export const api = {
  get:    <T>(path: string)               => request<T>('GET',   path),
  post:   <T>(path: string, body: unknown) => request<T>('POST',  path, body),
  patch:  <T>(path: string, body?: unknown)=> request<T>('PATCH', path, body),
  delete: <T>(path: string)               => request<T>('DELETE', path),
}
