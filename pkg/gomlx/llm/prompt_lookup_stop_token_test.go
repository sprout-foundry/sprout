//go:build darwin && arm64 && cgo

package llm_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
)

// TestPromptLookupStopTokenMidBatch is a regression test for a bug where
// prompt-lookup speculative decoding's batched accept loop never checked
// whether an accepted candidate WAS the stop token, only whether it should
// be hidden from onToken. A stop token landing mid-batch got silently
// swallowed (correctly hidden from the visible stream) but generation kept
// going past it into a hallucinated next turn — same bug class as the
// mtpOuter fix for MTP's own batch loop.
//
// This is most likely to fire in exactly the shape built here: an agentic
// tool-calling conversation, replayed via the real chat template, where an
// earlier assistant turn's "<short reply><|im_end|><|im_start|>user" tail
// repeats verbatim — precisely the pattern prompt-lookup's 3-gram matcher
// is built to find, and precisely what a real multi-tool-call session looks
// like (this reproduces what was seen live: garbled output right after a
// tool call, with literal <|im_start|>/<|endoftext|> text leaking into the
// response and eventual "blank or repetitive" abort).
//
// Not a fully deterministic repro (whether the model's own verification
// accepts a stop-token-containing candidate depends on real model
// behavior) — but a real, representative scenario, run several times with
// slightly different endings to raise the odds of exercising the path.
// Skips when SPROUT_MTP_PARITY_MODEL isn't set.
func TestPromptLookupStopTokenMidBatch(t *testing.T) {
	dir := os.Getenv("SPROUT_MTP_PARITY_MODEL")
	if dir == "" {
		t.Skip("SPROUT_MTP_PARITY_MODEL not set")
	}

	model, err := llm.NewModel(dir)
	if err != nil {
		t.Fatalf("NewModel(%q): %v", dir, err)
	}
	defer model.Close()

	questions := []string{
		"What does the config file say?",
		"What's in the file?",
		"What did the search find?",
		"Summarize the tool output.",
	}

	// Build a conversation with several completed tool-call round trips,
	// each ending in the SAME short assistant reply, so that reply's
	// "<reply><|im_end|><|im_start|>user" tail repeats verbatim multiple
	// times in the token history before the final turn.
	var msgs []llm.ChatMessage
	msgs = append(msgs, llm.ChatMessage{Role: "system", Content: "You are a terse coding assistant. Answer in one short sentence."})
	for i := 0; i < 6; i++ {
		msgs = append(msgs,
			llm.ChatMessage{Role: "user", Content: fmt.Sprintf("Tool result %d: the value is 42.", i)},
			llm.ChatMessage{Role: "assistant", Content: "Done."},
		)
	}
	msgs = append(msgs, llm.ChatMessage{Role: "user", Content: "Tool result 6: the value is 42."})

	cfg := llm.DefaultGenerateConfig()
	cfg.MaxTokens = 200 // generous budget: a runaway generation would use most/all of it
	cfg.Temperature = 0
	cfg.RepetitionPenalty = 0
	cfg.PromptLookupMaxDrafts = 4 // production default

	for _, q := range questions {
		q := q
		t.Run(q, func(t *testing.T) {
			prompt := model.FormatChat(msgs)

			var sb strings.Builder
			tokCount := 0
			if err := model.Generate(context.Background(), prompt, cfg, func(id int) {
				tokCount++
				sb.WriteString(model.DecodeToken(id))
			}); err != nil {
				t.Fatalf("Generate: %v", err)
			}
			text := sb.String()

			if strings.Contains(text, "<|im_start|>") || strings.Contains(text, "<|endoftext|>") || strings.Contains(text, "<|im_end|>") {
				t.Errorf("leaked special-token text in output for %q:\n  %q\n  (%d tokens generated)", q, text, tokCount)
			}
			if tokCount >= cfg.MaxTokens {
				t.Errorf("generation ran to the full MaxTokens budget for %q (likely swallowed stop token, never terminated naturally):\n  %q", q, text)
			}
			t.Logf("%q -> %d tokens: %q", q, tokCount, text)
		})
	}
}
