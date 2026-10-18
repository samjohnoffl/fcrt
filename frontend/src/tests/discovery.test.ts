// Unit tests for the discovery analytics helpers (pure functions, no network).
import { describe, it, expect } from 'vitest'
import {
  computeDiscoveryMetrics,
  rankDiscoveries,
  evadedRuleIDs,
  caughtRuleIDs,
  uncoveredRules,
} from '../lib/discovery'
import type { CoverageSummary, ExperimentPair } from '../api/types'

function exp(over: Partial<ExperimentPair>): ExperimentPair {
  return {
    id: over.id ?? Math.random().toString(36).slice(2),
    cohort_id: 'c1',
    name: over.name ?? 'e',
    status: over.status ?? 'completed',
    control_hash: 'h',
    manifest_hash: 'm',
    seed: 1,
    declared_change: 'd',
    mutation_type: over.mutation_type ?? 'split_transaction',
    created_at: over.created_at ?? '2024-01-01T00:00:00Z',
    ...over,
  }
}

const cov: CoverageSummary = {
  total_rules: 3,
  covered_rules: 1,
  coverage_pct: 33.3,
  rules: [
    { id: 'r1', rule_id: 'structuring_v1', rule_version: 'v1', times_triggered: 5, coverage_weight: 0.16, updated_at: '' },
    { id: 'r2', rule_id: 'velocity_v1',    rule_version: 'v1', times_triggered: 0, coverage_weight: 1.0,  updated_at: '' },
    { id: 'r3', rule_id: 'shell_v1',       rule_version: 'v1', times_triggered: 0, coverage_weight: 1.0,  updated_at: '' },
  ],
}

describe('discovery analytics', () => {
  it('classifies discoveries, regressions, and neutral runs', () => {
    const m = computeDiscoveryMetrics([
      exp({ evasion_delta: 3,  status: 'completed' }),
      exp({ evasion_delta: 0,  status: 'completed' }),
      exp({ evasion_delta: -2, status: 'completed' }),
      exp({ evasion_delta: 5,  status: 'running' }), // not completed → ignored
    ], cov)
    expect(m.total).toBe(4)
    expect(m.completed).toBe(3)
    expect(m.discoveries).toBe(1)
    expect(m.regressions).toBe(1)
    expect(m.neutral).toBe(1)
    expect(m.uncoveredRules).toBe(2)
  })

  it('ranks discoveries by evasion delta descending and excludes non-evasion', () => {
    const ranked = rankDiscoveries([
      exp({ name: 'small',  evasion_delta: 1 }),
      exp({ name: 'big',    evasion_delta: 9 }),
      exp({ name: 'none',   evasion_delta: 0 }),
      exp({ name: 'neg',    evasion_delta: -4 }),
    ])
    expect(ranked.map(e => e.name)).toEqual(['big', 'small'])
  })

  it('unions dropped rule ids across discoveries only', () => {
    const ids = evadedRuleIDs([
      exp({ evasion_delta: 2, dropped_alert_rule_ids: ['a', 'b'] }),
      exp({ evasion_delta: 1, dropped_alert_rule_ids: ['b', 'c'] }),
      exp({ evasion_delta: -1, dropped_alert_rule_ids: ['z'] }), // regression → excluded
    ])
    expect(ids).toEqual(['a', 'b', 'c'])
  })

  it('unions new rule ids across regressions only', () => {
    const ids = caughtRuleIDs([
      exp({ evasion_delta: -2, new_alert_rule_ids: ['structuring_v1'] }),
      exp({ evasion_delta: 3,  new_alert_rule_ids: ['ignored'] }),
    ])
    expect(ids).toEqual(['structuring_v1'])
  })

  it('returns never-triggered rules sorted by weight desc', () => {
    const gaps = uncoveredRules(cov)
    expect(gaps.map(r => r.rule_id)).toEqual(['velocity_v1', 'shell_v1'])
  })

  it('handles missing coverage gracefully', () => {
    expect(uncoveredRules(undefined)).toEqual([])
    expect(computeDiscoveryMetrics([exp({ evasion_delta: 1 })], undefined).uncoveredRules).toBe(0)
  })
})
