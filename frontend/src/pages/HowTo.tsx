import { Link } from 'react-router-dom'
import { PageHeader } from '../components/Layout'
import { RoleGate } from '../components/RoleGate'

const STEPS = [
  {
    title: 'Create and lock a cohort',
    body: 'Generate a seeded synthetic bank, then lock it. Locking freezes the population so every experiment is reproducible.',
    to: '/cohorts/new',
    action: 'Create a cohort',
  },
  {
    title: 'Request a scenario (optional)',
    body: 'Ask Gemini for an evasion proposal, or define the mutation yourself. AI output stays pending until a human approves it.',
    to: '/scenarios',
    action: 'Open scenarios',
  },
  {
    title: 'Run a counterfactual experiment',
    body: 'Run the baseline and the mutated variant against the same controls. Optionally attach a budgeted campaign.',
    to: '/experiments/new',
    action: 'New experiment',
  },
  {
    title: 'Review the evidence',
    body: 'Inspect the alert diff, per-control outcomes and the evidence bundle, then record an approve, reject or escalate decision.',
    to: '/reviews',
    action: 'Open review queue',
  },
  {
    title: 'Close the coverage gap',
    body: 'Read the three coverage maps, compare selection strategies in the benchmark, and export the regression pack.',
    to: '/coverage',
    action: 'View coverage',
  },
]

const OUTCOMES = [
  { status: 'triggered', label: 'Triggered', body: 'The control fired on the run.' },
  { status: 'not_triggered', label: 'Not triggered', body: 'Evaluated, but the rule threshold was not met.' },
  { status: 'not_applicable', label: 'Not applicable', body: 'The control was disabled or no events matched.' },
  { status: 'inconclusive', label: 'Inconclusive', body: 'Not enough history to decide either way.' },
  { status: 'failed', label: 'Failed', body: 'The control errored — never counted as a non-detection.' },
]

const CONCEPTS = [
  ['Cohort', 'A deterministic, seeded synthetic bank. Locking prevents the population from changing under a published result.'],
  ['Experiment pair', 'One baseline run and one variant run, executed with the same control configuration.'],
  ['Control outcome', 'One of five exclusive statuses per control family, with a reason code and evidence references.'],
  ['Campaign', 'A fixed control configuration plus a budget that caps how many experiments may run.'],
  ['Evidence bundle', 'A checksummed, replayable record of a run: alerts, outcomes, coverage delta and limitations.'],
  ['Counterfactual', 'The validator that separates the change you declared from dependent or unintended changes.'],
]

export function HowTo() {
  return (
    <>
      <PageHeader title="How to use" subtitle="From cohort to evidence in five steps" />
      <div className="page-body">
        <section className="card howto-hero">
          <div>
            <div className="howto-eyebrow">Quick start guide</div>
            <h2 className="howto-title">Test whether your AML controls hold under pressure.</h2>
            <p className="howto-lead">
              AML Red Team generates synthetic customers and transactions, mutates them to
              probe a specific control weakness, and records exactly what changed. Every
              result is reproducible from the cohort seed and control version.
            </p>
          </div>
          <div className="howto-cta">
            <RoleGate min="analyst">
              <Link className="btn btn-primary" to="/cohorts/new">Start with a cohort</Link>
              <Link className="btn btn-secondary" to="/experiments/new">Run an experiment</Link>
            </RoleGate>
            <p className="howto-note">
              Signed in as <strong>analyst</strong>? You can run the full workflow.
              Administrators additionally manage users and the audit log.
            </p>
          </div>
        </section>

        <h2 className="howto-section-title">The workflow</h2>
        <div className="howto-grid">
          {STEPS.map((step, i) => (
            <article className="card howto-step" key={step.title}>
              <div className="howto-num" aria-hidden="true">{i + 1}</div>
              <h3>{step.title}</h3>
              <p>{step.body}</p>
              <Link to={step.to}>{step.action} →</Link>
            </article>
          ))}
        </div>

        <h2 className="howto-section-title">Where Gemini is used</h2>
        <div className="howto-split">
          <div className="card">
            <div className="card-header"><span className="card-title">AI-assisted proposals</span><span className="tag">Advisory</span></div>
            <div className="card-body howto-prose">
              <p>
                Gemini proposes evasion scenarios from <strong>cohort summary statistics only</strong> —
                never row-level data. A proposal is schema-validated, stored with its raw prompt
                and response for audit, and must be approved by a person before an experiment can use it.
              </p>
              <p>
                Use <Link to="/scenarios">Scenarios</Link> to request or review proposals.
              </p>
            </div>
          </div>
          <div className="card">
            <div className="card-header"><span className="card-title">Deterministic execution</span></div>
            <div className="card-body howto-prose">
              <p>
                Detection, coverage, counterfactual classification and benchmarks are computed by
                deterministic Go code over stored runs. The model is never in the detection loop,
                so a result can always be replayed and audited.
              </p>
              <p>
                Compare strategies in <Link to="/benchmark">Benchmark Lab</Link>; the benchmark
                reports <em>insufficient data</em> instead of inventing a winner.
              </p>
            </div>
          </div>
        </div>

        <h2 className="howto-section-title">Reading control outcomes</h2>
        <div className="card">
          <div className="card-body">
            <div className="howto-outcomes">
              {OUTCOMES.map(o => (
                <div className="howto-outcome" key={o.status}>
                  <span className={`badge badge-${o.status}`}>
                    {o.label}
                  </span>
                  <p>{o.body}</p>
                </div>
              ))}
            </div>
            <p className="howto-note" style={{ marginTop: 16 }}>
              Coverage is derived from outcomes, not from whether an alert happened to mention a rule.
              A failed control is visible as failed — it is never silently treated as a non-detection.
            </p>
          </div>
        </div>

        <h2 className="howto-section-title">Concepts</h2>
        <div className="card">
          <dl className="howto-concepts">
            {CONCEPTS.map(([term, body]) => (
              <div key={term}>
                <dt>{term}</dt>
                <dd>{body}</dd>
              </div>
            ))}
          </dl>
        </div>

        <div className="card howto-footer">
          <div>
            <strong>Need the technical detail?</strong>
            <p className="howto-note">The README documents the architecture, API, deployment and the exact Gemini boundary.</p>
          </div>
          <div className="howto-footer-links">
            <Link className="btn btn-secondary btn-sm" to="/discovery">Discovery</Link>
            <Link className="btn btn-secondary btn-sm" to="/coverage">Coverage</Link>
            <Link className="btn btn-secondary btn-sm" to="/campaigns">Campaigns</Link>
          </div>
        </div>
      </div>
    </>
  )
}
