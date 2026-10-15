// API types matching the Go domain model exactly.
// Never invent fields here; update when the backend contract changes.

export type Status = 'pending' | 'running' | 'completed' | 'failed'
export type ScenarioStatus = 'pending' | 'approved' | 'rejected'

export interface ApiError {
  error: string
  code: string
}

/* ── Cohort ── */
export interface Predicate {
  entity_type?: string
  country_code?: string
  min_entities?: number
  max_entities?: number
}

export interface CohortManifest {
  cohort_id: string
  generator_version: string
  seed: number
  window_start: string
  window_end: string
  entity_count: number
  account_count: number
  tx_count: number
}

export interface Cohort {
  id: string
  name: string
  predicate: Predicate
  seed: number
  generator_version: string
  manifest: CohortManifest
  window_start?: string
  window_end?: string
  locked_at?: string
  created_at: string
  updated_at: string
}

export interface CreateCohortRequest {
  name: string
  seed: number
  predicate: Predicate
  window_start: string
  window_end: string
}

/* ── Scenario ── */
export interface ScenarioProposal {
  id: string
  cohort_id: string
  status: ScenarioStatus
  scenario_type: string
  description: string
  expected_evasion_vector: string
  suggested_tx_pattern: Record<string, unknown>[]
  gemini_prompt: string
  gemini_response: string
  gemini_model: string
  correlation_id: string
  approved_by?: string
  created_at: string
  updated_at: string
}

/* ── Experiment ── */
export interface RunResult {
  alert_count: number
  control_hash: string
  manifest_hash: string
  run_type: 'baseline' | 'variant'
  run_at: string
}

export interface Outcome {
  pair_id: string
  evasion_delta: number
  new_alert_rule_ids: string[]
  dropped_alert_rule_ids: string[]
  outcome_unchanged: boolean
}

export interface ExperimentPair {
  id: string
  cohort_id: string
  name: string
  status: Status
  control_hash: string
  manifest_hash: string
  seed: number
  declared_change: string
  mutation_type: string
  created_at: string
  completed_at?: string
}

export interface ExperimentDetail extends ExperimentPair {
  baseline: RunResult
  variant: RunResult
  outcome: Outcome
  mutation_spec: Record<string, unknown>
}

export interface CreateExperimentRequest {
  cohort_id: string
  name: string
  declared_change: string
  mutation_type: string
  mutation_spec: Record<string, unknown>
}

/* ── Evidence ── */
export interface GeminiTrace {
  correlation_id: string
  model: string
  prompt: string
  response: string
  called_at: string
}

export interface EvidenceBundle {
  run_id: string
  aml_rule_version: string
  cohort_snapshot: {
    cohort_id: string
    seed: number
    predicate: Predicate
    entity_count: number
    tx_count: number
    locked_at: string
  }
  scenario_params?: ScenarioProposal
  baseline_alerts: { id: string; rule_id: string }[]
  variant_alerts:  { id: string; rule_id: string }[]
  aml_diff: {
    baseline_count: number
    variant_count: number
    evasion_delta: number
    new_rule_ids: string[]
    dropped_rule_ids: string[]
  }
  coverage_delta: CoverageRule[]
  gemini_trace?: GeminiTrace
  created_at: string
  checksum: string
}

/* ── Coverage ── */
export interface CoverageRule {
  id: string
  rule_id: string
  rule_version: string
  times_triggered: number
  last_triggered_at?: string
  coverage_weight: number
  updated_at: string
}

export interface CoverageSummary {
  total_rules: number
  covered_rules: number
  coverage_pct: number
  rules: CoverageRule[]
}
