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
)

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

	// Create context with optional timeout
	var runCtx context.Context
	var cancel context.CancelFunc
	if opts.Timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, opts.Timeout)
	} else {
		runCtx, cancel = context.WithCancel(ctx)
	}
	defer cancel()

	// Create subagent
	subAgent, err := r.createSubagent(opts)
	if err != nil {
		return &SubagentResult{
			ID:      taskID,
			Error:   fmt.Errorf("create subagent: %w", err),
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
	defer DecrementActiveSubagents()

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
	// CRITICAL: order matters here. Unsubscribe BEFORE closing stopProgress
	// so the bus stops trying to write to our channel before our consumer
	// goroutine exits. The reverse order leaks the subscriber registration:
	// stop the consumer, leave the channel in eb.subscribers, bus keeps
	// writing, channel fills past cap=100, every subsequent publish on
	// every event type spams "Dropped X event for slow subscriber". With
	// long-running nested EA workflows that's many subscribers leaking, one
	// per spawned subagent — minutes of log noise per session.
	defer close(stopProgress)
	if eventBus != nil && progressSubName != "" {
		defer eventBus.Unsubscribe(progressSubName)
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
	subAgent.EnableStreaming(func(chunk string) {
		var pending []string
		outputMu.Lock()
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
			}
			lineBuf.Reset()
			if idx+1 < len(content) {
				lineBuf.WriteString(content[idx+1:])
			}
		}
		outputMu.Unlock()

		for _, line := range pending {
			_, _ = os.Stderr.Write([]byte(line))
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

	// Run the subagent in a goroutine with panic recovery
	done := make(chan *SubagentResult, 1)
	go func() {
		defer func() {
			if p := recover(); p != nil {
				done <- &SubagentResult{
					ID:      taskID,
					Error:   fmt.Errorf("subagent panic: %v", p),
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
	case <-runCtx.Done():
		// Cancelled or timed out
		cancel()
		// Wait for goroutine to finish (with timeout).
		// If the grace expires, the goroutine has leaked — log it so the
		// operator can see why the agent appeared to pause.
		select {
		case result = <-done:
		case <-time.After(5 * time.Second):
			packageLogWarnf("[subagent] %s did not honor cancellation within 5s — goroutine leaked", taskID)
			result = &SubagentResult{
				ID:      taskID,
				Error:   fmt.Errorf("subagent did not respond to cancellation"),
				Elapsed: time.Since(startTime),
			}
		}
	}

	// Flush any remaining buffered output
	outputMu.Lock()
	if lineBuf.Len() > 0 {
		remaining := strings.TrimSpace(lineBuf.String())
		if remaining != "" {
			_, _ = os.Stderr.Write([]byte(dimGray + prefix + reset + " " + remaining + "\n"))
		}
		lineBuf.Reset()
	}
	outputMu.Unlock()

	// Mark as completed
	running.Completed.Store(true)

	// Collect metrics from agent state
	tokensUsed := subAgent.state.GetTotalTokens()
	cost := subAgent.state.GetTotalCost()
	toolCalls := subAgent.state.GetTotalToolCalls()
	iterations := subAgent.state.GetCurrentIteration()

	// Determine cancellation status
	cancelled := runCtx.Err() != nil && !budgetExceeded.Load()

	// Merge metrics into result
	if result != nil {
		result.ID = taskID
		result.TokensUsed = tokensUsed
		result.Cost = cost
		result.ToolCalls = toolCalls
		result.Iterations = iterations
		result.Cancelled = cancelled
		result.BudgetExceeded = budgetExceeded.Load()
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
		progressMu.Lock()
		if len(progressLog) > 0 {
			result.ProgressLog = make([]SubagentProgressEntry, len(progressLog))
			copy(result.ProgressLog, progressLog)
		}
		progressMu.Unlock()
	}

	// Clean up tracking
	r.active.Delete(taskID)

	return result
}
