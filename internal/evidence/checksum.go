package evidence

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// ComputeChecksum returns the SHA-256 (hex) of the canonical JSON encoding of b
// with Checksum cleared. It is stable for a given bundle value, so a persisted
// bundle and a re-computed one with identical content yield the same checksum.
func ComputeChecksum(b Bundle) (string, error) {
	b.Checksum = ""
	data, err := json.Marshal(b)
	if err != nil {
		return "", fmt.Errorf("evidence: marshal for checksum: %w", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// VerifyChecksum recomputes the checksum and reports whether it matches b.Checksum.
func VerifyChecksum(b Bundle) (bool, error) {
	got, err := ComputeChecksum(b)
	if err != nil {
		return false, err
	}
	return got == b.Checksum, nil
}
