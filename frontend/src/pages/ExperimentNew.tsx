import { useState, useEffect } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { PageHeader } from '../components/Layout'
import { LoadingState } from '../components/StateViews'
import { JsonViewer } from '../components/JsonViewer'
import { cohortApi, experimentApi } from '../api/endpoints'
import type { CreateExperimentRequest } from '../api/types'

const MUTATION_TYPES = [
  { value: 'split_transaction',    label: 'Split Transaction' },
  { value: 'introduce_shell_entity', label: 'Introduce Shell Entity' },
  { value: 'delay_and_resubmit',   label: 'Delay and Resubmit' },
]

export function ExperimentNew() {
  const nav = useNavigate()
  const [params] = useSearchParams()
  const qc = useQueryClient()

  const [step, setStep] = useState<'config' | 'preview'>('config')
  const [form, setForm] = useState<CreateExperimentRequest>({
    cohort_id: params.get('cohort') ?? '',
    name: '',
    declared_change: '',
    mutation_type: 'split_transaction',
    mutation_spec: { split_count: 3, threshold_cents: 1000000 },
  })
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({})

  const { data: cohorts } = useQuery({ queryKey: ['cohorts'], queryFn: cohortApi.list })
  const lockedCohorts = (cohorts ?? []).filter(c => !!c.locked_at)

  const selectedCohort = lockedCohorts.find(c => c.id === form.cohort_id)

  const mut = useMutation({
    mutationFn: experimentApi.create,
    onSuccess: (exp) => {
      qc.invalidateQueries({ queryKey: ['experiments'] })
      nav(`/experiments/${exp.id}`)
    },
  })

  // Reset spec when mutation type changes
  useEffect(() => {
    const specDefaults: Record<string, Record<string, unknown>> = {
      split_transaction:     { split_count: 3, threshold_cents: 1000000 },
      introduce_shell_entity:{ threshold_cents: 500000 },
      delay_and_resubmit:    { delay_minutes: 120 },
    }
    setForm(f => ({ ...f, mutation_spec: specDefaults[f.mutation_type] ?? {} }))
  }, [form.mutation_type])

  function validate() {
    const errs: Record<string, string> = {}
    if (!form.cohort_id) errs.cohort_id = 'Select a locked cohort'
    if (!form.name.trim()) errs.name = 'Name is required'
    if (!form.declared_change.trim()) errs.declared_change = 'Declare the change'
    return errs
  }

  function handlePreview(e: React.FormEvent) {
    e.preventDefault()
    const errs = validate()
    setFieldErrors(errs)
    if (Object.keys(errs).length === 0) setStep('preview')
  }

  const set = (k: keyof CreateExperimentRequest, v: unknown) =>
    setForm(f => ({ ...f, [k]: v }))
  const setSpec = (k: string, v: unknown) =>
    setForm(f => ({ ...f, mutation_spec: { ...f.mutation_spec, [k]: v } }))

  return (
    <>
      <PageHeader
        title="New Experiment"
        subtitle={step === 'config' ? 'Step 1 — Configure' : 'Step 2 — Preview & Run'}
      />
      <div className="page-body">
        {/* Step indicator */}
        <div className="tabs" style={{ marginBottom: 24 }}>
          <button className={`tab-btn ${step === 'config' ? 'active' : ''}`} onClick={() => setStep('config')}>
            1 · Configuration
          </button>
          <button className={`tab-btn ${step === 'preview' ? 'active' : ''}`} disabled={step === 'config'}>
            2 · Preview & Run
          </button>
        </div>

        {step === 'config' && (
          <form onSubmit={handlePreview} noValidate>
            <div className="card" style={{ maxWidth: 640 }}>
              <div className="card-body">
                <div className="form-stack">
                  <div className="form-group">
                    <label className="form-label" htmlFor="exp-cohort">Cohort *</label>
                    <select id="exp-cohort" className="form-select"
                      value={form.cohort_id} onChange={e => set('cohort_id', e.target.value)}>
                      <option value="">Select a locked cohort…</option>
                      {lockedCohorts.map(c => (
                        <option key={c.id} value={c.id}>{c.name} (seed {c.seed})</option>
                      ))}
                    </select>
                    {fieldErrors.cohort_id && <span className="form-error">{fieldErrors.cohort_id}</span>}
                    {cohorts && lockedCohorts.length === 0 && (
                      <span className="form-hint" style={{ color: 'var(--warning)' }}>No locked cohorts available.</span>
                    )}
                  </div>

                  <div className="form-group">
                    <label className="form-label" htmlFor="exp-name">Experiment name *</label>
                    <input id="exp-name" className="form-input" type="text"
                      value={form.name} onChange={e => set('name', e.target.value)}
                      placeholder="e.g. Split-3 above 10k threshold" />
                    {fieldErrors.name && <span className="form-error">{fieldErrors.name}</span>}
                  </div>

                  <div className="form-group">
                    <label className="form-label" htmlFor="exp-mutation">Mutation type *</label>
                    <select id="exp-mutation" className="form-select"
                      value={form.mutation_type} onChange={e => set('mutation_type', e.target.value)}>
                      {MUTATION_TYPES.map(m => <option key={m.value} value={m.value}>{m.label}</option>)}
                    </select>
                  </div>

                  {/* Dynamic spec inputs */}
                  {form.mutation_type === 'split_transaction' && (
                    <div className="form-row">
                      <div className="form-group">
                        <label className="form-label" htmlFor="split-count">Split count (2–20)</label>
                        <input id="split-count" className="form-input" type="number" min={2} max={20}
                          value={(form.mutation_spec.split_count as number) ?? 3}
                          onChange={e => setSpec('split_count', Number(e.target.value))} />
                      </div>
                      <div className="form-group">
                        <label className="form-label" htmlFor="threshold">Threshold (cents)</label>
                        <input id="threshold" className="form-input" type="number" min={1}
                          value={(form.mutation_spec.threshold_cents as number) ?? 1000000}
                          onChange={e => setSpec('threshold_cents', Number(e.target.value))} />
                        <span className="form-hint">1 000 000 = $10,000.00</span>
                      </div>
                    </div>
                  )}
                  {form.mutation_type === 'introduce_shell_entity' && (
                    <div className="form-group">
                      <label className="form-label" htmlFor="shell-threshold">Threshold (cents)</label>
                      <input id="shell-threshold" className="form-input" type="number" min={1}
                        value={(form.mutation_spec.threshold_cents as number) ?? 500000}
                        onChange={e => setSpec('threshold_cents', Number(e.target.value))} />
                    </div>
                  )}
                  {form.mutation_type === 'delay_and_resubmit' && (
                    <div className="form-group">
                      <label className="form-label" htmlFor="delay">Delay minutes (1–1440)</label>
                      <input id="delay" className="form-input" type="number" min={1} max={1440}
                        value={(form.mutation_spec.delay_minutes as number) ?? 120}
                        onChange={e => setSpec('delay_minutes', Number(e.target.value))} />
                    </div>
                  )}

                  <div className="form-group">
                    <label className="form-label" htmlFor="declared">Declared change *</label>
                    <textarea id="declared" className="form-textarea"
                      rows={3}
                      value={form.declared_change}
                      onChange={e => set('declared_change', e.target.value)}
                      placeholder="Describe the mutation being applied and what AML evasion it tests…"
                    />
                    {fieldErrors.declared_change && <span className="form-error">{fieldErrors.declared_change}</span>}
                  </div>
                </div>
              </div>
              <div style={{ padding: '0 20px 20px', display: 'flex', gap: 8, justifyContent: 'flex-end' }}>
                <button type="button" className="btn btn-secondary" onClick={() => nav('/experiments')}>Cancel</button>
                <button type="submit" className="btn btn-primary">Preview →</button>
              </div>
            </div>
          </form>
        )}

        {step === 'preview' && (
          <div style={{ maxWidth: 640 }}>
            <div className="card" style={{ marginBottom: 16 }}>
              <div className="card-header">
                <span className="card-title">Pre-run Preview</span>
                <span className="tag">Review before executing</span>
              </div>
              <div className="card-body">
                <table style={{ width: '100%', fontSize: 13 }}>
                  <tbody>
                    {[
                      ['Cohort', selectedCohort?.name ?? form.cohort_id],
                      ['Cohort seed', String(selectedCohort?.seed ?? '—')],
                      ['Name', form.name],
                      ['Mutation type', form.mutation_type],
                      ['Declared change', form.declared_change],
                    ].map(([k, v]) => (
                      <tr key={k} style={{ borderBottom: '1px solid var(--border)' }}>
                        <td style={{ padding: '8px 0', color: 'var(--text-muted)', width: '40%' }}>{k}</td>
                        <td style={{ padding: '8px 0' }}>{v}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>

            <div className="card" style={{ marginBottom: 16 }}>
              <div className="card-header"><span className="card-title">Mutation Spec</span></div>
              <div className="card-body">
                <JsonViewer value={form.mutation_spec} maxHeight={200} />
              </div>
            </div>

            <div className="card" style={{ marginBottom: 24, padding: 16, background: 'var(--warning-muted)', borderColor: 'var(--warning)' }}>
              <p style={{ fontSize: 12, color: 'var(--warning)' }}>
                <strong>Reproducibility note:</strong> This experiment uses cohort seed <strong>{selectedCohort?.seed}</strong>.
                The same seed and rule version will always produce the same baseline. Results are not hardcoded —
                they depend on the live AML harness evaluation.
              </p>
            </div>

            {mut.error && <div className="form-error" style={{ marginBottom: 12 }}>Failed: {String(mut.error)}</div>}

            <div style={{ display: 'flex', gap: 8, justifyContent: 'flex-end' }}>
              <button className="btn btn-secondary" onClick={() => setStep('config')}>← Back</button>
              <button className="btn btn-primary" onClick={() => mut.mutate(form)} disabled={mut.isPending}>
                {mut.isPending ? <><LoadingState title="" /> Running…</> : 'Execute experiment'}
              </button>
            </div>
          </div>
        )}
      </div>
    </>
  )
}
