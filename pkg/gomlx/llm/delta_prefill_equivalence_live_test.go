//go:build darwin && arm64 && cgo

package llm_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
	_ "github.com/sprout-foundry/sprout/pkg/gomlx/llm/gemma4"
)

// TestDeltaPrefillEquivalenceLiveModel exercises the actual delta-prefill
// path (warm slot + ForwardPrefillFrom) that the agent's first turn takes,
// and compares it token-for-token against a fresh cold instance's full
// prefill of the identical prompt. Also checks cold self-determinism to
// separate GPU near-tie nondeterminism from cache-path corruption.
// Skips without SPROUT_MTP_PARITY_MODEL + SPROUT_REAL_PROMPT_FILE.
func TestDeltaPrefillEquivalenceLiveModel(t *testing.T) {
	dir := os.Getenv("SPROUT_MTP_PARITY_MODEL")
	promptFile := os.Getenv("SPROUT_REAL_PROMPT_FILE")
	if dir == "" || promptFile == "" {
		t.Skip("SPROUT_MTP_PARITY_MODEL / SPROUT_REAL_PROMPT_FILE not set")
	}
	raw, err := os.ReadFile(promptFile)
	if err != nil {
		t.Fatalf("read prompt: %v", err)
	}
	fullPrompt := string(raw)
	idx := strings.LastIndex(fullPrompt, "<|turn>user")
	if idx < 0 {
		t.Fatalf("no user turn marker")
	}
	warmPrefix := fullPrompt[:idx]
	// A second full prompt sharing the warm prefix but a different user
	// turn — forces the warm instance down the delta path (its slot holds
	// only the warm prefix) while the cold instance full-prefills it.
	fullPrompt2 := warmPrefix + "<|turn>user\nwhat tests exist in this repo\n<turn|>\n<|turn>model\n"

	cfg := llm.DefaultGenerateConfig()
	cfg.MaxTokens = 48
	cfg.Temperature = 0
	cfg.RepetitionPenalty = 0
	cfg.PromptLookupMaxDrafts = 0
	gen := func(m *llm.Model, prompt string) string {
		out, err := m.GenerateText(context.Background(), prompt, cfg)
		if err != nil {
			t.Fatalf("GenerateText: %v", err)
		}
		return out
	}

	coldModel, err := llm.NewModel(dir)
	if err != nil {
		t.Fatalf("cold NewModel: %v", err)
	}
	cold1 := gen(coldModel, fullPrompt2)
	cold2 := gen(coldModel, fullPrompt2)
	t.Logf("cold #1: %.140q", cold1)
	if cold1 != cold2 {
		t.Logf("NOTE: cold run not self-deterministic:\n  #1: %.140q\n  #2: %.140q", cold1, cold2)
	}
	coldModel.Close()

	warmModel, err := llm.NewModel(dir)
	if err != nil {
		t.Fatalf("warm NewModel: %v", err)
	}
	defer warmModel.Close()
	if err := warmModel.WarmSystemPrefix(warmPrefix); err != nil {
		t.Fatalf("WarmSystemPrefix: %v", err)
	}
	warmDelta := gen(warmModel, fullPrompt2)
	t.Logf("warm delta: %.140q", warmDelta)
	if warmDelta != cold1 {
		t.Errorf("DELTA vs COLD divergence:\n  delta: %.140q\n  cold:  %.140q", warmDelta, cold1)
	} else {
		t.Logf("delta == cold: %.140q", warmDelta)
	}

	// Second generation on the warm instance — the real captured prompt.
	warmDelta2 := gen(warmModel, fullPrompt)
	t.Logf("warm delta #2 (real prompt): %.140q", warmDelta2)
}
