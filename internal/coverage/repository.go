// Package coverage — repository.go
//
// Repository reads and writes the rule_coverage table.
// Coverage is updated after each completed experiment (AC-05.1).
// Uncovered rules receive maximum weight (AC-05.2).
package coverage

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yourorg/financial-crime-red-team/internal/aml"
)

// Repository handles rule_coverage persistence.
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository creates a Repository backed by the given pool.
func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// GetSummary returns the current coverage summary across all rules (AC-05.1, AC-05.2).
func (r *Repository) GetSummary(ctx context.Context) (Summary, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, rule_id, rule_version, times_triggered,
		       last_triggered_at, coverage_weight, updated_at
		FROM rule_coverage
		ORDER BY coverage_weight DESC, rule_id`)
	if err != nil {
		return Summary{}, fmt.Errorf("coverage: query rules: %w", err)
	}
	defer rows.Close()

	var rules []RuleCoverage
	for rows.Next() {
		var rc RuleCoverage
		var lastAt *time.Time
		if err := rows.Scan(
			&rc.ID, &rc.RuleID, &rc.RuleVersion, &rc.TimesTriggered,
			&lastAt, &rc.CoverageWeight, &rc.UpdatedAt,
		); err != nil {
			return Summary{}, fmt.Errorf("coverage: scan row: %w", err)
		}
		rc.LastTriggeredAt = lastAt
		rules = append(rules, rc)
	}
	if err := rows.Err(); err != nil {
		return Summary{}, err
	}

	total := len(rules)
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
	}, nil
}

// RecordTriggers increments times_triggered for each rule in ruleIDs (AC-05.1).
// Rules not yet in the table are inserted with times_triggered=1 and weight=1/2.
// Rules already present get their weight recalculated using inverse frequency:
//
//	weight = 1 / (times_triggered + 1)
//
// (DECISION-002 option B: inverse frequency algorithm).
func (r *Repository) RecordTriggers(ctx context.Context, ruleIDs []aml.RuleID, ruleVersion string) error {
	for _, rid := range ruleIDs {
		_, err := r.pool.Exec(ctx, `
			INSERT INTO rule_coverage (id, rule_id, rule_version, times_triggered, last_triggered_at, coverage_weight)
			VALUES ($1, $2, $3, 1, NOW(), 0.5)
			ON CONFLICT (rule_id) DO UPDATE
			  SET times_triggered  = rule_coverage.times_triggered + 1,
			      last_triggered_at = NOW(),
			      rule_version      = $3,
			      -- DECISION-002: inverse frequency weight = 1 / (times_triggered + 1)
			      coverage_weight   = 1.0::numeric / (rule_coverage.times_triggered + 1)::numeric`,
			uuid.New(), string(rid), ruleVersion,
		)
		if err != nil {
			return fmt.Errorf("coverage: record trigger %s: %w", rid, err)
		}
	}
	return nil
}

// EnsureRules ensures every known rule has a row in rule_coverage with
// times_triggered=0 and coverage_weight=1.0 (maximum, per AC-05.2).
// Call once on startup or after a rule set change.
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
