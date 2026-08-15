//go:build !darwin || !arm64 || !cgo || !mlx

// Package local_llm implements a local LLM inference engine using Apple's MLX
// framework. This is the non-Apple/non-cgo stub — every function returns an
// error so callers can fall back to cloud providers.
package local_llm

import (
	"errors"
)

var errUnavailable = errors.New("local_llm: not available on this platform (requires Apple Silicon + mlx build tag)")

// Model is a non-functional placeholder on unsupported platforms.
type Model struct{}

// Tokenizer is a non-functional placeholder on unsupported platforms.
type Tokenizer struct{}

// GenerateConfig controls generation behavior (mirrors the cgo build).
type GenerateConfig struct {
	MaxTokens   int
	Temperature float32
	TopP        float32
	TopK        int
}

// ChatMessage mirrors the cgo build.
type ChatMessage struct {
	Role    string
	Content string
}

// NewModel returns errUnavailable on stub builds.
func NewModel(modelPath, tokenizerPath string) (*Model, error) {
	return nil, errUnavailable
}

// Generate returns errUnavailable on stub builds.
func (m *Model) Generate(ctx interface{}, prompt string, cfg GenerateConfig, onToken func(id int)) error {
	return errUnavailable
}

// GenerateText returns errUnavailable on stub builds.
func (m *Model) GenerateText(ctx interface{}, prompt string, cfg GenerateConfig) (string, error) {
	return "", errUnavailable
}

// Close is a no-op on stub builds.
func (m *Model) Close() error { return nil }

// DefaultGenerateConfig returns zero values on stub builds.
func DefaultGenerateConfig() GenerateConfig {
	return GenerateConfig{MaxTokens: 512, Temperature: 0.6, TopP: 0.95, TopK: 20}
}

// LoadTokenizer returns errUnavailable on stub builds.
func LoadTokenizer(path string) (*Tokenizer, error) {
	return nil, errUnavailable
}
