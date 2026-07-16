package agent

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sprout-foundry/sprout/pkg/envutil"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// subagentRunContext holds all the state wired up during setupSubagentRun
// so that runTask can remain a thin orchestrator. Closures (streaming
// callback, terminal writer) capture fields from this struct.
type subagentRunContext struct {
	runCtx          context.Context
	cancel          context.CancelFunc
	subAgent        *Agent
	prefix          string
	dimGray         string
	reset           string
	eventBus        *events.EventBus
	stopProgress    chan struct{}
	progressSubName string
	progressLog     *[]SubagentProgressEntry
	progressMu      *sync.Mutex
	lineBuf         *strings.Builder
	outputMu        *sync.Mutex
	running         *runningSubagent
	budgetExceeded  *atomic.Bool
}

// setupSubagentRun creates and configures a subagent for execution.
// It returns a populated subagentRunContext on success, or an early-error
// *SubagentResult on failure (e.g., createSubagent error).
func (r *SubagentRunner) setupSubagentRun(
	ctx context.Context,
	taskID string,
	prompt string,
	opts SubagentOptions,
	cumulativeTokens *atomic.Int64,
	fleetBudgetLimit int64,
	startTime time.Time,
) (*subagentRunContext, *SubagentResult) {
	// Create context with optional timeout
	var runCtx context.Context
	var cancel context.CancelFunc
	if opts.Timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, opts.Timeout)
	} else {
		runCtx, cancel = context.WithCancel(ctx)
	}

	// Create subagent, deriving its interrupt context from runCtx so
	// cancellation propagates into the subagent's LLM calls.
	subAgent, err := r.createSubagent(opts, runCtx)
	if err != nil {
		cancel()
		return nil, &SubagentResult{
			ID:      taskID,
			Error:   agenterrors.Wrap(err, "create subagent"),
			Elapsed: time.Since(startTime),
		}
	}

	// SP-059 Phase 4: share the parent's clarification manager and assign
	// this subagent a clarification ID (its taskID). Lets the subagent
	// call request_clarification mid-run and route the user's response
	// back to itself via the shared manager.
	if r.parentAgent != nil && r.parentAgent.clarificationManager != nil {
		subAgent.clarificationManager = r.parentAgent.clarificationManager
		subAgent.subagentID = taskID
	}

	// SP-051-2d: bump the process-wide active-subagent counter so the CLI
	// status footer can show " · N sub" while subagents are running.
	// Decremented on Run completion via the defer below.
	IncrementActiveSubagents()

	// Wire up per-LLM-call fleet budget tracking (SP-037-2c).
	// This enables the subagent to debit tokens after each LLM call and
	// truncate gracefully when the shared budget is exceeded mid-run.
	if cumulativeTokens != nil && fleetBudgetLimit > 0 {
		subAgent.SetFleetBudget(cumulativeTokens, fleetBudgetLimit)
	}

	// Propagate the parent's USD budget to this subagent so the cap is
	// workflow-wide. Subagents share the same *FleetUsdBudget by
	// reference, so debits accumulate in a single counter.
	if r.parentAgent != nil {
		if usd := r.parentAgent.GetFleetUsdBudget(); usd != nil {
			subAgent.SetFleetUsdBudget(usd)
		}
	}

	// Set up terminal output prefixing for subagent. Dim the prefix so the
	// subagent's lines read as secondary to the primary's, but honor NO_COLOR /
	// non-terminal output — otherwise raw escape codes leak into pipes/logs.
	prefix := buildSubagentPrefix(opts.Persona, taskID)
	dimGray := "\033[90m"
	reset := "\033[0m"
	if !envutil.ResolveColorPreference(true) {
		dimGray = ""
		reset = ""
	}

	// Create OutputRouter with the shared eventBus so subagent events
	// (stream_chunk, agent_message, tool_log, etc.) are published to the
	// event bus when in WebUI mode.
	eventBus := r.shared.EventBus
	router := NewOutputRouter(subAgent, eventBus)
	subAgent.output.SetOutputRouter(router)

	// SP-059 Phase 3a: capture a per-run progress log by subscribing to
	// the shared event bus and filtering for subagent_activity events
	// whose task_id matches this run. Without this the primary's LLM
	// only sees the final stdout — no insight into *what* the subagent
	// did along the way. Bounded to subagentProgressLogCap entries
	// (head-trimmed) so a chatty subagent can't bloat the envelope.
	var progressLog []SubagentProgressEntry
	var progressMu sync.Mutex
	stopProgress := make(chan struct{})
	progressSubName := ""
	if eventBus != nil {
		progressSubName = fmt.Sprintf("subagent-progress-%s", taskID)
		eventCh := eventBus.Subscribe(progressSubName)
		go func() {
			for {
				select {
				case <-stopProgress:
					return
				case ev, ok := <-eventCh:
					if !ok {
						return
					}
					if ev.Type != "subagent_activity" {
						continue
					}
					data, dataOk := ev.Data.(map[string]interface{})
					if !dataOk {
						continue
					}
					if tid, _ := data["task_id"].(string); tid != taskID {
						continue
					}
					phase, _ := data["phase"].(string)
					message, _ := data["message"].(string)
					progressMu.Lock()
					if len(progressLog) >= subagentProgressLogCap {
						// Head-trim so the most recent entries are
						// always visible. Cheap because slice header
						// just moves; underlying array is reused.
						progressLog = progressLog[1:]
					}
					progressLog = append(progressLog, SubagentProgressEntry{
						OffsetMS: time.Since(startTime).Milliseconds(),
						Phase:    phase,
						Message:  message,
					})
					progressMu.Unlock()
				}
			}
		}()
	}

	// Determine a mutex for thread-safe output across parallel subagents.
	// Use the parent agent's output mutex if available; otherwise create
	// one so parallel subagents don't interleave terminal output.
	var outputMu *sync.Mutex
	if r.parentAgent != nil && r.parentAgent.output != nil {
		outputMu = r.parentAgent.output.GetOutputMutex()
	}
	if outputMu == nil {
		outputMu = &sync.Mutex{}
		subAgent.output.SetOutputMutex(outputMu)
	}

	// Line buffer for accumulating stream chunks. The mutex protects lineBuf
	// across parallel subagents; stderr writes happen AFTER releasing it so a
	// slow/full stderr pipe can't stall siblings holding lineBuf access.
	// Per-line writes stay below PIPE_BUF, so byte-level interleaving is safe.
	var lineBuf strings.Builder

	// Capture task metadata for publishing subagent_activity events from the
	// streaming callback. The subagent's LLM prose output normally goes to
	// stream_chunk events which are depth-suppressed in the WebUI — publishing
	// subagent_activity events alongside stderr writes ensures the
	// SubagentActivityFeed shows the subagent's actual responses.
	subPersona := opts.Persona
	subTaskID := taskID
	subEventBus := eventBus
	subIsParallel := !strings.HasPrefix(taskID, "subagent-")
	subAgent.EnableStreaming(func(chunk string) {
		var pending []string
		var rawLines []string // mirror of pending without ANSI/prefix formatting
		// RouteStreamChunk holds outputMu (via TryLock) before calling this
		// callback. Using TryLock here avoids re-entrancy deadlock: if the lock
		// is already held by the router, proceed without it — the router
		// serialises the write.
		selfLocked := outputMu.TryLock()
		lineBuf.WriteString(chunk)
		for {
			content := lineBuf.String()
			idx := strings.IndexByte(content, '\n')
			if idx == -1 {
				break
			}
			line := content[:idx]
			if strings.TrimSpace(line) != "" {
				pending = append(pending, dimGray+prefix+reset+" "+line+"\n")
				rawLines = append(rawLines, line)
			}
			lineBuf.Reset()
			if idx+1 < len(content) {
				lineBuf.WriteString(content[idx+1:])
			}
		}
		if selfLocked {
			outputMu.Unlock()
		}

		for _, line := range pending {
			_, _ = os.Stderr.Write([]byte(line))
		}
		// Publish each complete line as a subagent_activity event so the
		// SubagentActivityFeed in the WebUI shows the subagent's LLM prose
		// output. The stream_chunk events are depth-suppressed for subagents
		// (see handleStreamChunk in useWebSocketEventHandler.ts), so this is
		// the only path that surfaces the subagent's actual response text.
		if subEventBus != nil {
			for _, raw := range rawLines {
				subEventBus.Publish(events.EventTypeSubagentActivity, events.SubagentActivityEvent(
					subTaskID, "llm_output", "output", raw,
					map[string]interface{}{
						"task_id":     subTaskID,
						"persona":     subPersona,
						"is_parallel": subIsParallel,
					},
				))
			}
		}
	})

	// Terminal writer for complete messages (tool logs, agent messages).
	// These bypass the line buffer and print immediately with prefix.
	subAgent.output.SetTerminalWriter(func(message string) {
		var pending []string
		outputMu.Lock()
		if lineBuf.Len() > 0 {
			remaining := strings.TrimSpace(lineBuf.String())
			if remaining != "" {
				pending = append(pending, dimGray+prefix+reset+" "+remaining+"\n")
			}
			lineBuf.Reset()
		}
		msg := strings.TrimRight(message, "\n")
		msg = strings.TrimSpace(msg)
		if msg != "" {
			pending = append(pending, dimGray+prefix+reset+" "+msg+"\n")
		}
		outputMu.Unlock()

		for _, line := range pending {
			_, _ = os.Stderr.Write([]byte(line))
		}
	})

	// Track the running subagent
	running := &runningSubagent{
		ID:        taskID,
		Persona:   opts.Persona,
		Prompt:    prompt,
		StartedAt: startTime,
		Ctx:       runCtx,
		Cancel:    cancel,
		Agent:     subAgent,
	}
	r.active.Store(taskID, running)

	// Token budget monitoring
	var budgetExceeded atomic.Bool
	if opts.MaxTokens > 0 {
		go r.monitorBudget(runCtx, subAgent, opts.MaxTokens, &budgetExceeded)
	}

	// Per-subagent progress monitoring: emit periodic activity events so
	// callers (CLI footer, WebUI panel) can show live context usage and
	// cost as the subagent runs. The runner-level ticker is cheap (one
	// goroutine per active subagent) and converges on the same
	// CurrentContextTokens / MaxContextTokens the parent's footer reads
	// — so the subagent and primary token displays use the same source
	// of truth. The event is suppressed when the subagent hasn't burned
	// any tokens yet (typical of the first ~1s) so the first frame the
	// user sees already has meaningful numbers.
	go r.monitorProgress(runCtx, subAgent, taskID, opts.Persona)

	rc := &subagentRunContext{
		runCtx:          runCtx,
		cancel:          cancel,
		subAgent:        subAgent,
		prefix:          prefix,
		dimGray:         dimGray,
		reset:           reset,
		eventBus:        eventBus,
		stopProgress:    stopProgress,
		progressSubName: progressSubName,
		progressLog:     &progressLog,
		progressMu:      &progressMu,
		lineBuf:         &lineBuf,
		outputMu:        outputMu,
		running:         running,
	}
	// Same pointer as the one monitorBudget writes to, so
	// finalizeSubagentResult sees the real Store() value.
	rc.budgetExceeded = &budgetExceeded

	return rc, nil
}

// finalizeSubagentResult enriches the raw SubagentResult with metrics,
// progress log, change tracker snapshot, and output-quality signals.
// It also cleans up the active tracking map entry.
func (r *SubagentRunner) finalizeSubagentResult(
	rc *subagentRunContext,
	result *SubagentResult,
	subAgent *Agent,
	taskID string,
	startTime time.Time,
	opts SubagentOptions,
) *SubagentResult {
	// Flush any remaining buffered output. Use TryLock instead of blocking
	// Lock: if the goroutine leaked (the 5-second grace above expired),
	// it may still be holding outputMu inside its streaming callback. A
	// blocking Lock here would hang runTask indefinitely — leaving
	// DecrementActiveSubagents un-fired ("1 sub" badge stuck in the
	// footer) and blocking the parent's tool result. Best-effort flush
	// on cancellation is fine; any buffered bytes are cosmetic.
	if rc.outputMu.TryLock() {
		if rc.lineBuf.Len() > 0 {
			remaining := strings.TrimSpace(rc.lineBuf.String())
			if remaining != "" {
				_, _ = os.Stderr.Write([]byte(rc.dimGray + rc.prefix + rc.reset + " " + remaining + "\n"))
			}
			rc.lineBuf.Reset()
		}
		rc.outputMu.Unlock()
	}

	// Mark as completed
	rc.running.Completed.Store(true)

	// Collect metrics from agent state
	tokensUsed := subAgent.state.GetTotalTokens()
	cost := subAgent.state.GetTotalCost()
	toolCalls := subAgent.state.GetTotalToolCalls()
	iterations := subAgent.state.GetCurrentIteration()

	// Determine cancellation status
	cancelled := rc.runCtx.Err() != nil && !rc.budgetExceeded.Load()

	// Merge metrics into result
	if result != nil {
		result.ID = taskID
		result.TokensUsed = tokensUsed
		result.Cost = cost
		result.ToolCalls = toolCalls
		result.Iterations = iterations
		result.Cancelled = cancelled
		result.BudgetExceeded = rc.budgetExceeded.Load()
		result.Truncated = subAgent.FleetBudgetExceeded()
		// SP-059 Phase 2c: snapshot the subagent's change tracker so
		// the parent can surface a structured FilesModified manifest
		// to the LLM. Snapshot is a defensive copy (GetChanges returns
		// a copy), safe to keep after the subagent is torn down.
		if tracker := subAgent.GetChangeTracker(); tracker != nil {
			result.FileChanges = tracker.GetChanges()
		}
		// SP-059 Phase 3a: copy the captured progress log into the
		// result. Snapshot under the mutex so a late event arriving
		// after subAgent.ProcessQuery returned can't race the read.
		rc.progressMu.Lock()
		if len(*rc.progressLog) > 0 {
			result.ProgressLog = make([]SubagentProgressEntry, len(*rc.progressLog))
			copy(result.ProgressLog, *rc.progressLog)
		}
		rc.progressMu.Unlock()

		// Output quality signal: set OutputComplete so the orchestrator can
		// distinguish "subagent did useful work" from "subagent ran but
		// produced nothing actionable". The orchestrator LLM sees this as
		// "output_complete" in the tool result envelope and can decide to
		// retry or escalate.
		result.OutputComplete = isOutputComplete(result)

		// Diagnostic for "subagent completed without returning anything":
		// when a subagent exits cleanly (no error, not cancelled, not
		// budget-exceeded) but its output is empty or too brief to convey
		// findings, the orchestrator gets nothing useful back. This Warn
		// line is the ground truth for tuning Phase 2's substance
		// thresholds — it tells us whether the issue is truly empty
		// Output="" or just short Output like "I've reviewed the files."
		// (which slips past the existing blank-retry check).
		//
		// 50 chars is deliberately aggressive for Phase 1: we want to catch
		// ALL short outputs (including legitimate brief answers) so we can
		// see the full distribution and tune Phase 2's threshold from real
		// data. Some false positives are expected — this is observation only.
		if result.Error == nil && !result.Cancelled && !result.BudgetExceeded && !result.Truncated {
			// len() counts bytes, not runes — CJK/emoji outputs (3-4 bytes/char)
			// hit the threshold faster, so non-ASCII brief responses are more
			// likely to trigger. Acceptable bias for Phase 1 data collection.
			trimmed := strings.TrimSpace(result.Output)
			if len(trimmed) < 50 {
				preview := trimmed
				// Safety guard: cap preview length. Dead code at threshold=50
				// (preview can't exceed 50 bytes here), but retained so Phase 2
				// can raise the threshold without re-introducing a truncation risk.
				if len(preview) > 200 {
					preview = preview[:200] + "..."
				}
				// Collapse newlines so the line stays grep-friendly.
				preview = strings.Map(func(runeVal rune) rune {
					if runeVal == '\n' || runeVal == '\r' {
						return ' '
					}
					return runeVal
				}, preview)
				if r.parentAgent != nil {
					r.parentAgent.Logger().Warn(
						"[subagent] %s task=%s completed with insufficient output: "+
							"len=%d iters=%d tool_calls=%d tokens=%d preview=%q",
						opts.Persona, taskID,
						len(trimmed), iterations, toolCalls, tokensUsed, preview,
					)
				}
			}
		}
	}

	// Clean up tracking
	r.active.Delete(taskID)

	return result
}

// runTask executes a single subagent task.  When cumulativeTokens is non-nil
// and fleetBudgetLimit > 0, the subagent will debit tokens to the shared
// fleet tracker after each LLM call and truncate gracefully when the budget
// is exceeded mid-run.
func (r *SubagentRunner) runTask(
	ctx context.Context,
	taskID string,
	prompt string,
	opts SubagentOptions,
	cumulativeTokens *atomic.Int64,
	fleetBudgetLimit int64,
) *SubagentResult {
	startTime := time.Now()

	// Apply a default timeout when the caller didn't set one explicitly.
	// Without this, a hung subagent blocks the primary indefinitely — no
	// caller in subagent_runners.go sets opts.Timeout. 20 minutes is
	// generous enough for locally-hosted models (which may be slower than
	// cloud APIs) while still bounding the worst case.
	if opts.Timeout <= 0 {
		opts.Timeout = 20 * time.Minute
	}

	// Setup
	rc, errResult := r.setupSubagentRun(ctx, taskID, prompt, opts, cumulativeTokens, fleetBudgetLimit, startTime)
	if errResult != nil {
		return errResult
	}
	subAgent := rc.subAgent

	// Cleanup defers — order matches the original:
	// 1. cancel the run context
	// 2. decrement active-subagent counter
	// 3. close stopProgress channel
	// 4. unsubscribe from event bus
	defer rc.cancel()
	defer DecrementActiveSubagents()
	defer close(rc.stopProgress)
	if rc.eventBus != nil && rc.progressSubName != "" {
		defer rc.eventBus.Unsubscribe(rc.progressSubName)
	}

	// Run the subagent in a goroutine with panic recovery
	done := make(chan *SubagentResult, 1)
	go func() {
		defer func() {
			if p := recover(); p != nil {
				done <- &SubagentResult{
					ID:      taskID,
					Error:   agenterrors.NewAgent("subagent.Runner", fmt.Sprintf("subagent panic: %v", p), nil),
					Elapsed: time.Since(startTime),
				}
			}
		}()

		output, err := subAgent.ProcessQuery(prompt)
		done <- &SubagentResult{
			ID:      taskID,
			Output:  output,
			Error:   err,
			Elapsed: time.Since(startTime),
		}
	}()

	// Wait for completion or cancellation
	var result *SubagentResult
	select {
	case result = <-done:
	case <-rc.runCtx.Done():
		// Cancelled or timed out
		rc.cancel()
		// Wait for goroutine to finish (with timeout).
		// If the grace expires, the goroutine has leaked — log it so the
		// operator can see why the agent appeared to pause.
		select {
		case result = <-done:
		case <-time.After(5 * time.Second):
			packageLogWarnf("[subagent] %s did not honor cancellation within 5s — goroutine leaked", taskID)
			result = &SubagentResult{
				ID:      taskID,
				Error:   agenterrors.NewAgent("subagent.Runner", "subagent did not respond to cancellation", nil),
				Elapsed: time.Since(startTime),
			}
		}
	}

	// Finalize
	return r.finalizeSubagentResult(rc, result, subAgent, taskID, startTime, opts)
}
