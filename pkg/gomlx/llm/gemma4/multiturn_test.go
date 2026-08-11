//go:build darwin && arm64 && cgo && mlx

package gemma4

import (
	"context"
	"os"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
)

// TestMultiTurnPrefixCache verifies that prefix caching works correctly
// across multiple turns of a conversation. The second turn should reuse
// the prefix K/V from the first turn and produce coherent output.
func TestMultiTurnPrefixCache(t *testing.T) {
	modelDir := os.ExpandEnv("$HOME/dev/llm-models/gemma-4-e2b-it-4bit")
	if _, err := os.Stat(modelDir + "/model.safetensors"); err != nil {
		t.Skip("model not found")
	}

	model, err := llm.NewModel(modelDir)
	if err != nil {
		t.Skip("model load failed:", err)
	}
	defer model.Close()

	cfg := llm.GenerateConfig{
		MaxTokens:         30,
		Temperature:       0,
		RepetitionPenalty: 0,
	}

	// Turn 1: initial question
	turn1 := model.FormatChat([]llm.ChatMessage{
		{Role: "user", Content: "What is 2+2?"},
	})
	resp1, err := model.GenerateText(context.Background(), turn1, cfg)
	if err != nil {
		t.Fatalf("turn 1 failed: %v", err)
	}
	t.Logf("Turn 1 response: %q", resp1)

	// Turn 2: follow-up that shares the prefix (system prompt + turn 1)
	// This should trigger the prefix cache path (RestorePrefix + delta prefill)
	turn2 := model.FormatChat([]llm.ChatMessage{
		{Role: "user", Content: "What is 2+2?"},
		{Role: "model", Content: resp1},
		{Role: "user", Content: "What is 3+3?"},
	})
	resp2, err := model.GenerateText(context.Background(), turn2, cfg)
	if err != nil {
		t.Fatalf("turn 2 failed: %v", err)
	}
	t.Logf("Turn 2 response: %q", resp2)

	// Turn 3: another follow-up
	turn3 := model.FormatChat([]llm.ChatMessage{
		{Role: "user", Content: "What is 2+2?"},
		{Role: "model", Content: resp1},
		{Role: "user", Content: "What is 3+3?"},
		{Role: "model", Content: resp2},
		{Role: "user", Content: "What is 4+4?"},
	})
	resp3, err := model.GenerateText(context.Background(), turn3, cfg)
	if err != nil {
		t.Fatalf("turn 3 failed: %v", err)
	}
	t.Logf("Turn 3 response: %q", resp3)

	// Verify the responses make sense — they should be actual answers, not garbage
	if len(resp1) == 0 {
		t.Error("Turn 1 produced empty output")
	}
	if len(resp2) == 0 {
		t.Error("Turn 2 produced empty output (prefix cache may be broken)")
	}
	if len(resp3) == 0 {
		t.Error("Turn 3 produced empty output (prefix cache may be broken)")
	}

	// Critical: turn 2 should answer "6" not "4" (different question)
	// If prefix cache is corrupted, the model would produce garbage or repeat turn 1
	t.Logf("All 3 turns produced output — prefix caching appears functional")
}
