// Package job implements the database-backed work queue (DECISION-004 option A).
//
// Jobs are rows in the `job` table. The API enqueues them; workers claim them
// atomically with SELECT ... FOR UPDATE SKIP LOCKED so multiple worker replicas
// can run concurrently without duplicate claims.
package job

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Type identifies a kind of work the worker can dispatch.
type Type string

const (
	TypeGenerateCohort    Type = "generate_cohort"
	TypeRunExperiment     Type = "run_experiment"
	TypeRecomputeCoverage Type = "recompute_coverage"
)

// GenerateCohortPayload is the payload for TypeGenerateCohort. It matches the
// struct the worker decodes in handleGenerateCohort.
type GenerateCohortPayload struct {
	CohortID   uuid.UUID `json:"cohort_id"`
	Seed       int64     `json:"seed"`
	WindowDays int       `json:"window_days"`
}

// Repository enqueues jobs into the queue.
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository creates a job Repository.
func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// Enqueue inserts a pending job with the given JSON payload and returns its id.
// org_id is left to the column default (see migration 000006).
func (r *Repository) Enqueue(ctx context.Context, jobType Type, payload any) (uuid.UUID, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return uuid.Nil, fmt.Errorf("job: marshal payload: %w", err)
	}

	var id uuid.UUID
	err = r.pool.QueryRow(ctx, `
		INSERT INTO job (job_type, payload)
		VALUES ($1, $2)
		RETURNING id`, string(jobType), b).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("job: enqueue %s: %w", jobType, err)
	}
	return id, nil
}
