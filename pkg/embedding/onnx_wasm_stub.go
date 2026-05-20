//go:build wasm

package embedding

import (
	"context"
	"errors"
	"os"
	"path/filepath"
)

// This file provides stubs only for the types/functions whose REAL
// implementations require CGO and therefore can't compile for js/wasm.
// Pure-Go pieces of the embedding pipeline — ModelConfig, ModelDownloader,
// GemmaTokenizer, the static provider, the JSONL store, the IndexManager —
// live in their non-tagged files and build for WASM unchanged. The minimum
// here is: ONNXRuntime, ONNXEmbeddingProvider, DefaultModelDir.
//
// The browser-side counterpart that DOES want ONNX inference uses
// webui/src/services/onnxEmbeddingProvider.ts (onnxruntime-web in JS).
// A future syscall/js bridge could route this stub's Embed calls into that
// JS provider — see Tier 2a in docs/ONNX_RUNTIME.md.

var errWASMNotSupported = errors.New("onnx: native ONNX provider not available on WASM (CGO-only); use the static provider or wire the browser-side onnxruntime-web bridge")

// DefaultModelDir mirrors the non-wasm resolver: SPROUT_MODELS_DIR env var
// takes precedence; otherwise we anchor under SPROUT_CONFIG or the user's
// home directory. On WASM this points at the IndexedDB-backed MEMFS path
// that cmd/wasm sets up.
func DefaultModelDir() string {
	if dir := os.Getenv("SPROUT_MODELS_DIR"); dir != "" {
		return dir
	}
	configDir := os.Getenv("SPROUT_CONFIG")
	if configDir == "" {
		configDir = os.Getenv("LEDIT_CONFIG")
	}
	if configDir == "" {
		home, _ := os.UserHomeDir()
		configDir = filepath.Join(home, ".config", "sprout")
	}
	return filepath.Join(configDir, "models")
}

// ─── ONNXRuntime (stub) ──────────────────────────────────────────

type ONNXRuntime struct{}

func NewONNXRuntime() (*ONNXRuntime, error) { return nil, errWASMNotSupported }
func NewONNXRuntimeWithDir(_ string) (*ONNXRuntime, error) {
	return nil, errWASMNotSupported
}
func (r *ONNXRuntime) Ready() bool  { return false }
func (r *ONNXRuntime) Close() error { return nil }

// SessionOption is referenced by callers that compile for both targets.
// On WASM it carries the same shape but Apply is a no-op.
type SessionOption struct {
	IntraOpNumThreads int
	InterOpNumThreads int
}

func (o SessionOption) Apply(_ interface{}) error { return nil }

// ─── ONNXEmbeddingProvider (stub) ────────────────────────────────

type ONNXEmbeddingProvider struct{}

func NewONNXEmbeddingProvider(
	_ context.Context,
	_ *ONNXRuntime,
	_, _ string,
	_ int,
) (*ONNXEmbeddingProvider, error) {
	return nil, errWASMNotSupported
}

func (p *ONNXEmbeddingProvider) Embed(_ context.Context, _ string) ([]float32, error) {
	return nil, errWASMNotSupported
}

func (p *ONNXEmbeddingProvider) EmbedBatch(_ context.Context, _ []string) ([][]float32, error) {
	return nil, errWASMNotSupported
}

func (p *ONNXEmbeddingProvider) EmbedWithPrefix(_ context.Context, _, _ string) ([]float32, error) {
	return nil, errWASMNotSupported
}

func (p *ONNXEmbeddingProvider) EmbedBatchWithPrefix(_ context.Context, _ []string, _ string) ([][]float32, error) {
	return nil, errWASMNotSupported
}

func (p *ONNXEmbeddingProvider) Dimensions() int    { return 0 }
func (p *ONNXEmbeddingProvider) Name() string       { return "onnx-wasm-stub" }
func (p *ONNXEmbeddingProvider) ModelHash() string  { return "" }
func (p *ONNXEmbeddingProvider) Close() error       { return nil }
func (p *ONNXEmbeddingProvider) EmbedBatchInternal(_ context.Context, _ []string) ([][]float32, error) {
	return nil, errWASMNotSupported
}
