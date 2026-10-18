// Role definitions — the single place that explains what each role may do.
//
// The server is the real authorization boundary (internal/api/authz.go); this
// mirrors it for the UI so the difference between roles is visible and the
// interface only offers actions the user can actually perform.

import type { Role } from '../api/types'

export interface RoleInfo {
  id: Role
  label: string
  summary: string
  capabilities: string[]
}

export const ROLES: Record<Role, RoleInfo> = {
  analyst: {
    id: 'analyst',
    label: 'Analyst',
    summary: 'Runs the red-team workflow end to end.',
    capabilities: [
      'View the dashboard, cohorts, scenarios, experiments and coverage',
      'Create and lock transaction cohorts',
      'Request, review and approve AI scenario proposals',
      'Run, replay and benchmark experiments',
      'Download and verify the regression pack',
    ],
  },
  admin: {
    id: 'admin',
    label: 'Administrator',
    summary: 'Everything an analyst can do, plus platform administration.',
    capabilities: [
      'All analyst capabilities',
      'Add, disable and remove user accounts',
      'Change roles and reset passwords',
      'Read the audit log of every consequential action',
    ],
  },
}

/** Roles from least to most privileged. */
export const ROLE_ORDER: readonly Role[] = ['analyst', 'admin']

/** Human-readable label for a role. */
export function roleLabel(role: Role): string {
  return ROLES[role]?.label ?? role
}
