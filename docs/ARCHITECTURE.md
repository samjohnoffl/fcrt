# Architecture — Financial Crime AI Red Team

## System Overview

```
┌─────────────────────────────────────────────────────────────────────┐
│                         Firebase Hosting                            │
│                    React/TypeScript SPA (Vite)                      │
└────────────────────────────┬────────────────────────────────────────┘
                             │ HTTPS / REST+JSON
┌────────────────────────────▼────────────────────────────────────────┐
│                    Cloud Run: API Server (Go)                        │
│  cmd/api  →  internal/{bank,aml,scenario,experiment,coverage,evidence}│
└───────┬──────────────┬────────────────────────────┬─────────────────┘
        │              │                            │
   PostgreSQL    Vertex AI (Gemini)          Cloud Run: Worker (Go)
   (Cloud SQL)   internal/scenario           cmd/worker
        │              │                            │
        └──────────────┴────────────────────────────┘
                            PostgreSQL
```

## Services

### `cmd/api` — REST API Server
- HTTP router: `net/http` + `chi`
- Graceful shutdown, structured JSON logging (`slog`)
- Auth: Firebase ID tokens verified server-side (header: `Authorization: Bearer <token>`)
- All handlers in `internal/<domain>/handler.go`

### `cmd/worker` — Background Job Worker
- Polls a jobs table in PostgreSQL (no external queue in v1; **DECISION-004** tracks this)
- Processes: cohort generation, experiment execution, coverage re-computation
- Each job is idempotent (can be retried safely)

### `internal/bank` — Synthetic Bank Generator
- `Generator` struct: seeded `math/rand`, produces `Entity`, `Account`, `Transaction` structs
- Writes to `bank_entity`, `bank_account`, `bank_transaction` tables
- Cohort locking: sets `cohort.locked_at` preventing further modification

### `internal/aml` — Mock AML Harness
- `Harness` struct: stateless, accepts a `CohortID`, reads transactions, applies rules
- Rule registry: each rule is a Go function `func(tx []Transaction) []Alert`
- Built-in rules: StructuringRule, LayeringRule, RapidMovementRule
- Returns `HarnessResult{RuleVersion, Alerts []Alert}`; fully deterministic

### `internal/scenario` — Gemini Scenario Proposer
- Calls Vertex AI `gemini-1.5-pro` with a structured prompt template
- Response unmarshalled into `ScenarioProposal` struct
- Stores raw prompt + raw response in `scenario_proposal` table (evidence)
- **DECISION-001**: structured output via Gemini response schema vs. prompt-guided JSON

### `internal/experiment` — Experiment Runner
- Creates baseline + variant runs
- Orchestrates: lock cohort → run AML baseline → apply mutation → run AML variant → diff alerts
- Mutations: `SplitTransaction`, `IntroduceShellEntity`, `DelayAndResubmit`
- Stores `ExperimentRun`, `ExperimentVariant`, `AlertDiff` in DB

### `internal/coverage` — Coverage Tracker
- After each experiment, updates `rule_coverage` table
- Computes coverage score: fraction of rule IDs triggered at least once
- Weighted scoring: rules not triggered recently score higher (**DECISION-002**)

### `internal/evidence` — Evidence Bundle Builder
- Assembles: cohort snapshot (JSON), scenario params, AML diffs, coverage delta, Gemini trace
- Stores as JSONB in `evidence_bundle` table
- Exports to `evidence_RUNID.json` for regression use

## Data Flow: Full Experiment Cycle

```
1. User defines cohort predicate  →  POST /cohorts
2. Worker generates synthetic data →  cohort locked
3. User requests scenario proposal →  POST /scenarios  →  Gemini call
4. User approves scenario          →  PATCH /scenarios/{id}/approve
5. Worker runs experiment          →  baseline AML run → mutation → variant AML run
6. Diff computed, coverage updated →  evidence bundle emitted
7. Frontend displays results       →  GET /experiments/{id}/evidence
```

## Deployment Topology
- **Firebase Hosting**: serves built Vite SPA; proxies `/api/*` to Cloud Run API
- **Cloud Run API**: single container, stateless, min-instances=1
- **Cloud Run Worker**: single container, long-poll loop, min-instances=1
- **Cloud SQL PostgreSQL**: private VPC, accessed via Cloud SQL Connector (no public IP)

## Configuration
All runtime config via environment variables (see `.env.example`):
- `DATABASE_URL` — Cloud SQL connection string
- `VERTEX_PROJECT` — GCP project ID
- `VERTEX_LOCATION` — e.g. `us-central1`
- `FIREBASE_PROJECT` — Firebase project ID
- `LOG_LEVEL` — `debug|info|warn|error`
