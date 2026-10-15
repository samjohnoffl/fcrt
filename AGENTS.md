# AGENTS.md — Financial Crime AI Red Team

## Stable Constraints (Do NOT Override Without Explicit User Approval)

### Stack
| Layer | Technology |
|---|---|
| API / Workers | Go 1.22+ |
| Frontend | React 18 + TypeScript 5, Vite |
| Database | PostgreSQL 16 (Cloud SQL) |
| AI | Gemini 1.5 Pro via Vertex AI SDK for Go |
| Hosting | Firebase Hosting (frontend static assets) |
| Compute | Cloud Run (API + worker containers) |
| Migrations | golang-migrate (SQL files in `migrations/`) |
| Config | Environment variables; `.env.example` committed; no secrets committed |

### Non-Goals (Out of Scope)
- Real customer PII or real transaction data
- Live connection to any production banking system
- IAM provisioning, Cloud resource creation, or billing changes
- Deploying to production or staging environments
- Installing system-level dependencies (docker, gcloud, etc.) without user approval

### Coding Standards
- All Go packages use `internal/` unless explicitly exported
- All SQL migrations are numbered sequentially (`000001_init.up.sql` etc.)
- No `interface{}` / `any` without a comment explaining necessity
- All Gemini calls must be idempotent and logged with a correlation ID
- Frontend state: React Query for server state, no Redux unless user approves
- Every experiment run produces a deterministic, reproducible evidence bundle

### Naming Conventions
- Go: `snake_case` for files, `PascalCase` for exported types
- SQL: `snake_case` tables and columns, singular table names
- TypeScript: `camelCase` for variables, `PascalCase` for components/types

### Unresolved Decisions (See docs/DECISIONS.md)
- Do not invent answers to open decisions — record them instead
