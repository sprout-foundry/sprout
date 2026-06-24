// Package agent provides the seed integration layer.
//
// seed_query.go — processQueryWithSeed and supporting functions for the
// seed query processing pipeline, including state sync and post-hooks.

package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	core "github.com/sprout-foundry/seed/core"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// ---------------------------------------------------------------------------
// Integration entry point
// ---------------------------------------------------------------------------

// injectInputMsg carries a user-steer message from the forwarder goroutine
// into the injector goroutine in processQueryWithSeed.
type injectInputMsg struct {
	content string
}

// processQueryWithSeed runs the conversation loop through seed's core.Agent
// instead of sprout's native ConversationHandler.
func (a *Agent) processQueryWithSeed(userQuery string) (string, error) {
	a.initSubManagers()

	// Guard against concurrent queries on the same Agent instance. In
	// shared-agent mode (CLI + WebUI), the second caller gets
	// ErrQueryInProgress instead of corrupting the message list.
	if err := a.TryBeginQuery(); err != nil {
		return "", err
	}
	defer a.EndQuery()

	// ---- Pre-loop hooks (moved from old ConversationHandler.ProcessQuery) ----

	// Reset termination reason for fresh query
	a.state.SetLastRunTerminationReason("")

	// Reset interrupt context so a Stop from the previous query doesn't
	// instantly cancel this one. Per SP-034-1e we now plumb interruptCtx
	// all the way into http.NewRequestWithContext, so leaving a cancelled
	// ctx around would make the next ProcessQuery fail before the first
	// LLM call lands.
	a.resetInterruptForNewQuery()

	// Publish query started event
	a.publishEvent(events.EventTypeQueryStarted, events.QueryStartedEvent(userQuery, a.GetProvider(), a.GetModel()))

	// Reset streaming buffers for new query
	a.output.GetStreamingBuffer().Reset()
	a.output.GetReasoningBuffer().Reset()

	// Enable change tracking
	a.EnableChangeTracking(userQuery)

	// Reset circuit breaker history for a fresh query
	if a.state.GetCircuitBreaker() != nil {
		a.state.GetCircuitBreaker().mu.Lock()
		for key := range a.state.GetCircuitBreaker().Actions {
			delete(a.state.GetCircuitBreaker().Actions, key)
		}
		a.state.GetCircuitBreaker().mu.Unlock()
		if a.debug {
			a.Logger().Debug("DEBUG: Reset circuit breaker for new query\n")
		}
	}

	// Process images if present — multimodal support
	images, processedQuery, err := a.processImagesInQuery(userQuery)
	if err != nil {
		a.publishEvent(events.EventTypeError, events.ErrorEvent("Image processing failed", err))
		return "", fmt.Errorf("failed to process images in query: %w", err)
	}

	// Set conversation start time for duration calculation
	a.conversationStartTime = time.Now()

	// Proactive context injection: retrieve relevant past work on first turn
	// or cold session restore. Only inject when the conversation is new (no
	// prior user messages) or the session was just restored from persistence
	// AND proactive context has not already been injected this session.
	existingSupplement := a.state.GetPendingSystemSupplement()
	// Match the current header from FormatProactiveContext. Kept as a
	// distinctive substring so cosmetic edits to the wording don't break
	// the dedup guard (the prior literal drifted and re-injected context
	// on every cold restore).
	alreadyInjected := strings.Contains(existingSupplement, "Previous Work (Read-Only Reference)")
	shouldInjectProactiveContext := !alreadyInjected &&
		(len(a.state.GetMessages()) == 0 || a.state.GetPreviousSummary() != "")
	if shouldInjectProactiveContext {
		injectCtx, injectCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := a.InjectProactiveContext(injectCtx, processedQuery); err != nil {
			a.Logger().Debug("[proactive-context] injection failed: %v\n", err)
		}
		injectCancel()
	}

	// SP-066 Phase 3: semantic recall over the conversation store. Runs
	// on every user turn — including mid-session — so that summaries
	// rolled past the substitution window (or wiped by a prior /compact)
	// can still surface when the current message references them.
	// Bounded by a tight timeout because this is on the user's critical
	// path; recall is a nice-to-have and failure must degrade gracefully.
	recallCtx, recallCancel := context.WithTimeout(context.Background(), 2*time.Second)
	a.InjectSemanticRecall(recallCtx, processedQuery)
	recallCancel()

	// Build the user message with processed (cleaned) query and images
	userMessage := api.Message{
		Role:    "user",
		Content: processedQuery,
		Images:  images,
	}

	// Register pasted images with the provider for attachment during Chat requests
	// The map key is the file path so the provider can match them up.
	pastedImageMap := make(map[string][]api.ImageData)
	if len(images) > 0 {
		// All images are from the same query — group them under a single key
		pastedImageMap["_current"] = images
	}

	// Save pre-seed message count and user message for later merge
	preSeedMsgCount := len(a.state.GetMessages())
	preSeedUserMsg := userMessage

	// Create seed provider adapter wrapping sprout's ClientInterface.
	// Capture a stable client reference under the read lock — the query
	// goroutine must use the same client instance throughout the run,
	// even if SetProvider is called concurrently from another path.
	clientSnap := a.getClient()
	prov, err := NewSproutProvider(a, clientSnap)
	if err != nil {
		return "", fmt.Errorf("failed to create seed provider adapter: %w", err)
	}

	_ = prov // provider ready for seed agent construction

	// Use seed's ToolRegistry — registers all 30 sprout tools with
	// PreExecuteHook (security classification + subagent nesting prevention)
	// and handles channel stripping, alias resolution, arg parsing/repair,
	// type coercion, timeouts, truncation, circuit breakers, parallel exec.
	//
	// Create a single richEventPublisher for both the ToolRegistry and the
	// seed core agent, so ALL events (tool_start, tool_end, errors, metrics,
	// compaction, agent_message) carry the agent's event metadata (client_id,
	// chat_id, user_id). Without this, events from the seed core conversation
	// loop are published directly to the raw EventBus and lack the metadata
	// needed by the WebSocket forwarding logic (shouldForwardEventToConnection)
	// to route them to the correct browser tab.
	var seedPublisher core.EventPublisher
	if a.eventBus != nil {
		seedPublisher = newRichEventPublisher(a.eventBus, a)
	}
	seedRegistry := newSeedToolRegistryWithPublisher(a, seedPublisher)

	// Build seed Agent options
	opts := core.Options{
		Provider:       prov,
		Executor:       seedRegistry,
		MaxIterations:  a.maxIterations,
		Debug:          a.debug,
		EventPublisher: seedPublisher,
	}

	// Context-management wiring: hand seed the optimizer, summarizer, and
	// pruner so the chat loop's compaction cascade (proactive threshold +
	// recovery-on-overflow) uses sprout's configuration end-to-end. All three
	// fields are nil-tolerant on the seed side, so any that aren't configured
	// here simply fall back to seed's defaults.
	if optWrap := a.state.GetOptimizer(); optWrap != nil {
		opts.Optimizer = optWrap.Inner()
	}
	if pruner := a.state.GetConversationPruner(); pruner != nil {
		opts.Pruner = pruner
	}
	opts.LLMSummarizer = wrapLLMSummarizerWithEvents(newLLMSummarizer(clientSnap, a.GetProvider()), a)

	// SP-066 Phase 1: model-aware compaction trigger fraction. seed's default
	// (0.85) leaves only 15% of the context window for response + thinking +
	// tool I/O, which thinking-budget models exhaust before emitting any
	// user-visible text. computeCompactionTriggerFraction subtracts the
	// reservation fractions defined in context_budget.go so substitution
	// fires earlier — by default at 0.70 instead of 0.85.
	opts.CompactionTriggerFraction = a.computeCompactionTriggerFraction()

	if a.systemPrompt != "" {
		opts.SystemPrompt = a.systemPrompt
	}

	// Consume any pending system supplement (previous session context,
	// proactive context) and append to the system prompt so the seed agent
	// includes it in its first message.
	if supplement := a.consumePendingSystemSupplement(); supplement != "" {
		opts.SystemPrompt = opts.SystemPrompt + "\n\n" + supplement
	}

	// OnIteration callback: sync per-iteration context token estimates
	// back to sprout's state so the UI can show real-time token usage,
	// and emit the SP-066 context-management diagnostic so subscribers
	// can verify the model-aware trigger fraction is sized correctly.
	opts.OnIteration = func(iteration, messages, tokenEstimate, contextSize int) {
		a.state.SetCurrentIteration(iteration)
		a.state.SetCurrentContextTokens(tokenEstimate)
		a.state.SetMaxContextTokens(contextSize)
		a.PublishContextManagementDiagnostic(tokenEstimate, contextSize, iteration, messages, a.GetCachedTokens(), a.GetPromptTokens(), 0)
	}

	// Seed the agent with the existing conversation history so that
	// multi-turn continuity is preserved across queries.
	if msgs := a.state.GetMessages(); len(msgs) > 0 {
		opts.InitialMessages = msgs
	}

	// Restore turn checkpoints so that the message pipeline can apply
	// checkpoint compaction before sending to the provider. Without this,
	// restored sessions send the entire raw history (potentially hundreds of
	// messages with tool calls) instead of the compacted summary, causing
	// provider 400 errors due to mismatched tool calls/responses.
	if cps := a.state.GetTurnCheckpoints(); len(cps) > 0 {
		seedCPs := make([]core.TurnCheckpoint, len(cps))
		for i, cp := range cps {
			// Convert sprout's CheckpointFileChange manifest to seed's
			// FileChange slice so the model sees the git-style turn
			// manifest and can resolve revision_id via the view_history
			// tool when it needs the full diff.
			var seedChanges []core.FileChange
			if len(cp.FileChanges) > 0 {
				seedChanges = make([]core.FileChange, len(cp.FileChanges))
				for j, fc := range cp.FileChanges {
					seedChanges[j] = core.FileChange{Path: fc.Path, Op: fc.Op}
				}
			}
			seedCPs[i] = core.TurnCheckpoint{
				StartIndex:        cp.StartIndex,
				EndIndex:          cp.EndIndex,
				Summary:           cp.Summary,
				ActionableSummary: cp.ActionableSummary,
				FileChanges:       seedChanges,
				RevisionID:        cp.RevisionID,
			}
		}
		opts.InitialCheckpoints = seedCPs
	}

	// Create seed Agent
	seedAgent, err := core.NewAgent(opts)
	if err != nil {
		return "", fmt.Errorf("failed to create seed agent: %w", err)
	}

	// Run the query through seed's conversation loop.
	// Use the processed (cleaned) query so image placeholders are replaced.
	// ctx is the agent's interrupt context so TriggerInterrupt() — wired to
	// the webui Stop button at pkg/webui/api_query.go::handleAPIQueryStop —
	// actually aborts the in-flight HTTP request, not just the agent loop
	// after the next iteration boundary. See SP-034-1e.
	ctx, _ := a.snapshotInterrupt()
	if ctx == nil {
		ctx = context.Background()
	}

	// Bridge sprout's user-facing inputInjectionChan to seed's InjectInput.
	// Callers (CLI prompt goroutine, webui /api/query/steer) push into the
	// sprout channel via InjectInputContext; this forwarder drains it and
	// hands the message to seed, which consumes it at the next natural
	// break point in its loop (between iterations, before deciding to
	// terminate the turn). Without this bridge the sprout channel buffers
	// forever and "steering" silently no-ops.
	//
	// runCtx is scoped to this query (separate from a.interruptCtx, which
	// outlives a single Run) so the forwarder exits cleanly when the
	// model returns. seed.InjectInput is buffered size 1; if full we
	// briefly sleep before retrying so we don't lose the user's steer to
	// a transient collision with seed's own consumer.
	runCtx, runCancel := context.WithCancel(ctx)
	injectChan := make(chan injectInputMsg, 8)
	steerDone := make(chan struct{})

	// Forwarder: reads from sprout's input channel and sends to injectChan
	go func() {
		defer close(injectChan)
		ch := a.GetInputInjectionContext()
		for {
			select {
			case <-runCtx.Done():
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				select {
				case injectChan <- injectInputMsg{content: msg}:
				case <-runCtx.Done():
					return
				}
			}
		}
	}()

	// Injector: reads from injectChan and applies to seed agent
	go func() {
		defer close(steerDone)
		for msg := range injectChan {
			for !seedAgent.InjectInput(msg.content) {
				select {
				case <-runCtx.Done():
					return
				case <-time.After(25 * time.Millisecond):
				}
			}
		}
	}()
	defer func() {
		runCancel()
		<-steerDone
	}()

	result, err := seedAgent.Run(ctx, processedQuery)
	if err != nil {
		// Check if the fleet budget was exceeded mid-run
		if errors.Is(err, FleetBudgetExceededError) {
			// Extract the last assistant response as the truncated result
			a.syncSeedStateToSprout(seedAgent, preSeedUserMsg, preSeedMsgCount)

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
			a.finalizeConversationPostHooks(truncatedResult, processedQuery, preSeedMsgCount)

			return truncatedResult, nil
		}

		// Classify the error to provide a user-friendly message.
		// For permanent errors (auth, client error, context overflow), return
		// the error directly so both CLI and webui display it properly.
		classifiedErr := core.ClassifyError(err, a.GetModel())

		// Build a user-friendly message for the event and response
		wrapped := wrapError(classifiedErr)
		a.state.SetLastRunTerminationReason(RunTerminationCompleted)

		// Sync whatever state we can before returning
		a.syncSeedStateToSprout(seedAgent, preSeedUserMsg, preSeedMsgCount)

		// Finalize — publish the user-friendly message as the response
		a.finalizeConversationPostHooks(wrapped, processedQuery, preSeedMsgCount)

		// Return the classified error so CLI/webui display it properly.
		// The wrapped message is published via events above for display.
		return wrapped, classifiedErr
	}

	// Sync state back to sprout's agent manager
	a.syncSeedStateToSprout(seedAgent, preSeedUserMsg, preSeedMsgCount)

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

	// Run self-review gate if changes were tracked (primary only; a
	// subagent's work is reviewed by its parent orchestrator).
	if !a.IsSubagent() && a.IsChangeTrackingEnabled() && a.GetChangeCount() > 0 {
		if err := a.runSelfReviewGate(); err != nil {
			a.publishEvent(events.EventTypeError, events.ErrorEvent("Self-review gate failed", err))
			return "", fmt.Errorf("failed self-review gate: %w", err)
		}
	}

	// Finalize post-loop tasks
	a.finalizeConversationPostHooks(result, processedQuery, preSeedMsgCount)

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
	// Maybe checkpoint completed turn
	a.maybeCheckpointCompletedTurn(processedQuery, preSeedMsgCount, len(a.state.GetMessages()))

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
		processedQuery,
		finalContent,
		a.GetTotalTokens(),
		a.GetTotalCost(),
		duration,
	)
	if reason := a.GetLastRunTerminationReason(); reason != "" {
		completedEvent["status"] = reason
	}
	a.publishEvent(events.EventTypeQueryCompleted, completedEvent)
}

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

	a.RecordTurnCheckpointAsync(queryStartIndex, endIndex)
}

// syncSeedStateToSprout merges seed's state back into sprout's state manager.
// Since the seed agent is now created with InitialMessages (the existing
// conversation history), seed's final state contains the complete message
// sequence: [historical msgs, new user msg, assistant msg, tool msgs, ...].
// We simply replace sprout's messages with seed's messages and sync counters.
func (a *Agent) syncSeedStateToSprout(seedAgent *core.Agent, userMsg api.Message, preSeedMsgCount int) {
	if a.state == nil {
		return
	}

	seedState := seedAgent.State()
	if seedState == nil {
		return
	}

	seedMsgs := seedState.Messages()

	// Seed now has the full history (via InitialMessages) plus new messages
	// from this query. Replace sprout's messages entirely.
	a.state.SetMessages(seedMsgs)

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
}
