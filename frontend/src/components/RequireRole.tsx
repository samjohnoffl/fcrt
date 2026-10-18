// Route-level role guard. Renders children only when the signed-in user's role
// meets the minimum; otherwise sends them back to the dashboard.

import { Navigate } from 'react-router-dom'
import { hasRole } from '../lib/session'
import type { Role } from '../api/types'

export function RequireRole({ min, children }: { min: Role; children: React.ReactNode }) {
  if (!hasRole(min)) return <Navigate to="/" replace />
  return <>{children}</>
}
