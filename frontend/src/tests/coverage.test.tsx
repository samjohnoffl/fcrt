// Coverage dashboard tests — the three-map view: control outcomes, outcome
// statuses and scenario dimension values. fetch is stubbed; no network.

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { screen, waitFor, within } from '@testing-library/react'
import { CoverageDashboard } from '../pages/CoverageDashboard'
import { renderWithProviders, stubFetch } from './helpers'
import type { CoverageSummary } from '../api/types'

const summary: CoverageSummary = {
  total_rules: 2,
  covered_rules: 1,
  coverage_pct: 50,
  rules: [
    {
      id: 'r1', rule_id: 'structuring_v1', rule_version: 'v0.2.0',
      times_triggered: 4, times_not_triggered: 1, times_not_applicable: 0,
      times_inconclusive: 0, times_failed: 0, last_status: 'triggered',
      coverage_weight: 0.5, updated_at: '',
    },
    {
      id: 'r2', rule_id: 'velocity_v1', rule_version: 'v0.2.0',
      times_triggered: 0, times_not_triggered: 2, times_not_applicable: 1,
      times_inconclusive: 3, times_failed: 1, last_status: 'failed',
      coverage_weight: 1.0, updated_at: '',
    },
  ],
  scenario: [
    { dimension: 'geography', value: 'domestic', times_seen: 2, last_seen_at: '' },
    { dimension: 'geography', value: 'cross_border', times_seen: 1, last_seen_at: '' },
  ],
  outcomes: [
    { rule_id: 'structuring_v1', status: 'triggered', times_seen: 4, last_seen_at: '' },
  ],
}

beforeEach(() => {
  vi.unstubAllGlobals()
})

describe('CoverageDashboard', () => {
  it('renders control statuses and the scenario map separately', async () => {
    vi.stubGlobal('fetch', stubFetch([['/coverage', { data: summary }]]))
    renderWithProviders(<CoverageDashboard />)

    await waitFor(() => expect(screen.getByText('Control & Outcome Map')).toBeInTheDocument())

    const table = screen.getByRole('table', { name: /coverage rule table/i })
    expect(within(table).getByText('structuring_v1')).toBeInTheDocument()
    expect(within(table).getByText('velocity_v1')).toBeInTheDocument()

    // The velocity row exposes all five statuses; a failed evaluation must be
    // visible, not folded into "not triggered".
    const velocityRow = within(table).getByText('velocity_v1').closest('tr') as HTMLElement
    expect(within(velocityRow).getByText('3')).toBeInTheDocument() // inconclusive
    expect(within(velocityRow).getAllByText('1').length).toBeGreaterThanOrEqual(2) // not applicable + failed

    // Scenario coverage is its own map, grouped by genome dimension.
    expect(screen.getByText('Scenario Coverage Map')).toBeInTheDocument()
    expect(screen.getByText(/domestic/)).toBeInTheDocument()
    expect(screen.getByText(/cross_border/)).toBeInTheDocument()
    expect(screen.getByText('1 dimensions')).toBeInTheDocument()
  })

  it('explains when no scenario has been linked yet', async () => {
    vi.stubGlobal('fetch', stubFetch([['/coverage', { data: { ...summary, scenario: [] } }]]))
    renderWithProviders(<CoverageDashboard />)

    await waitFor(() => expect(screen.getByText('Scenario Coverage Map')).toBeInTheDocument())
    expect(screen.getByText(/No scenario coverage yet/i)).toBeInTheDocument()
  })
})
