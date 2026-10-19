# Financial Crime AI Red Team (FCRT)

> **Status**: Production-ready Go backend + React/TypeScript frontend — build clean, 404 automated tests passing (300 Go + 104 Vitest).

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
                         │ HTTPS + Bearer session token
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
| **AuthN** | Username/password login → HMAC-signed session token (no cookies); `AUTH_MODE=firebase` swaps in Firebase ID tokens (see DECISION-011) |
| **AuthZ** | Two hierarchical roles on every route: `analyst` (full workspace) < `admin` (+ user management, audit log) |
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

-- Coverage heat-map (adaptive cohort selection), three separate maps
rule_coverage (rule_id PRIMARY KEY, trigger_count, last_triggered_at,
               weight NUMERIC, times_not_triggered, times_not_applicable,
               times_inconclusive, times_failed, last_status)
outcome_coverage (rule_id, status, times_seen)        -- rule × five-status cross-tab
scenario_coverage (dimension, value, times_seen)      -- genome dimension × value

-- Control outcomes (one row per control per run)
rule_outcome (run_id, pair_id, rule_id, rule_version, status, reason_code,
              event_ids UUID[], account_ids UUID[], entity_ids UUID[])

-- Campaigns and reviewer decisions
campaign (id, name, goal, cohort_id, budget, experiments_used, status)
review_decision (pair_id UNIQUE, reviewer, decision, rationale)
```

---

## Stack

| Layer | Technology |
|---|---|
| API | Go 1.24, chi v5 router |
| Frontend | React 18 + TypeScript 5, Vite 5 |
| Database | PostgreSQL 16 (Cloud SQL) |
| AI | Gemini 1.5 Pro via Vertex AI REST |
| Auth | Username/password session tokens (default), Firebase ID tokens (optional) |
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
│   ├── api/main.go                   # REST API server (Cloud Run)
│   ├── worker/main.go                # Background job worker (SKIP LOCKED)
│   └── migrate/main.go               # golang-migrate wrapper (/migrate in the image)
├── internal/
│   ├── api/
│   │   ├── middleware.go             # Bearer auth middleware (verifier-agnostic)
│   │   ├── authz.go                  # Roles, Principal, RequireRole middleware
│   │   ├── auth_session.go           # Users, session tokens, /auth handlers
│   │   ├── response.go              # JSON envelope helpers
│   │   ├── router.go                # chi route registration
│   │   ├── handler_cohort.go
│   │   ├── handler_scenario.go
│   │   ├── handler_experiment.go
│   │   ├── handler_coverage.go
│   │   ├── handler_campaign.go       # campaigns + benchmark
│   │   ├── handler_regression.go     # regression test pack
│   │   └── handler_audit.go
│   ├── bank/                         # Synthetic bank generator + repository
│   ├── aml/                          # Mock AML rule harness
│   ├── scenario/                     # Gemini adapter + schema validator
│   ├── experiment/                   # Counterfactual experiment runner
│   ├── coverage/                     # Rule coverage tracker
│   ├── evidence/                     # Evidence bundle types + PostgreSQL repository
│   ├── job/                          # DB-backed work queue (DECISION-004)
│   ├── regression/                   # Reproducible regression test packs
│   ├── genome/                       # Genome encoding + diversity selection
│   ├── payment/                      # Payment intelligence / AML features
│   ├── benchmark/                    # Strategy comparison harness
│   └── audit/                        # Audit event log (DECISION-006)
├── frontend/                         # React/TypeScript SPA
│   └── src/
│       ├── pages/                    # Analyst pages (incl. Discovery, Benchmark Lab)
│       ├── components/               # Shared UI components
│       ├── lib/                      # Pure analytics helpers (discovery.ts)
│       ├── api/                      # Typed API client
│       └── tests/                    # Vitest tests
├── migrations/                       # PostgreSQL migration files (golang-migrate)
├── scripts/gcp-setup.sh              # GCP provisioning: Cloud SQL, Run, secrets, hosting
├── firebase.json                     # Firebase Hosting (SPA + /api/** rewrite to Cloud Run)
├── docker-compose.yml                # Local dev stack: postgres + migrate + api + worker
├── Dockerfile                        # API + worker + migrate image (distroless)
├── Makefile                          # Developer entry points (make help)
├── .dockerignore
├── .gcloudignore
├── .env.example                      # Environment variable template
├── go.mod / go.sum
└── .gitignore
```

---

## Getting Started (Local Dev)

### Option A — Docker Compose (fastest)

Brings up PostgreSQL 16, runs migrations, and starts the API + worker — no local toolchain needed.

```bash
# 1. Start the stack (postgres + migrate + api + worker)
make up
#    or: docker compose up --build -d

# 2. The API is now live (local test credentials: admin/admin)
curl -s localhost:8080/healthz
#    {"data":{"status":"ok"}}

# 3. Get a session token, then call an authenticated endpoint
TOKEN=$(curl -s -X POST localhost:8080/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin"}' | sed -E 's/.*"token":"([^"]+)".*/\1/')
curl -s -H "Authorization: Bearer $TOKEN" localhost:8080/api/v1/cohorts

# 4. Tail logs / stop
make logs
make down           # add -v via: docker compose down -v  (wipes the DB volume)
```

If host port 5432 or 8080 is already taken, override the published ports:

```bash
FCRT_PG_PORT=5433 FCRT_API_PORT=8081 docker compose up --build -d
```

The API runs with `AUTH_MODE=local`, so the built-in test accounts work
(`admin/admin` → Administrator, `analyst/analyst` → Analyst). They are seeded
into the `app_user` table on first boot; after that, manage accounts in the app
under **Administration → Users**. Start the frontend separately (Option B step
5) — it proxies `/api` to the API.

### Option B — Local toolchain

### Prerequisites

- Go 1.22+
- Node 20+
- PostgreSQL 16 (local or Docker)

No external migrate CLI is required — the repo ships one:

```bash
go run ./cmd/migrate up        # or: /migrate up (inside the image)
```

### Setup

```bash
# 1. Copy env template and edit
cp .env.example .env

# 2. Run DB migrations (uses the in-repo golang-migrate wrapper)
DATABASE_URL=postgres://... go run ./cmd/migrate up

# 3. Start API (auth defaults to AUTH_MODE=local with test accounts)
DATABASE_URL=postgres://... go run ./cmd/api

# 4. Start the worker (cohort generation + experiment jobs)
DATABASE_URL=postgres://... go run ./cmd/worker

# 5. Start frontend dev server
cd frontend && npm install && npm run dev
```

`make help` lists all developer targets (build, test, vet, up/down, migrate…).

`cmd/migrate` accepts `up`, `down` (one step), `reset` (all the way down) and
`version`. It reads `DATABASE_URL` and `MIGRATIONS_DIR` (default `/migrations`)
and keeps the same `schema_migrations` bookkeeping as the official
`migrate/migrate` image, so it can take over an existing database.

The frontend runs on `http://localhost:5173` and proxies API calls to `http://localhost:8080`.

In local dev the API issues its own session tokens. Sign in to obtain one:

```bash
TOK="Authorization: Bearer $(curl -s -X POST localhost:8080/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin"}' | sed -E 's/.*"token":"([^"]+)".*/\1/')"
echo "$TOK"
```

The `admin` account can do everything; `analyst` has the full red-team workflow
but no access to user management or the audit log. Alternatively set
`AUTH_MODE=none` to bypass auth entirely (local dev only).

### Who can do what

| Capability | `analyst` | `admin` |
|---|:---:|:---:|
| View dashboard, cohorts, scenarios, experiments, coverage | ✅ | ✅ |
| Create/lock cohorts, request & approve scenarios, run experiments | ✅ | ✅ |
| Verify/export the regression pack | ✅ | ✅ |
| Manage users (create, role, reset password, disable, delete) | — | ✅ |
| Read the audit log | — | ✅ |

The difference is enforced twice: server-side by `RequireRole` middleware on
every route, and in the UI (the **Administration** nav section and its pages are
hidden for analysts; visiting `/users` or `/audit` redirects to the dashboard).
Both roles are documented on the sign-in screen and on the Users page.

### Smoke test (end-to-end)

```bash
TOK="Authorization: Bearer $(curl -s -X POST localhost:8080/api/v1/auth/login \
  -H 'Content-Type: application/json' -d '{"username":"admin","password":"admin"}' \
  | sed -E 's/.*"token":"([^"]+)".*/\1/')"
# Create a cohort → enqueues a generation job
curl -s -X POST localhost:8080/api/v1/cohorts -H "$TOK" -H 'Content-Type: application/json' \
  -d '{"name":"Demo","seed":42,"predicate":{},"window_days":90}'
# Wait for the worker to generate + lock it, then run an experiment
curl -s -X POST localhost:8080/api/v1/experiments -H "$TOK" -H 'Content-Type: application/json' \
  -d '{"cohort_id":"<ID>","name":"Split","declared_change":"split high-value tx",
       "mutation_type":"split_transaction","mutation_spec":{"split_count":3,"threshold_cents":100000}}'
# Export the reproducible regression pack
curl -s localhost:8080/api/v1/regression/pack -H "$TOK" -o regression-pack.json
```

---

## Environment Variables

| Variable | Required | Default | Description |
|---|---|---|---|
| `DATABASE_URL` | **Yes** | — | PostgreSQL DSN |
| `API_PORT` | No | `8080` | TCP port |
| `AUTH_MODE` | No | `local` | `local` (username/password) \| `firebase` (ID tokens) \| `none` (dev only) |
| `AUTH_USERS` | No | `admin:admin:admin,analyst:analyst:analyst` | Seeds `app_user` on first boot (`username:password:role`); never clobbers existing accounts |
| `AUTH_SESSION_SECRET` | Prod | dev value | HMAC key that signs session tokens — set a long random value |
| `AUTH_SESSION_TTL_HOURS` | No | `12` | Session token lifetime |
| `FIREBASE_PROJECT` | `firebase` mode | — | GCP project for Firebase ID-token verification |
| `GEMINI_API_KEY` | No | — | Google AI Studio key — preferred; takes precedence over `VERTEX_PROJECT`. Unset = `POST /scenarios` returns 503 |
| `GEMINI_MODEL` | No | `gemini-3.8-flash` | Model override for AI scenario generation |
| `VERTEX_PROJECT` | No | — | GCP project for Vertex AI (fallback when no API key) |
| `VERTEX_LOCATION` | No | `us-central1` | Vertex AI region |
| `CORS_ORIGINS` | No | `http://localhost:5173` | Comma-separated allowed origins |
| `LOG_LEVEL` | No | `info` | `debug\|info\|warn\|error` |

---

## Security Model

Based on `docs/THREAT_MODEL.md` (STRIDE analysis):

| Control | Implementation |
|---|---|
| **T-01** Unauthenticated access | `AuthMiddleware` blocks all `/api/v1/*` without a valid Bearer token (only `/auth/login` and `/healthz` are public) |
| **T-01a** Authorization | `RequireRole` enforces reader/writer/admin roles per route; role gating is server-side |
| **T-03** SQL injection | All DB queries use parameterised pgx statements — no string interpolation |
| **T-04** Prompt injection | Gemini output schema-validated before use; never executed or interpolated into SQL |
| **T-10** Privilege escalation | Scenario approval requires `analyst`+; approver identity stored in DB row and audit log |
| **AC-03.2** Approval gate | Experiments can only reference `status=approved` proposals |
| **AC-05** Coverage | Three maps: `rule_coverage` (control statuses), `outcome_coverage` (rule × status), `scenario_coverage` (genome dimension × value) |
| **AC-06** Evidence | Evidence bundles include provenance hash, cohort seed, and honest limitation labels |
| **Login audit** | Every login attempt (success/failure) is written to `audit_event` |
| **Secret hygiene** | `.gitignore` excludes `.env`, `*.pem`, `*.p12`, `service-account*.json` |
| **Log safety** | `sanitiseLog()` strips newlines from all error messages before logging |

---

## API Reference

All endpoints require `Authorization: Bearer <token>`, where `<token>` is the
`token` returned by `POST /api/v1/auth/login`. Exceptions: `/auth/login` and
`/healthz` are public.

### Authentication

| Method | Path | Min role | Description |
|---|---|---|---|
| POST | `/api/v1/auth/login` | public | `{username,password}` → `{token, expires_at, user:{username,role}}` |
| GET | `/api/v1/auth/me` | analyst | Current principal (`{username, role}`) |
| POST | `/api/v1/auth/logout` | analyst | No-op for stateless tokens (client discards it) |

### User management (admin only)

| Method | Path | Description |
|---|---|---|
| GET | `/api/v1/users` | List accounts |
| POST | `/api/v1/users` | Create `{username,password,role}` |
| PATCH | `/api/v1/users/{username}` | Update `{role?,password?,disabled?}` |
| DELETE | `/api/v1/users/{username}` | Delete an account |

Passwords must be at least 6 characters. An admin cannot disable, demote or
delete **their own** account (self-lockout guard) — changing your own password
is allowed. Every create/update/delete is written to the audit log.

**Roles** (hierarchical — `admin` includes everything `analyst` can do):

| Role | Can do |
|---|---|
| `analyst` | The whole workspace: read everything, create/lock cohorts, request/approve/reject scenarios, run/replay experiments, verify & export the regression pack |
| `admin` | analyst + user management + read the audit log |

Every authenticated route requires at least `analyst`; `/users` and `/audit`
require `admin`.
Violations return `403 insufficient_role`; missing/invalid tokens return
`401 missing_token` / `401 invalid_token` / `401 token_expired`.

### Cohorts
| Method | Path | Description |
|---|---|---|
| GET | `/api/v1/cohorts` | List all cohorts |
| POST | `/api/v1/cohorts` | Create cohort (generates synthetic bank data) |
| GET | `/api/v1/cohorts/{id}` | Get cohort detail |
| GET | `/api/v1/cohorts/{id}/summary` | Get cohort statistics |
| POST | `/api/v1/cohorts/{id}/lock` | Lock cohort (immutable after this) |
| POST | `/api/v1/cohorts/{id}/discovery` | Select the next candidates with a persisted, explainable score (`{strategy?,budget?,seed?}`) |
| GET | `/api/v1/cohorts/{id}/discovery` | Latest stored run per strategy, including every candidate's scoring trace |
| GET | `/api/v1/cohorts/{id}/benchmark` | Compare stored Random / Gemini-only / Adaptive runs; returns `insufficient_data` instead of inventing a winner |

### Adaptive discovery (the core loop)

`POST /cohorts/{id}/discovery` scores every eligible proposal for the cohort and
persists the decision. The score is

```
S(x) = 0.30·C + 0.25·N + 0.25·I + 0.20·V     (weights v1.0.0)
```

`C` is expected coverage gain (inverse repetition), `N` is genome novelty
(minimum distance to already-tested genomes), `I` is a documented information
proxy (`1 − unchanged-outcome share`), and `V` is schema validity. Each stored
candidate carries its nine-dimension genome, all four components, the total and
a human-readable rationale, so the selection can be audited without re-running
Gemini.

Run all three strategies (`adaptive`, `random`, `gemini_only`) with the same
budget, then call `GET /cohorts/{id}/benchmark`. The benchmark refuses to name a
winner when a strategy is missing, when any run selected fewer than 5
candidates, or when the metrics tie.

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
| GET | `/api/v1/experiments/{id}/evidence` | Get persisted evidence bundle (AC-06); `?download=1` returns it as a JSON file |
| POST | `/api/v1/experiments/{id}/replay` | Replay experiment (reproducibility) |
| GET | `/api/v1/experiments/{id}/review` | Read the recorded reviewer decision |
| POST | `/api/v1/experiments/{id}/review` | Record `approved` / `rejected` / `escalated` (rationale required) |

### Coverage (three maps)
| Method | Path | Description |
|---|---|---|
| GET | `/api/v1/coverage` | Control coverage + `outcome_coverage` (rule × status) + `scenario_coverage` (genome dimension × value) |

### Customers
| Method | Path | Description |
|---|---|---|
| GET | `/api/v1/customers` | List synthetic customers (entities); optional `?cohort_id=` |

### Counterfactual validation
| Method | Path | Description |
|---|---|---|
| POST | `/api/v1/counterfactuals/validate` | Compare baseline/variant genomes; returns declared changes, dependent changes, violations and `tightly_controlled` / `partially_controlled` / `exploratory` / `invalid` |

### Campaigns (persisted, budget-enforced)
| Method | Path | Description |
|---|---|---|
| GET | `/api/v1/campaigns` | List persisted campaigns |
| POST | `/api/v1/campaigns` | Create a campaign (name, budget, optional cohort and control config) |
| GET | `/api/v1/campaigns/{id}` | Get a campaign |
| PATCH | `/api/v1/campaigns/{id}` | Update description, budget or status |
| GET | `/api/v1/campaigns/{id}/coverage` | Campaign metadata + experiment count; states that coverage maps are global |
| GET | `/api/v1/campaigns/{id}/benchmark` | Cohort benchmark for the campaign's cohort (honest `insufficient_data` when runs are missing) |

### Reviews
| Method | Path | Description |
|---|---|---|
| GET | `/api/v1/reviews/queue` | Completed experiments with no decision, oldest first |

### Regression Test Pack (PRODUCT_SPEC item 7)
| Method | Path | Description |
|---|---|---|
| GET | `/api/v1/regression/pack` | Export a reproducible regression pack (JSON download) |
| POST | `/api/v1/regression/verify` | Import + verify a pack's checksum/shape |

### Audit
| Method | Path | Description |
|---|---|---|
| GET | `/api/v1/audit` | List audit events (`org_id` optional; defaults to the default org) |

### System
| Method | Path | Description |
|---|---|---|
| GET | `/healthz` | Health probe (unauthenticated) |
| GET | `/health/live` | Liveness probe; does not touch the database |
| GET | `/health/ready` | Readiness probe; pings the database, `503` when unreachable |

### Alert schema (PRODUCT_SPEC item 3)

The AML harness emits alerts that are a simplified FinCEN SAR projection —
identifiers only, never PII:

```json
{
  "id": "…", "rule_id": "structuring_v1", "rule_version": "<control-hash>",
  "entity_id": "…", "account_ids": ["…"], "tx_ids": ["…"],
  "amount_cents": 2999700, "currency": "USD",
  "subject_type": "individual", "activity_type": "structuring",
  "severity": "medium", "description": "…", "detected_at": "…"
}
```

### Evidence bundles (PRODUCT_SPEC item 7)

Every experiment persists an immutable bundle to `evidence_bundle (JSONB)` with a
SHA-256 checksum. `GET /api/v1/experiments/{id}/evidence` serves the persisted
bundle (building and persisting one on first read if absent); add `?download=1`
to receive it as a file attachment.

> **Multi-tenancy note**: migration `000004` added `org_id NOT NULL` to every
> table; migration `000006` sets the single `default` organisation as the column
> default so single-tenant/local deployments work. Callers that know the real
> org still pass `org_id` explicitly.

---

## Tests

```bash
# Go unit tests (hermetic — integration tests skip in -short mode)
go test -short ./...

# Go integration tests against a live, migrated database
DATABASE_TEST_URL='postgres://fcrt:fcrt@localhost:5433/fincrime_redteam?sslmode=disable' \
  go test ./...

# Frontend
cd frontend && npm test
```

Current status: **345 tests** (250 Go + 95 frontend). In the default `-short`
mode the DB-backed Go integration tests skip; with a migrated database reachable
via `DATABASE_TEST_URL` all 250 run. Frontend tests cover the session layer, the
role model, the dashboard, user management and the shared components.

---

## User Interface

The frontend is a clean, light, material-flavoured product UI (React 18 + Vite,
no component framework). Design tokens live in `frontend/src/index.css`;
components never hard-code colour. Routes are code-split, so the charting
library only loads on the pages that use it.

| Area | Pages |
|---|---|
| Workspace | Dashboard, Cohorts, Scenarios, Experiments, Coverage, Discovery, Benchmark Lab |
| Administration (admin only) | Users, Audit log |

**Roles are visible, not just enforced.** The sign-in screen and the Users page
both spell out what an analyst and an administrator can do; the sidebar groups
admin-only pages under an *Administration* heading and shows the signed-in
user's role; direct navigation to `/users` or `/audit` as an analyst redirects
to the dashboard.

---

## Deployment

### Deployed architecture

The stack currently runs entirely on free tiers:

| Layer | Where | URL |
|---|---|---|
| Frontend | Firebase Hosting (Spark, no billing) | https://fcrt-511717.web.app |
| API + worker | Render free web service, Docker runtime | https://fcrt-api.onrender.com |
| Database | Neon free Postgres 16 (Frankfurt) | — |

Neither the GCP project nor the Render workspace has a payment method. Cloud Run
and Cloud SQL are **not** used: the GCP project has billing disabled, so they
refuse to provision.

Because the backend is outside GCP, Firebase Hosting cannot reverse-proxy to it
(it can only rewrite to Cloud Run or Cloud Functions). The frontend therefore
talks to the API by absolute origin, which means two things must always agree:
the build-time `VITE_API_BASE_URL` and the API's `CORS_ORIGINS`.

### Frontend → Firebase Hosting

```bash
cd frontend
VITE_API_BASE_URL=https://fcrt-api.onrender.com npm run build
cd ..
python3 scripts/deploy-hosting.py          # project/site default from .firebaserc
```

`scripts/deploy-hosting.py` drives the Hosting REST API directly, so the
`firebase-tools` CLI is not required. Use it when the CLI cannot be installed
(its download host is often blocked) or when only a `gcloud` service account is
available in CI. It needs `firebasehosting.googleapis.com` enabled and the
project added to Firebase, both one-off steps documented in the script header.

`firebase.json` remains the canonical config for anyone who does have the CLI:
`firebase deploy --only hosting` produces the same release.

### Backend → Render

[`render.yaml`](render.yaml) describes the free service. To deploy by hand:

1. **Database** — create a Neon project and take the *direct* connection string
   (not the `-pooler` host: the API is a long-lived process, and Neon recommends
   direct connections for those).
2. **Service** — point Render at this repo (a public repo needs no GitHub App
   install), runtime `docker`, plan `free`, region `frankfurt`, health check
   `/healthz`, and `dockerCommand: /fcrt-all`.
3. **Migrations** — run once, and again after any schema change:

   ```bash
   docker build -t fcrt .
   docker run --rm --entrypoint /migrate -e DATABASE_URL='<neon-url>' fcrt up
   ```

4. **Environment** — `DATABASE_URL`, `AUTH_USERS`, `AUTH_SESSION_SECRET`
   (`openssl rand -hex 32`), `GEMINI_API_KEY`, `GEMINI_MODEL`, and
   `CORS_ORIGINS=https://<project>.web.app`. `AUTH_MODE=local` is the default.

`/fcrt-all` runs the API and the job worker in one container. Render's free plan
has no free background workers, and the runtime image is distroless (no shell to
chain the binaries with), so `cmd/all` supervises both as child processes.

Free-tier behaviour worth knowing: the service sleeps after 15 minutes without
traffic (~1 minute to wake), Neon's compute suspends after a few minutes idle
(the first query after that pays a short resume), and Render grants 750 instance
hours per workspace per month.

### Alternative: all-GCP

`scripts/gcp-setup.sh` provisions the whole stack on Google Cloud when billing
is enabled on the project:

```bash
PROJECT_ID=my-project REGION=us-east1 ./scripts/gcp-setup.sh
```

It enables APIs, builds/pushes to Artifact Registry, creates Cloud SQL (private
IP + VPC connector), a runtime service account and secrets, deploys `fcrt-api`
and `fcrt-worker` to Cloud Run, runs migrations via a Cloud Run job using the
image's `/migrate` binary, then builds and deploys the frontend to Firebase
Hosting.

Deployment artifacts:

| File | Purpose |
|---|---|
| `Dockerfile` | Builds `fcrt-api`, `fcrt-worker`, `/migrate` and `/fcrt-all` into one distroless image |
| `cmd/all/main.go` | Supervises the API and worker in a single container for hosts without free workers |
| `fly.toml` | Fly.io app config (free tier, requires a card) |
| `render.yaml` | Render Blueprint (free tier, single `/fcrt-all` service) |
| `scripts/deploy-hosting.py` | Firebase Hosting deploy over the REST API, no CLI needed |
| `scripts/gcp-setup.sh` | One-shot GCP provisioning + deploy (needs billing) |
| `firebase.json` | Hosting config for the CLI: serves `frontend/dist`, SPA fallback |
| `.firebaserc` | Firebase project binding |
| `.gcloudignore` | Keeps build artifacts out of the image upload |

> **Rotate `GEMINI_API_KEY` before any public deployment** — the deployed service
> uses a shared key with the default test credentials, so anyone who finds the
> URL can spend it. Change `AUTH_USERS` away from `admin/admin` at the same time.

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
