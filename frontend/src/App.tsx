import { lazy, Suspense } from 'react'
import { Routes, Route, Navigate } from 'react-router-dom'
import { Layout } from './components/Layout'
import { RequireAuth } from './components/RequireAuth'
import { RequireRole } from './components/RequireRole'
import { LoadingState } from './components/StateViews'
import { Login } from './pages/Login'

// Route-level code splitting: each page (and the charting library the dashboard
// uses) loads on first visit, keeping the initial bundle small.

const Dashboard         = lazy(() => import('./pages/Dashboard').then(m => ({ default: m.Dashboard })))
const DiscoveryOverview = lazy(() => import('./pages/DiscoveryOverview').then(m => ({ default: m.DiscoveryOverview })))
const CohortList        = lazy(() => import('./pages/CohortList').then(m => ({ default: m.CohortList })))
const CohortNew         = lazy(() => import('./pages/CohortNew').then(m => ({ default: m.CohortNew })))
const CohortDetail      = lazy(() => import('./pages/CohortDetail').then(m => ({ default: m.CohortDetail })))
const ScenarioList      = lazy(() => import('./pages/ScenarioList').then(m => ({ default: m.ScenarioList })))
const ScenarioDetail    = lazy(() => import('./pages/ScenarioDetail').then(m => ({ default: m.ScenarioDetail })))
const ExperimentList    = lazy(() => import('./pages/ExperimentList').then(m => ({ default: m.ExperimentList })))
const ExperimentNew     = lazy(() => import('./pages/ExperimentNew').then(m => ({ default: m.ExperimentNew })))
const ExperimentDetail  = lazy(() => import('./pages/ExperimentDetail').then(m => ({ default: m.ExperimentDetail })))
const EvidenceViewer    = lazy(() => import('./pages/EvidenceViewer').then(m => ({ default: m.EvidenceViewer })))
const CoverageDashboard = lazy(() => import('./pages/CoverageDashboard').then(m => ({ default: m.CoverageDashboard })))
const ReviewQueue       = lazy(() => import('./pages/ReviewQueue').then(m => ({ default: m.ReviewQueue })))
const CampaignList      = lazy(() => import('./pages/CampaignList').then(m => ({ default: m.CampaignList })))
const CampaignDetail    = lazy(() => import('./pages/CampaignDetail').then(m => ({ default: m.CampaignDetail })))
const HowTo             = lazy(() => import('./pages/HowTo').then(m => ({ default: m.HowTo })))
const BenchmarkLab      = lazy(() => import('./pages/BenchmarkLab').then(m => ({ default: m.BenchmarkLab })))
const Users             = lazy(() => import('./pages/Users').then(m => ({ default: m.Users })))
const AuditLog          = lazy(() => import('./pages/AuditLog').then(m => ({ default: m.AuditLog })))

function PageLoader() {
  return <LoadingState title="Loading…" />
}

export default function App() {
  return (
    <Routes>
      {/* Public */}
      <Route path="/login" element={<Login />} />

      {/* Everything else requires a session */}
      <Route
        path="*"
        element={
          <RequireAuth>
            <Layout>
              <Suspense fallback={<PageLoader />}>
                <Routes>
                  <Route path="/"                          element={<Dashboard />} />
                  <Route path="/discovery"                 element={<DiscoveryOverview />} />
                  <Route path="/cohorts"                   element={<CohortList />} />
                  <Route path="/cohorts/new"               element={<CohortNew />} />
                  <Route path="/cohorts/:id"               element={<CohortDetail />} />
                  <Route path="/scenarios"                 element={<ScenarioList />} />
                  <Route path="/scenarios/:id"             element={<ScenarioDetail />} />
                  <Route path="/experiments"               element={<ExperimentList />} />
                  <Route path="/experiments/new"           element={<ExperimentNew />} />
                  <Route path="/experiments/:id"           element={<ExperimentDetail />} />
                  <Route path="/experiments/:id/evidence"  element={<EvidenceViewer />} />
                  <Route path="/campaigns"                 element={<CampaignList />} />
                  <Route path="/campaigns/:id"             element={<CampaignDetail />} />
                  <Route path="/reviews"                   element={<ReviewQueue />} />
                  <Route path="/coverage"                  element={<CoverageDashboard />} />
                  <Route path="/benchmark"                 element={<BenchmarkLab />} />
                  <Route path="/how-to"                    element={<HowTo />} />

                  {/* Admin only */}
                  <Route path="/users" element={<RequireRole min="admin"><Users /></RequireRole>} />
                  <Route path="/audit" element={<RequireRole min="admin"><AuditLog /></RequireRole>} />

                  <Route path="*"                          element={<Navigate to="/" replace />} />
                </Routes>
              </Suspense>
            </Layout>
          </RequireAuth>
        }
      />
    </Routes>
  )
}
