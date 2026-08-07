//go:build darwin && arm64 && cgo && mlx

package local_llm

import (
	"context"
	"fmt"
	"log"
	"runtime"
	"sync"

	"github.com/sprout-foundry/sprout/pkg/mlx"
)

// Model is a local LLM running on the Apple Silicon GPU via MLX.
// It holds the model weights and a stream for executing operations.
// Generate runs the autoregressive loop: prefill → decode tokens one at a time.
type Model struct {
	cfg      ModelConfig
	stream   *mlx.Stream
	weights  *weights
	tokenizer *Tokenizer
	mu       sync.Mutex
	closed   bool
}

// NewModel loads the Qwen3-0.6B model from the given model and tokenizer paths.
// The model runs entirely on the local GPU — no network calls after loading.
func NewModel(modelPath, tokenizerPath string) (*Model, error) {
	if !mlx.Available() {
		return nil, fmt.Errorf("local_llm: MLX GPU not available")
	}

	cfg := Qwen3_0_6B()

	// Weights are loaded using the default stream (may be CPU or GPU);
	// actual inference creates a fresh GPU stream on the calling thread.
	stream, err := mlx.DefaultStream()
	if err != nil {
		return nil, fmt.Errorf("create load stream: %w", err)
	}

	tok, err := LoadTokenizer(tokenizerPath)
	if err != nil {
		stream.Free()
		return nil, fmt.Errorf("load tokenizer: %w", err)
	}

	w, err := loadWeights(modelPath, cfg, stream)
	if err != nil {
		stream.Free()
		return nil, fmt.Errorf("load weights: %w", err)
	}

	m := &Model{
		cfg:       cfg,
		stream:    nil, // created per Generate call on the inference thread
		weights:   w,
		tokenizer: tok,
	}

	log.Printf("local_llm: %s loaded on GPU", cfg)
	return m, nil
}

// GenerateConfig controls text generation behavior.
type GenerateConfig struct {
	MaxTokens   int
	Temperature float32
	TopP        float32
	TopK        int
}

// DefaultGenerateConfig returns sensible defaults for concise generation.
func DefaultGenerateConfig() GenerateConfig {
	return GenerateConfig{
		MaxTokens:   512,
		Temperature: 0.6,
		TopP:        0.95,
		TopK:        20,
	}
}

// Generate runs the autoregressive generation loop.
// It calls onToken for each generated token ID, allowing the caller to
// decode and display tokens as they arrive. Returns when EOS is produced
// or maxTokens is reached.
func (m *Model) Generate(ctx context.Context, prompt string, genCfg GenerateConfig, onToken func(tokenID int)) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return fmt.Errorf("local_llm: model is closed")
	}

	// Tokenize the prompt with Qwen3 chat template
	tokenIDs := m.tokenizer.Encode(prompt)
	if len(tokenIDs) == 0 {
		return fmt.Errorf("local_llm: empty prompt after tokenization")
	}

	// Add BOS token
	if m.cfg.BOSTokenID >= 0 {
		tokenIDs = append([]int{m.cfg.BOSTokenID}, tokenIDs...)
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// Create a fresh GPU stream on this OS thread — MLX streams are thread-local.
	s, err := mlx.DefaultGPUStream()
	if err != nil {
		return fmt.Errorf("get GPU stream: %w", err)
	}
	defer s.Free()
	m.stream = s

	// Prefill: process the entire prompt in one forward pass
	logits, err := m.prefill(tokenIDs, s)
	if err != nil {
		return fmt.Errorf("prefill: %w", err)
	}

	// Sample the first token from the last position's logits
	nextToken, err := sample(logits, genCfg, s)
	if err != nil {
		return fmt.Errorf("sample: %w", err)
	}

	if onToken != nil {
		onToken(nextToken)
	}

	// Decode: generate tokens one at a time
	// Each step re-processes the full sequence (no KV cache yet). This is
	// O(n²) but correct. KV caching is a future optimization.
	generated := []int{nextToken}
	for i := 1; i < genCfg.MaxTokens; i++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if nextToken == m.cfg.EOSTokenID {
			break
		}

		// Full sequence: prompt tokens + all generated tokens so far
		allTokens := append(append([]int{}, tokenIDs...), generated...)
		logits, err := m.prefill(allTokens, s)
		if err != nil {
			return fmt.Errorf("decode step %d: %w", i, err)
		}

		nextToken, err = sample(logits, genCfg, s)
		if err != nil {
			return fmt.Errorf("sample step %d: %w", i, err)
		}
		generated = append(generated, nextToken)

		if onToken != nil {
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

// prefill processes the entire prompt in one forward pass and returns the
// logits for the last position. This is the expensive step — O(seq^2) attention.
func (m *Model) prefill(tokenIDs []int, s *mlx.Stream) ([]float32, error) {
	seqLen := len(tokenIDs)
	idData := make([]int64, seqLen)
	for i, id := range tokenIDs {
		idData[i] = int64(id)
	}
	idsArr, err := mlx.NewArrayFromInt64(idData, []int{1, seqLen})
	if err != nil {
		return nil, fmt.Errorf("create ids: %w", err)
	}
	defer idsArr.Free()

	logits, err := m.forwardPrefill(idsArr, seqLen)
	if err != nil {
		return nil, err
	}
	defer logits.Free()

	if err := s.Synchronize(); err != nil {
		return nil, fmt.Errorf("synchronize: %w", err)
	}

	// Extract logits for the last position: [1, seq, vocab] → [vocab]
	// logits is [1, seqLen, vocabSize]; we need the last position.
	logitsData, err := logits.Float32Data()
	if err != nil {
		return nil, fmt.Errorf("read logits: %w", err)
	}

	vocabSize := m.cfg.VocabSize
	lastPos := (seqLen - 1) * vocabSize
	result := make([]float32, vocabSize)
	copy(result, logitsData[lastPos:lastPos+vocabSize])
	return result, nil
}

// Close releases all GPU resources.
func (m *Model) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return nil
	}
	m.closed = true
	freeWeights(m.weights)
	if m.stream != nil {
		m.stream.Free()
	}
	return nil
}
