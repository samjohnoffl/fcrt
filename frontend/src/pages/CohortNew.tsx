import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { PageHeader } from '../components/Layout'
import { cohortApi } from '../api/endpoints'
import type { CreateCohortRequest } from '../api/types'

export function CohortNew() {
  const nav = useNavigate()
  const qc = useQueryClient()
  const [form, setForm] = useState<CreateCohortRequest>({
    name: '',
    seed: Math.floor(Math.random() * 1_000_000),
    predicate: {},
    window_start: '2024-01-01T00:00:00Z',
    window_end:   '2024-04-01T00:00:00Z',
  })
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({})

  const mut = useMutation({
    mutationFn: cohortApi.create,
    onSuccess: (cohort) => {
      qc.invalidateQueries({ queryKey: ['cohorts'] })
      nav(`/cohorts/${cohort.id}`)
    },
  })

  function validate() {
    const errs: Record<string, string> = {}
    if (!form.name.trim()) errs.name = 'Name is required'
    if (!form.seed) errs.seed = 'Seed is required'
    if (!form.window_start) errs.window_start = 'Window start is required'
    if (!form.window_end) errs.window_end = 'Window end is required'
    if (form.window_start >= form.window_end) errs.window_end = 'End must be after start'
    return errs
  }

  function submit(e: React.FormEvent) {
    e.preventDefault()
    const errs = validate()
    setFieldErrors(errs)
    if (Object.keys(errs).length > 0) return
    mut.mutate(form)
  }

  const set = (k: keyof CreateCohortRequest, v: unknown) =>
    setForm(f => ({ ...f, [k]: v }))

  return (
    <>
      <PageHeader title="New Cohort" subtitle="Define a synthetic banking dataset" />
      <div className="page-body">
        <div className="card" style={{ maxWidth: 640 }}>
          <form onSubmit={submit} noValidate>
            <div className="card-body">
              <div className="form-stack">
                <div className="form-group">
                  <label className="form-label" htmlFor="name">Cohort name *</label>
                  <input
                    id="name" className="form-input" type="text"
                    value={form.name} onChange={e => set('name', e.target.value)}
                    placeholder="e.g. Q1-2024 High-Risk"
                    aria-describedby={fieldErrors.name ? 'name-err' : undefined}
                  />
                  {fieldErrors.name && <span id="name-err" className="form-error">{fieldErrors.name}</span>}
                </div>

                <div className="form-row">
                  <div className="form-group">
                    <label className="form-label" htmlFor="seed">Random seed *</label>
                    <input
                      id="seed" className="form-input" type="number"
                      value={form.seed} onChange={e => set('seed', Number(e.target.value))}
                    />
                    <span className="form-hint">Same seed → same dataset. Change for a new draw.</span>
                    {fieldErrors.seed && <span className="form-error">{fieldErrors.seed}</span>}
                  </div>
                  <div className="form-group">
                    <label className="form-label" htmlFor="entity_type">Entity type filter</label>
                    <select
                      id="entity_type" className="form-select"
                      value={form.predicate.entity_type ?? ''}
                      onChange={e => set('predicate', { ...form.predicate, entity_type: e.target.value || undefined })}
                    >
                      <option value="">All types</option>
                      <option value="individual">Individual</option>
                      <option value="company">Company</option>
                      <option value="shell">Shell</option>
                    </select>
                  </div>
                </div>

                <div className="form-row">
                  <div className="form-group">
                    <label className="form-label" htmlFor="min_entities">Min entities</label>
                    <input
                      id="min_entities" className="form-input" type="number"
                      value={form.predicate.min_entities ?? ''}
                      onChange={e => set('predicate', { ...form.predicate, min_entities: e.target.value ? Number(e.target.value) : undefined })}
                      placeholder="10"
                    />
                  </div>
                  <div className="form-group">
                    <label className="form-label" htmlFor="max_entities">Max entities</label>
                    <input
                      id="max_entities" className="form-input" type="number"
                      value={form.predicate.max_entities ?? ''}
                      onChange={e => set('predicate', { ...form.predicate, max_entities: e.target.value ? Number(e.target.value) : undefined })}
                      placeholder="30"
                    />
                  </div>
                </div>

                <div className="form-row">
                  <div className="form-group">
                    <label className="form-label" htmlFor="window_start">Window start *</label>
                    <input
                      id="window_start" className="form-input" type="datetime-local"
                      value={form.window_start.slice(0, 16)}
                      onChange={e => set('window_start', e.target.value + ':00Z')}
                    />
                    {fieldErrors.window_start && <span className="form-error">{fieldErrors.window_start}</span>}
                  </div>
                  <div className="form-group">
                    <label className="form-label" htmlFor="window_end">Window end *</label>
                    <input
                      id="window_end" className="form-input" type="datetime-local"
                      value={form.window_end.slice(0, 16)}
                      onChange={e => set('window_end', e.target.value + ':00Z')}
                    />
                    {fieldErrors.window_end && <span className="form-error">{fieldErrors.window_end}</span>}
                  </div>
                </div>

                {mut.error && (
                  <div className="form-error">
                    Failed to create cohort: {String(mut.error)}
                  </div>
                )}
              </div>
            </div>
            <div style={{ padding: '0 20px 20px', display: 'flex', gap: 8, justifyContent: 'flex-end' }}>
              <button type="button" className="btn btn-secondary" onClick={() => nav('/cohorts')}>Cancel</button>
              <button type="submit" className="btn btn-primary" disabled={mut.isPending}>
                {mut.isPending ? 'Creating…' : 'Create Cohort'}
              </button>
            </div>
          </form>
        </div>
      </div>
    </>
  )
}
