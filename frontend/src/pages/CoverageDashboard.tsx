import { useQuery } from '@tanstack/react-query'
import { BarChart, Bar, XAxis, YAxis, Tooltip, ResponsiveContainer, Cell } from 'recharts'
import { PageHeader } from '../components/Layout'
import { LoadingState, ErrorState, EmptyState } from '../components/StateViews'
import { coverageApi } from '../api/endpoints'
import type { OutcomeStatus } from '../api/types'

const outcomeLabels: Record<OutcomeStatus, string> = {
  triggered: 'Triggered',
  not_triggered: 'Not triggered',
  not_applicable: 'Not applicable',
  inconclusive: 'Inconclusive',
  failed: 'Failed',
}

export function CoverageDashboard() {
  const { data, isLoading, error } = useQuery({ queryKey: ['coverage'], queryFn: coverageApi.get })

  const rules = data?.rules ?? []
  const scenarios = data?.scenario ?? []
  const uncovered = rules.filter(r => r.times_triggered === 0)
  const evaluations = rules.reduce(
    (sum, r) => sum + r.times_triggered + r.times_not_triggered + r.times_not_applicable + r.times_inconclusive + r.times_failed,
    0,
  )

  const chartData = [...rules]
    .sort((a, b) => b.times_triggered - a.times_triggered)
    .map(r => ({ name: r.rule_id, count: r.times_triggered, weight: r.coverage_weight }))

  // Scenario map: one row per genome dimension, one chip per exercised value.
  const scenarioByDimension = new Map<string, { value: string; times: number }[]>()
  for (const cell of scenarios) {
    const list = scenarioByDimension.get(cell.dimension) ?? []
    list.push({ value: cell.value, times: cell.times_seen })
    scenarioByDimension.set(cell.dimension, list)
  }

  return (
    <>
      <PageHeader
        title="Coverage Dashboard"
        subtitle="Three separate maps: control outcomes, scenario dimensions, and outcome distribution"
      />
      <div className="page-body">
        {isLoading ? <LoadingState title="Loading coverage…" /> :
         error ? <ErrorState title="Failed to load coverage" body={String(error)} /> :
         !data ? <EmptyState title="No coverage data" body="Run experiments to generate coverage data." /> : (
          <>
            {/* Summary cards */}
            <div className="metric-grid" style={{ marginBottom: 24 }}>
              <div className="metric-card">
                <div className="metric-label">Control coverage</div>
                <div className="metric-value" style={{ color: 'var(--success)' }}>
                  {data.coverage_pct.toFixed(1)}%
                </div>
                <div className="metric-sub">{data.covered_rules} of {data.total_rules} controls triggered</div>
              </div>
              <div className="metric-card">
                <div className="metric-label">Evaluations</div>
                <div className="metric-value">{evaluations}</div>
                <div className="metric-sub">control evaluations across all runs</div>
              </div>
              <div className="metric-card">
                <div className="metric-label">Never triggered</div>
                <div className="metric-value" style={{ color: uncovered.length > 0 ? 'var(--error)' : 'var(--success)' }}>
                  {uncovered.length}
                </div>
                {uncovered.length > 0 && (
                  <div className="metric-sub" style={{ color: 'var(--error)' }}>need attention</div>
                )}
              </div>
              <div className="metric-card">
                <div className="metric-label">Scenario values</div>
                <div className="metric-value">{scenarios.length}</div>
                <div className="metric-sub">genome dimension values exercised</div>
              </div>
            </div>

            {/* Bar chart */}
            {chartData.length > 0 && (
              <div className="card" style={{ marginBottom: 24 }}>
                <div className="card-header"><span className="card-title">Trigger Frequency by Control</span></div>
                <div className="card-body">
                  <ResponsiveContainer width="100%" height={260}>
                    <BarChart data={chartData} barSize={24} margin={{ top: 8, right: 8, bottom: 8, left: 0 }}>
                      <XAxis
                        dataKey="name"
                        tick={{ fill: 'var(--text-secondary)', fontSize: 11 }}
                        axisLine={false}
                        tickLine={false}
                        interval={0}
                        angle={-28}
                        textAnchor="end"
                        height={64}
                        tickFormatter={(value: string) => value.replace(/_v\d+$/, '')}
                      />
                      <YAxis tick={{ fill: 'var(--text-secondary)', fontSize: 11 }} axisLine={false} tickLine={false} />
                      <Tooltip
                        contentStyle={{ background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 6, fontSize: 12 }}
                        formatter={(v: number, n: string) => [v, n === 'count' ? 'Times triggered' : n]}
                      />
                      <Bar dataKey="count" radius={[3, 3, 0, 0]}>
                        {chartData.map(d => (
                          <Cell key={d.name} fill={d.count === 0 ? 'var(--error)' : 'var(--accent)'} />
                        ))}
                      </Bar>
                    </BarChart>
                  </ResponsiveContainer>
                </div>
              </div>
            )}

            {/* Control + outcome maps */}
            <div className="card" style={{ marginBottom: 24 }}>
              <div className="card-header">
                <span className="card-title">Control &amp; Outcome Map</span>
                {uncovered.length > 0 && (
                  <span className="badge badge-failed">{uncovered.length} never triggered</span>
                )}
              </div>
              <div className="table-wrap">
                <table aria-label="Coverage rule table">
                  <thead>
                    <tr>
                      <th scope="col">Control</th>
                      <th scope="col">Triggered</th>
                      <th scope="col">Not triggered</th>
                      <th scope="col">Not applicable</th>
                      <th scope="col">Inconclusive</th>
                      <th scope="col">Failed</th>
                      <th scope="col">Weight ↓</th>
                      <th scope="col">Last evaluated</th>
                    </tr>
                  </thead>
                  <tbody>
                    {[...rules]
                      .sort((a, b) => b.coverage_weight - a.coverage_weight)
                      .map(r => (
                        <tr
                          key={r.rule_id}
                          className={r.times_triggered === 0 ? 'alert-row-uncovered' : ''}
                          title={r.times_triggered === 0 ? 'This control has never triggered — high priority' : undefined}
                        >
                          <td className="td-mono">{r.rule_id}</td>
                          <td style={{ fontWeight: r.times_triggered > 0 ? 600 : 400 }}>
                            {r.times_triggered === 0
                              ? <span style={{ color: 'var(--error)' }}>0</span>
                              : r.times_triggered}
                          </td>
                          <td className="td-muted">{r.times_not_triggered}</td>
                          <td className="td-muted">{r.times_not_applicable}</td>
                          <td className="td-muted">{r.times_inconclusive}</td>
                          <td style={{ color: r.times_failed > 0 ? 'var(--error)' : undefined, fontWeight: r.times_failed > 0 ? 600 : 400 }}>
                            {r.times_failed}
                          </td>
                          <td>
                            <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                              <div style={{
                                width: 56, height: 6, borderRadius: 3,
                                background: 'var(--bg-overlay)',
                                overflow: 'hidden',
                              }}>
                                <div style={{
                                  width: `${Math.min(r.coverage_weight * 100, 100)}%`,
                                  height: '100%',
                                  background: r.times_triggered === 0 ? 'var(--error)' : 'var(--accent)',
                                  borderRadius: 3,
                                }} />
                              </div>
                              {r.coverage_weight.toFixed(3)}
                            </div>
                          </td>
                          <td className="td-muted">
                            {r.last_evaluated_at
                              ? new Date(r.last_evaluated_at).toLocaleDateString()
                              : <span style={{ color: 'var(--text-secondary)' }}>Never</span>}
                          </td>
                        </tr>
                      ))}
                  </tbody>
                </table>
              </div>
              <div className="card-body" style={{ paddingTop: 0 }}>
                <p className="td-muted" style={{ fontSize: 12, margin: 0 }}>
                  Statuses are exclusive. A failed control is never counted as a non-detection.
                </p>
              </div>
            </div>

            {/* Scenario map */}
            <div className="card">
              <div className="card-header">
                <span className="card-title">Scenario Coverage Map</span>
                <span className="badge">{scenarioByDimension.size} dimensions</span>
              </div>
              <div className="card-body">
                {scenarioByDimension.size === 0 ? (
                  <p className="td-muted" style={{ margin: 0 }}>
                    No scenario coverage yet. Link an experiment to an approved scenario proposal
                    to record the genome dimensions it exercises.
                  </p>
                ) : (
                  <div style={{ display: 'grid', gap: 14 }}>
                    {[...scenarioByDimension.entries()].map(([dimension, values]) => (
                      <div key={dimension}>
                        <div className="metric-label" style={{ marginBottom: 6 }}>{dimension}</div>
                        <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
                          {values
                            .sort((a, b) => b.times - a.times)
                            .map(v => (
                              <span
                                key={v.value}
                                className="badge"
                                title={`${v.value} exercised ${v.times} time(s)`}
                              >
                                {v.value} <span className="td-muted">×{v.times}</span>
                              </span>
                            ))}
                        </div>
                      </div>
                    ))}
                  </div>
                )}
              </div>
            </div>

            {/* Outcome status labels for screen readers / clarity */}
            <p className="td-muted" style={{ fontSize: 12, marginTop: 16 }}>
              Outcome states: {Object.values(outcomeLabels).join(' · ')}.
            </p>
          </>
        )}
      </div>
    </>
  )
}
