//go:build js

package embedding

import "strings"

// NewHNSWStore on WASM falls back to JSONLFileStore.
// The HNSW library uses SIMD intrinsics not available in the browser.
// The JSONL store provides the same VectorStore interface so all calling
// code works unchanged.
func NewHNSWStore(indexPath string, modelHash string) (VectorStore, error) {
	// Use .jsonl extension to avoid creating .hnsw files that won't work on WASM.
	path := indexPath
	if strings.HasSuffix(path, ".hnsw") {
		path = path[:len(path)-5] + ".jsonl"
	}
	return NewJSONLFileStore(path, modelHash)
}
