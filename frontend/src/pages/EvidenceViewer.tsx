import { useParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { PageHeader } from '../components/Layout'
import { LoadingState, ErrorState } from '../components/StateViews'
import { JsonViewer, CollapsibleJson } from '../components/JsonViewer'
import { experimentApi } from '../api/endpoints'

// SHA-256 in-browser checksum verification using Web Crypto API.
async function sha256hex(text: string): Promise<string> {
  const buf = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(text))
  return Array.from(new Uint8Array(buf)).map(b => b.toString(16).padStart(2, '0')).join('')
}

// Canonical form: bundle with checksum field set to "" before hashing.
function canonicalBundle(bundle: Record<string, unknown>): string {
  return JSON.stringify({ ...bundle, checksum: '' }, null, 2)
}

import { useState } from 'react'

export function EvidenceViewer() {
  const { id } = useParams<{ id: string }>()
  const [verifyResult, setVerifyResult] = useState<{ ok: boolean; computed: string } | null>(null)
  const [verifying, setVerifying] = useState(false)

  const { data: bundle, isLoading, error } = useQuery({
    queryKey: ['evidence', id],
    queryFn: () => experimentApi.evidence(id!),
    enabled: !!id,
  })

  async function verify() {
    if (!bundle) return
    setVerifying(true)
    try {
      const computed = await sha256hex(canonicalBundle(bundle as unknown as Record<string, unknown>))
      setVerifyResult({ ok: computed === bundle.checksum, computed })
    } finally {
      setVerifying(false)
    }
  }

  function download() {
    if (!bundle) return
    const blob = new Blob([JSON.stringify(bundle, null, 2)], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `evidence_${bundle.run_id}.json`
    a.click()
    URL.revokeObjectURL(url)
  }

  if (isLoading) return (
    <><PageHeader title="Evidence bundle" /><div className="page-body"><LoadingState /></div></>
  )
  if (error || !bundle) return (
    <><PageHeader title="Evidence bundle" /><div className="page-body"><ErrorState title="Failed to load evidence" body={String(error)} /></div></>
  )

  return (
    <>
      <PageHeader
        title="Evidence bundle"
        subtitle={`Run · ${bundle.run_id}`}
        actions={
          <button className="btn btn-secondary btn-sm" onClick={download}>
            ↓ Download JSON
          </button>
        }
      />
      <div className="page-body">
        {/* Checksum panel */}
        <div className="card" style={{ marginBottom: 16 }}>
          <div className="card-header">
            <span className="card-title">Integrity checksum</span>
            <button className="btn btn-ghost btn-sm" onClick={verify} disabled={verifying}>
              {verifying ? 'Verifying…' : '✓ Verify Checksum'}
            </button>
          </div>
          <div className="card-body">
            <div className="form-label" style={{ marginBottom: 6 }}>Stored SHA-256 (canonical form, checksum="")</div>
            <div className="checksum-box" role="note" aria-label="Stored checksum">{bundle.checksum}</div>
            {verifyResult && (
              <div style={{ marginTop: 12, padding: '10px 14px', borderRadius: 6,
                background: verifyResult.ok ? 'var(--success-muted)' : 'var(--error-muted)',
                border: `1px solid ${verifyResult.ok ? 'var(--success)' : 'var(--error)'}`,
              }}>
                <div style={{ fontSize: 13, fontWeight: 600, color: verifyResult.ok ? 'var(--success)' : 'var(--error)' }}>
                  {verifyResult.ok ? '✓ Checksum verified — bundle is intact' : '✗ Checksum mismatch — bundle may be corrupted'}
                </div>
                <div className="checksum-box" style={{ marginTop: 8 }}>Computed: {verifyResult.computed}</div>
              </div>
            )}
          </div>
        </div>

        {/* Cohort snapshot */}
        <div className="card" style={{ marginBottom: 16 }}>
          <div className="card-header"><span className="card-title">Cohort snapshot</span></div>
          <div className="card-body">
            <JsonViewer value={bundle.cohort_snapshot} maxHeight={200} />
          </div>
        </div>

        {/* AML diff summary */}
        <div className="card" style={{ marginBottom: 16 }}>
          <div className="card-header"><span className="card-title">AML diff summary</span></div>
          <div className="card-body">
            <div className="metric-grid">
              <div>
                <div className="metric-label">Baseline alerts</div>
                <div className="metric-value" style={{ fontSize: 24 }}>{bundle.aml_diff.baseline_alert_count}</div>
              </div>
              <div>
                <div className="metric-label">Variant alerts</div>
                <div className="metric-value" style={{ fontSize: 24 }}>{bundle.aml_diff.variant_alert_count}</div>
              </div>
              <div>
                <div className="metric-label">Evasion Δ</div>
                <div className="metric-value" style={{ fontSize: 24, color: bundle.aml_diff.evasion_delta > 0 ? 'var(--error)' : bundle.aml_diff.evasion_delta < 0 ? 'var(--success)' : 'var(--text-secondary)' }}>
                  {bundle.aml_diff.evasion_delta > 0 ? '+' : ''}{bundle.aml_diff.evasion_delta}
                </div>
              </div>
            </div>
          </div>
        </div>

        {/* Coverage delta */}
        {bundle.coverage_delta && bundle.coverage_delta.length > 0 && (
          <div className="card" style={{ marginBottom: 16 }}>
            <div className="card-header"><span className="card-title">Coverage delta</span></div>
            <div className="table-wrap">
              <table aria-label="Coverage delta">
                <thead>
                  <tr>
                    <th scope="col">Rule ID</th>
                    <th scope="col">Times Triggered</th>
                    <th scope="col">Weight</th>
                    <th scope="col">Last Triggered</th>
                  </tr>
                </thead>
                <tbody>
                  {bundle.coverage_delta.map(r => (
                    <tr key={r.rule_id} className={r.times_triggered === 0 ? 'alert-row-uncovered' : ''}>
                      <td className="td-mono">{r.rule_id}</td>
                      <td>{r.times_triggered}</td>
                      <td>{r.coverage_weight.toFixed(3)}</td>
                      <td className="td-muted">{r.last_triggered_at ? new Date(r.last_triggered_at).toLocaleString() : 'Never'}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        )}

        {/* Gemini trace */}
        {bundle.gemini_trace && (
          <div className="card" style={{ marginBottom: 16 }}>
            <div className="card-body">
              <CollapsibleJson label="Gemini Trace — Prompt" value={bundle.gemini_trace.prompt} />
              <div style={{ height: 12 }} />
              <CollapsibleJson label="Gemini Trace — Response" value={bundle.gemini_trace.response} />
            </div>
          </div>
        )}

        {/* Full bundle JSON */}
        <div className="card">
          <div className="card-body">
            <CollapsibleJson label="Full evidence bundle (raw JSON)" value={bundle} />
          </div>
        </div>

        {/* Reviewer limitations notice */}
        <div className="card" style={{
          marginTop: 16, padding: 16,
          background: 'var(--info-muted)', borderColor: 'var(--info)',
        }}>
          <div style={{ fontWeight: 600, fontSize: 13, color: 'var(--info)', marginBottom: 8 }}>
            Reviewer Limitations
          </div>
          <ul style={{ fontSize: 12, color: 'var(--text-secondary)', lineHeight: 1.8, paddingLeft: 20 }}>
            <li>The AML harness is a deterministic stub — it does not represent a production AML system.</li>
            <li>Evasion delta reflects mutations against synthetic data only. Real-world applicability requires validation.</li>
            <li>Gemini proposals are generated by an AI model and must be reviewed before use in policy decisions.</li>
            <li>Coverage percentages reflect synthetic rule exercise frequency, not real AML coverage.</li>
          </ul>
        </div>
      </div>
    </>
  )
}
