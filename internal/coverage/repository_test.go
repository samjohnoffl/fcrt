// Package coverage — repository_test.go
//
// Integration tests for the three coverage maps. They require a real
// PostgreSQL database with all migrations applied (DATABASE_TEST_URL).
package coverage

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yourorg/financial-crime-red-team/internal/aml"
)

func openTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test in -short mode")
	}
	url := os.Getenv("DATABASE_TEST_URL")
	if url == "" {
		url = "host=/var/run/postgresql dbname=fincrime_test sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Skipf("cannot connect to test DB: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("test DB not reachable: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func resetCoverage(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	tables := []string{"rule_coverage", "outcome_coverage", "scenario_coverage"}
	clear := func() {
		for _, table := range tables {
			_, _ = pool.Exec(context.Background(), "DELETE FROM "+table)
		}
	}
	clear()
	// Leave the shared development database as we found it: these maps are
	// global, so a test that deletes them must restore an empty state at exit
	// rather than leave synthetic coverage behind.
	t.Cleanup(clear)
}

func TestRecordOutcomes_UpdatesControlAndOutcomeMaps(t *testing.T) {
	pool := openTestPool(t)
	resetCoverage(t, pool)
	repo := NewRepository(pool)
	ctx := context.Background()

	outcomes := []aml.RuleOutcome{
		{RuleID: aml.RuleStructuring, RuleVersion: "v0.2.0", Status: aml.OutcomeTriggered, ReasonCode: aml.ReasonPatternDetected},
		{RuleID: aml.RuleLayering, RuleVersion: "v0.2.0", Status: aml.OutcomeNotTriggered, ReasonCode: aml.ReasonThresholdNotMet},
		{RuleID: aml.RuleVelocity, RuleVersion: "v0.2.0", Status: aml.OutcomeNotApplicable, ReasonCode: aml.ReasonNoEvents},
		{RuleID: aml.RuleDormancyActivation, RuleVersion: "v0.2.0", Status: aml.OutcomeInconclusive, ReasonCode: aml.ReasonInsufficientHistory},
		{RuleID: aml.RuleOwnershipRelationship, RuleVersion: "v0.2.0", Status: aml.OutcomeFailed, ReasonCode: aml.ReasonEvaluationError},
	}
	if err := repo.RecordOutcomes(ctx, outcomes, "v0.2.0"); err != nil {
		t.Fatalf("RecordOutcomes: %v", err)
	}
	// A second evaluation of the same controls must accumulate counts.
	if err := repo.RecordOutcomes(ctx, outcomes, "v0.2.0"); err != nil {
		t.Fatalf("RecordOutcomes (second): %v", err)
	}

	summary, err := repo.GetSummary(ctx)
	if err != nil {
		t.Fatalf("GetSummary: %v", err)
	}

	if summary.TotalRules != len(aml.AllRules) {
		t.Errorf("total_rules=%d, want %d (all known controls, including unevaluated)", summary.TotalRules, len(aml.AllRules))
	}

	byRule := make(map[aml.RuleID]RuleCoverage, len(summary.Rules))
	for _, rc := range summary.Rules {
		byRule[rc.RuleID] = rc
	}

	str := byRule[aml.RuleStructuring]
	if str.TimesTriggered != 2 {
		t.Errorf("structuring times_triggered=%d, want 2", str.TimesTriggered)
	}
	if str.LastStatus != string(aml.OutcomeTriggered) {
		t.Errorf("structuring last_status=%q, want triggered", str.LastStatus)
	}
	if str.LastTriggeredAt == nil {
		t.Error("structuring last_triggered_at is nil after a trigger")
	}

	dorm := byRule[aml.RuleDormancyActivation]
	if dorm.TimesInconclusive != 2 || dorm.TimesTriggered != 0 {
		t.Errorf("dormancy inconclusive=%d triggered=%d, want 2/0", dorm.TimesInconclusive, dorm.TimesTriggered)
	}

	own := byRule[aml.RuleOwnershipRelationship]
	if own.TimesFailed != 2 {
		t.Errorf("ownership times_failed=%d, want 2", own.TimesFailed)
	}

	if summary.CoveredRules != 1 {
		t.Errorf("covered_rules=%d, want 1 (only structuring triggered)", summary.CoveredRules)
	}

	// Outcome map must contain exactly one cell per (rule, status) pair.
	cells := make(map[string]int, len(summary.Outcomes))
	for _, c := range summary.Outcomes {
		cells[string(c.RuleID)+"/"+string(c.Status)] = c.TimesSeen
	}
	if got := cells["structuring_v1/triggered"]; got != 2 {
		t.Errorf("outcome cell structuring/triggered=%d, want 2", got)
	}
	if got := cells["dormancy_activation_v1/inconclusive"]; got != 2 {
		t.Errorf("outcome cell dormancy/inconclusive=%d, want 2", got)
	}
	if _, ok := cells["structuring_v1/not_triggered"]; ok {
		t.Error("unexpected outcome cell structuring/not_triggered")
	}
}

func TestRecordOutcomes_MissingStatusBecomesFailed(t *testing.T) {
	pool := openTestPool(t)
	resetCoverage(t, pool)
	repo := NewRepository(pool)
	ctx := context.Background()

	if err := repo.RecordOutcomes(ctx, []aml.RuleOutcome{
		{RuleID: aml.RuleRapidMovement, RuleVersion: "v0.2.0"}, // status omitted by bug
	}, "v0.2.0"); err != nil {
		t.Fatalf("RecordOutcomes: %v", err)
	}

	summary, err := repo.GetSummary(ctx)
	if err != nil {
		t.Fatalf("GetSummary: %v", err)
	}
	for _, rc := range summary.Rules {
		if rc.RuleID != aml.RuleRapidMovement {
			continue
		}
		if rc.TimesFailed != 1 || rc.TimesTriggered != 0 {
			t.Fatalf("rapid_movement failed=%d triggered=%d, want 1/0", rc.TimesFailed, rc.TimesTriggered)
		}
		return
	}
	t.Fatal("rapid_movement row missing")
}

func TestGetSummary_EmptyMapsAreArrays(t *testing.T) {
	pool := openTestPool(t)
	resetCoverage(t, pool)
	repo := NewRepository(pool)

	summary, err := repo.GetSummary(context.Background())
	if err != nil {
		t.Fatalf("GetSummary: %v", err)
	}
	if summary.Scenario == nil {
		t.Error("scenario is nil, want an empty array")
	}
	if summary.Outcomes == nil {
		t.Error("outcomes is nil, want an empty array")
	}
	if len(summary.Rules) != len(aml.AllRules) {
		t.Errorf("rules=%d, want %d known controls even before the first run", len(summary.Rules), len(aml.AllRules))
	}
}

func TestRecordScenario_UpdatesGenomeMap(t *testing.T) {
	pool := openTestPool(t)
	resetCoverage(t, pool)
	repo := NewRepository(pool)
	ctx := context.Background()

	dims := map[string]string{
		"actors":       "company",
		"geography":    "cross_border",
		"transactions": "structuring",
	}
	if err := repo.RecordScenario(ctx, dims); err != nil {
		t.Fatalf("RecordScenario: %v", err)
	}
	if err := repo.RecordScenario(ctx, dims); err != nil {
		t.Fatalf("RecordScenario (second): %v", err)
	}

	summary, err := repo.GetSummary(ctx)
	if err != nil {
		t.Fatalf("GetSummary: %v", err)
	}
	if len(summary.Scenario) != 3 {
		t.Fatalf("scenario cells=%d, want 3", len(summary.Scenario))
	}
	for _, c := range summary.Scenario {
		if c.TimesSeen != 2 {
			t.Errorf("%s=%s times_seen=%d, want 2", c.Dimension, c.Value, c.TimesSeen)
		}
	}
}
