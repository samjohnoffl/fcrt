// Package scenario — repository.go
//
// Repository persists and retrieves scenario_proposal rows.
// All writes are parameterised (T-03). Raw Gemini prompt/response stored
// verbatim for auditability (AC-03.1).
package scenario

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrProposalNotFound is returned when a proposal ID does not exist.
var ErrProposalNotFound = errors.New("scenario proposal not found")

// ErrInvalidStatusTransition is returned when the requested status change is illegal.
var ErrInvalidStatusTransition = errors.New("invalid scenario status transition")

// Repository handles scenario_proposal persistence.
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository creates a Repository backed by the given pool.
func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// Create inserts a new proposal and returns the DB-assigned row.
// geminiPrompt and geminiResponse are stored raw for auditability (AC-03.1).
func (r *Repository) Create(ctx context.Context, p Proposal) (Proposal, error) {
	patternJSON, err := json.Marshal(p.SuggestedTxPattern)
	if err != nil {
		return Proposal{}, fmt.Errorf("scenario: marshal tx pattern: %w", err)
	}

	const q = `
		INSERT INTO scenario_proposal
		  (id, cohort_id, status, scenario_type, description,
		   expected_evasion_vector, suggested_tx_pattern,
		   gemini_prompt, gemini_response, gemini_model, correlation_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		RETURNING id, cohort_id, status, scenario_type, description,
		          expected_evasion_vector, suggested_tx_pattern,
		          gemini_prompt, gemini_response, gemini_model,
		          correlation_id, approved_by, created_at, updated_at`

	row := r.pool.QueryRow(ctx, q,
		p.ID, p.CohortID, string(StatusPending),
		p.ScenarioType, p.Description, p.ExpectedEvasionVector,
		patternJSON, p.GeminiPrompt, p.GeminiResponse, p.GeminiModel,
		p.CorrelationID,
	)
	return scanProposal(row)
}

// Get retrieves a proposal by ID.
func (r *Repository) Get(ctx context.Context, id uuid.UUID) (Proposal, error) {
	const q = `
		SELECT id, cohort_id, status, scenario_type, description,
		       expected_evasion_vector, suggested_tx_pattern,
		       gemini_prompt, gemini_response, gemini_model,
		       correlation_id, approved_by, created_at, updated_at
		FROM scenario_proposal WHERE id = $1`
	row := r.pool.QueryRow(ctx, q, id)
	return scanProposal(row)
}

// List returns all proposals for a cohort, newest first.
func (r *Repository) List(ctx context.Context, cohortID *uuid.UUID, status *Status) ([]Proposal, error) {
	q := `
		SELECT id, cohort_id, status, scenario_type, description,
		       expected_evasion_vector, suggested_tx_pattern,
		       gemini_prompt, gemini_response, gemini_model,
		       correlation_id, approved_by, created_at, updated_at
		FROM scenario_proposal`
	args := []any{}
	idx := 1
	if cohortID != nil {
		q += fmt.Sprintf(" WHERE cohort_id = $%d", idx)
		args = append(args, *cohortID)
		idx++
		if status != nil {
			q += fmt.Sprintf(" AND status = $%d", idx)
			args = append(args, string(*status))
		}
	} else if status != nil {
		q += fmt.Sprintf(" WHERE status = $%d", idx)
		args = append(args, string(*status))
	}
	q += " ORDER BY created_at DESC"

	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("scenario: list proposals: %w", err)
	}
	defer rows.Close()

	var out []Proposal
	for rows.Next() {
		p, err := scanProposalRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// SetStatus updates the status of a proposal (pending→approved or pending→rejected).
// approvedBy is the Firebase UID of the user taking the action (T-10, AC-03.2).
func (r *Repository) SetStatus(ctx context.Context, id uuid.UUID, newStatus Status, byUID string) (Proposal, error) {
	p, err := r.Get(ctx, id)
	if err != nil {
		return Proposal{}, err
	}

	// Enforce status FSM: only pending proposals can be approved or rejected.
	if p.Status != StatusPending {
		return Proposal{}, fmt.Errorf("%w: cannot transition from %s", ErrInvalidStatusTransition, p.Status)
	}
	if newStatus != StatusApproved && newStatus != StatusRejected {
		return Proposal{}, fmt.Errorf("%w: target status %s not valid", ErrInvalidStatusTransition, newStatus)
	}

	const q = `
		UPDATE scenario_proposal SET status = $1, approved_by = $2
		WHERE id = $3
		RETURNING id, cohort_id, status, scenario_type, description,
		          expected_evasion_vector, suggested_tx_pattern,
		          gemini_prompt, gemini_response, gemini_model,
		          correlation_id, approved_by, created_at, updated_at`
	row := r.pool.QueryRow(ctx, q, string(newStatus), byUID, id)
	return scanProposal(row)
}

// IsApproved returns true if the proposal exists and has status=approved.
// Used by the experiment handler to enforce AC-03.2 (approval gate).
func (r *Repository) IsApproved(ctx context.Context, id uuid.UUID) (bool, error) {
	p, err := r.Get(ctx, id)
	if err != nil {
		return false, err
	}
	return p.Status == StatusApproved, nil
}

// ─── Scanning helpers ────────────────────────────────────────────────────────

func scanProposal(row pgx.Row) (Proposal, error) {
	var p Proposal
	var patternJSON []byte
	var approvedBy *string
	err := row.Scan(
		&p.ID, &p.CohortID, &p.Status,
		&p.ScenarioType, &p.Description, &p.ExpectedEvasionVector,
		&patternJSON, &p.GeminiPrompt, &p.GeminiResponse, &p.GeminiModel,
		&p.CorrelationID, &approvedBy, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Proposal{}, ErrProposalNotFound
		}
		return Proposal{}, fmt.Errorf("scan proposal: %w", err)
	}
	if approvedBy != nil {
		p.ApprovedBy = *approvedBy
	}
	if len(patternJSON) > 0 {
		if err := json.Unmarshal(patternJSON, &p.SuggestedTxPattern); err != nil {
			return Proposal{}, fmt.Errorf("unmarshal tx pattern: %w", err)
		}
	}
	return p, nil
}

func scanProposalRow(rows pgx.Rows) (Proposal, error) {
	var p Proposal
	var patternJSON []byte
	var approvedBy *string
	err := rows.Scan(
		&p.ID, &p.CohortID, &p.Status,
		&p.ScenarioType, &p.Description, &p.ExpectedEvasionVector,
		&patternJSON, &p.GeminiPrompt, &p.GeminiResponse, &p.GeminiModel,
		&p.CorrelationID, &approvedBy, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return Proposal{}, fmt.Errorf("scan proposal row: %w", err)
	}
	if approvedBy != nil {
		p.ApprovedBy = *approvedBy
	}
	if len(patternJSON) > 0 {
		if err := json.Unmarshal(patternJSON, &p.SuggestedTxPattern); err != nil {
			return Proposal{}, fmt.Errorf("unmarshal tx pattern row: %w", err)
		}
	}
	return p, nil
}
