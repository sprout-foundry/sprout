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
// so that runTask can remain a thin orchestrator.
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

	// Create subagent, deriving its interrupt context from runCtx.
	subAgent, err := r.createSubagent(opts, runCtx)
	if err != nil {
		cancel()
		return nil, &SubagentResult{
			ID:      taskID,
			Error:   agenterrors.Wrap(err, "create subagent"),
			Elapsed: time.Since(startTime),
		}
	}

	// Share the parent's clarification manager and assign this subagent a clarification ID.
	if r.parentAgent != nil && r.parentAgent.clarificationManager != nil {
		subAgent.clarificationManager = r.parentAgent.clarificationManager
		subAgent.subagentID = taskID
	}

	// Bump the process-wide active-subagent counter for the CLI status footer.
	IncrementActiveSubagents()

	// Wire up per-LLM-call fleet budget tracking.
	if cumulativeTokens != nil && fleetBudgetLimit > 0 {
		subAgent.SetFleetBudget(cumulativeTokens, fleetBudgetLimit)
	}

	// Propagate the parent's USD budget to this subagent.
	if r.parentAgent != nil {
		if usd := r.parentAgent.GetFleetUsdBudget(); usd != nil {
			subAgent.SetFleetUsdBudget(usd)
		}
	}

	// Set up terminal output prefixing for subagent.
	prefix := buildSubagentPrefix(opts.Persona, taskID)
	dimGray := "\033[90m"
	reset := "\033[0m"
	if !envutil.ResolveColorPreference(true) {
		dimGray = ""
		reset = ""
	}

	// Create OutputRouter with the shared eventBus for subagent events.
	eventBus := r.shared.EventBus
	router := NewOutputRouter(subAgent, eventBus)
	subAgent.output.SetOutputRouter(router)

	// Capture a per-run progress log by subscribing to the shared event bus.
	// Bounded to subagentProgressLogCap entries (head-trimmed).
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
						// Head-trim so the most recent entries are always visible.
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
	var outputMu *sync.Mutex
	if r.parentAgent != nil && r.parentAgent.output != nil {
		outputMu = r.parentAgent.output.GetOutputMutex()
	}
	if outputMu == nil {
		outputMu = &sync.Mutex{}
		subAgent.output.SetOutputMutex(outputMu)
	}

	// Line buffer for accumulating stream chunks.
	var lineBuf strings.Builder

	// Capture task metadata for publishing subagent_activity events.
	subPersona := opts.Persona
	subTaskID := taskID
	subEventBus := eventBus
	subIsParallel := !strings.HasPrefix(taskID, "subagent-")
	subAgent.EnableStreaming(func(chunk string) {
		var pending []string
		var rawLines []string // mirror of pending without ANSI/prefix formatting
		// RouteStreamChunk holds outputMu before calling this callback.
		// Using TryLock avoids re-entrancy deadlock.
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
		// Publish each complete line as a subagent_activity event for the WebUI feed.
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

	// Per-subagent progress monitoring: emit periodic activity events.
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
func (r *SubagentRunner) finalizeSubagentResult(
	rc *subagentRunContext,
	result *SubagentResult,
	subAgent *Agent,
	taskID string,
	startTime time.Time,
	opts SubagentOptions,
) *SubagentResult {
	// Flush any remaining buffered output. Use TryLock to avoid deadlock
	// if the goroutine leaked and still holds outputMu.
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
		// Snapshot the subagent's change tracker for the parent.
		if tracker := subAgent.GetChangeTracker(); tracker != nil {
			result.FileChanges = tracker.GetChanges()
		}
		// Copy the captured progress log into the result.
		rc.progressMu.Lock()
		if len(*rc.progressLog) > 0 {
			result.ProgressLog = make([]SubagentProgressEntry, len(*rc.progressLog))
			copy(result.ProgressLog, *rc.progressLog)
		}
		rc.progressMu.Unlock()

		// Output quality signal: set OutputComplete so the orchestrator can
		// distinguish "subagent did useful work" from "produced nothing actionable".
		result.OutputComplete = isOutputComplete(result)

		// Diagnostic: warn when a subagent exits cleanly but produces brief output.
		if result.Error == nil && !result.Cancelled && !result.BudgetExceeded && !result.Truncated {
			trimmed := strings.TrimSpace(result.Output)
			if len(trimmed) < 50 {
				preview := trimmed
				if len(preview) > 200 {
					preview = preview[:200] + "..."
				}
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

// runTask executes a single subagent task.
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
