//go:build !cgo || !(mlx || ggml)

// Package llm is the non-MLX stub. Every function returns an error so callers
// can fall back to cloud providers on unsupported platforms.
package llm

import (
	"errors"
)

var errUnavailable = errors.New("llm: not available on this platform (requires Apple Silicon + mlx build tag)")

type Model struct{}
type Tokenizer struct{}
type GenerateConfig struct {
	MaxTokens         int
	Temperature       float32
	TopP              float32
	TopK              int
	RepetitionPenalty float32
	ThinkingTokens    bool
}
type ChatMessage struct {
	Role    string
	Content string
}

func NewModel(modelDir string) (*Model, error) { return nil, errUnavailable }
func NewModelFromFiles(modelPath, configPath, tokPath string) (*Model, error) {
	return nil, errUnavailable
}
func (m *Model) Generate(ctx interface{}, prompt string, cfg GenerateConfig, onToken func(id int)) error {
	return errUnavailable
}
func (m *Model) GenerateText(ctx interface{}, prompt string, cfg GenerateConfig) (string, error) {
	return "", errUnavailable
}
func (m *Model) Close() error { return nil }
func DefaultGenerateConfig() GenerateConfig {
	return GenerateConfig{MaxTokens: 512, Temperature: 0.6, TopP: 0.95, TopK: 20, RepetitionPenalty: 1.1}
}
func LoadTokenizer(path string) (*Tokenizer, error) { return nil, errUnavailable }

// ModelMemoryGate is a no-op on stub builds (no MLX runtime to gate).
func ModelMemoryGate(modelDir string) error { return nil }

// ApplyMemoryLimits is a no-op on stub builds.
func ApplyMemoryLimits() error { return nil }

// TrimCachedMemory is a no-op on stub builds.
func TrimCachedMemory() error { return nil }
