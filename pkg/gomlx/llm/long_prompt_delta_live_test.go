//go:build darwin && arm64 && cgo

package llm_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
	_ "github.com/sprout-foundry/sprout/pkg/gomlx/llm/gemma4"
)

// TestLongPromptDeltaPrefillLiveModel reproduces the agent-path failure at
// real scale: a ~7.4K-token prompt (system prompt + tool declarations) is
// warmed as a prefix slot, then the full conversation delta-prefills 74
// tokens over the restored slot. Compares against a cold full prefill of
// the identical prompt, k=0 in both, to isolate the delta/warm-slot path
// from speculative decoding entirely. Skips without SPROUT_MTP_PARITY_MODEL.
func TestLongPromptDeltaPrefillLiveModel(t *testing.T) {
	dir := os.Getenv("SPROUT_MTP_PARITY_MODEL")
	if dir == "" {
		t.Skip("SPROUT_MTP_PARITY_MODEL not set")
	}

	model, err := llm.NewModel(dir)
	if err != nil {
		t.Fatalf("NewModel(%q): %v", dir, err)
	}
	defer model.Close()

	// Build a system prompt at agent scale: repeated tool-declaration-like
	// markdown blocks with code fences (the real prompt is ~7.3K tokens of
	// <|tool>declaration JSON + markdown; structure matters more than exact
	// content for exercising the prefill path).
	var sb strings.Builder
	sb.WriteString("You are Sprout, a software engineering agent.\n\n")
	for i := 0; i < 90; i++ {
		fmt.Fprintf(&sb, "## Tool %d\n\nshell_command_%d runs a shell command with parameters:\n\n", i, i)
		sb.WriteString("```json\n{\"command\": \"string\", \"background\": \"boolean\", \"wait_seconds\": \"integer\"}\n```\n\n")
		fmt.Fprintf(&sb, "Example usage %d:\n\n", i)
		sb.WriteString("```\nshell_command(command='ls -la', background=False)\n```\n\n")
	}
	system := sb.String()

	sysMsgs := []llm.ChatMessage{{Role: "system", Content: system}}
	warm := model.FormatChatPrefix(sysMsgs)
	user := model.FormatChatPrefix(sysMsgs) + "<|turn>user\ntell me about this codebase\n<turn|>\n" + "<|turn>model\n"

	warmTok := len(model.TokenizerEncode(warm))
	fullTok := len(model.TokenizerEncode(user))
	t.Logf("warm prefix tokens=%d full prompt tokens=%d delta=%d", warmTok, fullTok, fullTok-warmTok)

	cfg := llm.DefaultGenerateConfig()
	cfg.MaxTokens = 40
	cfg.Temperature = 0
	cfg.RepetitionPenalty = 0
	cfg.PromptLookupMaxDrafts = 0

	// Baseline: cold full prefill, no lookup.
	base, err := model.GenerateText(context.Background(), user, cfg)
	if err != nil {
		t.Fatalf("baseline GenerateText: %v", err)
	}
	t.Logf("baseline (cold, k=0): %q", base)

	// Warm the slot, then generate — delta prefill path, still k=0.
	if err := model.WarmSystemPrefix(warm); err != nil {
		t.Fatalf("WarmSystemPrefix: %v", err)
	}
	warmOut, err := model.GenerateText(context.Background(), user, cfg)
	if err != nil {
		t.Fatalf("warm GenerateText: %v", err)
	}
	t.Logf("warm slot (delta prefill, k=0): %q", warmOut)
	if strings.TrimSpace(warmOut) != strings.TrimSpace(base) {
		t.Errorf("DELTA PREFILL DIVERGENCE:\n  warm:  %q\n  base:  %q", warmOut, base)
	}

	garbage := func(s string) bool {
		trimmed := strings.TrimSpace(s)
		if len(trimmed) < 10 {
			return true
		}
		runs := strings.Split(trimmed, "\n")
		distinct := map[string]bool{}
		for _, r := range runs {
			distinct[strings.TrimSpace(r)] = true
		}
		return len(runs) > 20 && len(distinct) <= 3
	}
	if garbage(base) {
		t.Errorf("baseline produced degenerate output (sliding-window regression?): %.120q", base)
	}
}
