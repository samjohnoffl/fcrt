import { api } from './client'
import type {
  Cohort, CreateCohortRequest, CoverageSummary,
  EvidenceBundle, ExperimentDetail, ExperimentPair,
  CreateExperimentRequest, ScenarioProposal,
} from './types'

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
  get:      (id: string)    => api.get<ExperimentDetail>(`/experiments/${id}`),
  create:   (r: CreateExperimentRequest) => api.post<ExperimentPair>('/experiments', r),
  evidence: (id: string)    => api.get<EvidenceBundle>(`/experiments/${id}/evidence`),
}

/* ── Coverage ── */
export const coverageApi = {
  get: () => api.get<CoverageSummary>('/coverage'),
}
