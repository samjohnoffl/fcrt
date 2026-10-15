# UX Specification — Financial Crime AI Red Team

## Design Principles
1. **Research-first density** — Users are ML/FinCrime engineers; prefer data-dense tables over simplified cards
2. **Reproducibility visible** — Every result must show its run ID, seed, and rule version
3. **Progressive disclosure** — Experiment detail pages expand into raw JSON evidence on demand
4. **No irreversible actions without confirmation** — Cohort locking, experiment execution, scenario approval all require explicit confirmation dialogs

---

## Navigation Structure

```
/ (Dashboard)
  ├── /cohorts
  │     ├── /cohorts/new
  │     └── /cohorts/:id
  ├── /scenarios
  │     └── /scenarios/:id
  ├── /experiments
  │     ├── /experiments/new
  │     └── /experiments/:id
  │           └── /experiments/:id/evidence
  └── /coverage
```

---

## Pages

### Dashboard `/`
- Coverage gauge: fraction of AML rules exercised (ring chart)
- Recent experiments table: name, status, evasion delta, run timestamp
- Quick-action buttons: "New Cohort", "Request Scenario", "Run Experiment"

### Cohort List `/cohorts`
- Table: name, seed, predicate summary, lock status, entity count, created_at
- Filter by locked/unlocked
- Button: "New Cohort" → opens `/cohorts/new`

### Cohort Detail `/cohorts/:id`
- Cohort metadata header (read-only once locked)
- Entity/Account/Transaction counts with breakdown charts
- "Lock Cohort" button (confirmation required): disabled if already locked
- Entity graph preview (D3 force-directed, max 200 nodes rendered)

### Scenario List `/scenarios`
- Table: scenario type, cohort, status (pending/approved/rejected), created_at
- Status badge colours: pending=amber, approved=green, rejected=grey
- Filter by status

### Scenario Detail `/scenarios/:id`
- Full proposal text, evasion vector, suggested tx pattern (JSON viewer)
- Gemini prompt/response collapsible panel
- Approve / Reject buttons (confirmation required); disabled if not `pending`

### Experiment List `/experiments`
- Table: name, cohort, rule version, status, evasion delta, completed_at
- Status badge: pending=blue, running=amber, completed=green, failed=red

### Experiment Detail `/experiments/:id`
- Baseline vs. Variant comparison:
  - Alert counts side-by-side
  - Alert diff table: rule_id, baseline count, variant count, delta
  - Evasion delta highlight (positive delta = evasion detected)
- Mutation spec viewer (JSON)
- "View Evidence Bundle" button → `/experiments/:id/evidence`

### Evidence Bundle `/experiments/:id/evidence`
- Read-only JSON viewer (syntax highlighted)
- Download button: `evidence_<runid>.json`
- Checksum displayed; "Verify Checksum" button re-computes in-browser

### Coverage Dashboard `/coverage`
- Table: rule_id, rule_version, times_triggered, last_triggered_at, coverage_weight
- Bar chart: rule hit frequency
- Highlight rows where `times_triggered == 0` (uncovered rules)

---

## Component Library
All components are custom; no UI framework mandated. Use:
- CSS custom properties for theming (dark mode default)
- `react-query` for all data fetching
- `@tanstack/react-table` for sortable/filterable tables
- `recharts` for charts (ring, bar)
- A simple JSON viewer component (inline, no external dependency preferred)

---

## API Contract (Frontend → Backend)
All endpoints are `/api/v1/...` and return `application/json`.
Error shape: `{"error": "...", "code": "..."}`.
Auth header: `Authorization: Bearer <Firebase ID token>`.

| Method | Path | Description |
|---|---|---|
| GET | /api/v1/cohorts | List cohorts |
| POST | /api/v1/cohorts | Create cohort |
| GET | /api/v1/cohorts/:id | Get cohort detail |
| POST | /api/v1/cohorts/:id/lock | Lock cohort |
| GET | /api/v1/scenarios | List scenario proposals |
| POST | /api/v1/scenarios | Request Gemini scenario |
| GET | /api/v1/scenarios/:id | Get scenario detail |
| PATCH | /api/v1/scenarios/:id/approve | Approve scenario |
| PATCH | /api/v1/scenarios/:id/reject | Reject scenario |
| GET | /api/v1/experiments | List experiments |
| POST | /api/v1/experiments | Create experiment |
| GET | /api/v1/experiments/:id | Get experiment detail |
| GET | /api/v1/experiments/:id/evidence | Get evidence bundle |
| GET | /api/v1/coverage | Get coverage summary |

---

## Accessibility
- WCAG 2.1 AA minimum
- All interactive elements keyboard-navigable
- Tables have proper `aria-label` and `scope` headers
- Confirmation dialogs trap focus

---

## Unresolved UX Decisions
- **DECISION-007**: Entity graph visualisation library (D3 vs. Cytoscape.js)
- **DECISION-008**: Whether to support experiment comparison across multiple cohorts in a single view
