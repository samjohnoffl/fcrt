type BadgeVariant =
  | 'pending' | 'running' | 'completed' | 'failed'
  | 'approved' | 'rejected'
  | 'locked' | 'unlocked'

interface Props {
  status: string
}

function toVariant(s: string): BadgeVariant {
  const map: Record<string, BadgeVariant> = {
    pending: 'pending', running: 'running',
    completed: 'completed', failed: 'failed',
    approved: 'approved', rejected: 'rejected',
    locked: 'locked', unlocked: 'unlocked',
  }
  return map[s.toLowerCase()] ?? 'pending'
}

const LABELS: Record<BadgeVariant, string> = {
  pending: 'Pending', running: 'Running', completed: 'Completed', failed: 'Failed',
  approved: 'Approved', rejected: 'Rejected', locked: 'Locked', unlocked: 'Unlocked',
}

export function StatusBadge({ status }: Props) {
  const variant = toVariant(status)
  return (
    <span className={`badge badge-${variant}`} role="status" aria-label={`Status: ${LABELS[variant]}`}>
      {LABELS[variant]}
    </span>
  )
}

export function DeltaBadge({ delta }: { delta: number }) {
  if (delta === 0) return <span className="delta-zero">0</span>
  if (delta > 0) return <span className="delta-positive">+{delta}</span>
  return <span className="delta-negative">{delta}</span>
}
