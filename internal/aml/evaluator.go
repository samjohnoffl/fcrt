// Package aml — evaluator.go
//
// Evaluator applies the AML rule set to a frozen BatchSelection, producing
// a deterministic HarnessResult. Given the same input and ControlConfig,
// Evaluate() always produces the same alerts in the same order.
//
// Determinism contract:
//   - All slices in the input are treated as immutable.
//   - Alert IDs are derived deterministically from rule+entity+account IDs
//     using SHA-256, not crypto/rand.
//   - Alert slices are sorted by (RuleID, AlertID) before return.
//   - The evaluator never reads from wall clock; it uses tx_timestamp only.
package aml

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/yourorg/financial-crime-red-team/internal/bank"
)

// Evaluator applies AML rules to a frozen bank context.
// The receiver is named "ev" throughout to avoid shadowing by local "e" variables.
type Evaluator struct {
	config *ControlConfig
}

// NewEvaluator creates an Evaluator for the given config.
// Returns an error if the config fails validation.
func NewEvaluator(cfg *ControlConfig) (*Evaluator, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("evaluator: invalid config: %w", err)
	}
	return &Evaluator{config: cfg}, nil
}

// Evaluate runs all enabled AML rules against the provided BatchSelection.
// The selection must be frozen (cohort locked); callers are responsible for
// ensuring this invariant before calling.
//
// Returns a HarnessResult whose Alerts are sorted by (RuleID, AlertID) for
// byte-identical output across repeated calls with identical inputs.
func (ev *Evaluator) Evaluate(sel *bank.BatchSelection) HarnessResult {
	var alerts []Alert

	if ev.config.IsRuleEnabled(RuleStructuring) {
		alerts = append(alerts, ev.runStructuring(sel)...)
	}
	if ev.config.IsRuleEnabled(RuleLayering) {
		alerts = append(alerts, ev.runLayering(sel)...)
	}
	if ev.config.IsRuleEnabled(RuleRapidMovement) {
		alerts = append(alerts, ev.runRapidMovement(sel)...)
	}

	// Sort alerts for deterministic output order.
	sort.Slice(alerts, func(i, j int) bool {
		ai, aj := alerts[i], alerts[j]
		if ai.RuleID != aj.RuleID {
			return string(ai.RuleID) < string(aj.RuleID)
		}
		return ai.ID.String() < aj.ID.String()
	})

	return HarnessResult{
		CohortID:    sel.CohortID,
		RuleVersion: ev.config.ConfigHash,
		Alerts:      alerts,
		RunAt:       time.Time{}, // zero: callers set this from their own clock
	}
}

// ─── Structuring Rule ─────────────────────────────────────────────────────────
//
// Fires when an entity sends ≥MinTransactions transactions each below
// ReportingThresholdCents to the same counterparty account within WindowHours.

func (ev *Evaluator) runStructuring(sel *bank.BatchSelection) []Alert {
	cfg := ev.config.Structuring

	// Build account → entity reverse map.
	acctToEntity := make(map[uuid.UUID]uuid.UUID, len(sel.Accounts))
	for _, a := range sel.Accounts {
		acctToEntity[a.ID] = a.EntityID
	}
	subjectType := entityTypeMap(sel)

	// Group transactions: (from-entity, to-account) → entries.
	type txKey struct {
		fromEntity uuid.UUID
		toAcct     uuid.UUID
	}
	type txEntry struct {
		id       uuid.UUID
		ts       time.Time
		amtCents int64
		currency string
	}
	grouped := make(map[txKey][]txEntry)

	for _, tx := range sel.Transactions {
		if tx.AmountCents >= cfg.ReportingThresholdCents {
			continue // above threshold — not structuring
		}
		fromEntity, ok := acctToEntity[tx.FromAccountID]
		if !ok {
			continue
		}
		k := txKey{fromEntity: fromEntity, toAcct: tx.ToAccountID}
		grouped[k] = append(grouped[k], txEntry{
			id:       tx.ID,
			ts:       tx.TxTimestamp,
			amtCents: tx.AmountCents,
			currency: tx.Currency,
		})
	}

	windowDur := time.Duration(cfg.WindowHours) * time.Hour
	configHash := ev.config.ConfigHash
	var alerts []Alert

	for k, entries := range grouped {
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].ts.Before(entries[j].ts)
		})

		for i := 0; i < len(entries); i++ {
			windowEnd := entries[i].ts.Add(windowDur)
			var inWindow []txEntry
			for j := i; j < len(entries); j++ {
				if !entries[j].ts.Before(windowEnd) {
					break
				}
				inWindow = append(inWindow, entries[j])
			}
			if len(inWindow) >= cfg.MinTransactions {
				txIDs := make([]uuid.UUID, len(inWindow))
				var totalCents int64
				var currency string
				for x, ent := range inWindow {
					txIDs[x] = ent.id
					totalCents += ent.amtCents
					if currency == "" {
						currency = ent.currency
					}
				}
				alerts = append(alerts, Alert{
					ID:           deterministicAlertID(RuleStructuring, k.fromEntity, k.toAcct, entries[i].ts),
					RuleID:       RuleStructuring,
					RuleVersion:  configHash,
					EntityID:     k.fromEntity,
					AccountIDs:   []uuid.UUID{k.toAcct},
					TxIDs:        txIDs,
					AmountCents:  totalCents,
					Currency:     currency,
					SubjectType:  subjectType[k.fromEntity],
					ActivityType: "structuring",
					Severity:     "medium",
					Description: fmt.Sprintf(
						"structuring: %d transactions below threshold to same counterparty within %dh",
						len(inWindow), cfg.WindowHours,
					),
				})
				break // one alert per (from-entity, to-account) pair
			}
		}
	}
	return alerts
}

// ─── Layering Rule ────────────────────────────────────────────────────────────
//
// Fires when funds trace through ≥MinHops distinct accounts within WindowHours.

func (ev *Evaluator) runLayering(sel *bank.BatchSelection) []Alert {
	cfg := ev.config.Layering
	configHash := ev.config.ConfigHash
	windowDur := time.Duration(cfg.WindowHours) * time.Hour

	acctToEntity := make(map[uuid.UUID]uuid.UUID, len(sel.Accounts))
	for _, a := range sel.Accounts {
		acctToEntity[a.ID] = a.EntityID
	}
	subjectType := entityTypeMap(sel)
	txByID := txLookup(sel)

	// Build adjacency list.
	type hop struct {
		to  uuid.UUID
		ts  time.Time
		tid uuid.UUID
	}
	adj := make(map[uuid.UUID][]hop)
	for _, tx := range sel.Transactions {
		adj[tx.FromAccountID] = append(adj[tx.FromAccountID], hop{
			to:  tx.ToAccountID,
			ts:  tx.TxTimestamp,
			tid: tx.ID,
		})
	}
	for k := range adj {
		sort.Slice(adj[k], func(i, j int) bool {
			return adj[k][i].ts.Before(adj[k][j].ts)
		})
	}

	var alerts []Alert
	alerted := make(map[uuid.UUID]bool)

	type frame struct {
		acct    uuid.UUID
		depth   int
		startTS time.Time
		visited map[uuid.UUID]bool
		path    []uuid.UUID
		txPath  []uuid.UUID
	}

	for _, startAcct := range sel.Accounts {
		entityID := acctToEntity[startAcct.ID]
		if alerted[entityID] {
			continue
		}

		stack := []frame{{
			acct:    startAcct.ID,
			depth:   0,
			visited: map[uuid.UUID]bool{startAcct.ID: true},
			path:    []uuid.UUID{startAcct.ID},
		}}

		found := false
		for len(stack) > 0 && !found {
			f := stack[len(stack)-1]
			stack = stack[:len(stack)-1]

			for _, h := range adj[f.acct] {
				if f.visited[h.to] {
					continue
				}
				chainStart := h.ts
				if f.depth > 0 {
					chainStart = f.startTS
					if h.ts.Before(f.startTS) || h.ts.After(f.startTS.Add(windowDur)) {
						continue
					}
				}

				newVisited := make(map[uuid.UUID]bool, len(f.visited)+1)
				for kk, v := range f.visited {
					newVisited[kk] = v
				}
				newVisited[h.to] = true

				newPath := append(append([]uuid.UUID{}, f.path...), h.to)
				newTxPath := append(append([]uuid.UUID{}, f.txPath...), h.tid)
				newDepth := f.depth + 1

				if newDepth >= cfg.MinHops {
					amount, currency := sumAmount(txByID, newTxPath)
					alerts = append(alerts, Alert{
						ID:           deterministicAlertID(RuleLayering, entityID, startAcct.ID, chainStart),
						RuleID:       RuleLayering,
						RuleVersion:  configHash,
						EntityID:     entityID,
						AccountIDs:   newPath,
						TxIDs:        newTxPath,
						AmountCents:  amount,
						Currency:     currency,
						SubjectType:  subjectType[entityID],
						ActivityType: "layering",
						Severity:     "high",
						Description: fmt.Sprintf(
							"layering: fund chain of %d hops detected within %dh",
							newDepth, cfg.WindowHours,
						),
					})
					alerted[entityID] = true
					found = true
					break
				}

				stack = append(stack, frame{
					acct:    h.to,
					depth:   newDepth,
					startTS: chainStart,
					visited: newVisited,
					path:    newPath,
					txPath:  newTxPath,
				})
			}
		}
	}
	return alerts
}

// ─── Rapid Movement Rule ──────────────────────────────────────────────────────
//
// Fires when an account receives ≥MinAmountCents and debits ≥DebitRatioPct
// of that amount within WindowMinutes.

func (ev *Evaluator) runRapidMovement(sel *bank.BatchSelection) []Alert {
	cfg := ev.config.RapidMovement
	configHash := ev.config.ConfigHash
	windowDur := time.Duration(cfg.WindowMinutes) * time.Minute

	acctToEntity := make(map[uuid.UUID]uuid.UUID, len(sel.Accounts))
	for _, a := range sel.Accounts {
		acctToEntity[a.ID] = a.EntityID
	}
	subjectType := entityTypeMap(sel)

	type mvt struct {
		id       uuid.UUID
		ts       time.Time
		amtCents int64
		currency string
	}
	credits := make(map[uuid.UUID][]mvt)
	debits := make(map[uuid.UUID][]mvt)

	for _, tx := range sel.Transactions {
		if tx.AmountCents >= cfg.MinAmountCents {
			credits[tx.ToAccountID] = append(credits[tx.ToAccountID], mvt{
				id: tx.ID, ts: tx.TxTimestamp, amtCents: tx.AmountCents, currency: tx.Currency,
			})
		}
		debits[tx.FromAccountID] = append(debits[tx.FromAccountID], mvt{
			id: tx.ID, ts: tx.TxTimestamp, amtCents: tx.AmountCents, currency: tx.Currency,
		})
	}
	for k := range credits {
		sort.Slice(credits[k], func(i, j int) bool { return credits[k][i].ts.Before(credits[k][j].ts) })
	}
	for k := range debits {
		sort.Slice(debits[k], func(i, j int) bool { return debits[k][i].ts.Before(debits[k][j].ts) })
	}

	alerted := make(map[uuid.UUID]bool)
	var alerts []Alert

	for acctID, inflows := range credits {
		entityID, ok := acctToEntity[acctID]
		if !ok || alerted[entityID] {
			continue
		}
		outflows := debits[acctID]

		for _, credit := range inflows {
			windowEnd := credit.ts.Add(windowDur)
			var totalDebited int64
			var debitTxIDs []uuid.UUID
			for _, debit := range outflows {
				if debit.ts.Before(credit.ts) {
					continue
				}
				if debit.ts.After(windowEnd) {
					break
				}
				totalDebited += debit.amtCents
				debitTxIDs = append(debitTxIDs, debit.id)
			}
			if credit.amtCents == 0 {
				continue
			}
			ratio := int(totalDebited * 100 / credit.amtCents)
			if ratio >= cfg.DebitRatioPct {
				alerts = append(alerts, Alert{
					ID:           deterministicAlertID(RuleRapidMovement, entityID, acctID, credit.ts),
					RuleID:       RuleRapidMovement,
					RuleVersion:  configHash,
					EntityID:     entityID,
					AccountIDs:   []uuid.UUID{acctID},
					TxIDs:        append([]uuid.UUID{credit.id}, debitTxIDs...),
					AmountCents:  credit.amtCents,
					Currency:     credit.currency,
					SubjectType:  subjectType[entityID],
					ActivityType: "rapid_movement",
					Severity:     "high",
					Description: fmt.Sprintf(
						"rapid_movement: %d%% of %d cents debited within %dm of credit",
						ratio, credit.amtCents, cfg.WindowMinutes,
					),
				})
				alerted[entityID] = true
				break
			}
		}
	}
	return alerts
}

// ─── SAR field helpers ────────────────────────────────────────────────────────

// entityTypeMap indexes entities by ID for alert subject typing.
func entityTypeMap(sel *bank.BatchSelection) map[uuid.UUID]string {
	m := make(map[uuid.UUID]string, len(sel.Entities))
	for _, e := range sel.Entities {
		m[e.ID] = string(e.EntityType)
	}
	return m
}

// txLookup indexes transactions by ID so alert amounts can be aggregated.
func txLookup(sel *bank.BatchSelection) map[uuid.UUID]bank.Transaction {
	m := make(map[uuid.UUID]bank.Transaction, len(sel.Transactions))
	for _, tx := range sel.Transactions {
		m[tx.ID] = tx
	}
	return m
}

// sumAmount totals the amounts of the given transactions and returns the first
// non-empty currency seen. Missing IDs are ignored.
func sumAmount(txByID map[uuid.UUID]bank.Transaction, ids []uuid.UUID) (int64, string) {
	var total int64
	var currency string
	for _, id := range ids {
		tx, ok := txByID[id]
		if !ok {
			continue
		}
		total += tx.AmountCents
		if currency == "" {
			currency = tx.Currency
		}
	}
	return total, currency
}

// ─── Deterministic alert ID ───────────────────────────────────────────────────

// deterministicAlertID produces a reproducible UUID for an alert from its
// rule, entity, account, and the timestamp of the first triggering transaction.
// Uses SHA-256 so the same inputs always produce the same UUID.
func deterministicAlertID(rule RuleID, entityID, acctID uuid.UUID, ts time.Time) uuid.UUID {
	h := sha256.New()
	h.Write([]byte(rule))
	h.Write(entityID[:])
	h.Write(acctID[:])
	var tsBuf [8]byte
	binary.BigEndian.PutUint64(tsBuf[:], uint64(ts.UnixNano()))
	h.Write(tsBuf[:])
	sum := h.Sum(nil)
	var id uuid.UUID
	copy(id[:], sum[:16])
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	return id
}
