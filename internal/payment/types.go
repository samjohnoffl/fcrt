package payment

import (
	"time"

	"github.com/google/uuid"
)

// This package implements a payment intelligence layer inspired by SWIFT MT103
// (Single Customer Credit Transfer) and ISO 20022 pacs.008 (FI To FI Customer
// Credit Transfer). It normalises the synthetic bank.Transaction model into a
// canonical payment message and extracts AML-relevant features used by the
// scenario generator and the coverage engine.
//
// No real SWIFT/ISO 20022 wire messages are parsed; this is a simplified,
// structurally-faithful model for synthetic data only.

// ChargeBearer mirrors ISO 20022 ChargeBearerType1Code.
type ChargeBearer string

const (
	ChargeBearerShared   ChargeBearer = "SHAR" // shared
	ChargeBearerDebtor   ChargeBearer = "DEBT" // borne by debtor
	ChargeBearerCreditor ChargeBearer = "CRED" // borne by creditor
	ChargeBearerService  ChargeBearer = "SLEV" // following service level
)

// PurposeCode is an ISO 20022 ExternalPurpose1Code (subset relevant to AML).
type PurposeCode string

const (
	PurposeGoods    PurposeCode = "GDDS" // purchase of goods
	PurposeServices PurposeCode = "SCVE" // purchase of services
	PurposeSalary   PurposeCode = "SALA" // salary payment
	PurposeTax      PurposeCode = "TAXS" // tax payment
	PurposeTransfer PurposeCode = "CORT" // trade settlement / general transfer
	PurposeCrypto   PurposeCode = "BCCT" // crypto / digital asset transfer
	PurposeUnknown  PurposeCode = "OTHR" // other
)

// Party is a debtor or creditor in a payment message.
// There is no PII: names are procedurally generated.
type Party struct {
	// Name is the account holder name.
	Name string `json:"name"`
	// AccountID is the internal account UUID (not an IBAN).
	AccountID uuid.UUID `json:"account_id"`
	// Country is the ISO 3166-1 alpha-2 country of the party.
	Country string `json:"country"`
	// AgentBIC is a synthetic business identifier code of the account servicer.
	AgentBIC string `json:"agent_bic,omitempty"`
	// EntityType mirrors bank.EntityType ("individual"/"company"/"shell").
	EntityType string `json:"entity_type,omitempty"`
}

// Amount is a monetary amount with its ISO 4217 currency.
// Value is held in the smallest unit (cents/satoshis) to avoid float drift.
type Amount struct {
	ValueCents int64  `json:"value_cents"`
	Currency   string `json:"currency"`
}

// RemittanceInfo carries the free-text payment reference (ISO 20022
// RemittanceInformation, MT103 field 70). Structured references may indicate
// invoice-based trade or, conversely, deliberate obfuscation.
type RemittanceInfo struct {
	// Unstructured is raw free text.
	Unstructured string `json:"unstructured,omitempty"`
	// Structured holds key/value reference pairs when present.
	Structured map[string]string `json:"structured,omitempty"`
}

// Message is a canonical payment message inspired by ISO 20022 pacs.008.
type Message struct {
	// MsgID is the message identifier (MT103 field 20 / pacs.008 MsgId).
	MsgID string `json:"msg_id"`
	// MessageFamily is the normalised message type ("pacs.008" or "mt103").
	MessageFamily string `json:"message_family"`
	// NormalizationVersion identifies the mapping rules used to produce this
	// message, so two messages are only compared when they were normalised the
	// same way.
	NormalizationVersion string `json:"normalization_version"`
	// SourceRecordHash is a deterministic hash of the source ledger record.
	// It is the double-count guard: the same record normalised twice produces
	// the same hash and can be deduplicated before aggregation.
	SourceRecordHash string `json:"source_record_hash"`
	// UETR is the Unique End-to-end Transaction Reference (SWIFT gpi).
	UETR uuid.UUID `json:"uetr"`
	// CreatedAt is when the message was created locally.
	CreatedAt time.Time `json:"created_at"`

	// InterbankSettlementAmount is the instructed amount.
	InterbankSettlementAmount Amount `json:"interbank_settlement_amount"`
	// ChargeBearer determines who pays fees.
	ChargeBearer ChargeBearer `json:"charge_bearer"`
	// Purpose is the ISO 20022 purpose code.
	Purpose PurposeCode `json:"purpose"`

	Debtor   Party `json:"debtor"`
	Creditor Party `json:"creditor"`

	// ValueDate is the settlement date.
	ValueDate time.Time `json:"value_date"`
	// Remittance is the payment reference.
	Remittance RemittanceInfo `json:"remittance"`

	// SourceTxID links back to the originating synthetic transaction.
	SourceTxID uuid.UUID `json:"source_tx_id"`
}

// NewMessageID derives a deterministic message identifier from a transaction ID.
func NewMessageID(txID uuid.UUID) string {
	return "MSG-" + txID.String()[:13]
}

// Supported message families.
const (
	MessageFamilyPacs008 = "pacs.008"
	MessageFamilyMT103   = "mt103"
)

// NormalizationVersion is the version of the transaction -> message mapping.
// Bump it when the mapping changes in a way that would alter extracted features.
const NormalizationVersion = "v1.0.0"
