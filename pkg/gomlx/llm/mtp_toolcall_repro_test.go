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

// TestMTPToolCallRepro replays the same repeated tool-call conversation
// shape as TestPromptLookupStopTokenMidBatch, but with MTP enabled
// (PromptLookupMaxDrafts=0 so MTP is actually exercised, not shadowed by
// prompt-lookup's priority), against a model that actually has an MTP head
// (qwen3.5-4b-4bit does not; the sprout-tuned q5 variants do). Checks
// whether MTP has its own version of the stop-token-mid-batch corruption,
// or whether last session's "commit corruption" report was actually the
// prompt-lookup bug misattributed to MTP (MTP requires an MTP head; if the
// model tested then had none, MaxMTPDrafts would have been a no-op and
// generation would have silently gone through prompt-lookup the whole
// time, since PromptLookupMaxDrafts=4 is set unconditionally in
// local_provider.go).
func TestMTPToolCallRepro(t *testing.T) {
	dir := os.Getenv("SPROUT_MTP_MODEL")
	if dir == "" {
		t.Skip("SPROUT_MTP_MODEL not set")
	}
	model, err := llm.NewModel(dir)
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	defer model.Close()
	if !model.MTPAvailable() {
		t.Fatal("MTP not available on this model")
	}

	var msgs []llm.ChatMessage
	msgs = append(msgs, llm.ChatMessage{Role: "system", Content: "You are a terse coding assistant. Answer in one short sentence."})
	for i := 0; i < 6; i++ {
		msgs = append(msgs,
			llm.ChatMessage{Role: "user", Content: fmt.Sprintf("Tool result %d: the value is 42.", i)},
			llm.ChatMessage{Role: "assistant", Content: "Done."},
		)
	}

	questions := []string{
		"Tool result 6: the value is 42.",
		"What does the config file say?",
		"What's in the file?",
		"What did the search find?",
		"Summarize the tool output.",
	}

	cfg := llm.DefaultGenerateConfig()
	cfg.MaxTokens = 200
	cfg.Temperature = 0
	cfg.RepetitionPenalty = 0
	cfg.PromptLookupMaxDrafts = 0
	cfg.MaxMTPDrafts = 3

	for _, q := range questions {
		q := q
		t.Run(q, func(t *testing.T) {
			convo := append(append([]llm.ChatMessage{}, msgs...), llm.ChatMessage{Role: "user", Content: q})
			prompt := model.FormatChat(convo)

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
				t.Errorf("leaked special-token text in MTP output for %q:\n  %q\n  (%d tokens generated)", q, text, tokCount)
			}
			if tokCount >= cfg.MaxTokens {
				t.Errorf("MTP generation ran to the full MaxTokens budget for %q (likely swallowed stop token):\n  %q", q, text)
			}
			t.Logf("%q -> %d tokens: %q", q, tokCount, text)
		})
	}
}

// TestMTPCommitMessageRepro mirrors what triggered the original corruption
// report: sprout commit generating a message from a real diff, with MTP
// enabled. Uses the same message-building shape local_provider.go's
// SendChatRequest uses (system + diff prompt), not a raw string, so the
// chat-template turn boundaries are realistic.
func TestMTPCommitMessageRepro(t *testing.T) {
	dir := os.Getenv("SPROUT_MTP_MODEL")
	if dir == "" {
		t.Skip("SPROUT_MTP_MODEL not set")
	}
	model, err := llm.NewModel(dir)
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	defer model.Close()
	if !model.MTPAvailable() {
		t.Fatal("MTP not available on this model")
	}

	diff := `diff --git a/pkg/gomlx/llm/model.go b/pkg/gomlx/llm/model.go
index baf3cc42f..2d89df0b8 100644
--- a/pkg/gomlx/llm/model.go
+++ b/pkg/gomlx/llm/model.go
@@ -117,7 +117,7 @@ const minPrefixReuse = 8

-const maxPrefixLen = 4096
+func maxPrefixLenTokens() int {
+	return 32768
+}
`
	msgs := []llm.ChatMessage{
		{Role: "system", Content: "You are a git commit message generator. Given a diff, write a concise conventional-commit-style title. Return ONLY the title, nothing else."},
		{Role: "user", Content: diff},
	}
	prompt := model.FormatChat(msgs)

	cfg := llm.DefaultGenerateConfig()
	cfg.MaxTokens = 100
	cfg.Temperature = 0
	cfg.RepetitionPenalty = 0
	cfg.PromptLookupMaxDrafts = 0
	cfg.MaxMTPDrafts = 3

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
		t.Errorf("leaked special-token text in MTP commit message:\n  %q\n  (%d tokens generated)", text, tokCount)
	}
	if tokCount >= cfg.MaxTokens {
		t.Errorf("MTP commit-message generation ran to the full MaxTokens budget:\n  %q", text)
	}
	t.Logf("-> %d tokens: %q", tokCount, text)
}
