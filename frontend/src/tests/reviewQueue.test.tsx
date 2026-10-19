// Review queue tests — the reviewer workflow: pending list, required
// rationale, and submission of a decision. fetch is stubbed; no network.

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { ReviewQueue } from '../pages/ReviewQueue'
import { renderWithProviders, jsonResponse } from './helpers'
import type { ReviewQueueItem } from '../api/types'

const item: ReviewQueueItem = {
  id: '11111111-1111-1111-1111-111111111111',
  cohort_id: '22222222-2222-2222-2222-222222222222',
  name: 'counterparty-expansion-7',
  status: 'completed',
  control_hash: 'a'.repeat(64),
  manifest_hash: 'b'.repeat(64),
  seed: 7,
  declared_change: 'Increase counterparty diversity while keeping controls fixed',
  mutation_type: 'network_expansion',
  created_at: '2026-05-01T10:00:00Z',
  evasion_delta: 2,
  outcome_unchanged: false,
}

beforeEach(() => {
  vi.unstubAllGlobals()
})

function stubReviewFetch() {
  const posted: { body?: Record<string, unknown> } = {}
  const fetchMock = async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
    const url = String(input)
    if (url.includes('/reviews/queue')) {
      const queue = posted.body ? [] : [item]
      return jsonResponse({ data: { queue, count: queue.length } })
    }
    if (url.includes('/review') && init?.method === 'POST') {
      posted.body = JSON.parse(String(init.body))
      return jsonResponse({ data: { id: 'r1', pair_id: item.id, ...posted.body } , status: 201 })
    }
    return jsonResponse({ data: [] })
  }
  vi.stubGlobal('fetch', fetchMock)
  return posted
}

describe('ReviewQueue', () => {
  it('lists completed experiments awaiting a decision', async () => {
    stubReviewFetch()
    renderWithProviders(<ReviewQueue />)

    expect(await screen.findByText('counterparty-expansion-7')).toBeInTheDocument()
    expect(screen.getByText('1 pending')).toBeInTheDocument()
    expect(screen.getByText(/Increase counterparty diversity/)).toBeInTheDocument()
  })

  it('requires a rationale before submitting', async () => {
    const posted = stubReviewFetch()
    renderWithProviders(<ReviewQueue />)
    const user = userEvent.setup()

    await user.click(await screen.findByRole('button', { name: 'Review' }))
    await user.click(screen.getByRole('button', { name: 'Record decision' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(/rationale is required/i)
    expect(posted.body).toBeUndefined()
  })

  it('submits a rejection with a rationale and clears the queue', async () => {
    const posted = stubReviewFetch()
    renderWithProviders(<ReviewQueue />)
    const user = userEvent.setup()

    await user.click(await screen.findByRole('button', { name: 'Review' }))
    await user.selectOptions(screen.getByLabelText('Decision'), 'rejected')
    await user.type(screen.getByLabelText('Rationale'), 'Outcome was unchanged; no new coverage.')
    await user.click(screen.getByRole('button', { name: 'Record decision' }))

    await waitFor(() => expect(posted.body).toBeDefined())
    expect(posted.body).toMatchObject({
      decision: 'rejected',
      rationale: 'Outcome was unchanged; no new coverage.',
    })
    expect(await screen.findByText('Queue is clear')).toBeInTheDocument()
  })
})
