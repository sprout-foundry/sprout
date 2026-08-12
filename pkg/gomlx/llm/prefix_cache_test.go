//go:build darwin && arm64 && cgo

package llm_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
	_ "github.com/sprout-foundry/sprout/pkg/gomlx/llm/qwen3"
	_ "github.com/sprout-foundry/sprout/pkg/gomlx/llm/qwen35"
)

// prefixTestModel returns the smallest installed model dir that supports the
// prefix-cache test, or "" to skip. Prefers the fast 0.6B qwen3.
func prefixTestModel(t *testing.T) string {
	t.Helper()
	home, _ := os.UserHomeDir()
	candidates := []string{
		home + "/.cache/sprout/models/qwen3-0.6b",
		home + "/dev/llm-models/qwen3.5-0.8b-4bit",
		home + "/dev/llm-models/qwen3.5-4b-4bit",
	}
	for _, dir := range candidates {
		if _, err := os.Stat(dir); err == nil {
			return dir
		}
	}
	return ""
}

// TestPrefixCacheParity verifies that a request whose prompt shares a prefix
// with the previous request produces the same output as a cold request.
// If the prefix-cache path corrupted the KV state, the two outputs diverge.
func TestPrefixCacheParity(t *testing.T) {
	dir := prefixTestModel(t)
	if dir == "" {
		t.Skip("no local model installed")
	}
	m, err := llm.NewModel(dir)
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	defer m.Close()

	base := "<|im_start|>system\nYou are a helpful assistant.<|im_end|>\n<|im_start|>user\nSay exactly: hello world<|im_end|>\n<|im_start|>assistant\n<think>\n\n</think>\n\n"
	extra := "<|im_start|>user\nSay exactly: goodbye moon<|im_end|>\n<|im_start|>assistant\n<think>\n\n</think>\n\n"

	cfg := llm.DefaultGenerateConfig()
	cfg.MaxTokens = 40
	cfg.Temperature = 0.0
	cfg.RepetitionPenalty = 0.0

	// Cold: full prompt from scratch.
	want, err := m.GenerateText(context.Background(), base+extra, cfg)
	if err != nil {
		t.Fatalf("cold generate: %v", err)
	}
	// Warm: first request caches `base`, second shares its prefix.
	if _, err := m.GenerateText(context.Background(), base, cfg); err != nil {
		t.Fatalf("cache seed: %v", err)
	}
	got, err := m.GenerateText(context.Background(), base+extra, cfg)
	if err != nil {
		t.Fatalf("warm generate: %v", err)
	}
	if strings.TrimSpace(got) != strings.TrimSpace(want) {
		t.Fatalf("prefix-cached output mismatch:\n cold: %q\n warm: %q", want, got)
	}
}

// TestPrefixCacheSpeedsUp measures the delta-prefill win on a longer prefix.
// The warm run (delta prefill) should be meaningfully faster than the cold
// run (full prefill) when the shared history is long. Logs both timings.
func TestPrefixCacheSpeedsUp(t *testing.T) {
	dir := prefixTestModel(t)
	if dir == "" {
		t.Skip("no local model installed")
	}
	m, err := llm.NewModel(dir)
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	defer m.Close()

	// Long shared history (~160 tokens), then a short new question.
	hist := strings.Repeat("The quick brown fox jumps over the lazy dog. ", 16)
	p1 := "<|im_start|>user\n" + hist + "<|im_end|>\n<|im_start|>assistant\n<think>\n\n</think>\n\n"
	p2 := p1 + "<|im_start|>user\nWhat is the answer?<|im_end|>\n<|im_start|>assistant\n<think>\n\n</think>\n\n"

	cfg := llm.DefaultGenerateConfig()
	cfg.MaxTokens = 20
	cfg.Temperature = 0.0
	cfg.RepetitionPenalty = 0.0

	// Cold timing.
	start := time.Now()
	if _, err := m.GenerateText(context.Background(), p2, cfg); err != nil {
		t.Fatalf("cold: %v", err)
	}
	cold := time.Since(start)

	// Warm up the prefix cache with p1.
	if _, err := m.GenerateText(context.Background(), p1, cfg); err != nil {
		t.Fatalf("seed: %v", err)
	}
	start = time.Now()
	if _, err := m.GenerateText(context.Background(), p2, cfg); err != nil {
		t.Fatalf("warm: %v", err)
	}
	warm := time.Since(start)

	t.Logf("cold=%v warm=%v (delta prefill should be < cold)", cold, warm)
	if warm >= cold {
		t.Logf("WARNING: prefix cache did not speed up (cold %v >= warm %v) — check delta path", cold, warm)
	}
}
