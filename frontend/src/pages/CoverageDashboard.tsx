import { useQuery } from '@tanstack/react-query'
import { BarChart, Bar, XAxis, YAxis, Tooltip, ResponsiveContainer, Cell } from 'recharts'
import { PageHeader } from '../components/Layout'
import { LoadingState, ErrorState, EmptyState } from '../components/StateViews'
import { coverageApi } from '../api/endpoints'

export function CoverageDashboard() {
  const { data, isLoading, error } = useQuery({ queryKey: ['coverage'], queryFn: coverageApi.get })

  const rules = data?.rules ?? []
  const uncovered = rules.filter(r => r.times_triggered === 0)

  const chartData = [...rules]
    .sort((a, b) => b.times_triggered - a.times_triggered)
    .map(r => ({ name: r.rule_id, count: r.times_triggered, weight: r.coverage_weight }))

  return (
    <>
      <PageHeader
        title="Coverage Dashboard"
        subtitle="AML rule exercise frequency across all experiments"
      />
      <div className="page-body">
        {isLoading ? <LoadingState title="Loading coverage…" /> :
         error ? <ErrorState title="Failed to load coverage" body={String(error)} /> :
         !data ? <EmptyState title="No coverage data" body="Run experiments to generate coverage data." /> : (
          <>
            {/* Summary cards */}
            <div className="metric-grid" style={{ marginBottom: 24 }}>
              <div className="metric-card">
                <div className="metric-label">Coverage</div>
                <div className="metric-value" style={{ color: 'var(--success)' }}>
                  {data.coverage_pct.toFixed(1)}%
                </div>
                <div className="metric-sub">{data.covered_rules} of {data.total_rules} rules</div>
              </div>
              <div className="metric-card">
                <div className="metric-label">Total Rules</div>
                <div className="metric-value">{data.total_rules}</div>
              </div>
              <div className="metric-card">
                <div className="metric-label">Covered</div>
                <div className="metric-value" style={{ color: 'var(--success)' }}>{data.covered_rules}</div>
              </div>
              <div className="metric-card">
                <div className="metric-label">Uncovered</div>
                <div className="metric-value" style={{ color: uncovered.length > 0 ? 'var(--error)' : 'var(--success)' }}>
                  {uncovered.length}
                </div>
                {uncovered.length > 0 && (
                  <div className="metric-sub" style={{ color: 'var(--error)' }}>need attention</div>
                )}
              </div>
            </div>

            {/* Bar chart */}
            {chartData.length > 0 && (
              <div className="card" style={{ marginBottom: 24 }}>
                <div className="card-header"><span className="card-title">Rule Hit Frequency</span></div>
                <div className="card-body">
                  <ResponsiveContainer width="100%" height={220}>
                    <BarChart data={chartData} barSize={28} margin={{ top: 0, right: 8, bottom: 0, left: 0 }}>
                      <XAxis dataKey="name" tick={{ fill: 'var(--text-secondary)', fontSize: 11 }} axisLine={false} tickLine={false} />
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

            {/* Sensitivity map table */}
            <div className="card">
              <div className="card-header">
                <span className="card-title">Sensitivity Map</span>
                {uncovered.length > 0 && (
                  <span className="badge badge-failed">{uncovered.length} uncovered</span>
                )}
              </div>
              <div className="table-wrap">
                <table aria-label="Coverage rule table">
                  <thead>
                    <tr>
                      <th scope="col">Rule ID</th>
                      <th scope="col">Rule Version</th>
                      <th scope="col">Times Triggered</th>
                      <th scope="col">Coverage Weight ↓</th>
                      <th scope="col">Last Triggered</th>
                    </tr>
                  </thead>
                  <tbody>
                    {[...rules]
                      .sort((a, b) => b.coverage_weight - a.coverage_weight)
                      .map(r => (
                        <tr
                          key={r.rule_id}
                          className={r.times_triggered === 0 ? 'alert-row-uncovered' : ''}
                          title={r.times_triggered === 0 ? 'This rule has never been triggered — high priority' : undefined}
                        >
                          <td className="td-mono">{r.rule_id}</td>
                          <td className="td-mono td-muted">{r.rule_version}</td>
                          <td>
                            {r.times_triggered === 0
                              ? <span style={{ color: 'var(--error)', fontWeight: 600 }}>0 uncovered</span>
                              : r.times_triggered}
                          </td>
                          <td>
                            <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                              <div style={{
                                width: 60, height: 6, borderRadius: 3,
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
                            {r.last_triggered_at ? new Date(r.last_triggered_at).toLocaleDateString() : <span style={{ color: 'var(--error)' }}>Never</span>}
                          </td>
                        </tr>
                      ))}
                  </tbody>
                </table>
              </div>
            </div>
          </>
        )}
      </div>
    </>
  )
}
