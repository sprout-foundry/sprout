//go:build darwin && arm64 && cgo

package llm_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
	_ "github.com/sprout-foundry/sprout/pkg/gomlx/llm/qwen3"
)

// modelPathForTest returns the model dir if the Qwen3 weights are present,
// or "" to signal the test should skip.
func modelPathForTest(t *testing.T) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	paths := []string{
		filepath.Join(home, ".cache", "sprout", "models", "qwen3-0.6b"),
		filepath.Join(home, ".local", "share", "sprout", "models", "qwen3-0.6b"),
	}
	for _, p := range paths {
		if _, err := os.Stat(filepath.Join(p, "model.safetensors")); err == nil {
			return p
		}
	}
	t.Skip("qwen3-0.6b model not downloaded; run the model fetch step first")
	return ""
}

// TestGeneration exercises a short greedy generation and checks the output is
// coherent (non-empty, no crash, contains expected keyword for a fixed prompt).
func TestGeneration(t *testing.T) {
	modelDir := modelPathForTest(t)
	model, err := llm.NewModel(modelDir)
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	defer model.Close()

	prompt := "<|im_start|>system\nYou are a helpful assistant.<|im_end|>\n<|im_start|>user\nSay exactly: hello world<|im_end|>\n<|im_start|>assistant\n<think>\n\n</think>\n\n"
	cfg := llm.DefaultGenerateConfig()
	cfg.MaxTokens = 120
	cfg.Temperature = 0.0
	cfg.RepetitionPenalty = 0.0 // GPU argmax path

	text, err := model.GenerateText(context.Background(), prompt, cfg)
	if err != nil {
		t.Fatalf("GenerateText: %v", err)
	}
	if strings.TrimSpace(text) == "" {
		t.Fatal("empty output")
	}
	if !strings.Contains(strings.ToLower(text), "hello") {
		t.Fatalf("output missing expected keyword; got: %q", text)
	}
}

// TestCacheDecodeParity verifies cache decode top-5 tokens match full re-encode.
func TestCacheDecodeParity(t *testing.T) {
	modelDir := modelPathForTest(t)
	model, err := llm.NewModel(modelDir)
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	defer model.Close()

	prompt := "<|im_start|>system\nYou are a helpful assistant.<|im_end|>\n<|im_start|>user\nWhat is 2+2?<|im_end|>\n<|im_start|>assistant\n"
	tokens := model.TokenizerEncode(prompt)
	tokens = append([]int{model.BOSID()}, tokens...)

	_, fullTop5, cacheTop5, err := model.DebugDecodeComparison(tokens, 223)
	if err != nil {
		t.Fatalf("DebugDecodeComparison: %v", err)
	}
	if len(fullTop5) != len(cacheTop5) {
		t.Fatalf("top-5 length mismatch: %v vs %v", fullTop5, cacheTop5)
	}
	for i := range fullTop5 {
		if fullTop5[i] != cacheTop5[i] {
			t.Fatalf("top-5 diverge at %d: %v vs %v", i, fullTop5, cacheTop5)
		}
	}
}
