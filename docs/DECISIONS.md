# Decisions Log — Financial Crime AI Red Team

## Format
| Field | Meaning |
|---|---|
| Status | `open` · `decided` · `superseded` |
| Impact | Area of codebase affected |

Decisions are numbered sequentially. Never delete; supersede with a new entry.

---

## DECISION-001 · Gemini Response Schema Enforcement
**Status**: open  
**Impact**: `internal/scenario`  
**Question**: Should we use the Gemini API's native JSON response schema feature (constraining the model's output grammar) or rely on prompt-guided JSON + post-hoc validation?  
**Options**:
- A) Native Vertex AI response schema (compile-time guarantee, but schema must be re-declared in Go structs)
- B) Prompt-guided + `encoding/json` unmarshal + validation library
**Considerations**: Option A is more robust but couples us to Vertex AI schema API version. Option B is more portable.  
**Decision**: _(unresolved — needs input from team on Vertex AI SDK version locked in)_

---

## DECISION-002 · Coverage Weight Algorithm
**Status**: open  
**Impact**: `internal/coverage`  
**Question**: What formula should `coverage_weight` use to prioritise under-exercised rules for the next cohort selection?  
**Options**:
- A) Binary: 1.0 if never triggered, 0.0 otherwise
- B) Inverse frequency: `1 / (times_triggered + 1)`
- C) Recency-decayed: frequency inversely weighted by time since last trigger
**Decision**: _(unresolved — requires discussion on whether recency matters for regression safety)_

---

## DECISION-003 · Multi-Organisation Isolation
**Status**: open  
**Impact**: `cmd/api`, all DB tables  
**Question**: Should v1 support multiple isolated organisations (tenants) or be single-org?  
**Context**: Adding `org_id` to all tables now is low cost; retrofitting later is high cost.  
**Options**:
- A) Single-org now; add `org_id` later
- B) Add `org_id` FK to all tables now (even if only one org exists)
**Decision**: _(unresolved — recommend option B for forward compatibility, but not decided)_

---

## DECISION-004 · Worker Job Queue Mechanism
**Status**: open  
**Impact**: `cmd/worker`  
**Question**: Should the worker use PostgreSQL-backed job polling (simple, already in stack) or a dedicated message queue (Cloud Pub/Sub, Cloud Tasks)?  
**Options**:
- A) PostgreSQL `job` table with `SELECT ... FOR UPDATE SKIP LOCKED` (v1 default)
- B) Cloud Tasks for durability and dead-letter queuing
- C) Cloud Pub/Sub for fan-out scenarios  
**Decision**: _(unresolved — option A assumed for v1 to avoid new infra; revisit at scale)_

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
**Status**: open  
**Impact**: `cmd/api`, `internal/scenario`, `internal/experiment`  
**Question**: Where should approval/rejection audit events be stored?  
**Options**:
- A) Structured log lines only (Cloud Logging)
- B) Dedicated `audit_event` table in PostgreSQL
- C) Cloud Audit Logs via GCP IAM policy  
**Decision**: _(unresolved — option A assumed for v1 minimum; option B preferred for searchability)_

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
