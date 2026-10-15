// Package experiment — helpers.go
// Small utility functions used within the experiment package.
package experiment

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
)

// jsonMarshal is a thin wrapper around json.Marshal for use in pair.go helpers.
func jsonMarshal(v any) ([]byte, error) {
	return json.Marshal(v)
}

// sha256Hex returns the hex-encoded SHA-256 digest of b.
func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return fmt.Sprintf("%x", sum)
}
