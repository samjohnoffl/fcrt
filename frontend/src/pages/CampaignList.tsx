import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { PageHeader } from '../components/Layout'
import { RoleGate } from '../components/RoleGate'
import { StatusBadge } from '../components/StatusBadge'
import { LoadingState, ErrorState, EmptyState } from '../components/StateViews'
import { campaignApi, cohortApi } from '../api/endpoints'
import type { CreateCampaignRequest } from '../api/types'

const EMPTY_FORM: CreateCampaignRequest = {
  name: '',
  description: '',
  goal: '',
  budget: 10,
  cohort_id: '',
}

export function CampaignList() {
  const nav = useNavigate()
  const qc = useQueryClient()
  const [dialog, setDialog] = useState(false)
  const [form, setForm] = useState<CreateCampaignRequest>(EMPTY_FORM)

  const { data, isLoading, error } = useQuery({ queryKey: ['campaigns'], queryFn: campaignApi.list })
  const { data: cohorts } = useQuery({ queryKey: ['cohorts'], queryFn: cohortApi.list })

  const createMut = useMutation({
    mutationFn: campaignApi.create,
    onSuccess: (c) => {
      qc.invalidateQueries({ queryKey: ['campaigns'] })
      setDialog(false)
      setForm(EMPTY_FORM)
      nav(`/campaigns/${c.id}`)
    },
  })

  const campaigns = data ?? []

  return (
    <>
      <PageHeader
        title="Campaigns"
        subtitle="Budgeted series of experiments against a fixed control configuration"
        actions={
          <RoleGate min="analyst">
            <button className="btn btn-primary btn-sm" onClick={() => setDialog(true)}>
              New campaign
            </button>
          </RoleGate>
        }
      />
      <div className="page-body">
        <div className="card">
          {isLoading ? <LoadingState title="Loading campaigns…" /> :
           error ? <ErrorState title="Failed to load campaigns" body={String(error)} /> :
           campaigns.length === 0 ? (
             <EmptyState
               title="No campaigns"
               body="A campaign caps how many experiments a team may run and keeps their control configuration fixed."
               action={
                 <RoleGate min="analyst">
                   <button className="btn btn-primary btn-sm" onClick={() => setDialog(true)}>
                     New campaign
                   </button>
                 </RoleGate>
               }
             />
           ) : (
             <div className="table-wrap">
               <table aria-label="Campaigns">
                 <thead>
                   <tr>
                     <th scope="col">Name</th>
                     <th scope="col">Status</th>
                     <th scope="col">Budget</th>
                     <th scope="col">Cohort</th>
                     <th scope="col">Created</th>
                   </tr>
                 </thead>
                 <tbody>
                   {campaigns.map(c => (
                     <tr
                       key={c.id}
                       className="clickable-row"
                       tabIndex={0}
                       role="link"
                       aria-label={`Open campaign ${c.name}`}
                       onClick={() => nav(`/campaigns/${c.id}`)}
                       onKeyDown={event => {
                         if (event.key === 'Enter') nav(`/campaigns/${c.id}`)
                       }}
                     >
                       <td>
                         <div style={{ fontWeight: 500 }}>{c.name}</div>
                         {c.goal && <div className="td-muted" style={{ fontSize: 12 }}>{c.goal}</div>}
                       </td>
                       <td><StatusBadge status={c.status} /></td>
                       <td className="td-mono">
                         {c.experiments_used} / {c.budget}
                       </td>
                       <td className="td-mono td-muted">
                         {c.cohort_id ? `${c.cohort_id.slice(0, 8)}…` : '—'}
                       </td>
                       <td className="td-muted">{new Date(c.created_at).toLocaleDateString()}</td>
                     </tr>
                   ))}
                 </tbody>
               </table>
             </div>
           )}
        </div>
      </div>

      <div className="dialog-backdrop" role="dialog" aria-modal="true" aria-labelledby="campaign-title"
        style={{ display: dialog ? 'grid' : 'none' }}
        onClick={e => { if (e.target === e.currentTarget) setDialog(false) }}
      >
        <div className="dialog">
          <h2 id="campaign-title" className="dialog-title">New campaign</h2>
          <p className="dialog-body">
            The campaign fixes the control configuration for every experiment it admits and
            refuses new runs once the budget is spent.
          </p>
          <div className="form-group">
            <label className="form-label" htmlFor="campaign-name">Name *</label>
            <input id="campaign-name" className="form-input" type="text"
              value={form.name} onChange={e => setForm(f => ({ ...f, name: e.target.value }))}
              placeholder="e.g. Structuring pressure test — Q3" />
          </div>
          <div className="form-group">
            <label className="form-label" htmlFor="campaign-goal">Goal</label>
            <input id="campaign-goal" className="form-input" type="text"
              value={form.goal ?? ''} onChange={e => setForm(f => ({ ...f, goal: e.target.value }))}
              placeholder="What should this campaign learn?" />
          </div>
          <div className="form-group">
            <label className="form-label" htmlFor="campaign-description">Description</label>
            <textarea id="campaign-description" className="form-textarea" rows={2}
              value={form.description}
              onChange={e => setForm(f => ({ ...f, description: e.target.value }))} />
          </div>
          <div className="form-row">
            <div className="form-group">
              <label className="form-label" htmlFor="campaign-budget">Experiment budget *</label>
              <input id="campaign-budget" className="form-input" type="number" min={1} max={1000}
                value={form.budget || ''}
                onChange={e => setForm(f => ({
                  ...f,
                  budget: e.target.value === '' ? 0 : Number(e.target.value),
                }))} />
            </div>
            <div className="form-group">
              <label className="form-label" htmlFor="campaign-cohort">Cohort</label>
              <select id="campaign-cohort" className="form-select"
                value={form.cohort_id ?? ''}
                onChange={e => setForm(f => ({ ...f, cohort_id: e.target.value }))}>
                <option value="">No cohort (benchmark unavailable)</option>
                {(cohorts ?? []).map(c => (
                  <option key={c.id} value={c.id}>
                    {c.name}{c.locked_at ? '' : ' — not locked'}
                  </option>
                ))}
              </select>
              <span className="form-hint">A cohort is required for the campaign benchmark; it must be locked to run experiments.</span>
            </div>
          </div>
          {createMut.error && <span className="form-error">{String(createMut.error)}</span>}
          <div className="dialog-actions">
            <button className="btn btn-secondary" onClick={() => setDialog(false)}>Cancel</button>
            <button className="btn btn-primary"
              onClick={() => createMut.mutate({ ...form, cohort_id: form.cohort_id || undefined })}
              disabled={!form.name.trim() || form.budget <= 0 || createMut.isPending}>
              {createMut.isPending ? 'Creating…' : 'Create campaign'}
            </button>
          </div>
        </div>
      </div>
    </>
  )
}
