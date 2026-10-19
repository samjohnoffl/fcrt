import { api } from './client'
import type {
  Cohort, CreateCohortRequest, CoverageSummary,
  EvidenceBundle, ExperimentDetailResponse, ExperimentPair,
  CreateExperimentRequest, ScenarioProposal,
  Campaign, CreateCampaignRequest, UpdateCampaignRequest, CampaignCoverage,
  ComparisonResult, DiscoveryRun, BenchmarkStrategy,
  RegressionVerifyResponse, LoginResponse, Principal,
  AppUser, CreateUserRequest, UpdateUserRequest, AuditEvent,
  ReviewDecision, ReviewDecisionValue, ReviewQueueItem,
} from './types'

/* ── Authentication ── */
export const authApi = {
  login:  (username: string, password: string) =>
    api.post<LoginResponse>('/auth/login', { username, password }),
  me:     () => api.get<Principal>('/auth/me'),
  logout: () => api.post<{ status: string }>('/auth/logout', {}),
}

/* ── User management (admin only) ── */
export const userApi = {
  list:   ()                       => api.get<AppUser[]>('/users'),
  create: (r: CreateUserRequest)   => api.post<AppUser>('/users', r),
  update: (username: string, r: UpdateUserRequest) =>
    api.patch<AppUser>(`/users/${encodeURIComponent(username)}`, r),
  remove: (username: string)       =>
    api.delete<{ status: string }>(`/users/${encodeURIComponent(username)}`),
}

/* ── Audit log (admin only) ── */
export const auditApi = {
  list: (params?: { limit?: number; event_type?: string }) => {
    const q = new URLSearchParams()
    if (params?.limit) q.set('limit', String(params.limit))
    if (params?.event_type) q.set('event_type', params.event_type)
    const qs = q.toString()
    return api.get<AuditEvent[]>(`/audit${qs ? `?${qs}` : ''}`)
  },
}

/* ── Cohorts ── */
export const cohortApi = {
  list:   ()              => api.get<Cohort[]>('/cohorts'),
  get:    (id: string)    => api.get<Cohort>(`/cohorts/${id}`),
  create: (r: CreateCohortRequest) => api.post<Cohort>('/cohorts', r),
  lock:   (id: string)    => api.post<Cohort>(`/cohorts/${id}/lock`, {}),
}

/* ── Scenarios ── */
export const scenarioApi = {
  list:    ()              => api.get<ScenarioProposal[]>('/scenarios'),
  get:     (id: string)    => api.get<ScenarioProposal>(`/scenarios/${id}`),
  request: (cohort_id: string) => api.post<ScenarioProposal>('/scenarios', { cohort_id }),
  approve: (id: string)   => api.patch<ScenarioProposal>(`/scenarios/${id}/approve`),
  reject:  (id: string)   => api.patch<ScenarioProposal>(`/scenarios/${id}/reject`),
}

/* ── Experiments ── */
export const experimentApi = {
  list:     ()              => api.get<ExperimentPair[]>('/experiments'),
  get:      (id: string)    => api.get<ExperimentDetailResponse>(`/experiments/${id}`),
  create:   (r: CreateExperimentRequest) => api.post<ExperimentPair>('/experiments', r),
  evidence: (id: string)    => api.get<EvidenceBundle>(`/experiments/${id}/evidence`),
}

/* ── Coverage ── */
export const coverageApi = {
  get: () => api.get<CoverageSummary>('/coverage'),
}

/* ── Adaptive discovery & benchmark (cohort-scoped) ── */
export const discoveryApi = {
  /** Run one selection strategy and persist its scoring trace. */
  run: (
    cohortId: string,
    body: { strategy: BenchmarkStrategy; budget?: number; seed?: number },
  ) => api.post<DiscoveryRun>(`/cohorts/${cohortId}/discovery`, body),

  /** Latest stored run for each strategy. */
  latest: (cohortId: string) =>
    api.get<{ cohort_id: string; runs: Record<string, DiscoveryRun>; note: string }>(
      `/cohorts/${cohortId}/discovery`,
    ),

  /** Compare the stored runs. Returns insufficient_data until all three exist. */
  benchmark: (cohortId: string) =>
    api.get<ComparisonResult>(`/cohorts/${cohortId}/benchmark`),
}

/* ── Campaigns ── */
export const campaignApi = {
  list:      ()                                  => api.get<Campaign[]>('/campaigns'),
  get:       (id: string)                        => api.get<Campaign>(`/campaigns/${id}`),
  create:    (r: CreateCampaignRequest)          => api.post<Campaign>('/campaigns', r),
  update:    (id: string, r: UpdateCampaignRequest) =>
    api.patch<Campaign>(`/campaigns/${id}`, r),
  coverage:  (id: string)                        => api.get<CampaignCoverage>(`/campaigns/${id}/coverage`),
  benchmark: (id: string)                        => api.get<ComparisonResult>(`/campaigns/${id}/benchmark`),
}

/* ── Reviews ── */
export const reviewApi = {
  queue: () => api.get<{ queue: ReviewQueueItem[]; count: number }>('/reviews/queue'),
  get: (id: string) => api.get<ReviewDecision>(`/experiments/${id}/review`),
  create: (id: string, decision: ReviewDecisionValue, rationale: string) =>
    api.post<ReviewDecision>(`/experiments/${id}/review`, { decision, rationale }),
}

/* ── Regression Test Pack (PRODUCT_SPEC item 7) ── */
export const regressionApi = {
  /** Download the regression pack as a JSON file (auth header attached). */
  download: (limit?: number) =>
    api.download(
      `/regression/pack${limit ? `?limit=${limit}` : ''}`,
      'regression-pack.json',
    ),
  verify: (pack: unknown) => api.post<RegressionVerifyResponse>('/regression/verify', pack),
}
