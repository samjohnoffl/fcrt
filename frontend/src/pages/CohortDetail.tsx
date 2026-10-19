import { useState } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { BarChart, Bar, XAxis, YAxis, Tooltip, ResponsiveContainer } from 'recharts'
import { PageHeader } from '../components/Layout'
import { RoleGate } from '../components/RoleGate'
import { StatusBadge } from '../components/StatusBadge'
import { LoadingState, ErrorState } from '../components/StateViews'
import { ConfirmDialog } from '../components/ConfirmDialog'
import { JsonViewer } from '../components/JsonViewer'
import { cohortApi } from '../api/endpoints'

export function CohortDetail() {
  const { id } = useParams<{ id: string }>()
  const nav = useNavigate()
  const qc = useQueryClient()
  const [lockDialog, setLockDialog] = useState(false)

  const { data: cohort, isLoading, error } = useQuery({
    queryKey: ['cohort', id],
    queryFn: () => cohortApi.get(id!),
    enabled: !!id,
  })

  const lockMut = useMutation({
    mutationFn: () => cohortApi.lock(id!),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['cohort', id] })
      qc.invalidateQueries({ queryKey: ['cohorts'] })
      setLockDialog(false)
    },
  })

  if (isLoading) return (
    <>
      <PageHeader title="Cohort" />
      <div className="page-body"><LoadingState /></div>
    </>
  )
  if (error || !cohort) return (
    <>
      <PageHeader title="Cohort" />
      <div className="page-body"><ErrorState title="Failed to load cohort" body={String(error)} /></div>
    </>
  )

  const mfst = cohort.manifest
  const locked = !!cohort.locked_at

  const barData = mfst ? [
    { name: 'Entities', value: mfst.entity_count },
    { name: 'Accounts', value: mfst.account_count },
    { name: 'Transactions', value: mfst.tx_count },
  ] : []

  return (
    <>
      <PageHeader
        title={cohort.name}
        subtitle={`Cohort · ${id}`}
        actions={
          <RoleGate min="analyst">
            <button className="btn btn-secondary btn-sm" onClick={() => nav(`/experiments/new?cohort=${id}`)}>
              Run experiment
            </button>
            <button
              className="btn btn-primary btn-sm"
              onClick={() => setLockDialog(true)}
              disabled={locked}
              aria-disabled={locked}
            >
              {locked ? 'Locked' : 'Lock cohort'}
            </button>
          </RoleGate>
        }
      />
      <div className="page-body">
        {/* Metadata */}
        <div className="metric-grid" style={{ marginBottom: 24 }}>
          <div className="metric-card">
            <div className="metric-label">Status</div>
            <div style={{ marginTop: 8 }}><StatusBadge status={locked ? 'locked' : 'unlocked'} /></div>
          </div>
          <div className="metric-card">
            <div className="metric-label">Seed</div>
            <div className="metric-value" style={{ fontSize: 22 }}>{cohort.seed}</div>
          </div>
          <div className="metric-card">
            <div className="metric-label">Entities</div>
            <div className="metric-value">{mfst?.entity_count ?? '—'}</div>
          </div>
          <div className="metric-card">
            <div className="metric-label">Accounts</div>
            <div className="metric-value">{mfst?.account_count ?? '—'}</div>
          </div>
          <div className="metric-card">
            <div className="metric-label">Transactions</div>
            <div className="metric-value">{mfst?.tx_count ?? '—'}</div>
          </div>
        </div>

        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 16 }}>
          {/* Breakdown chart */}
          {mfst && (
            <div className="card">
              <div className="card-header"><span className="card-title">Dataset breakdown</span></div>
              <div className="card-body">
                <ResponsiveContainer width="100%" height={180}>
                  <BarChart data={barData} barSize={36}>
                    <XAxis dataKey="name" tick={{ fill: 'var(--text-secondary)', fontSize: 12 }} axisLine={false} tickLine={false} />
                    <YAxis hide />
                    <Tooltip
                      contentStyle={{ background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 6, fontSize: 12 }}
                    />
                    <Bar dataKey="value" fill="var(--accent)" radius={[3, 3, 0, 0]} />
                  </BarChart>
                </ResponsiveContainer>
              </div>
            </div>
          )}

          {/* Metadata detail */}
          <div className="card">
            <div className="card-header"><span className="card-title">Metadata</span></div>
            <div className="card-body">
              <table aria-label="Cohort metadata" style={{ width: '100%', fontSize: 12 }}>
                <tbody>
                  {[
                    ['ID', id],
                    ['Generator', cohort.generator_version],
                    ['Window start', cohort.window_start ? new Date(cohort.window_start).toLocaleString() : '—'],
                    ['Window end', cohort.window_end ? new Date(cohort.window_end).toLocaleString() : '—'],
                    ['Locked at', locked ? new Date(cohort.locked_at!).toLocaleString() : '—'],
                    ['Created', new Date(cohort.created_at).toLocaleString()],
                  ].map(([k, v]) => (
                    <tr key={k} style={{ borderBottom: '1px solid var(--border)' }}>
                      <td style={{ padding: '6px 0', color: 'var(--text-muted)', width: '40%' }}>{k}</td>
                      <td style={{ padding: '6px 0', fontFamily: 'var(--font-mono)', wordBreak: 'break-all' }}>{v}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        </div>

        {/* Predicate */}
        <div className="card" style={{ marginTop: 16 }}>
          <div className="card-header"><span className="card-title">Predicate</span></div>
          <div className="card-body">
            <JsonViewer value={cohort.predicate} maxHeight={200} />
          </div>
        </div>
      </div>

      <ConfirmDialog
        open={lockDialog}
        title="Lock this cohort?"
        body="Locking is irreversible. The cohort data will be frozen and cannot be modified. Experiments can then be run against it."
        confirmLabel="Lock Cohort"
        danger
        onConfirm={() => lockMut.mutate()}
        onCancel={() => setLockDialog(false)}
        loading={lockMut.isPending}
      />
    </>
  )
}
