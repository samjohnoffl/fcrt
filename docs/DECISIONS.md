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
- **Hosting**: Firebase Hosting (Spark, free) serves `frontend/dist` and rewrites `/api/**` to the backend so the SPA and API share an origin. The backend is a single distroless image that runs on any free-tier container host (Fly.io / Render / Railway / Koyeb) with a free-tier PostgreSQL (Neon / Supabase) and `GEMINI_API_KEY`; `/migrate up` runs once before first boot.
- **Known blocker**: deploying to GCP requires billing on the project; that remains with the user to enable. `scripts/gcp-setup.sh` is kept for when it is.
