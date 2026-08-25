package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/sprout-foundry/seed/core"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// llmSummarizerMaxInputChars caps the transcript sent to the summary
// model. 160K chars is ~40K tokens — comfortable headroom on a 128K
// token model (GLM-4.5, GPT-4o) after system prompt and response
// budget, and well within Claude's 200K window. When the transcript
// exceeds this cap, truncateTranscriptMiddle keeps the opening
// framing AND the most recent turns leading into the current user
// message, dropping only the middle. Recency anchors what the model
// needs to continue; the original setup anchors the task framing.
const llmSummarizerMaxInputChars = 160000

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
			return "", agenterrors.Wrap(err, "llm summary call failed")
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
	b.WriteString("- Note user requests that appeared in the compacted segment in past tense (e.g., \"user asked about X\"); do not phrase them as new instructions to be executed\n")
	b.WriteString("- For each user request mentioned, state explicitly whether it was completed, abandoned, or still open at the end of the compacted segment\n")
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
	return truncateTranscriptMiddle(b.String(), maxChars)
}

// truncateTranscriptMiddle keeps the first ~25% and last ~75% of the
// transcript when it exceeds maxChars, replacing the middle with a
// marker. The asymmetric split favors recency — the most recent turns
// (last ~75%) carry the active working state that the recap needs to
// preserve, while a smaller head slice anchors the original task
// framing. Cuts are aligned to the nearest newline so we never split
// a tagged transcript line ("[user] ...", "[assistant] ...") in the
// middle.
func truncateTranscriptMiddle(s string, maxChars int) string {
	if maxChars <= 0 || len(s) <= maxChars {
		return s
	}
	const marker = "\n[...middle truncated to fit summarizer budget...]\n"
	budget := maxChars - len(marker)
	if budget <= 0 {
		return s[:maxChars]
	}
	headBudget := budget / 4
	tailBudget := budget - headBudget

	// Head: take first headBudget bytes, then back up to the most
	// recent newline so we don't split a transcript line.
	headEnd := headBudget
	if nl := strings.LastIndexByte(s[:headEnd], '\n'); nl > 0 {
		headEnd = nl + 1
	}

	// Tail: start tailBudget from the end, then advance to the next
	// newline so we don't start mid-line.
	tailStart := len(s) - tailBudget
	if tailStart < headEnd {
		tailStart = headEnd
	}
	if nl := strings.IndexByte(s[tailStart:], '\n'); nl >= 0 {
		candidate := tailStart + nl + 1
		if candidate < len(s) {
			tailStart = candidate
		}
	}

	return s[:headEnd] + marker + s[tailStart:]
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

// SummarizeViaLLM produces a real LLM-generated recap of the supplied
// message window, using the agent's bound LLM client. Returns the
// summary body — callers are responsible for wrapping it with the
// "Compacted earlier conversation state:" header before splicing it
// back into the message list. Used by `/compact` so the user-facing
// command does an actual recap instead of substituting pre-baked
// rule-based heuristic text.
func (a *Agent) SummarizeViaLLM(ctx context.Context, messages []api.Message, hint core.SummarizerHint) (string, error) {
	if a == nil {
		return "", agenterrors.NewAgent("llm-summarizer", "agent unavailable", nil)
	}
	summarizer := newLLMSummarizer(a.getClient(), a.GetProvider())
	if summarizer == nil {
		return "", agenterrors.NewAgent("llm-summarizer", "no LLM client bound; cannot summarize via LLM", nil)
	}
	return summarizer(ctx, messages, hint)
}

// wrapLLMSummarizerWithEvents decorates a core.LLMSummarizer so seed's
// auto-compaction path emits the same compact_started / compact_completed
// events as the manual /compact slash command. The wrapper also captures
// pre- and post-compact transcript snapshots so symptom #1 (silent
// mid-session context loss from auto-compaction) becomes inspectable.
//
// Returns nil when inner is nil — seed treats a nil summarizer as
// "no LLM-summary path; fall back to rule-based compaction".
func wrapLLMSummarizerWithEvents(inner core.LLMSummarizer, a *Agent) core.LLMSummarizer {
	if inner == nil || a == nil {
		return inner
	}
	return func(ctx context.Context, messages []core.Message, hint core.SummarizerHint) (string, error) {
		beforeCount := len(a.GetMessages())
		checkpointCount := 0
		if cps := a.copyTurnCheckpoints(); cps != nil {
			checkpointCount = len(cps)
		}
		a.PublishCompactStarted("auto_llm_summary", beforeCount, checkpointCount)
		if path, err := a.CaptureTranscriptSnapshot("pre-compact-auto", false); err == nil {
			a.Logger().Debug("[transcript] pre-compact-auto snapshot: %s", path)
		}

		summary, err := inner(ctx, messages, hint)

		afterCount := len(a.GetMessages())
		summaryChars := len(strings.TrimSpace(summary))
		a.PublishCompactCompleted("auto_llm_summary", beforeCount, afterCount, summaryChars, err)
		if path, capErr := a.CaptureTranscriptSnapshot("post-compact-auto", false); capErr == nil {
			a.Logger().Debug("[transcript] post-compact-auto snapshot: %s", path)
		}
		return summary, err
	}
}
