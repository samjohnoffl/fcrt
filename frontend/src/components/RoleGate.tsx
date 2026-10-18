// Renders children only when the signed-in user's role meets the minimum.
// Server-side authorization is the real boundary; this only keeps the UI from
// offering actions that would be rejected with 403.

import { hasRole } from '../lib/session'
import type { Role } from '../api/types'

export function RoleGate({ min, children }: { min: Role; children: React.ReactNode }) {
  if (!hasRole(min)) return null
  return <>{children}</>
}
