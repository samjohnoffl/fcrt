// Audit log — admin-only view of every consequential action (DECISION-006).
//
// Backed by GET /api/v1/audit. Shows who did what, to which object, and when.

import { Fragment, useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { PageHeader } from '../components/Layout'
import { LoadingState, ErrorState, EmptyState } from '../components/StateViews'
import { JsonViewer } from '../components/JsonViewer'
import { auditApi } from '../api/endpoints'
import { FetchError } from '../api/client'
import type { AuditEvent } from '../api/types'

// Human labels for the event types the platform writes.
const EVENT_LABELS: Record<string, string> = {
  scenario_approved: 'Scenario approved',
  scenario_rejected: 'Scenario rejected',
  experiment_started: 'Experiment started',
  experiment_completed: 'Experiment completed',
  experiment_failed: 'Experiment failed',
  cohort_created: 'Cohort created',
  cohort_locked: 'Cohort locked',
  auth_login_succeeded: 'Signed in',
  auth_login_failed: 'Sign-in failed',
  user_created: 'User created',
  user_updated: 'User updated',
  user_deleted: 'User deleted',
}

const FILTERS = ['', ...Object.keys(EVENT_LABELS)]

export function AuditLog() {
  const [eventType, setEventType] = useState('')
  const [expanded, setExpanded] = useState<string | null>(null)

  const query = useQuery({
    queryKey: ['audit', eventType],
    queryFn: () => auditApi.list({ limit: 200, event_type: eventType || undefined }),
  })

  const events: AuditEvent[] = useMemo(() => query.data ?? [], [query.data])

  return (
    <>
      <PageHeader
        title="Audit log"
        subtitle="Every consequential action, newest first"
        actions={
          <select
            className="form-select"
            aria-label="Filter by event type"
            value={eventType}
            onChange={e => setEventType(e.target.value)}
            style={{ width: 200 }}
          >
            {FILTERS.map(f => (
              <option key={f} value={f}>
                {f === '' ? 'All events' : (EVENT_LABELS[f] ?? f)}
              </option>
            ))}
          </select>
        }
      />
      <div className="page-body">
        <div className="card">
          <div className="card-header">
            <span className="card-title">Events</span>
            <span className="text-muted" style={{ fontSize: 12 }}>
              {events.length} shown
            </span>
          </div>

          {query.isLoading ? (
            <LoadingState title="Loading audit events…" />
          ) : query.error ? (
            <ErrorState
              title="Failed to load the audit log"
              body={query.error instanceof FetchError ? query.error.message : String(query.error)}
            />
          ) : events.length === 0 ? (
            <EmptyState
              title="No audit events"
              body="Actions such as approving a scenario, running an experiment or managing a user will appear here."
            />
          ) : (
            <div className="table-wrap">
              <table aria-label="Audit events">
                <thead>
                  <tr>
                    <th scope="col">When</th>
                    <th scope="col">Event</th>
                    <th scope="col">Actor</th>
                    <th scope="col">Target</th>
                    <th scope="col" />
                  </tr>
                </thead>
                <tbody>
                  {events.map(ev => (
                    <Fragment key={ev.id}>
                      <tr>
                        <td className="td-muted" style={{ whiteSpace: 'nowrap' }}>
                          {new Date(ev.created_at).toLocaleString()}
                        </td>
                        <td>{EVENT_LABELS[ev.event_type] ?? ev.event_type}</td>
                        <td className="td-mono">{ev.actor_uid || '—'}</td>
                        <td className="td-muted">{ev.target_type || '—'}</td>
                        <td style={{ textAlign: 'right' }}>
                          <button
                            className="btn btn-ghost btn-sm"
                            onClick={() => setExpanded(expanded === ev.id ? null : ev.id)}
                            aria-expanded={expanded === ev.id}
                          >
                            {expanded === ev.id ? 'Hide' : 'Details'}
                          </button>
                        </td>
                      </tr>
                      {expanded === ev.id && (
                        <tr>
                          <td colSpan={5} style={{ background: 'var(--bg-subtle)' }}>
                            <JsonViewer value={ev.details ?? {}} maxHeight={240} />
                            {(ev.ip_address || ev.user_agent) && (
                              <div className="text-muted" style={{ fontSize: 12, marginTop: 8 }}>
                                {ev.ip_address && <>IP {ev.ip_address}</>}
                                {ev.ip_address && ev.user_agent && ' · '}
                                {ev.user_agent}
                              </div>
                            )}
                          </td>
                        </tr>
                      )}
                    </Fragment>
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
