// seed_query_result.go — the seed query result handling + seed-state sync
// layer: handleQueryResult, finalizeConversationPostHooks,
// maybeCheckpointCompletedTurn, syncSeedStateToSprout, and rebaseQueryStart.
// Split out of seed_query.go.

package agent

import (
	"errors"
	"fmt"
	"time"

	core "github.com/sprout-foundry/seed/core"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// ---------------------------------------------------------------------------
// Post-hooks and state sync
// ---------------------------------------------------------------------------

// maybeCheckpointCompletedTurn checks if a turn checkpoint should be recorded
// for a completed or max-iterations conversation turn.
func (a *Agent) maybeCheckpointCompletedTurn(processedQuery string, queryStartIndex, numMessages int) {
	if a.state == nil {
		return
	}
	messages := a.state.GetMessages()
	if queryStartIndex < 0 || queryStartIndex >= numMessages {
		return
	}

	reason := a.GetLastRunTerminationReason()
	if reason != RunTerminationCompleted && reason != RunTerminationMaxIterations {
		return
	}

	endIndex := numMessages - 1
	hasAssistant := false
	for i := queryStartIndex; i <= endIndex; i++ {
		if messages[i].Role == "assistant" {
			hasAssistant = true
			break
		}
	}
	if !hasAssistant {
		return
	}

	a.RecordTurnCheckpoint(queryStartIndex, endIndex)
}

// syncSeedStateToSprout merges seed's state back into sprout's state manager.
// Since the seed agent is now created with InitialMessages (the existing
// conversation history), seed's final state contains the complete message
// sequence: [historical msgs, new user msg, assistant msg, tool msgs, ...].
// We simply replace sprout's messages with seed's messages and sync counters.
//
// Returns the compaction survivor map when seed persisted a mid-turn
// compaction (nil otherwise), so callers can rebase their own pre-run
// indices (preSeedMsgCount) alongside the checkpoint rebase.
func (a *Agent) syncSeedStateToSprout(seedAgent *core.Agent) map[int]int {
	if a.state == nil {
		return nil
	}

	seedState := seedAgent.State()
	if seedState == nil {
		return nil
	}

	seedMsgs := seedState.Messages()

	// Seed now has the full history (via InitialMessages) plus new messages
	// from this query. Replace sprout's messages entirely.
	a.state.SetMessages(seedMsgs)

	// Rebase sprout's richer turn checkpoints through seed's compaction
	// survivor map when a mid-turn compaction persisted: the map's old
	// indices are pre-run state indices, which is exactly the layout
	// sprout's checkpoints were recorded against. Seed rebased its own
	// (plain) checkpoints in the loop; sprout's carry ID/FileChanges/
	// RevisionID metadata that must survive the same index shift.
	rebase := seedState.LastCompactionRebase()
	if rebase != nil {
		a.rebaseTurnCheckpoints(rebase)
	}

	// Accumulate token and cost counters across queries. The seed agent is
	// created fresh per query (see opts.InitialMessages earlier in this file)
	// so seedState.TotalTokens() and seedState.TotalCost() reflect only the
	// current query's API consumption. Without accumulation, sprout's
	// lifetime counters would be overwritten by each query — earlier
	// counters told a confusing story (e.g. tiny second-query delta hiding
	// the first query's cost).
	a.state.SetTotalTokens(a.state.GetTotalTokens() + seedState.TotalTokens())
	a.state.SetTotalCost(a.state.GetTotalCost() + seedState.TotalCost())

	// Calculate iteration count from seed's messages
	assistantCount := 0
	for _, msg := range seedMsgs {
		if msg.Role == "assistant" {
			assistantCount++
		}
	}

	// Determine termination reason
	terminationReason := ""
	if a.maxIterations > 0 && assistantCount >= a.maxIterations {
		terminationReason = RunTerminationMaxIterations
	} else if assistantCount > 0 {
		terminationReason = RunTerminationCompleted
	}
	a.state.SetLastRunTerminationReason(terminationReason)

	if assistantCount > 0 {
		a.state.SetCurrentIteration(assistantCount - 1)
	} else {
		a.state.SetCurrentIteration(0)
	}

	if a.debug {
		a.Logger().Debug("[sync] Seed sync complete: msgCount=%d, assistantCount=%d, terminationReason=%s, iteration=%d\n",
			len(seedMsgs), assistantCount, terminationReason, a.state.GetCurrentIteration())
	}

	return rebase
}

// rebaseQueryStart shifts a pre-run message index (e.g. preSeedMsgCount)
// through a compaction survivor map, clamping into the new layout. Used
// after syncSeedStateToSprout so finalize hooks that anchor against the
// pre-run conversation boundary (turn checkpoints) span the right
// messages after a mid-turn compaction shrank the list.
func rebaseQueryStart(idx int, survivorOf map[int]int, newLen int) int {
	if len(survivorOf) == 0 {
		return idx
	}
	if nw, ok := survivorOf[idx]; ok {
		return nw
	}
	// The boundary message itself was compacted away: the turn's own
	// messages were appended after the compaction (they postdate the
	// survivor set), so the turn starts right after the last surviving
	// pre-boundary message. When nothing before the boundary survived,
	// the turn sits at the head of the new layout.
	best := 0
	for old, nw := range survivorOf {
		if old < idx && nw+1 > best {
			best = nw + 1
		}
	}
	if best > newLen {
		best = newLen
	}
	return best
}

// publishTurnMetrics pushes the fresh totals at the end of a turn. The WebUI
// status bar's cost/context segments only refresh on metrics_update events,
// which otherwise fire solely on errors, chat switches, and reconnects — so
// spend tracking appeared frozen between turns (SP-113/SP-053-3 follow-up).
func (a *Agent) publishTurnMetrics() {
	a.publishEvent(
		events.EventTypeMetricsUpdate,
		events.MetricsUpdateEventWithCategory(
			a.GetProvider(),
			a.GetModel(),
			a.GetTotalTokens(),
			a.GetCurrentContextTokens(),
			a.getModelContextLimit(),
			a.state.GetCurrentIteration(),
			a.GetTotalCost(),
			"",
		),
	)
}

// handleQueryResult processes the result from seedAgent.Run(), handling fleet
// budget exceeded, general errors, and the success path. It syncs state back
// to sprout, commits tracked changes, and runs post-loop hooks.
func (a *Agent) handleQueryResult(qc *queryRunContext, result string, err error) (string, error) {
	if err != nil {
		// Check if the fleet budget was exceeded mid-run
		if errors.Is(err, FleetBudgetExceededError) {
			// Extract the last assistant response as the truncated result
			rebase := a.syncSeedStateToSprout(qc.seedAgent)
			qc.preSeedMsgCount = rebaseQueryStart(qc.preSeedMsgCount, rebase, len(a.state.GetMessages()))

			var truncatedResult string
			messages := a.state.GetMessages()
			for i := len(messages) - 1; i >= 0; i-- {
				if messages[i].Role == "assistant" && messages[i].Content != "" {
					truncatedResult = messages[i].Content
					break
				}
			}
			if truncatedResult == "" {
				truncatedResult = result
			}

			a.state.SetLastRunTerminationReason(RunTerminationFleetBudgetExceeded)
			a.journalSeedState(qc.seedAgent.State())
			a.finalizeConversationPostHooks(truncatedResult, qc.processedQuery, qc.preSeedMsgCount)

			return truncatedResult, nil
		}

		// A stop (the interrupt context was cancelled) is not a failure: keep
		// what the run produced and report it as interrupted. Classifying it
		// turned the cancelled request into a "temporary error … could not
		// recover" answer plus a failed-query event.
		if qc.runCtx.Err() != nil || errors.Is(err, core.ErrInterrupted) {
			rebase := a.syncSeedStateToSprout(qc.seedAgent)
			qc.preSeedMsgCount = rebaseQueryStart(qc.preSeedMsgCount, rebase, len(a.state.GetMessages()))
			a.state.SetLastRunTerminationReason(RunTerminationInterrupted)
			a.journalSeedState(qc.seedAgent.State())
			// The steps that finished before the stop were spent; the
			// footer shows them rather than waiting for the next turn.
			a.publishTurnMetrics()
			return "", fmt.Errorf("%w: %w", ErrRunInterrupted, err)
		}

		// Classify the error to provide a user-friendly message.
		// For permanent errors (auth, client error, context overflow), return
		// the error directly so both CLI and webui display it properly.
		classifiedErr := core.ClassifyError(err, a.GetModel())

		// Build a user-friendly message for the event and response
		wrapped := wrapError(classifiedErr)
		a.state.SetLastRunTerminationReason(RunTerminationCompleted)

		// Sync whatever state we can before returning
		rebase := a.syncSeedStateToSprout(qc.seedAgent)
		qc.preSeedMsgCount = rebaseQueryStart(qc.preSeedMsgCount, rebase, len(a.state.GetMessages()))
		a.journalSeedState(qc.seedAgent.State())
		a.finalizeConversationPostHooks(wrapped, qc.processedQuery, qc.preSeedMsgCount)

		// Return the classified error so CLI/webui display it properly.
		// The wrapped message is published via events above for display.
		return wrapped, classifiedErr
	}

	// Sync state back to sprout's agent manager
	rebase := a.syncSeedStateToSprout(qc.seedAgent)
	qc.preSeedMsgCount = rebaseQueryStart(qc.preSeedMsgCount, rebase, len(a.state.GetMessages()))
	a.journalSeedState(qc.seedAgent.State())

	// SP-152 §152b: language-guard the turn's final assistant message
	// before anything downstream (the query-completed event, later turns,
	// the CLI result) sees it. On a mismatch the corrected text — or the
	// localized notice — replaces the message in state and becomes the
	// turn's result; the mismatched original is kept on the message for
	// "view original". The streaming branch below still returns "" to
	// avoid duplicate display; the corrected text reaches the WebUI via
	// the query-completed event and via state.
	result = a.applyLanguageGuard(qc, result)

	// SP-149 §149c/§149d (149.6): attach the turn's verification result to
	// the final reply — AFTER the language guard, so the guard's
	// language-detection sees only the model's own text, and before the
	// return, via the stored per-turn state (reset in prepareQueryRun).
	// The attachment is a no-op when the turn-end hook never ran for the
	// turn. The error and interrupt paths above never reach this point:
	// an interrupted turn reports as an interrupt, not a verification
	// result.
	result = a.attachVerificationReply(result)

	// SP-151 §151a (151.3): emit the turn's verification + completion
	// progress events from the SP-149 turn-end result (progress_verification
	// when a result exists, then progress_complete). Placed after the
	// verification reply attachment and before the commit/finalize/streaming
	// early-returns below so both success outcomes emit them. The error and
	// interrupt paths above never reach this point: a failed or interrupted
	// run is not a completed run, so it emits no completion event (its
	// consumers already get the error/interrupt events).
	a.publishTurnProgressComplete()

	// ---- Post-loop hooks (moved from old ConversationHandler.finalizeConversation) ----

	// Commit tracked changes. Subagents are EXEMPT: their writes are
	// merged into the parent's tracker via MergeChild, and the PARENT's
	// Commit persists them (tagged "subagent:<persona>"). If a subagent
	// committed its own history entry it would (a) double-persist every
	// subagent-touched file, and (b) litter history with useless revision
	// dirs whose instructions field is just "subagent run". The subagent's
	// in-memory tracker still captures its FilesModified manifest for the
	// SubagentResult handoff — it just never flushes to disk itself.
	if !a.IsSubagent() && a.IsChangeTrackingEnabled() && a.GetChangeCount() > 0 {
		if commitErr := a.CommitChanges("Task completed"); commitErr != nil {
			a.Logger().Debug("Warning: Failed to commit changes: %v\n", commitErr)
		}
	}

	// Finalize post-loop tasks
	a.finalizeConversationPostHooks(result, qc.processedQuery, qc.preSeedMsgCount)

	// If streaming was enabled and content was streamed, return empty string
	// to avoid duplicate display in the top-level CLI console.
	// Subagents are exempt: their streaming callback writes prefixed lines to
	// stderr for the human, but the orchestrator LLM only sees what we return
	// here via SubagentResult.Output — returning "" would make the orchestrator
	// think the subagent did nothing and re-attempt the task.
	if !a.IsSubagent() && a.output.IsStreamingEnabled() && len(a.output.GetStreamingBuffer().String()) > 0 {
		return "", nil
	}

	return result, nil
}

// finalizeConversationPostHooks runs post-loop hooks shared by success and error paths.
func (a *Agent) finalizeConversationPostHooks(result string, processedQuery string, preSeedMsgCount int) {
	// The stored conversation carries the timestamp envelope from
	// injection-time stamping; downstream consumers (turn checkpoint
	// summaries, embedding signatures, transcript events) want clean text.
	cleanQuery := StripUserMessageTimestamp(processedQuery)

	// Maybe checkpoint completed turn
	a.maybeCheckpointCompletedTurn(cleanQuery, preSeedMsgCount, len(a.state.GetMessages()))

	// Publish query completed event
	var finalContent string
	messages := a.state.GetMessages()
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" {
			finalContent = messages[i].Content
			break
		}
	}
	// Fallback to the result string
	if finalContent == "" {
		finalContent = result
	}

	duration := time.Since(a.conversationStartTime)
	completedEvent := events.QueryCompletedEvent(
		cleanQuery,
		finalContent,
		a.GetTotalTokens(),
		a.GetTotalCost(),
		duration,
	)
	if reason := a.GetLastRunTerminationReason(); reason != "" {
		completedEvent["status"] = reason
	}
	a.publishEvent(events.EventTypeQueryCompleted, completedEvent)

	a.publishTurnMetrics()
}
