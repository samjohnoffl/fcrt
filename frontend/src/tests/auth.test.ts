// Authentication/session tests + API client envelope-unwrap behaviour.
// No network: fetch and localStorage are the only external surfaces.

import { describe, it, expect, beforeEach, vi } from 'vitest'
import {
  getToken, getUser, setSession, clearSession, isAuthenticated, hasRole,
} from '../lib/session'
import { api } from '../api/client'
import type { Principal } from '../api/types'

const admin: Principal = { username: 'admin', role: 'admin' }

describe('session storage', () => {
  beforeEach(() => localStorage.clear())

  it('starts unauthenticated', () => {
    expect(getToken()).toBe('')
    expect(getUser()).toBeNull()
    expect(isAuthenticated()).toBe(false)
  })

  it('stores and reads a session', () => {
    setSession('tok-123', admin)
    expect(getToken()).toBe('tok-123')
    expect(getUser()).toEqual(admin)
    expect(isAuthenticated()).toBe(true)
  })

  it('clears a session', () => {
    setSession('tok-123', admin)
    clearSession()
    expect(getToken()).toBe('')
    expect(getUser()).toBeNull()
    expect(isAuthenticated()).toBe(false)
  })

  it('ignores a corrupt cached user', () => {
    localStorage.setItem('aml_id_token', 'tok')
    localStorage.setItem('aml_user', 'not json')
    expect(getUser()).toBeNull()
    expect(isAuthenticated()).toBe(false)
  })

  it('rejects a cached user with an unknown role', () => {
    localStorage.setItem('aml_id_token', 'tok')
    localStorage.setItem('aml_user', JSON.stringify({ username: 'x', role: 'root' }))
    expect(getUser()).toBeNull()
  })

  it('rejects the removed "viewer" role', () => {
    localStorage.setItem('aml_id_token', 'tok')
    localStorage.setItem('aml_user', JSON.stringify({ username: 'x', role: 'viewer' }))
    expect(getUser()).toBeNull()
    expect(isAuthenticated()).toBe(false)
  })
})

describe('hasRole (hierarchical)', () => {
  beforeEach(() => localStorage.clear())

  const cases: Array<[Principal['role'], boolean, boolean]> = [
    // role,       canAnalyst, canAdmin
    ['analyst', true,  false],
    ['admin',   true,  true],
  ]

  it.each(cases)('%s role gates correctly', (role, analyst, adm) => {
    setSession('tok', { username: 'u', role })
    expect(hasRole('analyst')).toBe(analyst)
    expect(hasRole('admin')).toBe(adm)
  })

  it('denies when signed out', () => {
    expect(hasRole('analyst')).toBe(false)
    expect(hasRole('admin')).toBe(false)
  })
})

describe('api client envelope handling', () => {
  const fetchMock = vi.fn()

  beforeEach(() => {
    localStorage.clear()
    fetchMock.mockReset()
    vi.stubGlobal('fetch', fetchMock)
  })

  const ok = (body: unknown) => ({
    ok: true, status: 200, json: async () => body,
  })

  it('unwraps a {data: ...} envelope', async () => {
    fetchMock.mockResolvedValue(ok({ data: { username: 'admin', role: 'admin' } }))
    const me = await api.get<Principal>('/auth/me')
    expect(me).toEqual({ username: 'admin', role: 'admin' })
  })

  it('passes a bare array through unchanged', async () => {
    fetchMock.mockResolvedValue(ok([{ id: '1' }]))
    const rows = await api.get<{ id: string }[]>('/cohorts')
    expect(rows).toEqual([{ id: '1' }])
  })

  it('passes a non-envelope object through unchanged', async () => {
    fetchMock.mockResolvedValue(ok({ ok: true, total: 3 }))
    const res = await api.get<{ ok: boolean; total: number }>('/regression/pack')
    expect(res).toEqual({ ok: true, total: 3 })
  })

  it('attaches the bearer token when present', async () => {
    setSession('tok-abc', admin)
    fetchMock.mockResolvedValue(ok({ data: [] }))
    await api.get('/cohorts')
    const init = fetchMock.mock.calls[0][1] as RequestInit
    expect((init.headers as Record<string, string>)['Authorization']).toBe('Bearer tok-abc')
  })

  it('clears the session on 401', async () => {
    setSession('tok-abc', admin)
    // Sit on /login so the client's redirect is a no-op in jsdom.
    window.history.replaceState({}, '', '/login')
    fetchMock.mockResolvedValue({
      ok: false, status: 401, statusText: 'Unauthorized',
      json: async () => ({ code: 'token_expired', message: 'expired' }),
    })
    await expect(api.get('/cohorts')).rejects.toMatchObject({ status: 401 })
    expect(getToken()).toBe('')
    expect(getUser()).toBeNull()
  })
})
