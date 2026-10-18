// Route guard: renders children only when a session exists, otherwise redirects
// to /login, preserving the attempted path so we can return after sign-in.

import { Navigate, useLocation } from 'react-router-dom'
import { isAuthenticated } from '../lib/session'

export function RequireAuth({ children }: { children: React.ReactNode }) {
  const location = useLocation()

  if (!isAuthenticated()) {
    return <Navigate to="/login" replace state={{ from: location.pathname + location.search }} />
  }
  return <>{children}</>
}
