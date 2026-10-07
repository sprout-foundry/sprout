// Package agent provides the seed integration layer.
// This file holds the query-run preparation path: the pasted-image
// registration, the user-message timestamp inject / strip helpers,
// the processQueryWithSeed entry point, and prepareQueryRun. The result
// handling + seed-state sync layer lives in seed_query_result.go.

package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	core "github.com/sprout-foundry/seed/core"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/verify"
)

// ---------------------------------------------------------------------------
// Integration entry point
// ---------------------------------------------------------------------------
// registerPastedImagesWithProvider hands extracted image data to the provider
// so it can attach the images to the first user message in each Chat request.
// If the provider is not a *sproutProvider (e.g. a mock in tests), the images
// are logged and skipped rather than silently dropped.
func registerPastedImagesWithProvider(a *Agent, prov core.Provider, images map[string][]api.ImageData) {
	if len(images) == 0 {
		return
	}
	sp, ok := prov.(*sproutProvider)
	if !ok {
		a.Logger().Debug("[WARN] Cannot register pasted images: provider is not *sproutProvider (got %T)\n", prov)
		return
	}
	sp.RegisterPastedImages(images)
}

// InjectUserMessageTimestamp prepends a <current-time>...</current-time> tag
// to the user message so the model sees the exact moment of each turn without
// invalidating the prompt-prefix cache. The system prompt stays static across
// requests (date/time injection there would defeat provider caching and cost
// users real money on every turn). The stamp is applied once at injection —
// prepareQueryRun stamps the query before it enters the conversation state,
// and steer messages are stamped at delivery — so the stamped bytes are part
// of the prefix every later request replays byte-identically. The
// provider-boundary net (stampTurnTimestamp) only catches messages that
// somehow arrive unstamped. Persisted state is stripped (ExportState), so
// restored sessions re-stamp on their first turn.
// ISO 8601 with timezone offset is machine-parseable; the Local parenthetical
// matches what the user sees in their OS clock so the model can reason about
// time-of-day naturally.
//
// Empty or whitespace-only input is returned unchanged so wakeup-only turns
// (background-task notifications with no user message) don't produce a bare
// timestamp that the model would have to interpret.
func InjectUserMessageTimestamp(userMessage string) string {
	return InjectUserMessageTimestampAt(userMessage, time.Now())
}

// InjectUserMessageTimestampAt prepends a timestamp fixed at at. Providers use
// it to keep one turn's prompt byte-identical across iterations and retries.
func InjectUserMessageTimestampAt(userMessage string, at time.Time) string {
	if strings.TrimSpace(userMessage) == "" {
		return userMessage
	}
	// ISO 8601 with offset. Location().String() gives "Local" inside Go's
	// test runner, so we also include the resolved zone name for human
	// readability — the model uses both the absolute timestamp and the
	// local clock to reason about "wait 5 minutes" or "is the user up late".
	return fmt.Sprintf(
		"<current-time>%s (Local: %s, %s)</current-time>\n\n%s",
		at.Format(time.RFC3339),
		at.Format("2006-01-02 15:04:05"),
		at.Location().String(),
		userMessage,
	)
}

// StripUserMessageTimestamp removes a leading provider timestamp envelope
// from a user message. It accepts legacy LF and CRLF separators and leaves
// malformed tags or tags that do not start at offset zero unchanged.
//
// The matching uses the first <current-time>...</current-time> envelope in
// the input. Because the envelope is a small fixed shape and our injector
// (InjectUserMessageTimestamp) only emits well-formed envelopes whose body
// (the RFC3339 / formatted time / zone name) never contains "</current-time>"
// or "\n\n" between the tags, a substring scan is sufficient. A leading
// tag whose body is empty returns "". Tag detection is anchored at offset 0,
// so anything with leading whitespace before the tag is left intact.
func StripUserMessageTimestamp(userMessage string) string {
	const (
		openTag  = "<current-time>"
		closeTag = "</current-time>"
	)
	if !strings.HasPrefix(userMessage, openTag) {
		return userMessage
	}
	closeIndex := strings.Index(userMessage[len(openTag):], closeTag)
	if closeIndex < 0 {
		return userMessage
	}
	body := userMessage[len(openTag)+closeIndex+len(closeTag):]
	body = strings.TrimPrefix(body, "\r\n\r\n")
	body = strings.TrimPrefix(body, "\n\n")
	return body
}

// queryRunContext holds all shared state between the preparation and execution
// phases of processQueryWithSeed. It keeps the thin orchestrator clean and
// avoids a long list of return values from prepareQueryRun.
type queryRunContext struct {
	seedAgent       *core.Agent
	preSeedMsgCount int
	processedQuery  string
	runCtx          context.Context
	runCancel       context.CancelFunc
}

// processQueryWithSeed runs the conversation loop through seed's core.Agent
// instead of sprout's native ConversationHandler.
func (a *Agent) processQueryWithSeed(source, userQuery string) (string, error) {
	// Admit the query before publishing any per-turn state. This makes the
	// query guard the ownership boundary for turnTimestamp: a rejected caller
	// cannot overwrite or clear the timestamp belonging to the active turn.
	if err := a.TryBeginQueryAs(source); err != nil {
		return "", err
	}
	defer func() {
		a.turnTimestampMu.Lock()
		a.turnTimestamp = time.Time{}
		a.turnTimestampMu.Unlock()
		a.EndQuery()
		// Immediate wakeup check: a background task may have completed
		// while this turn ran. Resuming here avoids waiting for the WebUI
		// poller's next tick — or the user's next message — to act on it.
		a.TryAutoResume()
	}()

	a.turnTimestampMu.Lock()
	a.turnTimestamp = time.Now()
	a.turnTimestampMu.Unlock()

	a.beginTurnJournal(userQuery)

	qc, err := a.prepareQueryRun(userQuery, source)
	if err != nil {
		a.endTurnJournal()
		return "", err
	}
	defer func() {
		a.endTurnJournal()
		qc.runCancel()
	}()

	result, err := qc.seedAgent.Run(qc.runCtx, qc.processedQuery)
	if err == nil {
		// Quality after edits: on the success path only, the turn-end
		// quality hook may run the project's formatter and linter and
		// continue the turn (a repair round). It runs before verification
		// so a formatter's in-place rewrite is what verification then
		// builds and tests. A hard error from the first Run flows to
		// handleQueryResult unchanged (skipping both hooks).
		result, err = a.runTurnEndQuality(qc, result)
	}
	if err == nil {
		// On the success path only, the turn-end
		// verification hook may continue the turn (a repair round). A
		// hard error from the first Run flows to handleQueryResult
		// unchanged.
		result, err = a.runTurnEndVerification(qc, result)
	}
	return a.handleQueryResult(qc, result, err)
}

// ---------------------------------------------------------------------------
// Preparation phase
// ---------------------------------------------------------------------------

// prepareQueryRun sets up the seed agent, wires steering goroutines, and
// returns everything needed for the execution phase. It handles:
//
// - State reset (termination reason, interrupt, streaming buffers, etc.)
// - Image processing and proactive context injection
// - Seed provider and tool registry construction
// - core.Options assembly (compaction, callbacks, checkpoints)
// - Seed agent creation
// - Steer forwarder and injector goroutines
func (a *Agent) prepareQueryRun(userQuery, source string) (*queryRunContext, error) {
	a.initSubManagers()

	// Query admission and release are owned by processQueryWithSeed so the
	// guard and per-turn timestamp have exactly the same lifecycle.

	// ---- Pre-loop hooks (moved from old ConversationHandler.ProcessQuery) ----

	// Reset termination reason for fresh query
	a.state.SetLastRunTerminationReason("")

	// Reset interrupt context so a Stop from the previous query doesn't instantly cancel this one.
	a.resetInterruptForNewQuery()

	// Publish query started event. The chat bubble shows the user-facing
	// display text (raw user text, or "Looking into '…'…" for wakeup
	// turns) — never the internal wakeup batch prepended for the model.
	display := a.takePendingQueryDisplay()
	a.rememberQueryDisplay(userQuery, display)
	a.publishEvent(events.EventTypeQueryStarted, events.QueryStartedEventWithDisplay(
		userQuery, display, source, a.GetProvider(), a.GetModel()))

	// Reset streaming buffers for new query
	a.output.GetStreamingBuffer().Reset()
	a.output.GetReasoningBuffer().Reset()

	// Enable change tracking
	a.EnableChangeTracking(userQuery)

	// Reset the per-turn verification state at the
	// same per-turn point change tracking opens its window: a previous
	// turn's stored result, repair attempts, and limit must never attach
	// to this turn's reply — a turn's final reply may only carry that
	// turn's verification outcome (the turn-end hook stores it at the
	// turn's end).
	a.resetTurnVerification()

	// Quality after edits: reset the per-turn quality state at the same
	// per-turn point, so a previous turn's quality result never attaches to
	// this turn.
	a.resetTurnQuality()

	// Capture the turn's verification inputs — the starter
	// manifest's commands and the plan's acceptance — once, at the turn's
	// start, right after the per-turn verification state is reset. Every
	// verification run of the turn (every repair round) executes against
	// this snapshot rather than re-reading the files, so a model that
	// edits .sprout/starter.json or .sprout/plan.json mid-turn cannot
	// change what "passing" means. A snapshot is taken only when
	// verification is enabled for this turn; a disabled turn takes none.
	if a.configManager != nil {
		if cfg := a.configManager.GetConfig(); cfg != nil && cfg.VerificationEnabled() {
			a.setTurnVerifySnapshot(verify.New().Snapshot(a.GetWorkspaceRoot()))
		}
	}

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
		return nil, agenterrors.NewAgent("seed-query", "failed to process images in query", err)
	}

	// Stamp the turn timestamp into the query ONCE, at injection. The
	// stamped text becomes part of the in-memory conversation state, so
	// every request this turn — and every later turn that replays this
	// message as prefix — carries byte-identical user-message content.
	// This is what keeps the provider prompt cache eligible across turns:
	// stamping only at the provider boundary made a message go out
	// stamped during its own turn and unstamped from the next turn on,
	// flipping prefix bytes at every turn boundary. The stamp is stripped
	// again at ExportState, so persisted sessions stay envelope-free
	// (StripUserMessageTimestamp consumers keep working on disk state).
	// Applied after image processing so placeholders see clean text; the
	// query_started event above already published the raw display text.
	a.turnTimestampMu.RLock()
	turnStamp := a.turnTimestamp
	a.turnTimestampMu.RUnlock()
	if !turnStamp.IsZero() {
		processedQuery = InjectUserMessageTimestampAt(processedQuery, turnStamp)
	}

	// Set conversation start time for duration calculation
	a.conversationStartTime = time.Now()

	// Resolve the turn's user language once (recent user
	// messages + the current query, with the configured fallback) and store
	// it on the agent so the streaming provider path can gate assistant-text
	// delivery through the hold-back. Inactive (no hold-back, byte-identical
	// streaming) for subagents, when the guard is disabled, or when the user
	// language is undetermined.
	a.resolveTurnLanguageGuard(processedQuery)

	// Group extracted images for provider registration. All images from this
	// query are attached to the first user message by attachPastedImages.
	pastedImageMap := make(map[string][]api.ImageData)
	if len(images) > 0 {
		pastedImageMap["_current"] = images
	}

	// Save pre-seed message count for later merge
	preSeedMsgCount := len(a.state.GetMessages())

	// Create seed provider adapter wrapping sprout's ClientInterface.
	// Capture a stable client reference under the read lock — the query
	// goroutine must use the same client instance throughout the run,
	// even if SetProvider is called concurrently from another path.
	clientSnap := a.getClient()
	prov, err := NewSproutProvider(a, clientSnap)
	if err != nil {
		return nil, agenterrors.NewAgent("seed-query", "failed to create seed provider adapter", err)
	}

	// Register pasted images with the provider so attachPastedImages can
	// attach them to the first user message in each Chat request. Without
	// this call the provider's pastedImages map stays empty and images
	// extracted by processImagesInQuery never reach the model.
	registerPastedImagesWithProvider(a, prov, pastedImageMap)

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

	// Steer delivery: staged messages (steer_staging.go) are handed to seed
	// one per conversation-loop boundary by hooks that run inside seed's
	// loop goroutine — provider return (Chat/ChatStream) and tool-batch
	// return (executor wrapper). Both fire immediately before seed's own
	// injection pickup checks, preserving the eager pipeline's timing while
	// keeping every message retractable until pickup. Messages still staged
	// when the run ends simply wait for the next run's boundaries.
	steerDeliverer := &steerBoundaryDeliverer{agent: a}
	setProviderSteerHook(prov, func() { steerDeliverer.deliverOne() })

	// Build seed Agent options
	opts := core.Options{
		Provider:       prov,
		Executor:       &steerFlushExecutor{inner: seedRegistry, deliverer: steerDeliverer},
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

	// Model-aware compaction trigger fraction. seed's default (0.85) leaves only 15% of the
	// context window for response + thinking + tool I/O, which thinking-budget models exhaust
	// before emitting any user-visible text. computeCompactionTriggerFraction subtracts the
	// reservation fractions so substitution fires earlier — by default at 0.70 instead of 0.85.
	opts.CompactionTriggerFraction = a.computeCompactionTriggerFraction()
	opts.SubstitutionTargetFraction = 0.50

	// When the project's starter manifest names a starter, its
	// stack skill auto-activates at turn start — folded into the system
	// prompt before it is handed to the seed agent (the next block).
	// Best-effort and idempotent: a missing/invalid manifest, a starter with
	// no skill, or an already-active skill is a no-op and never fails the
	// turn.
	a.autoActivateStarterSkill()

	if a.systemPrompt != "" {
		opts.SystemPrompt = a.systemPrompt
	}

	// Consume any pending system supplement (previous session context) and
	// append to the system prompt so the seed agent
	// includes it in its first message.
	if supplement := a.consumePendingSystemSupplement(); supplement != "" {
		opts.SystemPrompt = opts.SystemPrompt + "\n\n" + supplement
	}

	// When a structured plan exists, append a compact plan
	// summary (goal + scope items with status) so the model works scope item
	// by scope item. planContextSummary returns "" when there is no plan or
	// the plan is unreadable/invalid, so an absent plan never changes the
	// context and never fails the turn.
	if planSummary := a.planContextSummary(); planSummary != "" {
		opts.SystemPrompt = opts.SystemPrompt + "\n\n" + planSummary
	}

	// When the project's starter manifest names an older
	// version of its starter than the embedded starter tree, append the
	// upgrade notice so the agent may propose the upgrade (using the
	// skill's upgrade note) — but never apply it silently. The hook runs
	// after the system prompt is set (above), because the notice is a
	// per-turn append to opts.SystemPrompt, like the plan summary.
	// starterUpgradeNotice returns "" when there is no manifest, nothing to
	// propose, or an ambiguous version comparison, so an up-to-date or
	// starter-less project never changes the context and never fails the
	// turn. It only reads (manifest + embedded catalogue): the hook itself
	// never writes a project file.
	if upgradeNotice := a.starterUpgradeNotice(); upgradeNotice != "" {
		opts.SystemPrompt = opts.SystemPrompt + "\n\n" + upgradeNotice
	}

	var seedAgentRef *core.Agent

	// OnIteration callback: sync per-iteration context token estimates back to sprout's state
	// so the UI can show real-time token usage, and emit the context-management diagnostic.
	opts.OnIteration = func(iteration, messages, tokenEstimate, contextSize int) {
		a.state.SetCurrentIteration(iteration)
		a.state.SetCurrentContextTokens(tokenEstimate)

		// SP-138: journal seed's live conversation at each iteration start.
		// This is the mid-turn WAL — a hard kill between iterations loses at
		// most one iteration. State read under the closure-captured ref,
		// which is assigned before Run() begins iterating.
		if seedAgentRef != nil {
			if st := seedAgentRef.State(); st != nil {
				a.journalSeedState(st)
			}
		}

		// SP-126: clamp to the effective cap (user MaxContextTokens or native window).
		// After a model switch, effectiveContextCap is refreshed by
		// refreshEffectiveContextCap() so this uses the current model's cap.
		// One snapshot so both reads see the same value.
		cap := a.effectiveCapSnapshot()
		if cap > 0 && contextSize > cap {
			contextSize = cap
		}
		// When the provider reports no context info (contextSize == 0),
		// fall back to the effective cap if we have one.
		if contextSize == 0 {
			contextSize = cap
		}
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
	// Acquire the checkpoint read-lock to avoid racing with concurrent
	// checkpointing and rollup operations that modify the slice.
	cps := func() []TurnCheckpoint {
		mu := a.state.GetCheckpointMutex()
		mu.RLock()
		defer mu.RUnlock()
		return a.state.GetTurnCheckpoints()
	}()
	if len(cps) > 0 {
		seedCPs := make([]core.TurnCheckpoint, len(cps))
		for i, cp := range cps {
			seedCPs[i] = core.TurnCheckpoint{
				StartIndex:        cp.StartIndex,
				EndIndex:          cp.EndIndex,
				Summary:           cp.Summary,
				ActionableSummary: cp.ActionableSummary,
			}
		}
		opts.InitialCheckpoints = seedCPs
	}

	// Create seed Agent
	seedAgent, err := core.NewAgent(opts)
	if err != nil {
		return nil, agenterrors.NewAgent("seed-query", "failed to create seed agent", err)
	}
	seedAgentRef = seedAgent

	// Run the query through seed's conversation loop.
	// Use the processed (cleaned) query so image placeholders are replaced.
	// ctx is the agent's interrupt context so TriggerInterrupt() aborts the in-flight HTTP request.
	ctx, _ := a.snapshotInterrupt()
	if ctx == nil {
		ctx = context.Background()
	}
	runCtx, runCancel := context.WithCancel(ctx)

	// Steer delivery wiring, phase 2: the deliverer needs the constructed
	// seed agent. Everything else (provider hook, executor wrapper) was
	// wired above before core.NewAgent consumed the options.
	steerDeliverer.setSeedAgent(seedAgent)

	return &queryRunContext{
		seedAgent:       seedAgent,
		preSeedMsgCount: preSeedMsgCount,
		processedQuery:  processedQuery,
		runCtx:          runCtx,
		runCancel:       runCancel,
	}, nil
}

// ---------------------------------------------------------------------------
// Execution result handling
// ---------------------------------------------------------------------------
