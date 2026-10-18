import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { PageHeader } from '../components/Layout'
import { RoleGate } from '../components/RoleGate'
import { StatusBadge, DeltaBadge } from '../components/StatusBadge'
import { LoadingState, ErrorState, EmptyState } from '../components/StateViews'
import { experimentApi, regressionApi } from '../api/endpoints'

export function ExperimentList() {
  const nav = useNavigate()
  const [filter, setFilter] = useState<'all' | 'pending' | 'running' | 'completed' | 'failed'>('all')
  const [packBusy, setPackBusy] = useState(false)
  const { data, isLoading, error } = useQuery({ queryKey: ['experiments'], queryFn: experimentApi.list })

  const downloadPack = async () => {
    setPackBusy(true)
    try {
      await regressionApi.download()
    } catch (e) {
      console.error('regression pack download failed', e)
    } finally {
      setPackBusy(false)
    }
  }

  const filtered = (data ?? [])
    .filter(e => filter === 'all' || e.status === filter)
    .sort((a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime())

  return (
    <>
      <PageHeader
        title="Experiments"
        subtitle="Baseline vs. variant AML comparisons"
        actions={
          <>
            <button className="btn btn-ghost btn-sm" onClick={downloadPack} disabled={packBusy} title="Download a reproducible Regression Test Pack (JSON)">
              {packBusy ? 'Preparing…' : 'Download regression pack'}
            </button>
            <RoleGate min="analyst"><button className="btn btn-primary btn-sm" onClick={() => nav('/experiments/new')}>New experiment</button></RoleGate>
          </>
        }
      />
      <div className="page-body">
        <div className="filter-bar" style={{ marginBottom: 16 }}>
          <span className="text-muted" style={{ fontSize: 12 }}>Status:</span>
          {(['all', 'pending', 'running', 'completed', 'failed'] as const).map(f => (
            <button key={f} className={`btn btn-sm ${filter === f ? 'btn-primary' : 'btn-ghost'}`} onClick={() => setFilter(f)}>
              {f.charAt(0).toUpperCase() + f.slice(1)}
            </button>
          ))}
          {data && <span className="text-muted" style={{ fontSize: 12, marginLeft: 'auto' }}>{filtered.length} result{filtered.length !== 1 ? 's' : ''}</span>}
        </div>
        <div className="card">
          {isLoading ? <LoadingState title="Loading experiments…" /> :
           error ? <ErrorState title="Failed to load experiments" body={String(error)} /> :
           filtered.length === 0 ? (
            <EmptyState
              title="No experiments"
              body={filter !== 'all' ? `No ${filter} experiments.` : 'Run your first experiment to compare baseline vs. variant AML results.'}
              action={filter === 'all' ? <RoleGate min="analyst"><button className="btn btn-primary btn-sm" onClick={() => nav('/experiments/new')}>New experiment</button></RoleGate> : undefined}
            />
          ) : (
            <div className="table-wrap">
              <table aria-label="Experiments">
                <thead>
                  <tr>
                    <th scope="col">Name</th>
                    <th scope="col">Mutation</th>
                    <th scope="col">Status</th>
                    <th scope="col">Evasion Δ</th>
                    <th scope="col">Rule Version</th>
                    <th scope="col">Created</th>
                    <th scope="col">Completed</th>
                  </tr>
                </thead>
                <tbody>
                  {filtered.map(e => (
                    <tr key={e.id} style={{ cursor: 'pointer' }} onClick={() => nav(`/experiments/${e.id}`)}>
                      <td style={{ fontWeight: 500, maxWidth: 240 }} className="truncate">{e.name}</td>
                      <td><span className="tag">{e.mutation_type}</span></td>
                      <td><StatusBadge status={e.status} /></td>
                      <td>
                        {e.status === 'completed'
                          ? <DeltaBadge delta={e.evasion_delta ?? 0} />
                          : <span className="text-muted">—</span>}
                      </td>
                      <td className="td-mono td-muted">{e.control_hash?.slice(0, 8) ?? '—'}</td>
                      <td className="td-muted">{new Date(e.created_at).toLocaleDateString()}</td>
                      <td className="td-muted">{e.completed_at ? new Date(e.completed_at).toLocaleDateString() : '—'}</td>
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
