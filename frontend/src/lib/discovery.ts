// Discovery analytics — pure helpers shared by the Discovery Overview screen.
//
// A "discovery" is an experiment whose mutation reduced AML alerts
// (evasion_delta > 0; delta = baseline_alerts − variant_alerts). The rules that
// stopped firing are the evasion vectors found. A negative delta is a
// "regression" — the mutation triggered *more* alerts, i.e. detection improved.

import type { CoverageRule, CoverageSummary, ExperimentPair } from '../api/types'

export interface DiscoveryMetrics {
  total: number
  completed: number
  /** Completed experiments with evasion_delta > 0. */
  discoveries: number
  /** Completed experiments with evasion_delta < 0. */
  regressions: number
  /** Completed experiments with evasion_delta == 0. */
  neutral: number
  /** Distinct rule ids that stopped firing across all discoveries. */
  distinctRulesEvaded: number
  /** Rules that have never fired (coverage gaps). */
  uncoveredRules: number
}

const isCompleted = (e: ExperimentPair) => e.status === 'completed'
const delta = (e: ExperimentPair) => e.evasion_delta ?? 0

/** Completed experiments that produced evasion, ranked by delta (descending). */
export function rankDiscoveries(experiments: ExperimentPair[]): ExperimentPair[] {
  return experiments
    .filter(e => isCompleted(e) && delta(e) > 0)
    .sort((a, b) => delta(b) - delta(a))
}

/** Distinct rule ids that stopped firing across all discoveries. */
export function evadedRuleIDs(experiments: ExperimentPair[]): string[] {
  const set = new Set<string>()
  for (const e of experiments) {
    if (isCompleted(e) && delta(e) > 0) {
      for (const r of e.dropped_alert_rule_ids ?? []) set.add(r)
    }
  }
  return [...set].sort()
}

/** Distinct rule ids newly triggered by regressions (detection improvements). */
export function caughtRuleIDs(experiments: ExperimentPair[]): string[] {
  const set = new Set<string>()
  for (const e of experiments) {
    if (isCompleted(e) && delta(e) < 0) {
      for (const r of e.new_alert_rule_ids ?? []) set.add(r)
    }
  }
  return [...set].sort()
}

/** Rules that have never fired — the detection blind spots. */
export function uncoveredRules(coverage?: CoverageSummary): CoverageRule[] {
  if (!coverage) return []
  return coverage.rules
    .filter(r => r.times_triggered === 0)
    .sort((a, b) => b.coverage_weight - a.coverage_weight)
}

export function computeDiscoveryMetrics(
  experiments: ExperimentPair[],
  coverage?: CoverageSummary,
): DiscoveryMetrics {
  const completed = experiments.filter(isCompleted)
  let discoveries = 0
  let regressions = 0
  let neutral = 0
  for (const e of completed) {
    const d = delta(e)
    if (d > 0) discoveries++
    else if (d < 0) regressions++
    else neutral++
  }
  return {
    total: experiments.length,
    completed: completed.length,
    discoveries,
    regressions,
    neutral,
    distinctRulesEvaded: evadedRuleIDs(experiments).length,
    uncoveredRules: uncoveredRules(coverage).length,
  }
}
