// Discovery Overview — aggregates evasion discoveries, detection blind spots,
// and the scenario pipeline into one red-team situational view.
import { useQuery } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { PageHeader } from '../components/Layout'
import { LoadingState, ErrorState, EmptyState } from '../components/StateViews'
import { DeltaBadge } from '../components/StatusBadge'
import { experimentApi, coverageApi, scenarioApi } from '../api/endpoints'
import {
  computeDiscoveryMetrics,
  rankDiscoveries,
  evadedRuleIDs,
  uncoveredRules,
} from '../lib/discovery'

export function DiscoveryOverview() {
  const nav = useNavigate()
  const exp = useQuery({ queryKey: ['experiments'], queryFn: experimentApi.list })
  const cov = useQuery({ queryKey: ['coverage'], queryFn: coverageApi.get })
  const scn = useQuery({ queryKey: ['scenarios'], queryFn: scenarioApi.list })

  const experiments = exp.data ?? []
  const metrics = computeDiscoveryMetrics(experiments, cov.data)
  const top = rankDiscoveries(experiments).slice(0, 15)
  const evaded = evadedRuleIDs(experiments)
  const gaps = uncoveredRules(cov.data)

  const scenarios = scn.data ?? []
  const pendingScenarios = scenarios.filter(s => s.status === 'pending').length
  const approvedScenarios = scenarios.filter(s => s.status === 'approved').length

  return (
    <>
      <PageHeader
        title="Discovery overview"
        subtitle="Where the red team found evasion — and where detection is blind"
        actions={
          <button className="btn btn-primary btn-sm" onClick={() => nav('/experiments/new')}>
            Run experiment
          </button>
        }
      />
      <div className="page-body">
        {/* Discovery metrics */}
        <div className="metric-grid" style={{ marginBottom: 24 }}>
          <div className="metric-card">
            <div className="metric-label">Discoveries</div>
            <div className="metric-value" style={{ color: 'var(--danger)' }}>
              {metrics.discoveries}
            </div>
            <div className="metric-sub">mutations that evaded detection</div>
          </div>
          <div className="metric-card">
            <div className="metric-label">Rules Evaded</div>
            <div className="metric-value" style={{ color: 'var(--danger)' }}>
              {metrics.distinctRulesEvaded}
            </div>
            <div className="metric-sub">distinct AML rules bypassed</div>
          </div>
          <div className="metric-card">
            <div className="metric-label">Detection blind spots</div>
            <div className="metric-value" style={{ color: 'var(--warning)' }}>
              {metrics.uncoveredRules}
            </div>
            <div className="metric-sub">rules never triggered</div>
          </div>
          <div className="metric-card">
            <div className="metric-label">Detections Improved</div>
            <div className="metric-value" style={{ color: 'var(--success)' }}>
              {metrics.regressions}
            </div>
            <div className="metric-sub">mutations that raised alerts</div>
          </div>
        </div>

        {/* Top discoveries */}
        <div className="card" style={{ marginBottom: 24 }}>
          <div className="card-header">
            <span className="card-title">Top discoveries</span>
            <button className="btn btn-ghost btn-sm" onClick={() => nav('/experiments')}>
              View all →
            </button>
          </div>
          {exp.isLoading ? (
            <LoadingState title="Loading discoveries…" />
          ) : exp.error ? (
            <ErrorState title="Failed to load experiments" body={String(exp.error)} />
          ) : top.length === 0 ? (
            <EmptyState
              title="No evasion discovered yet"
              body="Run experiments that mutate transactions to find gaps in the AML rules."
              action={
                <button className="btn btn-primary btn-sm" onClick={() => nav('/experiments/new')}>
                  Run experiment
                </button>
              }
            />
          ) : (
            <div className="table-wrap">
              <table aria-label="Top discoveries">
                <thead>
                  <tr>
                    <th scope="col">Experiment</th>
                    <th scope="col">Mutation</th>
                    <th scope="col">Evaded Rules</th>
                    <th scope="col">Δ</th>
                    <th scope="col">Found</th>
                  </tr>
                </thead>
                <tbody>
                  {top.map(e => (
                    <tr
                      key={e.id}
                      className="clickable-row"
                      tabIndex={0}
                      role="link"
                      aria-label={`Open experiment ${e.name}`}
                      onClick={() => nav(`/experiments/${e.id}`)}
                      onKeyDown={event => {
                        if (event.key === 'Enter') nav(`/experiments/${e.id}`)
                      }}
                    >
                      <td>{e.name}</td>
                      <td><span className="tag">{e.mutation_type}</span></td>
                      <td>
                        {(e.dropped_alert_rule_ids ?? []).length === 0
                          ? <span className="text-muted">—</span>
                          : (e.dropped_alert_rule_ids ?? []).map(r => (
                              <span key={r} className="tag" style={{ marginRight: 4 }}>{r}</span>
                            ))}
                      </td>
                      <td><DeltaBadge delta={e.evasion_delta ?? 0} /></td>
                      <td className="td-muted">
                        {new Date(e.created_at).toLocaleDateString()}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>

        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 24 }}>
          {/* Evasion vectors */}
          <div className="card">
            <div className="card-header">
              <span className="card-title">Evasion vectors</span>
            </div>
            {evaded.length === 0 ? (
              <div className="page-body text-muted" style={{ fontSize: 13 }}>
                No rules have been bypassed yet.
              </div>
            ) : (
              <ul style={{ margin: 0, padding: '8px 20px 16px', listStyle: 'none' }}>
                {evaded.map(r => (
                  <li key={r} style={{ padding: '6px 0', display: 'flex', gap: 8, alignItems: 'center' }}>
                    <span className="tag tag-danger">{r}</span>
                    <span className="text-muted" style={{ fontSize: 12 }}>bypassed by at least one mutation</span>
                  </li>
                ))}
              </ul>
            )}
          </div>

          {/* Detection blind spots */}
          <div className="card">
            <div className="card-header">
              <span className="card-title">Detection blind spots</span>
              <button className="btn btn-ghost btn-sm" onClick={() => nav('/coverage')}>
                Coverage →
              </button>
            </div>
            {cov.isLoading ? (
              <LoadingState title="Loading coverage…" />
            ) : gaps.length === 0 ? (
              <div className="page-body text-muted" style={{ fontSize: 13 }}>
                Every known rule has fired at least once.
              </div>
            ) : (
              <div className="table-wrap">
                <table aria-label="Uncovered rules">
                  <thead>
                    <tr>
                      <th scope="col">Rule</th>
                      <th scope="col">Version</th>
                      <th scope="col">Weight</th>
                    </tr>
                  </thead>
                  <tbody>
                    {gaps.map(r => (
                      <tr key={r.id}>
                        <td><span className="tag tag-warning">{r.rule_id}</span></td>
                        <td className="td-muted">{r.rule_version}</td>
                        <td>{r.coverage_weight.toFixed(2)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>
        </div>

        {/* Scenario pipeline */}
        <div className="card" style={{ marginTop: 24 }}>
          <div className="card-header">
            <span className="card-title">Scenario pipeline</span>
            <button className="btn btn-ghost btn-sm" onClick={() => nav('/scenarios')}>
              Manage →
            </button>
          </div>
          <div className="metric-grid" style={{ padding: 16 }}>
            <div className="metric-card">
              <div className="metric-label">Pending Review</div>
              <div className="metric-value">{pendingScenarios}</div>
            </div>
            <div className="metric-card">
              <div className="metric-label">Approved</div>
              <div className="metric-value">{approvedScenarios}</div>
            </div>
            <div className="metric-card">
              <div className="metric-label">Total Proposals</div>
              <div className="metric-value">{scenarios.length}</div>
            </div>
          </div>
        </div>
      </div>
    </>
  )
}
