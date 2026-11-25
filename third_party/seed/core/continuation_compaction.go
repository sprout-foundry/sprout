package core

import (
	"context"
	"fmt"
	"strings"
)

// Continuation compaction: the model summarizes the conversation in a request
// that continues the live one — the same messages and tools with one
// instruction appended — so a provider's prompt cache serves the whole prefix
// and only the instruction and the summary are new work. The summary then
// replaces the middle of the conversation, and the conversation settles near
// TargetTokens: one re-read of a small context instead of repeated re-reads
// of a large one.

const (
	defaultContinuationTargetTokens  = 24_000
	defaultContinuationSummaryTokens = 6_000
	// minContinuationTailTokens keeps the latest steps verbatim even when the
	// target leaves little room beside the summary.
	minContinuationTailTokens = 4_000
)

// ContinuationCompaction turns on continuation compaction. The zero value uses
// the defaults.
type ContinuationCompaction struct {
	// TargetTokens is the conversation's estimated size after a pass:
	// the opening anchor, the summary and the verbatim recent tail.
	TargetTokens int
	// SummaryTokens is the summary's budget, asked of the model and enforced
	// (a summary well over it is refused and the loop falls back).
	SummaryTokens int
	// Reasoning, when set, is the request's reasoning setting for the summary
	// call; empty keeps the provider's.
	Reasoning string
}

func (c ContinuationCompaction) budgets() (target, summary int) {
	target, summary = c.TargetTokens, c.SummaryTokens
	if target <= 0 {
		target = defaultContinuationTargetTokens
	}
	if summary <= 0 {
		summary = defaultContinuationSummaryTokens
	}
	if summary > target/2 {
		summary = target / 2
	}
	return target, summary
}

// continuationInstruction asks for the summary. The model sees the whole
// conversation; the latest steps stay verbatim after its summary, so it is
// told to cover everything, with the current state last.
func continuationInstruction(summaryTokens int) string {
	words := summaryTokens * 3 / 4
	return fmt.Sprintf(`The conversation so far is about to be compacted to free context. Write a summary that will replace it; the most recent steps are kept verbatim after your summary.

Do not call any tools. Reply with the summary only, at most %d words, in plain sections:
- Task: what was asked, in full, including every requirement and constraint.
- Done: what has been changed so far (files, functions, commands) and why.
- Findings: facts learned that are still needed (paths, names, errors, test results, decisions).
- Open: what is unfinished, failing or undecided.
- Next: the immediate next steps.`, words)
}

// continuationTailStart is where the verbatim tail begins: the latest
// messages that fit budget, at least two, never splitting a tool call from its
// results and never reaching into the anchor.
func continuationTailStart(messages []Message, budget int, estimate func([]Message) int, anchorEnd int) int {
	start := len(messages)
	total := 0
	for i := len(messages) - 1; i >= anchorEnd; i-- {
		total += estimate([]Message{messages[i]})
		if total > budget && len(messages)-i > 2 {
			break
		}
		start = i
	}
	return adjustCompactionBoundary(messages, start, anchorEnd)
}

// compactByContinuation runs one continuation compaction pass over messages
// (the request the loop is about to send). It reports false, leaving the
// caller to fall back, when the conversation is too short, the summary call
// fails, or the summary is empty, calls a tool or overruns its budget.
func (ch *ConversationHandler) compactByContinuation(ctx context.Context, messages []Message, cc ContinuationCompaction) (StructuralCompactionResult, ChatUsage, bool) {
	tools := ch.agent.executor.GetTools()
	estimate := func(msgs []Message) int {
		return ch.agent.provider.EstimateTokens(&ChatRequest{Messages: msgs})
	}
	target, summaryBudget := cc.budgets()

	anchorEnd := compactionAnchorEnd(messages)
	tailBudget := target - summaryBudget - estimate(messages[:anchorEnd])
	if floor := min(minContinuationTailTokens, target/4); tailBudget < floor {
		tailBudget = floor
	}
	recentStart := continuationTailStart(messages, tailBudget, estimate, anchorEnd)
	if recentStart-anchorEnd < 2 {
		return StructuralCompactionResult{}, ChatUsage{}, false
	}

	// The request repeats what the loop sends, so its prefix is the one the
	// provider has cached; the instruction is the only new message.
	live := reorderToolResultsForThreading(ch.removeOrphanedToolResults(messages))
	req := &ChatRequest{
		Messages:  append(append(make([]Message, 0, len(live)+1), live...), Message{Role: "user", Content: continuationInstruction(summaryBudget)}),
		Tools:     tools,
		MaxTokens: summaryBudget * 3 / 2,
		Reasoning: cc.Reasoning,
	}
	resp, err := ch.agent.provider.Chat(ctx, req)
	if err != nil {
		ch.agent.debugLog("[~] continuation compaction: summary call failed: %v\n", err)
		return StructuralCompactionResult{}, ChatUsage{}, false
	}
	if resp == nil || len(resp.Choices) == 0 {
		return StructuralCompactionResult{}, ChatUsage{}, false
	}
	choice := resp.Choices[0].Message
	summary := strings.TrimSpace(choice.Content)
	if summary == "" || len(choice.ToolCalls) > 0 {
		ch.agent.debugLog("[~] continuation compaction: no usable summary (tool calls: %d)\n", len(choice.ToolCalls))
		return StructuralCompactionResult{}, resp.Usage, false
	}
	if n := estimate([]Message{{Role: "assistant", Content: summary}}); n > summaryBudget*3/2 {
		ch.agent.debugLog("[~] continuation compaction: summary of %d tokens overruns its %d budget\n", n, summaryBudget)
		return StructuralCompactionResult{}, resp.Usage, false
	}

	middle := messages[anchorEnd:recentStart]
	opt := StructuralOptions{ArtifactBlock: ch.agent.compactionArtifacts}
	body := wrapStructuralSummary(middle, opt.withArtifactBlock(summary, middle), "")
	compacted := spliceSummary(messages, anchorEnd, recentStart, body)
	compacted = dropConsecutiveAssistantAfter(compacted, anchorEnd)
	return StructuralCompactionResult{
		Messages:   compacted,
		Strategy:   "continuation_summary",
		Summarized: len(middle),
	}, resp.Usage, true
}
