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
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/embedding"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/factory"
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
	ID           string
	Output       string
	Error        error
	TokensUsed   int
	Cost         float64
	ToolCalls    int
	Elapsed      time.Duration
	Cancelled    bool
	BudgetExceeded bool
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

// SubagentRunner manages in-process subagent execution
type SubagentRunner struct {
	parentAgent *Agent
	shared      *SharedState
	active      sync.Map // taskID -> *runningSubagent
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

// NewSubagentRunner creates a new SubagentRunner
func NewSubagentRunner(parent *Agent, shared *SharedState) *SubagentRunner {
	return &SubagentRunner{
		parentAgent: parent,
		shared:      shared,
	}
}

// Run spawns an in-process subagent and waits for completion
func (r *SubagentRunner) Run(ctx context.Context, prompt string, opts SubagentOptions) *SubagentResult {
	taskID := fmt.Sprintf("subagent-%d", time.Now().UnixNano())
	return r.runTask(ctx, taskID, prompt, opts)
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
			// Acquire semaphore (if limited), respecting context cancellation
			if sem != nil {
				select {
				case sem <- struct{}{}:
					defer func() { <-sem }()
				case <-parallelCtx.Done():
					defer wg.Done()
					results[idx] = &SubagentResult{
						ID:        t.ID,
						Error:     parallelCtx.Err(),
						Cancelled: true,
					}
					return
				}
			}

			// Budget check after acquiring semaphore, before starting work
			if opts.FleetTokenBudget > 0 && cumulativeTokens.Load() >= int64(opts.FleetTokenBudget) {
				defer wg.Done()
				results[idx] = &SubagentResult{
					ID:             t.ID,
					Error:          fmt.Errorf("fleet token budget exceeded"),
					BudgetExceeded: true,
				}
				return
			}

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
			result := r.runTask(parallelCtx, t.ID, t.Prompt, taskOpts)
			results[idx] = result
			if result != nil {
				cumulativeTokens.Add(int64(result.TokensUsed))
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

// runTask executes a single subagent task
func (r *SubagentRunner) runTask(ctx context.Context, taskID, prompt string, opts SubagentOptions) *SubagentResult {
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
	var budgetExceeded bool
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
	cancelled := runCtx.Err() != nil && !budgetExceeded

	// Merge metrics into result
	if result != nil {
		result.ID = taskID
		result.TokensUsed = tokensUsed
		result.Cost = cost
		result.ToolCalls = toolCalls
		result.Cancelled = cancelled
		result.BudgetExceeded = budgetExceeded
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

	// Create client via factory
	client, err := factory.CreateProviderClient(clientType, finalModel)
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

	return agent, nil
}

// monitorBudget watches token usage and cancels if budget exceeded
func (r *SubagentRunner) monitorBudget(ctx context.Context, agent *Agent, maxTokens int, budgetExceeded *bool) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tokens := agent.state.GetTotalTokens()
			if tokens >= maxTokens {
				*budgetExceeded = true
				agent.interruptCancel()
				return
			}
		}
	}
}
