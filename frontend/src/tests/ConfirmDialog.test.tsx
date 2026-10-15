// ConfirmDialog unit tests
import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { ConfirmDialog } from '../components/ConfirmDialog'

const baseProps = {
  open: true,
  title: 'Are you sure?',
  body: 'This action cannot be undone.',
  onConfirm: vi.fn(),
  onCancel: vi.fn(),
}

describe('ConfirmDialog', () => {
  it('renders when open=true', () => {
    render(<ConfirmDialog {...baseProps} />)
    expect(screen.getByRole('dialog')).toBeInTheDocument()
    expect(screen.getByText('Are you sure?')).toBeInTheDocument()
    expect(screen.getByText('This action cannot be undone.')).toBeInTheDocument()
  })

  it('does not render when open=false', () => {
    render(<ConfirmDialog {...baseProps} open={false} />)
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('calls onConfirm when confirm button clicked', async () => {
    const onConfirm = vi.fn()
    const user = userEvent.setup()
    render(<ConfirmDialog {...baseProps} onConfirm={onConfirm} />)
    await user.click(screen.getByText('Confirm'))
    expect(onConfirm).toHaveBeenCalledOnce()
  })

  it('calls onCancel when cancel button clicked', async () => {
    const onCancel = vi.fn()
    const user = userEvent.setup()
    render(<ConfirmDialog {...baseProps} onCancel={onCancel} />)
    await user.click(screen.getByText('Cancel'))
    expect(onCancel).toHaveBeenCalledOnce()
  })

  it('calls onCancel when Escape is pressed', async () => {
    const onCancel = vi.fn()
    const user = userEvent.setup()
    render(<ConfirmDialog {...baseProps} onCancel={onCancel} />)
    await user.keyboard('{Escape}')
    expect(onCancel).toHaveBeenCalledOnce()
  })

  it('disables both buttons when loading=true', () => {
    render(<ConfirmDialog {...baseProps} loading={true} />)
    const btns = screen.getAllByRole('button')
    btns.forEach(b => expect(b).toBeDisabled())
  })

  it('shows "Working…" text when loading=true', () => {
    render(<ConfirmDialog {...baseProps} loading={true} />)
    expect(screen.getByText('Working…')).toBeInTheDocument()
  })

  it('uses custom confirm/cancel labels', () => {
    render(<ConfirmDialog {...baseProps} confirmLabel="Delete" cancelLabel="Go back" />)
    expect(screen.getByText('Delete')).toBeInTheDocument()
    expect(screen.getByText('Go back')).toBeInTheDocument()
  })

  it('has aria-modal="true"', () => {
    render(<ConfirmDialog {...baseProps} />)
    expect(screen.getByRole('dialog')).toHaveAttribute('aria-modal', 'true')
  })

  it('has aria-labelledby pointing to title', () => {
    render(<ConfirmDialog {...baseProps} />)
    const dialog = screen.getByRole('dialog')
    const labelId = dialog.getAttribute('aria-labelledby')
    expect(document.getElementById(labelId!)).toHaveTextContent('Are you sure?')
  })

  it('applies btn-danger class when danger=true', () => {
    render(<ConfirmDialog {...baseProps} danger={true} confirmLabel="Delete" />)
    const confirmBtn = screen.getByText('Delete')
    expect(confirmBtn).toHaveClass('btn-danger')
  })
})
