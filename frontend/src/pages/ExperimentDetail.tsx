import { useParams, useNavigate } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { PageHeader } from '../components/Layout'
import { StatusBadge, DeltaBadge } from '../components/StatusBadge'
import { LoadingState, ErrorState } from '../components/StateViews'
import { JsonViewer } from '../components/JsonViewer'
import { experimentApi } from '../api/endpoints'

export function ExperimentDetail() {
  const { id } = useParams<{ id: string }>()
  const nav = useNavigate()

  const { data: exp, isLoading, error } = useQuery({
    queryKey: ['experiment', id],
    queryFn: () => experimentApi.get(id!),
    enabled: !!id,
    // Poll while running
    refetchInterval: (q) => q.state.data?.status === 'running' ? 3000 : false,
  })

  if (isLoading) return (
    <><PageHeader title="Experiment" /><div className="page-body"><LoadingState /></div></>
  )
  if (error || !exp) return (
    <><PageHeader title="Experiment" /><div className="page-body"><ErrorState title="Failed to load experiment" body={String(error)} /></div></>
  )

  const isComplete = exp.status === 'completed'
  const outcome = exp.outcome
  const baseline = exp.baseline
  const variant  = exp.variant

  return (
    <>
      <PageHeader
        title={exp.name}
        subtitle={`Experiment · ${id}`}
        actions={
          isComplete && (
            <button className="btn btn-secondary btn-sm" onClick={() => nav(`/experiments/${id}/evidence`)}>
              📦 View Evidence Bundle
            </button>
          )
        }
      />
      <div className="page-body">
        {/* Metadata row */}
        <div className="metric-grid" style={{ marginBottom: 24 }}>
          <div className="metric-card">
            <div className="metric-label">Status</div>
            <div style={{ marginTop: 8 }}><StatusBadge status={exp.status} /></div>
          </div>
          <div className="metric-card">
            <div className="metric-label">Mutation</div>
            <div style={{ marginTop: 8 }}><span className="tag">{exp.mutation_type}</span></div>
          </div>
          <div className="metric-card">
            <div className="metric-label">Evasion Δ</div>
            <div className="metric-value" style={{ marginTop: 4 }}>
              {isComplete ? <DeltaBadge delta={outcome?.evasion_delta ?? 0} /> : <span className="text-muted">—</span>}
            </div>
          </div>
          <div className="metric-card">
            <div className="metric-label">Outcome</div>
            <div style={{ marginTop: 8, fontSize: 13 }}>
              {isComplete
                ? (outcome?.outcome_unchanged
                    ? <span style={{ color: 'var(--text-secondary)' }}>Unchanged</span>
                    : <span style={{ color: outcome.evasion_delta > 0 ? 'var(--error)' : 'var(--success)' }}>
                        {outcome.evasion_delta > 0 ? 'Evasion detected' : 'Alerts increased'}
                      </span>
                  )
                : <span className="text-muted">—</span>}
            </div>
          </div>
        </div>

        {/* Baseline vs Variant comparison */}
        {isComplete && baseline && variant && (
          <div className="section" style={{ marginBottom: 24 }}>
            <div className="section-header"><span className="section-title">Baseline vs. Variant</span></div>
            <div className="compare-grid">
              <div className="compare-side">
                <div className="compare-label">Baseline</div>
                <div className="compare-count baseline">{baseline.alert_count}</div>
                <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 8 }}>alerts generated</div>
                <div style={{ fontSize: 11, fontFamily: 'var(--font-mono)', marginTop: 12, color: 'var(--text-muted)' }}>
                  run: {new Date(baseline.run_at).toLocaleString()}
                </div>
              </div>
              <div className="compare-side">
                <div className="compare-label">Variant</div>
                <div className="compare-count variant">{variant.alert_count}</div>
                <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 8 }}>alerts generated</div>
                <div style={{ fontSize: 11, fontFamily: 'var(--font-mono)', marginTop: 12, color: 'var(--text-muted)' }}>
                  run: {new Date(variant.run_at).toLocaleString()}
                </div>
              </div>
            </div>
          </div>
        )}

        {/* Alert diff table */}
        {isComplete && outcome && (
          <div className="card" style={{ marginBottom: 16 }}>
            <div className="card-header">
              <span className="card-title">Alert Diff</span>
              <DeltaBadge delta={outcome.evasion_delta} />
            </div>
            <div className="table-wrap">
              <table aria-label="Alert diff">
                <thead>
                  <tr>
                    <th scope="col">Rule ID</th>
                    <th scope="col">Baseline</th>
                    <th scope="col">Variant</th>
                    <th scope="col">Δ</th>
                    <th scope="col">Effect</th>
                  </tr>
                </thead>
                <tbody>
                  {/* New alerts (evasion caught) */}
                  {outcome.new_alert_rule_ids.map(ruleId => (
                    <tr key={`new-${ruleId}`}>
                      <td className="td-mono">{ruleId}</td>
                      <td>0</td>
                      <td>1</td>
                      <td><DeltaBadge delta={-1} /></td>
                      <td><span className="tag" style={{ color: 'var(--success)' }}>New alert</span></td>
                    </tr>
                  ))}
                  {/* Dropped alerts (evasion) */}
                  {outcome.dropped_alert_rule_ids.map(ruleId => (
                    <tr key={`drop-${ruleId}`}>
                      <td className="td-mono">{ruleId}</td>
                      <td>1</td>
                      <td>0</td>
                      <td><DeltaBadge delta={1} /></td>
                      <td><span className="tag" style={{ color: 'var(--error)' }}>Evaded</span></td>
                    </tr>
                  ))}
                  {outcome.new_alert_rule_ids.length === 0 && outcome.dropped_alert_rule_ids.length === 0 && (
                    <tr>
                      <td colSpan={5} style={{ textAlign: 'center', color: 'var(--text-muted)', padding: '16px' }}>
                        No rule-level changes — alert sets are identical
                      </td>
                    </tr>
                  )}
                </tbody>
              </table>
            </div>
          </div>
        )}

        {/* Experiment metadata */}
        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 16 }}>
          <div className="card">
            <div className="card-header"><span className="card-title">Hashes</span></div>
            <div className="card-body">
              {[
                ['Control hash', exp.control_hash],
                ['Manifest hash', exp.manifest_hash],
              ].map(([k, v]) => (
                <div key={k} style={{ marginBottom: 12 }}>
                  <div className="metric-label">{k}</div>
                  <div className="monospace" style={{ marginTop: 4, wordBreak: 'break-all', fontSize: 11 }}>{v ?? '—'}</div>
                </div>
              ))}
            </div>
          </div>
          <div className="card">
            <div className="card-header"><span className="card-title">Declared Change</span></div>
            <div className="card-body">
              <p style={{ fontSize: 13, lineHeight: 1.7 }}>{exp.declared_change}</p>
            </div>
          </div>
        </div>

        {/* Mutation spec */}
        <div className="card" style={{ marginTop: 16 }}>
          <div className="card-header"><span className="card-title">Mutation Spec</span></div>
          <div className="card-body">
            <JsonViewer value={exp.mutation_spec ?? {}} maxHeight={200} />
          </div>
        </div>
      </div>
    </>
  )
}
