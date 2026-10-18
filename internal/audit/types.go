// Package audit defines types for audit event logging (DECISION-006).
// Records approval/rejection events for scenario proposals and experiment execution.
package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// EventType identifies the kind of audit event.
type EventType string

const (
	EventScenarioApproved    EventType = "scenario_approved"
	EventScenarioRejected    EventType = "scenario_rejected"
	EventExperimentStarted   EventType = "experiment_started"
	EventExperimentCompleted EventType = "experiment_completed"
	EventExperimentFailed    EventType = "experiment_failed"
	EventCohortCreated       EventType = "cohort_created"
	EventCohortLocked        EventType = "cohort_locked"
	EventLoginSucceeded      EventType = "auth_login_succeeded"
	EventLoginFailed         EventType = "auth_login_failed"
	EventUserCreated         EventType = "user_created"
	EventUserUpdated         EventType = "user_updated"
	EventUserDeleted         EventType = "user_deleted"
)

// Event is an immutable audit log entry.
type Event struct {
	ID         uuid.UUID      `json:"id"`
	OrgID      uuid.UUID      `json:"org_id"`
	EventType  EventType      `json:"event_type"`
	ActorUID   string         `json:"actor_uid"`
	TargetType string         `json:"target_type"` // scenario_proposal | experiment | cohort
	TargetID   uuid.UUID      `json:"target_id"`
	Details    map[string]any `json:"details"` // flexible event-specific payload
	IPAddress  string         `json:"ip_address,omitempty"`
	UserAgent  string         `json:"user_agent,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
}

// Repository handles audit_event persistence.
type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// DefaultOrgID returns the id of the single 'default' organisation. It is used
// when a caller has no org context (e.g. local dev / single-tenant deployment);
// real multi-tenant callers pass an explicit org_id.
func (r *Repository) DefaultOrgID(ctx context.Context) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.pool.QueryRow(ctx,
		`SELECT id FROM organisations WHERE name = 'default' ORDER BY created_at LIMIT 1`).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("audit: resolve default org: %w", err)
	}
	return id, nil
}

// Record inserts a new audit event.
func (r *Repository) Record(ctx context.Context, e Event) error {
	detailsJSON, err := json.Marshal(e.Details)
	if err != nil {
		return fmt.Errorf("audit: marshal details: %w", err)
	}

	// org_id falls back to the 'default' organisation when the caller has no org
	// context (uuid.Nil). Real multi-tenant deployments always pass a real org.
	const q = `
		INSERT INTO audit_event (id, org_id, event_type, actor_uid, target_type, target_id, details, ip_address, user_agent, created_at)
		VALUES ($1,
		        COALESCE(NULLIF($2::uuid, '00000000-0000-0000-0000-000000000000'::uuid),
		                 (SELECT id FROM organisations WHERE name = 'default' ORDER BY created_at LIMIT 1)),
		        $3, $4, $5, $6, $7, $8, $9, $10)`

	_, err = r.pool.Exec(ctx, q,
		e.ID, e.OrgID, string(e.EventType), e.ActorUID,
		e.TargetType, e.TargetID, detailsJSON,
		e.IPAddress, e.UserAgent, e.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("audit: insert event: %w", err)
	}
	return nil
}

// List returns events for an org, filtered by type/target, newest first.
func (r *Repository) List(ctx context.Context, orgID uuid.UUID, eventType *EventType, targetType, targetID string, limit, offset int) ([]Event, error) {
	q := `SELECT id, org_id, event_type, actor_uid, target_type, target_id, details, ip_address, user_agent, created_at FROM audit_event WHERE org_id = $1`
	args := []any{orgID}
	argIdx := 2

	if eventType != nil {
		q += fmt.Sprintf(" AND event_type = $%d", argIdx)
		args = append(args, string(*eventType))
		argIdx++
	}
	if targetType != "" {
		q += fmt.Sprintf(" AND target_type = $%d", argIdx)
		args = append(args, targetType)
		argIdx++
	}
	if targetID != "" {
		q += fmt.Sprintf(" AND target_id = $%d", argIdx)
		args = append(args, targetID)
		argIdx++
	}

	q += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d OFFSET $%d", argIdx, argIdx+1)
	args = append(args, limit, offset)

	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("audit: query events: %w", err)
	}
	defer rows.Close()

	var out []Event
	for rows.Next() {
		var e Event
		var detailsJSON []byte
		var createdAt time.Time
		var eventTypeStr string
		if err := rows.Scan(
			&e.ID, &e.OrgID, &eventTypeStr, &e.ActorUID,
			&e.TargetType, &e.TargetID, &e.Details, &e.IPAddress, &e.UserAgent, &createdAt,
		); err != nil {
			return nil, fmt.Errorf("audit: scan row: %w", err)
		}
		e.EventType = EventType(eventTypeStr)
		e.CreatedAt = createdAt
		if len(detailsJSON) > 0 {
			_ = json.Unmarshal(detailsJSON, &e.Details)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
