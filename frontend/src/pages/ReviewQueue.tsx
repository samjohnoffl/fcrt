import { Fragment, useState } from 'react'
import { Link } from 'react-router-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { PageHeader } from '../components/Layout'
import { LoadingState, ErrorState, EmptyState } from '../components/StateViews'
import { reviewApi } from '../api/endpoints'
import type { ReviewDecisionValue } from '../api/types'

const decisions: { value: ReviewDecisionValue; label: string }[] = [
  { value: 'approved', label: 'Approve' },
  { value: 'rejected', label: 'Reject' },
  { value: 'escalated', label: 'Escalate' },
]

export function ReviewQueue() {
  const queryClient = useQueryClient()
  const { data, isLoading, error } = useQuery({
    queryKey: ['review-queue'],
    queryFn: reviewApi.queue,
  })

  const [reviewing, setReviewing] = useState<string | null>(null)
  const [decision, setDecision] = useState<ReviewDecisionValue>('approved')
  const [rationale, setRationale] = useState('')
  const [formError, setFormError] = useState<string | null>(null)

  const submit = useMutation({
    mutationFn: (pairID: string) => reviewApi.create(pairID, decision, rationale.trim()),
    onSuccess: () => {
      setReviewing(null)
      setRationale('')
      setFormError(null)
      void queryClient.invalidateQueries({ queryKey: ['review-queue'] })
    },
    onError: (err: unknown) => setFormError(String(err)),
  })

  const queue = data?.queue ?? []

  function openForm(pairID: string) {
    setReviewing(pairID)
    setDecision('approved')
    setRationale('')
    setFormError(null)
  }

  return (
    <>
      <PageHeader
        title="Review Queue"
        subtitle="Completed experiments awaiting a reviewer decision"
      />
      <div className="page-body">
        {isLoading ? <LoadingState title="Loading review queue…" /> :
         error ? <ErrorState title="Failed to load review queue" body={String(error)} /> :
         queue.length === 0 ? (
          <EmptyState
            title="Queue is clear"
            body="Every completed experiment has a recorded decision."
          />
         ) : (
          <div className="card">
            <div className="card-header">
              <span className="card-title">Awaiting review</span>
              <span className="badge">{queue.length} pending</span>
            </div>
            <div className="table-wrap">
              <table aria-label="Review queue table">
                <thead>
                  <tr>
                    <th scope="col">Experiment</th>
                    <th scope="col">Declared change</th>
                    <th scope="col">Mutation</th>
                    <th scope="col">Evasion delta</th>
                    <th scope="col">Created</th>
                    <th scope="col"><span className="sr-only">Actions</span></th>
                  </tr>
                </thead>
                <tbody>
                  {queue.map(item => (
                    <Fragment key={item.id}>
                      <tr>
                        <td>
                          <Link to={`/experiments/${item.id}`}>
                            {item.name}
                          </Link>
                        </td>
                        <td className="td-muted" style={{ maxWidth: 340 }}>{item.declared_change}</td>
                        <td className="td-mono td-muted">{item.mutation_type}</td>
                        <td style={{ fontWeight: 600 }}>{item.evasion_delta ?? '—'}</td>
                        <td className="td-muted">
                          {new Date(item.created_at).toLocaleDateString()}
                        </td>
                        <td>
                          <button
                            className="btn btn-sm"
                            onClick={() => openForm(item.id)}
                            aria-expanded={reviewing === item.id}
                          >
                            Review
                          </button>
                        </td>
                      </tr>
                      {reviewing === item.id && (
                        <tr>
                          <td colSpan={6} style={{ background: 'var(--bg-overlay)' }}>
                            <form
                              onSubmit={e => {
                                e.preventDefault()
                                if (!rationale.trim()) {
                                  setFormError('A rationale is required.')
                                  return
                                }
                                submit.mutate(item.id)
                              }}
                              style={{ display: 'grid', gap: 10, padding: '4px 0' }}
                            >
                              <div style={{ display: 'flex', gap: 12, alignItems: 'center', flexWrap: 'wrap' }}>
                                <label htmlFor={`decision-${item.id}`} className="metric-label">
                                  Decision
                                </label>
                                <select
                                  id={`decision-${item.id}`}
                                  value={decision}
                                  onChange={e => setDecision(e.target.value as ReviewDecisionValue)}
                                  className="form-select"
                                  style={{ width: 160 }}
                                >
                                  {decisions.map(d => (
                                    <option key={d.value} value={d.value}>{d.label}</option>
                                  ))}
                                </select>
                              </div>
                              <div>
                                <label htmlFor={`rationale-${item.id}`} className="metric-label">
                                  Rationale
                                </label>
                                <textarea
                                  id={`rationale-${item.id}`}
                                  value={rationale}
                                  onChange={e => setRationale(e.target.value)}
                                  className="form-textarea"
                                  rows={3}
                                  placeholder="Why is this decision correct? Reference the outcome and coverage evidence."
                                  style={{ width: '100%', resize: 'vertical' }}
                                />
                              </div>
                              {formError && (
                                <div className="alert alert-error" role="alert">{formError}</div>
                              )}
                              <div style={{ display: 'flex', gap: 8 }}>
                                <button
                                  type="submit"
                                  className="btn btn-primary btn-sm"
                                  disabled={submit.isPending}
                                >
                                  {submit.isPending ? 'Saving…' : 'Record decision'}
                                </button>
                                <button
                                  type="button"
                                  className="btn btn-ghost btn-sm"
                                  onClick={() => setReviewing(null)}
                                >
                                  Cancel
                                </button>
                              </div>
                            </form>
                          </td>
                        </tr>
                      )}
                    </Fragment>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
         )}
      </div>
    </>
  )
}
