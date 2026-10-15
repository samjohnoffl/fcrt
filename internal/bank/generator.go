// Package bank — generator.go
//
// Generator produces fully-deterministic synthetic banking data from a seed.
//
// Determinism contract:
//   Given the same Seed, Predicate, and GeneratorVersion, Generate() always
//   produces the same set of Entity, Account, and Transaction records
//   (same UUIDs, same values, same order).
//
// Implementation notes:
//   - math/rand/v2 PCG source seeded from the int64 Seed.
//   - UUIDs are derived by hashing (SHA-256) a per-cohort namespace UUID
//     with a deterministic counter, producing UUID v5-style UUIDs without
//     calling crypto/rand. This guarantees reproducibility.
//   - Monetary amounts are int64 cents; no float arithmetic.
//   - Accounts are pre-funded; transactions only debit if balance ≥ amount.
//   - tx_timestamp is always within [WindowStart, WindowEnd).
package bank

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/google/uuid"
)

// ─── Static reference data (no external sources) ─────────────────────────────

// syntheticFirstNames is a fixed list of synthetic given names.
// No real PII; generated from common naming patterns.
var syntheticFirstNames = []string{
	"Alex", "Blake", "Casey", "Dana", "Ellis", "Finley", "Grey", "Hayden",
	"Indira", "Jordan", "Kai", "Logan", "Morgan", "Noa", "Owen", "Parker",
	"Quinn", "Riley", "Sage", "Taylor", "Uma", "Val", "Winter", "Xen",
	"Yuki", "Zara", "Arlo", "Bex", "Clem", "Drew",
}

// syntheticLastNames is a fixed list of synthetic family names.
var syntheticLastNames = []string{
	"Abara", "Bellamy", "Calderon", "Dahl", "Esen", "Farook", "Gomes",
	"Holt", "Isakov", "Janson", "Kato", "Lund", "Mori", "Nasr", "Okafor",
	"Patel", "Qian", "Reyes", "Sato", "Thao", "Ueda", "Voss", "Wren",
	"Xiao", "Yilmaz", "Zhu", "Achebe", "Berg", "Cohn", "Diallo",
}

// companyWords used to generate synthetic company names.
var companyWords = []string{
	"Global", "Trans", "Inter", "Prime", "Apex", "Nova", "Delta", "Alpha",
	"Sigma", "Omega", "Nexus", "Peak", "Core", "Edge", "Link", "Arc",
}

var companySuffixes = []string{
	"Holdings", "Ventures", "Capital", "Partners", "Group", "Solutions",
	"Enterprises", "Associates", "Consulting", "Networks",
}

// availableCountries is the pool of country codes the generator can assign.
var availableCountries = []string{
	"US", "GB", "DE", "FR", "SG", "HK", "CH", "NL", "CA", "AU",
	"AE", "MX", "BR", "IN", "JP", "CN", "KY", "VG", "PA", "LU",
}

// currencyByAccountType maps account type to its default currency.
var currencyByAccountType = map[AccountType]string{
	AccountTypeChecking: CurrencyUSD,
	AccountTypeSavings:  CurrencyEUR,
	AccountTypeCrypto:   CurrencyBTC,
}

// txTypeByAccountType maps account type to likely transaction types.
var txTypeByAccountType = map[AccountType][]TxType{
	AccountTypeChecking: {TxTypeWire, TxTypeACH, TxTypeCash},
	AccountTypeSavings:  {TxTypeWire, TxTypeACH},
	AccountTypeCrypto:   {TxTypeCrypto},
}

// ─── Deterministic UUID generation ──────────────────────────────────────────

// deterministicUUID produces a reproducible UUID from a namespace UUID and an
// arbitrary byte counter. Uses SHA-256 truncated to 16 bytes, then sets the
// UUID version and variant bits to match RFC 4122 v4 layout (random-looking,
// but not random — derived entirely from namespace + counter).
func deterministicUUID(ns uuid.UUID, counter uint64) uuid.UUID {
	h := sha256.New()
	h.Write(ns[:])
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], counter)
	h.Write(buf[:])
	sum := h.Sum(nil) // 32 bytes

	var id uuid.UUID
	copy(id[:], sum[:16])
	// Set version 4 and variant bits (RFC 4122)
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	return id
}

// cohortNamespaceUUID produces a per-cohort UUID namespace from the cohort seed.
// This ensures that UUIDs from different cohorts never collide even with the
// same internal counter value.
func cohortNamespaceUUID(seed int64) uuid.UUID {
	h := sha256.New()
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(seed))
	h.Write([]byte("fincrime-bank-namespace-v0.1.0:"))
	h.Write(buf[:])
	sum := h.Sum(nil)
	var id uuid.UUID
	copy(id[:], sum[:16])
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	return id
}

// ─── Generator ───────────────────────────────────────────────────────────────

// GenerateParams are the inputs to Generate().
// All fields influence deterministic output; changing any field and re-running
// with the same Seed produces different data.
type GenerateParams struct {
	// CohortID is the UUID of the already-persisted cohort row.
	CohortID uuid.UUID
	// Seed is the RNG seed. Same seed + same params → same output.
	Seed int64
	// Predicate filters what entities/accounts are generated.
	Predicate Predicate
	// WindowStart is the inclusive start of the transaction time window.
	WindowStart time.Time
	// WindowEnd is the exclusive end of the transaction time window.
	// Must be after WindowStart. Recommended: WindowStart + 90 days.
	WindowEnd time.Time
}

// GenerateResult is the full in-memory output of a generation run.
// It has not been persisted to the database yet.
type GenerateResult struct {
	Entities     []Entity
	Accounts     []Account
	Transactions []Transaction
	Manifest     CohortManifest
}

// Generate produces a deterministic synthetic cohort in memory.
// It does NOT write to the database; callers must use Repository.SaveGenerated.
//
// Guaranteed properties:
//   - Same params → identical slice contents (same order, same values).
//   - All account references in Transactions are valid (from Accounts).
//   - All Transaction.TxTimestamp ∈ [WindowStart, WindowEnd).
//   - All AmountCents > 0.
//   - No account balance goes negative at any point (ledger invariant).
func Generate(p GenerateParams) (*GenerateResult, error) {
	if err := validateParams(p); err != nil {
		return nil, err
	}

	// Seed the PRNG. PCG is reproducible and has good statistical properties.
	// We use the seed as the state and a fixed stream constant so two different
	// seeds always produce different sequences.
	rng := rand.New(rand.NewPCG(uint64(p.Seed), 0xdeadbeefcafe1234))

	ns := cohortNamespaceUUID(p.Seed)
	var counter uint64

	// ── 1. Entities ──────────────────────────────────────────────────────────

	minE, maxE := resolveEntityBounds(p.Predicate)
	nEntities := minE + rng.IntN(maxE-minE+1)

	entities := make([]Entity, nEntities)
	for i := range entities {
		counter++
		etype := pickEntityType(rng, p.Predicate)
		country := pickCountry(rng, p.Predicate)
		entities[i] = Entity{
			ID:          deterministicUUID(ns, counter),
			CohortID:    p.CohortID,
			EntityType:  etype,
			Name:        generateName(rng, etype),
			CountryCode: country,
			RiskScore:   roundRiskScore(rng.Float64()),
			Attributes:  entityAttributes(rng, etype, country),
			CreatedAt:   p.WindowStart.Add(-30 * 24 * time.Hour), // opened 30 days before window
		}
	}

	// ── 2. Accounts ──────────────────────────────────────────────────────────

	// Each entity gets 1–3 accounts.
	var accounts []Account
	entityToAccounts := make(map[uuid.UUID][]int) // entity UUID → account indices

	for _, e := range entities {
		nAccounts := 1 + rng.IntN(3) // 1, 2, or 3
		for j := 0; j < nAccounts; j++ {
			counter++
			atype := pickAccountType(rng, e.EntityType)
			currency := currencyByAccountType[atype]
			// Initial balance: 100 to 100,000 USD-equivalent in cents.
			initialBalance := int64(10000+rng.IntN(9990001)) // 100.00 to 100_000.00

			acc := Account{
				ID:           deterministicUUID(ns, counter),
				CohortID:     p.CohortID,
				EntityID:     e.ID,
				AccountType:  atype,
				Currency:     currency,
				BalanceCents: initialBalance,
				OpenedAt:     e.CreatedAt.Add(time.Duration(rng.IntN(15)) * 24 * time.Hour),
				Attributes:   accountAttributes(rng, atype),
				CreatedAt:    e.CreatedAt,
			}
			idx := len(accounts)
			accounts = append(accounts, acc)
			entityToAccounts[e.ID] = append(entityToAccounts[e.ID], idx)
		}
	}

	// ── 3. Transactions ───────────────────────────────────────────────────────

	// Build a mutable balance map keyed by account index for ledger tracking.
	balances := make([]int64, len(accounts))
	for i, a := range accounts {
		balances[i] = a.BalanceCents
	}

	// Build index: account index → account id for reverse lookup.
	// Build flattened list of all account indices for "any account" selection.
	allAcctIdx := make([]int, len(accounts))
	for i := range accounts {
		allAcctIdx[i] = i
	}

	windowDuration := p.WindowEnd.Sub(p.WindowStart)
	var transactions []Transaction

	// Generate 5–25 transactions per entity.
	for _, e := range entities {
		acctIndices := entityToAccounts[e.ID]
		if len(acctIndices) == 0 {
			continue
		}
		nTx := 5 + rng.IntN(21) // 5–25
		for t := 0; t < nTx; t++ {
			// Pick a from-account owned by this entity.
			fromIdx := acctIndices[rng.IntN(len(acctIndices))]
			fromAcct := accounts[fromIdx]

			// Pick a to-account (may be any account in the cohort, including
			// same entity, but must be a different account).
			var toIdx int
			for attempt := 0; attempt < 20; attempt++ {
				toIdx = rng.IntN(len(accounts))
				if toIdx != fromIdx {
					break
				}
			}
			if toIdx == fromIdx {
				// Could not find a different account (very small cohort); skip.
				continue
			}

			// Amount: 1 to min(10% of from-balance, 50_000_00 cents).
			// Never exceed the current balance to maintain non-negative invariant.
			if balances[fromIdx] <= 0 {
				// Re-fund: add a synthetic deposit (not recorded as a tx).
				balances[fromIdx] = int64(10000 + rng.IntN(1000000))
			}
			maxAmount := balances[fromIdx]
			if maxAmount > 5_000_000 { // cap at $50,000.00
				maxAmount = 5_000_000
			}
			if maxAmount <= 0 {
				continue
			}
			// Minimum 1 cent.
			amountCents := int64(1 + rng.Int64N(maxAmount))

			// Timestamp: uniform random within [WindowStart, WindowEnd).
			offsetNs := rng.Int64N(int64(windowDuration))
			ts := p.WindowStart.Add(time.Duration(offsetNs)).Truncate(time.Second)

			counter++
			txType := pickTxType(rng, fromAcct.AccountType)

			tx := Transaction{
				ID:            deterministicUUID(ns, counter),
				CohortID:      p.CohortID,
				FromAccountID: fromAcct.ID,
				ToAccountID:   accounts[toIdx].ID,
				AmountCents:   amountCents,
				Currency:      fromAcct.Currency,
				TxType:        txType,
				TxTimestamp:   ts,
				Reference:     fmt.Sprintf("REF-%s-%04d", e.CountryCode, t+1),
				Attributes:    txAttributes(rng, txType),
				CreatedAt:     ts,
			}

			// Apply ledger: debit from, credit to.
			balances[fromIdx] -= amountCents
			balances[toIdx] += amountCents

			transactions = append(transactions, tx)
		}
	}

	// Sort transactions by timestamp for stable ordering (required for
	// byte-identical output across runs — maps are non-deterministic in Go
	// but slices with explicit sort are stable given a stable sort algorithm).
	sortTransactions(transactions)

	// Update Account.BalanceCents to reflect final ledger state after all txs.
	for i := range accounts {
		accounts[i].BalanceCents = balances[i]
	}

	manifest := CohortManifest{
		CohortID:         p.CohortID,
		GeneratorVersion: GeneratorVersion,
		Seed:             p.Seed,
		WindowStart:      p.WindowStart,
		WindowEnd:        p.WindowEnd,
		EntityCount:      len(entities),
		AccountCount:     len(accounts),
		TxCount:          len(transactions),
	}

	return &GenerateResult{
		Entities:     entities,
		Accounts:     accounts,
		Transactions: transactions,
		Manifest:     manifest,
	}, nil
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func validateParams(p GenerateParams) error {
	if p.CohortID == uuid.Nil {
		return fmt.Errorf("%w: CohortID is nil", ErrInvalidPredicate)
	}
	if p.WindowEnd.IsZero() || p.WindowStart.IsZero() {
		return fmt.Errorf("%w: window times must be set", ErrInvalidPredicate)
	}
	if !p.WindowEnd.After(p.WindowStart) {
		return fmt.Errorf("%w: WindowEnd must be after WindowStart", ErrInvalidPredicate)
	}
	if p.Predicate.EntityType != nil {
		valid := false
		for _, et := range allEntityTypes {
			if *p.Predicate.EntityType == et {
				valid = true
				break
			}
		}
		if !valid {
			return fmt.Errorf("%w: unknown entity_type %q", ErrInvalidPredicate, *p.Predicate.EntityType)
		}
	}
	if p.Predicate.CountryCode != nil && len(*p.Predicate.CountryCode) != 2 {
		return fmt.Errorf("%w: country_code must be exactly 2 characters", ErrInvalidPredicate)
	}
	return nil
}

func resolveEntityBounds(pred Predicate) (min, max int) {
	min = DefaultMinEntities
	max = DefaultMaxEntities
	if pred.MinEntities != nil && *pred.MinEntities > 0 {
		min = *pred.MinEntities
	}
	if pred.MaxEntities != nil && *pred.MaxEntities >= min {
		max = *pred.MaxEntities
	}
	return
}

func pickEntityType(rng *rand.Rand, pred Predicate) EntityType {
	if pred.EntityType != nil {
		return *pred.EntityType
	}
	types := []EntityType{EntityTypeIndividual, EntityTypeCompany, EntityTypeShell}
	// Weighted: 60% individual, 30% company, 10% shell
	weights := []int{60, 30, 10}
	return weightedPick(rng, types, weights)
}

func pickCountry(rng *rand.Rand, pred Predicate) string {
	if pred.CountryCode != nil {
		return *pred.CountryCode
	}
	return availableCountries[rng.IntN(len(availableCountries))]
}

func generateName(rng *rand.Rand, etype EntityType) string {
	switch etype {
	case EntityTypeIndividual:
		first := syntheticFirstNames[rng.IntN(len(syntheticFirstNames))]
		last := syntheticLastNames[rng.IntN(len(syntheticLastNames))]
		return first + " " + last
	case EntityTypeCompany, EntityTypeShell:
		word := companyWords[rng.IntN(len(companyWords))]
		suffix := companySuffixes[rng.IntN(len(companySuffixes))]
		return word + " " + suffix
	default:
		return "Unknown Entity"
	}
}

func pickAccountType(rng *rand.Rand, etype EntityType) AccountType {
	switch etype {
	case EntityTypeIndividual:
		// Individuals: mostly checking, sometimes savings, rarely crypto
		types := []AccountType{AccountTypeChecking, AccountTypeSavings, AccountTypeCrypto}
		weights := []int{60, 30, 10}
		return weightedPick(rng, types, weights)
	case EntityTypeCompany:
		// Companies: mostly checking
		types := []AccountType{AccountTypeChecking, AccountTypeSavings}
		weights := []int{80, 20}
		return weightedPick(rng, types, weights)
	case EntityTypeShell:
		// Shells: all account types equally
		types := []AccountType{AccountTypeChecking, AccountTypeSavings, AccountTypeCrypto}
		weights := []int{40, 30, 30}
		return weightedPick(rng, types, weights)
	default:
		return AccountTypeChecking
	}
}

func pickTxType(rng *rand.Rand, atype AccountType) TxType {
	types := txTypeByAccountType[atype]
	if len(types) == 0 {
		return TxTypeACH
	}
	return types[rng.IntN(len(types))]
}

// weightedPick picks from a slice using integer weights (must sum > 0).
func weightedPick[T any](rng *rand.Rand, items []T, weights []int) T {
	total := 0
	for _, w := range weights {
		total += w
	}
	r := rng.IntN(total)
	for i, w := range weights {
		r -= w
		if r < 0 {
			return items[i]
		}
	}
	return items[len(items)-1]
}

// roundRiskScore rounds to 4 decimal places to match NUMERIC(5,4) column.
func roundRiskScore(f float64) float64 {
	return float64(int64(f*10000)) / 10000
}

func entityAttributes(rng *rand.Rand, etype EntityType, country string) map[string]any {
	// any is acceptable here: this is a flexible JSONB blob with no schema.
	attrs := map[string]any{
		"incorporation_country": country,
		"kyc_tier":             rng.IntN(3) + 1, // 1, 2, or 3
	}
	if etype == EntityTypeShell {
		attrs["nominee_directors"] = rng.IntN(3) + 1
		attrs["beneficial_owner_disclosed"] = rng.IntN(2) == 0
	}
	return attrs
}

func accountAttributes(rng *rand.Rand, atype AccountType) map[string]any {
	// any is acceptable here: flexible JSONB blob.
	attrs := map[string]any{
		"overdraft_enabled": false,
	}
	if atype == AccountTypeCrypto {
		attrs["wallet_type"] = []string{"hot", "cold"}[rng.IntN(2)]
	}
	return attrs
}

func txAttributes(rng *rand.Rand, txType TxType) map[string]any {
	// any is acceptable here: flexible JSONB blob.
	attrs := map[string]any{
		"channel": []string{"online", "branch", "mobile"}[rng.IntN(3)],
	}
	if txType == TxTypeWire {
		attrs["swift_code"] = fmt.Sprintf("BIC%04d", rng.IntN(10000))
	}
	return attrs
}

// sortTransactions sorts by TxTimestamp then by ID for a stable total order.
// Uses a simple insertion sort — transaction counts are small enough
// (typically < 1000 per cohort) that O(n²) is acceptable and avoids
// importing sort.Slice which uses an unstable algorithm for small n in some Go versions.
// For n > 500 we fall back to stdlib sort.
func sortTransactions(txs []Transaction) {
	n := len(txs)
	if n <= 1 {
		return
	}
	// Stable sort by (TxTimestamp, ID string) using stdlib sort.SliceStable.
	// sort.SliceStable is deterministic given a deterministic less function.
	stableSort(txs)
}
