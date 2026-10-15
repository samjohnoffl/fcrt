// Package bank defines the synthetic banking domain types and all constants
// needed by the generator, repository, and tests.
//
// Monetary arithmetic rule: all amounts that cross package boundaries use
// integer cents (int64) to avoid floating-point rounding errors. The
// Currency field is always an ISO 4217 three-letter code. Conversion to
// decimal for display is the caller's responsibility.
package bank

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// GeneratorVersion is the semantic version of the bank generator.
// Bump this whenever the generation algorithm changes in a way that
// would produce different output for the same seed + predicate.
const GeneratorVersion = "v0.1.0"

// ─── Enumerated types ────────────────────────────────────────────────────────

// EntityType classifies a synthetic entity.
type EntityType string

const (
	EntityTypeIndividual EntityType = "individual"
	EntityTypeCompany    EntityType = "company"
	EntityTypeShell      EntityType = "shell"
)

// allEntityTypes lists all valid EntityType values for validation.
var allEntityTypes = []EntityType{
	EntityTypeIndividual,
	EntityTypeCompany,
	EntityTypeShell,
}

// AccountType classifies a bank account.
type AccountType string

const (
	AccountTypeChecking AccountType = "checking"
	AccountTypeSavings  AccountType = "savings"
	AccountTypeCrypto   AccountType = "crypto"
)

// TxType classifies a transaction.
type TxType string

const (
	TxTypeWire   TxType = "wire"
	TxTypeACH    TxType = "ach"
	TxTypeCrypto TxType = "crypto"
	TxTypeCash   TxType = "cash"
)

// ─── Currency constants ───────────────────────────────────────────────────────

// Supported ISO 4217 currency codes.
const (
	CurrencyUSD = "USD"
	CurrencyEUR = "EUR"
	CurrencyGBP = "GBP"
	CurrencyBTC = "BTC" // crypto: satoshis as cents unit
)

// ─── Core domain types ───────────────────────────────────────────────────────

// Predicate is the JSON filter DSL applied during cohort generation.
// All fields are optional; a nil field matches any value.
type Predicate struct {
	// EntityType restricts generated entities to a single type.
	EntityType *EntityType `json:"entity_type,omitempty"`
	// CountryCode restricts to a single ISO 3166-1 alpha-2 country.
	CountryCode *string `json:"country_code,omitempty"`
	// MinEntities is the inclusive minimum number of entities to generate.
	// Defaults to DefaultMinEntities when nil.
	MinEntities *int `json:"min_entities,omitempty"`
	// MaxEntities is the inclusive maximum number of entities to generate.
	// Defaults to DefaultMaxEntities when nil.
	MaxEntities *int `json:"max_entities,omitempty"`
}

// Default generation parameters used when the predicate fields are nil.
const (
	DefaultMinEntities = 10
	DefaultMaxEntities = 30
)

// CohortManifest is a point-in-time summary of the generated cohort.
// It is stored as JSONB in cohort.manifest and must be re-derivable
// from the same seed + predicate + generator_version.
type CohortManifest struct {
	CohortID         uuid.UUID `json:"cohort_id"`
	GeneratorVersion string    `json:"generator_version"`
	Seed             int64     `json:"seed"`
	WindowStart      time.Time `json:"window_start"`
	WindowEnd        time.Time `json:"window_end"`
	EntityCount      int       `json:"entity_count"`
	AccountCount     int       `json:"account_count"`
	TxCount          int       `json:"tx_count"`
}

// Cohort is a versioned, lockable set of synthetic banking data.
type Cohort struct {
	ID               uuid.UUID      `json:"id"`
	Name             string         `json:"name"`
	Predicate        Predicate      `json:"predicate"`
	Seed             int64          `json:"seed"`
	GeneratorVersion string         `json:"generator_version"`
	Manifest         CohortManifest `json:"manifest"`
	WindowStart      *time.Time     `json:"window_start,omitempty"`
	WindowEnd        *time.Time     `json:"window_end,omitempty"`
	LockedAt         *time.Time     `json:"locked_at,omitempty"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
}

// Entity is a synthetic customer or legal entity.
// No real PII is stored; names and attributes are procedurally generated.
type Entity struct {
	ID          uuid.UUID      `json:"id"`
	CohortID    uuid.UUID      `json:"cohort_id"`
	EntityType  EntityType     `json:"entity_type"`
	Name        string         `json:"name"`
	CountryCode string         `json:"country_code"` // ISO 3166-1 alpha-2
	RiskScore   float64        `json:"risk_score"`   // 0.0–1.0 inclusive
	Attributes  map[string]any `json:"attributes"`   // stored as JSONB; any requires comment per AGENTS.md
	CreatedAt   time.Time      `json:"created_at"`
}

// Account is a bank account belonging to an Entity.
// BalanceCents is the integer ledger balance in the smallest currency unit
// (cents for USD/EUR/GBP, satoshis for BTC). Must be >= 0.
type Account struct {
	ID           uuid.UUID      `json:"id"`
	CohortID     uuid.UUID      `json:"cohort_id"`
	EntityID     uuid.UUID      `json:"entity_id"`
	AccountType  AccountType    `json:"account_type"`
	Currency     string         `json:"currency"`     // ISO 4217
	BalanceCents int64          `json:"balance_cents"` // integer, not float
	OpenedAt     time.Time      `json:"opened_at"`
	Attributes   map[string]any `json:"attributes"` // stored as JSONB; any requires comment per AGENTS.md
	CreatedAt    time.Time      `json:"created_at"`
}

// Transaction is a single monetary movement between two accounts.
//
// Monetary rules:
//   - AmountCents is in the smallest unit of Currency (> 0 always).
//   - Currency must match the FromAccount's currency.
//   - ToAccount may hold a different currency (FX not modelled; amounts
//     are recorded in the source currency only).
//   - TxTimestamp must fall within the cohort's [WindowStart, WindowEnd).
//   - FromAccountID != ToAccountID.
type Transaction struct {
	ID            uuid.UUID      `json:"id"`
	CohortID      uuid.UUID      `json:"cohort_id"`
	FromAccountID uuid.UUID      `json:"from_account_id"`
	ToAccountID   uuid.UUID      `json:"to_account_id"`
	AmountCents   int64          `json:"amount_cents"` // integer, not float
	Currency      string         `json:"currency"`     // ISO 4217
	TxType        TxType         `json:"tx_type"`
	TxTimestamp   time.Time      `json:"tx_timestamp"`
	Reference     string         `json:"reference,omitempty"`
	Attributes    map[string]any `json:"attributes"` // stored as JSONB; any requires comment per AGENTS.md
	CreatedAt     time.Time      `json:"created_at"`
}

// BatchSelection is the output of a cohort batch-selection query.
// It pins the time boundaries and reproduces the manifest for auditability.
type BatchSelection struct {
	CohortID    uuid.UUID      `json:"cohort_id"`
	WindowStart time.Time      `json:"window_start"`
	WindowEnd   time.Time      `json:"window_end"`
	Manifest    CohortManifest `json:"manifest"`
	Entities    []Entity       `json:"entities"`
	Accounts    []Account      `json:"accounts"`
	// Transactions is the ordered subset within [WindowStart, WindowEnd).
	Transactions []Transaction `json:"transactions"`
}

// ─── Sentinel errors ─────────────────────────────────────────────────────────

// ErrCohortLocked is returned when a write is attempted on a locked cohort.
var ErrCohortLocked = errors.New("cohort is locked and cannot be modified")

// ErrInvalidPredicate is returned when the predicate contains contradictory
// or unsupported field values.
var ErrInvalidPredicate = errors.New("invalid cohort predicate")

// ErrInsufficientBalance is returned when a generated transaction would
// make an account balance go negative.
var ErrInsufficientBalance = errors.New("insufficient account balance")

// ErrInvalidTransaction is returned when a transaction violates a ledger invariant.
var ErrInvalidTransaction = errors.New("invalid transaction: ledger invariant violated")
