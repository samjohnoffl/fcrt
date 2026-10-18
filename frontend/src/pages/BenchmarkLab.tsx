import { useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { BarChart, Bar, XAxis, YAxis, Tooltip, ResponsiveContainer, Legend } from 'recharts'
import { PageHeader } from '../components/Layout'
import { RoleGate } from '../components/RoleGate'
import { LoadingState, ErrorState, EmptyState } from '../components/StateViews'
import { campaignApi } from '../api/endpoints'
import type { BenchmarkResponse, StrategySummary } from '../api/types'

const STRATEGY_COLORS: Record<string, string> = {
  random: 'var(--text-secondary)',
  gemini_only: 'var(--warning, #d29922)',
  adaptive: 'var(--accent)',
}

export function BenchmarkLab() {
  const [candidates, setCandidates] = useState(120)
  const [campaignId, setCampaignId] = useState('')

  const createAndRun = useMutation({
    mutationFn: async (): Promise<BenchmarkResponse> => {
      let id = campaignId
      if (!id) {
        const campaign = await campaignApi.create({
          name: `Benchmark ${new Date().toISOString().slice(0, 19)}`,
          description: 'Ad-hoc benchmark lab run',
          budget: 500,
        })
        id = campaign.id
        setCampaignId(id)
      }
      return campaignApi.benchmark(id, candidates)
    },
  })

  const result = createAndRun.data?.benchmark
  const strategies = result?.strategies ?? []

  const chartData = strategies.map((s: StrategySummary) => ({
    name: s.name.replace('_', '-'),
    coverage: +(s.coverage_gain * 100).toFixed(1),
    valid: +(s.valid_candidate_yield * 100).toFixed(1),
    novelty: +(s.novelty_score * 100).toFixed(1),
  }))

  return (
    <>
      <PageHeader
        title="Benchmark Lab"
        subtitle="Compare Random vs Gemini-only vs Adaptive candidate selection"
      />
      <div className="page-body">
        {/* Controls */}
        <div className="card" style={{ marginBottom: 24 }}>
          <div className="card-header"><span className="card-title">Run Configuration</span></div>
          <div className="card-body">
            <div className="form-row" style={{ alignItems: 'flex-end' }}>
              <div className="form-group">
                <label className="form-label" htmlFor="candidates">Candidates per strategy</label>
                <input
                  id="candidates" className="form-input" type="number"
                  min={10} max={10000}
                  value={candidates}
                  onChange={e => setCandidates(Number(e.target.value) || 100)}
                />
                <span className="form-hint">Higher = more stable comparison, slower run.</span>
              </div>
              <div className="form-group">
                <label className="form-label" htmlFor="campaign">Campaign ID (optional)</label>
                <input
                  id="campaign" className="form-input" type="text"
                  value={campaignId}
                  placeholder="auto-create if empty"
                  onChange={e => setCampaignId(e.target.value)}
                />
                <span className="form-hint">Blank creates a fresh campaign for this run.</span>
              </div>
              <div className="form-group">
                <RoleGate min="analyst">
                  <button
                    className="btn btn-primary"
                    onClick={() => createAndRun.mutate()}
                    disabled={createAndRun.isPending}
                  >
                    {createAndRun.isPending ? 'Running…' : 'Run Benchmark'}
                  </button>
                </RoleGate>
              </div>
            </div>
          </div>
        </div>

        {/* Results */}
        {createAndRun.isPending ? <LoadingState title="Running all three strategies…" /> :
         createAndRun.error ? <ErrorState title="Benchmark failed" body={String(createAndRun.error)} /> :
         !result ? (
           <EmptyState
             title="No benchmark results yet"
             body="Configure the run above and click “Run Benchmark” to compare strategies."
           />
         ) : (
           <>
             {/* Winner banner */}
             <div className="metric-grid" style={{ marginBottom: 24 }}>
               <div className="metric-card">
                 <div className="metric-label">Winner</div>
                 <div className="metric-value" style={{ color: 'var(--accent)', textTransform: 'capitalize' }}>
                   {result.winner.replace('_', '-')}
                 </div>
                 <div className="metric-sub">by coverage gain</div>
               </div>
               <div className="metric-card">
                 <div className="metric-label">Candidates</div>
                 <div className="metric-value">{result.num_candidates}</div>
                 <div className="metric-sub">per strategy</div>
               </div>
               <div className="metric-card">
                 <div className="metric-label">Strategies</div>
                 <div className="metric-value">{strategies.length}</div>
                 <div className="metric-sub">compared</div>
               </div>
             </div>

             {/* Comparison chart */}
             <div className="card" style={{ marginBottom: 24 }}>
               <div className="card-header"><span className="card-title">Strategy Comparison (% scale)</span></div>
               <div className="card-body">
                 <ResponsiveContainer width="100%" height={260}>
                   <BarChart data={chartData} barSize={22} margin={{ top: 0, right: 8, bottom: 0, left: 0 }}>
                     <XAxis dataKey="name" tick={{ fill: 'var(--text-secondary)', fontSize: 11 }} axisLine={false} tickLine={false} />
                     <YAxis tick={{ fill: 'var(--text-secondary)', fontSize: 11 }} axisLine={false} tickLine={false} />
                     <Tooltip contentStyle={{ background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 6, fontSize: 12 }} />
                     <Legend wrapperStyle={{ fontSize: 12 }} />
                     <Bar dataKey="coverage" name="Coverage gain" fill="var(--accent)" radius={[3, 3, 0, 0]} />
                     <Bar dataKey="valid" name="Valid yield" fill="var(--success)" radius={[3, 3, 0, 0]} />
                     <Bar dataKey="novelty" name="Novelty" fill="var(--text-secondary)" radius={[3, 3, 0, 0]} />
                   </BarChart>
                 </ResponsiveContainer>
               </div>
             </div>

             {/* Detail table */}
             <div className="card">
               <div className="card-header">
                 <span className="card-title">Metrics Detail</span>
                 {result.notes && <span className="badge">{result.notes}</span>}
               </div>
               <div className="table-wrap">
                 <table aria-label="Benchmark strategy comparison">
                   <thead>
                     <tr>
                       <th scope="col">Strategy</th>
                       <th scope="col">Valid Yield</th>
                       <th scope="col">Approved Rate</th>
                       <th scope="col">Coverage Gain</th>
                       <th scope="col">Novelty</th>
                       <th scope="col">Avg Score</th>
                       <th scope="col">Cost / Finding</th>
                       <th scope="col">Runtime</th>
                     </tr>
                   </thead>
                   <tbody>
                     {strategies.map((s: StrategySummary) => (
                       <tr key={s.name} className={s.name === result.winner ? 'row-winner' : ''}>
                         <td>
                           <span style={{ color: STRATEGY_COLORS[s.name] ?? 'inherit', fontWeight: 600 }}>
                             {s.name.replace('_', '-')}
                           </span>
                           {s.name === result.winner && <span className="badge badge-approved" style={{ marginLeft: 8 }}>winner</span>}
                         </td>
                         <td>{(s.valid_candidate_yield * 100).toFixed(1)}%</td>
                         <td>{(s.approved_rate * 100).toFixed(1)}%</td>
                         <td>{(s.coverage_gain * 100).toFixed(1)}%</td>
                         <td>{(s.novelty_score * 100).toFixed(1)}%</td>
                         <td>{s.average_adaptive_score != null ? s.average_adaptive_score.toFixed(3) : '—'}</td>
                         <td>{s.cost_per_finding.toFixed(1)}</td>
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
