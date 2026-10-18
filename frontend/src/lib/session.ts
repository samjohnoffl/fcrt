// Session storage + role helpers.
//
// Deliberately dependency-free (type-only imports) so it can be used by both
// the API client and React components without an import cycle.
//
// The token lives in localStorage under 'aml_id_token' (the key the API client
// already reads). The decoded Principal is cached under 'aml_user' purely for
// UI decisions — the server is always the source of truth for authorization.

import type { Principal, Role } from '../api/types'

const TOKEN_KEY = 'aml_id_token'
const USER_KEY = 'aml_user'

const RANK: Record<Role, number> = { analyst: 1, admin: 2 }

export function getToken(): string {
  try {
    return localStorage.getItem(TOKEN_KEY) ?? ''
  } catch {
    return ''
  }
}

export function getUser(): Principal | null {
  try {
    const raw = localStorage.getItem(USER_KEY)
    if (!raw) return null
    const parsed = JSON.parse(raw) as Principal
    if (!parsed?.username || !(parsed.role in RANK)) return null
    return parsed
  } catch {
    return null
  }
}

export function setSession(token: string, user: Principal): void {
  try {
    localStorage.setItem(TOKEN_KEY, token)
    localStorage.setItem(USER_KEY, JSON.stringify(user))
  } catch {
    /* storage unavailable (private mode) — session lasts for this page only */
  }
}

export function clearSession(): void {
  try {
    localStorage.removeItem(TOKEN_KEY)
    localStorage.removeItem(USER_KEY)
  } catch {
    /* ignore */
  }
}

export function isAuthenticated(): boolean {
  return getToken() !== '' && getUser() !== null
}

/** True when the current user's role meets the minimum required role. */
export function hasRole(min: Role): boolean {
  const user = getUser()
  if (!user) return false
  return (RANK[user.role] ?? 0) >= RANK[min]
}
