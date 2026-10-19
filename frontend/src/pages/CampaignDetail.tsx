import { useNavigate, useParams } from 'react-router-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { PageHeader } from '../components/Layout'
import { RoleGate } from '../components/RoleGate'
import { StatusBadge } from '../components/StatusBadge'
import { LoadingState, ErrorState, EmptyState } from '../components/StateViews'
import { JsonViewer } from '../components/JsonViewer'
import { campaignApi, cohortApi, experimentApi } from '../api/endpoints'

function pct(v: number): string {
  return `${(v * 100).toFixed(1)}%`
}

export function CampaignDetail() {
  const { id } = useParams<{ id: string }>()
  const nav = useNavigate()
  const qc = useQueryClient()

  const campaign = useQuery({
    queryKey: ['campaign', id],
    queryFn: () => campaignApi.get(id!),
    enabled: !!id,
  })
  const coverage = useQuery({
    queryKey: ['campaign', id, 'coverage'],
    queryFn: () => campaignApi.coverage(id!),
    enabled: !!id,
  })
  const benchmark = useQuery({
    queryKey: ['campaign', id, 'benchmark'],
    queryFn: () => campaignApi.benchmark(id!),
    enabled: !!id && !!campaign.data?.cohort_id,
    retry: false,
  })
  const cohorts = useQuery({ queryKey: ['cohorts'], queryFn: cohortApi.list })
  const experiments = useQuery({ queryKey: ['experiments'], queryFn: experimentApi.list })

  const statusMut = useMutation({
    mutationFn: (status: 'active' | 'paused' | 'completed') => campaignApi.update(id!, { status }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['campaign', id] })
      qc.invalidateQueries({ queryKey: ['campaigns'] })
    },
  })

  if (campaign.isLoading) return (
    <>
      <PageHeader title="Campaign" />
      <div className="page-body"><LoadingState /></div>
    </>
  )
  if (campaign.error || !campaign.data) return (
    <>
      <PageHeader title="Campaign" />
      <div className="page-body">
        <ErrorState title="Failed to load campaign" body={String(campaign.error)} />
      </div>
    </>
  )

  const c = campaign.data
  const cohort = (cohorts.data ?? []).find(x => x.id === c.cohort_id)
  const linked = (experiments.data ?? []).filter(e => e.campaign_id === c.id)
  const remaining = Math.max(0, c.budget - c.experiments_used)
  const pctUsed = c.budget > 0 ? Math.min(100, (c.experiments_used / c.budget) * 100) : 0

  return (
    <>
      <PageHeader
        title={c.name}
        subtitle={`Campaign · ${c.id}`}
        actions={
          <RoleGate min="analyst">
            {c.status === 'active' ? (
              <button className="btn btn-secondary btn-sm" onClick={() => statusMut.mutate('paused')}
                disabled={statusMut.isPending}>
                Pause campaign
              </button>
            ) : (
              <button className="btn btn-primary btn-sm" onClick={() => statusMut.mutate('active')}
                disabled={statusMut.isPending}>
                Reactivate campaign
              </button>
            )}
            <button className="btn btn-primary btn-sm"
              onClick={() => nav(`/experiments/new?campaign=${c.id}${c.cohort_id ? `&cohort=${c.cohort_id}` : ''}`)}
              disabled={c.status !== 'active' || remaining === 0}>
              Run experiment
            </button>
          </RoleGate>
        }
      />
      <div className="page-body">
        {statusMut.error && <div className="form-error" style={{ marginBottom: 12 }}>{String(statusMut.error)}</div>}

        <div className="metric-grid" style={{ marginBottom: 24 }}>
          <div className="metric-card">
            <div className="metric-label">Status</div>
            <div style={{ marginTop: 6 }}><StatusBadge status={c.status} /></div>
          </div>
          <div className="metric-card">
            <div className="metric-label">Budget used</div>
            <div className="metric-value">{c.experiments_used} / {c.budget}</div>
            <div className="meter" aria-label={`${Math.round(pctUsed)}% of budget used`}>
              <span style={{ width: `${pctUsed}%` }} />
            </div>
            <div className="metric-sub">{remaining} remaining</div>
          </div>
          <div className="metric-card">
            <div className="metric-label">Cohort</div>
            <div className="metric-value" style={{ fontSize: 16 }}>
              {cohort?.name ?? (c.cohort_id ? `${c.cohort_id.slice(0, 8)}…` : 'None')}
            </div>
            <div className="metric-sub">
              {c.cohort_id ? 'Benchmark available' : 'No cohort — benchmark unavailable'}
            </div>
          </div>
          <div className="metric-card">
            <div className="metric-label">Created</div>
            <div className="metric-value" style={{ fontSize: 16 }}>
              {new Date(c.created_at).toLocaleDateString()}
            </div>
            <div className="metric-sub">Fixed control configuration</div>
          </div>
        </div>

        {(c.description || c.goal) && (
          <div className="card" style={{ marginBottom: 24 }}>
            <div className="card-header"><span className="card-title">Brief</span></div>
            <div className="card-body">
              {c.goal && <p style={{ marginBottom: 8 }}><strong>Goal:</strong> {c.goal}</p>}
              {c.description && <p className="td-muted">{c.description}</p>}
            </div>
          </div>
        )}

        <div style={{ display: 'grid', gridTemplateColumns: 'minmax(0, 1fr) minmax(0, 1fr)', gap: 24, marginBottom: 24 }}>
          <div className="card">
            <div className="card-header">
              <span className="card-title">Experiments</span>
              <span className="tag">{linked.length}</span>
            </div>
            {linked.length === 0 ? (
              <EmptyState title="No experiments yet" body="Runs started from this campaign appear here." />
            ) : (
              <div className="table-wrap">
                <table aria-label="Campaign experiments">
                  <thead>
                    <tr>
                      <th scope="col">Name</th>
                      <th scope="col">Status</th>
                      <th scope="col">Created</th>
                    </tr>
                  </thead>
                  <tbody>
                    {linked.map(e => (
                      <tr key={e.id} style={{ cursor: 'pointer' }} onClick={() => nav(`/experiments/${e.id}`)}>
                        <td>{e.name}</td>
                        <td><StatusBadge status={e.status} /></td>
                        <td className="td-muted">{new Date(e.created_at).toLocaleDateString()}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>

          <div className="card">
            <div className="card-header">
              <span className="card-title">Coverage scope</span>
              <span className="tag">{coverage.data?.coverage_scope ?? 'global'}</span>
            </div>
            <div className="card-body">
              <p className="td-muted" style={{ fontSize: 13, marginBottom: 12 }}>
                {coverage.data?.note ?? 'Coverage maps are global for this deployment.'}
              </p>
              <button className="btn btn-secondary btn-sm" onClick={() => nav('/coverage')}>
                Open coverage dashboard
              </button>
            </div>
          </div>
        </div>

        <div className="card" style={{ marginBottom: 24 }}>
          <div className="card-header">
            <span className="card-title">Campaign benchmark</span>
            {benchmark.data?.insufficient_data && <span className="tag">Insufficient data</span>}
          </div>
          {!c.cohort_id ? (
            <EmptyState title="No cohort attached" body="Attach a cohort to compare stored selection strategies for this campaign." />
          ) : benchmark.isLoading ? (
            <LoadingState title="Reading stored discovery runs…" />
          ) : benchmark.error ? (
            <EmptyState title="Benchmark unavailable" body={String(benchmark.error)} />
          ) : !benchmark.data ? (
            <EmptyState title="No benchmark" body="Run discovery strategies for this cohort first." />
          ) : (
            <div className="card-body">
              <p className="td-muted" style={{ fontSize: 13, marginBottom: 12 }}>{benchmark.data.notes}</p>
              {benchmark.data.insufficient_data || benchmark.data.strategies.length === 0 ? (
                <div className="alert alert-warn" role="status">
                  Not enough stored runs to declare a winner. Missing: {(benchmark.data.missing_strategies ?? []).join(', ') || 'all strategies'}.
                </div>
              ) : (
                <div className="table-wrap">
                  <table aria-label="Campaign benchmark strategies">
                    <thead>
                      <tr>
                        <th scope="col">Strategy</th>
                        <th scope="col">Selected</th>
                        <th scope="col">Approved</th>
                        <th scope="col">Coverage gain</th>
                        <th scope="col">Novelty</th>
                        <th scope="col">Info gain</th>
                      </tr>
                    </thead>
                    <tbody>
                      {benchmark.data.strategies.map(s => (
                        <tr key={s.strategy}>
                          <td>
                            {s.strategy}
                            {benchmark.data?.winner === s.strategy && <span className="tag" style={{ marginLeft: 8 }}>winner</span>}
                          </td>
                          <td className="td-mono">{s.selected_candidates}</td>
                          <td className="td-mono">{pct(s.approved_rate)}</td>
                          <td className="td-mono">{pct(s.expected_coverage_gain)}</td>
                          <td className="td-mono">{pct(s.mean_novelty)}</td>
                          <td className="td-mono">{pct(s.mean_information_gain)}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </div>
          )}
        </div>

        {c.control_config && (
          <div className="card">
            <div className="card-header"><span className="card-title">Control configuration</span></div>
            <div className="card-body"><JsonViewer value={c.control_config} maxHeight={240} /></div>
          </div>
        )}
      </div>
    </>
  )
}
