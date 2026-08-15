//go:build darwin && arm64 && cgo

package lfm2

import (
	"os"
	"runtime"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
	"github.com/sprout-foundry/sprout/pkg/gomlx/mlx"
	"github.com/sprout-foundry/sprout/pkg/tensor"
)

// TestLFM2Generation loads the LFM2 model and verifies it can generate
// coherent tokens for a simple prompt.
func TestLFM2Generation(t *testing.T) {
	modelDir := os.Getenv("HOME") + "/dev/llm-models/lfm2.5-2.6b-mlx/8bit"
	if _, err := os.Stat(modelDir + "/config.json"); err != nil {
		t.Skipf("model not found at %s", modelDir)
	}

	backend := tensor.DetectBackend()
	if backend == nil || !backend.Available() {
		t.Skip("no GPU backend")
	}

	runtime.LockOSThread()

	stream, err := mlx.DefaultGPUStream()
	if err != nil {
		t.Fatalf("stream: %v", err)
	}

	cfg, err := llm.LoadConfig(modelDir + "/config.json")
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	t.Logf("config: %s", cfg)

	arch, err := llm.ArchFactory(cfg.Arch)
	if err != nil {
		t.Fatalf("arch factory for %s: %v", cfg.Arch, err)
	}
	impl, err := arch(cfg, backend)
	if err != nil {
		t.Fatalf("new arch: %v", err)
	}
	impl.SetStream(stream)

	if err := impl.InitWeights(modelDir+"/model.safetensors", stream); err != nil {
		t.Fatalf("init weights: %v", err)
	}
	defer impl.FreeWeights()

	// Apply memory limits
	if err := llm.ApplyMemoryLimits(); err != nil {
		t.Logf("memory limits: %v (continuing)", err)
	}

	// Token IDs for: <|startoftext|><|im_start|>user\nWhat is 2+2?<|im_end|>\n<|im_start|>assistant\n<think>
	// from the Python tokenizer
	promptIDs := []int{124894, 124899, 5922, 207, 2992, 355, 229, 26, 19, 26, 39, 124900, 207, 124899, 63514, 207, 124901}
	seqLen := len(promptIDs)

	idData := make([]int64, seqLen)
	for i, id := range promptIDs {
		idData[i] = int64(id)
	}
	idsArr, err := backend.NewArrayFromInt64(idData, []int{1, seqLen})
	if err != nil {
		t.Fatalf("create ids: %v", err)
	}

	cache := llm.NewKVCache(cfg.NumLayers, stream, backend)

	// Prefill
	logits, err := impl.ForwardPrefill(idsArr, seqLen, cache)
	if err != nil {
		t.Fatalf("forward prefill: %v", err)
	}
	if len(logits) == 0 {
		t.Fatal("empty logits")
	}

	// Argmax of logits
	bestIdx := 0
	bestVal := logits[0]
	for i, v := range logits {
		if v > bestVal {
			bestVal = v
			bestIdx = i
		}
	}
	t.Logf("prefill argmax: token %d (logit %.4f)", bestIdx, bestVal)

	// Decode a few tokens
	greedy, ok := impl.(llm.GreedyArchitecture)
	if !ok {
		t.Fatal("not a GreedyArchitecture")
	}

	tokens := []int{bestIdx}
	for i := 0; i < 10; i++ {
		nextToken, err := greedy.ForwardDecodeArgmax(tokens[len(tokens)-1], seqLen+i, cache)
		if err != nil {
			t.Fatalf("decode step %d: %v", i, err)
		}
		tokens = append(tokens, nextToken)
	}
	t.Logf("generated tokens: %v", tokens)

	// Load tokenizer to decode
	tok, err := llm.LoadTokenizer(modelDir + "/tokenizer.json")
	if err != nil {
		t.Logf("load tokenizer: %v", err)
		return
	}
	for _, tk := range tokens {
		t.Logf("  token %d: %q", tk, tok.Decode([]int{tk}))
	}
}
