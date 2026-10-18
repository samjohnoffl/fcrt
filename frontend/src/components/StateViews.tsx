// Shared state views: loading spinner, empty state, error state, cancelled.
// All accept an optional action slot. Icons are inline SVG (no emoji) so they
// match the rest of the interface and scale crisply.

interface StateProps {
  title?: string
  body?: string
  action?: React.ReactNode
}

const STROKE = {
  fill: 'none',
  stroke: 'currentColor',
  strokeWidth: 1.6,
  strokeLinecap: 'round' as const,
  strokeLinejoin: 'round' as const,
}

function StateIcon({ children, tone }: { children: React.ReactNode; tone?: string }) {
  return (
    <span className="state-icon" style={tone ? { color: tone } : undefined} aria-hidden="true">
      <svg viewBox="0 0 24 24" width="32" height="32" {...STROKE}>{children}</svg>
    </span>
  )
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
      <StateIcon>
        <rect x="3" y="4.5" width="18" height="15" rx="2.5" />
        <path d="M3 9.5h18M8.5 4.5v15" />
      </StateIcon>
      <div className="state-title">{title}</div>
      {body && <p className="state-body">{body}</p>}
      {action}
    </div>
  )
}

export function ErrorState({ title = 'Something went wrong', body, action }: StateProps) {
  return (
    <div className="state-container">
      <StateIcon tone="var(--error)">
        <circle cx="12" cy="12" r="9" />
        <path d="M12 7.5v5.5M12 16.2v.1" />
      </StateIcon>
      <div className="state-title">{title}</div>
      {body && <p className="state-body">{body}</p>}
      {action}
    </div>
  )
}

export function CancelledState({ action }: { action?: React.ReactNode }) {
  return (
    <div className="state-container">
      <StateIcon>
        <circle cx="12" cy="12" r="9" />
        <path d="m9 9 6 6M15 9l-6 6" />
      </StateIcon>
      <div className="state-title">Cancelled</div>
      <p className="state-body">This operation was cancelled.</p>
      {action}
    </div>
  )
}
