// Users page tests — admin user management.
//
// Covers: rendering the directory, the self-lockout guards in the UI, creating a
// user, disabling a user, and deleting a user behind the confirm dialog.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Users } from '../pages/Users'
import { renderWithProviders } from './helpers'
import { setSession } from '../lib/session'
import type { AppUser } from '../api/types'

const ADMIN: AppUser = {
  username: 'admin',
  role: 'admin',
  disabled: false,
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
}

const ANA: AppUser = {
  username: 'ana',
  role: 'analyst',
  disabled: false,
  created_at: '2026-02-01T00:00:00Z',
  updated_at: '2026-02-01T00:00:00Z',
}

interface Call {
  method: string
  url: string
  body: unknown
}

let calls: Call[] = []
let users: AppUser[]

function installFetch() {
  calls = []
  users = [ADMIN, ANA]
  vi.stubGlobal('fetch', async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input)
    const method = init?.method ?? 'GET'
    const body = init?.body ? JSON.parse(String(init.body)) : undefined
    calls.push({ method, url, body })

    const respond = (data: unknown, status = 200) =>
      ({ ok: status < 300, status, statusText: 'OK', json: async () => ({ data }) }) as unknown as Response

    if (url.endsWith('/users') && method === 'GET') return respond(users)
    if (url.endsWith('/users') && method === 'POST') {
      const created: AppUser = {
        username: body.username, role: body.role, disabled: false,
        created_at: '2026-03-01T00:00:00Z', updated_at: '2026-03-01T00:00:00Z',
      }
      users = [...users, created]
      return respond(created, 201)
    }
    if (method === 'PATCH') {
      const username = decodeURIComponent(url.split('/').pop() ?? '')
      users = users.map(u => (u.username === username ? { ...u, ...body } : u))
      return respond(users.find(u => u.username === username))
    }
    if (method === 'DELETE') {
      const username = decodeURIComponent(url.split('/').pop() ?? '')
      users = users.filter(u => u.username !== username)
      return respond({ status: 'deleted', username })
    }
    return respond([])
  })
}

beforeEach(() => {
  localStorage.clear()
  setSession('tok', { username: 'admin', role: 'admin' })
  installFetch()
})

afterEach(() => { vi.unstubAllGlobals() })

describe('Users page', () => {
  it('lists accounts and explains the two roles', async () => {
    renderWithProviders(<Users />)

    const table = await screen.findByRole('table', { name: 'User accounts' })
    expect(within(table).getByText('admin')).toBeInTheDocument()
    expect(within(table).getByText('ana')).toBeInTheDocument()

    // Role differences are shown on the page (in the role cards).
    const roleCards = document.querySelector('.role-grid') as HTMLElement
    expect(within(roleCards).getByText('Analyst')).toBeInTheDocument()
    expect(within(roleCards).getByText('Administrator')).toBeInTheDocument()
  })

  it('marks the signed-in account and blocks self-management', async () => {
    renderWithProviders(<Users />)
    await screen.findByRole('table', { name: 'User accounts' })

    const table = screen.getByRole('table', { name: 'User accounts' })
    const adminRow = within(table).getByText('admin').closest('tr')!
    const anaRow = within(table).getByText('ana').closest('tr')!

    // The operator's own row is labelled and its destructive controls disabled.
    expect(within(adminRow).getByText('you')).toBeInTheDocument()
    expect(within(adminRow).getByRole('combobox')).toBeDisabled()
    expect(within(adminRow).getByRole('button', { name: /disable/i })).toBeDisabled()
    expect(within(adminRow).getByRole('button', { name: /delete/i })).toBeDisabled()

    // Another account's controls are enabled.
    expect(within(anaRow).getByRole('combobox')).toBeEnabled()
    expect(within(anaRow).getByRole('button', { name: /delete/i })).toBeEnabled()
  })

  it('creates a user', async () => {
    const user = userEvent.setup()
    renderWithProviders(<Users />)
    await screen.findByRole('table', { name: 'User accounts' })

    await user.click(screen.getByRole('button', { name: /add user/i }))
    await user.type(screen.getByLabelText('Username'), 'j.rivera')
    await user.type(screen.getByLabelText('Password'), 'secret123')
    await user.selectOptions(screen.getByLabelText('Role'), 'admin')
    await user.click(screen.getByRole('button', { name: /create user/i }))

    await waitFor(() => {
      const post = calls.find(c => c.method === 'POST' && c.url.endsWith('/users'))
      expect(post?.body).toEqual({ username: 'j.rivera', password: 'secret123', role: 'admin' })
    })
    expect(await screen.findByText('j.rivera')).toBeInTheDocument()
  })

  it('will not submit an invalid user', async () => {
    const user = userEvent.setup()
    renderWithProviders(<Users />)
    await screen.findByRole('table', { name: 'User accounts' })

    await user.click(screen.getByRole('button', { name: /add user/i }))
    // Bad username (space) and a too-short password.
    await user.type(screen.getByLabelText('Username'), 'bad name')
    await user.type(screen.getByLabelText('Password'), '123')

    expect(screen.getByRole('button', { name: /create user/i })).toBeDisabled()
  })

  it('disables a user', async () => {
    const user = userEvent.setup()
    renderWithProviders(<Users />)
    await screen.findByRole('table', { name: 'User accounts' })

    const anaRow = within(screen.getByRole('table', { name: 'User accounts' }))
      .getByText('ana').closest('tr')!
    await user.click(within(anaRow).getByRole('button', { name: /disable/i }))

    await waitFor(() => {
      const patch = calls.find(c => c.method === 'PATCH' && c.url.includes('/users/ana'))
      expect(patch?.body).toEqual({ disabled: true })
    })
  })

  it('deletes a user only after confirmation', async () => {
    const user = userEvent.setup()
    renderWithProviders(<Users />)
    await screen.findByRole('table', { name: 'User accounts' })

    const table = screen.getByRole('table', { name: 'User accounts' })
    const anaRow = within(table).getByText('ana').closest('tr')!
    await user.click(within(anaRow).getByRole('button', { name: /delete/i }))

    // Confirmation is required first.
    const dialog = await screen.findByRole('dialog')
    expect(calls.some(c => c.method === 'DELETE')).toBe(false)

    // Cancelling does not delete.
    await user.click(within(dialog).getByRole('button', { name: /^cancel$/i }))
    expect(calls.some(c => c.method === 'DELETE')).toBe(false)

    // Confirming does.
    await user.click(within(anaRow).getByRole('button', { name: /delete/i }))
    const dialog2 = await screen.findByRole('dialog')
    await user.click(within(dialog2).getByRole('button', { name: /^delete$/i }))

    await waitFor(() => {
      expect(calls.some(c => c.method === 'DELETE' && c.url.includes('/users/ana'))).toBe(true)
    })
    await waitFor(() => expect(screen.queryByText('ana')).not.toBeInTheDocument())
  })
})
