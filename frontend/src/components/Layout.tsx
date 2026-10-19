import { NavLink, useNavigate } from 'react-router-dom'
import { getUser, hasRole, clearSession } from '../lib/session'
import { roleLabel } from '../lib/roles'
import { authApi } from '../api/endpoints'

// ─── Icons ────────────────────────────────────────────────────────────────────
// Inline, stroke-based, currentColor — no icon dependency, no emoji.

type IconName =
  | 'dashboard' | 'discovery' | 'cohorts' | 'scenarios' | 'experiments'
  | 'coverage' | 'benchmark' | 'reviews' | 'users' | 'audit'

const SVG = {
  fill: 'none',
  stroke: 'currentColor',
  strokeWidth: 1.8,
  strokeLinecap: 'round' as const,
  strokeLinejoin: 'round' as const,
}

const ICONS: Record<IconName, JSX.Element> = {
  dashboard: (
    <svg viewBox="0 0 24 24" width="16" height="16" {...SVG}>
      <rect x="3" y="3" width="7.5" height="7.5" rx="2" />
      <rect x="13.5" y="3" width="7.5" height="7.5" rx="2" />
      <rect x="3" y="13.5" width="7.5" height="7.5" rx="2" />
      <rect x="13.5" y="13.5" width="7.5" height="7.5" rx="2" />
    </svg>
  ),
  discovery: (
    <svg viewBox="0 0 24 24" width="16" height="16" {...SVG}>
      <circle cx="11" cy="11" r="7" />
      <path d="m20 20-3.6-3.6" />
    </svg>
  ),
  cohorts: (
    <svg viewBox="0 0 24 24" width="16" height="16" {...SVG}>
      <path d="M12 3 3 8l9 5 9-5-9-5Z" />
      <path d="m3 13 9 5 9-5" />
    </svg>
  ),
  scenarios: (
    <svg viewBox="0 0 24 24" width="16" height="16" {...SVG}>
      <path d="M12 4l1.7 4.6L18 10l-4.3 1.4L12 16l-1.7-4.6L6 10l4.3-1.4L12 4Z" />
      <path d="M18.5 16.5l.7 1.8 1.8.7-1.8.7-.7 1.8-.7-1.8-1.8-.7 1.8-.7.7-1.8Z" />
    </svg>
  ),
  experiments: (
    <svg viewBox="0 0 24 24" width="16" height="16" {...SVG}>
      <path d="M9.5 3h5M10.5 3v6.2L5.6 17.4A2 2 0 0 0 7.3 20.5h9.4a2 2 0 0 0 1.7-3.1L13.5 9.2V3" />
      <path d="M8 14h8" />
    </svg>
  ),
  coverage: (
    <svg viewBox="0 0 24 24" width="16" height="16" {...SVG}>
      <path d="M12 3l8 3v5.5c0 5-3.4 8.3-8 9.2-4.6-.9-8-4.2-8-9.2V6l8-3Z" />
      <path d="m9 12 2 2 4-4" />
    </svg>
  ),
  benchmark: (
    <svg viewBox="0 0 24 24" width="16" height="16" {...SVG}>
      <path d="M3.5 20.5h17" />
      <rect x="4.5" y="11" width="3.6" height="6.5" rx="1" />
      <rect x="10.2" y="6.5" width="3.6" height="11" rx="1" />
      <rect x="15.9" y="14" width="3.6" height="3.5" rx="1" />
    </svg>
  ),
  reviews: (
    <svg viewBox="0 0 24 24" width="16" height="16" {...SVG}>
      <circle cx="12" cy="12" r="8.5" />
      <path d="m8.2 12.2 2.5 2.5 5-5" />
    </svg>
  ),
  users: (
    <svg viewBox="0 0 24 24" width="16" height="16" {...SVG}>
      <circle cx="12" cy="8" r="3.6" />
      <path d="M5 20a7 7 0 0 1 14 0" />
    </svg>
  ),
  audit: (
    <svg viewBox="0 0 24 24" width="16" height="16" {...SVG}>
      <path d="M6 3h8.5L19 7.5V21H6z" />
      <path d="M14 3v5h5" />
      <path d="M9.5 12.5h6M9.5 16h6" />
    </svg>
  ),
}

function NavIcon({ name }: { name: IconName }) {
  return <span className="nav-icon" aria-hidden="true">{ICONS[name]}</span>
}

// ─── Navigation ───────────────────────────────────────────────────────────────

const WORKSPACE_NAV: { to: string; icon: IconName; label: string }[] = [
  { to: '/',            icon: 'dashboard',   label: 'Dashboard' },
  { to: '/cohorts',     icon: 'cohorts',     label: 'Cohorts' },
  { to: '/scenarios',   icon: 'scenarios',   label: 'Scenarios' },
  { to: '/experiments', icon: 'experiments', label: 'Experiments' },
  { to: '/reviews',     icon: 'reviews',     label: 'Review Queue' },
  { to: '/coverage',    icon: 'coverage',    label: 'Coverage' },
  { to: '/discovery',   icon: 'discovery',   label: 'Discovery' },
  { to: '/benchmark',   icon: 'benchmark',   label: 'Benchmark Lab' },
]

const ADMIN_NAV: { to: string; icon: IconName; label: string }[] = [
  { to: '/users', icon: 'users', label: 'Users' },
  { to: '/audit', icon: 'audit', label: 'Audit log' },
]

export function Layout({ children }: { children: React.ReactNode }) {
  const navigate = useNavigate()
  const user = getUser()

  function signOut() {
    // Best-effort server call (stateless token; failure is harmless).
    void authApi.logout().catch(() => undefined).finally(() => {
      clearSession()
      navigate('/login', { replace: true })
    })
  }

  return (
    <div className="layout">
      <aside className="sidebar">
        <div className="sidebar-header">
          <div className="brand-mark">A</div>
          <div>
            <div className="brand-title">AML Red Team</div>
            <div className="brand-subtitle">Financial Crime AI</div>
          </div>
        </div>

        <nav className="sidebar-nav" aria-label="Main navigation">
          <div className="nav-section-label">Workspace</div>
          {WORKSPACE_NAV.map(({ to, icon, label }) => (
            <NavLink
              key={to}
              to={to}
              end={to === '/'}
              className={({ isActive }) => `nav-link${isActive ? ' active' : ''}`}
            >
              <NavIcon name={icon} />
              {label}
            </NavLink>
          ))}

          {hasRole('admin') && (
            <>
              <div className="nav-section-label">Administration</div>
              {ADMIN_NAV.map(({ to, icon, label }) => (
                <NavLink
                  key={to}
                  to={to}
                  className={({ isActive }) => `nav-link${isActive ? ' active' : ''}`}
                >
                  <NavIcon name={icon} />
                  {label}
                </NavLink>
              ))}
            </>
          )}
        </nav>

        <div className="sidebar-user">
          {user && (
            <>
              <div className="sidebar-user-avatar" aria-hidden="true">
                {user.username.charAt(0).toUpperCase()}
              </div>
              <div className="sidebar-user-meta">
                <div className="sidebar-user-name" title={user.username}>{user.username}</div>
                <div className={`role-badge role-${user.role}`}>{roleLabel(user.role)}</div>
              </div>
            </>
          )}
          <button className="btn btn-ghost btn-sm sidebar-signout" onClick={signOut}>
            Sign out
          </button>
        </div>
      </aside>
      <div className="main-content">{children}</div>
    </div>
  )
}

export function PageHeader({
  title, subtitle, actions,
}: {
  title: string
  subtitle?: string
  actions?: React.ReactNode
}) {
  return (
    <header className="page-header">
      <div>
        <h1 className="page-title">{title}</h1>
        {subtitle && <p className="page-subtitle">{subtitle}</p>}
      </div>
      {actions && <div className="page-header-actions">{actions}</div>}
    </header>
  )
}
