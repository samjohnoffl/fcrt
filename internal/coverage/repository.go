// Package coverage — repository.go
//
// Repository reads and writes the three coverage maps:
//
//	rule_coverage      — control coverage (all five statuses)
//	outcome_coverage   — rule x status cross-tab
//	scenario_coverage  — genome dimension x value
//
// Coverage is updated after each evaluation (baseline and variant), so the
// counts represent control evaluations, not experiments.
package coverage

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yourorg/financial-crime-red-team/internal/aml"
)

// Repository handles coverage persistence.
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository creates a Repository backed by the given pool.
func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// GetSummary returns the three coverage maps (AC-05.1, AC-05.2).
func (r *Repository) GetSummary(ctx context.Context) (Summary, error) {
	rules, err := r.rules(ctx)
	if err != nil {
		return Summary{}, err
	}
	scenario, err := r.scenario(ctx)
	if err != nil {
		return Summary{}, err
	}
	outcomes, err := r.outcomes(ctx)
	if err != nil {
		return Summary{}, err
	}
	// The API contract is an array, never null, even before the first run.
	if scenario == nil {
		scenario = []ScenarioCoverage{}
	}
	if outcomes == nil {
		outcomes = []OutcomeCoverage{}
	}

	total := len(rules)
	// Known controls that have never been evaluated still belong in the control
	// map (at zero) so the UI does not silently omit an untested family.
	seen := make(map[aml.RuleID]bool, len(rules))
	for _, rc := range rules {
		seen[rc.RuleID] = true
	}
	for _, id := range aml.AllRules {
		if !seen[id] {
			rules = append(rules, RuleCoverage{
				RuleID:         id,
				RuleVersion:    aml.ControlVersion,
				CoverageWeight: 1.0,
			})
			total++
		}
	}
	covered := 0
	for _, rc := range rules {
		if rc.TimesTriggered > 0 {
			covered++
		}
	}
	pct := 0.0
	if total > 0 {
		pct = float64(covered) / float64(total) * 100
	}
	return Summary{
		TotalRules:   total,
		CoveredRules: covered,
		CoveragePct:  pct,
		Rules:        rules,
		Scenario:     scenario,
		Outcomes:     outcomes,
	}, nil
}

func (r *Repository) rules(ctx context.Context) ([]RuleCoverage, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, rule_id, rule_version, times_triggered,
		       times_not_triggered, times_not_applicable,
		       times_inconclusive, times_failed,
		       COALESCE(last_status, ''),
		       last_triggered_at, last_evaluated_at,
		       coverage_weight, updated_at
		FROM rule_coverage
		ORDER BY coverage_weight DESC, rule_id`)
	if err != nil {
		return nil, fmt.Errorf("coverage: query rules: %w", err)
	}
	defer rows.Close()

	var rules []RuleCoverage
	for rows.Next() {
		var rc RuleCoverage
		var lastTriggered, lastEvaluated *time.Time
		if err := rows.Scan(
			&rc.ID, &rc.RuleID, &rc.RuleVersion, &rc.TimesTriggered,
			&rc.TimesNotTriggered, &rc.TimesNotApplicable,
			&rc.TimesInconclusive, &rc.TimesFailed,
			&rc.LastStatus, &lastTriggered, &lastEvaluated,
			&rc.CoverageWeight, &rc.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("coverage: scan row: %w", err)
		}
		rc.LastTriggeredAt = lastTriggered
		rc.LastEvaluatedAt = lastEvaluated
		rules = append(rules, rc)
	}
	return rules, rows.Err()
}

func (r *Repository) scenario(ctx context.Context) ([]ScenarioCoverage, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT dimension, value, times_seen, last_seen_at
		FROM scenario_coverage
		ORDER BY dimension, value`)
	if err != nil {
		return nil, fmt.Errorf("coverage: query scenario map: %w", err)
	}
	defer rows.Close()

	var cells []ScenarioCoverage
	for rows.Next() {
		var c ScenarioCoverage
		if err := rows.Scan(&c.Dimension, &c.Value, &c.TimesSeen, &c.LastSeenAt); err != nil {
			return nil, fmt.Errorf("coverage: scan scenario cell: %w", err)
		}
		cells = append(cells, c)
	}
	return cells, rows.Err()
}

func (r *Repository) outcomes(ctx context.Context) ([]OutcomeCoverage, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT rule_id, status, times_seen, last_seen_at
		FROM outcome_coverage
		ORDER BY rule_id, status`)
	if err != nil {
		return nil, fmt.Errorf("coverage: query outcome map: %w", err)
	}
	defer rows.Close()

	var cells []OutcomeCoverage
	for rows.Next() {
		var c OutcomeCoverage
		if err := rows.Scan(&c.RuleID, &c.Status, &c.TimesSeen, &c.LastSeenAt); err != nil {
			return nil, fmt.Errorf("coverage: scan outcome cell: %w", err)
		}
		cells = append(cells, c)
	}
	return cells, rows.Err()
}

// RecordOutcomes updates control coverage and outcome coverage from the
// five-status results of one or more evaluations. It must be called for the
// baseline and variant runs separately; a rule that was not applicable is
// recorded as such, never as a trigger.
func (r *Repository) RecordOutcomes(ctx context.Context, outcomes []aml.RuleOutcome, ruleVersion string) error {
	if len(outcomes) == 0 {
		return nil
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("coverage: begin tx: %w", err)
	}
	defer tx.Rollback(ctx) // nolint

	for _, oc := range outcomes {
		if oc.Status == "" {
			// A missing status is a harness error, not a clean result.
			oc.Status = aml.OutcomeFailed
			oc.ReasonCode = aml.ReasonEvaluationError
		}
		if ruleVersion == "" {
			ruleVersion = oc.RuleVersion
		}
		if err := upsertRuleOutcome(ctx, tx, oc, ruleVersion); err != nil {
			return err
		}
		if err := upsertOutcomeCoverage(ctx, tx, oc); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func upsertRuleOutcome(ctx context.Context, tx pgx.Tx, oc aml.RuleOutcome, ruleVersion string) error {
	// Weight is inverse frequency of triggers, with a floor of 0.5 for a
	// never-triggered control so uncovered controls stay attractive targets.
	const q = `
		INSERT INTO rule_coverage
		  (id, rule_id, rule_version, times_triggered,
		   times_not_triggered, times_not_applicable, times_inconclusive, times_failed,
		   last_status, last_evaluated_at, last_triggered_at, coverage_weight)
		VALUES ($1, $2, $3,
		        CASE WHEN $4 = 'triggered' THEN 1 ELSE 0 END,
		        CASE WHEN $4 = 'not_triggered' THEN 1 ELSE 0 END,
		        CASE WHEN $4 = 'not_applicable' THEN 1 ELSE 0 END,
		        CASE WHEN $4 = 'inconclusive' THEN 1 ELSE 0 END,
		        CASE WHEN $4 = 'failed' THEN 1 ELSE 0 END,
		        $4,
		        NOW(),
		        CASE WHEN $4 = 'triggered' THEN NOW() ELSE NULL END,
		        CASE WHEN $4 = 'triggered' THEN 0.5 ELSE 1.0 END)
		ON CONFLICT (rule_id) DO UPDATE SET
		  rule_version         = EXCLUDED.rule_version,
		  times_triggered      = rule_coverage.times_triggered
		                         + CASE WHEN $4 = 'triggered' THEN 1 ELSE 0 END,
		  times_not_triggered  = rule_coverage.times_not_triggered
		                         + CASE WHEN $4 = 'not_triggered' THEN 1 ELSE 0 END,
		  times_not_applicable = rule_coverage.times_not_applicable
		                         + CASE WHEN $4 = 'not_applicable' THEN 1 ELSE 0 END,
		  times_inconclusive   = rule_coverage.times_inconclusive
		                         + CASE WHEN $4 = 'inconclusive' THEN 1 ELSE 0 END,
		  times_failed         = rule_coverage.times_failed
		                         + CASE WHEN $4 = 'failed' THEN 1 ELSE 0 END,
		  last_status          = EXCLUDED.last_status,
		  last_evaluated_at    = NOW(),
		  last_triggered_at    = CASE WHEN $4 = 'triggered' THEN NOW()
		                              ELSE rule_coverage.last_triggered_at END,
		  coverage_weight      = 1.0::numeric /
		                         (rule_coverage.times_triggered
		                          + CASE WHEN $4 = 'triggered' THEN 1 ELSE 0 END
		                          + 1)::numeric,
		  updated_at           = NOW()`
	_, err := tx.Exec(ctx, q, uuid.New(), string(oc.RuleID), ruleVersion, string(oc.Status))
	if err != nil {
		return fmt.Errorf("coverage: record outcome %s/%s: %w", oc.RuleID, oc.Status, err)
	}
	return nil
}

func upsertOutcomeCoverage(ctx context.Context, tx pgx.Tx, oc aml.RuleOutcome) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO outcome_coverage (rule_id, status, times_seen)
		VALUES ($1, $2, 1)
		ON CONFLICT (rule_id, status) DO UPDATE
		  SET times_seen   = outcome_coverage.times_seen + 1,
		      last_seen_at = NOW()`,
		string(oc.RuleID), string(oc.Status))
	if err != nil {
		return fmt.Errorf("coverage: record outcome cell %s/%s: %w", oc.RuleID, oc.Status, err)
	}
	return nil
}

// RecordScenario updates the scenario map from a genome's dimensions.
// Values are recorded verbatim so the UI can show what was exercised.
func (r *Repository) RecordScenario(ctx context.Context, dims map[string]string) error {
	if len(dims) == 0 {
		return nil
	}
	// Sort keys for deterministic execution order. Row order does not affect
	// the aggregate result, but stable order keeps tests and traces readable.
	keys := make([]string, 0, len(dims))
	for k := range dims {
		keys = append(keys, k)
	}
	sortStrings(keys)

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("coverage: begin scenario tx: %w", err)
	}
	defer tx.Rollback(ctx) // nolint

	for _, k := range keys {
		v := strings.TrimSpace(dims[k])
		if v == "" {
			continue
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO scenario_coverage (dimension, value, times_seen)
			VALUES ($1, $2, 1)
			ON CONFLICT (dimension, value) DO UPDATE
			  SET times_seen   = scenario_coverage.times_seen + 1,
			      last_seen_at = NOW()`,
			k, v); err != nil {
			return fmt.Errorf("coverage: record scenario %s=%s: %w", k, v, err)
		}
	}
	return tx.Commit(ctx)
}

// EnsureRules ensures every known rule has a row in rule_coverage with
// times_triggered=0 and coverage_weight=1.0 (maximum, per AC-05.2).
func (r *Repository) EnsureRules(ctx context.Context, ruleIDs []aml.RuleID, ruleVersion string) error {
	for _, rid := range ruleIDs {
		_, err := r.pool.Exec(ctx, `
			INSERT INTO rule_coverage (id, rule_id, rule_version, times_triggered, coverage_weight)
			VALUES ($1, $2, $3, 0, 1.0)
			ON CONFLICT (rule_id) DO NOTHING`,
			uuid.New(), string(rid), ruleVersion,
		)
		if err != nil {
			return fmt.Errorf("coverage: ensure rule %s: %w", rid, err)
		}
	}
	return nil
}

// sortStrings is a tiny insertion sort to avoid importing sort in this file's
// hot path (dimension maps are fewer than ten entries).
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
