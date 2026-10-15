# Product Specification — Financial Crime AI Red Team

## Purpose
A sandboxed platform for red-teaming AML (Anti-Money Laundering) detection models using a synthetic banking environment, Gemini-assisted scenario generation, and reproducible counterfactual experiments.

## Core User
Internal AI safety / FinCrime ML teams who need to systematically discover weaknesses in AML models before deployment, and produce auditable evidence of coverage gaps.

---

## Feature Areas

### 1. Synthetic Banking Environment
- Generate synthetic customer entities, accounts, and transaction graphs that are statistically plausible but contain no real PII
- Parameterised cohort profiles: transaction velocity, currency mix, geographic spread, counterparty topology
- Seed-deterministic generation (same seed → same dataset, reproducible across runs)

### 2. Batch Cohort Selection
- Define a cohort via a SQL-like predicate (JSON DSL) applied to the synthetic bank
- Cohorts stored versioned in PostgreSQL
- Each cohort is immutable once "locked" for an experiment run

### 3. Deterministic Mock AML Harness
- A rule-engine stub implementing representative AML rules (structuring, layering, rapid movement)
- Fully deterministic: given the same transaction set and rule version, produces the same alert set
- Alert output schema matches a simplified version of FinCEN SAR fields
- Rules versioned; harness returns rule version in every response

### 4. Gemini-Assisted Scenario Proposals
- Given a cohort + current AML rule gaps, call Gemini 1.5 Pro to propose novel attack scenarios
- Prompt includes: current rule coverage summary, existing confirmed gaps, cohort profile
- Response parsed into structured `ScenarioProposal` (type, description, expected_evasion_vector, suggested_transaction_pattern)
- Human-in-the-loop: proposals require approval before execution

### 5. Counterfactual Baseline / Variant Experiments
- Baseline: run AML harness on a cohort, record alert set
- Variant: apply a mutation (e.g. split transaction, introduce shell entity) and re-run
- Diff alert sets to measure evasion delta
- All variants logged with parent experiment ID for traceability

### 6. Adaptive Coverage-Guided Selection
- Track which AML rules have been exercised across all experiments
- Score uncovered or under-covered rules higher in next-cohort selection
- Coverage metrics exposed via API and visualised in frontend

### 7. Reproducible Evidence & Regression Tests
- Every experiment emits an Evidence Bundle: cohort snapshot, scenario params, AML output diff, coverage delta, Gemini trace (prompt + response)
- Evidence bundles stored in PostgreSQL (JSONB) and optionally exported as JSON files
- Regression: re-run any evidence bundle deterministically and assert identical AML diff

---

## Non-Goals (v1)
- Real-time streaming transaction processing
- Integration with live AML systems
- Multi-tenant isolation (single-org deployment)
- Mobile frontend

---

## Glossary
| Term | Definition |
|---|---|
| Cohort | A versioned, locked set of synthetic entities/accounts/transactions |
| Scenario | A proposed or executed attack pattern targeting an AML rule gap |
| Experiment | One baseline + ≥1 variant runs comparing AML alert sets |
| Evidence Bundle | The full reproducible artifact of one experiment run |
| Evasion Delta | Change in alert count/type between baseline and variant |
| Coverage | Set of AML rules exercised across all experiments |
