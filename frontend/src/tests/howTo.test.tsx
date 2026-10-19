// How-to guide tests — the onboarding surface must explain the workflow,
// the Gemini boundary, and the five control outcome states.

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { screen } from '@testing-library/react'
import { HowTo } from '../pages/HowTo'
import { renderWithProviders } from './helpers'
import { setSession } from '../lib/session'

beforeEach(() => {
  localStorage.clear()
  vi.unstubAllGlobals()
})

describe('HowTo', () => {
  it('walks through the five workflow steps with working links', () => {
    setSession('tok', { username: 'analyst', role: 'analyst' })
    renderWithProviders(<HowTo />)

    expect(screen.getByRole('heading', { name: 'How to use' })).toBeInTheDocument()
    expect(screen.getByText('From cohort to evidence in five steps')).toBeInTheDocument()
    expect(screen.getByText('Create and lock a cohort')).toBeInTheDocument()
    expect(screen.getByText('Run a counterfactual experiment')).toBeInTheDocument()
    expect(screen.getByText('Close the coverage gap')).toBeInTheDocument()

    const links = screen.getAllByRole('link').map(a => a.getAttribute('href'))
    expect(links).toEqual(expect.arrayContaining([
      '/cohorts/new', '/experiments/new', '/reviews', '/coverage',
    ]))
  })

  it('states the Gemini boundary and every outcome status', () => {
    setSession('tok', { username: 'analyst', role: 'analyst' })
    renderWithProviders(<HowTo />)

    expect(screen.getByText(/cohort summary statistics only/i)).toBeInTheDocument()
    expect(screen.getByText(/must be approved by a person/i)).toBeInTheDocument()
    expect(screen.getByText(/never in the detection loop/i)).toBeInTheDocument()

    for (const label of ['Triggered', 'Not triggered', 'Not applicable', 'Inconclusive', 'Failed']) {
      expect(screen.getAllByText(label).length).toBeGreaterThan(0)
    }
  })
})
