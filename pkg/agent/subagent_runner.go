// Package agent provides subagent management via the SubagentRunner, which supports
// both serial (Run) and parallel (RunParallel) execution of subagent tasks.
//
// SubagentRunner Concurrency Invariants:
//
//   - MaxConcurrentSubagents: When > 0, a buffered channel semaphore limits the number
//     of concurrently executing subagents. Tasks waiting for a slot respect parent context
//     cancellation and return Cancelled=true.
//
//   - FleetTokenBudget: When > 0, tracks cumulative token usage across the fleet via
//     atomic.Int64. Once the budget is reached, not-yet-started tasks are skipped with
//     BudgetExceeded=true. Currently running tasks are NOT interrupted.
//
//   - Order Preservation: RunParallel returns results in the same order as the input tasks,
//     regardless of execution order.
package agent

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	agent_api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/embedding"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/factory"
	"github.com/sprout-foundry/sprout/pkg/utils"
)

// SubagentOptions configures an in-process subagent
type SubagentOptions struct {
	Persona      string          // "coder", "tester", "debugger", etc.
	Model        string          // optional model override
	Provider     string          // optional provider override
	SystemPrompt string          // optional system prompt override
	MaxTokens    int             // token budget (0 = unlimited)
	Timeout      time.Duration   // execution timeout (0 = unlimited)
	WorkingDir             string          // optional: override workspace root (must be within $HOME)
	MaxConcurrentSubagents int             // max parallel subagents (0 = unlimited, default unlimited)
	FleetTokenBudget       int             // shared token budget across all parallel subagents (0 = unlimited)
}

// SharedState holds resources shared between parent and subagents
type SharedState struct {
	EventBus      *events.EventBus
	TodoManager   *tools.TodoManager
	EmbeddingMgr  *embedding.EmbeddingManager
	ConfigManager *configuration.Manager
	WorkspaceRoot string
}

// SubagentResult is the structured output from a subagent
type SubagentResult struct {
	ID              string
	Output          string
	Error           error
	TokensUsed      int
	Cost            float64
	ToolCalls       int
	Elapsed         time.Duration
	Cancelled       bool
	BudgetExceeded  bool  // true if task was skipped because fleet budget was already exceeded before starting
	Truncated       bool  // true if subagent was cut short due to fleet budget exceeded mid-run
}

// SubagentTask represents a single parallel subagent task
type SubagentTask struct {
	ID         string
	Prompt     string
	Model      string
	Provider   string
	Persona    string
	WorkingDir string // optional: override workspace root
}

// SubagentMetrics tracks operational metrics for the subagent runner.
type SubagentMetrics struct {
	Active            int64 // Currently executing subagents
	Queued            int64 // Waiting for semaphore slot
	Completed         int64 // Successfully completed
	Failed            int64 // Completed with error
	Cancelled         int64 // Cancelled (parent ctx or budget)
	TotalQueuedWaitMS int64 // Cumulative milliseconds spent waiting in queue
}

// SubagentRunner manages in-process subagent execution
type SubagentRunner struct {
	parentAgent *Agent
	shared      *SharedState
	active      sync.Map // taskID -> *runningSubagent

	// Operational metrics (atomic for concurrent access)
	metricActive       atomic.Int64
	metricQueued       atomic.Int64
	metricCompleted    atomic.Int64
	metricFailed       atomic.Int64
	metricCancelled    atomic.Int64
	metricQueuedWaitMS atomic.Int64

	// testClientFactory overrides client creation for testing only.
	// When non-nil, it is called instead of factory.CreateProviderClient.
	// This field is never set in production code.
	testClientFactory func(clientType agent_api.ClientType, model string) (agent_api.ClientInterface, error)
}

// runningSubagent tracks an active subagent execution
type runningSubagent struct {
	ID        string
	Persona   string
	Prompt    string
	StartedAt time.Time
	Agent     *Agent
	Ctx       context.Context
	Cancel    context.CancelFunc
	Completed atomic.Bool
}

// buildSubagentPrefix returns the terminal prefix for a subagent based on persona and taskID.
// For single subagents (taskID starting with "subagent-"), returns "[{persona}]".
// For parallel subagents (other taskIDs), returns "[{persona}:{taskID}]".
func buildSubagentPrefix(persona, taskID string) string {
	if taskID != "" && !strings.HasPrefix(taskID, "subagent-") {
		return fmt.Sprintf("[%s:%s]", persona, taskID)
	}
	return fmt.Sprintf("[%s]", persona)
}

// activeSubagentCount is the process-wide count of currently-running
// subagents. The CLI status footer reads it via GetActiveSubagents() to
// render " · N sub" while delegation is in flight. SP-051-2d.
var activeSubagentCount atomic.Int64

// IncrementActiveSubagents bumps the active-subagent counter; paired with
// DecrementActiveSubagents under a defer in the spawner.
func IncrementActiveSubagents() { activeSubagentCount.Add(1) }

// DecrementActiveSubagents lowers the active-subagent counter when a
// subagent finishes (success, error, cancel — any terminal state).
func DecrementActiveSubagents() { activeSubagentCount.Add(-1) }

// GetActiveSubagents returns the current number of running subagents.
func GetActiveSubagents() int { return int(activeSubagentCount.Load()) }

// NewSubagentRunner creates a new SubagentRunner
func NewSubagentRunner(parent *Agent, shared *SharedState) *SubagentRunner {
	return &SubagentRunner{
		parentAgent: parent,
		shared:      shared,
	}
}

// Metrics returns a snapshot of the subagent runner's operational metrics.
func (r *SubagentRunner) Metrics() SubagentMetrics {
	return SubagentMetrics{
		Active:            r.metricActive.Load(),
		Queued:            r.metricQueued.Load(),
		Completed:         r.metricCompleted.Load(),
		Failed:            r.metricFailed.Load(),
		Cancelled:         r.metricCancelled.Load(),
		TotalQueuedWaitMS: r.metricQueuedWaitMS.Load(),
	}
}

// publishLifecycleEvent emits a subagent_activity event with a status field
// describing the lifecycle transition. The event is only published when
// the shared EventBus is available.
func (r *SubagentRunner) publishLifecycleEvent(taskID, persona, status, reason string, tokensUsed int, elapsedMs int64) {
	if r.shared == nil || r.shared.EventBus == nil {
		return
	}
	data := map[string]any{
		"task_id": taskID,
		"persona": persona,
		"status":  status, // "queued", "started", "completed", "cancelled"
	}
	if reason != "" {
		data["reason"] = reason
	}
	if tokensUsed > 0 {
		data["tokens_used"] = tokensUsed
	}
	if elapsedMs > 0 {
		data["elapsed_ms"] = elapsedMs
	}
	r.shared.EventBus.Publish(events.EventTypeSubagentActivity, data)

	// Also write to the runlog for persistent structured logging.
	logger := utils.GetRunLogger()
	if logger != nil {
		logger.LogEvent("subagent_activity", data)
	}
}

// Run spawns an in-process subagent and waits for completion
func (r *SubagentRunner) Run(ctx context.Context, prompt string, opts SubagentOptions) *SubagentResult {
	taskID := fmt.Sprintf("subagent-%d", time.Now().UnixNano())
	return r.runTask(ctx, taskID, prompt, opts, nil, 0)
}

// RunParallel spawns multiple subagents concurrently.
// If the parent context is cancelled, remaining subagents are cancelled
// and their results are set to cancellation errors.
func (r *SubagentRunner) RunParallel(ctx context.Context, tasks []SubagentTask, opts SubagentOptions) []*SubagentResult {
	if len(tasks) == 0 {
		return nil
	}

	results := make([]*SubagentResult, len(tasks))
	var wg sync.WaitGroup

	// Create a derived context so we can cancel remaining subagents
	// when the parent context is cancelled or when we detect early
	// termination is needed.
	parallelCtx, parallelCancel := context.WithCancel(ctx)
	defer parallelCancel()

	// Semaphore for limiting concurrent subagents
	var sem chan struct{}
	if opts.MaxConcurrentSubagents > 0 {
		sem = make(chan struct{}, opts.MaxConcurrentSubagents)
	}

	// Fleet token budget tracking
	var cumulativeTokens atomic.Int64

	for i, task := range tasks {
		wg.Add(1)
		go func(idx int, t SubagentTask) {
			// Resolve persona early so all lifecycle events use the same value
			persona := opts.Persona
			if t.Persona != "" {
				persona = t.Persona
			}

			r.metricQueued.Add(1)
			queueStart := time.Now()

			// Emit: queued
			r.publishLifecycleEvent(t.ID, persona, "queued", "", 0, 0)

			// Acquire semaphore (if limited), respecting context cancellation
			if sem != nil {
				select {
				case sem <- struct{}{}:
					defer func() { <-sem }()
				case <-parallelCtx.Done():
					r.metricQueued.Add(-1)
					r.metricCancelled.Add(1)
					r.publishLifecycleEvent(t.ID, persona, "cancelled", "context_cancelled", 0, 0)
					defer wg.Done()
					results[idx] = &SubagentResult{
						ID:        t.ID,
						Error:     parallelCtx.Err(),
						Cancelled: true,
					}
					return
				}
			}

			// Track queue wait time and transition from queued to active
			r.metricQueuedWaitMS.Add(int64(time.Since(queueStart).Milliseconds()))
			r.metricQueued.Add(-1)
			r.metricActive.Add(1)

			// Budget check after acquiring semaphore, before starting work
			if opts.FleetTokenBudget > 0 && cumulativeTokens.Load() >= int64(opts.FleetTokenBudget) {
				r.metricActive.Add(-1)
				r.metricCancelled.Add(1)
				r.publishLifecycleEvent(t.ID, persona, "cancelled", "budget_exceeded", 0, 0)
				defer wg.Done()
				results[idx] = &SubagentResult{
					ID:             t.ID,
					Error:          fmt.Errorf("fleet token budget exceeded"),
					BudgetExceeded: true,
				}
				return
			}

			// Emit: started
			r.publishLifecycleEvent(t.ID, persona, "started", "", 0, 0)

			defer wg.Done()
			taskOpts := opts
			if t.Model != "" {
				taskOpts.Model = t.Model
			}
			if t.Provider != "" {
				taskOpts.Provider = t.Provider
			}
			if t.Persona != "" {
				taskOpts.Persona = t.Persona
			}
			if t.WorkingDir != "" {
				taskOpts.WorkingDir = t.WorkingDir
			}
			result := r.runTask(parallelCtx, t.ID, t.Prompt, taskOpts, &cumulativeTokens, int64(opts.FleetTokenBudget))
			results[idx] = result
			if result != nil {
				cumulativeTokens.Add(int64(result.TokensUsed))
				if result.Cancelled {
					r.metricActive.Add(-1)
					r.metricCancelled.Add(1)
				} else if result.Error != nil {
					r.metricActive.Add(-1)
					r.metricFailed.Add(1)
				} else {
					r.metricActive.Add(-1)
					r.metricCompleted.Add(1)
				}

				// Emit: completed / cancelled after runTask returns
				completedStatus := "completed"
				completedReason := ""
				if result.Cancelled {
					completedStatus = "cancelled"
					completedReason = "context_cancelled"
				} else if result.BudgetExceeded {
					completedStatus = "cancelled"
					completedReason = "budget_exceeded"
				}
				r.publishLifecycleEvent(t.ID, persona, completedStatus, completedReason, result.TokensUsed, result.Elapsed.Milliseconds())
			}
		}(i, task)
	}

	wg.Wait()
	return results
}

// GetActiveSubagents returns information about currently running subagents
func (r *SubagentRunner) GetActiveSubagents() []*runningSubagent {
	var active []*runningSubagent
	r.active.Range(func(key, value interface{}) bool {
		if sub, ok := value.(*runningSubagent); ok {
			if !sub.Completed.Load() {
				active = append(active, sub)
			}
		}
		return true
	})
	return active
}

// CancelSubagent cancels a specific running subagent by ID
func (r *SubagentRunner) CancelSubagent(id string) bool {
	if val, ok := r.active.Load(id); ok {
		if sub, ok := val.(*runningSubagent); ok {
			sub.Cancel()
			return true
		}
	}
	return false
}

// CancelAll cancels all running subagents
func (r *SubagentRunner) CancelAll() {
	r.active.Range(func(key, value interface{}) bool {
		if sub, ok := value.(*runningSubagent); ok {
			if !sub.Completed.Load() {
				sub.Cancel()
			}
		}
		return true
	})
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

	// Set up terminal output prefixing for subagent
	prefix := buildSubagentPrefix(opts.Persona, taskID)
	const dimGray = "\033[90m"
	const reset = "\033[0m"

	// Create OutputRouter with the shared eventBus so subagent events
	// (stream_chunk, agent_message, tool_log, etc.) are published to the
	// event bus when in WebUI mode.
	eventBus := r.shared.EventBus
	router := NewOutputRouter(subAgent, eventBus)
	subAgent.output.SetOutputRouter(router)

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
			log.Printf("[subagent] %s did not honor cancellation within 5s — goroutine leaked", taskID)
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

	// Determine cancellation status
	cancelled := runCtx.Err() != nil && !budgetExceeded.Load()

	// Merge metrics into result
	if result != nil {
		result.ID = taskID
		result.TokensUsed = tokensUsed
		result.Cost = cost
		result.ToolCalls = toolCalls
		result.Cancelled = cancelled
		result.BudgetExceeded = budgetExceeded.Load()
		result.Truncated = subAgent.FleetBudgetExceeded()
	}

	// Clean up tracking
	r.active.Delete(taskID)

	return result
}

// createSubagent creates a new in-process agent for subagent execution
func (r *SubagentRunner) createSubagent(opts SubagentOptions) (*Agent, error) {
	if r.shared == nil || r.shared.ConfigManager == nil {
		return nil, fmt.Errorf("shared state and config manager are required")
	}

	// Resolve provider/model: use opts overrides, then parent agent, then config defaults
	provider := opts.Provider
	model := opts.Model

	if provider == "" && r.parentAgent != nil {
		parentProvider := r.parentAgent.GetProvider()
		if parentProvider != "" && parentProvider != "unknown" {
			provider = parentProvider
		}
	}
	if model == "" && r.parentAgent != nil {
		parentModel := r.parentAgent.GetModel()
		if parentModel != "" && parentModel != "unknown" {
			model = parentModel
		}
	}

	// Resolve client type from config
	clientType, finalModel, err := r.shared.ConfigManager.ResolveProviderModel(provider, model)
	if err != nil {
		return nil, fmt.Errorf("resolve provider/model: %w", err)
	}

	// Create client via factory (or test hook for testing)
	var client agent_api.ClientInterface
	if r.testClientFactory != nil {
		client, err = r.testClientFactory(clientType, finalModel)
	} else {
		client, err = factory.CreateProviderClient(clientType, finalModel)
	}
	if err != nil {
		return nil, fmt.Errorf("create client: %w", err)
	}

	// Build system prompt
	systemPrompt := opts.SystemPrompt
	if systemPrompt == "" {
		// Use a minimal default for subagents
		systemPrompt = "You are a helpful coding assistant that can execute tools to complete tasks."
	}

	// Determine effective workspace root
	effectiveWorkspaceRoot := r.shared.WorkspaceRoot
	if opts.WorkingDir != "" {
		effectiveWorkspaceRoot = opts.WorkingDir
	}

	// Create interrupt context for this subagent
	interruptCtx, interruptCancel := context.WithCancel(context.Background())

	// Create sub-managers
	stateMgr := NewAgentStateManager(false)
	outputMgr := NewAgentOutputManager()
	securityMgr := NewAgentSecurityManager()
	mcpMgr := NewAgentMCPManager()

	// Construct the agent struct
	agent := &Agent{
		client:              client,
		systemPrompt:        systemPrompt,
		baseSystemPrompt:    systemPrompt,
		maxIterations:       0, // unlimited
		clientType:          clientType,
		debug:               r.parentAgent != nil && r.parentAgent.debug,
		configManager:       r.shared.ConfigManager,
		shellCommandHistory: make(map[string]*ShellCommandResult),
		inputInjectionChan:  make(chan string, inputInjectionBufferSize),
		interruptCtx:        interruptCtx,
		interruptCancel:     interruptCancel,
		workspaceRoot:       effectiveWorkspaceRoot,
		state:               stateMgr,
		output:              outputMgr,
		security:            securityMgr,
		mcpSub:              mcpMgr,
		// Shared resources
		todoMgr:       r.shared.TodoManager,
		eventBus:      r.shared.EventBus,
		embeddingMgr:  r.shared.EmbeddingMgr,
	}

	// Inherit the parent's TerminalManager. Without this, subagents (and
	// recursively their own subagents) try to call shell_command with
	// background=true / check_background / stop_background and fail with
	// "background mode requires WebUI terminal manager" even though the
	// root agent has a TerminalManager attached. The TerminalManager is
	// process-scoped (one per WebUI server); chat IDs route work to the
	// right session pool, so direct inheritance by reference is correct.
	if r.parentAgent != nil {
		if tm := r.parentAgent.GetTerminalManager(); tm != nil {
			agent.terminalManager = tm
		}
	}

	// Set subagentDepth based on parent's depth + 1.
	// This enables configurable nesting: EA (0) → orchestrator (1) → coder/tester (2).
	agent.subagentDepth = r.parentAgent.subagentDepth + 1

	// Propagate rootPersonaID from parent so depth limits can vary by root persona.
	if r.parentAgent.rootPersonaID != "" {
		agent.rootPersonaID = r.parentAgent.rootPersonaID
	}

	// SP-058: propagate the active risk-profile override so the user's
	// session-level --risk-profile (or per-step workflow override)
	// continues to apply inside subagents. Without this the subagent
	// would fall back to the config-level setting and a user who set
	// --risk-profile=readonly would find subagents running under the
	// config default instead — silently bypassing their intent. The
	// readonly profile's DefaultRisk=Critical still blocks subagent
	// writes (Critical is checked before the IsSubagent auto-approve),
	// so this propagation is what makes readonly actually readonly
	// during delegation.
	agent.riskProfileOverride = r.parentAgent.riskProfileOverride

	// SP-051: tag every event this subagent publishes with depth + persona
	// so the CLI tool-timeline can indent and color-badge by who's running.
	// Merge (not replace) so parent-set chat/client/user routing keys still
	// flow through subagent events to the right WebUI client.
	parentMeta := r.parentAgent.output.GetEventMetadata()
	merged := make(map[string]interface{}, len(parentMeta)+3)
	for k, v := range parentMeta {
		merged[k] = v
	}
	merged["subagent_depth"] = agent.subagentDepth
	if persona := strings.TrimSpace(opts.Persona); persona != "" {
		merged["active_persona"] = persona
	}
	agent.SetEventMetadata(merged)

	return agent, nil
}

// monitorBudget watches token usage and cancels if budget exceeded
func (r *SubagentRunner) monitorBudget(ctx context.Context, agent *Agent, maxTokens int, budgetExceeded *atomic.Bool) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tokens := agent.state.GetTotalTokens()
			if tokens >= maxTokens {
				budgetExceeded.Store(true)
				agent.interruptCancel()
				return
			}
		}
	}
}
