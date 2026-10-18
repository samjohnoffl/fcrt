import { useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { BarChart, Bar, XAxis, YAxis, Tooltip, ResponsiveContainer, Legend } from 'recharts'
import { PageHeader } from '../components/Layout'
import { RoleGate } from '../components/RoleGate'
import { LoadingState, ErrorState, EmptyState } from '../components/StateViews'
import { cohortApi, discoveryApi } from '../api/endpoints'
import type { BenchmarkStrategy, ComparisonResult } from '../api/types'

const STRATEGIES: BenchmarkStrategy[] = ['random', 'gemini_only', 'adaptive']

const STRATEGY_LABEL: Record<BenchmarkStrategy, string> = {
  random: 'Random',
  gemini_only: 'Gemini-only',
  adaptive: 'Adaptive',
}

const STRATEGY_COLORS: Record<string, string> = {
  random: 'var(--text-secondary)',
  gemini_only: 'var(--warning)',
  adaptive: 'var(--accent)',
}

function pct(v: number): string {
  return `${(v * 100).toFixed(1)}%`
}

export function BenchmarkLab() {
  const [budget, setBudget] = useState(5)
  const [cohortId, setCohortId] = useState('')

  const cohorts = useQuery({ queryKey: ['cohorts'], queryFn: cohortApi.list })
  const effectiveCohort = cohortId || cohorts.data?.[0]?.id || ''

  const runAll = useMutation({
    mutationFn: async (): Promise<ComparisonResult> => {
      if (!effectiveCohort) throw new Error('Create a cohort first.')
      for (const strategy of STRATEGIES) {
        await discoveryApi.run(effectiveCohort, { strategy, budget })
      }
      return discoveryApi.benchmark(effectiveCohort)
    },
  })

  const result = runAll.data
  const strategies = result?.strategies ?? []
  const winner = result?.winner

  const chartData = strategies.map(s => ({
    name: STRATEGY_LABEL[s.strategy],
    coverage: +(s.expected_coverage_gain * 100).toFixed(1),
    novelty: +(s.mean_novelty * 100).toFixed(1),
    information: +(s.mean_information_gain * 100).toFixed(1),
  }))

  return (
    <>
      <PageHeader
        title="Benchmark Lab"
        subtitle="Compare stored Adaptive, Random and Gemini-only selection runs"
      />
      <div className="page-body">
        <div className="card" style={{ marginBottom: 24 }}>
          <div className="card-header"><span className="card-title">Run configuration</span></div>
          <div className="card-body">
            <div className="form-row" style={{ alignItems: 'flex-end' }}>
              <div className="form-group">
                <label className="form-label" htmlFor="cohort">Cohort</label>
                <select
                  id="cohort" className="form-select"
                  value={effectiveCohort}
                  onChange={e => setCohortId(e.target.value)}
                  disabled={!cohorts.data?.length}
                >
                  {(cohorts.data ?? []).map(c => (
                    <option key={c.id} value={c.id}>{c.name}</option>
                  ))}
                </select>
                <span className="form-hint">
                  {cohorts.data?.length
                    ? 'All three strategies run against this cohort with the same budget.'
                    : 'Create a cohort first.'}
                </span>
              </div>
              <div className="form-group">
                <label className="form-label" htmlFor="budget">Candidates per strategy</label>
                <input
                  id="budget" className="form-input" type="number"
                  min={1} max={50}
                  value={budget}
                  onChange={e => setBudget(Number(e.target.value) || 5)}
                />
                <span className="form-hint">A conclusion needs at least 5 selected candidates per strategy.</span>
              </div>
              <div className="form-group">
                <RoleGate min="analyst">
                  <button
                    className="btn btn-primary"
                    onClick={() => runAll.mutate()}
                    disabled={runAll.isPending || !effectiveCohort}
                  >
                    {runAll.isPending ? 'Running…' : 'Run all three strategies'}
                  </button>
                </RoleGate>
              </div>
            </div>
          </div>
        </div>

        {runAll.isPending ? <LoadingState title="Running Random, Gemini-only and Adaptive…" /> :
         runAll.error ? <ErrorState title="Benchmark failed" body={String(runAll.error)} /> :
         !result ? (
           <EmptyState
             title="No stored comparison yet"
             body="Run the three strategies to create a benchmark from persisted selection traces."
           />
         ) : (
           <>
             {result.insufficient_data ? (
               <div className="alert alert-warn" role="status">
                 <strong>No ranking conclusion.</strong> {result.notes}
                 {result.missing_strategies?.length ? ` Missing: ${result.missing_strategies.join(', ')}.` : ''}
               </div>
             ) : (
               <div className="alert alert-info" role="status">
                 <strong>Winner: {winner ? STRATEGY_LABEL[winner] : '—'}.</strong> {result.notes}
               </div>
             )}

             <div className="metric-grid" style={{ marginBottom: 24 }}>
               <div className="metric-card">
                 <div className="metric-label">Winner</div>
                 <div className="metric-value" style={{ color: winner ? 'var(--accent)' : 'var(--text-secondary)' }}>
                   {winner ? STRATEGY_LABEL[winner] : 'No conclusion'}
                 </div>
                 <div className="metric-sub">
                   {result.insufficient_data ? 'insufficient evidence' : 'by expected coverage gain'}
                 </div>
               </div>
               <div className="metric-card">
                 <div className="metric-label">Strategies</div>
                 <div className="metric-value">{strategies.length} / 3</div>
                 <div className="metric-sub">stored runs compared</div>
               </div>
               <div className="metric-card">
                 <div className="metric-label">Budget</div>
                 <div className="metric-value">{strategies[0]?.budget ?? budget}</div>
                 <div className="metric-sub">candidates per run</div>
               </div>
             </div>

             {chartData.length > 0 && (
               <div className="card" style={{ marginBottom: 24 }}>
                 <div className="card-header"><span className="card-title">Score components (% scale)</span></div>
                 <div className="card-body">
                   <ResponsiveContainer width="100%" height={260}>
                     <BarChart data={chartData} barSize={22} margin={{ top: 0, right: 8, bottom: 0, left: 0 }}>
                       <XAxis dataKey="name" tick={{ fill: 'var(--text-secondary)', fontSize: 11 }} axisLine={false} tickLine={false} />
                       <YAxis tick={{ fill: 'var(--text-secondary)', fontSize: 11 }} axisLine={false} tickLine={false} />
                       <Tooltip contentStyle={{ background: 'var(--bg-surface)', border: '1px solid var(--border)', borderRadius: 6, fontSize: 12 }} />
                       <Legend wrapperStyle={{ fontSize: 12 }} />
                       <Bar dataKey="coverage" name="Expected coverage" fill="var(--accent)" radius={[3, 3, 0, 0]} />
                       <Bar dataKey="novelty" name="Novelty" fill="var(--success)" radius={[3, 3, 0, 0]} />
                       <Bar dataKey="information" name="Information" fill="var(--text-muted)" radius={[3, 3, 0, 0]} />
                     </BarChart>
                   </ResponsiveContainer>
                 </div>
               </div>
             )}

             <div className="card">
               <div className="card-header">
                 <span className="card-title">Metrics from stored runs</span>
               </div>
               <div className="table-wrap">
                 <table aria-label="Benchmark strategy comparison">
                   <thead>
                     <tr>
                       <th scope="col">Strategy</th>
                       <th scope="col">Pool</th>
                       <th scope="col">Selected</th>
                       <th scope="col">Valid yield</th>
                       <th scope="col">Approved</th>
                       <th scope="col">Coverage</th>
                       <th scope="col">Novelty</th>
                       <th scope="col">Info gain</th>
                       <th scope="col">Mean score</th>
                       <th scope="col">Runtime</th>
                     </tr>
                   </thead>
                   <tbody>
                     {strategies.map(s => (
                       <tr key={s.strategy} className={s.strategy === winner ? 'row-winner' : ''}>
                         <td>
                           <span style={{ color: STRATEGY_COLORS[s.strategy], fontWeight: 600 }}>
                             {STRATEGY_LABEL[s.strategy]}
                           </span>
                           {s.strategy === winner && (
                             <span className="badge badge-approved" style={{ marginLeft: 8 }}>winner</span>
                           )}
                         </td>
                         <td>{s.pool_size}</td>
                         <td>{s.selected_candidates}</td>
                         <td>{pct(s.valid_candidate_yield)}</td>
                         <td>{s.approved_candidates} ({pct(s.approved_rate)})</td>
                         <td>{pct(s.expected_coverage_gain)}</td>
                         <td>{pct(s.mean_novelty)}</td>
                         <td>{pct(s.mean_information_gain)}</td>
                         <td>{s.mean_adaptive_score.toFixed(3)}</td>
                         <td className="td-muted">{s.runtime_ms} ms</td>
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
