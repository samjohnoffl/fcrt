// StatusBadge and DeltaBadge unit tests
import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { StatusBadge, DeltaBadge } from '../components/StatusBadge'

describe('StatusBadge', () => {
  const cases = [
    ['pending',   'Pending'],
    ['running',   'Running'],
    ['completed', 'Completed'],
    ['failed',    'Failed'],
    ['approved',  'Approved'],
    ['rejected',  'Rejected'],
    ['locked',    'Locked'],
    ['unlocked',  'Unlocked'],
  ] as const

  for (const [status, label] of cases) {
    it(`renders "${label}" for status "${status}"`, () => {
      render(<StatusBadge status={status} />)
      expect(screen.getByRole('status')).toHaveTextContent(label)
    })

    it(`applies badge-${status} class for "${status}"`, () => {
      render(<StatusBadge status={status} />)
      expect(screen.getByRole('status')).toHaveClass(`badge-${status}`)
    })
  }

  it('falls back to "Pending" for unknown status', () => {
    render(<StatusBadge status="unknown_garbage" />)
    expect(screen.getByRole('status')).toHaveTextContent('Pending')
  })

  it('has accessible aria-label', () => {
    render(<StatusBadge status="completed" />)
    expect(screen.getByLabelText('Status: Completed')).toBeInTheDocument()
  })
})

describe('DeltaBadge', () => {
  it('renders positive delta with + prefix and warning', () => {
    render(<DeltaBadge delta={3} />)
    expect(screen.getByText(/\+3/)).toBeInTheDocument()
  })

  it('renders negative delta as-is', () => {
    render(<DeltaBadge delta={-2} />)
    expect(screen.getByText('-2')).toBeInTheDocument()
  })

  it('renders zero delta', () => {
    render(<DeltaBadge delta={0} />)
    expect(screen.getByText('0')).toBeInTheDocument()
  })

  it('applies delta-positive class for positive values', () => {
    const { container } = render(<DeltaBadge delta={5} />)
    expect(container.firstChild).toHaveClass('delta-positive')
  })

  it('applies delta-negative class for negative values', () => {
    const { container } = render(<DeltaBadge delta={-1} />)
    expect(container.firstChild).toHaveClass('delta-negative')
  })

  it('applies delta-zero class for zero', () => {
    const { container } = render(<DeltaBadge delta={0} />)
    expect(container.firstChild).toHaveClass('delta-zero')
  })
})
