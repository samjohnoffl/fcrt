// Scenario creation tests — the Gemini-assisted proposal flow: an analyst
// opens the request dialog, picks a locked cohort, posts the request, and is
// navigated to the stored proposal. fetch is stubbed; no network.

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Route, Routes } from 'react-router-dom'
import { ScenarioList } from '../pages/ScenarioList'
import { renderWithProviders, jsonResponse } from './helpers'
import { setSession } from '../lib/session'
import type { Cohort, ScenarioProposal } from '../api/types'

const cohort: Cohort = {
  id: '22222222-2222-2222-2222-222222222222',
  name: 'Locked retail cohort',
  predicate: { entity_type: 'individual' },
  seed: 7,
  generator_version: 'v1',
  manifest: {
    cohort_id: '22222222-2222-2222-2222-222222222222',
    generator_version: 'v1',
    seed: 7,
    window_start: '2026-02-01T00:00:00Z',
    window_end: '2026-05-01T00:00:00Z',
    entity_count: 12,
    account_count: 30,
    tx_count: 250,
  },
  locked_at: '2026-05-01T09:00:00Z',
  created_at: '2026-05-01T09:00:00Z',
  updated_at: '2026-05-01T09:00:00Z',
}

const created: ScenarioProposal = {
  id: '33333333-3333-3333-3333-333333333333',
  cohort_id: cohort.id,
  status: 'pending',
  scenario_type: 'split_transaction',
  description: 'Split one transfer into three below the threshold',
  expected_evasion_vector: 'Bypasses the structuring control.',
  suggested_tx_pattern: [],
  gemini_prompt: '{}',
  gemini_response: '[]',
  gemini_model: 'gemini-test',
  correlation_id: '44444444-4444-4444-4444-444444444444',
  created_at: '2026-05-01T10:00:00Z',
  updated_at: '2026-05-01T10:00:00Z',
}

function scenarioRoutes() {
  return (
    <Routes>
      <Route path="/scenarios" element={<ScenarioList />} />
      <Route path="/scenarios/:id" element={<div>Proposal detail</div>} />
    </Routes>
  )
}

function stubScenarioFetch(postResponse: () => Response) {
  const requests: Array<{ url: string; method: string; body?: unknown }> = []
  vi.stubGlobal('fetch', async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
    const url = String(input)
    const method = init?.method ?? 'GET'
    requests.push({ url, method, body: init?.body ? JSON.parse(String(init.body)) : undefined })
    if (url.includes('/cohorts')) return jsonResponse({ data: [cohort] })
    if (url.includes('/scenarios') && method === 'POST') return postResponse()
    if (url.includes('/scenarios')) return jsonResponse({ data: [] })
    return jsonResponse({ data: [] })
  })
  return requests
}

beforeEach(() => {
  localStorage.clear()
  vi.unstubAllGlobals()
})

describe('ScenarioList', () => {
  it('creates a scenario for a locked cohort and opens its detail page', async () => {
    setSession('tok', { username: 'analyst', role: 'analyst' })
    const requests = stubScenarioFetch(() => jsonResponse({ data: created }, 201))

    renderWithProviders(scenarioRoutes(), '/scenarios')
    const user = userEvent.setup()

    await user.click((await screen.findAllByRole('button', { name: 'Request scenario' }))[0])
    await user.selectOptions(screen.getByLabelText('Cohort *'), cohort.id)
    await user.click(screen.getByRole('button', { name: 'Request' }))

    expect(await screen.findByText('Proposal detail')).toBeInTheDocument()
    const post = requests.find(r => r.method === 'POST')
    expect(post?.url).toContain('/scenarios')
    expect(post?.body).toEqual({ cohort_id: cohort.id })
  })

  it('shows the API error and stays on the list when generation is unavailable', async () => {
    setSession('tok', { username: 'analyst', role: 'analyst' })
    stubScenarioFetch(() => jsonResponse({
      code: 'gemini_not_configured',
      message: 'AI scenario generation is not configured on this instance',
    }, 503))

    renderWithProviders(scenarioRoutes(), '/scenarios')
    const user = userEvent.setup()

    await user.click((await screen.findAllByRole('button', { name: 'Request scenario' }))[0])
    await user.selectOptions(screen.getByLabelText('Cohort *'), cohort.id)
    await user.click(screen.getByRole('button', { name: 'Request' }))

    await waitFor(() =>
      expect(screen.getByText(/not configured on this instance/)).toBeInTheDocument(),
    )
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Request' })).toBeEnabled(),
    )
    expect(screen.queryByText('Proposal detail')).not.toBeInTheDocument()
  })
})
