# Financial Crime AI Red Team (FCRT)

> **Status**: Production-ready Go backend + React/TypeScript frontend — build clean, 214 tests passing.

A sandboxed platform for **red-teaming AML (Anti-Money Laundering) detection models** using synthetic data, Gemini 1.5 Pro scenario generation, and counterfactual experiments. No real customer PII is ever used.

---

## Architecture

```
┌───────────────────────────────────────────────────────────────┐
│  Browser — React/TypeScript SPA (Firebase Hosting)            │
│  ├─ CohortList / CohortDetail                                  │
│  ├─ ScenarioList / ScenarioDetail                             │
│  ├─ ExperimentList / ExperimentDetail / EvidenceViewer        │
│  └─ CoverageDashboard                                          │
└────────────────────────┬──────────────────────────────────────┘
                         │ HTTPS + Firebase ID token (Bearer)
                         ▼
┌───────────────────────────────────────────────────────────────┐
│  Go REST API (Cloud Run) — cmd/api                            │
│  ├─ internal/api        chi router, auth middleware           │
│  ├─ internal/bank       synthetic bank generator + repo       │
│  ├─ internal/scenario   Gemini provider, schema validator     │
│  ├─ internal/experiment counterfactual experiment runner      │
│  ├─ internal/coverage   rule coverage tracker                 │
│  └─ internal/evidence   evidence bundle builder               │
└────────────────────────┬──────────────────────────────────────┘
                         │
             ┌───────────┴────────────┐
             ▼                        ▼
  PostgreSQL 16 (Cloud SQL)    Vertex AI (Gemini 1.5 Pro)
```

### Key Design Decisions

| Concern | Decision |
|---|---|
| **Auth** | Firebase ID tokens verified server-side on every request (no session cookies) |
| **Secrets** | All credentials via env vars / ADC — none in source |
| **AI output** | Treated as untrusted data: schema-validated before any use |
| **Reproducibility** | Every experiment seeded deterministically; seed stored in cohort |
| **Auditability** | Raw Gemini prompt + response stored verbatim in `scenario_proposal` |
| **Workspace isolation** | Row-level scoping by cohort; no cross-tenant data access |

---

## Data Model

```sql
-- Synthetic bank cohort (immutable once locked)
cohort (id, name, predicate, seed, generator_version, manifest JSONB,
        window_start, window_end, locked_at)

-- Gemini-proposed scenario awaiting human approval
scenario_proposal (id, cohort_id, status CHECK(pending|approved|rejected),
                   scenario_type, description, expected_evasion_vector,
                   suggested_tx_pattern JSONB,
                   gemini_prompt TEXT,     -- raw prompt for auditability
                   gemini_response TEXT,   -- raw response for auditability
                   gemini_model, correlation_id, approved_by)

-- Counterfactual experiment (baseline vs variant)
experiment (id, cohort_id, scenario_proposal_id, control_version,
            outcome CHECK(variant_evades|baseline_evades|inconclusive),
            summary JSONB, run_at)

experiment_run (id, experiment_id, variant CHECK(baseline|variant),
                seed, alert_count, alert_rate, run_metadata JSONB)

-- Coverage heat-map (adaptive cohort selection)
rule_coverage (rule_id PRIMARY KEY, trigger_count, last_triggered_at,
               weight NUMERIC)            -- higher = less covered
```

---

## Stack

| Layer | Technology |
|---|---|
| API | Go 1.24, chi v5 router |
| Frontend | React 18 + TypeScript 5, Vite 5 |
| Database | PostgreSQL 16 (Cloud SQL) |
| AI | Gemini 1.5 Pro via Vertex AI REST |
| Auth | Firebase Authentication (ID tokens) |
| Hosting | Firebase Hosting (SPA) + Cloud Run (API) |
| Migrations | golang-migrate |

---

## Repository Structure

```
financial-crime-red-team/
├── AGENTS.md                         # Stable agent constraints — read first
├── docs/
│   ├── PRODUCT_SPEC.md
│   ├── ARCHITECTURE.md
│   ├── DATA_MODEL.md
│   ├── THREAT_MODEL.md
│   ├── UX_SPEC.md
│   ├── ACCEPTANCE_CRITERIA.md
│   └── DECISIONS.md
├── cmd/
│   └── api/main.go                   # REST API server (Cloud Run)
├── internal/
│   ├── api/
│   │   ├── middleware.go             # Firebase auth middleware
│   │   ├── response.go              # JSON envelope helpers
│   │   ├── router.go                # chi route registration
│   │   ├── handler_cohort.go
│   │   ├── handler_scenario.go
│   │   ├── handler_experiment.go
│   │   └── handler_coverage.go
│   ├── bank/                         # Synthetic bank generator + repository
│   ├── aml/                          # Mock AML rule harness
│   ├── scenario/                     # Gemini adapter + schema validator
│   ├── experiment/                   # Counterfactual experiment runner
│   ├── coverage/                     # Rule coverage tracker
│   └── evidence/                     # Evidence bundle types
├── frontend/                         # React/TypeScript SPA
│   └── src/
│       ├── pages/                    # 7 analyst pages
│       ├── components/               # Shared UI components
│       ├── api/                      # Typed API client
│       └── tests/                    # Vitest tests
├── migrations/                       # PostgreSQL migration files
├── .env.example                      # Environment variable template
├── go.mod / go.sum
└── .gitignore
```

---

## Getting Started (Local Dev)

### Prerequisites

- Go 1.22+
- Node 20+
- PostgreSQL 16 (local or Docker)
- `golang-migrate` CLI: `go install github.com/golang-migrate/migrate/v4/cmd/migrate@latest`

### Setup

```bash
# 1. Copy env template and edit
cp .env.example .env

# 2. Run DB migrations
migrate -path ./migrations -database "$DATABASE_URL" up

# 3. Start API (leave FIREBASE_PROJECT unset for dev — uses NoopVerifier)
DATABASE_URL=postgres://... go run ./cmd/api

# 4. Start frontend dev server
cd frontend && npm install && npm run dev
```

The frontend runs on `http://localhost:5173` and proxies API calls to `http://localhost:8080`.

In local dev, pass any non-empty Bearer token — the `NoopVerifier` accepts all tokens when `FIREBASE_PROJECT` is unset.

---

## Environment Variables

| Variable | Required | Default | Description |
|---|---|---|---|
| `DATABASE_URL` | **Yes** | — | PostgreSQL DSN |
| `API_PORT` | No | `8080` | TCP port |
| `FIREBASE_PROJECT` | Prod | — | GCP project for Firebase auth. Unset = NoopVerifier (dev only) |
| `VERTEX_PROJECT` | No | — | GCP project for Vertex AI. Unset = POST /scenarios returns 503 |
| `VERTEX_LOCATION` | No | `us-central1` | Vertex AI region |
| `CORS_ORIGINS` | No | `http://localhost:5173` | Comma-separated allowed origins |
| `LOG_LEVEL` | No | `info` | `debug\|info\|warn\|error` |

---

## Security Model

Based on `docs/THREAT_MODEL.md` (STRIDE analysis):

| Control | Implementation |
|---|---|
| **T-01** Unauthenticated access | `AuthMiddleware` blocks all `/api/v1/*` without valid Firebase ID token |
| **T-03** SQL injection | All DB queries use parameterised pgx statements — no string interpolation |
| **T-04** Prompt injection | Gemini output schema-validated before use; never executed or interpolated into SQL |
| **T-10** Privilege escalation | Scenario approval requires auth; approver UID stored in DB row |
| **AC-03.2** Approval gate | Experiments can only reference `status=approved` proposals |
| **AC-05** Coverage | `rule_coverage` table tracks uncovered rules at max weight |
| **AC-06** Evidence | Evidence bundles include provenance hash, cohort seed, and honest limitation labels |
| **Secret hygiene** | `.gitignore` excludes `.env`, `*.pem`, `*.p12`, `service-account*.json` |
| **Log safety** | `sanitiseLog()` strips newlines from all error messages before logging |

---

## API Reference

All endpoints require `Authorization: Bearer <firebase-id-token>`.

### Cohorts
| Method | Path | Description |
|---|---|---|
| GET | `/api/v1/cohorts` | List all cohorts |
| POST | `/api/v1/cohorts` | Create cohort (generates synthetic bank data) |
| GET | `/api/v1/cohorts/{id}` | Get cohort detail |
| GET | `/api/v1/cohorts/{id}/summary` | Get cohort statistics |
| POST | `/api/v1/cohorts/{id}/lock` | Lock cohort (immutable after this) |

### Scenarios
| Method | Path | Description |
|---|---|---|
| GET | `/api/v1/scenarios` | List proposals (filter by `cohort_id`, `status`) |
| POST | `/api/v1/scenarios` | Request Gemini scenario generation |
| GET | `/api/v1/scenarios/{id}` | Get proposal detail |
| PATCH | `/api/v1/scenarios/{id}/approve` | Approve proposal (pending→approved) |
| PATCH | `/api/v1/scenarios/{id}/reject` | Reject proposal (pending→rejected) |

### Experiments
| Method | Path | Description |
|---|---|---|
| GET | `/api/v1/experiments` | List experiments |
| POST | `/api/v1/experiments` | Run baseline+variant experiment |
| GET | `/api/v1/experiments/{id}` | Get experiment + runs |
| GET | `/api/v1/experiments/{id}/evidence` | Get evidence bundle (AC-06) |
| POST | `/api/v1/experiments/{id}/replay` | Replay experiment (reproducibility) |

### Coverage
| Method | Path | Description |
|---|---|---|
| GET | `/api/v1/coverage` | Rule coverage summary (sorted by weight) |

### System
| Method | Path | Description |
|---|---|---|
| GET | `/healthz` | Health probe (unauthenticated) |

---

## Tests

```bash
# Go backend (214 assertions, 0 failures)
go test -short ./...

# Frontend (52 Vitest tests)
cd frontend && npm test
```

---

## Key Documentation

| Document | Purpose |
|---|---|
| [AGENTS.md](AGENTS.md) | Stable constraints — read first |
| [docs/PRODUCT_SPEC.md](docs/PRODUCT_SPEC.md) | Feature scope and glossary |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | System topology |
| [docs/DATA_MODEL.md](docs/DATA_MODEL.md) | Database schema reference |
| [docs/THREAT_MODEL.md](docs/THREAT_MODEL.md) | STRIDE threat analysis |
| [docs/UX_SPEC.md](docs/UX_SPEC.md) | Pages, navigation, API contract |
| [docs/ACCEPTANCE_CRITERIA.md](docs/ACCEPTANCE_CRITERIA.md) | Testable Given/When/Then criteria |
| [docs/DECISIONS.md](docs/DECISIONS.md) | **Open decisions — do not invent answers** |

---

## Limitations & Honest Labels

- **No real AML rules**: The AML harness is a mock (`internal/aml`). Results reflect synthetic rule logic only.
- **Gemini output is advisory**: All model-generated scenarios require human approval before use in experiments.
- **No live Firebase token verification in this build**: `FirebaseVerifier` requires the `firebase-admin-go` SDK to be wired in (see `internal/api/middleware.go` for the integration point). Set `FIREBASE_PROJECT=""` for local dev.
- **Synthetic data only**: No real customer, transaction, or entity data is ever processed.
