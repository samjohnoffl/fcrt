// Users — admin-only user management (AUTH_MODE=local).
//
// Backed by /api/v1/users. The server enforces both admin-only access and the
// self-lockout guards; this page mirrors them so the interface never offers an
// action that would be rejected.

import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { PageHeader } from '../components/Layout'
import { LoadingState, ErrorState } from '../components/StateViews'
import { ConfirmDialog } from '../components/ConfirmDialog'
import { userApi } from '../api/endpoints'
import { FetchError } from '../api/client'
import { getUser } from '../lib/session'
import { ROLES, ROLE_ORDER, roleLabel } from '../lib/roles'
import type { AppUser, Role, UpdateUserRequest } from '../api/types'

function message(e: unknown): string {
  return e instanceof FetchError ? e.message : 'Something went wrong. Please try again.'
}

export function Users() {
  const qc = useQueryClient()
  const me = getUser()

  const users = useQuery({ queryKey: ['users'], queryFn: userApi.list })
  const [error, setError] = useState('')
  const [showAdd, setShowAdd] = useState(false)
  const [pendingDelete, setPendingDelete] = useState<AppUser | null>(null)
  const [resetFor, setResetFor] = useState<AppUser | null>(null)

  const invalidate = () => void qc.invalidateQueries({ queryKey: ['users'] })

  const createMut = useMutation({
    mutationFn: userApi.create,
    onSuccess: () => { setError(''); setShowAdd(false); invalidate() },
    onError: e => setError(message(e)),
  })

  const updateMut = useMutation({
    mutationFn: ({ username, body }: { username: string; body: UpdateUserRequest }) =>
      userApi.update(username, body),
    onSuccess: () => { setError(''); invalidate() },
    onError: e => setError(message(e)),
  })

  const deleteMut = useMutation({
    mutationFn: (username: string) => userApi.remove(username),
    onSuccess: () => { setError(''); setPendingDelete(null); invalidate() },
    onError: e => { setPendingDelete(null); setError(message(e)) },
  })

  return (
    <>
      <PageHeader
        title="Users"
        subtitle="Accounts that can sign in and what they are allowed to do"
        actions={
          <button className="btn btn-primary btn-sm" onClick={() => setShowAdd(v => !v)}>
            {showAdd ? 'Cancel' : 'Add user'}
          </button>
        }
      />
      <div className="page-body">
        {error && <div className="alert alert-error" role="alert">{error}</div>}

        {/* Roles at a glance — the same wording the login screen uses. */}
        <div className="role-grid" style={{ marginBottom: 20 }}>
          {ROLE_ORDER.map(id => {
            const role = ROLES[id]
            return (
              <div key={id} className="role-card">
                <div className="role-card-head">
                  <span className={`role-badge role-${id}`}>{role.label}</span>
                  <span className="text-muted" style={{ fontSize: 12 }}>{role.summary}</span>
                </div>
                <ul className="role-card-list">
                  {role.capabilities.map(c => <li key={c}>{c}</li>)}
                </ul>
              </div>
            )
          })}
        </div>

        {showAdd && (
          <div className="card" style={{ marginBottom: 20 }}>
            <div className="card-header"><span className="card-title">New user</span></div>
            <div className="card-body">
              <AddUserForm
                busy={createMut.isPending}
                onCancel={() => setShowAdd(false)}
                onSubmit={v => createMut.mutate(v)}
              />
            </div>
          </div>
        )}

        <div className="card">
          <div className="card-header">
            <span className="card-title">Accounts</span>
            <span className="text-muted" style={{ fontSize: 12 }}>
              {users.data?.length ?? 0} total
            </span>
          </div>

          {users.isLoading ? (
            <LoadingState title="Loading users…" />
          ) : users.error ? (
            <ErrorState title="Failed to load users" body={message(users.error)} />
          ) : (
            <div className="table-wrap">
              <table aria-label="User accounts">
                <thead>
                  <tr>
                    <th scope="col">User</th>
                    <th scope="col">Role</th>
                    <th scope="col">Status</th>
                    <th scope="col" style={{ textAlign: 'right' }}>Actions</th>
                  </tr>
                </thead>
                <tbody>
                  {(users.data ?? []).map(u => {
                    const isSelf = u.username === me?.username
                    return (
                      <tr key={u.username}>
                        <td>
                          <span className="user-cell">
                            <span className="user-avatar" aria-hidden="true">
                              {u.username.charAt(0).toUpperCase()}
                            </span>
                            <span className="user-cell-name">
                              {u.username}
                              {isSelf && <span className="tag">you</span>}
                            </span>
                            <div className="text-muted" style={{ fontSize: 11 }}>
                              created {new Date(u.created_at).toLocaleDateString()}
                            </div>
                          </span>
                        </td>
                        <td>
                          <select
                            className="form-select"
                            aria-label={`Role for ${u.username}`}
                            value={u.role}
                            disabled={isSelf || updateMut.isPending}
                            title={isSelf ? 'You cannot change your own role' : undefined}
                            onChange={e =>
                              updateMut.mutate({
                                username: u.username,
                                body: { role: e.target.value as Role },
                              })
                            }
                          >
                            {ROLE_ORDER.map(r => (
                              <option key={r} value={r}>{roleLabel(r)}</option>
                            ))}
                          </select>
                        </td>
                        <td>
                          <span className={`badge badge-${u.disabled ? 'rejected' : 'completed'}`}>
                            {u.disabled ? 'disabled' : 'active'}
                          </span>
                        </td>
                        <td style={{ textAlign: 'right', whiteSpace: 'nowrap' }}>
                          <button
                            className="btn btn-ghost btn-sm"
                            onClick={() => setResetFor(u)}
                          >
                            Reset password
                          </button>{' '}
                          <button
                            className="btn btn-ghost btn-sm"
                            disabled={isSelf || updateMut.isPending}
                            title={isSelf ? 'You cannot disable your own account' : undefined}
                            onClick={() =>
                              updateMut.mutate({ username: u.username, body: { disabled: !u.disabled } })
                            }
                          >
                            {u.disabled ? 'Enable' : 'Disable'}
                          </button>{' '}
                          <button
                            className="btn btn-ghost btn-sm"
                            disabled={isSelf || deleteMut.isPending}
                            title={isSelf ? 'You cannot delete your own account' : undefined}
                            onClick={() => setPendingDelete(u)}
                            style={{ color: 'var(--error)' }}
                          >
                            Delete
                          </button>
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </div>
          )}
        </div>
      </div>

      <ConfirmDialog
        open={pendingDelete !== null}
        title="Delete user"
        body={
          pendingDelete
            ? `Delete "${pendingDelete.username}"? They will no longer be able to sign in. This cannot be undone.`
            : ''
        }
        confirmLabel="Delete"
        danger
        loading={deleteMut.isPending}
        onCancel={() => setPendingDelete(null)}
        onConfirm={() => pendingDelete && deleteMut.mutate(pendingDelete.username)}
      />

      {resetFor && (
        <ResetPasswordDialog
          username={resetFor.username}
          busy={updateMut.isPending}
          onCancel={() => setResetFor(null)}
          onSubmit={password => {
            updateMut.mutate(
              { username: resetFor.username, body: { password } },
              { onSuccess: () => setResetFor(null) },
            )
          }}
        />
      )}
    </>
  )
}

// ─── Add user ─────────────────────────────────────────────────────────────────

function AddUserForm({
  busy, onSubmit, onCancel,
}: {
  busy: boolean
  onSubmit: (v: { username: string; password: string; role: Role }) => void
  onCancel: () => void
}) {
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [role, setRole] = useState<Role>('analyst')

  const valid = /^[a-zA-Z0-9._-]{1,64}$/.test(username.trim()) && password.length >= 6

  return (
    <form
      className="form-stack"
      onSubmit={e => {
        e.preventDefault()
        if (valid) onSubmit({ username: username.trim(), password, role })
      }}
    >
      <div className="form-row">
        <div className="form-group">
          <label className="form-label" htmlFor="new-username">Username</label>
          <input
            id="new-username"
            className="form-input"
            value={username}
            onChange={e => setUsername(e.target.value)}
            placeholder="e.g. j.rivera"
            autoComplete="off"
            required
          />
          <span className="form-hint">Letters, digits, dot, underscore or hyphen.</span>
        </div>
        <div className="form-group">
          <label className="form-label" htmlFor="new-password">Password</label>
          <input
            id="new-password"
            type="password"
            className="form-input"
            value={password}
            onChange={e => setPassword(e.target.value)}
            placeholder="At least 6 characters"
            autoComplete="new-password"
            required
          />
        </div>
        <div className="form-group">
          <label className="form-label" htmlFor="new-role">Role</label>
          <select
            id="new-role"
            className="form-select"
            value={role}
            onChange={e => setRole(e.target.value as Role)}
          >
            {ROLE_ORDER.map(r => (
              <option key={r} value={r}>{roleLabel(r)}</option>
            ))}
          </select>
          <span className="form-hint">{ROLES[role].summary}</span>
        </div>
      </div>
      <div className="form-actions">
        <button type="button" className="btn btn-secondary" onClick={onCancel} disabled={busy}>
          Cancel
        </button>
        <button type="submit" className="btn btn-primary" disabled={busy || !valid}>
          {busy ? 'Creating…' : 'Create user'}
        </button>
      </div>
    </form>
  )
}

// ─── Reset password ───────────────────────────────────────────────────────────

function ResetPasswordDialog({
  username, busy, onSubmit, onCancel,
}: {
  username: string
  busy: boolean
  onSubmit: (password: string) => void
  onCancel: () => void
}) {
  const [password, setPassword] = useState('')
  const valid = password.length >= 6

  return (
    <div
      className="dialog-backdrop"
      role="dialog"
      aria-modal="true"
      aria-labelledby="reset-title"
      onClick={e => { if (e.target === e.currentTarget) onCancel() }}
    >
      <form
        className="dialog"
        onSubmit={e => { e.preventDefault(); if (valid) onSubmit(password) }}
      >
        <h2 id="reset-title" className="dialog-title">Reset password</h2>
        <p className="dialog-body">
          Set a new password for <strong>{username}</strong>.
        </p>
        <div className="form-group">
          <label className="form-label" htmlFor="reset-password">New password</label>
          <input
            id="reset-password"
            type="password"
            className="form-input"
            autoFocus
            autoComplete="new-password"
            value={password}
            onChange={e => setPassword(e.target.value)}
            placeholder="At least 6 characters"
          />
        </div>
        <div className="dialog-actions">
          <button type="button" className="btn btn-secondary" onClick={onCancel} disabled={busy}>
            Cancel
          </button>
          <button type="submit" className="btn btn-primary" disabled={busy || !valid}>
            {busy ? 'Saving…' : 'Set password'}
          </button>
        </div>
      </form>
    </div>
  )
}
