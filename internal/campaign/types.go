// Package campaign defines persisted red-team campaigns.
//
// A campaign is the unit of goal and budget: it names a cohort, fixes a control
// configuration and caps how many experiments may be run. The budget is
// enforced in the experiment handler at the point an experiment is linked, so a
// campaign cannot be silently overrun.
package campaign

import (
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/yourorg/financial-crime-red-team/internal/aml"
)

// Status is the campaign lifecycle state.
type Status string

const (
	StatusActive    Status = "active"
	StatusCompleted Status = "completed"
	StatusPaused    Status = "paused"
)

// Valid reports whether s is a known status.
func (s Status) Valid() bool {
	switch s {
	case StatusActive, StatusCompleted, StatusPaused:
		return true
	default:
		return false
	}
}

// Campaign is a persisted campaign record.
type Campaign struct {
	ID              uuid.UUID          `json:"id"`
	Name            string             `json:"name"`
	Description     string             `json:"description"`
	Goal            string             `json:"goal"`
	CohortID        *uuid.UUID         `json:"cohort_id,omitempty"`
	Budget          int                `json:"budget"`
	ExperimentsUsed int                `json:"experiments_used"`
	Status          Status             `json:"status"`
	ControlConfig   *aml.ControlConfig `json:"control_config,omitempty"`
	CreatedAt       time.Time          `json:"created_at"`
	UpdatedAt       time.Time          `json:"updated_at"`
}

// Sentinel errors.
var (
	ErrNotFound        = errors.New("campaign not found")
	ErrBudgetExhausted = errors.New("campaign budget exhausted")
	ErrNotActive       = errors.New("campaign is not active")
)
