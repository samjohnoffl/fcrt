package payment

import (
	"sort"

	"github.com/google/uuid"
)

// Scored is a payment message with its extracted features and intelligence score.
type Scored struct {
	Message  Message
	Features Features
	Score    float64
}

// Analyze extracts features and computes the intelligence score for one message.
func Analyze(msg Message, ctx Context) Scored {
	f := Extract(msg, ctx)
	return Scored{Message: msg, Features: f, Score: f.IntelligenceScore()}
}

// Deduplicate removes repeated ledger records from a batch.
//
// Two messages are the same record when their SourceRecordHash matches; when
// that is absent (hand-built messages in tests) the source transaction ID is
// used, then the message ID. Messages with no identity at all are never
// dropped. The second return value holds the duplicates that were removed, so
// a caller can report how many records were suppressed.
func Deduplicate(msgs []Message) (unique, duplicates []Message) {
	seen := make(map[string]bool, len(msgs))
	for _, m := range msgs {
		key := m.SourceRecordHash
		if key == "" && m.SourceTxID != uuid.Nil {
			key = "tx:" + m.SourceTxID.String()
		}
		if key == "" && m.MsgID != "" {
			key = "msg:" + m.MsgID
		}
		if key == "" {
			unique = append(unique, m)
			continue
		}
		if seen[key] {
			duplicates = append(duplicates, m)
			continue
		}
		seen[key] = true
		unique = append(unique, m)
	}
	return unique, duplicates
}

// AnalyzeBatch scores a slice of messages and returns them sorted by descending
// intelligence score (most suspicious first). Ties break on MsgID for determinism.
// Duplicate ledger records are removed first (see Deduplicate).
func AnalyzeBatch(msgs []Message, ctx Context) []Scored {
	msgs, _ = Deduplicate(msgs)
	out := make([]Scored, len(msgs))
	for i, m := range msgs {
		out[i] = Analyze(m, ctx)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Message.MsgID < out[j].Message.MsgID
	})
	return out
}

// Summary aggregates the batch intelligence of a set of payments.
type Summary struct {
	Total       int     `json:"total"`
	HighRisk    int     `json:"high_risk"`    // score >= 0.5
	Structuring int     `json:"structuring"`  // AmountBelowThreshold == 1
	CrossBorder int     `json:"cross_border"` // CrossBorder == 1
	MeanScore   float64 `json:"mean_score"`   // mean intelligence score
	MaxScore    float64 `json:"max_score"`    // highest intelligence score
}

// Summarize computes aggregate intelligence metrics for a batch of scored payments.
func Summarize(scored []Scored) Summary {
	s := Summary{Total: len(scored)}
	if len(scored) == 0 {
		return s
	}
	var sum float64
	for _, sc := range scored {
		sum += sc.Score
		if sc.Score >= 0.5 {
			s.HighRisk++
		}
		if sc.Features.AmountBelowThreshold == 1.0 {
			s.Structuring++
		}
		if sc.Features.CrossBorder == 1.0 {
			s.CrossBorder++
		}
		if sc.Score > s.MaxScore {
			s.MaxScore = sc.Score
		}
	}
	s.MeanScore = clamp01(sum / float64(len(scored)))
	s.MaxScore = clamp01(s.MaxScore)
	return s
}
