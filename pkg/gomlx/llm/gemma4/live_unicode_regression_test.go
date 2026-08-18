//go:build darwin && arm64 && cgo

package gemma4

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
)

// TestGemmaLiveUnicodeRoundTrip guards the two tokenizer regressions that
// made stock Gemma models unusable, at the live-model level:
//
//  1. Decode must not run raw-Unicode vocab tokens through the GPT-2
//     byte-level decoder (é → \xe9, č → \r mojibake).
//  2. The model's EOS must resolve to <eos>/1 (or <turn|>/106 via
//     StopTokenIDs), never the Qwen default 151645 — in Gemma's vocab that
//     id is the ordinary code token "▁()=>", which truncated any
//     generation containing an arrow function.
func TestGemmaLiveUnicodeRoundTrip(t *testing.T) {
	modelDir := os.ExpandEnv("$HOME/dev/llm-models/gemma-4-e2b-it-4bit")
	if _, err := os.Stat(modelDir + "/model.safetensors"); err != nil {
		t.Skip("model not found")
	}
	model, err := llm.NewModel(modelDir)
	if err != nil {
		t.Skip("model load failed:", err)
	}
	defer model.Close()

	if got := model.Config().EOSTokenID; got != 1 {
		t.Errorf("EOSTokenID = %d, want 1 (<eos>); a Qwen-default id would truncate arrow-function code", got)
	}

	// Tokenizer-level round trip: HF parity for non-ASCII text.
	text := "héllo wörld — naïve café"
	ids := model.TokenizerEncode(text)
	var rt strings.Builder
	for _, id := range ids {
		rt.WriteString(model.DecodeToken(id))
	}
	if rt.String() != text {
		t.Errorf("tokenizer round-trip: got %q want %q", rt.String(), text)
	}

	// Live echo: model reproduces unicode in a tool-response-shaped turn.
	prompt := model.FormatChat([]llm.ChatMessage{
		{Role: "user", Content: "Repeat this exactly: héllo wörld — naïve café"},
	})
	cfg := llm.DefaultGenerateConfig()
	cfg.MaxTokens = 64
	cfg.Temperature = 0
	cfg.RepetitionPenalty = 0
	cfg.PromptLookupMaxDrafts = 0

	var sb strings.Builder
	if err := model.Generate(context.Background(), prompt, cfg, func(id int) {
		sb.WriteString(model.DecodeToken(id))
	}); err != nil {
		t.Fatalf("generate: %v", err)
	}
	out := sb.String()
	if strings.ContainsAny(out, "\x89\xef\xf6\xe9") && !strings.Contains(out, "héllo") {
		t.Errorf("mojibake in live output: %q", out)
	}
	t.Logf("live output: %q", out)
}
