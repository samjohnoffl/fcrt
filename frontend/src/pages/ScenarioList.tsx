import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { PageHeader } from '../components/Layout'
import { RoleGate } from '../components/RoleGate'
import { StatusBadge } from '../components/StatusBadge'
import { LoadingState, ErrorState, EmptyState } from '../components/StateViews'
import { scenarioApi, cohortApi } from '../api/endpoints'

export function ScenarioList() {
  const nav = useNavigate()
  const qc = useQueryClient()
  const [filter, setFilter] = useState<'all' | 'pending' | 'approved' | 'rejected'>('all')
  const [requestDialog, setRequestDialog] = useState(false)
  const [selectedCohort, setSelectedCohort] = useState('')

  const { data, isLoading, error } = useQuery({ queryKey: ['scenarios'], queryFn: scenarioApi.list })
  const { data: cohorts } = useQuery({ queryKey: ['cohorts'], queryFn: cohortApi.list })

  const requestMut = useMutation({
    mutationFn: () => scenarioApi.request(selectedCohort),
    onSuccess: (s) => {
      qc.invalidateQueries({ queryKey: ['scenarios'] })
      setRequestDialog(false)
      nav(`/scenarios/${s.id}`)
    },
  })

  const filtered = (data ?? []).filter(s => filter === 'all' || s.status === filter)

  return (
    <>
      <PageHeader
        title="Scenarios"
        subtitle="Gemini-assisted AML evasion proposals"
        actions={<RoleGate min="analyst"><button className="btn btn-primary btn-sm" onClick={() => setRequestDialog(true)}>Request scenario</button></RoleGate>}
      />
      <div className="page-body">
        <div className="filter-bar" style={{ marginBottom: 16 }}>
          <span className="text-muted" style={{ fontSize: 12 }}>Status:</span>
          {(['all', 'pending', 'approved', 'rejected'] as const).map(f => (
            <button key={f} className={`btn btn-sm ${filter === f ? 'btn-primary' : 'btn-ghost'}`} onClick={() => setFilter(f)}>
              {f.charAt(0).toUpperCase() + f.slice(1)}
            </button>
          ))}
        </div>
        <div className="card">
          {isLoading ? <LoadingState title="Loading scenarios…" /> :
           error ? <ErrorState title="Failed to load scenarios" body={String(error)} /> :
           filtered.length === 0 ? (
            <EmptyState
              title="No scenarios"
              body="Request a Gemini scenario proposal to get started."
              action={<RoleGate min="analyst"><button className="btn btn-primary btn-sm" onClick={() => setRequestDialog(true)}>Request scenario</button></RoleGate>}
            />
          ) : (
            <div className="table-wrap">
              <table aria-label="Scenario proposals">
                <thead>
                  <tr>
                    <th scope="col">Type</th>
                    <th scope="col">Cohort</th>
                    <th scope="col">Status</th>
                    <th scope="col">Model</th>
                    <th scope="col">Created</th>
                  </tr>
                </thead>
                <tbody>
                  {filtered.map(s => (
                    <tr
                      key={s.id}
                      className="clickable-row"
                      tabIndex={0}
                      role="link"
                      aria-label={`Open scenario ${s.scenario_type}`}
                      onClick={() => nav(`/scenarios/${s.id}`)}
                      onKeyDown={event => {
                        if (event.key === 'Enter') nav(`/scenarios/${s.id}`)
                      }}
                    >
                      <td><span className="tag">{s.scenario_type}</span></td>
                      <td className="td-mono td-muted">{s.cohort_id.slice(0, 8)}…</td>
                      <td><StatusBadge status={s.status} /></td>
                      <td className="td-muted">{s.gemini_model}</td>
                      <td className="td-muted">{new Date(s.created_at).toLocaleDateString()}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      </div>

      {/* Request dialog */}
      <div className="dialog-backdrop" role="dialog" aria-modal="true" aria-labelledby="req-title"
        style={{ display: requestDialog ? 'grid' : 'none' }}
        onClick={e => { if (e.target === e.currentTarget) setRequestDialog(false) }}
      >
        <div className="dialog">
          <h2 id="req-title" className="dialog-title">Request Gemini scenario</h2>
          <p className="dialog-body">Select a locked cohort. Gemini will propose an AML evasion scenario based on the cohort profile and current coverage gaps.</p>
          <div className="form-group">
            <label className="form-label" htmlFor="cohort-select">Cohort *</label>
            <select id="cohort-select" className="form-select" value={selectedCohort} onChange={e => setSelectedCohort(e.target.value)}>
              <option value="">Select a cohort…</option>
              {(cohorts ?? []).filter(c => !!c.locked_at).map(c => (
                <option key={c.id} value={c.id}>{c.name}</option>
              ))}
            </select>
            {cohorts && cohorts.filter(c => !!c.locked_at).length === 0 && (
              <span className="form-hint" style={{ color: 'var(--warning)' }}>No locked cohorts. Lock a cohort first.</span>
            )}
          </div>
          {requestMut.error && <span className="form-error">{String(requestMut.error)}</span>}
          <div className="dialog-actions">
            <button className="btn btn-secondary" onClick={() => setRequestDialog(false)}>Cancel</button>
            <button
              className="btn btn-primary"
              onClick={() => requestMut.mutate()}
              disabled={!selectedCohort || requestMut.isPending}
            >
              {requestMut.isPending ? 'Requesting…' : 'Request'}
            </button>
          </div>
        </div>
      </div>
    </>
  )
}
