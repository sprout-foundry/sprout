package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/sprout-foundry/seed/core"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// llmSummarizerMaxInputChars caps the conversation snippet sent to the
// summary model. The cap is proportional to message count up to this hard
// ceiling so a single oversized middle segment never sends the summary call
// itself into context overflow.
const llmSummarizerMaxInputChars = 32000

// newLLMSummarizer returns a core.LLMSummarizer bound to the supplied LLM
// client. When the client is nil, returns nil — seed treats a nil
// summarizer as "no LLM-summary path; fall back to rule-based compaction".
//
// The summarizer formats the window of messages into a compact transcript,
// asks the model for a neutral factual recap, and returns the cleaned reply
// text. Seed's structural compaction pipeline wraps that text into the
// canonical "Compacted earlier conversation state:" header before splicing
// it into history, so this function returns only the summary body.
func newLLMSummarizer(client api.ClientInterface, providerName string) core.LLMSummarizer {
	if client == nil {
		return nil
	}
	return func(ctx context.Context, messages []core.Message, hint core.SummarizerHint) (string, error) {
		if len(messages) == 0 {
			return "", nil
		}
		_ = providerName

		systemContent := buildSummarizerSystemPrompt(hint)
		userContent := buildSummarizerTranscript(messages, llmSummarizerMaxInputChars)
		if strings.TrimSpace(userContent) == "" {
			return "", nil
		}

		req := []api.Message{
			{Role: "system", Content: systemContent},
			{Role: "user", Content: userContent},
		}
		resp, err := client.SendChatRequest(ctx, req, nil, "", false)
		if err != nil {
			return "", fmt.Errorf("llm summary call failed: %w", err)
		}
		if resp == nil || len(resp.Choices) == 0 {
			return "", nil
		}
		return strings.TrimSpace(resp.Choices[0].Message.Content), nil
	}
}

// buildSummarizerSystemPrompt produces the system instruction shaped by the
// hint. DetailLevel selects framing ("brief" / "summary" / "detailed");
// MaxWords becomes a soft word cap in the prompt body.
func buildSummarizerSystemPrompt(hint core.SummarizerHint) string {
	var b strings.Builder
	b.WriteString("You are a conversation context summarizer. Summarize the following conversation segment concisely as a reference note for the AI agent continuing this session.\n\n")
	if hint.DetailLevel != "" {
		fmt.Fprintf(&b, "Detail level: %s", hint.DetailLevel)
		if hint.MaxWords > 0 {
			fmt.Fprintf(&b, " (target ~%d words)", hint.MaxWords)
		}
		b.WriteString("\n\n")
	}
	b.WriteString("Rules:\n")
	b.WriteString("- Preserve: what files were read/modified, what errors occurred, what the current state was\n")
	b.WriteString("- Explicitly preserve the latest user request that appears in the compacted segment\n")
	b.WriteString("- Explicitly state whether the work was still in progress at the end of the compacted segment\n")
	b.WriteString("- Do NOT add planning, suggestions, or \"next steps\"\n")
	b.WriteString("- Respond in English only\n")
	if hint.MaxWords > 0 {
		fmt.Fprintf(&b, "- Keep under %d words\n", hint.MaxWords)
	} else {
		b.WriteString("- Keep under 600 words\n")
	}
	b.WriteString("- Use a neutral, factual tone")
	return b.String()
}

// buildSummarizerTranscript renders the message window as a compact
// role-tagged transcript suitable as the summarizer's user-message input.
// Tool calls are reduced to their function names; tool results are
// shortened via their content header. Messages carrying the synthetic
// checkpoint marker are flagged as `[checkpoint summary]` so the model
// can treat them as pre-compressed.
func buildSummarizerTranscript(messages []core.Message, maxChars int) string {
	var b strings.Builder
	for _, msg := range messages {
		switch msg.Role {
		case "user":
			b.WriteString("[user] ")
			b.WriteString(msg.Content)
			b.WriteString("\n")
		case "assistant":
			if len(msg.ToolCalls) > 0 {
				b.WriteString("[assistant/tool_calls] ")
				b.WriteString(joinAssistantToolNames(msg.ToolCalls))
				b.WriteString("\n")
				continue
			}
			content := strings.TrimSpace(msg.Content)
			if content == "" {
				continue
			}
			if isCheckpointMessage(msg) {
				b.WriteString("[checkpoint summary] ")
			} else {
				b.WriteString("[assistant] ")
			}
			b.WriteString(content)
			b.WriteString("\n")
		case "tool":
			summary := firstLine(msg.Content)
			if summary == "" {
				continue
			}
			b.WriteString("[tool] ")
			b.WriteString(summary)
			b.WriteString("\n")
		}
	}
	out := b.String()
	if len(out) > maxChars {
		out = out[:maxChars] + "\n[...truncated...]"
	}
	return out
}

func joinAssistantToolNames(calls []core.ToolCall) string {
	if len(calls) == 0 {
		return "(no tools)"
	}
	names := make([]string, 0, len(calls))
	seen := make(map[string]struct{}, len(calls))
	for _, tc := range calls {
		name := strings.TrimSpace(tc.Function.Name)
		if name == "" {
			name = "unknown_tool"
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	return "Assistant invoked tools: " + strings.Join(names, ", ")
}

func isCheckpointMessage(msg core.Message) bool {
	if msg.Meta != nil && msg.Meta[core.MetaKeyCheckpoint] == "true" {
		return true
	}
	return strings.Contains(msg.Content, "Compacted earlier conversation state:")
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}
