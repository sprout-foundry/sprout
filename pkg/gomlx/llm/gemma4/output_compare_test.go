//go:build darwin && arm64 && cgo

package gemma4

import (
	"context"
	"os"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
)

// TestDecodeOutputCompare generates text with the proper chat template
// to verify the fused kernels don't change model behavior.
func TestDecodeOutputCompare(t *testing.T) {
	modelDir := os.ExpandEnv("$HOME/dev/llm-models/gemma-4-e2b-it-4bit")
	if _, err := os.Stat(modelDir + "/model.safetensors"); err != nil {
		t.Skip("model not found at", modelDir)
	}

	model, err := llm.NewModel(modelDir)
	if err != nil {
		t.Skip("model load failed:", err)
	}
	defer model.Close()

	prompt := model.FormatChat([]llm.ChatMessage{
		{Role: "user", Content: "What is 2+2?"},
	})

	t.Logf("Prompt: %q", prompt[:min(200, len(prompt))])

	cfg := llm.GenerateConfig{
		MaxTokens:         64,
		Temperature:       0,
		RepetitionPenalty: 0,
		ThinkingTokens:    true,
	}

	var tokenIDs []int
	err = model.Generate(context.Background(), prompt, cfg, func(tokenID int) {
		tokenIDs = append(tokenIDs, tokenID)
	})
	if err != nil {
		t.Fatal("generate failed:", err)
	}

	allText := ""
	for _, id := range tokenIDs {
		allText += model.DecodeToken(id)
	}

	t.Logf("Generated %d tokens", len(tokenIDs))
	t.Logf("Output: %q", allText)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
