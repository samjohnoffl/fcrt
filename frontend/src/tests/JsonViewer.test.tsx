// JsonViewer unit tests
import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { JsonViewer, CollapsibleJson } from '../components/JsonViewer'

describe('JsonViewer', () => {
  it('renders a region with aria-label', () => {
    render(<JsonViewer value={{ foo: 'bar' }} />)
    expect(screen.getByRole('region', { name: 'JSON content' })).toBeInTheDocument()
  })

  it('highlights string values with json-string span', () => {
    const { container } = render(<JsonViewer value={{ name: 'alice' }} />)
    const strings = container.querySelectorAll('.json-string')
    expect(strings.length).toBeGreaterThan(0)
  })

  it('highlights numeric values', () => {
    const { container } = render(<JsonViewer value={{ count: 42 }} />)
    const numbers = container.querySelectorAll('.json-number')
    expect(numbers.length).toBeGreaterThan(0)
  })

  it('highlights boolean values', () => {
    const { container } = render(<JsonViewer value={{ flag: true }} />)
    const bools = container.querySelectorAll('.json-bool')
    expect(bools.length).toBeGreaterThan(0)
  })

  it('highlights null values', () => {
    const { container } = render(<JsonViewer value={{ x: null }} />)
    const nulls = container.querySelectorAll('.json-null')
    expect(nulls.length).toBeGreaterThan(0)
  })

  it('highlights object keys', () => {
    const { container } = render(<JsonViewer value={{ myKey: 1 }} />)
    const keys = container.querySelectorAll('.json-key')
    expect(keys.length).toBeGreaterThan(0)
  })

  it('renders an empty object without error', () => {
    expect(() => render(<JsonViewer value={{}} />)).not.toThrow()
  })

  it('renders an array without error', () => {
    expect(() => render(<JsonViewer value={[1, 2, 3]} />)).not.toThrow()
  })
})

describe('CollapsibleJson', () => {
  it('starts collapsed — JSON body not rendered', () => {
    render(<CollapsibleJson label="Show me" value={{ x: 1 }} />)
    const btn = screen.getByRole('button', { name: /Show me/ })
    // When collapsed the JSON viewer is conditionally unmounted, not just hidden.
    expect(btn).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByRole('region', { name: 'JSON content' })).not.toBeInTheDocument()
  })

  it('expands on click', async () => {
    const user = userEvent.setup()
    render(<CollapsibleJson label="Show me" value={{ x: 1 }} />)
    const btn = screen.getByRole('button', { name: /Show me/ })
    expect(btn).toHaveAttribute('aria-expanded', 'false')
    await user.click(btn)
    expect(btn).toHaveAttribute('aria-expanded', 'true')
    expect(screen.getByRole('region', { name: 'JSON content' })).toBeVisible()
  })

  it('collapses again on second click', async () => {
    const user = userEvent.setup()
    render(<CollapsibleJson label="Toggle" value={{ y: 2 }} />)
    const btn = screen.getByRole('button')
    await user.click(btn)
    await user.click(btn)
    expect(btn).toHaveAttribute('aria-expanded', 'false')
  })
})
