package commands

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/sprout-foundry/seed/core"
	"github.com/sprout-foundry/sprout/pkg/agent"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// compactSummaryHeader is the canonical wrapper that flags a message as
// the substitute for compacted history. Matches the header used by
// seed's structural compaction and by isCheckpointMessage so downstream
// readers (and the next /transcript snapshot) classify it correctly.
const compactSummaryHeader = "Compacted earlier conversation state:"

// Manual /compact tuning constants.
//
// These mirror seed's Structural* values where the semantics line up —
// min-message threshold and min-middle-size threshold — and intentionally
// diverge from StructuralRecentToKeep (24) because /compact is a
// USER-INITIATED conservation action. The user explicitly asked for
// context reduction when they type /compact; the auto-path runs silently
// in the background and must preserve as much recent causal chain as
// possible to avoid visible mid-session context loss. A 12-message recent
// window covers the last ~3–6 turns of agentic work, which is enough for
// continuity without defeating the purpose of the manual hammer.
//
// Rationale per constant:
//   - compactMinMessagesToCompact (30): match seed. Below 30 messages the
//     LLM call costs more than it saves.
//   - compactMinMiddleMessages (6): match seed. Middles smaller than 6
//     messages aren't worth a summary rewrite.
//   - compactRecentToKeep (12): deliberately smaller than seed's 24.
//     /compact is opt-in reduction; the user wants headroom back.
//   - compactSummaryMaxWords (1500): raise from 600 so a long-running
//     agentic session (which often has 50–150K tokens of middle
//     activity) still compresses into a useful recap rather than a
//     one-paragraph stub.
//   - manual vs. auto anchor treatment: sprout's /compact copies the
//     anchor verbatim, while seed's auto-compaction demotes the
//     anchored user/assistant messages into past-tense stubs to
//     prevent the model from re-anchoring on a stale original prompt.
//     /compact preserves the live opening task because manual
//     compaction is user-initiated: the user is returning to the same
//     task, not abandoning it.
const (
	compactMinMessagesToCompact = 30
	compactMinMiddleMessages    = 6
	compactRecentToKeep         = 12
	compactSummaryMaxWords      = 1500
)

// CompactCommand implements the /compact slash command. It splits the
// conversation into three segments — the opening task anchor
// (system + first user/assistant turn), the middle, and the recent
// causal chain (last 12 messages, adjusted to keep tool calls paired
// with their results) — and replaces the middle with a single
// LLM-generated recap while preserving anchor and recent verbatim.
type CompactCommand struct {
	ctx context.Context // SP-073: cancellation context for LLM calls
}

// SetContext sets the cancellation context for the LLM summarization call.
// When wired through the command registry, this receives the agent's
// InterruptCtx so Stop/Ctrl+C can abort in-flight compaction.
//
// Note: in concurrent multi-chat daemon mode, the registry reuses the
// same CompactCommand singleton, so c.ctx may be overwritten by a
// concurrent call. The Execute method falls back to the agent's own
// InterruptCtx() to avoid cross-chat interference.
func (c *CompactCommand) SetContext(ctx context.Context) {
	c.ctx = ctx
}

// getContext returns the stored context or the agent's interrupt context,
// falling back to context.Background() as a last resort.
func (c *CompactCommand) getContext(chatAgent *agent.Agent) context.Context {
	if chatAgent != nil {
		return chatAgent.InterruptCtx()
	}
	if c.ctx != nil {
		return c.ctx
	}
	return context.Background()
}

// Name returns the command name
func (c *CompactCommand) Name() string {
	return "compact"
}

// Description returns the command description
func (c *CompactCommand) Description() string {
	return "LLM-summarize the middle of the conversation, preserving the opening task anchor and the recent causal chain"
}

// Usage returns the detailed help text shown by `/help compact`.
func (c *CompactCommand) Usage() string {
	return strings.Join([]string{
		"/compact          LLM-summarize the middle of the conversation, keeping",
		"                  the opening task anchor and the most recent ~12",
		"                  messages verbatim. Minimum 30 total messages.",
		"",
		"Reduces context usage. The original task framing (system prompt +",
		"first user/assistant turn) and the recent causal chain (last ~12",
		"messages, adjusted to keep tool calls paired with their results)",
		"are preserved. Only the middle is summarized. File changes from the",
		"compacted middle are carried forward in the recap.",
	}, "\n")
}

// Execute runs the compact command
func (c *CompactCommand) Execute(args []string, chatAgent *agent.Agent) error {
	if chatAgent == nil {
		return errors.New("agent not available")
	}

	messages := chatAgent.GetMessages()
	if len(messages) < compactMinMessagesToCompact {
		fmt.Printf("\n[info] Need at least %d messages to compact (have %d).\n",
			compactMinMessagesToCompact, len(messages))
		return nil
	}

	// Mirror seed's CompactWithLLMSummary boundary logic: keep the
	// opening anchor (system + first user/assistant turn) and the last
	// compactRecentToKeep messages intact; summarize only the middle.
	anchorEnd := compactAnchorEnd(messages)
	recentStart := len(messages) - compactRecentToKeep
	if recentStart <= anchorEnd {
		fmt.Println("\n[info] Not enough distinct history beyond anchor + recent window to compact.")
		return nil
	}
	recentStart = adjustRecentBoundary(messages, recentStart, anchorEnd)
	if recentStart-anchorEnd < compactMinMiddleMessages {
		fmt.Println("\n[info] Middle segment too small to be worth summarizing.")
		return nil
	}

	anchor := messages[:anchorEnd]
	middle := messages[anchorEnd:recentStart]
	tail := messages[recentStart:]

	// Pre-compact snapshot for diagnostics (best-effort).
	if path, err := chatAgent.CaptureTranscriptSnapshot("pre-compact-manual", true); err == nil {
		fmt.Printf("[transcript] pre-compact snapshot: %s\n", path)
	}

	chatAgent.PublishCompactStarted("manual", len(messages), 0)

	fmt.Printf("\n[compact] Summarizing %d middle messages via LLM...\n", len(middle))

	ctx := c.getContext(chatAgent)
	hint := core.SummarizerHint{
		DetailLevel: "detailed",
		MaxWords:    compactSummaryMaxWords,
	}
	body, err := chatAgent.SummarizeViaLLM(ctx, middle, hint)
	if err != nil {
		chatAgent.PublishCompactCompleted("manual", len(messages), len(messages), 0, err)
		return fmt.Errorf("LLM summarization failed: %w", err)
	}
	body = strings.TrimSpace(body)
	if body == "" {
		emptyErr := errors.New("LLM returned an empty summary")
		chatAgent.PublishCompactCompleted("manual", len(messages), len(messages), 0, emptyErr)
		return emptyErr
	}

	// Preserve the head's file-change manifest by appending it to the
	// summary body. The block format round-trips through
	// ExtractFileChangesFromMessages so the manifest persists across
	// successive compactions instead of being lost the moment the LLM
	// recap replaces the tool-call detail it was derived from. The
	// manifest represents file changes that happened DURING the
	// compacted middle, which is exactly the window we want carried
	// forward.
	manifest := agent.ExtractFileChangesFromMessages(middle)
	summaryContent := compactSummaryHeader + "\n" + body
	if block := agent.FormatFileChangesForSummary(manifest); block != "" {
		summaryContent += "\n\n" + block
	}

	summaryMsg := api.Message{
		Role:    "assistant",
		Content: summaryContent,
	}
	// Anchor preserved verbatim (system + first user/assistant turn);
	// recent causal chain preserved verbatim (last 12 messages, tool-
	// adjusted); middle replaced by summary. This mirrors seed's
	// structural compaction splice.
	newMessages := make([]api.Message, 0, len(anchor)+1+len(tail))
	newMessages = append(newMessages, anchor...)
	newMessages = append(newMessages, summaryMsg)
	newMessages = append(newMessages, tail...)
	chatAgent.SetMessages(newMessages)

	// Drop turn checkpoints — their stored indices point into the old
	// message list and are no longer meaningful after substitution.
	chatAgent.ReplaceTurnCheckpoints(nil)

	if path, err := chatAgent.CaptureTranscriptSnapshot("post-compact-manual", false); err == nil {
		fmt.Printf("[transcript] post-compact snapshot: %s\n", path)
	}
	chatAgent.PublishCompactCompleted("manual", len(messages), len(newMessages), len(body), nil)

	fmt.Println("\n[compact] LLM-driven compaction complete:")
	fmt.Printf("       Anchor preserved: %d messages (original task framing)\n", len(anchor))
	fmt.Printf("       Middle summarized: %d messages\n", len(middle))
	fmt.Printf("       Recent preserved: %d messages (causal chain)\n", len(tail))
	fmt.Printf("       New total: %d messages\n", len(newMessages))
	fmt.Printf("       Summary length: %d chars\n", len(body))
	if len(manifest) > 0 {
		fmt.Printf("       File changes carried forward: %d entries\n", len(manifest))
	}

	return nil
}

// compactAnchorEnd returns the index past the opening task anchor —
// the system message (if any) plus the first user message and any
// immediately-following non-tool-calling assistant response.
//
// This is the sprout-side replica of seed's unexported
// compactionAnchorEnd. The anchor's contents are carried forward
// verbatim by /compact so the model retains the original task framing
// after compaction.
//
// Behavior:
//   - len == 0 → 0
//   - system at [0] → start at 1
//   - find first user message at or after anchorEnd
//   - include it, then include the following assistant reply if it
//     has no tool calls (a tool-calling assistant in the anchor would
//     need its tool results to follow; better to leave that assistant
//     in the middle than to anchor it without its results)
//   - if no user message is found, anchorEnd falls back to 1
func compactAnchorEnd(messages []api.Message) int {
	if len(messages) == 0 {
		return 0
	}

	anchorEnd := 0
	if messages[0].Role == "system" {
		anchorEnd = 1
	}

	for i := anchorEnd; i < len(messages); i++ {
		if messages[i].Role != "user" {
			continue
		}
		anchorEnd = i + 1
		if i+1 < len(messages) && messages[i+1].Role == "assistant" && len(messages[i+1].ToolCalls) == 0 {
			anchorEnd = i + 2
		}
		break
	}

	if anchorEnd == 0 {
		anchorEnd = 1
	}
	return anchorEnd
}

// adjustRecentBoundary walks recentStart backward past dangling tool
// results and assistant-with-tool-calls messages so the compaction cut
// never splits a tool call from its result. This is the sprout-side
// replica of seed's unexported adjustCompactionBoundary.
//
// The loop continues while recentStart > anchorEnd and the slot at or
// just before recentStart is part of an in-flight tool call chain:
//   - if messages[recentStart] is a tool result, step back so the
//     result stays with the assistant tool call that produced it
//     (which lives at recentStart-1 and will then be in the middle or
//     recent window together with its result)
//   - if messages[recentStart-1] is an assistant WITH tool calls and
//     would otherwise be the last "kept" message before the tail,
//     step back so the tool call sits with its results in the same
//     segment
//
// The function never crosses anchorEnd — the anchor is fixed.
func adjustRecentBoundary(messages []api.Message, recentStart, anchorEnd int) int {
	for recentStart > anchorEnd {
		if recentStart < len(messages) && messages[recentStart].Role == "tool" {
			recentStart--
			continue
		}
		if recentStart-1 >= anchorEnd && messages[recentStart-1].Role == "assistant" && len(messages[recentStart-1].ToolCalls) > 0 {
			recentStart--
			continue
		}
		break
	}
	return recentStart
}
