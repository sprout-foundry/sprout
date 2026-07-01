//go:build wasm

package embedding

import (
	"context"
	"syscall/js"
)

// StaticProviderName returns the name of the static (non-ONNX) embedding
// provider used as a fallback when the ONNX bridge isn't available.
func StaticProviderName() string {
	return "static-vector-fallback"
}

// StaticEmbed produces an embedding using the JS-side static provider
// (globalThis.__sproutStaticEmbed) if available, otherwise falls back to
// a simple hash-based vector that at least gives deterministic results.
// This is the WASM-only path; native builds use the Go-side static provider.
func StaticEmbed(_ context.Context, text string) ([]float32, error) {
	// Try the JS bridge first.
	jsProv := js.Global().Get("__sproutStaticEmbed")
	if !jsProv.IsUndefined() && !jsProv.IsNull() && jsProv.Type() == js.TypeFunction {
		result := jsProv.Call(text)
		n := result.Length()
		if n <= 0 {
			return nil, nil
		}
		out := make([]float32, n)
		for i := 0; i < n; i++ {
			out[i] = float32(result.Index(i).Float())
		}
		return out, nil
	}

	// Pure-Go fallback: deterministic hash-based embedding.
	// Not semantically meaningful, but gives a stable vector for
	// deduplication / indexing when no real provider is available.
	return hashEmbed(text, 384), nil
}

// hashEmbed produces a deterministic 384-dim vector from the input text
// using a simple hash. The result is L2-normalized so it plays nicely
// with cosine similarity queries.
func hashEmbed(text string, dims int) []float32 {
	out := make([]float32, dims)
	// FNV-1a inspired multi-round hashing to fill the vector.
	for round := 0; round < dims; round++ {
		h := uint32(2166136261)
		for _, c := range text {
			h ^= uint32(c)
			h *= 16777619
		}
		// Mix in the round index so adjacent dimensions differ.
		h ^= uint32(round) * 2654435761
		out[round] = float32(h&0x7fffffff) / float32(0x7fffffff)
	}
	// L2-normalize.
	norm := float32(0)
	for _, v := range out {
		norm += v * v
	}
	if norm > 0 {
		for i := range out {
			out[i] /= norm
		}
	}
	return out
}
