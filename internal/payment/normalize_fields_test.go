package payment

import (
	"testing"

	"github.com/yourorg/financial-crime-red-team/internal/bank"
)

func TestNormalizePaymentIntelligenceFields(t *testing.T) {
	tx := mkTx(900_000, "INV=12345")
	from, to := mkAccount(), mkAccount()
	msg := Normalize(tx, from, mkEntity("US", bank.EntityTypeIndividual), to, mkEntity("GB", bank.EntityTypeCompany))

	if msg.MessageFamily != MessageFamilyMT103 {
		t.Errorf("message_family=%q, want %q for a wire", msg.MessageFamily, MessageFamilyMT103)
	}
	if msg.NormalizationVersion != NormalizationVersion {
		t.Errorf("normalization_version=%q, want %q", msg.NormalizationVersion, NormalizationVersion)
	}
	if len(msg.SourceRecordHash) != 64 {
		t.Errorf("source_record_hash length=%d, want 64 hex chars", len(msg.SourceRecordHash))
	}

	ach := tx
	ach.TxType = bank.TxTypeACH
	if got := Normalize(ach, from, mkEntity("US", bank.EntityTypeIndividual), to, mkEntity("GB", bank.EntityTypeCompany)).MessageFamily; got != MessageFamilyPacs008 {
		t.Errorf("message_family=%q, want %q for an ACH payment", got, MessageFamilyPacs008)
	}
}

func TestSourceRecordHashIsDeterministic(t *testing.T) {
	tx := mkTx(900_000, "INV=1")
	h1 := SourceRecordHash(tx)
	h2 := SourceRecordHash(tx)
	if h1 != h2 {
		t.Fatalf("hash is not deterministic: %s != %s", h1, h2)
	}

	tx.AmountCents = 900_001
	if SourceRecordHash(tx) == h1 {
		t.Fatal("hash did not change when the amount changed")
	}
}

func TestDeduplicateRemovesDoubleCountedLedgerRecord(t *testing.T) {
	tx := mkTx(900_000, "INV=1")
	from, to := mkAccount(), mkAccount()
	fromE, toE := mkEntity("US", bank.EntityTypeIndividual), mkEntity("GB", bank.EntityTypeCompany)

	// The same ledger record seen from two extracts normalises to the same hash.
	msgA := Normalize(tx, from, fromE, to, toE)
	msgB := Normalize(tx, from, fromE, to, toE)
	other := Normalize(mkTx(50_000, "INV=2"), from, fromE, to, toE)

	unique, duplicates := Deduplicate([]Message{msgA, msgB, other})
	if len(unique) != 2 {
		t.Fatalf("unique=%d, want 2", len(unique))
	}
	if len(duplicates) != 1 {
		t.Fatalf("duplicates=%d, want 1", len(duplicates))
	}
	if duplicates[0].SourceRecordHash != msgA.SourceRecordHash {
		t.Error("removed message is not the repeated ledger record")
	}
}

func TestAnalyzeBatchDeduplicatesBeforeScoring(t *testing.T) {
	tx := mkTx(950_000, "INV=1")
	from, to := mkAccount(), mkAccount()
	fromE, toE := mkEntity("US", bank.EntityTypeIndividual), mkEntity("RU", bank.EntityTypeShell)
	msg := Normalize(tx, from, fromE, to, toE)

	scored := AnalyzeBatch([]Message{msg, msg}, Context{HighRiskCountries: map[string]bool{"RU": true}})
	if len(scored) != 1 {
		t.Fatalf("scored=%d, want 1 after deduplication", len(scored))
	}
}

func TestDeduplicateKeepsMessagesWithoutIdentity(t *testing.T) {
	a := Message{}
	b := Message{}
	unique, duplicates := Deduplicate([]Message{a, b})
	if len(unique) != 2 || len(duplicates) != 0 {
		t.Fatalf("unique=%d duplicates=%d, want 2/0 (no identity, never dropped)", len(unique), len(duplicates))
	}
}
