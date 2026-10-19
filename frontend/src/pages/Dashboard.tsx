// Dashboard — coverage gauge + recent experiments + quick actions
import { useQuery } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { PieChart, Pie, Cell, Tooltip, ResponsiveContainer } from 'recharts'
import { PageHeader } from '../components/Layout'
import { RoleGate } from '../components/RoleGate'
import { LoadingState, ErrorState, EmptyState } from '../components/StateViews'
import { StatusBadge, DeltaBadge } from '../components/StatusBadge'
import { coverageApi } from '../api/endpoints'
import { experimentApi } from '../api/endpoints'
import type { ExperimentPair } from '../api/types'

function CoverageRing({ pct }: { pct: number }) {
  const data = [
    { name: 'Covered', value: pct },
    { name: 'Uncovered', value: 100 - pct },
  ]
  return (
    <div className="coverage-ring-wrap">
      <ResponsiveContainer width={120} height={120}>
        <PieChart>
          <Pie
            data={data}
            cx="50%" cy="50%"
            innerRadius={38} outerRadius={54}
            startAngle={90} endAngle={-270}
            dataKey="value"
            strokeWidth={0}
          >
            <Cell fill="var(--success)" />
            <Cell fill="var(--bg-overlay)" />
          </Pie>
          <Tooltip
            contentStyle={{ background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 6 }}
            formatter={(v: number) => [`${v.toFixed(1)}%`]}
          />
        </PieChart>
      </ResponsiveContainer>
      <div className="ring-label">
        <div className="ring-pct" style={{ color: 'var(--success)' }}>{pct.toFixed(1)}%</div>
        <div className="ring-sub">AML rule coverage</div>
      </div>
    </div>
  )
}

export function Dashboard() {
  const nav = useNavigate()
  const cov = useQuery({ queryKey: ['coverage'], queryFn: coverageApi.get })
  const exp = useQuery({ queryKey: ['experiments'], queryFn: experimentApi.list })

  const recent: ExperimentPair[] = (exp.data ?? [])
    .slice()
    .sort((a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime())
    .slice(0, 10)

  return (
    <>
      <PageHeader
        title="Dashboard"
        subtitle="AML Red Team overview"
        actions={
          <RoleGate min="analyst">
            <button className="btn btn-secondary btn-sm" onClick={() => nav('/cohorts/new')}>+ New cohort</button>
            <button className="btn btn-secondary btn-sm" onClick={() => nav('/scenarios')}>Request scenario</button>
            <button className="btn btn-primary btn-sm" onClick={() => nav('/experiments/new')}>Run experiment</button>
          </RoleGate>
        }
      />
      <div className="page-body">
        {/* Metrics row */}
        <div className="metric-grid" style={{ marginBottom: 24 }}>
          <div className="metric-card">
            <div className="metric-label">Total Experiments</div>
            <div className="metric-value">{exp.data?.length ?? '—'}</div>
            <div className="metric-sub">all time</div>
          </div>
          <div className="metric-card">
            <div className="metric-label">Completed</div>
            <div className="metric-value">
              {exp.data?.filter(e => e.status === 'completed').length ?? '—'}
            </div>
            <div className="metric-sub">successful runs</div>
          </div>
          <div className="metric-card">
            <div className="metric-label">Covered Rules</div>
            <div className="metric-value" style={{ color: 'var(--success)' }}>
              {cov.data ? `${cov.data.covered_rules}/${cov.data.total_rules}` : '—'}
            </div>
            <div className="metric-sub">AML rules triggered</div>
          </div>
          <div className="metric-card" style={{ display: 'flex', alignItems: 'center' }}>
            {cov.isLoading ? (
              <LoadingState title="Loading coverage…" />
            ) : cov.error ? (
              <span className="text-muted">Coverage unavailable</span>
            ) : cov.data ? (
              <CoverageRing pct={cov.data.coverage_pct} />
            ) : null}
          </div>
        </div>

        {/* Recent experiments */}
        <div className="card">
          <div className="card-header">
            <span className="card-title">Recent experiments</span>
            <button className="btn btn-ghost btn-sm" onClick={() => nav('/experiments')}>View all →</button>
          </div>
          {exp.isLoading ? (
            <LoadingState title="Loading experiments…" />
          ) : exp.error ? (
            <ErrorState title="Failed to load experiments" body={String(exp.error)} />
          ) : recent.length === 0 ? (
            <EmptyState
              title="No experiments yet"
              body="Run your first experiment to see results here."
              action={<RoleGate min="analyst"><button className="btn btn-primary btn-sm" onClick={() => nav('/experiments/new')}>Run experiment</button></RoleGate>}
            />
          ) : (
            <div className="table-wrap">
              <table aria-label="Recent experiments">
                <thead>
                  <tr>
                    <th scope="col">Name</th>
                    <th scope="col">Status</th>
                    <th scope="col">Evasion Δ</th>
                    <th scope="col">Mutation</th>
                    <th scope="col">Created</th>
                  </tr>
                </thead>
                <tbody>
                  {recent.map(e => (
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
                      <td><StatusBadge status={e.status} /></td>
                      <td>
                        {e.status === 'completed'
                          ? <DeltaBadge delta={e.evasion_delta ?? 0} />
                          : <span className="text-muted">—</span>
                        }
                      </td>
                      <td><span className="tag">{e.mutation_type}</span></td>
                      <td className="td-muted">{new Date(e.created_at).toLocaleDateString()}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      </div>
    </>
  )
}
