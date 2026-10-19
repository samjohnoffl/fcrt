# Decisions Log — Financial Crime AI Red Team

## Format
| Field | Meaning |
|---|---|
| Status | `open` · `decided` · `superseded` |
| Impact | Area of codebase affected |

Decisions are numbered sequentially. Never delete; supersede with a new entry.

---

## DECISION-001 · Gemini Response Schema Enforcement
**Status**: decided  
**Impact**: `internal/scenario`  
**Question**: Should we use the Gemini API's native JSON response schema feature (constraining the model's output grammar) or rely on prompt-guided JSON + post-hoc validation?  
**Options**:
- A) Native Vertex AI response schema (compile-time guarantee, but schema must be re-declared in Go structs)
- B) Prompt-guided + `encoding/json` unmarshal + validation library
**Considerations**: Option A is more robust but couples us to Vertex AI schema API version. Option B is more portable.  
**Decision**: **Option A** — use the native response schema, with the schema declared alongside the Go `ScenarioProposal` struct and validated on unmarshal as a belt-and-braces check.

---

## DECISION-002 · Coverage Weight Algorithm
**Status**: decided  
**Impact**: `internal/coverage`  
**Question**: What formula should `coverage_weight` use to prioritise under-exercised rules for the next cohort selection?  
**Options**:
- A) Binary: 1.0 if never triggered, 0.0 otherwise
- B) Inverse frequency: `1 / (times_triggered + 1)`
- C) Recency-decayed: frequency inversely weighted by time since last trigger
**Decision**: **Option B** — inverse frequency `1 / (times_triggered + 1)`. Simple, monotonic, and never zero (so recency weighting can be layered on later without changing the contract).

---

## DECISION-003 · Multi-Organisation Isolation
**Status**: decided  
**Impact**: `cmd/api`, all DB tables  
**Question**: Should v1 support multiple isolated organisations (tenants) or be single-org?  
**Context**: Adding `org_id` to all tables now is low cost; retrofitting later is high cost.  
**Options**:
- A) Single-org now; add `org_id` later
- B) Add `org_id` FK to all tables now (even if only one org exists)
**Decision**: **Option B** — `org_id NOT NULL` on every table (migration `000004`). To keep single-tenant/local deployments working without app-side org resolution, migration `000006` points the column default at the single `default` organisation. Callers that know the real org still pass `org_id` explicitly; the multi-tenancy path stays open.

---

## DECISION-004 · Worker Job Queue Mechanism
**Status**: decided  
**Impact**: `cmd/worker`, `internal/job`  
**Question**: Should the worker use PostgreSQL-backed job polling (simple, already in stack) or a dedicated message queue (Cloud Pub/Sub, Cloud Tasks)?  
**Options**:
- A) PostgreSQL `job` table with `SELECT ... FOR UPDATE SKIP LOCKED` (v1 default)
- B) Cloud Tasks for durability and dead-letter queuing
- C) Cloud Pub/Sub for fan-out scenarios  
**Decision**: **Option A** — `internal/job` implements a `SKIP LOCKED` work queue with attempt counting and backoff. No new infrastructure; revisit at scale.

---

## DECISION-005 · Data Archival Strategy
**Status**: open  
**Impact**: All DB tables  
**Question**: When experiments and evidence bundles grow large, what is the archival / retention policy?  
**Options**:
- A) No archival in v1 (append-only, manual cleanup)
- B) TTL-based deletion (e.g. soft-delete after 90 days)
- C) Export to Cloud Storage, delete from PostgreSQL  
**Decision**: _(unresolved — v1 uses option A by default; revisit when storage costs noted)_

---

## DECISION-006 · Audit Logging
**Status**: decided  
**Impact**: `cmd/api`, `internal/scenario`, `internal/experiment`, `internal/audit`  
**Question**: Where should approval/rejection audit events be stored?  
**Options**:
- A) Structured log lines only (Cloud Logging)
- B) Dedicated `audit_event` table in PostgreSQL
- C) Cloud Audit Logs via GCP IAM policy  
**Decision**: **Option B** — a dedicated `audit_event` table (queryable via `GET /api/v1/audit`), in addition to structured log lines. `internal/audit` owns the repository; `org_id` is optional and defaults to the default org.

---

## DECISION-007 · Entity Graph Visualisation Library
**Status**: open  
**Impact**: `frontend/src/components`  
**Question**: D3 force-directed (full control, steep learning curve) vs. Cytoscape.js (higher-level, easier large-graph handling)?  
**Decision**: _(unresolved — depends on expected graph size; for >500 nodes Cytoscape performs better)_

---

## DECISION-008 · Cross-Cohort Experiment Comparison
**Status**: open  
**Impact**: `frontend`, `cmd/api`  
**Question**: Should the UI allow comparing experiment results across different cohorts in a single view?  
**Considerations**: Useful for understanding if evasion is cohort-specific. Requires normalisation logic.  
**Decision**: _(unresolved — deferred to post-v1)_

---

## DECISION-009 · Evidence Bundle Persistence
**Status**: decided  
**Impact**: `internal/evidence`, `internal/api`, `migrations/000008`  
**Question**: Should evidence bundles be persisted to PostgreSQL, and in what shape?  
**Context**: `evidence_bundle` existed in `000001` but was never written, and its FK pointed at the legacy `experiment` table (superseded by `experiment_pair` in `000003`), so any insert would have failed.  
**Options**:
- A) Build the bundle on read only; never persist
- B) Persist the canonical `evidence.Bundle` (JSONB), upsert keyed by experiment, with a SHA-256 checksum; re-point the FK to `experiment_pair`
- C) Persist to Cloud Storage instead
**Decision**: **Option B** — PRODUCT_SPEC item 7 requires bundles "stored in PostgreSQL (JSONB) and optionally exported as JSON files". Migration `000008` re-points the FK to `experiment_pair` and adds a unique constraint on `experiment_id`; `internal/evidence.Repository` upserts the bundle; `GET /experiments/{id}/evidence` serves the persisted bundle and `?download=1` returns it as a file attachment.

---

## DECISION-010 · Migrations at Deploy Time
**Status**: decided  
**Impact**: `cmd/migrate`, `Dockerfile`, `docker-compose.yml`, `scripts/gcp-setup.sh`  
**Question**: How should the Cloud Run migration job run golang-migrate, given the runtime image is distroless and the deployment script invoked a non-existent `/migrate` binary?  
**Options**:
- A) Keep using the public `migrate/migrate` image in a separate job (migrations must then be shipped separately)
- B) Build a small in-repo `cmd/migrate` wrapper (wraps the golang-migrate library) into the app image as `/migrate`
**Decision**: **Option B** — `cmd/migrate` embeds the golang-migrate library, reads `DATABASE_URL` + `MIGRATIONS_DIR` (default `/migrations`), and shares the same `schema_migrations` bookkeeping as the official image (so it can adopt an existing database). Local Compose and the Cloud Run job now exercise the identical code path. `down` moves one step; `reset` is the explicit full teardown.

---

## DECISION-011 · Authentication Backend (username/password session tokens)
**Status**: decided (explicit user approval — overrides the `Auth = Firebase (ID tokens)` stable constraint)
**Impact**: `internal/api/auth_session.go`, `internal/api/authz.go`, `internal/api/middleware.go`, `internal/api/router.go`, `cmd/api`, `frontend`, `docker-compose.yml`, `scripts/gcp-setup.sh`
**Question**: The stack constraint fixed authentication to Firebase ID tokens, but there is no Firebase-backed login UI and the user asked for a default test username/password so the platform can be demonstrated/deployed immediately. How should authentication (and the previously absent authorization) work?
**Options**:
- A) Firebase only — requires a real Firebase project, user provisioning, and a login UI before anything can be exercised
- B) Username/password login that issues an HMAC-signed, stateless session token, with a `AUTH_MODE` switch to keep Firebase as an option
- C) Users in PostgreSQL with bcrypt + DB sessions
**Decision**: **Option B**, chosen by the user. `AUTH_MODE` selects `local` (default; `AUTH_USERS="user:pass:role[,...]"`), `firebase` (ID tokens, unchanged path), or `none` (dev only). `POST /auth/login` returns a signed token (`base64url(payload).base64url(HMAC-SHA256)`); the existing `AuthVerifier` interface was widened to return a `Principal{Username, Role}` rather than a bare UID.
**Authorization**: a hierarchical role model — `analyst` (the whole red-team workflow: create/lock cohorts, request/approve/reject scenarios, run/replay experiments, create campaigns, verify the regression pack) < `admin` (also user management and the audit log). Enforced by `RequireRole` middleware per route, not in the handlers. (Superseded the original three-role model — see DECISION-012.)
**Security notes**: default test credentials (`admin/admin`) are permitted in any environment per explicit user instruction, but the API logs a loud startup warning when they are in use. `AUTH_SESSION_SECRET` falls back to an insecure development value with a warning; on GCP it is generated with `openssl rand -hex 32` and injected from Secret Manager (`fcrt-session-secret`). Passwords are compared in constant time. Because the token is stateless, `POST /auth/logout` is a client-side discard. Login success/failure is written to `audit_event`.
**Follow-up**: If real accounts are ever required, add a `user` table with bcrypt hashes and short-lived refresh tokens (would supersede this decision for production).
**Update (see DECISION-012)**: superseded in part — the role model is now two roles (`analyst`, `admin`) and local users are DB-backed.

---

## DECISION-012 · Two Roles + Database-Backed User Management
**Status**: decided (explicit user instruction — "drop the viewer role, give admin a user management page and a few other admin features")
**Impact**: `internal/api/authz.go`, `internal/api/handler_user.go`, `internal/api/auth_session.go`, `internal/api/router.go`, `internal/auth`, `migrations/000009_app_user`, `cmd/api`, `frontend`
**Question**: The role model had three levels (`viewer` < `analyst` < `admin`) but `viewer` added little: it could only read, and there was no UI that meaningfully distinguished it. Users were also stateless env-var entries, so an admin could not manage accounts. How should authorization and account management work?
**Options**:
- A) Keep three roles and add a user CRUD API over the env list
- B) Two roles (`analyst` < `admin`), local users persisted in PostgreSQL, admin-only management UI
- C) Keep roles as-is and skip user management
**Decision**: **Option B**.
- **Roles**: exactly two, hierarchical — `analyst` (full workspace) < `admin` (+ user management, audit log). `RoleViewer` was deleted. Every authenticated route requires **at least `analyst`**, so an unknown/missing role is denied even if authentication somehow succeeded. `/users` and `/audit` additionally require `admin`.
- **Storage**: new `app_user` table (`000009`) — `id, org_id, username, password_hash, role CHECK IN ('analyst','admin'), disabled, timestamps`, unique `(org_id, username)`. Passwords are bcrypt hashes (`internal/auth/password.go`); comparison is constant-time and unknown users still run a dummy hash to avoid a timing oracle.
- **Seeding**: `AUTH_USERS` seeds the table on boot via `EnsureUser`, which inserts only missing accounts so admin edits survive restarts. If the table is unreachable the API logs a warning and falls back to the in-memory env store (user management then returns `501`).
- **Policy**: `MinPasswordLen = 6` for accounts created through the API, but the bootstrap seed bypasses it so the documented test credentials (`admin/admin`) work anywhere.
- **Self-lockout guard**: an admin cannot disable, demote or delete **their own** account; changing their own password is allowed. Enforced in the handler and mirrored in the UI (the controls are disabled on your own row).
- **Auditability**: `user_created`, `user_updated`, `user_deleted` audit events added to `internal/audit/types.go`; the IP and user agent are recorded from the request.
**UI**: a new admin-only **Users** page (list, create, change role, reset password, disable/enable, delete) and an **Audit log** page. The sidebar hides the *Administration* section for analysts and `/users` · `/audit` redirect to the dashboard; the sign-in screen and the Users page both spell out the difference between the roles.

---

## DECISION-013 · Gemini Provider: AI Studio API Key + Default Model
**Status**: decided
**Impact**: `internal/scenario/gemini.go`, `internal/scenario/gemini_api_test.go`, `cmd/api`, `.env.example`, `docker-compose.yml`
**Question**: Vertex AI needs a GCP project with billing enabled, which was unavailable. Google AI Studio API keys need neither a project nor billing. Should the platform support the API-key path, and which model?
**Options**:
- A) Vertex AI only (`VERTEX_PROJECT` + ADC)
- B) Gemini API key via `x-goog-api-key` (preferred when present), Vertex AI as fallback
- C) Both, with the API key taking precedence
**Decision**: **Option C** — `NewGeminiAPIClient` is selected when `GEMINI_API_KEY` is set; otherwise the Vertex AI client is used, so existing deployments are unaffected.
**Details**:
- The Gemini API **rejects `additionalProperties`** in `responseSchema` (HTTP 400) even though Vertex AI accepts it. The API-key provider therefore recursively strips unsupported keys (`stripUnsupportedSchemaKeys`) before sending, while keeping `enum`, `items` and `required`.
- Default model is **`gemini-3.8-flash`** (`scenario.DefaultModel`). Retired models are rejected for new keys: `gemini-2.5-flash` returns 404 ("no longer available to new users") and `gemini-1.5-pro` no longer exists.
- 5xx responses retry with bounded exponential backoff; 4xx do not (a 503 during testing is what proved retries matter).
- The producing model is recorded on the proposal (`GenerationResult.ModelID` → `scenario_proposal.gemini_model`) for auditability, replacing a hard-coded `"gemini-1.5-pro"`.
- The API key is a secret: it lives only in `.env` (gitignored) / the host's environment and is never committed. **Rotate it before any public deployment.**
**Evidence**: verified live end-to-end — `POST /cohorts` → worker generates and locks the cohort → `POST /scenarios` (3.5k-token prompt, ~10–20 s) returns a schema-validated proposal (`split_transaction`, `split_count`, `threshold_cents`) with `gemini_model=gemini-3.8-flash`. When the model returns candidates that fail validation the API answers `422 no_valid_candidates` and logs the rejection reasons.

---

## DECISION-014 · UI Design Language + Deployment Target
**Status**: decided
**Impact**: `frontend/src/index.css`, `frontend/src/components`, `frontend/src/pages`, `frontend/src/App.tsx`, `README.md`, `firebase.json`
**Question**: The UI was a dark, dense "enterprise console" that the user judged cluttered, and the GCP deployment path was blocked (project billing disabled, so Artifact Registry, Secret Manager and Cloud Run all reject with "This API method requires billing to be enabled").
**Options**:
- A) Keep the dark theme; keep Cloud Run
- B) Restyle to a light, material-flavoured product UI; deploy the frontend to Firebase Hosting (free) and the backend to a free-tier non-GCP container host
**Decision**: **Option B**.
- **Design**: light theme driven entirely by CSS custom properties (`#f8f9fa` surfaces, `#dadce0` borders, `#202124` text, `#1a73e8` accent, Roboto). Emoji were replaced with inline SVG icons; the sidebar gained grouped sections and a user block showing the signed-in role; the sign-in screen became a two-column card with a role legend; the dead Vite boilerplate (`App.css`, `react.svg`) was deleted.
- **Performance**: routes are `React.lazy` code-split behind a `Suspense` fallback. The initial JS chunk fell from 690 kB (192 kB gzip) to 214 kB (68 kB gzip); the charting library now loads only on pages that need it.
- **Hosting**: Firebase Hosting (Spark, free) serves `frontend/dist`. The backend is a single distroless image that runs on any free-tier container host (Fly.io / Render / Railway / Koyeb) with a free-tier PostgreSQL (Neon / Supabase) and `GEMINI_API_KEY`; `/migrate up` runs once before first boot. (Superseded by DECISION-015: Hosting cannot reverse-proxy to a non-GCP backend, so the `/api/**` rewrite was dropped and the frontend targets the API by absolute origin.)
- **Known blocker**: deploying to GCP requires billing on the project; that remains with the user to enable. `scripts/gcp-setup.sh` is kept for when it is.

---

## DECISION-015 · Deployed Topology: Firebase Hosting + Render + Neon
**Status**: decided
**Impact**: `cmd/all/main.go`, `Dockerfile`, `render.yaml`, `firebase.json`, `.firebaserc`, `scripts/deploy-hosting.py`, `frontend/src/api/client.ts`, `README.md`
**Question**: DECISION-014 proposed sharing an origin via a Hosting `/api/**` rewrite, but Firebase Hosting only rewrites to Cloud Run or Cloud Functions, and the GCP project has billing disabled — so no backend could exist in GCP. Where do the API and database actually run, and how should the SPA reach the API?
**Options**:
- A) Enable GCP billing and use Cloud Run + Cloud SQL (~$10–30/month)
- B) Free tiers: Firebase Hosting (frontend) + Render free web service (API) + Neon free Postgres, reached by absolute origin
- C) Wait until billing is enabled and deploy nothing
**Decision**: **Option B**.
**Details**:
- **Hosting cannot proxy the API.** The `/api/**` `run` rewrite was removed; the SPA calls `VITE_API_BASE_URL` directly. The consequence is that two settings must always agree: the build-time `VITE_API_BASE_URL` and the API's `CORS_ORIGINS`. A mismatch shows up as a CORS error in the browser, not as a 4xx.
- **Neon, not Render Postgres.** Free Render Postgres *expires 30 days after creation* and is deleted after a 14-day grace period, which would take the data with it. Neon's free tier does not expire. The **direct** connection string is used rather than the `-pooler` host: the API is a long-lived process, which is exactly the case Neon recommends direct connections for (the pooled endpoint is for serverless/ephemeral clients and needs `simple_protocol` for pgx).
- **One container, two processes.** Render's free plan has no background workers (paid from the first minute) and the runtime image is distroless, so `sh -c "worker & api"` is impossible. `cmd/all` starts the worker and then the API as children, forwards `SIGTERM`/`SIGINT`, escalates to `SIGKILL` after a 20s grace period, and exits non-zero when a child dies so Render restarts the container. `WORKER_BINARY` / `API_BINARY` make it configurable; the signal channel is injectable, which is what makes the shutdown paths testable.
- **The container image is unchanged otherwise.** `/fcrt-api` and `/fcrt-worker` remain the entrypoints for `docker compose` and Cloud Run; `/fcrt-all` is opt-in via `dockerCommand`.
- **Hosting deploys without the CLI.** `scripts/deploy-hosting.py` drives the Hosting REST API, because `firebase-tools` could not be installed (its download host returns 000 from this environment). Two API quirks it encodes: the version `config` dialect differs from `firebase.json` (`glob`/`path` + a header *map*, not `source`/`destination` + a header list), and the SHA-256 declared in `populateFiles` must be the hash of the *gzipped* bytes that are actually uploaded.
- **`AUTH_MODE=local` with seeded `admin/admin` and `analyst/analyst`**, per DECISION-011/012, so the demo works on the public URL. This is a demo posture, not a production one — see the warning in the README.
**Evidence**: live at https://fcrt-511717.web.app against https://fcrt-api.onrender.com. Verified through the browser: sign-in as admin, Dashboard/Cohorts/Scenarios/Users/Audit-log all rendering live data; `GET /users` 200 for admin and 403 for analyst; `/audit` 403 for analyst; no token 401. Worker verified in-container: `POST /cohorts` auto-locked after ~10s with 22 entities / 301 transactions. Gemini verified live: `POST /scenarios` returned a schema-valid `split_transaction` proposal with `gemini_model=gemini-3.8-flash` in 19s.

---

## DECISION-016 · Real Genome, Live Adaptive Selection, Honest Benchmark
**Status**: decided
**Impact**: `internal/genome`, `internal/discovery`, `internal/benchmark`, `internal/api/handler_discovery.go`, `internal/api/router.go`, `migrations/000010_discovery.*`, `internal/experiment/{types,pair,repository}.go`, `cmd/worker/main.go`, `frontend/src/pages/BenchmarkLab.tsx`, `frontend/src/api/*`
**Question**: A functional audit against the project report found that three named core capabilities were not real: the Financial Crime Genome was a five-float vector imported by no package, the adaptive score was implemented twice and called by nothing, and the benchmark returned hard-coded constants (`total/4` valid, fixed coverage gains) with a rigged winner. The design document explicitly requires benchmark metrics to be computed from stored runs.
**Options**:
- A) Leave the stubs; they demo acceptably
- B) Make the genome structured and used, wire one adaptive selector, and derive every benchmark number from stored runs (including "insufficient data")
- C) Remove the capabilities from the report
**Decision**: **Option B**.
**Details**:
- **Genome v2.** `internal/genome` is now a structured schema (`genome_id`, `schema_version`, `reference_typology_id`, nine named dimensions, `constraints`, `synthetic_only`). Dimensions are categorical codes plus two bounded integer magnitudes (transaction count, counterparty count). Encoding is deterministic and rule-based from the proposal; ambiguous text maps to `unknown` rather than being guessed. `Distance` returns the mean of nine per-dimension distances and `DistanceComponents` exposes every input. `Novelty` is the minimum distance to a previously tested genome. Similarity is explicitly *not* presented as proof of novelty.
- **One selector.** `internal/discovery` is the single scoring implementation. `S(x) = wC·C + wN·N + wI·I + wV·V` with weights `v1.0.0` (0.30 coverage, 0.25 novelty, 0.25 information, 0.20 validity). Coverage is inverse repetition; novelty is genome distance; information is a documented proxy (`1 − unchanged-outcome share`); validity is 1 for a schema-valid genome. Invalid genomes and duplicate fingerprints are rejected with reasons. Adaptive ties break on proposal ID, and the random strategy is seeded so a run is reproducible.
- **Persisted, explainable decisions.** `discovery_run` stores strategy, weights, seed, budget, pool sizes and runtime; `discovery_candidate` stores each candidate's genome, four components, total, rank and a human-readable rationale. `POST /cohorts/{id}/discovery` selects and persists; `GET /cohorts/{id}/discovery` returns the latest run per strategy. The experiment pair now records `scenario_proposal_id`, which is what lets the loop know which proposals have already been tested.
- **The dead scorers were deleted**, including `cmd/worker`'s `ComputeAdaptiveScore` block, so there is no second, divergent formula.
- **Benchmark is now derived, not simulated.** `internal/benchmark` reads the latest stored run for each of `random`, `gemini_only` and `adaptive` and computes yield, approval rate and mean components from those rows. It reports `insufficient_data` when a strategy has not been run, when any run selected fewer than 5 candidates, or when all strategies tie; in those cases it declares **no winner**. `GET /campaigns/{id}/benchmark` now returns `404 campaign_not_persisted` because campaigns have no table; the real endpoint is `GET /cohorts/{id}/benchmark`.
**Evidence**: `go test ./...` passes (genome, discovery and benchmark have new unit tests). Live against the dev database: all three strategies returned `201` with persisted traces, and `GET /cohorts/{id}/benchmark` returned `insufficient_data=true` with the honest note rather than a fabricated winner — exactly the intended behaviour for a one-candidate pool.
**Known gap (not fixed here)**: the AML simulator still implements 3 of the 8 control families and has no five-status outcome model; the three coverage maps, counterfactual validity classification, payment-layer event fields, bank relationship/institution entities, `/customers`, `/health/live|ready`, campaign persistence and the review-queue screen remain outstanding. They are recorded here so they are not mistaken for done. (Resolved by DECISION-017, except the bank relationship/institution graph.)

---

## DECISION-017 · Control Outcomes, Three Coverage Maps, Counterfactual Validation, Persisted Campaigns and Review Queue
**Status**: decided
**Impact**: `internal/aml/{types,config,evaluator}.go`, `internal/coverage`, `internal/counterfactual`, `internal/payment`, `internal/campaign`, `internal/experiment`, `internal/api/*`, `migrations/000011…000013`, `frontend/src/pages/{CoverageDashboard,ReviewQueue}.tsx`, `frontend/src/components/Layout.tsx`, `frontend/src/App.tsx`
**Question**: DECISION-016 recorded several report requirements as unimplemented. Which of them can be made real without fabricating data, and how should the remainder be represented?
**Options**:
- A) Leave them as documented gaps
- B) Implement the missing control families, outcome semantics, coverage maps, counterfactual checks, payment fields, campaign persistence and review workflow; leave only the synthetic relationship/institution graph as an explicit gap
- C) Mock the missing pieces so the UI looks complete
**Decision**: **Option B**.

**Control simulator (8 families, five statuses)**
- Added `velocity`, `counterparty_expansion`, `dormancy_activation`, `geographic_band` and `ownership_relationship` to the existing `structuring`, `layering` and `rapid_movement`. Each has a versioned threshold block in `ControlConfig`; `ControlVersion` is now `v0.2.0`, so old and new configurations are not silently compared.
- `HarnessResult` now carries one `RuleOutcome` per family. The five statuses are exclusive: `triggered`, `not_triggered`, `not_applicable`, `inconclusive`, `failed`. A rule that is disabled is `not_applicable` with `rule_disabled`; a rule with no input is `not_applicable` with `no_events`; a dormancy rule without two events is `inconclusive` with `insufficient_history`; a panic or unknown rule is `failed`, never `not_triggered`.
- Triggered outcomes retain rule ID, rule version (config hash), contributing event IDs, account/entity IDs, amount and currency, so the evidence can be traced to specific synthetic events.

**Three coverage maps**
- Migration `000011` adds `rule_outcome`, extends `rule_coverage` with per-status counters, and adds `outcome_coverage` (rule × status) and `scenario_coverage` (genome dimension × value). `GET /api/v1/coverage` returns all three.
- Coverage is written from the five-status outcomes of both the baseline and variant runs. The old handler recorded every rule that appeared in *any* alert list as triggered, and the worker counted only triggers; both overstated coverage and were replaced.
- Scenario coverage is recorded from the deterministic genome of the linked approved proposal. If no proposal is linked, no scenario row is written — the map stays honestly empty instead of inventing dimension values.

**Counterfactual validity**
- New `internal/counterfactual` compares a baseline and variant genome and returns: declared dimensions, every changed dimension, dependent changes with the dimension they derive from, violations, and a classification (`tightly_controlled`, `partially_controlled`, `exploratory`, `invalid`).
- Pairs are rejected when a dimension changes without being declared or derived, when a declared dimension did not actually change, when no dimension is declared, or when schema versions differ. Dependencies are explicit (`transactions → network`, `geography → network`, `actors → ownership`, etc.).
- `POST /api/v1/counterfactuals/validate` exposes this. The report is structural: it never claims that the declared change caused the outcome.

**Payment intelligence**
- `payment.Message` now records `message_family` (`pacs.008` / `mt103`), `normalization_version` (`v1.0.0`) and a deterministic `source_record_hash`. `Deduplicate` removes the same ledger record normalised twice before `AnalyzeBatch` scores a batch; messages with no identity are never dropped.

**API and review workflow**
- `GET /api/v1/customers` lists synthetic entities (optionally filtered by `cohort_id`).
- `GET /health/live` never touches the database; `GET /health/ready` pings it and returns 503 when unreachable.
- Campaigns are persisted (`migration 000012`): create/list/get/patch, a cohort link, a budget, status, and a control configuration. Experiments accept an optional `campaign_id`; the handler checks campaign status and budget before execution, rejects a cohort mismatch, and increments usage after a successful save.
- `GET /campaigns/{id}/coverage` reports the campaign, its experiment count and budget, and states that the coverage maps are global rather than pretending to slice them. `GET /campaigns/{id}/benchmark` runs the honest cohort comparison when the campaign has a cohort.
- Review decisions are persisted (`migration 000013`). `GET /reviews/queue` returns completed unreviewed pairs oldest-first; `POST /experiments/{id}/review` requires a decision and a rationale, records the reviewer from the session, writes an audit event, and returns `409` on a second review. The frontend adds a Review Queue screen with inline approve/reject/escalate forms.

**Known gaps (still not implemented)**
- The synthetic bank still has no separate relationship/institution entity graph. Ownership is represented by `account.entity_id`, and counterparty edges are derivable from transactions, but the report's `customer_relationships` / institution tables and cohort-neighbourhood completeness/exclusion records are not present. The UI and API do not claim otherwise.
- Campaigns are persisted and budgeted, but the frontend has no campaign management screen, and coverage is not sliced per campaign at outcome-write time.
- Counterfactual validation is exposed as a standalone API and is not yet attached to stored experiment pairs, because the experiment request does not carry a baseline genome.

**Evidence**: `go test ./...` (295 test runs, including new outcome, counterfactual, coverage, campaign, payment and review tests) and the frontend suite (102 tests, including the three-map coverage view and the review workflow) pass. Migrations apply cleanly to version 13.
