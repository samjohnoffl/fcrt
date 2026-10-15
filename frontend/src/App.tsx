import { Routes, Route, Navigate } from 'react-router-dom'
import { Layout } from './components/Layout'
import { Dashboard }         from './pages/Dashboard'
import { CohortList }        from './pages/CohortList'
import { CohortNew }         from './pages/CohortNew'
import { CohortDetail }      from './pages/CohortDetail'
import { ScenarioList }      from './pages/ScenarioList'
import { ScenarioDetail }    from './pages/ScenarioDetail'
import { ExperimentList }    from './pages/ExperimentList'
import { ExperimentNew }     from './pages/ExperimentNew'
import { ExperimentDetail }  from './pages/ExperimentDetail'
import { EvidenceViewer }    from './pages/EvidenceViewer'
import { CoverageDashboard } from './pages/CoverageDashboard'

export default function App() {
  return (
    <Layout>
      <Routes>
        <Route path="/"                               element={<Dashboard />} />
        <Route path="/cohorts"                        element={<CohortList />} />
        <Route path="/cohorts/new"                    element={<CohortNew />} />
        <Route path="/cohorts/:id"                    element={<CohortDetail />} />
        <Route path="/scenarios"                      element={<ScenarioList />} />
        <Route path="/scenarios/:id"                  element={<ScenarioDetail />} />
        <Route path="/experiments"                    element={<ExperimentList />} />
        <Route path="/experiments/new"                element={<ExperimentNew />} />
        <Route path="/experiments/:id"                element={<ExperimentDetail />} />
        <Route path="/experiments/:id/evidence"       element={<EvidenceViewer />} />
        <Route path="/coverage"                       element={<CoverageDashboard />} />
        <Route path="*"                               element={<Navigate to="/" replace />} />
      </Routes>
    </Layout>
  )
}
