//go:build js

// Package embedding stubs for WASM (js/wasm) builds.
// ONNX Runtime cannot run in WASM (requires CGO/shared libs).
// This file provides type definitions so manager.go compiles for GOOS=js GOARCH=wasm.
package embedding

import (
	"context"
)

// ONNXRuntime is a no-op stub for WASM builds.
type ONNXRuntime struct{}

// NewONNXRuntimeWithDir returns nil in WASM builds.
func NewONNXRuntimeWithDir(modelDir string) (*ONNXRuntime, error) { return nil, nil }

// SessionOption is a no-op stub for WASM builds.
type SessionOption struct {
	IntraOpNumThreads int
	InterOpNumThreads int
}

// ONNXEmbeddingProvider is a no-op stub for WASM builds.
type ONNXEmbeddingProvider struct{}

// NewONNXEmbeddingProvider returns nil in WASM builds.
func NewONNXEmbeddingProvider(ctx context.Context, runtime *ONNXRuntime, modelPath, tokenizerPath string, dims int) (*ONNXEmbeddingProvider, error) { return nil, nil }

// Embed is unused in WASM builds.
func (p *ONNXEmbeddingProvider) Embed(ctx context.Context, text string) ([]float32, error) { return nil, nil }

// EmbedBatch is unused in WASM builds.
func (p *ONNXEmbeddingProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) { return nil, nil }

// EmbedWithPrefix is unused in WASM builds.
func (p *ONNXEmbeddingProvider) EmbedWithPrefix(ctx context.Context, text, prefix string) ([]float32, error) { return nil, nil }

// Dimensions is unused in WASM builds.
func (p *ONNXEmbeddingProvider) Dimensions() int { return 0 }

// Name is unused in WASM builds.
func (p *ONNXEmbeddingProvider) Name() string { return "" }

// ModelHash is unused in WASM builds.
func (p *ONNXEmbeddingProvider) ModelHash() string { return "" }

// Close is unused in WASM builds.
func (p *ONNXEmbeddingProvider) Close() error { return nil }
