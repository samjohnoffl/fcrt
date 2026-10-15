import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { PageHeader } from '../components/Layout'
import { StatusBadge } from '../components/StatusBadge'
import { LoadingState, ErrorState, EmptyState } from '../components/StateViews'
import { cohortApi } from '../api/endpoints'

export function CohortList() {
  const nav = useNavigate()
  const [filter, setFilter] = useState<'all' | 'locked' | 'unlocked'>('all')
  const { data, isLoading, error } = useQuery({ queryKey: ['cohorts'], queryFn: cohortApi.list })

  const filtered = (data ?? []).filter(c => {
    if (filter === 'locked') return !!c.locked_at
    if (filter === 'unlocked') return !c.locked_at
    return true
  })

  return (
    <>
      <PageHeader
        title="Cohorts"
        subtitle="Versioned synthetic banking datasets"
        actions={<button className="btn btn-primary btn-sm" onClick={() => nav('/cohorts/new')}>+ New Cohort</button>}
      />
      <div className="page-body">
        <div className="filter-bar" style={{ marginBottom: 16 }}>
          <span className="text-muted" style={{ fontSize: 12 }}>Filter:</span>
          {(['all', 'locked', 'unlocked'] as const).map(f => (
            <button
              key={f}
              className={`btn btn-sm ${filter === f ? 'btn-primary' : 'btn-ghost'}`}
              onClick={() => setFilter(f)}
            >
              {f.charAt(0).toUpperCase() + f.slice(1)}
            </button>
          ))}
          {data && <span className="text-muted" style={{ fontSize: 12, marginLeft: 'auto' }}>{filtered.length} cohort{filtered.length !== 1 ? 's' : ''}</span>}
        </div>
        <div className="card">
          {isLoading ? (
            <LoadingState title="Loading cohorts…" />
          ) : error ? (
            <ErrorState title="Failed to load cohorts" body={String(error)} />
          ) : filtered.length === 0 ? (
            <EmptyState
              title="No cohorts found"
              body={filter !== 'all' ? `No ${filter} cohorts.` : 'Create your first cohort to get started.'}
              action={filter === 'all' ? <button className="btn btn-primary btn-sm" onClick={() => nav('/cohorts/new')}>+ New Cohort</button> : undefined}
            />
          ) : (
            <div className="table-wrap">
              <table aria-label="Cohorts">
                <thead>
                  <tr>
                    <th scope="col">Name</th>
                    <th scope="col">Seed</th>
                    <th scope="col">Entities</th>
                    <th scope="col">Transactions</th>
                    <th scope="col">Lock Status</th>
                    <th scope="col">Created</th>
                  </tr>
                </thead>
                <tbody>
                  {filtered.map(c => (
                    <tr key={c.id} style={{ cursor: 'pointer' }} onClick={() => nav(`/cohorts/${c.id}`)}>
                      <td style={{ fontWeight: 500 }}>{c.name}</td>
                      <td className="td-mono">{c.seed}</td>
                      <td>{c.manifest?.entity_count ?? '—'}</td>
                      <td>{c.manifest?.tx_count ?? '—'}</td>
                      <td><StatusBadge status={c.locked_at ? 'locked' : 'unlocked'} /></td>
                      <td className="td-muted">{new Date(c.created_at).toLocaleDateString()}</td>
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
