package payment

import (
	"github.com/yourorg/financial-crime-red-team/internal/bank"
)

// Normalize converts a synthetic bank.Transaction (with its account and entity
// context) into a canonical ISO 20022-inspired payment Message.
//
// The mapping is structural: internal account UUIDs stand in for IBANs, the
// transaction currency/amount map to InterbankSettlementAmount, and the free
// text reference maps to structured/unstructured remittance info.
func Normalize(
	tx bank.Transaction,
	fromAccount bank.Account,
	fromEntity bank.Entity,
	toAccount bank.Account,
	toEntity bank.Entity,
) Message {
	msg := Message{
		MsgID:     NewMessageID(tx.ID),
		UETR:      tx.ID, // reuse the tx UUID as the end-to-end reference
		CreatedAt: tx.CreatedAt,
		InterbankSettlementAmount: Amount{
			ValueCents: tx.AmountCents,
			Currency:   tx.Currency,
		},
		ChargeBearer: mapChargeBearer(tx.TxType),
		Purpose:      mapPurpose(tx),
		Debtor:       normalizeParty(fromAccount, fromEntity),
		Creditor:     normalizeParty(toAccount, toEntity),
		ValueDate:    tx.TxTimestamp,
		Remittance:   normalizeRemittance(tx.Reference),
		SourceTxID:   tx.ID,
	}
	return msg
}

// normalizeParty builds a Party from an account and its owning entity.
func normalizeParty(acct bank.Account, ent bank.Entity) Party {
	return Party{
		Name:       ent.Name,
		AccountID:  acct.ID,
		Country:    ent.CountryCode,
		AgentBIC:   syntheticBIC(ent.CountryCode),
		EntityType: string(ent.EntityType),
	}
}

// mapChargeBearer maps internal transaction types to an ISO 20022 charge bearer.
func mapChargeBearer(t bank.TxType) ChargeBearer {
	switch t {
	case bank.TxTypeWire:
		return ChargeBearerShared
	case bank.TxTypeACH:
		return ChargeBearerService
	case bank.TxTypeCrypto:
		return ChargeBearerDebtor
	case bank.TxTypeCash:
		return ChargeBearerCreditor
	default:
		return ChargeBearerShared
	}
}

// mapPurpose infers an ISO 20022 purpose code from transaction attributes.
func mapPurpose(tx bank.Transaction) PurposeCode {
	if tx.TxType == bank.TxTypeCrypto {
		return PurposeCrypto
	}
	// Allow an explicit override via attributes.
	if v, ok := tx.Attributes["purpose"].(string); ok {
		switch PurposeCode(v) {
		case PurposeGoods, PurposeServices, PurposeSalary, PurposeTax, PurposeTransfer, PurposeCrypto:
			return PurposeCode(v)
		}
	}
	if tx.TxType == bank.TxTypeWire {
		return PurposeTransfer
	}
	return PurposeUnknown
}

// normalizeRemittance parses a transaction reference into structured/unstructured
// remittance info. References of the form "KEY=VALUE;KEY2=VALUE2" are treated as
// structured; anything else is unstructured free text.
func normalizeRemittance(ref string) RemittanceInfo {
	if ref == "" {
		return RemittanceInfo{}
	}
	out := RemittanceInfo{}
	fields := splitPairs(ref)
	if len(fields) == 0 {
		out.Unstructured = ref
		return out
	}
	out.Structured = fields
	return out
}

// splitPairs parses "k=v;k2=v2" into a map. Returns nil if no valid pair exists.
func splitPairs(s string) map[string]string {
	m := map[string]string{}
	i := 0
	for i < len(s) {
		// find key
		start := i
		for i < len(s) && s[i] != '=' && s[i] != ';' {
			i++
		}
		if i >= len(s) || s[i] != '=' {
			return nil // not a well-formed pair list
		}
		key := s[start:i]
		i++ // skip '='
		// find value
		vstart := i
		for i < len(s) && s[i] != ';' {
			i++
		}
		val := s[vstart:i]
		if key == "" {
			return nil
		}
		m[key] = val
		if i < len(s) && s[i] == ';' {
			i++ // skip ';'
		}
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

// syntheticBIC derives a deterministic, clearly-fake BIC from a country code.
// Format: XXXXCCXX (8 chars). The "ZZ" suffix marks it as synthetic.
func syntheticBIC(country string) string {
	if len(country) != 2 {
		country = "XX"
	}
	return "SYNT" + country + "ZZ"
}
