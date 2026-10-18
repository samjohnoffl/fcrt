// cmd/worker/main.go — Background job worker entry point.
// Polls the `job` table and dispatches work to internal packages.
// DECISION-004: Uses PostgreSQL-backed polling (option A).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yourorg/financial-crime-red-team/internal/aml"
	"github.com/yourorg/financial-crime-red-team/internal/bank"
	"github.com/yourorg/financial-crime-red-team/internal/coverage"
	"github.com/yourorg/financial-crime-red-team/internal/experiment"
	"github.com/yourorg/financial-crime-red-team/internal/scenario"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel(),
	}))
	slog.SetDefault(logger)

	// ── Config from environment ────────────────────────────────────────────
	dbURL := mustEnv("DATABASE_URL")
	pollInterval := pollIntervalFromEnv()
	amlRuleVersion := envOrDefault("AML_RULE_VERSION", aml.ControlVersion)

	// ── Database pool ──────────────────────────────────────────────────────
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		slog.Error("open db pool", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		slog.Error("db ping failed", "err", err)
		os.Exit(1)
	}
	slog.Info("database connected")

	// ── Repositories ───────────────────────────────────────────────────────
	bankRepo := bank.NewRepository(pool)
	scenarioRepo := scenario.NewRepository(pool)
	experimentRepo := experiment.NewRepository(pool)
	coverageRepo := coverage.NewRepository(pool)

	// ── Job polling loop ───────────────────────────────────────────────────
	slog.Info("worker starting", "poll_interval", pollInterval, "aml_rule_version", amlRuleVersion)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				processJobs(ctx, pool, bankRepo, scenarioRepo, experimentRepo, coverageRepo, amlRuleVersion, logger)
			}
		}
	}()

	<-stop
	slog.Info("worker shutting down")
}

// processJobs selects a pending job atomically, dispatches it, and updates status.
func processJobs(
	ctx context.Context,
	pool *pgxpool.Pool,
	bankRepo *bank.Repository,
	scenarioRepo *scenario.Repository,
	experimentRepo *experiment.Repository,
	coverageRepo *coverage.Repository,
	amlRuleVersion string,
	logger *slog.Logger,
) {
	// Claim one pending job with SKIP LOCKED (avoids worker contention).
	const claimQ = `
		SELECT id, job_type, payload
		FROM job
		WHERE status = 'pending'
		ORDER BY created_at ASC
		FOR UPDATE SKIP LOCKED
		LIMIT 1`

	var id uuid.UUID
	var jobType string
	var payload []byte
	err := pool.QueryRow(ctx, claimQ).Scan(&id, &jobType, &payload)
	if err != nil {
		// No pending jobs is not an error.
		return
	}

	logger.Info("job claimed", "job_id", id, "job_type", jobType)

	// Mark as running.
	if _, err := pool.Exec(ctx, `UPDATE job SET status = 'running', updated_at = NOW() WHERE id = $1`, id); err != nil {
		logger.Error("job mark running failed", "job_id", id, "err", err)
		return
	}

	// Dispatch.
	dispatchErr := dispatch(ctx, jobType, payload, bankRepo, scenarioRepo, experimentRepo, coverageRepo, amlRuleVersion)

	// Record completion or failure.
	if dispatchErr != nil {
		logger.Error("job failed", "job_id", id, "job_type", jobType, "err", dispatchErr)
		// Increment attempts; retry later if under max_attempts.
		var attempts int
		pool.QueryRow(ctx, `SELECT attempts FROM job WHERE id = $1`, id).Scan(&attempts)
		if attempts+1 >= 5 {
			_, _ = pool.Exec(ctx, `
				UPDATE job SET status = 'failed', last_error = $2, updated_at = NOW()
				WHERE id = $1`, id, dispatchErr.Error())
		} else {
			_, _ = pool.Exec(ctx, `
				UPDATE job SET status = 'pending', attempts = attempts + 1,
					last_error = $2, updated_at = NOW()
				WHERE id = $1`, id, dispatchErr.Error())
		}
	} else {
		logger.Info("job completed", "job_id", id, "job_type", jobType)
		if _, err := pool.Exec(ctx, `UPDATE job SET status = 'completed', updated_at = NOW() WHERE id = $1`, id); err != nil {
			logger.Error("job mark completed failed", "job_id", id, "err", err)
		}
	}
}

// dispatch routes a job to the appropriate handler.
func dispatch(
	ctx context.Context,
	jobType string,
	payload []byte,
	bankRepo *bank.Repository,
	scenarioRepo *scenario.Repository,
	experimentRepo *experiment.Repository,
	coverageRepo *coverage.Repository,
	amlRuleVersion string,
) error {

	switch jobType {
	case "generate_cohort":
		return handleGenerateCohort(ctx, payload, bankRepo)
	case "run_experiment":
		return handleRunExperiment(ctx, payload, bankRepo, scenarioRepo, experimentRepo, coverageRepo, amlRuleVersion)
	case "recompute_coverage":
		return handleRecomputeCoverage(ctx, payload, coverageRepo)
	default:
		return fmt.Errorf("unknown job_type: %s", jobType)
	}
}

// ── Job handlers ──────────────────────────────────────────────────────────

func handleGenerateCohort(
	ctx context.Context,
	payload []byte,
	bankRepo *bank.Repository,
) error {
	var job struct {
		CohortID   uuid.UUID `json:"cohort_id"`
		Seed       int64     `json:"seed"`
		WindowDays int       `json:"window_days"`
	}
	if err := json.Unmarshal(payload, &job); err != nil {
		return fmt.Errorf("parse generate_cohort payload: %w", err)
	}

	// Get the cohort to extract predicate + window times.
	c, err := bankRepo.GetCohort(ctx, job.CohortID)
	if err != nil {
		return fmt.Errorf("get cohort: %w", err)
	}

	now := time.Now().UTC()
	windowStart := now.AddDate(0, 0, -job.WindowDays)
	windowEnd := now

	// Generate synthetic data deterministically.
	result, err := bank.Generate(bank.GenerateParams{
		CohortID:    job.CohortID,
		Seed:        job.Seed,
		Predicate:   c.Predicate,
		WindowStart: windowStart,
		WindowEnd:   windowEnd,
	})
	if err != nil {
		return fmt.Errorf("generate bank data: %w", err)
	}

	// Persist + lock cohort atomically.
	if err := bankRepo.SaveGenerated(ctx, result); err != nil {
		return fmt.Errorf("save generated cohort: %w", err)
	}

	slog.Info("cohort generated", "cohort_id", job.CohortID, "entities", result.Manifest.EntityCount, "transactions", result.Manifest.TxCount)
	return nil
}

func handleRunExperiment(
	ctx context.Context,
	payload []byte,
	bankRepo *bank.Repository,
	scenarioRepo *scenario.Repository,
	experimentRepo *experiment.Repository,
	coverageRepo *coverage.Repository,
	amlRuleVersion string,
) error {
	var job struct {
		PairID             uuid.UUID      `json:"pair_id"`
		ScenarioProposalID uuid.UUID      `json:"scenario_proposal_id,omitempty"`
		CohortID           uuid.UUID      `json:"cohort_id"`
		Name               string         `json:"name"`
		DeclaredChange     string         `json:"declared_change"`
		MutationType       string         `json:"mutation_type"`
		MutationSpec       map[string]any `json:"mutation_spec"`
	}
	if err := json.Unmarshal(payload, &job); err != nil {
		return fmt.Errorf("parse run_experiment payload: %w", err)
	}

	// Get the cohort for manifest + window.
	c, err := bankRepo.GetCohort(ctx, job.CohortID)
	if err != nil {
		return fmt.Errorf("get cohort: %w", err)
	}
	if c.LockedAt == nil {
		return fmt.Errorf("cohort must be locked before running experiment")
	}

	// Select batch from the cohort.
	sel, err := bankRepo.SelectBatch(ctx, job.CohortID, *c.WindowStart, *c.WindowEnd)
	if err != nil {
		return fmt.Errorf("select batch: %w", err)
	}

	// Build control config.
	cfg := aml.DefaultControlConfig()

	// Build input context.
	inputCtx, err := experiment.NewInputContext(c.Manifest, *c.WindowStart, *c.WindowEnd)
	if err != nil {
		return fmt.Errorf("build input context: %w", err)
	}

	// Build mutation spec.
	mutationType := experiment.MutationType(job.MutationType)
	mutationSpec := job.MutationSpec
	if mutationSpec == nil {
		mutationSpec = make(map[string]any)
	}

	spec := experiment.PairSpec{
		Name:           job.Name,
		Input:          inputCtx,
		Config:         cfg,
		DeclaredChange: job.DeclaredChange,
		MutationType:   mutationType,
		MutationSpec:   mutationSpec,
	}

	// Execute the experiment (deterministic).
	startedAt := time.Now().UTC()
	result, err := experiment.Execute(spec, sel, startedAt)
	if err != nil {
		return fmt.Errorf("execute experiment: %w", err)
	}

	// Persist to DB.
	if err := experimentRepo.SavePairResult(ctx, result); err != nil {
		return fmt.Errorf("save experiment result: %w", err)
	}

	// Update rule coverage from both baseline and variant alerts.
	var ruleIDs []aml.RuleID
	for _, a := range result.Baseline.RawAlerts {
		ruleIDs = append(ruleIDs, a.RuleID)
	}
	for _, a := range result.Variant.RawAlerts {
		ruleIDs = append(ruleIDs, a.RuleID)
	}
	if len(ruleIDs) > 0 {
		if err := coverageRepo.RecordTriggers(ctx, ruleIDs, amlRuleVersion); err != nil {
			slog.Warn("record coverage triggers failed", "pair_id", result.Pair.ID, "err", err)
		}
	}

	slog.Info("experiment completed", "pair_id", result.Pair.ID, "evasion_delta", result.Outcome.EvasionDelta)
	return nil
}

func handleRecomputeCoverage(
	ctx context.Context,
	payload []byte,
	coverageRepo *coverage.Repository,
) error {
	var job struct {
		RuleIDs     []string `json:"rule_ids,omitempty"`
		RuleVersion string   `json:"rule_version"`
	}
	if err := json.Unmarshal(payload, &job); err != nil {
		return fmt.Errorf("parse recompute_coverage payload: %w", err)
	}

	ruleIDs := make([]aml.RuleID, len(job.RuleIDs))
	for i, rid := range job.RuleIDs {
		ruleIDs[i] = aml.RuleID(rid)
	}

	if len(ruleIDs) == 0 {
		// Ensure all known rules have rows.
		ruleIDs = []aml.RuleID{aml.RuleStructuring, aml.RuleLayering, aml.RuleRapidMovement}
	}

	if err := coverageRepo.EnsureRules(ctx, ruleIDs, job.RuleVersion); err != nil {
		return fmt.Errorf("ensure coverage rules: %w", err)
	}

	slog.Info("coverage recomputed", "rule_count", len(ruleIDs))
	return nil
}

// ── Helpers ───────────────────────────────────────────────────────────────

func logLevel() slog.Level {
	switch os.Getenv("LOG_LEVEL") {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func pollIntervalFromEnv() time.Duration {
	v := os.Getenv("WORKER_POLL_INTERVAL_SECONDS")
	if v == "" {
		return 5 * time.Second
	}
	d, err := time.ParseDuration(v + "s")
	if err != nil {
		return 5 * time.Second
	}
	return d
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		slog.Error("required env var not set", "key", key)
		os.Exit(1)
	}
	return v
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// ─── Adaptive Scoring ───────────────────────────────────────────────────────
//
// S(x) = wc·C(x) + wn·N(x) + wi·I(x) + wv·V(x)
//
// C(x): coverage gain (inverse of rule coverage weight)
// N(x): novelty/diversity (genome distance from prior candidates)
// I(x): information gain proxy (heuristic - uncertainty reduction)
// V(x): feasibility/validity (1.0 if valid, 0.0 if invalid)
//
// All components normalized to [0,1]. Weights versioned via ADAPTIVE_WEIGHT_VERSION.

type AdaptiveWeights struct {
	WC float64 // coverage gain weight
	WN float64 // novelty weight
	WI float64 // information gain weight
	WV float64 // validity weight
}

func DefaultAdaptiveWeights() AdaptiveWeights {
	return AdaptiveWeights{
		WC: 0.4,
		WN: 0.3,
		WI: 0.2,
		WV: 0.1,
	}
}

func (w AdaptiveWeights) Validate() error {
	sum := w.WC + w.WN + w.WI + w.WV
	if math.Abs(sum-1.0) > 0.001 {
		return fmt.Errorf("adaptive weights must sum to 1.0, got %.3f", sum)
	}
	return nil
}

type CandidateScore struct {
	CandidateID uuid.UUID
	Score       float64
	Components  ScoreComponents
}

type ScoreComponents struct {
	C float64 // coverage gain
	N float64 // novelty
	I float64 // information gain
	V float64 // validity
}

func ComputeAdaptiveScore(
	ctx context.Context,
	pool *pgxpool.Pool,
	coverageRepo *coverage.Repository,
	candidate scenario.Proposal,
	priorCandidates []scenario.Proposal,
	weights AdaptiveWeights,
) (CandidateScore, error) {

	c := computeCoverageGain(ctx, pool, candidate)
	n := computeNovelty(candidate, priorCandidates)
	i := computeInfoGain(ctx, pool, candidate)
	v := computeValidity(candidate)

	score := weights.WC*c + weights.WN*n + weights.WI*i + weights.WV*v

	return CandidateScore{
		CandidateID: candidate.ID,
		Score:       score,
		Components:  ScoreComponents{C: c, N: n, I: i, V: v},
	}, nil
}

func computeCoverageGain(ctx context.Context, pool *pgxpool.Pool, cand scenario.Proposal) float64 {
	// C(x): coverage gain based on which AML rules the candidate targets
	// Use expected_evasion_vector to map to AML rule IDs
	ruleIDs := evictionVectorToRuleIDs(cand.ExpectedEvasionVector)
	if len(ruleIDs) == 0 {
		return 0.5 // neutral if no specific rules mapped
	}

	sum := 0.0
	for _, rid := range ruleIDs {
		var weight float64
		err := pool.QueryRow(ctx, `SELECT coverage_weight FROM rule_coverage WHERE rule_id = $1`, string(rid)).Scan(&weight)
		if err != nil {
			// uncovered rule = max weight (priority candidate)
			sum += 1.0
		} else {
			sum += weight
		}
	}
	avg := sum / float64(len(ruleIDs))
	return math.Min(1.0, math.Max(0.0, avg))
}

// evictionVectorToRuleIDs maps a free-text evasion vector to AML rule IDs.
// A full implementation would use NLP/LLM classification; for now we use
// keyword matching based on known rule patterns.
func evictionVectorToRuleIDs(ev string) []aml.RuleID {
	var ids []aml.RuleID
	lower := strings.ToLower(ev)
	// RuleStructuring: structuring, smurfing, layering, split
	if strings.Contains(lower, "structur") || strings.Contains(lower, "smurf") || strings.Contains(lower, "layer") || strings.Contains(lower, "split") {
		ids = append(ids, aml.RuleStructuring)
	}
	// RuleRapidMovement: rapid, quick, fast, urgent
	if strings.Contains(lower, "rapid") || strings.Contains(lower, "quick") || strings.Contains(lower, "fast") {
		ids = append(ids, aml.RuleRapidMovement)
	}
	// RuleLayering: layering, shell company, proxy, round-tripping
	if strings.Contains(lower, "layer") || strings.Contains(lower, "shell") || strings.Contains(lower, "proxy") || strings.Contains(lower, "round.trip") {
		ids = append(ids, aml.RuleLayering)
	}
	return uniqueRuleIDs(ids)
}

func computeNovelty(cand scenario.Proposal, prior []scenario.Proposal) float64 {
	if len(prior) == 0 {
		return 1.0
	}
	minDist := 1.0
	for _, p := range prior {
		d := genomeDistance(cand, p)
		if d < minDist {
			minDist = d
		}
	}
	return minDist
}

func computeInfoGain(ctx context.Context, pool *pgxpool.Pool, cand scenario.Proposal) float64 {
	// Heuristic: higher for candidates targeting rules with high uncertainty
	// Label as heuristic per spec.
	return 0.5 // placeholder heuristic
}

func computeValidity(cand scenario.Proposal) float64 {
	return 1.0
}

func extractRuleIDsFromSpec(spec map[string]any) []aml.RuleID {
	var ids []aml.RuleID
	if mt, ok := spec["mutation_type"].(string); ok {
		switch mt {
		case "split_transaction":
			ids = append(ids, aml.RuleStructuring)
		case "delay_and_resubmit":
			ids = append(ids, aml.RuleRapidMovement)
		case "introduce_shell_entity":
			ids = append(ids, aml.RuleLayering)
		}
	}
	if tc, ok := spec["threshold_cents"].(float64); ok && tc <= 1000000 {
		ids = append(ids, aml.RuleStructuring)
	}
	return uniqueRuleIDs(ids)
}

func uniqueRuleIDs(ids []aml.RuleID) []aml.RuleID {
	seen := make(map[aml.RuleID]bool)
	var out []aml.RuleID
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

func genomeDistance(a, b scenario.Proposal) float64 {
	// Distance based on scenario_type and evasion vector similarity
	if a.ScenarioType != b.ScenarioType {
		return 1.0 // different types = maximally different
	}
	// Same type: compare evasion vectors for partial differentiation
	dist := 0.0
	if a.ExpectedEvasionVector != b.ExpectedEvasionVector {
		dist = 0.5 // different evasion vectors = partially different
	}
	// If same scenario type AND same evasion vector, they're very similar
	return math.Min(1.0, dist)
}

func getPriorCandidates(ctx context.Context, pool *pgxpool.Pool, limit int) ([]scenario.Proposal, error) {
	rows, err := pool.Query(ctx, `
		SELECT id, cohort_id, status, scenario_type, description,
		       expected_evasion_vector, suggested_tx_pattern,
		       gemini_prompt, gemini_response, gemini_model, correlation_id, approved_by, created_at, updated_at
		FROM scenario_proposal
		WHERE status IN ('approved','rejected')
		ORDER BY created_at DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var candidates []scenario.Proposal
	for rows.Next() {
		var p scenario.Proposal
		var patternJSON []byte
		var approvedBy *string
		err := rows.Scan(
			&p.ID, &p.CohortID, &p.Status, &p.ScenarioType, &p.Description,
			&p.ExpectedEvasionVector, &patternJSON,
			&p.GeminiPrompt, &p.GeminiResponse, &p.GeminiModel,
			&p.CorrelationID, &approvedBy, &p.CreatedAt, &p.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		if approvedBy != nil {
			p.ApprovedBy = *approvedBy
		}
		if len(patternJSON) > 0 {
			_ = json.Unmarshal(patternJSON, &p.SuggestedTxPattern)
		}
		candidates = append(candidates, p)
	}
	return candidates, rows.Err()
}
