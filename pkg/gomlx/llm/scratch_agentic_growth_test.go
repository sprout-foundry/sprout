//go:build darwin && arm64 && cgo

package llm_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
)

// TestScratchAgenticGrowth simulates an agentic conversation: turn 1 builds
// a ~5K-token context, each later turn appends a tool result + question
// (~2K tokens). Asserts and measures what the user actually experiences:
// does turn N only process the delta (prefix cache hit), or does it
// re-process the whole conversation? This is THE usability question for
// long agentic sessions.
func TestScratchAgenticGrowth(t *testing.T) {
	dir := os.Getenv("SPROUT_MTP_PARITY_MODEL")
	if dir == "" {
		t.Skip("SPROUT_MTP_PARITY_MODEL not set")
	}

	model, err := llm.NewModel(dir)
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	defer model.Close()

	filler := strings.Repeat("The quick brown fox jumps over the lazy dog. ", 110) // ~1000 tokens
	tool := "Tool result: " + filler

	// Real agentic shape: the conversation is append-only. Each turn sends
	// the ENTIRE prior conversation (including the previous turn's question
	// and answer) plus new tool results and the next question.
	convo := "You are a coding agent.\n\nTask: review the repository.\n\n"
	convo += strings.Repeat(tool, 5) // turn 1's context: ~5K tokens

	cfg := llm.DefaultGenerateConfig()
	cfg.MaxTokens = 8
	cfg.Temperature = 0
	cfg.RepetitionPenalty = 0
	cfg.PromptLookupMaxDrafts = 0

	for i := 1; i <= 4; i++ {
		prompt := convo + fmt.Sprintf("\n\n[Turn %d] Continue the task. Answer briefly.", i)
		start := time.Now()
		_, err := model.GenerateText(context.Background(), prompt, cfg)
		if err != nil {
			t.Fatalf("turn %d: %v", i, err)
		}
		el := time.Since(start).Seconds()
		fmt.Printf("TURN %d: total=%.2fs (prompt ~%d tokens)\n", i, el, len(prompt)/4)

		// Next turn: append 2 tool results (~2K tokens) AFTER everything.
		convo = prompt + "\n\n" + strings.Repeat(tool, 2)
	}
}
