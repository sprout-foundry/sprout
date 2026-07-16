//go:build js && wasm

// Tests for the SproutWasm.embedding* surface added by SP-100 Phase 1.
// Pure-Go helpers (constant validation, default-model fallback path)
// are testable in isolation; the js.FuncOf wrappers themselves depend
// on a live JS host and are exercised end-to-end via the WASM shell
// tests in cmd/wasm/wasm_smoke_test.go.

package main

import "testing"

func TestEmbeddingModelDefault(t *testing.T) {
	if EmbeddingModelDefault != "gemma-300m" {
		t.Errorf("EmbeddingModelDefault = %q, want %q", EmbeddingModelDefault, "gemma-300m")
	}
}

func TestBackendNames(t *testing.T) {
	if BackendStatic != "static" {
		t.Errorf("BackendStatic = %q, want %q", BackendStatic, "static")
	}
	if BackendOnnxWeb != "onnx-web" {
		t.Errorf("BackendOnnxWeb = %q, want %q", BackendOnnxWeb, "onnx-web")
	}
}

func TestEmbeddingJSFuncs_RegistersThreeEntries(t *testing.T) {
	funcs := embeddingJSFuncs()
	want := []string{"embeddingModel", "switchEmbeddingBackend", "embeddingBackendStatus"}
	for _, k := range want {
		if _, ok := funcs[k]; !ok {
			t.Errorf("embeddingJSFuncs() missing key %q", k)
		}
	}
	// No surprise extras — keep the surface minimal so future additions
	// are deliberate and reviewable.
	if got := len(funcs); got != len(want) {
		t.Errorf("embeddingJSFuncs() returned %d entries, want %d", got, len(want))
	}
}