// Package discovery — repository.go
//
// Persistence for adaptive selection runs. Every run stores the weights, the
// seed, the pool size, the full scoring trace and the rejection reasons, so a
// decision can be audited after the fact.
package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yourorg/financial-crime-red-team/internal/genome"
	"github.com/yourorg/financial-crime-red-team/internal/scenario"
)

// Repository handles discovery_run / discovery_candidate persistence.
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository creates a Repository backed by the given pool.
func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// LoadState rebuilds the evidence base for a cohort from stored experiments.
func (r *Repository) LoadState(ctx context.Context, cohortID uuid.UUID) (State, error) {
	st := NewState()

	rows, err := r.pool.Query(ctx, `
		SELECT ep.mutation_type,
		       COUNT(DISTINCT ep.id) AS total,
		       COUNT(DISTINCT ep.id) FILTER (WHERE eo.outcome_unchanged) AS unchanged
		FROM experiment_pair ep
		LEFT JOIN experiment_outcome eo ON eo.pair_id = ep.id
		WHERE ep.cohort_id = $1 AND ep.status = 'completed'
		GROUP BY ep.mutation_type`, cohortID)
	if err != nil {
		return st, fmt.Errorf("discovery: query outcome history: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var mutationType string
		var total, unchanged int
		if err := rows.Scan(&mutationType, &total, &unchanged); err != nil {
			return st, fmt.Errorf("discovery: scan outcome history: %w", err)
		}
		st.MutationCounts[mutationType] = total
		st.Outcomes[mutationType] = OutcomeStat{
			Total:     total,
			Unchanged: unchanged,
			Changed:   total - unchanged,
		}
		st.TotalExperiments += total
	}
	if err := rows.Err(); err != nil {
		return st, fmt.Errorf("discovery: outcome history rows: %w", err)
	}

	priorRows, err := r.pool.Query(ctx, `
		SELECT sp.scenario_type, sp.expected_evasion_vector, sp.description, sp.suggested_tx_pattern
		FROM experiment_pair ep
		JOIN scenario_proposal sp ON sp.id = ep.scenario_proposal_id
		WHERE ep.cohort_id = $1 AND ep.status = 'completed'`, cohortID)
	if err != nil {
		return st, fmt.Errorf("discovery: query prior genomes: %w", err)
	}
	defer priorRows.Close()
	for priorRows.Next() {
		p, err := scanProposal(priorRows)
		if err != nil {
			return st, err
		}
		st.PriorGenomes = append(st.PriorGenomes, genome.Encode(p))
	}
	if err := priorRows.Err(); err != nil {
		return st, fmt.Errorf("discovery: prior genome rows: %w", err)
	}

	return st, nil
}

// PendingCandidates returns proposals that are eligible for selection: not
// rejected, and not already attached to an experiment.
func (r *Repository) PendingCandidates(ctx context.Context, cohortID uuid.UUID) ([]Candidate, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT sp.id, sp.scenario_type, sp.expected_evasion_vector, sp.description,
		       sp.suggested_tx_pattern, sp.created_at
		FROM scenario_proposal sp
		WHERE sp.cohort_id = $1
		  AND sp.status <> 'rejected'
		  AND NOT EXISTS (
		      SELECT 1 FROM experiment_pair ep WHERE ep.scenario_proposal_id = sp.id
		  )
		ORDER BY sp.created_at ASC, sp.id ASC`, cohortID)
	if err != nil {
		return nil, fmt.Errorf("discovery: query pending candidates: %w", err)
	}
	defer rows.Close()

	out := make([]Candidate, 0)
	for rows.Next() {
		var (
			id          uuid.UUID
			scenarioTyp string
			evasion     string
			description string
			rawPattern  []byte
			createdAt   time.Time
		)
		if err := rows.Scan(&id, &scenarioTyp, &evasion, &description, &rawPattern, &createdAt); err != nil {
			return nil, fmt.Errorf("discovery: scan pending candidate: %w", err)
		}
		p := scenario.Proposal{
			ID:                    id,
			ScenarioType:          scenarioTyp,
			ExpectedEvasionVector: evasion,
			Description:           description,
			SuggestedTxPattern:    decodePattern(rawPattern),
		}
		out = append(out, Candidate{
			ProposalID:   id,
			MutationType: scenarioTyp,
			ScenarioType: scenarioTyp,
			Genome:       genome.Encode(p),
			CreatedAt:    createdAt,
		})
	}
	return out, rows.Err()
}

// Save persists a run and its candidates atomically.
func (r *Repository) Save(ctx context.Context, run Run) (Run, error) {
	wJSON, err := json.Marshal(run.Weights)
	if err != nil {
		return Run{}, fmt.Errorf("discovery: marshal weights: %w", err)
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Run{}, fmt.Errorf("discovery: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := tx.QueryRow(ctx, `
		INSERT INTO discovery_run
		    (id, cohort_id, strategy, weights_version, weights, seed, budget,
		     pool_size, valid_pool_size, notes, duration_ms)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		RETURNING created_at`,
		run.ID, run.CohortID, string(run.Strategy), run.Weights.Version, wJSON,
		run.Seed, run.Budget, run.PoolSize, run.ValidPoolSize, run.Notes, run.DurationMS,
	).Scan(&run.CreatedAt); err != nil {
		return Run{}, fmt.Errorf("discovery: insert run: %w", err)
	}

	for _, c := range run.Candidates {
		gJSON, err := json.Marshal(c.Genome)
		if err != nil {
			return Run{}, fmt.Errorf("discovery: marshal genome: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO discovery_candidate
			    (run_id, scenario_proposal_id, mutation_type, genome, genome_valid,
			     score_c, score_n, score_i, score_v, total_score, rank, selected, rationale)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
			run.ID, c.ProposalID, c.MutationType, gJSON, c.GenomeValid,
			c.Components.Coverage, c.Components.Novelty, c.Components.Information,
			c.Components.Validity, c.Score, c.Rank, c.Selected, c.Rationale,
		); err != nil {
			return Run{}, fmt.Errorf("discovery: insert candidate: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return Run{}, fmt.Errorf("discovery: commit: %w", err)
	}
	return run, nil
}

// LatestRuns returns the most recent run per strategy for a cohort.
func (r *Repository) LatestRuns(ctx context.Context, cohortID uuid.UUID) (map[Strategy]Run, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT DISTINCT ON (strategy)
		       id, cohort_id, strategy, weights, seed, budget,
		       pool_size, valid_pool_size, notes, duration_ms, created_at
		FROM discovery_run
		WHERE cohort_id = $1
		ORDER BY strategy, created_at DESC`, cohortID)
	if err != nil {
		return nil, fmt.Errorf("discovery: query runs: %w", err)
	}
	defer rows.Close()

	runs := make(map[Strategy]Run)
	for rows.Next() {
		var (
			run   Run
			strat string
			wJSON []byte
		)
		if err := rows.Scan(&run.ID, &run.CohortID, &strat, &wJSON, &run.Seed,
			&run.Budget, &run.PoolSize, &run.ValidPoolSize, &run.Notes,
			&run.DurationMS, &run.CreatedAt); err != nil {
			return nil, fmt.Errorf("discovery: scan run: %w", err)
		}
		run.Strategy = Strategy(strat)
		if err := json.Unmarshal(wJSON, &run.Weights); err != nil {
			return nil, fmt.Errorf("discovery: unmarshal weights: %w", err)
		}
		runs[run.Strategy] = run
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("discovery: run rows: %w", err)
	}

	for strat, run := range runs {
		cands, err := r.candidates(ctx, run.ID)
		if err != nil {
			return nil, err
		}
		run.Candidates = cands
		runs[strat] = run
	}
	return runs, nil
}

// ApproveCount returns how many selected candidates in a run have since been
// approved by a human.
func (r *Repository) ApproveCount(ctx context.Context, runID uuid.UUID) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM discovery_candidate dc
		JOIN scenario_proposal sp ON sp.id = dc.scenario_proposal_id
		WHERE dc.run_id = $1 AND dc.selected AND sp.status = 'approved'`, runID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("discovery: count approved: %w", err)
	}
	return n, nil
}

func (r *Repository) candidates(ctx context.Context, runID uuid.UUID) ([]Scored, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT scenario_proposal_id, mutation_type, genome, genome_valid,
		       score_c, score_n, score_i, score_v, total_score, rank, selected, rationale
		FROM discovery_candidate
		WHERE run_id = $1
		ORDER BY rank ASC`, runID)
	if err != nil {
		return nil, fmt.Errorf("discovery: query candidates: %w", err)
	}
	defer rows.Close()

	out := make([]Scored, 0)
	for rows.Next() {
		var (
			c     Scored
			gJSON []byte
		)
		if err := rows.Scan(&c.ProposalID, &c.MutationType, &gJSON, &c.GenomeValid,
			&c.Components.Coverage, &c.Components.Novelty, &c.Components.Information,
			&c.Components.Validity, &c.Score, &c.Rank, &c.Selected, &c.Rationale); err != nil {
			return nil, fmt.Errorf("discovery: scan candidate: %w", err)
		}
		if err := json.Unmarshal(gJSON, &c.Genome); err != nil {
			return nil, fmt.Errorf("discovery: unmarshal genome: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ─── row helpers ─────────────────────────────────────────────────────────────

func scanProposal(row pgx.Row) (scenario.Proposal, error) {
	var (
		p          scenario.Proposal
		rawPattern []byte
	)
	if err := row.Scan(&p.ScenarioType, &p.ExpectedEvasionVector, &p.Description, &rawPattern); err != nil {
		return scenario.Proposal{}, fmt.Errorf("discovery: scan proposal: %w", err)
	}
	p.SuggestedTxPattern = decodePattern(rawPattern)
	return p, nil
}

// decodePattern accepts either the array form written by the Gemini handler or
// the empty-object default from the migration.
func decodePattern(raw []byte) []scenario.TxPatternStep {
	if len(raw) == 0 {
		return nil
	}
	var steps []scenario.TxPatternStep
	if err := json.Unmarshal(raw, &steps); err == nil {
		return steps
	}
	var single scenario.TxPatternStep
	if err := json.Unmarshal(raw, &single); err == nil && len(single) > 0 {
		return []scenario.TxPatternStep{single}
	}
	return nil
}
