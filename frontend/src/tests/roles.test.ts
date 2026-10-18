// Role definitions — the UI mirror of the server's authorization policy.

import { describe, it, expect } from 'vitest'
import { ROLES, ROLE_ORDER, roleLabel } from '../lib/roles'

describe('roles', () => {
  it('defines exactly the two supported roles', () => {
    expect(ROLE_ORDER).toEqual(['analyst', 'admin'])
    expect(Object.keys(ROLES).sort()).toEqual(['admin', 'analyst'])
  })

  it('orders roles from least to most privileged', () => {
    expect(ROLE_ORDER[0]).toBe('analyst')
    expect(ROLE_ORDER[ROLE_ORDER.length - 1]).toBe('admin')
  })

  it('describes what each role can do', () => {
    expect(ROLES.analyst.capabilities.length).toBeGreaterThan(0)
    expect(ROLES.admin.capabilities.length).toBeGreaterThan(0)
    // The admin description must state that it includes everything an analyst can do.
    expect(ROLES.admin.capabilities[0].toLowerCase()).toContain('all analyst')
    // Administration-only capabilities must not appear under the analyst role.
    const analystText = ROLES.analyst.capabilities.join(' ').toLowerCase()
    expect(analystText).not.toContain('audit log')
    expect(analystText).not.toContain('user accounts')
    // ...but the admin role does have them.
    const adminText = ROLES.admin.capabilities.join(' ').toLowerCase()
    expect(adminText).toContain('user accounts')
    expect(adminText).toContain('audit log')
  })

  it('gives each role a human label', () => {
    expect(roleLabel('admin')).toBe('Administrator')
    expect(roleLabel('analyst')).toBe('Analyst')
  })
})
