// API types matching the Go domain model exactly.
// Never invent fields here; update when the backend contract changes.

export type Status = 'pending' | 'queued' | 'running' | 'completed' | 'failed' | 'cancelled' | 'inconclusive'
export type ScenarioStatus = 'pending' | 'approved' | 'rejected'

export interface ApiError {
  /** Legacy/alias field; the API currently sends `message`. */
  error?: string
  /** Human-readable error message (backend `errorBody.message`). */
  message?: string
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
  id: string
  pair_id: string
  run_type: 'baseline' | 'variant'
  control_hash: string
  manifest_hash: string
  alert_count: number
  alert_summary?: Record<string, number>
  started_at: string
  completed_at?: string
}

export interface Outcome {
  id: string
  pair_id: string
  baseline_run_id: string
  variant_run_id: string
  evasion_delta: number
  new_alert_rule_ids: string[]
  dropped_alert_rule_ids: string[]
  outcome_unchanged: boolean
  created_at: string
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
  mutation_spec?: Record<string, unknown>
  campaign_id?: string
  created_at: string
  completed_at?: string
  /** Outcome summary — present only once the pair has completed. */
  evasion_delta?: number
  new_alert_rule_ids?: string[]
  dropped_alert_rule_ids?: string[]
  outcome_unchanged?: boolean
}

/** GET /experiments/{id} returns the pair, its outcome, and both runs. */
export interface ExperimentDetailResponse {
  pair: ExperimentPair
  outcome?: Outcome
  runs: RunResult[]
}

export interface CreateExperimentRequest {
  cohort_id: string
  name: string
  declared_change: string
  mutation_type: string
  mutation_spec: Record<string, unknown>
  campaign_id?: string
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
  scenario_params: ScenarioProposal | null
  baseline_alerts: { id: string; rule_id: string }[]
  variant_alerts:  { id: string; rule_id: string }[]
  aml_diff: {
    baseline_alert_count: number
    variant_alert_count: number
    evasion_delta: number
    new_alert_rule_ids: string[]
    dropped_alert_rule_ids: string[]
  }
  coverage_delta: CoverageRule[]
  gemini_trace: GeminiTrace | null
  created_at: string
  checksum: string
  /** Honest limitation labels (AC-06). */
  limitations?: string[]
}

/* ── Coverage ── */
export type OutcomeStatus =
  | 'triggered'
  | 'not_triggered'
  | 'not_applicable'
  | 'inconclusive'
  | 'failed'

export interface CoverageRule {
  id: string
  rule_id: string
  rule_version: string
  times_triggered: number
  times_not_triggered: number
  times_not_applicable: number
  times_inconclusive: number
  times_failed: number
  last_status?: OutcomeStatus
  last_triggered_at?: string
  last_evaluated_at?: string
  coverage_weight: number
  updated_at: string
}

export interface ScenarioCoverage {
  dimension: string
  value: string
  times_seen: number
  last_seen_at: string
}

export interface OutcomeCoverage {
  rule_id: string
  status: OutcomeStatus
  times_seen: number
  last_seen_at: string
}

export interface CoverageSummary {
  total_rules: number
  covered_rules: number
  coverage_pct: number
  rules: CoverageRule[]
  scenario: ScenarioCoverage[]
  outcomes: OutcomeCoverage[]
}

/* ── Reviews ── */
export type ReviewDecisionValue = 'approved' | 'rejected' | 'escalated'

export interface ReviewDecision {
  id: string
  pair_id: string
  reviewer: string
  decision: ReviewDecisionValue
  rationale: string
  created_at: string
}

/** The queue flattens the pair and its outcome summary. */
export interface ReviewQueueItem extends ExperimentPair {}

/* ── Campaign ── */
export type CampaignStatus = 'active' | 'completed' | 'paused'

export interface Campaign {
  id: string
  name: string
  description: string
  goal?: string
  cohort_id?: string
  budget: number
  experiments_used: number
  status: CampaignStatus
  control_config?: Record<string, unknown>
  created_at: string
  updated_at: string
}

export interface CreateCampaignRequest {
  name: string
  description: string
  budget: number
  goal?: string
  cohort_id?: string
}

export interface UpdateCampaignRequest {
  description?: string
  budget?: number
  status?: CampaignStatus
}

/** The honest campaign coverage payload: the maps are global, not sliced. */
export interface CampaignCoverage {
  campaign_id: string
  cohort_id?: string
  experiments: number
  budget: number
  experiments_used: number
  coverage_scope: 'global'
  coverage_endpoint: string
  note: string
}

/* ── Adaptive discovery & benchmark ── */
export type BenchmarkStrategy = 'random' | 'gemini_only' | 'adaptive'

export interface GenomeDimensions {
  actors: string
  accounts: string
  transactions: string
  transaction_count: number
  temporal: string
  network: string
  counterparty_count: number
  ownership: string
  geography: string
  behavioral_change: string
  control_context: string
}

export interface Genome {
  genome_id: string
  schema_version: string
  reference_typology_id?: string
  dimensions: GenomeDimensions
  constraints?: string[]
  synthetic_only: boolean
}

export interface ScoreComponents {
  coverage: number
  novelty: number
  information: number
  validity: number
}

export interface ScoredCandidate {
  proposal_id: string
  mutation_type: string
  genome: Genome
  genome_valid: boolean
  components: ScoreComponents
  score: number
  rank: number
  selected: boolean
  rationale: string
}

export interface DiscoveryRun {
  id: string
  cohort_id: string
  strategy: BenchmarkStrategy
  weights: {
    version: string
    coverage: number
    novelty: number
    information: number
    validity: number
  }
  seed: number
  budget: number
  pool_size: number
  valid_pool_size: number
  candidates: ScoredCandidate[]
  rejected?: { proposal_id: string; reason: string }[]
  notes?: string
  duration_ms: number
  created_at: string
}

export interface StrategySummary {
  strategy: BenchmarkStrategy
  run_id: string
  weights_version: string
  seed: number
  budget: number
  pool_size: number
  valid_pool_size: number
  selected_candidates: number
  approved_candidates: number
  valid_candidate_yield: number
  approved_rate: number
  expected_coverage_gain: number
  mean_novelty: number
  mean_information_gain: number
  mean_adaptive_score: number
  runtime_ms: number
}

export interface ComparisonResult {
  cohort_id: string
  strategies: StrategySummary[]
  winner?: BenchmarkStrategy
  insufficient_data: boolean
  missing_strategies?: string[]
  notes: string
  computed_at: string
}

/* ── Regression Test Pack ── */
export interface RegressionCaseResult {
  experiment_id: string
  name: string
  passed: boolean
  mismatches?: string[]
  expected: Record<string, unknown>
  actual: Record<string, unknown>
}

export interface RegressionVerifyResponse {
  ok: boolean
  pack_version: string
  total: number
  passed: number
  failed: number
  results: RegressionCaseResult[]
}

/* ── Authentication & authorization ── */

/** Authorization roles, hierarchical: admin ⊃ analyst. */
export type Role = 'analyst' | 'admin'

/** The authenticated caller. */
export interface Principal {
  username: string
  role: Role
}

/** Response body of POST /auth/login. */
export interface LoginResponse {
  token: string
  token_type: string
  expires_at: string
  user: Principal
}

/* ── User management (admin) ── */

/** An application account as returned by GET /users. */
export interface AppUser {
  username: string
  role: Role
  disabled: boolean
  created_at: string
  updated_at: string
}

export interface CreateUserRequest {
  username: string
  password: string
  role: Role
}

export interface UpdateUserRequest {
  role?: Role
  password?: string
  disabled?: boolean
}

/* ── Audit log (admin) ── */

/** An immutable audit event from GET /audit. */
export interface AuditEvent {
  id: string
  org_id: string
  event_type: string
  actor_uid: string
  target_type: string
  target_id: string
  details: Record<string, unknown> | null
  ip_address?: string
  user_agent?: string
  created_at: string
}
