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

// TestReplayRealGemmaPrompt replays a captured real agent prompt
// (SPROUT_REAL_PROMPT_FILE — see SPROUT_DUMP_PROMPT in
// local_provider.go's logLocalExchange to capture one) against the model
// in three configs to isolate the failing mechanism: cold full prefill
// k=0 (pure model), warm-slot delta prefill k=0 (prefix-cache path),
// warm delta k=6 (production). Guards the sliding-window regression:
// with windowing missing, ALL THREE produce runaway repetition (“ / ##)
// on prompts past sliding_window (512) tokens, cold included.
// Skips without SPROUT_MTP_PARITY_MODEL + SPROUT_REAL_PROMPT_FILE.
func TestReplayRealGemmaPrompt(t *testing.T) {
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

	model, err := llm.NewModel(dir)
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	defer model.Close()

	// The warmable prefix: everything up to the final user turn. The real
	// provider warms system messages via FormatChatPrefix; the slot that
	// matched held 7354 of 7428 tokens, so the delta is the trailing user
	// turn + <|turn>model cue. Derive the prefix by finding the last
	// "<|turn>user" marker.
	idx := strings.LastIndex(fullPrompt, "<|turn>user")
	if idx < 0 {
		t.Fatalf("no <|turn>user marker in captured prompt")
	}
	warmPrefix := fullPrompt[:idx]
	t.Logf("full prompt bytes=%d warm prefix bytes=%d delta bytes=%d",
		len(fullPrompt), len(warmPrefix), len(fullPrompt)-len(warmPrefix))

	cfg := llm.DefaultGenerateConfig()
	cfg.MaxTokens = 60
	cfg.Temperature = 0
	cfg.RepetitionPenalty = 0
	cfg.PromptLookupMaxDrafts = 0

	cold, err := model.GenerateText(context.Background(), fullPrompt, cfg)
	if err != nil {
		t.Fatalf("cold generate: %v", err)
	}
	t.Logf("COLD k=0: %.160q", cold)

	if err := model.WarmSystemPrefix(warmPrefix); err != nil {
		t.Fatalf("warm: %v", err)
	}
	warm0, err := model.GenerateText(context.Background(), fullPrompt, cfg)
	if err != nil {
		t.Fatalf("warm k=0 generate: %v", err)
	}
	t.Logf("WARM k=0: %.160q", warm0)

	cfg.PromptLookupMaxDrafts = 6
	if err := model.WarmSystemPrefix(warmPrefix); err != nil {
		t.Fatalf("warm (k6): %v", err)
	}
	warm6, err := model.GenerateText(context.Background(), fullPrompt, cfg)
	if err != nil {
		t.Fatalf("warm k=6 generate: %v", err)
	}
	t.Logf("WARM k=6: %.160q", warm6)

	garbage := func(s string) bool {
		trimmed := strings.TrimSpace(s)
		if len(trimmed) < 10 {
			return true
		}
		// The observed failure mode is runaway repetition: hundreds of
		// near-empty lines cycling between 2-3 distinct values (`` , ##).
		// Coherent output — prose or a tool call — is a handful of lines
		// with mostly distinct content.
		runs := strings.Split(trimmed, "\n")
		distinct := map[string]bool{}
		for _, r := range runs {
			distinct[strings.TrimSpace(r)] = true
		}
		return len(runs) > 20 && len(distinct) <= 3
	}

	for name, out := range map[string]string{"cold k=0": cold, "warm k=0": warm0, "warm k=6": warm6} {
		if garbage(out) {
			t.Errorf("%s produced degenerate output: %.120q", name, out)
		}
	}
	if cold != warm0 {
		t.Logf("NOTE: cold vs warm k=0 differ:\n  cold: %.120q\n  warm: %.120q", cold, warm0)
	}
}
