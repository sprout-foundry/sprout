//go:build darwin && arm64 && cgo && mlx

package llm

import (
	"context"
	"fmt"
	"log"
	"runtime"
	"sync"

	"github.com/sprout-foundry/sprout/pkg/gomlx/mlx"
)

// Model is a local LLM running on the Apple Silicon GPU via MLX. It drives
// the generation loop (prefill → decode) and delegates the forward pass to
// the Architecture implementation.
type Model struct {
	cfg         ModelConfig
	stream      *mlx.Stream
	arch        Architecture
	tokenizer   *Tokenizer
	cache       *KVCache
	mu          sync.Mutex
	closed      bool
}

// NewModel creates a Model from a HuggingFace model directory containing
// config.json, model.safetensors, and tokenizer.json. The architecture is
// auto-detected from config.json.
func NewModel(modelDir string) (*Model, error) {
	if !mlx.Available() {
		return nil, fmt.Errorf("llm: MLX GPU not available")
	}

	cfgPath := modelDir + "/config.json"
	weightsPath := modelDir + "/model.safetensors"
	tokPath := modelDir + "/tokenizer.json"

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	arch, err := createArchitecture(cfg)
	if err != nil {
		return nil, err
	}

	tok, err := LoadTokenizer(tokPath)
	if err != nil {
		return nil, fmt.Errorf("load tokenizer: %w", err)
	}

	// Weights are loaded using the default stream; actual inference creates
	// a fresh GPU stream on the calling thread (MLX streams are thread-local).
	loadStream, err := mlx.DefaultStream()
	if err != nil {
		return nil, fmt.Errorf("create load stream: %w", err)
	}

	if err := arch.InitWeights(weightsPath, loadStream); err != nil {
		loadStream.Free()
		return nil, fmt.Errorf("load weights: %w", err)
	}

	m := &Model{
		cfg:       cfg,
		stream:    nil, // created per Generate call on the inference thread
		arch:      arch,
		tokenizer: tok,
	}

	log.Printf("llm: %s loaded on GPU", cfg)
	return m, nil
}

// NewModelFromFiles creates a Model from explicit file paths. This gives
// callers flexibility for non-standard layouts. modelPath is the safetensors
// file, configPath is config.json, tokenizerPath is tokenizer.json.
func NewModelFromFiles(modelPath, configPath, tokenizerPath string) (*Model, error) {
	if !mlx.Available() {
		return nil, fmt.Errorf("llm: MLX GPU not available")
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	arch, err := createArchitecture(cfg)
	if err != nil {
		return nil, err
	}

	tok, err := LoadTokenizer(tokenizerPath)
	if err != nil {
		return nil, fmt.Errorf("load tokenizer: %w", err)
	}

	loadStream, err := mlx.DefaultStream()
	if err != nil {
		return nil, fmt.Errorf("create load stream: %w", err)
	}

	if err := arch.InitWeights(modelPath, loadStream); err != nil {
		loadStream.Free()
		return nil, fmt.Errorf("load weights: %w", err)
	}

	m := &Model{
		cfg:       cfg,
		arch:      arch,
		tokenizer: tok,
	}

	log.Printf("llm: %s loaded on GPU", cfg)
	return m, nil
}

// GenerateConfig controls text generation behavior.
type GenerateConfig struct {
	MaxTokens         int
	Temperature       float32
	TopP              float32
	TopK              int
	RepetitionPenalty float32
	// ThinkingTokens determines whether to include <think>...</think> blocks
	// in the output. When false (default), thinking tokens are filtered.
	ThinkingTokens bool
}

// DefaultGenerateConfig returns sensible defaults for concise generation.
func DefaultGenerateConfig() GenerateConfig {
	return GenerateConfig{
		MaxTokens:         512,
		Temperature:       0.6,
		TopP:              0.95,
		TopK:              20,
		RepetitionPenalty: 1.1,
	}
}

// Generate runs the autoregressive generation loop. It calls onToken for each
// generated token ID (after filtering thinking tokens if applicable).
// Returns when EOS is produced, maxTokens is reached, or context is cancelled.
func (m *Model) Generate(ctx context.Context, prompt string, genCfg GenerateConfig, onToken func(tokenID int)) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return fmt.Errorf("llm: model is closed")
	}

	tokenIDs := m.tokenizer.Encode(prompt)
	if len(tokenIDs) == 0 {
		return fmt.Errorf("llm: empty prompt after tokenization")
	}

	if m.cfg.BOSTokenID > 0 {
		tokenIDs = append([]int{m.cfg.BOSTokenID}, tokenIDs...)
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	s, err := mlx.DefaultGPUStream()
	if err != nil {
		return fmt.Errorf("get GPU stream: %w", err)
	}
	defer s.Free()
	m.stream = s
	m.arch.SetStream(s)

	// KV cache is disabled until correctness bug is fixed.
	// The full re-encode path (O(n²)) is used instead.
	// TODO: re-enable KV cache for O(n) decode.
	// cache := NewKVCache(m.cfg.NumLayers, s)
	// defer cache.Free()
	// m.cache = cache
	_ = NewKVCache // keep reference to avoid unused import

	// Prefill: process the entire prompt
	logits, err := m.arch.ForwardPrefill(m.makeIDsArray(tokenIDs), len(tokenIDs), nil)
	if err != nil {
		return fmt.Errorf("prefill: %w", err)
	}

	// Apply repetition penalty from the prompt tokens
	if genCfg.RepetitionPenalty != 0 {
		applyRepetitionPenalty(logits, tokenIDs, genCfg.RepetitionPenalty)
	}

	nextToken := sample(logits, genCfg)
	if onToken != nil && !m.shouldFilterToken(nextToken, genCfg) {
		onToken(nextToken)
	}

	// Decode loop using KV cache — each step processes only 1 new token
	generated := []int{nextToken}
	for i := 1; i < genCfg.MaxTokens; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if nextToken == m.cfg.EOSTokenID {
			break
		}

		// Build recent tokens for repetition penalty (last 64 tokens)
		recentTokens := append(generated, tokenIDs...)
		recentStart := len(recentTokens) - 64
		if recentStart < 0 {
			recentStart = 0
		}
		recent := recentTokens[recentStart:]

		// Full sequence re-encode for correctness.
		// TODO: replace with cache.ForwardDecode once KV cache bug is fixed.
		allTokens := append(append([]int{}, tokenIDs...), generated...)
		logits, err = m.arch.ForwardPrefill(m.makeIDsArray(allTokens), len(allTokens), nil)
		if err != nil {
			return fmt.Errorf("decode step %d: %w", i, err)
		}

		if genCfg.RepetitionPenalty != 0 {
			applyRepetitionPenalty(logits, recent, genCfg.RepetitionPenalty)
		}

		nextToken = sample(logits, genCfg)
		generated = append(generated, nextToken)

		if onToken != nil && !m.shouldFilterToken(nextToken, genCfg) {
			onToken(nextToken)
		}
	}

	return nil
}

// GenerateText is a convenience wrapper that collects all tokens into a string.
func (m *Model) GenerateText(ctx context.Context, prompt string, genCfg GenerateConfig) (string, error) {
	var tokenIDs []int
	err := m.Generate(ctx, prompt, genCfg, func(id int) {
		tokenIDs = append(tokenIDs, id)
	})
	if err != nil {
		return "", err
	}
	return m.tokenizer.Decode(tokenIDs), nil
}

// Close releases all GPU resources.
func (m *Model) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return nil
	}
	m.closed = true
	m.arch.FreeWeights()
	return nil
}

// makeIDsArray converts token IDs to an MLX int64 array [1, seqLen].
func (m *Model) makeIDsArray(ids []int) *mlx.Array {
	data := make([]int64, len(ids))
	for i, id := range ids {
		data[i] = int64(id)
	}
	arr, _ := mlx.NewArrayFromInt64(data, []int{1, len(ids)})
	return arr
}

// shouldFilterToken returns true if the token should be hidden from the caller.
// Used to filter thinking-mode tokens when ThinkingTokens is false.
func (m *Model) shouldFilterToken(tokenID int, genCfg GenerateConfig) bool {
	if genCfg.ThinkingTokens {
		return false
	}
	// Qwen3 thinking mode: tokens between <think> and </think> are filtered.
	// For now, we don't implement thinking token detection — this is a stub
	// that returns false. A full implementation would track state.
	return false
}
