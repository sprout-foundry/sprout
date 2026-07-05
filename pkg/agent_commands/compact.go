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

// CompactCommand implements the /compact slash command. It produces an
// LLM-generated recap of the messages preceding the most recent user
// turn, substitutes that recap for the recapped messages, and leaves
// the latest user turn and everything after it intact.
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
	return "Summarize prior conversation via the LLM and replace it with the recap, preserving the most recent user turn"
}

// Usage returns the detailed help text shown by `/help compact`.
func (c *CompactCommand) Usage() string {
	return strings.Join([]string{
		"/compact          LLM-summarize the earlier conversation and replace",
		"                  it with the recap, keeping the most recent user turn.",
		"",
		"Reduces context usage. The latest user message (and everything after)",
		"is always preserved. System messages and the file-change manifest are",
		"carried forward automatically.",
	}, "\n")
}

// Execute runs the compact command
func (c *CompactCommand) Execute(args []string, chatAgent *agent.Agent) error {
	if chatAgent == nil {
		return errors.New("agent not available")
	}

	messages := chatAgent.GetMessages()
	if len(messages) < 4 {
		fmt.Println("\n[info] Not enough conversation history to compact.")
		return nil
	}

	// Preserve the latest user turn and everything after it. Compact
	// the head into a single LLM-generated recap message.
	splitIdx := lastUserMessageIndex(messages)
	if splitIdx <= 0 {
		fmt.Println("\n[info] No prior user turn to summarize.")
		return nil
	}
	head := messages[:splitIdx]
	tail := messages[splitIdx:]
	if len(head) < 2 {
		fmt.Println("\n[info] Head context too small to be worth summarizing.")
		return nil
	}

	// Pre-compact snapshot for diagnostics (best-effort).
	if path, err := chatAgent.CaptureTranscriptSnapshot("pre-compact-manual", true); err == nil {
		fmt.Printf("[transcript] pre-compact snapshot: %s\n", path)
	}

	chatAgent.PublishCompactStarted("manual", len(messages), 0)

	fmt.Printf("\n[compact] Summarizing %d earlier messages via LLM...\n", len(head))

	ctx := c.getContext(chatAgent)
	hint := core.SummarizerHint{
		DetailLevel: "detailed",
		MaxWords:    600,
	}
	body, err := chatAgent.SummarizeViaLLM(ctx, head, hint)
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

	// Preserve any system messages from the head — the LLM summarizer
	// transcript builder doesn't render system role, so their content
	// would otherwise be lost in the recap.
	var preservedSystem []api.Message
	for _, m := range head {
		if m.Role == "system" {
			preservedSystem = append(preservedSystem, m)
		}
	}

	// Preserve the head's file-change manifest by appending it to the
	// summary body. The block format round-trips through
	// ExtractFileChangesFromMessages so the manifest persists across
	// successive compactions instead of being lost the moment the LLM
	// recap replaces the tool-call detail it was derived from.
	manifest := agent.ExtractFileChangesFromMessages(head)
	summaryContent := compactSummaryHeader + "\n" + body
	if block := agent.FormatFileChangesForSummary(manifest); block != "" {
		summaryContent += "\n\n" + block
	}

	summaryMsg := api.Message{
		Role:    "assistant",
		Content: summaryContent,
	}
	newMessages := make([]api.Message, 0, len(preservedSystem)+1+len(tail))
	newMessages = append(newMessages, preservedSystem...)
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
	fmt.Printf("       Summarized: %d messages\n", len(head))
	fmt.Printf("       Preserved:  %d messages (current user turn and after)\n", len(tail))
	fmt.Printf("       New total:  %d messages\n", len(newMessages))
	fmt.Printf("       Summary length: %d chars\n", len(body))
	if len(manifest) > 0 {
		fmt.Printf("       File changes carried forward: %d entries\n", len(manifest))
	}

	return nil
}

// lastUserMessageIndex returns the index of the most recent message
// with role "user", or -1 if no such message exists.
func lastUserMessageIndex(messages []api.Message) int {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return i
		}
	}
	return -1
}
