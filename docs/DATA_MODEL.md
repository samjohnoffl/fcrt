# Data Model — Financial Crime AI Red Team

## Design Principles
- All tables use UUID primary keys (`gen_random_uuid()`)
- `created_at` / `updated_at` on every table (trigger-maintained)
- No soft-deletes in v1 (append-only for auditability); **DECISION-005** tracks archival
- JSONB for variable-schema blobs (evidence, proposals); indexed with GIN

---

## Tables

### `cohort`
Represents a locked, versioned set of synthetic entities for one experiment group.

| Column | Type | Notes |
|---|---|---|
| id | UUID PK | |
| name | TEXT NOT NULL | Human label |
| predicate | JSONB NOT NULL | Filter DSL applied during generation |
| seed | BIGINT NOT NULL | RNG seed for deterministic generation |
| locked_at | TIMESTAMPTZ | NULL = mutable; set = immutable |
| created_at | TIMESTAMPTZ | |
| updated_at | TIMESTAMPTZ | |

### `bank_entity`
Synthetic customer or legal entity.

| Column | Type | Notes |
|---|---|---|
| id | UUID PK | |
| cohort_id | UUID FK → cohort | |
| entity_type | TEXT | `individual` \| `company` \| `shell` |
| name | TEXT | Synthetic name |
| country_code | CHAR(2) | ISO 3166-1 alpha-2 |
| risk_score | NUMERIC(5,4) | 0.0–1.0 |
| attributes | JSONB | Additional profile fields |
| created_at | TIMESTAMPTZ | |

### `bank_account`
Account belonging to an entity.

| Column | Type | Notes |
|---|---|---|
| id | UUID PK | |
| cohort_id | UUID FK → cohort | |
| entity_id | UUID FK → bank_entity | |
| account_type | TEXT | `checking` \| `savings` \| `crypto` |
| currency | CHAR(3) | ISO 4217 |
| opened_at | TIMESTAMPTZ | Synthetic date |
| attributes | JSONB | |
| created_at | TIMESTAMPTZ | |

### `bank_transaction`
Individual transaction between accounts.

| Column | Type | Notes |
|---|---|---|
| id | UUID PK | |
| cohort_id | UUID FK → cohort | |
| from_account_id | UUID FK → bank_account | |
| to_account_id | UUID FK → bank_account | |
| amount | NUMERIC(18,4) NOT NULL | |
| currency | CHAR(3) | |
| tx_type | TEXT | `wire` \| `ach` \| `crypto` \| `cash` |
| tx_timestamp | TIMESTAMPTZ NOT NULL | |
| reference | TEXT | |
| attributes | JSONB | |
| created_at | TIMESTAMPTZ | |

---

### `scenario_proposal`
Gemini-generated scenario proposal awaiting human approval.

| Column | Type | Notes |
|---|---|---|
| id | UUID PK | |
| cohort_id | UUID FK → cohort | |
| status | TEXT | `pending` \| `approved` \| `rejected` |
| scenario_type | TEXT | e.g. `structuring_evasion` |
| description | TEXT | |
| expected_evasion_vector | TEXT | |
| suggested_tx_pattern | JSONB | Structured mutation spec |
| gemini_prompt | TEXT | Raw prompt sent |
| gemini_response | TEXT | Raw response received |
| gemini_model | TEXT | e.g. `gemini-1.5-pro` |
| correlation_id | UUID | For log tracing |
| approved_by | TEXT | User UID (Firebase) |
| created_at | TIMESTAMPTZ | |
| updated_at | TIMESTAMPTZ | |

---

### `experiment`
A top-level experiment grouping a baseline + variants.

| Column | Type | Notes |
|---|---|---|
| id | UUID PK | |
| cohort_id | UUID FK → cohort | |
| scenario_proposal_id | UUID FK → scenario_proposal NULL | Optional; manual experiments allowed |
| name | TEXT | |
| status | TEXT | `pending` \| `running` \| `completed` \| `failed` |
| aml_rule_version | TEXT NOT NULL | Pinned at experiment creation |
| created_at | TIMESTAMPTZ | |
| updated_at | TIMESTAMPTZ | |

### `experiment_run`
One run (baseline or variant) within an experiment.

| Column | Type | Notes |
|---|---|---|
| id | UUID PK | |
| experiment_id | UUID FK → experiment | |
| run_type | TEXT | `baseline` \| `variant` |
| mutation_spec | JSONB NULL | NULL for baseline |
| alert_count | INT | |
| alert_summary | JSONB | Rule → count map |
| raw_alerts | JSONB | Full alert list |
| started_at | TIMESTAMPTZ | |
| completed_at | TIMESTAMPTZ | |

### `experiment_diff`
Alert-set diff between a variant and its baseline.

| Column | Type | Notes |
|---|---|---|
| id | UUID PK | |
| experiment_id | UUID FK → experiment | |
| baseline_run_id | UUID FK → experiment_run | |
| variant_run_id | UUID FK → experiment_run | |
| evasion_delta | INT | alerts_baseline - alerts_variant |
| new_alert_rule_ids | TEXT[] | Rules newly triggered |
| dropped_alert_rule_ids | TEXT[] | Rules no longer triggered |
| created_at | TIMESTAMPTZ | |

---

### `rule_coverage`
Tracks which AML rules have been exercised.

| Column | Type | Notes |
|---|---|---|
| id | UUID PK | |
| rule_id | TEXT NOT NULL UNIQUE | Stable rule identifier |
| rule_version | TEXT NOT NULL | |
| times_triggered | INT DEFAULT 0 | |
| last_triggered_at | TIMESTAMPTZ | |
| coverage_weight | NUMERIC(5,4) | For adaptive selection scoring |
| updated_at | TIMESTAMPTZ | |

---

### `evidence_bundle`
Immutable, reproducible artifact of one experiment run. One row per
`experiment_pair` (unique on `experiment_id`); upserted by
`internal/evidence.Repository` and served by `GET /experiments/{id}/evidence`.

| Column | Type | Notes |
|---|---|---|
| id | UUID PK | |
| experiment_id | UUID FK → `experiment_pair` | unique; re-pointed from the legacy `experiment` table in migration `000008` |
| bundle | JSONB NOT NULL | Full serialised `evidence.Bundle` |
| checksum | TEXT NOT NULL | SHA-256 of bundle JSON (checksum field excluded) |
| created_at | TIMESTAMPTZ | |
| org_id | UUID FK → organisations | added in `000004`; defaults to the `default` org (`000006`) |

---

## Indexes (Initial Set)
```sql
CREATE INDEX ON bank_transaction (cohort_id, tx_timestamp);
CREATE INDEX ON bank_transaction (from_account_id);
CREATE INDEX ON bank_transaction (to_account_id);
CREATE INDEX ON experiment_run (experiment_id, run_type);
CREATE INDEX ON rule_coverage (coverage_weight DESC);
CREATE INDEX ON evidence_bundle USING GIN (bundle);
CREATE INDEX ON scenario_proposal (cohort_id, status);
```

## Migrations
All DDL lives in `migrations/` as numbered SQL files.
First migration: `000001_init.up.sql` / `000001_init.down.sql`
