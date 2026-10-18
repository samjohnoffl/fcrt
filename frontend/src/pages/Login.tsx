// Login page — username/password sign-in.
//
// Calls POST /api/v1/auth/login, stores the returned session token, and returns
// the user to wherever they were heading. One-click fill is offered for the
// built-in test accounts, and the two roles are spelled out so it is obvious
// what each one can do.

import { useState } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { authApi } from '../api/endpoints'
import { FetchError } from '../api/client'
import { setSession } from '../lib/session'
import { ROLES, ROLE_ORDER } from '../lib/roles'
import type { Role } from '../api/types'

const TEST_ACCOUNTS: { username: string; password: string; role: Role }[] = [
  { username: 'admin',   password: 'admin',   role: 'admin' },
  { username: 'analyst', password: 'analyst', role: 'analyst' },
]

export function Login() {
  const navigate = useNavigate()
  const location = useLocation()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const from = (location.state as { from?: string } | null)?.from ?? '/'

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setError('')
    setBusy(true)
    try {
      const res = await authApi.login(username.trim(), password)
      setSession(res.token, res.user)
      navigate(from, { replace: true })
    } catch (err) {
      setError(err instanceof FetchError ? err.message : 'Sign-in failed. Please try again.')
    } finally {
      setBusy(false)
    }
  }

  function fill(u: string, p: string) {
    setUsername(u)
    setPassword(p)
    setError('')
  }

  return (
    <div className="login-screen">
      <div className="login-panel">
        <form className="card login-card" onSubmit={submit} aria-label="Sign in">
          <div className="login-brand">
            <div className="brand-mark" aria-hidden="true">A</div>
            <div>
              <div className="brand-title">AML Red Team</div>
              <div className="brand-subtitle">Financial Crime AI</div>
            </div>
          </div>

          <div className="form-stack">
            <div className="form-group">
              <label className="form-label" htmlFor="username">Username</label>
              <input
                id="username"
                className="form-input"
                autoComplete="username"
                autoFocus
                value={username}
                onChange={e => setUsername(e.target.value)}
                disabled={busy}
                required
              />
            </div>

            <div className="form-group">
              <label className="form-label" htmlFor="password">Password</label>
              <input
                id="password"
                type="password"
                className="form-input"
                autoComplete="current-password"
                value={password}
                onChange={e => setPassword(e.target.value)}
                disabled={busy}
                required
              />
            </div>

            {error && <div className="form-error" role="alert">{error}</div>}

            <button className="btn btn-primary btn-block" type="submit" disabled={busy}>
              {busy ? 'Signing in…' : 'Sign in'}
            </button>
          </div>

          <div className="login-demo">
            <div className="login-demo-label">Test accounts</div>
            <div className="login-demo-buttons">
              {TEST_ACCOUNTS.map(a => (
                <button
                  key={a.username}
                  type="button"
                  className="btn btn-secondary btn-sm"
                  onClick={() => fill(a.username, a.password)}
                  disabled={busy}
                >
                  {a.username} <span className="text-muted">({ROLES[a.role].label})</span>
                </button>
              ))}
            </div>
          </div>
        </form>

        <aside className="login-roles" aria-label="Role differences">
          <h2 className="login-roles-title">What each role can do</h2>
          {ROLE_ORDER.map(id => {
            const role = ROLES[id]
            return (
              <div key={id} className="role-card">
                <div className="role-card-head">
                  <span className={`role-badge role-${id}`}>{role.label}</span>
                </div>
                <p className="role-card-summary">{role.summary}</p>
                <ul className="role-card-list">
                  {role.capabilities.map(c => <li key={c}>{c}</li>)}
                </ul>
              </div>
            )
          })}
        </aside>
      </div>
    </div>
  )
}
