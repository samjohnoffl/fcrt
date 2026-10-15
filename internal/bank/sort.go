// Package bank — sort.go
// Provides a stable sort for transactions that references only stdlib.
package bank

import "sort"

// stableSort sorts a Transaction slice by (TxTimestamp ASC, ID ASC).
// Uses sort.SliceStable which guarantees a deterministic result when the
// less function is deterministic — which it is here (no map or pointer access).
func stableSort(txs []Transaction) {
	sort.SliceStable(txs, func(i, j int) bool {
		ti := txs[i].TxTimestamp
		tj := txs[j].TxTimestamp
		if !ti.Equal(tj) {
			return ti.Before(tj)
		}
		// Secondary sort by UUID string for a total order.
		return txs[i].ID.String() < txs[j].ID.String()
	})
}
