// repetition_recovery.go — the recovery-compaction escalation of the
// streamed repetition guard. When the nudge retry ALSO degenerates into a
// repetition loop, the provider performs a careful re-seed before the final
// attempt:
//
//  1. De-loop (lossless, index-stable): collapse the degenerate line runs
//     out of the assistant messages — both the VISIBLE content and the
//     reasoning_content. The existing reasoning-channel scrub already did
//     this for reasoning_content; the visible action-announcements ("Run.",
//     "Let me run it.") were never scrubbed, and they are the channel the
//     DeepSeek V4 fragmented loop actually lives in. De-looping is the
//     durable effect: it removes the self-reinforcing attractor (the model
//     re-reads its own loop back as the immediate prefix on every later
//     request of the turn) and is written back to seed's live state so the
//     CURRENT query's later iterations no longer read the loop.
//
//  2. LLM-summary the middle (lossy, "full" compaction): best-effort, only
//     when the conversation is long enough that seed's structural compaction
//     would run at all. The summary is produced with a small thinking
//     surface (low effort by default) so the summarizer call itself is
//     fast and unlikely to degenerate, and it is framed as a decision
//     (name the next concrete action) rather than a bare recap — the
//     failure is specific to committing the next tool call. This re-seeds
//     the immediate retry from a clean, shorter context; it is not written
//     back to state (the lossy splice would need a checkpoint rebase the
//     provider cannot safely compute — seed's own compaction owns that).
//
// The escalation is model-agnostic: any model that double-loops gets the
// re-seed. The model-specific levers (low-effort summarizer, decision-framed
// summary) apply where the knobs exist and degrade to no-ops where they do
// not. A failed or empty summary falls back to the de-loop-only recovery, so
// the escalation never fails the turn on a best-effort step.
package agent

import (
	"context"
	"fmt"
	"strings"

	core "github.com/sprout-foundry/seed/core"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// collapseDegenerateRunsToSingle collapses the degenerate text in a recovery
// re-seed: each run of identical short lines to a SINGLE surviving copy (the
// first occurrence), and each degenerate cycling window to its distinct
// phrases. It is the recovery de-loop: unlike collapseDegenerateLineRuns
// (which keeps MinRepetitions-1 copies, a conservative live-scrub amount), a
// recovery re-seed must remove the self-reinforcing attractor, so it strips
// the repetition down to one copy.
//
// A run only collapses when it reaches the guard's MinRepetitions threshold
// (the same trigger that flags a degenerate run, so legitimate lists below
// the threshold are left intact). Runs below the threshold pass through
// verbatim. Long, blank, and neutral lines end the current run, as in
// collapseDegenerateLineRuns; fenced code blocks are skipped, as the guard
// skips them. Returns the collapsed text and the number of lines removed.
//
// To keep exactly one copy of a confirmed run, the run's non-first copies are
// buffered (bounded by MinRepetitions-2) and flushed only if the run turns out
// to be legitimate; once the run reaches the threshold the buffer is
// discarded and every later copy is dropped.
func collapseDegenerateRunsToSingle(text string, cfg RepetitionGuardConfig) (string, int) {
	collapsed, removed := collapseIdenticalRunsToSingle(text, cfg)
	cycled, cycledRemoved := collapseCyclingLines(collapsed, cfg)
	if cycledRemoved == 0 {
		return collapsed, removed
	}
	return cycled, removed + cycledRemoved
}

// collapseIdenticalRunsToSingle collapses each run of identical short lines to
// a SINGLE surviving copy (the first occurrence).
func collapseIdenticalRunsToSingle(text string, cfg RepetitionGuardConfig) (string, int) {
	lines := strings.SplitAfter(text, "\n")
	if len(lines) < 2 {
		return text, 0
	}
	var b strings.Builder
	b.Grow(len(text))

	removed := 0
	runLine := ""
	runCount := 0
	inFence := false
	fence := ""
	// pending holds the current run's non-first copies, buffered so they can be
	// flushed (legitimate run) or discarded (degenerate run) only once the
	// run's fate is known. Bounded: a run stops buffering the moment it
	// reaches the threshold.
	pending := make([]string, 0, 4)
	flushPending := func() {
		for _, line := range pending {
			b.WriteString(line)
		}
		pending = pending[:0]
	}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if marker, ok := fenceMarker(trimmed); ok {
			if !inFence {
				inFence, fence = true, marker
			} else if marker == fence {
				inFence, fence = false, ""
			}
			flushPending()
			runLine, runCount = "", 0
			b.WriteString(line)
			continue
		}
		if inFence {
			b.WriteString(line)
			continue
		}
		normalized := normalizeLoopLine(trimmed)
		if trimmed == "" || len([]rune(trimmed)) > cfg.MaxLineChars || normalized == "" {
			// Blank and long lines are neutral: end the current run, flushing
			// any held copies (a legitimate run interrupted).
			flushPending()
			runLine, runCount = "", 0
			b.WriteString(line)
			continue
		}
		if normalized == runLine {
			runCount++
			if runCount >= cfg.MinRepetitions {
				// The run is degenerate: discard the held copies and every
				// later occurrence, keeping only the first (already written).
				removed += len(pending)
				pending = pending[:0]
				removed++
				continue
			}
			// A repeat not yet degenerate: hold it back until the run's fate
			// is known. The first copy of the run was written when the run
			// started, so this only ever buffers the 2nd..(N-1)th copies.
			pending = append(pending, line)
			continue
		}
		// A different short line: the previous run was legitimate, so flush
		// its held copies, then start a new run with this line written.
		flushPending()
		runLine, runCount = normalized, 1
		b.WriteString(line)
	}
	// End of text: flush any legitimate held run.
	flushPending()
	if removed == 0 {
		return text, 0
	}
	return b.String(), removed
}

// collapseDegenerateRunsInMessages returns a copy of msgs with the degenerate
// line runs collapsed (to a single copy) out of every assistant message's
// VISIBLE content and reasoning_content. It is index-stable: the returned
// slice has the same length and the same message identity (roles, tool calls,
// meta) as the input; only the repeated degenerate text is removed. When
// nothing degenerated it returns the input unchanged and changed=false.
//
// visibleCfg and reasoningCfg are the RESOLVED guard configs for the two
// channels: the visible threshold (the looser prose run) and the reasoning
// threshold (the tighter thinking run, which self-reinforces because the
// model replays its own reasoning on every later request).
func collapseDegenerateRunsInMessages(msgs []api.Message, visibleCfg, reasoningCfg RepetitionGuardConfig) (out []api.Message, changed bool) {
	if len(msgs) == 0 {
		return msgs, false
	}
	out = make([]api.Message, len(msgs))
	copy(out, msgs)
	changed = false
	for i := range out {
		if out[i].Role != "assistant" {
			continue
		}
		if out[i].Content != "" {
			scrubbed, removed := collapseDegenerateRunsToSingle(out[i].Content, visibleCfg)
			if removed > 0 {
				out[i].Content = scrubbed
				changed = true
			}
		}
		if out[i].ReasoningContent != "" {
			scrubbed, removed := collapseDegenerateRunsToSingle(out[i].ReasoningContent, reasoningCfg)
			if removed > 0 {
				out[i].ReasoningContent = scrubbed
				changed = true
			}
		}
	}
	if !changed {
		return msgs, false
	}
	return out, true
}

// recoveryStateHandle is the slice of the seed live-state surface the
// recovery path needs: read the current state messages and write the
// de-looped list back. seed's *core.State satisfies it; a nil handle means
// the durable de-loop is skipped (the in-flight re-seed still runs).
type recoveryStateHandle interface {
	Messages() []core.Message
	SetMessages(msgs []core.Message)
}

// buildRecoverySummarizer returns a core.LLMSummarizer bound to the supplied
// client that produces the recovery summary: the same neutral factual recap
// the manual /compact uses, framed as a decision (name the next concrete
// action) and run at a small thinking surface so the call is fast and
// unlikely to itself degenerate. When client is nil it returns nil (the
// caller treats that as "no summary path; use the de-loop only").
func buildRecoverySummarizer(client api.ClientInterface, providerName, effort string) core.LLMSummarizer {
	if client == nil {
		return nil
	}
	return func(ctx context.Context, messages []core.Message, hint core.SummarizerHint) (string, error) {
		if len(messages) == 0 {
			return "", nil
		}
		systemContent := buildRecoverySummarizerSystemPrompt(hint)
		userContent := buildSummarizerTranscript(messages, llmSummarizerMaxInputChars)
		if strings.TrimSpace(userContent) == "" {
			return "", nil
		}
		req := []api.Message{
			{Role: "system", Content: systemContent},
			{Role: "user", Content: userContent},
		}
		resp, err := client.SendChatRequest(ctx, req, nil, effort, false)
		if err != nil {
			return "", err
		}
		if resp == nil || len(resp.Choices) == 0 {
			return "", nil
		}
		return strings.TrimSpace(resp.Choices[0].Message.Content), nil
	}
}

// buildRecoverySummarizerSystemPrompt produces the recovery summary prompt:
// the neutral factual recap contract (what was read/modified, what errors,
// what is open) plus the decision framing that names the next concrete
// action. The decision framing is the model-specific lever — the V4
// degeneration is a commitment failure at the tool-call decision point, so
// the re-seed hands the model a ready-made next step at exactly that point.
func buildRecoverySummarizerSystemPrompt(hint core.SummarizerHint) string {
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
	b.WriteString("- Note user requests that appeared in the compacted segment in past tense; do not phrase them as new instructions to be executed\n")
	b.WriteString("- For each user request mentioned, state explicitly whether it was completed, abandoned, or still open at the end of the compacted segment\n")
	b.WriteString("- Do NOT add planning or suggestions beyond the single next action below\n")
	b.WriteString("- Respond in English only\n")
	if hint.MaxWords > 0 {
		fmt.Fprintf(&b, "- Keep under %d words\n", hint.MaxWords)
	} else {
		b.WriteString("- Keep under 400 words\n")
	}
	b.WriteString("- Use a neutral, factual tone\n")
	b.WriteString("\nEnd with a single line: \"Next concrete action: \" followed by the one tool call or answer the agent should emit next to make progress — not a list of options, not a re-derivation of what was already concluded.\n")
	return b.String()
}

// summarizeMiddle runs seed's structural LLM-summary compaction over msgs and
// returns the compacted slice. When the segment is too short to compact (or
// the summarizer is nil, or it returns nothing) the input is returned
// unchanged and ok is false. summarized is the number of middle messages the
// summary compressed (0 when it did not run). The caller uses the compacted
// slice only as the in-flight re-seed for the retry; it does NOT write it
// back to state.
func summarizeMiddle(ctx context.Context, msgs []api.Message, summarizer core.LLMSummarizer) (compacted []api.Message, ok bool, summarized int) {
	if summarizer == nil || len(msgs) < core.StructuralMinMessagesToCompact {
		return msgs, false, 0
	}
	r := core.CompactWithLLMSummary(ctx, msgs, summarizer)
	if r.Strategy == "none" {
		return msgs, false, 0
	}
	return r.Messages, true, r.Summarized
}
