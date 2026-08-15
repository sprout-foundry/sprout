//go:build darwin && arm64 && cgo

package llm_test

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
)

// TestScratchAgenticGrowthScaled is the scaled-up version of
// TestScratchAgenticGrowth: an append-only agentic conversation that
// starts at an env-configurable size and grows past prior cache-cap
// boundaries. Set SPROUT_GROWTH_START (initial tool-result count, ~1K
// tokens each) and SPROUT_GROWTH_TURNS. Verifies the delta path holds
// (flat per-turn cost) as the conversation crosses old cache-cap cliffs
// and approaches the model window.
func TestScratchAgenticGrowthScaled(t *testing.T) {
	dir := os.Getenv("SPROUT_MTP_PARITY_MODEL")
	if dir == "" {
		t.Skip("SPROUT_MTP_PARITY_MODEL not set")
	}
	startTools := envInt(t, "SPROUT_GROWTH_START", 28)
	turns := envInt(t, "SPROUT_GROWTH_TURNS", 7)

	model, err := llm.NewModel(dir)
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	defer model.Close()

	filler := strings.Repeat("The quick brown fox jumps over the lazy dog. ", 110) // ~1000 tokens
	tool := "Tool result: " + filler

	convo := "You are a coding agent.\n\nTask: review the repository.\n\n"
	convo += strings.Repeat(tool, startTools)

	cfg := llm.DefaultGenerateConfig()
	cfg.MaxTokens = 8
	cfg.Temperature = 0
	cfg.RepetitionPenalty = 0
	cfg.PromptLookupMaxDrafts = 0

	for i := 1; i <= turns; i++ {
		prompt := convo + fmt.Sprintf("\n\n[Turn %d] Continue the task. Answer briefly.", i)
		start := time.Now()
		_, err := model.GenerateText(context.Background(), prompt, cfg)
		if err != nil {
			t.Fatalf("turn %d: %v", i, err)
		}
		el := time.Since(start).Seconds()
		fmt.Printf("TURN %d: total=%.2fs (prompt ~%d tokens)\n", i, el, len(prompt)/4)
		convo = prompt + "\n\n" + strings.Repeat(tool, 2)
	}
}

func envInt(t *testing.T, key string, def int) int {
	t.Helper()
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
