import { NavLink } from 'react-router-dom'

const NAV = [
  { to: '/',           icon: '⬡', label: 'Dashboard' },
  { to: '/cohorts',    icon: '⬛', label: 'Cohorts' },
  { to: '/scenarios',  icon: '✦', label: 'Scenarios' },
  { to: '/experiments',icon: '⚗', label: 'Experiments' },
  { to: '/coverage',   icon: '◎', label: 'Coverage' },
]

export function Layout({ children }: { children: React.ReactNode }) {
  return (
    <div className="layout">
      <aside className="sidebar">
        <div className="sidebar-header">
          <div className="sidebar-logo">A</div>
          <div>
            <div className="sidebar-title">AML Red Team</div>
            <div className="sidebar-subtitle">FinCrime AI</div>
          </div>
        </div>
        <nav className="sidebar-nav" aria-label="Main navigation">
          <div className="nav-section-label">Workspace</div>
          {NAV.map(({ to, icon, label }) => (
            <NavLink
              key={to}
              to={to}
              end={to === '/'}
              className={({ isActive }) => `nav-link${isActive ? ' active' : ''}`}
              aria-current={undefined}
            >
              <span className="nav-icon" aria-hidden="true">{icon}</span>
              {label}
            </NavLink>
          ))}
        </nav>
        <div className="sidebar-footer">v0.1.0 · gemini-1.5-pro</div>
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
      {actions && <div style={{ display: 'flex', gap: 8 }}>{actions}</div>}
    </header>
  )
}
