// Campaign UI tests — list, creation (with budget) and the detail surface that
// shows budget consumption, linked experiments and the honest coverage scope.
// fetch is stubbed; no network.

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Route, Routes } from 'react-router-dom'
import { CampaignList } from '../pages/CampaignList'
import { CampaignDetail } from '../pages/CampaignDetail'
import { renderWithProviders, jsonResponse } from './helpers'
import { setSession } from '../lib/session'
import type { Campaign, Cohort, ExperimentPair } from '../api/types'

const campaign: Campaign = {
  id: 'campaign-1',
  name: 'Structuring pressure test',
  description: 'Probe the structuring and layering controls',
  goal: 'Find a gap before the audit',
  cohort_id: 'cohort-1',
  budget: 5,
  experiments_used: 2,
  status: 'active',
  created_at: '2026-05-01T10:00:00Z',
  updated_at: '2026-05-01T10:00:00Z',
}

const cohort: Cohort = {
  id: 'cohort-1',
  name: 'Retail cohort',
  predicate: {},
  seed: 7,
  generator_version: 'v1',
  manifest: {
    cohort_id: 'cohort-1', generator_version: 'v1', seed: 7,
    window_start: '2026-02-01T00:00:00Z', window_end: '2026-05-01T00:00:00Z',
    entity_count: 12, account_count: 30, tx_count: 250,
  },
  locked_at: '2026-05-01T09:00:00Z',
  created_at: '2026-05-01T09:00:00Z',
  updated_at: '2026-05-01T09:00:00Z',
}

const linked: ExperimentPair = {
  id: 'exp-1',
  cohort_id: cohort.id,
  name: 'split-3',
  status: 'completed',
  control_hash: 'a'.repeat(64),
  manifest_hash: 'b'.repeat(64),
  seed: 7,
  declared_change: 'Split one payment into three',
  mutation_type: 'split_transaction',
  campaign_id: campaign.id,
  created_at: '2026-05-02T10:00:00Z',
}

function stubCampaignFetch() {
  const calls: Array<{ url: string; method: string; body?: unknown }> = []
  vi.stubGlobal('fetch', async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
    const url = String(input)
    const method = init?.method ?? 'GET'
    const body = init?.body ? JSON.parse(String(init.body)) : undefined
    calls.push({ url, method, body })

    if (url.includes('/coverage')) return jsonResponse({
      data: {
        campaign_id: campaign.id, cohort_id: cohort.id,
        experiments: 2, budget: 5, experiments_used: 2,
        coverage_scope: 'global', coverage_endpoint: '/api/v1/coverage',
        note: 'The three coverage maps are global; campaign-level slicing is not recorded at outcome-write time.',
      },
    })
    if (url.includes('/benchmark')) return jsonResponse({
      data: {
        cohort_id: cohort.id, strategies: [], insufficient_data: true,
        missing_strategies: ['random', 'gemini_only', 'adaptive'],
        notes: 'Not enough stored runs to compare strategies.',
        computed_at: '2026-05-02T10:00:00Z',
      },
    })
    if (url.includes('/campaigns/campaign-1') && method === 'PATCH') {
      return jsonResponse({ data: { ...campaign, status: body.status } })
    }
    if (url.includes('/campaigns/campaign-1')) return jsonResponse({ data: campaign })
    if (url.includes('/campaigns') && method === 'POST') {
      return jsonResponse({ data: { ...campaign, ...body, id: 'campaign-new' } }, 201)
    }
    if (url.includes('/campaigns')) return jsonResponse({ data: [campaign] })
    if (url.includes('/cohorts')) return jsonResponse({ data: [cohort] })
    if (url.includes('/experiments')) return jsonResponse({ data: [linked] })
    return jsonResponse({ data: [] })
  })
  return calls
}

beforeEach(() => {
  localStorage.clear()
  setSession('tok', { username: 'analyst', role: 'analyst' })
  vi.unstubAllGlobals()
})

describe('Campaigns', () => {
  it('lists campaigns with their budget consumption', async () => {
    stubCampaignFetch()
    renderWithProviders(
      <Routes>
        <Route path="/campaigns" element={<CampaignList />} />
      </Routes>,
      '/campaigns',
    )

    const table = await screen.findByRole('table', { name: 'Campaigns' })
    expect(await screen.findByText('Structuring pressure test')).toBeInTheDocument()
    expect(table).toHaveTextContent('2 / 5')
    expect(table).toHaveTextContent('Active')
  })

  it('creates a budgeted campaign bound to a cohort', async () => {
    const calls = stubCampaignFetch()
    renderWithProviders(
      <Routes>
        <Route path="/campaigns" element={<CampaignList />} />
        <Route path="/campaigns/:id" element={<div>Campaign detail</div>} />
      </Routes>,
      '/campaigns',
    )
    const user = userEvent.setup()

    await user.click(await screen.findByRole('button', { name: 'New campaign' }))
    await user.type(screen.getByLabelText('Name *'), 'New budgeted campaign')
    const budget = screen.getByLabelText('Experiment budget *')
    await user.clear(budget)
    await user.type(budget, '7')
    await user.selectOptions(screen.getByLabelText('Cohort'), cohort.id)
    await user.click(screen.getByRole('button', { name: 'Create campaign' }))

    expect(await screen.findByText('Campaign detail')).toBeInTheDocument()
    const post = calls.find(c => c.method === 'POST' && c.url.endsWith('/campaigns'))
    expect(post?.body).toMatchObject({
      name: 'New budgeted campaign',
      budget: 7,
      cohort_id: cohort.id,
    })
  })

  it('shows budget, linked experiments and the global coverage scope', async () => {
    stubCampaignFetch()
    renderWithProviders(
      <Routes>
        <Route path="/campaigns/:id" element={<CampaignDetail />} />
      </Routes>,
      '/campaigns/campaign-1',
    )

    expect(await screen.findByText('2 / 5')).toBeInTheDocument()
    expect(await screen.findByText('split-3')).toBeInTheDocument()
    expect(await screen.findByText(/coverage maps are global/i)).toBeInTheDocument()
    expect(await screen.findByText(/declare a winner/)).toBeInTheDocument()
  })

  it('pauses an active campaign', async () => {
    const calls = stubCampaignFetch()
    renderWithProviders(
      <Routes>
        <Route path="/campaigns/:id" element={<CampaignDetail />} />
      </Routes>,
      '/campaigns/campaign-1',
    )
    const user = userEvent.setup()

    await user.click(await screen.findByRole('button', { name: 'Pause campaign' }))
    await waitFor(() => {
      const patch = calls.find(c => c.method === 'PATCH')
      expect(patch?.body).toEqual({ status: 'paused' })
    })
  })
})
