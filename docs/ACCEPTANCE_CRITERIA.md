# Acceptance Criteria — Financial Crime AI Red Team

## Format
Each criterion uses the form:
> **Given** [precondition] **When** [action] **Then** [expected outcome]

Criteria are grouped by feature area and tagged with the source product feature.

---

## AC-01 · Synthetic Bank Generation

**AC-01.1** Deterministic generation
> Given a cohort with seed `S` and predicate `P`
> When the worker runs GenerateCohort
> Then running GenerateCohort again with the same `S` and `P` produces byte-identical `bank_entity`, `bank_account`, and `bank_transaction` rows (same IDs, same values)

**AC-01.2** Cohort locking
> Given a cohort that has been generated and locked
> When any service attempts to insert or update rows in `bank_entity`, `bank_account`, or `bank_transaction` for that cohort
> Then the operation fails with a domain error `ErrCohortLocked`

**AC-01.3** Predicate filtering
> Given a predicate `{"entity_type": "individual", "country_code": "US"}`
> When GenerateCohort runs
> Then all generated `bank_entity` rows satisfy the predicate; no rows with other types or countries are created

---

## AC-02 · Mock AML Harness

**AC-02.1** Determinism
> Given a locked cohort C and AML rule version V
> When `Harness.Run(C, V)` is called twice in succession
> Then both calls return identical `HarnessResult` structs (same alert IDs, same rule IDs, same counts)

**AC-02.2** Rule version pinning
> Given an experiment created when rule version is `v1.2.0`
> When the rule set is later upgraded to `v1.3.0`
> Then re-running the experiment evidence bundle via the regression runner uses `v1.2.0` and produces the same alert set

**AC-02.3** StructuringRule coverage
> Given a cohort containing transactions where a single entity splits a $10,000 payment into 3 sub-$10,000 transactions within 24 hours to the same counterparty
> When the harness runs StructuringRule
> Then an alert of type `structuring` is generated for that entity

---

## AC-03 · Gemini Scenario Proposals

**AC-03.1** Proposal stored with raw trace
> Given an approved cohort
> When `POST /api/v1/scenarios` is called
> Then a `scenario_proposal` row is created with non-null `gemini_prompt`, `gemini_response`, and `correlation_id`

**AC-03.2** Proposal requires approval before experiment
> Given a `scenario_proposal` with status `pending`
> When `POST /api/v1/experiments` references that scenario proposal ID
> Then the API returns HTTP 422 with error code `scenario_not_approved`

**AC-03.3** Proposal response parsed into structured fields
> Given Gemini returns a valid JSON response matching the ScenarioProposal schema
> When the response is processed
> Then `scenario_type`, `description`, `expected_evasion_vector`, and `suggested_tx_pattern` are all populated in the DB row

---

## AC-04 · Counterfactual Experiments

**AC-04.1** Baseline and variant runs created
> Given an approved experiment
> When the worker executes the experiment
> Then exactly one `experiment_run` row with `run_type=baseline` and at least one with `run_type=variant` exist for that experiment

**AC-04.2** Evasion delta computed correctly
> Given baseline alerts = 5, variant alerts = 3
> When `experiment_diff` is computed
> Then `evasion_delta = 2` and `dropped_alert_rule_ids` lists the 2 rules no longer triggered

**AC-04.3** Mutation spec stored
> Given a `SplitTransaction` mutation applied in a variant
> When the variant run completes
> Then `experiment_run.mutation_spec` contains the full mutation parameters used

---

## AC-05 · Coverage Tracking

**AC-05.1** Coverage updated after each experiment
> Given experiment E completes successfully
> When coverage is recomputed
> Then `rule_coverage.times_triggered` is incremented for every rule ID that appeared in the baseline or variant alerts

**AC-05.2** Uncovered rules score highest
> Given rule R has `times_triggered = 0`
> When coverage weights are computed
> Then R has the highest possible `coverage_weight` score among all rules

---

## AC-06 · Evidence Bundles

**AC-06.1** Bundle is reproducible
> Given an evidence bundle with ID B and checksum H
> When the regression runner replays the bundle
> Then the resulting AML alert diff is identical and the newly computed checksum equals H

**AC-06.2** Bundle downloadable via API
> Given an experiment in status `completed`
> When `GET /api/v1/experiments/:id/evidence` is called
> Then the response is valid JSON containing keys: `cohort_snapshot`, `scenario_params`, `aml_diff`, `coverage_delta`, `gemini_trace`, `checksum`

---

## AC-07 · Authentication

**AC-07.1** Unauthenticated request rejected
> Given no `Authorization` header
> When any `/api/v1/*` endpoint is called
> Then HTTP 401 is returned

**AC-07.2** Expired token rejected
> Given an expired Firebase ID token
> When any `/api/v1/*` endpoint is called
> Then HTTP 401 is returned with error code `token_expired`
