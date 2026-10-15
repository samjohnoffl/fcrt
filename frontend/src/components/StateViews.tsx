// Shared state views: Loading spinner, empty state, error state.
// All three accept optional action slot.

interface StateProps {
  title?: string
  body?: string
  action?: React.ReactNode
}

export function LoadingState({ title = 'Loading…' }: { title?: string }) {
  return (
    <div className="state-container" role="status" aria-live="polite">
      <div className="state-spinner" aria-hidden="true" />
      <span className="state-body">{title}</span>
    </div>
  )
}

export function EmptyState({ title = 'No data', body, action }: StateProps) {
  return (
    <div className="state-container">
      <div className="state-icon" aria-hidden="true">◎</div>
      <div className="state-title">{title}</div>
      {body && <p className="state-body">{body}</p>}
      {action}
    </div>
  )
}

export function ErrorState({ title = 'Something went wrong', body, action }: StateProps) {
  return (
    <div className="state-container">
      <div className="state-icon" aria-hidden="true">⚠</div>
      <div className="state-title" style={{ color: 'var(--error)' }}>{title}</div>
      {body && <p className="state-body">{body}</p>}
      {action}
    </div>
  )
}

export function CancelledState({ action }: { action?: React.ReactNode }) {
  return (
    <div className="state-container">
      <div className="state-icon" aria-hidden="true">✕</div>
      <div className="state-title">Cancelled</div>
      <p className="state-body">This operation was cancelled.</p>
      {action}
    </div>
  )
}
