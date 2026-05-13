package agent

import (
	"context"
	"fmt"
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
	ID       string
	Prompt   string
	Model    string
	Provider string
	Persona  string
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

// RunParallel spawns multiple subagents concurrently
func (r *SubagentRunner) RunParallel(ctx context.Context, tasks []SubagentTask, opts SubagentOptions) []*SubagentResult {
	if len(tasks) == 0 {
		return nil
	}

	results := make([]*SubagentResult, len(tasks))
	var wg sync.WaitGroup

	for i, task := range tasks {
		wg.Add(1)
		go func(idx int, t SubagentTask) {
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
			results[idx] = r.runTask(ctx, t.ID, t.Prompt, taskOpts)
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
		// Wait for goroutine to finish (with timeout)
		select {
		case result = <-done:
		case <-time.After(5 * time.Second):
			result = &SubagentResult{
				ID:      taskID,
				Error:   fmt.Errorf("subagent did not respond to cancellation"),
				Elapsed: time.Since(startTime),
			}
		}
	}

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
		workspaceRoot:       r.shared.WorkspaceRoot,
		state:               stateMgr,
		output:              outputMgr,
		security:            securityMgr,
		mcpSub:              mcpMgr,
		// Shared resources
		todoMgr:       r.shared.TodoManager,
		eventBus:      r.shared.EventBus,
		embeddingMgr:  r.shared.EmbeddingMgr,
	}

	// Set isSubagent=true so the subagent knows it's a subagent.
	// This prevents nested subagent spawning and skips interactive prompts.
	agent.isSubagent = true

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
