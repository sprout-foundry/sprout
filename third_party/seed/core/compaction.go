package core

import (
	"unicode/utf8"
)

// Compaction constants.
const (
	defaultRecentToKeep = 24
	defaultMinMessages  = 5

	// defaultCompactionTriggerFraction is the share of the context window above
	// which the chat loop runs proactive compaction before sending the next
	// request. Because token estimation is approximate (4 chars/token) and a
	// single tool result can dump tens of thousands of tokens into history,
	// the trigger must sit below 1.0 to give the loop room to react.
	defaultCompactionTriggerFraction = 0.85

	// defaultCompactionSettleMargin is how far below the trigger, as a share
	// of the context window, a proactive compaction settles by default.
	defaultCompactionSettleMargin = 0.05

	// defaultSubstitutionTargetFraction is the context share that Phase 0a
	// (iterative checkpoint substitution) targets when pressure fires.
	// Phase 0a substitutes checkpoint summaries one at a time (oldest first)
	// until the estimate falls to this fraction. It needs to sit well below
	// the compaction trigger (default 0.70) so each substitution pass buys
	// real headroom — not a marginal reduction that forces the pruner or
	// drop-pipeline to clean up on the next iteration. At 0.50, a single
	// substitution pass clears enough tokens for several turns of work.
	// The emergency drop/truncate cascade (Phase 1+) always targets
	// emergencyTargetFraction regardless of this setting.
	defaultSubstitutionTargetFraction = 0.50

	// recoveryCompactionTargetFraction is the more aggressive target used when
	// the provider has already returned a ContextOverflowError. Compaction
	// during a recovery retry trims further so the retried request has clear
	// headroom for the same response budget.
	recoveryCompactionTargetFraction = 0.70

	emergencyTargetFraction = 0.85
	toolResultMaxChars      = 1500
	toolResultHeadChars     = 750
	toolResultTailChars     = 500

	oldMsgMaxChars   = 1200
	oldMsgHeadChars  = 600
	oldMsgTailChars  = 400
	charsPerToken    = 4
	toolCallOverhead = 20
	msgOverhead      = 10
)

// CompactionResult holds the output of a compaction operation along with
// metadata about what strategy was used and how much was saved.
type CompactionResult struct {
	Messages     []Message
	Strategy     string // "none", "tool_trim", "checkpoint_drop", "truncation", or "emergency"
	TokensBefore int
	TokensAfter  int
}

// TokensSaved returns the estimated tokens saved by compaction.
func (r CompactionResult) TokensSaved() int {
	if r.TokensBefore > r.TokensAfter {
		return r.TokensBefore - r.TokensAfter
	}
	return 0
}

// MessageCountDelta returns how many messages were removed.
func (r CompactionResult) MessageCountDelta(before int) int {
	return before - len(r.Messages)
}

// CompactInputs bundles the optional context that lets Compact run the full
// progressive pipeline. When Checkpoints / Optimizer / NameFn are non-nil,
// the loss-minimizing Phase 0 substitution + masking passes run before the
// drop-based Phase 1+ pipeline. Callers that only have (messages,
// tokenLimit) should use Compact() — equivalent to CompactWith with empty
// optionals.
type CompactInputs struct {
	// Messages is the prepared message slice to compact. Required.
	Messages []Message
	// TokenLimit is the upper bound on output tokens. Compact targets a
	// fraction of this internally (emergencyTargetFraction = 0.85).
	TokenLimit int
	// Checkpoints, when non-nil, enables Phase 0a: iterative substitution
	// of the oldest checkpointed turn with its summary, one at a time,
	// stopping the moment the estimate falls to target.
	Checkpoints []TurnCheckpoint
	// MaskNameFn maps a tool-call ID to a human-readable tool name. When
	// non-nil it enables Phase 0b: iterative observation masking of the
	// oldest big consumed tool result, oldest first, after Phase 0a is
	// exhausted. Pass nil to skip Phase 0b.
	MaskNameFn func(callID string) string
	// EstimateFn, when non-nil, replaces the internal roughTokens estimator
	// used by Phase 0a/0b iteration. Pass the live provider's EstimateTokens
	// so the iterative passes use the same token math as the loop's trigger
	// decision — without this they can disagree (trigger says "compact", but
	// the internal estimate says we're already under target, so Phase 0
	// does nothing and we silently degrade to Phase 1+). Optional.
	EstimateFn func(messages []Message) int
	// SubstitutionTargetFraction overrides the Phase 0a substitution target.
	// When zero (default), Phase 0a uses the same emergencyTargetFraction as
	// the drop phases. Set to a lower fraction (e.g. 0.50) to make each
	// substitution pass buy more headroom. Only affects Phase 0a; Phase 1+
	// drops always use emergencyTargetFraction.
	SubstitutionTargetFraction float64
	// TargetTokens, when non-zero, overrides the internally-derived compaction
	// target (emergencyTargetFraction × TokenLimit) for every phase — early
	// exit, Phase 0a/0b stop conditions, and the Phase 1+ drop/truncate
	// targets. The chat loop passes its compaction trigger (with margin) so a
	// proactive pass settles BELOW the trigger; without this, the derived
	// 0.85 × TokenLimit target can sit above the trigger and the loop
	// re-fires compaction every iteration (thrash). Zero keeps the historical
	// behavior for external callers and the recovery path.
	TargetTokens int
	// RecentTailTokens, when > 0, sizes the retained tail by estimated tokens
	// instead of the fixed RecentToKeep message count. A message count cannot
	// express a context budget: 24 messages can be 2K tokens of chat or 200K
	// of tool output. Zero keeps the historical count-based boundary.
	RecentTailTokens int
}

// Compact reduces messages to fit within the token limit using the
// drop-based pipeline only (no checkpoint substitution, no observation
// masking). Equivalent to CompactWith with no Checkpoints / MaskNameFn.
//
// Kept for backward compatibility with callers that don't have access to
// the agent's checkpoint state — notably tryContextOverflowRecovery, which
// runs on the post-compaction message slice in the retry loop. New code
// should prefer CompactWith.
func Compact(messages []Message, tokenLimit int) CompactionResult {
	return CompactWith(CompactInputs{Messages: messages, TokenLimit: tokenLimit})
}

// CompactWith is the progressive compaction entry point.
//
// Pipeline (each phase exits early as soon as estimate falls to target):
//
//   - **Phase 0a — Iterative checkpoint substitution.** Replace the oldest
//     checkpointed turn with its summary, one at a time. Smallest unit of
//     information loss. Skipped if Checkpoints is empty.
//   - **Phase 0b — Iterative observation masking.** Mask the oldest big
//     consumed tool result with a placeholder, one at a time. Honors the
//     observationMaskKeepLast window. Skipped if MaskNameFn is nil.
//   - **Phase 1 — Drop oldest checkpoint summaries.** Existing pipeline.
//   - **Phase 1.5 — Drop oldest raw turns.** Existing pipeline.
//   - **Phase 2 — Emergency truncate.** Existing pipeline.
//
// Why this order: Phase 0 preserves *raw recoverable* context (the model
// can still refer back to the summary, which carries a revision pointer
// for sprout's view_history tool to recover the diff). Phase 1+ drops
// content entirely. We exhaust the recoverable steps before resorting to
// destruction.
//
// See docs/compaction.md for the full design rationale.
func CompactWith(in CompactInputs) CompactionResult {
	messages := in.Messages
	tokenLimit := in.TokenLimit
	// Use the caller-supplied estimator when provided so the entire
	// pipeline agrees with the trigger decision (see CompactInputs.EstimateFn
	// docs). When the caller doesn't supply one, fall back to the internal
	// roughTokens estimator when no provider-tokenizer is supplied.
	estimate := func(msgs []Message) int { return roughTokens(msgs) }
	if in.EstimateFn != nil {
		estimate = in.EstimateFn
	}
	tokensBefore := estimate(messages)
	targetTokens := int(float64(tokenLimit) * emergencyTargetFraction)
	// Explicit target override (chat loop passes its trigger minus margin
	// when the configured trigger sits below the derived target) wins over
	// the derived one. The guard keeps default configurations byte-identical
	// to the historical behavior — the override exists to tighten, and the
	// loop only sets it when the derived target would settle over the
	// trigger.
	if in.TargetTokens > 0 && in.TargetTokens < targetTokens {
		targetTokens = in.TargetTokens
	}

	// Early exit: nothing to compact if there are too few messages OR we're
	// already under the target. Compare against target (not tokenLimit) so
	// we don't no-op while sitting *at* the hard limit with no headroom for
	// the next iteration's response — that would let provider
	// context-overflow errors slip through.
	if len(messages) <= defaultMinMessages || tokensBefore <= targetTokens {
		return CompactionResult{
			Messages:     messages,
			Strategy:     "none",
			TokensBefore: tokensBefore,
			TokensAfter:  tokensBefore,
		}
	}
	strategies := []string{} // accumulate phases that actually fired

	// Phase 0a: iterative checkpoint substitution (loss-minimizing).
	if len(in.Checkpoints) > 0 {
		// Phase 0a can target a lower fraction than Phase 1+ so each
		// substitution pass buys substantial headroom. Default keeps the
		// historical behavior (same as emergencyTargetFraction).
		subTarget := targetTokens
		if in.SubstitutionTargetFraction > 0 && in.SubstitutionTargetFraction < 1.0 {
			subTarget = int(float64(tokenLimit) * in.SubstitutionTargetFraction)
		}
		newMsgs, applied, under := IterativelySubstituteCheckpoints(messages, in.Checkpoints, subTarget, estimate)
		if applied > 0 {
			messages = newMsgs
			strategies = append(strategies, "substitute")
		}
		if under {
			return CompactionResult{
				Messages:     messages,
				Strategy:     joinStrategies(strategies),
				TokensBefore: tokensBefore,
				TokensAfter:  estimate(messages),
			}
		}
	}

	// Phase 0b: iterative observation masking (loss-minimizing).
	if in.MaskNameFn != nil {
		newMsgs, applied, under := IterativelyMaskOldestConsumedToolResults(messages, in.MaskNameFn, targetTokens, estimate)
		if applied > 0 {
			messages = newMsgs
			strategies = append(strategies, "mask")
		}
		if under {
			return CompactionResult{
				Messages:     messages,
				Strategy:     joinStrategies(strategies),
				TokensBefore: tokensBefore,
				TokensAfter:  estimate(messages),
			}
		}
	}

	// Phase 1+: drop-based pipeline. The drop functions internally use the
	// roughTokens estimator (not the caller-supplied EstimateFn) to decide
	// how much to drop, so the inter-phase checks must also use roughTokens
	// for consistency — otherwise a wildly-different provider estimate (e.g.
	// a mock pinned to ContextSize+) would prevent the early exit even
	// after drops succeeded.
	dropped := dropOldestCheckpointSummaries(messages, in.RecentTailTokens, estimate, targetTokens)

	if roughTokens(dropped) <= targetTokens {
		strategies = append(strategies, "checkpoint_drop")
		return CompactionResult{
			Messages:     dropped,
			Strategy:     joinStrategies(strategies),
			TokensBefore: tokensBefore,
			TokensAfter:  estimate(dropped),
		}
	}

	// Phase 1.5: Try dropping oldest turns to free space.
	turnDropped := dropOldestTurns(dropped, in.RecentTailTokens, estimate, targetTokens)

	if roughTokens(turnDropped) <= targetTokens {
		strategies = append(strategies, "tool_trim")
		return CompactionResult{
			Messages:     turnDropped,
			Strategy:     joinStrategies(strategies),
			TokensBefore: tokensBefore,
			TokensAfter:  estimate(turnDropped),
		}
	}

	// Phase 2: Emergency truncation on the already-dropped messages.
	result, droppedMsgs := emergencyTruncateWithTarget(turnDropped, tokenLimit, targetTokens, in.RecentTailTokens, estimate)
	if droppedMsgs {
		strategies = append(strategies, "emergency")
	} else {
		strategies = append(strategies, "truncation")
	}
	return CompactionResult{
		Messages:     result,
		Strategy:     joinStrategies(strategies),
		TokensBefore: tokensBefore,
		TokensAfter:  estimate(result),
	}
}

// joinStrategies returns a "+"-joined strategy name like "substitute+mask"
// or "substitute+checkpoint_drop", matching the existing event-shape
// (single strategy) when only one fired.
func joinStrategies(s []string) string {
	if len(s) == 0 {
		return "none"
	}
	if len(s) == 1 {
		return s[0]
	}
	out := s[0]
	for _, x := range s[1:] {
		out += "+" + x
	}
	return out
}

// dropOldestTurns drops complete turns (user + assistant + tool chain) oldest
// first until the token count is at or below targetTokens, or there are no more
// removable turns outside the recent boundary.
//
// A turn is a user message followed by an assistant response and any associated
// tool results. If dropping complete turns isn't enough, it falls back to
// dropping individual messages oldest first.
//
// It uses recentToKeep as the recent boundary: messages within the
// last recentToKeep positions are never dropped. When recentToKeep is <= 0
// the boundary is the historical defaultRecentToKeep count; when it is > 0 it
// is a token budget for recentTailStart, which prices the tail with estimate.
//
// This function works on a copy and never mutates the original slice.
func dropOldestTurns(messages []Message, recentToKeep int, estimate func([]Message) int, targetTokens int) []Message {
	// Work on a copy so we never mutate the original.
	msgs := make([]Message, len(messages))
	copy(msgs, messages)

	// Nothing to drop with too few messages.
	if len(msgs) <= defaultMinMessages {
		return msgs
	}

	// The opening task anchor is never droppable (see
	// dropOldestCheckpointSummaries). Indices inside it never move because
	// they are never removed, so the range is computed once.
	anchorStart, anchorEnd := compactionAnchorRange(msgs)

	// Repeatedly drop the oldest complete turn outside the recent zone.
	for roughTokens(msgs) > targetTokens && len(msgs) > defaultMinMessages {
		recentStart := recentTailStart(msgs, recentToKeep, estimate, anchorEnd)
		if recentStart < 1 {
			recentStart = 1
		}

		// Find the start of the oldest complete turn outside the recent boundary.
		turnStart, turnEnd, spanningEnd := findOldestCompleteTurn(msgs, recentStart, anchorStart, anchorEnd)

		// No complete turn found — fall back to individual message dropping.
		if turnStart < 0 {
			// Try dropping individual non-system messages oldest first.
			dropIdx := findDropableMessage(msgs, recentStart, spanningEnd, anchorStart, anchorEnd)
			if dropIdx < 0 {
				break
			}
			msgs = append(msgs[:dropIdx], msgs[dropIdx+1:]...)
			continue
		}

		// Remove the entire turn.
		msgs = append(msgs[:turnStart], msgs[turnEnd:]...)
	}

	return msgs
}

// findOldestCompleteTurn finds the oldest complete turn (user + assistant + tool
// results) that starts before recentStart. Returns (startIdx, endIdx) where
// endIdx is exclusive. Returns (-1, -1) if no complete turn is found.
//
// A complete turn requires at least a user message followed by an assistant
// message. Incomplete turns (lone user messages with no response) are skipped
// and handled by the individual message fallback in dropOldestTurns.
//
// anchorStart/anchorEnd bound the opening task anchor, which is never
// droppable: a turn overlapping it is skipped.
//
// If a turn starts before recentStart but extends past it, the turn cannot be
// dropped. In that case (-1, -1) is returned and spanningEnd is set to the
// end index of the spanning turn so the caller can protect those messages.
func findOldestCompleteTurn(msgs []Message, recentStart, anchorStart, anchorEnd int) (startIdx, endIdx, spanningEnd int) {
	// Scan from index 1 (the system message at 0 is never droppable) for the
	// oldest user message.
	for i := 1; i < recentStart && i < len(msgs); i++ {
		if anchorProtects(i, anchorStart, anchorEnd) {
			continue
		}
		if msgs[i].Role != "user" {
			continue
		}
		turnEnd := identifyTurnEnd(msgs, i)
		if turnEnd > i && turnEnd <= recentStart {
			return i, turnEnd, 0
		}
		if turnEnd > i && turnEnd > recentStart {
			// Turn spans the boundary — protect it. No later turn can be
			// fully outside the recent zone either, so stop searching.
			return -1, -1, turnEnd
		}
	}
	return -1, -1, 0
}

// identifyTurnEnd walks forward from a user message at startIdx and returns
// the index past the last message belonging to the same turn. A turn ends
// when we hit the next user message or the end of the slice.
//
// The turn structure is:
//
//	user → assistant (with optional tool calls) → [tool results → assistant]* → next user
func identifyTurnEnd(msgs []Message, startIdx int) int {
	if startIdx >= len(msgs) {
		return startIdx
	}

	pos := startIdx + 1
	for pos < len(msgs) {
		msg := msgs[pos]

		// A new user message starts a new turn.
		if msg.Role == "user" {
			break
		}

		// System messages in the middle are skipped (shouldn't happen normally).
		if msg.Role == "system" {
			pos++
			continue
		}

		// assistant message — advance past it
		pos++

		// If the assistant had tool calls, advance past matching tool results.
		// We look for tool messages that follow this assistant message.
		for pos < len(msgs) && msgs[pos].Role == "tool" {
			pos++
			// After tool results, there may be another assistant message
			// (the model continues after seeing tool results).
			if pos < len(msgs) && msgs[pos].Role == "assistant" {
				pos++
				// Check if this assistant also has tool calls.
			} else {
				break
			}
		}
	}

	return pos
}

// findDropableMessage finds the first non-system, non-recent message that can
// be dropped. Returns the index or -1 if none found.
// protectedEnd is the exclusive end index of a boundary-spanning turn that
// must not be touched; 0 means no protection needed.
// anchorStart/anchorEnd bound the opening task anchor, which is never
// droppable.
//
// A tool result and the assistant message carrying its tool call are one
// indivisible unit: dropping either alone leaves an orphaned call or an
// orphaned result, which strict providers (DeepSeek, MiniMax) reject outright.
// Both roles are therefore skipped here, and -1 is returned when nothing else
// qualifies.
func findDropableMessage(msgs []Message, recentStart int, protectedEnd int, anchorStart, anchorEnd int) int {
	for i := 1; i < recentStart && i < len(msgs); i++ {
		// Don't drop messages that belong to a boundary-spanning turn.
		if protectedEnd > 0 && i < protectedEnd {
			continue
		}
		if anchorProtects(i, anchorStart, anchorEnd) {
			continue
		}
		if msgs[i].Role == "system" {
			continue
		}
		if participatesInToolPair(msgs[i]) {
			continue
		}
		return i
	}
	return -1
}

// participatesInToolPair reports whether a message is one half of a tool
// call/result pair: a tool result, or an assistant message carrying tool
// calls. Such a message is only removable together with its counterpart, so
// the single-message drop paths leave it alone.
func participatesInToolPair(m Message) bool {
	return m.Role == "tool" || (m.Role == "assistant" && len(m.ToolCalls) > 0)
}

// findDropSpan returns the [start, end) span that Phase 3 should remove. It
// prefers a single standalone message. When the only candidates before the
// tail are tool pairs, it returns one complete pair — the assistant message
// carrying the calls plus the tool results that follow it — so the pair is
// removable as a unit instead of being an obstruction that leaves history over
// budget. Returns (-1, -1) when nothing can be dropped.
func findDropSpan(msgs []Message, recentStart, anchorStart, anchorEnd int) (int, int) {
	for i := 1; i < recentStart && i < len(msgs); i++ {
		if anchorProtects(i, anchorStart, anchorEnd) || msgs[i].Role == "system" {
			continue
		}
		if participatesInToolPair(msgs[i]) {
			continue
		}
		return i, i + 1
	}
	for i := 1; i < recentStart && i < len(msgs); i++ {
		if anchorProtects(i, anchorStart, anchorEnd) || msgs[i].Role != "assistant" || len(msgs[i].ToolCalls) == 0 {
			continue
		}
		end := i + 1
		for end < len(msgs) && msgs[end].Role == "tool" {
			end++
		}
		// Only drop a pair that ends before the tail, or the drop would eat
		// into the recent causal chain.
		if end > recentStart {
			continue
		}
		return i, end
	}
	return -1, -1
}

// DropOldestTurns drops complete turns (user + assistant + tool chain) oldest
// first until the token count is at or below targetTokens.
//
// It uses defaultRecentToKeep as the recent boundary and works on a copy
// of the input slice. Falls back to individual message dropping when no
// complete turns remain.
func DropOldestTurns(messages []Message, targetTokens int) []Message {
	return dropOldestTurns(messages, 0, nil, targetTokens)
}

// DropOldestCheckpointSummaries drops checkpoint summary messages (oldest first)
// until the token count is at or below targetTokens, or there are no more
// removable checkpoint messages outside the recent boundary.
//
// It uses defaultRecentToKeep as the recent boundary: messages within the
// last defaultRecentToKeep positions are never dropped.
//
// This function works on a copy and never mutates the original slice.
func DropOldestCheckpointSummaries(messages []Message, targetTokens int) []Message {
	return dropOldestCheckpointSummaries(messages, 0, nil, targetTokens)
}

// dropOldestCheckpointSummaries is the unexported implementation that accepts
// a custom recentToKeep boundary: <= 0 means the historical
// defaultRecentToKeep count, > 0 is a token budget for recentTailStart priced
// with estimate.
// compactionAnchorRange returns the [start, end) span of the opening task
// anchor: the first genuine user message, plus the assistant reply that made no
// tool calls when it immediately follows.
//
// Synthetic summary messages are not the task statement, so they are skipped
// when locating the start — a summary spliced into history must stay droppable
// even when it sits early in the list. start == end means the history carries
// no anchor (only a system message, or nothing), and nothing is protected
// beyond the system message the phases already skip by role.
//
// The summarizers keep this same span verbatim (compactionAnchorEnd). Every
// phase that removes or truncates history must agree with them: a drop that
// eats the anchor leaves the model working from summaries that still claim to
// have kept the framing, which reads as progressive drift.
func compactionAnchorRange(messages []Message) (start, end int) {
	start = -1
	for i, m := range messages {
		if m.Role == "user" && !isCheckpointSummary(m) {
			start = i
			break
		}
	}
	if start < 0 {
		return 0, 0
	}
	end = start + 1
	if end < len(messages) && messages[end].Role == "assistant" && len(messages[end].ToolCalls) == 0 {
		end++
	}
	return start, end
}

// anchorProtects reports whether index i lies inside the opening task anchor.
func anchorProtects(i, anchorStart, anchorEnd int) bool {
	return anchorStart < anchorEnd && i >= anchorStart && i < anchorEnd
}

// minRecentTailMessages is the floor on the verbatim tail: however large the
// newest messages are, at least this many of them survive a drop. Without the
// floor a single enormous newest message could push the token-sized boundary
// to the very end, leaving the model with no verbatim causal chain to continue
// from.
const minRecentTailMessages = 4

// recentTailStart returns the index where the verbatim recent tail begins.
//
// With budget <= 0 it returns the historical message-count boundary
// (len(msgs) - defaultRecentToKeep), so callers that supply no budget stay
// byte-identical. With budget > 0 the tail is sized by estimated tokens
// instead: it walks backward from the newest message, summing the estimate of
// each message individually, and starts the tail at the first index whose
// running total exceeds budget. A token-sized tail is what makes the retained
// span meaningful — 24 messages is 2K tokens of chat and 200K of tool output,
// so a count alone cannot express "keep the most recent N tokens of context".
//
// The result is clamped so the tail never shrinks below minRecentTailMessages
// and never begins before anchorEnd: the opening task anchor is preserved
// verbatim by every phase, so a tail that reaches into it would be describing
// history the caller is not allowed to drop anyway.
//
// A token budget is also never allowed to retain LESS than the historical
// message count would. The count boundary is the caller-visible floor for a
// short history: with fewer than defaultRecentToKeep messages it protects the
// whole slice, and a budget that prices those few messages above its own
// limit must not start dropping the oldest of them — that would make a
// budgeted pass strictly more destructive than the unbudgeted one it
// replaces, on exactly the short histories where compaction has least to gain.
//
// estimate may be nil, in which case roughTokens prices the messages.
func recentTailStart(msgs []Message, budget int, estimate func([]Message) int, anchorEnd int) int {
	if estimate == nil {
		estimate = roughTokens
	}

	start := len(msgs) - defaultRecentToKeep
	if budget > 0 {
		total := 0
		start = 0
		for i := len(msgs) - 1; i >= 0; i-- {
			total += estimate([]Message{msgs[i]})
			if total > budget {
				start = i + 1
				break
			}
		}
		// A budget never retains less than the historical message count.
		if countStart := len(msgs) - defaultRecentToKeep; start > countStart {
			start = countStart
		}
	}

	// Floor: keep at least minRecentTailMessages messages.
	if floor := len(msgs) - minRecentTailMessages; start > floor {
		start = floor
	}
	// The tail never reaches back into the anchor.
	if start < anchorEnd {
		start = anchorEnd
	}
	if start < 0 {
		start = 0
	}
	return start
}

func dropOldestCheckpointSummaries(messages []Message, recentToKeep int, estimate func([]Message) int, targetTokens int) []Message {
	// Work on a copy so we never mutate the original.
	msgs := make([]Message, len(messages))
	copy(msgs, messages)

	// Nothing to drop with too few messages.
	if len(msgs) <= defaultMinMessages {
		return msgs
	}

	// The opening task anchor is never droppable. The summarizers already
	// preserve it verbatim (compactionAnchorEnd), but a drop phase that treats
	// it as ordinary old history deletes the only verbatim record of what the
	// session was asked to do — after which the model works from summaries
	// that still claim to have kept the framing. Indices inside the anchor
	// never move because they are never removed.
	anchorStart, anchorEnd := compactionAnchorRange(msgs)

	// Repeatedly drop the oldest checkpoint message outside the recent zone
	// until we're under targetTokens.
	for roughTokens(msgs) > targetTokens && len(msgs) > defaultMinMessages {
		dropIdx := -1
		recentStart := recentTailStart(msgs, recentToKeep, estimate, anchorEnd)
		if recentStart < 1 {
			recentStart = 1
		}

		// Find the oldest checkpoint message outside the recent boundary and
		// outside the anchor.
		for i := 0; i < recentStart && i < len(msgs); i++ {
			if anchorProtects(i, anchorStart, anchorEnd) {
				continue
			}
			if msgs[i].Meta != nil && msgs[i].Meta[MetaKeyCheckpoint] == "true" {
				dropIdx = i
				break
			}
		}

		// No removable checkpoint message found.
		if dropIdx < 0 {
			break
		}

		// Remove the message at dropIdx.
		msgs = append(msgs[:dropIdx], msgs[dropIdx+1:]...)
	}

	return msgs
}

// emergencyTruncate aggressively trims message content to fit the limit.
// Returns the trimmed messages and a boolean indicating whether messages
// were dropped (true = "emergency", false = "truncation" only).
func emergencyTruncate(messages []Message, tokenLimit int) ([]Message, bool) {
	targetTokens := int(float64(tokenLimit) * emergencyTargetFraction)
	return emergencyTruncateWithTarget(messages, tokenLimit, targetTokens, 0, nil)
}

// emergencyTruncateWithTarget is emergencyTruncate with an explicit target
// override (from CompactInputs.TargetTokens). It tightens every internal
// phase; the caller-supplied tokenLimit remains the outer bound.
//
// recentTail carries CompactInputs.RecentTailTokens: <= 0 keeps the historical
// defaultRecentToKeep count, > 0 sizes the protected recent zone by tokens via
// recentTailStart, priced with estimate.
func emergencyTruncateWithTarget(messages []Message, tokenLimit, targetTokens, recentTail int, estimate func([]Message) int) ([]Message, bool) {
	if targetTokens <= 0 {
		targetTokens = int(float64(tokenLimit) * emergencyTargetFraction)
	}
	currentTokens := roughTokens(messages)

	if currentTokens <= targetTokens {
		return messages, false
	}

	// Work on a copy
	trimmed := make([]Message, len(messages))
	copy(trimmed, messages)

	msgsDropped := false

	// Phase 1: Trim tool results to max 1500 chars each
	for i := range trimmed {
		if trimmed[i].Role == "tool" && utf8.RuneCountInString(trimmed[i].Content) > toolResultMaxChars {
			trimmed[i].Content = truncateHeadTail(trimmed[i].Content, toolResultHeadChars, toolResultTailChars)
		}
	}

	currentTokens = roughTokens(trimmed)
	if currentTokens <= targetTokens {
		return trimmed, false
	}

	// The opening task anchor is never truncated or dropped: it is the only
	// verbatim record of what the session was asked to do. Without this the
	// emergency path trimmed the task statement to a head/tail stub and then
	// removed it outright (Phase 3 always started at index 1).
	anchorStart, anchorEnd := compactionAnchorRange(trimmed)

	// Phase 2: Trim older user/assistant messages
	recentStart := recentTailStart(trimmed, recentTail, estimate, anchorEnd)
	if recentStart < 1 {
		recentStart = 1
	}

	for i := range trimmed {
		if i >= recentStart || anchorProtects(i, anchorStart, anchorEnd) ||
			trimmed[i].Role == "system" || trimmed[i].Content == "" {
			continue
		}
		if utf8.RuneCountInString(trimmed[i].Content) > oldMsgMaxChars {
			trimmed[i].Content = truncateOldContentHeadTail(trimmed[i].Content)
		}
	}

	currentTokens = roughTokens(trimmed)
	if currentTokens <= targetTokens {
		return trimmed, msgsDropped
	}

	// Phase 3: Drop oldest non-system, non-recent, non-anchor messages until
	// under limit. A tool call and its results are one indivisible unit: half
	// of a pair is never dropped, but the pair as a whole is droppable.
	for len(trimmed) > defaultMinMessages+1 && roughTokens(trimmed) > targetTokens {
		// Recompute per iteration: a token-budgeted boundary moves by the
		// weight of what was removed, not by one message.
		recentStart = recentTailStart(trimmed, recentTail, estimate, anchorEnd)
		if recentStart < 1 {
			recentStart = 1
		}

		start, end := findDropSpan(trimmed, recentStart, anchorStart, anchorEnd)
		// Nothing droppable left: stop rather than spin on the same slice.
		if start < 0 || start >= recentStart {
			break
		}
		trimmed = append(trimmed[:start], trimmed[end:]...)
		msgsDropped = true
	}

	return trimmed, msgsDropped
}

// roughTokens gives a rough token estimate (4 chars ≈ 1 token).
func roughTokens(messages []Message) int {
	total := 0
	for _, msg := range messages {
		total += utf8.RuneCountInString(msg.Content) / charsPerToken
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			total += len(msg.ToolCalls) * toolCallOverhead
			for _, tc := range msg.ToolCalls {
				total += utf8.RuneCountInString(tc.Function.Arguments) / charsPerToken
			}
		}
		total += msgOverhead // per-message overhead
	}
	return total
}

// truncateHeadTail keeps headRunes from start and tailRunes from end.
func truncateHeadTail(s string, headRunes, tailRunes int) string {
	r := []rune(s)
	if len(r) <= headRunes+tailRunes {
		return s
	}
	return string(r[:headRunes]) + "\n... [truncated] ...\n" + string(r[len(r)-tailRunes:])
}

// truncateOldContentHeadTail truncates old message content using head+tail
// preservation: keeps oldMsgHeadChars from the start and oldMsgTailChars from
// the end, with a truncation marker in between. This preserves both the
// beginning context and the ending context of old messages.
func truncateOldContentHeadTail(s string) string {
	return truncateHeadTail(s, oldMsgHeadChars, oldMsgTailChars)
}
