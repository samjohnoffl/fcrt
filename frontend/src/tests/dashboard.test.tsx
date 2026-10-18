// Dashboard tests — the primary landing surface: coverage, headline metrics and
// the recent-experiments table. fetch is stubbed; no network.

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { screen, waitFor, within } from '@testing-library/react'
import { Dashboard } from '../pages/Dashboard'
import { renderWithProviders, stubFetch } from './helpers'
import { setSession } from '../lib/session'
import type { ExperimentPair, Role } from '../api/types'

const coverage = {
  total_rules: 10,
  covered_rules: 4,
  coverage_pct: 40,
  rules: [],
}

function experiment(over: Partial<ExperimentPair> = {}): ExperimentPair {
  return {
    id: '11111111-1111-1111-1111-111111111111',
    cohort_id: '22222222-2222-2222-2222-222222222222',
    name: 'structuring-v3',
    status: 'completed',
    control_hash: 'a'.repeat(64),
    manifest_hash: 'b'.repeat(64),
    seed: 42,
    declared_change: 'Split payments to stay under the reporting threshold',
    mutation_type: 'split_transaction',
    created_at: '2026-05-01T10:00:00Z',
    evasion_delta: 3,
    ...over,
  }
}

function signIn(role: Role) {
  setSession('tok', { username: role === 'admin' ? 'admin' : 'analyst', role })
}

beforeEach(() => {
  localStorage.clear()
  vi.unstubAllGlobals()
})

describe('Dashboard', () => {
  it('renders headline metrics from the coverage and experiment APIs', async () => {
    signIn('admin')
    vi.stubGlobal('fetch', stubFetch([
      ['/coverage', { data: coverage }],
      ['/experiments', { data: [experiment(), experiment({ id: 'x', status: 'running', evasion_delta: undefined })] }],
    ]))

    const { container } = renderWithProviders(<Dashboard />)

    const metrics = container.querySelector('.metric-grid') as HTMLElement
    await waitFor(() =>
      expect(within(metrics).getByText('Total Experiments').parentElement).toHaveTextContent('2'),
    )
    expect(within(metrics).getByText('Completed').parentElement).toHaveTextContent('1')
    // Covered rules "4/10" and the ring percentage.
    expect(within(metrics).getByText('4/10')).toBeInTheDocument()
    expect(within(metrics).getByText('40.0%')).toBeInTheDocument()
  })

  it('lists recent experiments with their mutation and status', async () => {
    signIn('analyst')
    vi.stubGlobal('fetch', stubFetch([
      ['/coverage', { data: coverage }],
      ['/experiments', { data: [experiment()] }],
    ]))

    renderWithProviders(<Dashboard />)

    const table = await screen.findByRole('table', { name: 'Recent experiments' })
    expect(within(table).getByText('structuring-v3')).toBeInTheDocument()
    expect(within(table).getByText('split_transaction')).toBeInTheDocument()
    expect(within(table).getByText('Completed')).toBeInTheDocument()
    // Evasion +3 is rendered by DeltaBadge.
    expect(within(table).getByText(/\+3/)).toBeInTheDocument()
  })

  it('shows the "no experiments" empty state and a call to action', async () => {
    signIn('analyst')
    vi.stubGlobal('fetch', stubFetch([
      ['/coverage', { data: coverage }],
      ['/experiments', { data: [] }],
    ]))

    renderWithProviders(<Dashboard />)

    expect(await screen.findByText('No experiments yet')).toBeInTheDocument()
    // The header and the empty state both offer the action.
    expect(screen.getAllByRole('button', { name: /run experiment/i }).length).toBeGreaterThan(0)
  })

  it('offers the primary actions to both analyst and admin', async () => {
    for (const role of ['analyst', 'admin'] as const) {
      localStorage.clear()
      signIn(role)
      vi.stubGlobal('fetch', stubFetch([
        ['/coverage', { data: coverage }],
        ['/experiments', { data: [] }],
      ]))

      const { unmount } = renderWithProviders(<Dashboard />)
      await waitFor(() => expect(screen.getByText('Dashboard')).toBeInTheDocument())

      expect(screen.getAllByRole('button', { name: /new cohort/i }).length).toBeGreaterThan(0)
      expect(screen.getAllByRole('button', { name: /request scenario/i }).length).toBeGreaterThan(0)
      expect(screen.getAllByRole('button', { name: /run experiment/i }).length).toBeGreaterThan(0)
      unmount()
    }
  })

  it('degrades gracefully when coverage is unavailable', async () => {
    signIn('analyst')
    vi.stubGlobal('fetch', async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.includes('/coverage')) {
        return { ok: false, status: 500, statusText: 'Server Error', json: async () => ({ code: 'db_error', message: 'down' }) } as unknown as Response
      }
      return { ok: true, status: 200, json: async () => ({ data: [] }) } as unknown as Response
    })

    renderWithProviders(<Dashboard />)

    expect(await screen.findByText('Coverage unavailable')).toBeInTheDocument()
    // The rest of the page still renders.
    expect(screen.getByText('Recent Experiments')).toBeInTheDocument()
  })
})
