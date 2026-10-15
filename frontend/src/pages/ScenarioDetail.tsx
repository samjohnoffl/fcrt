import { useState } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { PageHeader } from '../components/Layout'
import { StatusBadge } from '../components/StatusBadge'
import { LoadingState, ErrorState } from '../components/StateViews'
import { ConfirmDialog } from '../components/ConfirmDialog'
import { JsonViewer, CollapsibleJson } from '../components/JsonViewer'
import { scenarioApi } from '../api/endpoints'

export function ScenarioDetail() {
  const { id } = useParams<{ id: string }>()
  const nav = useNavigate()
  const qc = useQueryClient()
  const [dialog, setDialog] = useState<'approve' | 'reject' | null>(null)

  const { data: s, isLoading, error } = useQuery({
    queryKey: ['scenario', id],
    queryFn: () => scenarioApi.get(id!),
    enabled: !!id,
  })

  const approveMut = useMutation({
    mutationFn: () => scenarioApi.approve(id!),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['scenario', id] }); setDialog(null) },
  })
  const rejectMut = useMutation({
    mutationFn: () => scenarioApi.reject(id!),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['scenario', id] }); setDialog(null) },
  })

  if (isLoading) return (
    <><PageHeader title="Scenario" /><div className="page-body"><LoadingState /></div></>
  )
  if (error || !s) return (
    <><PageHeader title="Scenario" /><div className="page-body"><ErrorState title="Failed to load scenario" body={String(error)} /></div></>
  )

  const isPending = s.status === 'pending'

  return (
    <>
      <PageHeader
        title={s.scenario_type}
        subtitle={`Scenario · ${id}`}
        actions={
          <>
            <button
              className="btn btn-danger btn-sm"
              onClick={() => setDialog('reject')}
              disabled={!isPending}
              aria-disabled={!isPending}
            >
              Reject
            </button>
            <button
              className="btn btn-primary btn-sm"
              onClick={() => setDialog('approve')}
              disabled={!isPending}
              aria-disabled={!isPending}
            >
              Approve
            </button>
          </>
        }
      />
      <div className="page-body">
        {/* Header row */}
        <div className="metric-grid" style={{ marginBottom: 24 }}>
          <div className="metric-card">
            <div className="metric-label">Status</div>
            <div style={{ marginTop: 8 }}><StatusBadge status={s.status} /></div>
          </div>
          <div className="metric-card">
            <div className="metric-label">Type</div>
            <div style={{ marginTop: 8 }}><span className="tag">{s.scenario_type}</span></div>
          </div>
          <div className="metric-card">
            <div className="metric-label">Model</div>
            <div className="metric-sub" style={{ marginTop: 8 }}>{s.gemini_model}</div>
          </div>
          <div className="metric-card">
            <div className="metric-label">Correlation ID</div>
            <div className="monospace td-muted" style={{ marginTop: 8, fontSize: 11 }}>{s.correlation_id}</div>
          </div>
        </div>

        {/* Description */}
        <div className="card" style={{ marginBottom: 16 }}>
          <div className="card-header"><span className="card-title">Description</span></div>
          <div className="card-body">
            <p style={{ fontSize: 13, lineHeight: 1.7, color: 'var(--text-primary)' }}>{s.description}</p>
          </div>
        </div>

        {/* Evasion vector */}
        <div className="card" style={{ marginBottom: 16 }}>
          <div className="card-header"><span className="card-title">Expected Evasion Vector</span></div>
          <div className="card-body">
            <p style={{ fontSize: 13, lineHeight: 1.7 }}>{s.expected_evasion_vector}</p>
          </div>
        </div>

        {/* Suggested tx pattern */}
        <div className="card" style={{ marginBottom: 16 }}>
          <div className="card-header"><span className="card-title">Suggested Transaction Pattern</span></div>
          <div className="card-body">
            <JsonViewer value={s.suggested_tx_pattern} />
          </div>
        </div>

        {/* Gemini trace — collapsible */}
        <div className="card" style={{ marginBottom: 16 }}>
          <div className="card-body">
            <CollapsibleJson label="Gemini Prompt" value={s.gemini_prompt} />
            <div style={{ height: 12 }} />
            <CollapsibleJson label="Gemini Raw Response" value={s.gemini_response} />
          </div>
        </div>

        {/* Run experiment from this scenario */}
        {s.status === 'approved' && (
          <div style={{ display: 'flex', justifyContent: 'flex-end', marginTop: 8 }}>
            <button
              className="btn btn-primary"
              onClick={() => nav(`/experiments/new?scenario=${id}&cohort=${s.cohort_id}`)}
            >
              ⚗ Create Experiment from Scenario
            </button>
          </div>
        )}
      </div>

      <ConfirmDialog
        open={dialog === 'approve'}
        title="Approve this scenario?"
        body="Approving allows an experiment to be created from this scenario. This action requires explicit confirmation."
        confirmLabel="Approve"
        onConfirm={() => approveMut.mutate()}
        onCancel={() => setDialog(null)}
        loading={approveMut.isPending}
      />
      <ConfirmDialog
        open={dialog === 'reject'}
        title="Reject this scenario?"
        body="Rejecting marks the scenario as unsuitable. It will not be used in any experiment."
        confirmLabel="Reject"
        danger
        onConfirm={() => rejectMut.mutate()}
        onCancel={() => setDialog(null)}
        loading={rejectMut.isPending}
      />
    </>
  )
}
