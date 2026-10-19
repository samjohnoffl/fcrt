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
	"strings"
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
	outcomes := make([]RuleOutcome, 0, len(AllRules))

	for _, id := range AllRules {
		if !ev.config.IsRuleEnabled(id) {
			outcomes = append(outcomes, RuleOutcome{
				RuleID:      id,
				RuleVersion: ev.config.ConfigHash,
				Status:      OutcomeNotApplicable,
				ReasonCode:  ReasonRuleDisabled,
				Detail:      "rule is not enabled in this control configuration",
			})
			continue
		}
		oc, ruleAlerts := ev.runRuleSafe(id, sel)
		outcomes = append(outcomes, oc)
		alerts = append(alerts, ruleAlerts...)
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
		Outcomes:    outcomes,
		RunAt:       time.Time{}, // zero: callers set this from their own clock
	}
}

// runRuleSafe isolates one control. A panic inside a rule is converted to a
// FAILED outcome; it must never be reported as NOT TRIGGERED, because that
// would claim the control examined behaviour it never examined.
func (ev *Evaluator) runRuleSafe(id RuleID, sel *bank.BatchSelection) (oc RuleOutcome, alerts []Alert) {
	oc = RuleOutcome{RuleID: id, RuleVersion: ev.config.ConfigHash}

	defer func() {
		if r := recover(); r != nil {
			oc.Status = OutcomeFailed
			oc.ReasonCode = ReasonEvaluationError
			oc.Detail = fmt.Sprintf("control error: %v", r)
			alerts = nil
		}
	}()

	alerts = ev.runRule(id, sel)
	if len(alerts) == 0 {
		oc.Status, oc.ReasonCode, oc.Detail = ev.noTriggerStatus(id, sel)
		return oc, alerts
	}

	oc.Status = OutcomeTriggered
	oc.ReasonCode = ReasonPatternDetected
	oc.Detail = alerts[0].Description
	oc.EventIDs, oc.AccountIDs, oc.EntityIDs, oc.AmountCents, oc.Currency = collectAlertRefs(alerts)
	return oc, alerts
}

// runRule dispatches to the concrete control implementation.
func (ev *Evaluator) runRule(id RuleID, sel *bank.BatchSelection) []Alert {
	switch id {
	case RuleStructuring:
		return ev.runStructuring(sel)
	case RuleLayering:
		return ev.runLayering(sel)
	case RuleRapidMovement:
		return ev.runRapidMovement(sel)
	case RuleVelocity:
		return ev.runVelocity(sel)
	case RuleCounterpartyExpansion:
		return ev.runCounterpartyExpansion(sel)
	case RuleDormancyActivation:
		return ev.runDormancyActivation(sel)
	case RuleGeographicBand:
		return ev.runGeographicBand(sel)
	case RuleOwnershipRelationship:
		return ev.runOwnershipRelationship(sel)
	default:
		panic(fmt.Sprintf("unknown control %q", id))
	}
}

// noTriggerStatus distinguishes "examined and clean" from "could not examine".
func (ev *Evaluator) noTriggerStatus(id RuleID, sel *bank.BatchSelection) (OutcomeStatus, ReasonCode, string) {
	if len(sel.Transactions) == 0 {
		return OutcomeNotApplicable, ReasonNoEvents, "no synthetic events in the selected batch"
	}
	switch id {
	case RuleDormancyActivation:
		if !hasAccountWithMinTx(sel, 2) {
			return OutcomeInconclusive, ReasonInsufficientHistory,
				"no account has enough history to measure a dormancy gap"
		}
	case RuleGeographicBand:
		if !hasCountryData(sel) {
			return OutcomeNotApplicable, ReasonMissingField, "counterparty country data is unavailable"
		}
	case RuleOwnershipRelationship:
		if !hasEntityWithAccounts(sel, 2) {
			return OutcomeNotApplicable, ReasonMissingField, "no entity controls multiple accounts"
		}
	}
	return OutcomeNotTriggered, ReasonThresholdNotMet, "control evaluated the behaviour and found no match"
}

// collectAlertRefs merges alert references into one outcome.
func collectAlertRefs(alerts []Alert) (events, accounts, entities []uuid.UUID, amount int64, currency string) {
	seenEvent := map[uuid.UUID]bool{}
	seenAcct := map[uuid.UUID]bool{}
	seenEntity := map[uuid.UUID]bool{}
	for _, a := range alerts {
		for _, id := range a.TxIDs {
			if !seenEvent[id] {
				seenEvent[id] = true
				events = append(events, id)
			}
		}
		for _, id := range a.AccountIDs {
			if !seenAcct[id] {
				seenAcct[id] = true
				accounts = append(accounts, id)
			}
		}
		if !seenEntity[a.EntityID] {
			seenEntity[a.EntityID] = true
			entities = append(entities, a.EntityID)
		}
		amount += a.AmountCents
		if currency == "" {
			currency = a.Currency
		}
	}
	sort.Slice(events, func(i, j int) bool { return events[i].String() < events[j].String() })
	sort.Slice(accounts, func(i, j int) bool { return accounts[i].String() < accounts[j].String() })
	sort.Slice(entities, func(i, j int) bool { return entities[i].String() < entities[j].String() })
	return events, accounts, entities, amount, currency
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

// ─── Velocity Rule ────────────────────────────────────────────────────────────
//
// Fires when an account's outbound activity rises materially between the first
// and second half of the evaluation window (count or value).

func (ev *Evaluator) runVelocity(sel *bank.BatchSelection) []Alert {
	cfg := ev.config.Velocity
	_, end := evalWindow(sel)
	window := time.Duration(cfg.WindowHours) * time.Hour
	evalStart := end.Add(-window)
	baselineStart := evalStart.Add(-window)

	acctToEntity := ownerMap(sel)

	type stat struct {
		count    int
		value    int64
		currency string
		ids      []uuid.UUID
	}
	before := make(map[uuid.UUID]*stat)
	after := make(map[uuid.UUID]*stat)

	add := func(m map[uuid.UUID]*stat, tx bank.Transaction) {
		s := m[tx.FromAccountID]
		if s == nil {
			s = &stat{}
			m[tx.FromAccountID] = s
		}
		s.count++
		s.value += tx.AmountCents
		s.ids = append(s.ids, tx.ID)
		if s.currency == "" {
			s.currency = tx.Currency
		}
	}
	for _, tx := range sel.Transactions {
		switch {
		case tx.TxTimestamp.Before(baselineStart):
			continue
		case tx.TxTimestamp.Before(evalStart):
			add(before, tx)
		case tx.TxTimestamp.Before(end):
			add(after, tx)
		default:
			continue
		}
	}

	var alerts []Alert
	for acct, a := range after {
		b := before[acct]
		bc, bv := 0, int64(0)
		if b != nil {
			bc, bv = b.count, b.value
		}
		countRise := a.count - bc
		valueRose := bv > 0 && float64(a.value) >= float64(bv)*(1.0+float64(cfg.MinValueIncreasePct)/100.0)
		if countRise < cfg.MinCountIncrease && !valueRose {
			continue
		}
		entity := acctToEntity[acct]
		alerts = append(alerts, Alert{
			ID:           deterministicAlertID(RuleVelocity, entity, acct, evalStart),
			RuleID:       RuleVelocity,
			RuleVersion:  ev.config.ConfigHash,
			EntityID:     entity,
			AccountIDs:   []uuid.UUID{acct},
			TxIDs:        a.ids,
			AmountCents:  a.value,
			Currency:     a.currency,
			SubjectType:  entityTypeMap(sel)[entity],
			ActivityType: "velocity",
			Severity:     "medium",
			Description: fmt.Sprintf(
				"velocity: outbound activity rose from %d to %d transactions over %dh (value %d→%d cents)",
				bc, a.count, cfg.WindowHours, bv, a.value,
			),
		})
	}
	return alerts
}

// ─── Counterparty Expansion Rule ──────────────────────────────────────────────
//
// Fires when the number of distinct counterparties grows by at least
// MinNewCounterparties between the preceding window and the evaluation window.

func (ev *Evaluator) runCounterpartyExpansion(sel *bank.BatchSelection) []Alert {
	cfg := ev.config.CounterpartyExpansion
	_, end := evalWindow(sel)
	window := time.Duration(cfg.WindowHours) * time.Hour
	evalStart := end.Add(-window)
	baselineStart := evalStart.Add(-window)

	acctToEntity := ownerMap(sel)
	subjects := entityTypeMap(sel)

	type counts struct {
		counterparties map[uuid.UUID]bool
		ids            []uuid.UUID
	}
	before := make(map[uuid.UUID]*counts)
	after := make(map[uuid.UUID]*counts)

	for _, tx := range sel.Transactions {
		entity, ok := acctToEntity[tx.FromAccountID]
		if !ok {
			continue
		}
		var bucket map[uuid.UUID]*counts
		switch {
		case tx.TxTimestamp.Before(baselineStart):
			continue
		case tx.TxTimestamp.Before(evalStart):
			bucket = before
		case tx.TxTimestamp.Before(end):
			bucket = after
		default:
			continue
		}
		c := bucket[entity]
		if c == nil {
			c = &counts{counterparties: make(map[uuid.UUID]bool)}
			bucket[entity] = c
		}
		c.counterparties[tx.ToAccountID] = true
		c.ids = append(c.ids, tx.ID)
	}

	var alerts []Alert
	for entity, a := range after {
		prev := 0
		if b := before[entity]; b != nil {
			prev = len(b.counterparties)
		}
		if len(a.counterparties) < cfg.MinNewCounterparties || len(a.counterparties) < prev+cfg.MinNewCounterparties {
			continue
		}
		alerts = append(alerts, Alert{
			ID:           deterministicAlertID(RuleCounterpartyExpansion, entity, entity, evalStart),
			RuleID:       RuleCounterpartyExpansion,
			RuleVersion:  ev.config.ConfigHash,
			EntityID:     entity,
			TxIDs:        a.ids,
			Currency:     "",
			SubjectType:  subjects[entity],
			ActivityType: "counterparty_expansion",
			Severity:     "medium",
			Description: fmt.Sprintf(
				"counterparty_expansion: distinct counterparties rose from %d to %d within %dh",
				prev, len(a.counterparties), cfg.WindowHours,
			),
		})
	}
	return alerts
}

// ─── Dormancy Activation Rule ─────────────────────────────────────────────────
//
// Fires when an account has no activity for DormantDays and then resumes with a
// transaction at or above MinResumeAmountCents.

func (ev *Evaluator) runDormancyActivation(sel *bank.BatchSelection) []Alert {
	cfg := ev.config.Dormancy
	acctToEntity := ownerMap(sel)
	subjects := entityTypeMap(sel)

	byAcct := make(map[uuid.UUID][]bank.Transaction)
	for _, tx := range sel.Transactions {
		byAcct[tx.FromAccountID] = append(byAcct[tx.FromAccountID], tx)
		byAcct[tx.ToAccountID] = append(byAcct[tx.ToAccountID], tx)
	}

	dormant := time.Duration(cfg.DormantDays) * 24 * time.Hour
	var alerts []Alert

	for acct, txs := range byAcct {
		if len(txs) < 2 {
			continue
		}
		sort.Slice(txs, func(i, j int) bool { return txs[i].TxTimestamp.Before(txs[j].TxTimestamp) })
		for i := 1; i < len(txs); i++ {
			gap := txs[i].TxTimestamp.Sub(txs[i-1].TxTimestamp)
			if gap < dormant || txs[i].AmountCents < cfg.MinResumeAmountCents {
				continue
			}
			entity := acctToEntity[acct]
			alerts = append(alerts, Alert{
				ID:           deterministicAlertID(RuleDormancyActivation, entity, acct, txs[i].TxTimestamp),
				RuleID:       RuleDormancyActivation,
				RuleVersion:  ev.config.ConfigHash,
				EntityID:     entity,
				AccountIDs:   []uuid.UUID{acct},
				TxIDs:        []uuid.UUID{txs[i-1].ID, txs[i].ID},
				AmountCents:  txs[i].AmountCents,
				Currency:     txs[i].Currency,
				SubjectType:  subjects[entity],
				ActivityType: "dormancy_activation",
				Severity:     "medium",
				Description: fmt.Sprintf(
					"dormancy_activation: account inactive for %d days then resumed with %d cents",
					int(gap.Hours()/24), txs[i].AmountCents,
				),
			})
			break // one alert per account
		}
	}
	return alerts
}

// ─── Geographic Band Rule ─────────────────────────────────────────────────────
//
// Fires when an entity whose own jurisdiction is not high-risk sends at least
// MinTransactions to counterparties in a high-risk jurisdiction.

func (ev *Evaluator) runGeographicBand(sel *bank.BatchSelection) []Alert {
	cfg := ev.config.Geographic
	if len(cfg.HighRiskCountryCodes) == 0 {
		return nil
	}
	high := make(map[string]bool, len(cfg.HighRiskCountryCodes))
	for _, c := range cfg.HighRiskCountryCodes {
		high[strings.ToUpper(c)] = true
	}

	acctToEntity := ownerMap(sel)
	country := make(map[uuid.UUID]string, len(sel.Entities))
	subjects := entityTypeMap(sel)
	for _, e := range sel.Entities {
		country[e.ID] = strings.ToUpper(e.CountryCode)
	}

	type exposure struct {
		ids  []uuid.UUID
		acct uuid.UUID
	}
	byEntity := make(map[uuid.UUID]*exposure)

	for _, tx := range sel.Transactions {
		fromEntity, ok := acctToEntity[tx.FromAccountID]
		if !ok {
			continue
		}
		toEntity, ok := acctToEntity[tx.ToAccountID]
		if !ok {
			continue
		}
		if high[country[fromEntity]] || !high[country[toEntity]] {
			continue
		}
		x := byEntity[fromEntity]
		if x == nil {
			x = &exposure{acct: tx.FromAccountID}
			byEntity[fromEntity] = x
		}
		x.ids = append(x.ids, tx.ID)
	}

	var alerts []Alert
	for entity, x := range byEntity {
		if len(x.ids) < cfg.MinTransactions {
			continue
		}
		alerts = append(alerts, Alert{
			ID:           deterministicAlertID(RuleGeographicBand, entity, x.acct, time.Time{}),
			RuleID:       RuleGeographicBand,
			RuleVersion:  ev.config.ConfigHash,
			EntityID:     entity,
			AccountIDs:   []uuid.UUID{x.acct},
			TxIDs:        x.ids,
			SubjectType:  subjects[entity],
			ActivityType: "geographic_band",
			Severity:     "high",
			Description: fmt.Sprintf(
				"geographic_band: %d transactions to high-risk jurisdictions from a non-high-risk entity",
				len(x.ids),
			),
		})
	}
	return alerts
}

// ─── Ownership Relationship Rule ──────────────────────────────────────────────
//
// Fires when an entity controls at least MinLinkedAccounts accounts and moves
// funds between those linked accounts at least MinTransactions times within
// WindowHours. The signal is shared control, not merely shared ownership.

func (ev *Evaluator) runOwnershipRelationship(sel *bank.BatchSelection) []Alert {
	cfg := ev.config.Ownership
	_, end := evalWindow(sel)
	start := end.Add(-time.Duration(cfg.WindowHours) * time.Hour)

	acctToEntity := ownerMap(sel)
	subjects := entityTypeMap(sel)

	entityAccounts := make(map[uuid.UUID]map[uuid.UUID]bool)
	for _, a := range sel.Accounts {
		if entityAccounts[a.EntityID] == nil {
			entityAccounts[a.EntityID] = make(map[uuid.UUID]bool)
		}
		entityAccounts[a.EntityID][a.ID] = true
	}

	type linked struct {
		txIDs    []uuid.UUID
		accounts map[uuid.UUID]bool
	}
	byEntity := make(map[uuid.UUID]*linked)

	for _, tx := range sel.Transactions {
		if tx.TxTimestamp.Before(start) || !tx.TxTimestamp.Before(end) {
			continue
		}
		fromEntity, ok1 := acctToEntity[tx.FromAccountID]
		toEntity, ok2 := acctToEntity[tx.ToAccountID]
		if !ok1 || !ok2 || fromEntity != toEntity {
			continue
		}
		if len(entityAccounts[fromEntity]) < cfg.MinLinkedAccounts {
			continue
		}
		l := byEntity[fromEntity]
		if l == nil {
			l = &linked{accounts: make(map[uuid.UUID]bool)}
			byEntity[fromEntity] = l
		}
		l.txIDs = append(l.txIDs, tx.ID)
		l.accounts[tx.FromAccountID] = true
		l.accounts[tx.ToAccountID] = true
	}

	var alerts []Alert
	for entity, l := range byEntity {
		if len(l.accounts) < cfg.MinLinkedAccounts || len(l.txIDs) < cfg.MinTransactions {
			continue
		}
		acctIDs := make([]uuid.UUID, 0, len(l.accounts))
		for id := range l.accounts {
			acctIDs = append(acctIDs, id)
		}
		sort.Slice(acctIDs, func(i, j int) bool { return acctIDs[i].String() < acctIDs[j].String() })
		alerts = append(alerts, Alert{
			ID:           deterministicAlertID(RuleOwnershipRelationship, entity, acctIDs[0], start),
			RuleID:       RuleOwnershipRelationship,
			RuleVersion:  ev.config.ConfigHash,
			EntityID:     entity,
			AccountIDs:   acctIDs,
			TxIDs:        l.txIDs,
			SubjectType:  subjects[entity],
			ActivityType: "ownership_relationship",
			Severity:     "high",
			Description: fmt.Sprintf(
				"ownership_relationship: %d linked accounts moved funds between themselves %d times within %dh",
				len(acctIDs), len(l.txIDs), cfg.WindowHours,
			),
		})
	}
	return alerts
}

// ─── Rule helpers ─────────────────────────────────────────────────────────────

// ownerMap indexes accounts by their owning entity.
func ownerMap(sel *bank.BatchSelection) map[uuid.UUID]uuid.UUID {
	m := make(map[uuid.UUID]uuid.UUID, len(sel.Accounts))
	for _, a := range sel.Accounts {
		m[a.ID] = a.EntityID
	}
	return m
}

// evalWindow returns the evaluation interval. It prefers the pinned selection
// window and falls back to the observed transaction range for unit tests or
// partially pinned selections.
func evalWindow(sel *bank.BatchSelection) (time.Time, time.Time) {
	if !sel.WindowStart.IsZero() && !sel.WindowEnd.IsZero() && sel.WindowEnd.After(sel.WindowStart) {
		return sel.WindowStart, sel.WindowEnd
	}
	if len(sel.Transactions) == 0 {
		return sel.WindowStart, sel.WindowEnd
	}
	start, end := sel.Transactions[0].TxTimestamp, sel.Transactions[0].TxTimestamp
	for _, tx := range sel.Transactions[1:] {
		if tx.TxTimestamp.Before(start) {
			start = tx.TxTimestamp
		}
		if tx.TxTimestamp.After(end) {
			end = tx.TxTimestamp
		}
	}
	return start, end.Add(time.Second)
}

func hasAccountWithMinTx(sel *bank.BatchSelection, min int) bool {
	counts := make(map[uuid.UUID]int)
	for _, tx := range sel.Transactions {
		counts[tx.FromAccountID]++
		counts[tx.ToAccountID]++
	}
	for _, n := range counts {
		if n >= min {
			return true
		}
	}
	return false
}

func hasCountryData(sel *bank.BatchSelection) bool {
	for _, e := range sel.Entities {
		if e.CountryCode != "" {
			return true
		}
	}
	return false
}

func hasEntityWithAccounts(sel *bank.BatchSelection, min int) bool {
	counts := make(map[uuid.UUID]int)
	for _, a := range sel.Accounts {
		counts[a.EntityID]++
	}
	for _, n := range counts {
		if n >= min {
			return true
		}
	}
	return false
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
