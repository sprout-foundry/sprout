package orchestration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/alantheprice/ledit/pkg/agent"
	"github.com/alantheprice/ledit/pkg/changetracker"
	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/orchestration/types"
	"github.com/alantheprice/ledit/pkg/prompts"
	ui "github.com/alantheprice/ledit/pkg/ui"
	"github.com/alantheprice/ledit/pkg/utils"
)

// MultiAgentOrchestrator manages the execution of multiple agents with different personas
type MultiAgentOrchestrator struct {
	plan        *types.MultiAgentOrchestrationPlan
	config      *config.Config
	logger      *utils.Logger
	agents      map[string]*AgentRunner
	stepDeps    map[string][]string // step ID -> dependent step IDs
	settings    *types.ProcessSettings
	validation  *types.ValidationConfig
	statePath   string
	concurrency int
	resume      bool
}

// AgentRunner manages a single agent instance
type AgentRunner struct {
	definition *types.AgentDefinition
	status     *types.AgentStatus
	config     *config.Config
	logger     *utils.Logger
}

// NewMultiAgentOrchestrator creates a new orchestrator for multi-agent processes
func NewMultiAgentOrchestrator(processFile *types.ProcessFile, cfg *config.Config, logger *utils.Logger, resume bool, statePath string) *MultiAgentOrchestrator {
	// Initialize agent statuses
	agentStatuses := make(map[string]types.AgentStatus)
	for _, agentDef := range processFile.Agents {
		agentStatuses[agentDef.ID] = types.AgentStatus{
			Status:      "idle",
			CurrentStep: "",
			Progress:    0,
			LastUpdate:  time.Now().Format(time.RFC3339),
			Errors:      []string{},
			Output:      make(map[string]string),
		}
	}

	// Create the orchestration plan
	plan := &types.MultiAgentOrchestrationPlan{
		Goal:          processFile.Goal,
		BaseModel:     processFile.BaseModel,
		Agents:        processFile.Agents,
		Steps:         processFile.Steps,
		CurrentStep:   0,
		Status:        "pending",
		AgentStatuses: agentStatuses,
		Attempts:      0,
		LastError:     "",
		CreatedAt:     time.Now().Format(time.RFC3339),
		CompletedAt:   "",
	}

	// Build dependency graph for steps
	stepDeps := buildStepDependencies(processFile.Steps)

	orch := &MultiAgentOrchestrator{
		plan:       plan,
		config:     cfg,
		logger:     logger,
		agents:     make(map[string]*AgentRunner),
		stepDeps:   stepDeps,
		settings:   processFile.Settings,
		validation: processFile.Validation,
		statePath:  filepath.Join(".ledit", "orchestration_state.json"),
		resume:     resume,
	}
	if strings.TrimSpace(statePath) != "" {
		orch.statePath = statePath
	}
	// Concurrency: if parallel execution is enabled, default to min(4, NumCPU)
	if orch.settings != nil && orch.settings.ParallelExecution {
		max := runtime.NumCPU()
		if max > 4 {
			max = 4
		}
		if max < 1 {
			max = 1
		}
		orch.concurrency = max
	} else {
		orch.concurrency = 1
	}
	return orch
}

// Execute runs the multi-agent orchestration process
func (o *MultiAgentOrchestrator) Execute() error {
	o.logger.LogProcessStep("🚀 Starting multi-agent orchestration process")
	o.logger.LogProcessStep(fmt.Sprintf("Goal: %s", o.plan.Goal))
	o.logger.LogProcessStep(fmt.Sprintf("Agents: %d, Steps: %d", len(o.plan.Agents), len(o.plan.Steps)))

	// Save initial state so tests can verify process started
	o.plan.Status = "in_progress"
	o.plan.CreatedAt = time.Now().Format(time.RFC3339)
	_ = o.saveState()

	// Attempt resume from previous state if requested
	if o.resume {
		if err := o.loadStateIfCompatible(); err == nil {
			// Log brief resume summary
			completed := 0
			for _, s := range o.plan.Steps {
				if s.Status == "completed" {
					completed++
				}
			}
			o.logger.LogProcessStep("♻️ Loaded previous orchestration state; resuming...")
			o.logger.LogProcessStep(fmt.Sprintf("   Progress: %d/%d steps completed", completed, len(o.plan.Steps)))
			runnable := o.listRunnableStepIDs()
			if len(runnable) > 0 {
				o.logger.LogProcessStep(fmt.Sprintf("   Next runnable steps: %s", strings.Join(runnable, ", ")))
			} else {
				o.logger.LogProcessStep("   No steps currently runnable (waiting on dependencies or all done)")
			}
		} else if !os.IsNotExist(err) {
			o.logger.LogProcessStep(fmt.Sprintf("ℹ️ Could not resume from state: %v (starting fresh)", err))
		}
	} else {
		// Not resuming; if a state file exists, inform that it will be ignored
		if _, err := os.Stat(o.statePath); err == nil {
			o.logger.LogProcessStep("ℹ️ State file detected but --resume not set; ignoring previous state")
		}
	}

	// Initialize all agents
	if err := o.initializeAgents(); err != nil {
		return fmt.Errorf("failed to initialize agents: %w", err)
	}
	o.printProgressTable()

	// Execute steps with dependency handling, retries, timeouts, and optional parallelism
	if err := o.executeSteps(); err != nil {
		return fmt.Errorf("failed to execute steps: %w", err)
	}

	// Validate final results
	if err := o.validateResults(); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	// Run configured validation commands
	if err := o.runValidationStage(); err != nil {
		return err
	}

	o.plan.Status = "completed"
	o.plan.CompletedAt = time.Now().Format(time.RFC3339)
	o.logger.LogProcessStep("✅ Multi-agent orchestration completed successfully")

	// Persist final state
	_ = o.saveState()

	return nil
}

// initializeAgents sets up all agent runners
func (o *MultiAgentOrchestrator) initializeAgents() error {
	o.logger.LogProcessStep("🔧 Initializing agents...")

	for i := range o.plan.Agents {
		agentDef := &o.plan.Agents[i]
		// Create agent-specific config
		agentConfig := o.createAgentConfig(agentDef)

		agentStatus := o.plan.AgentStatuses[agentDef.ID]
		agentRunner := &AgentRunner{
			definition: agentDef,
			status:     &agentStatus,
			config:     agentConfig,
			logger:     o.logger,
		}

		o.agents[agentDef.ID] = agentRunner
		o.logger.LogProcessStep(fmt.Sprintf("  ✅ %s (%s) - %s", agentDef.Name, agentDef.Persona, agentDef.Description))
	}

	return nil
}

// createAgentConfig creates a configuration specific to an agent
func (o *MultiAgentOrchestrator) createAgentConfig(agentDef *types.AgentDefinition) *config.Config {
	// Clone the base config
	agentConfig := *o.config

	// Use agent-specific model, or base model from process file, or fall back to config default
	if agentDef.Model != "" {
		agentConfig.EditingModel = agentDef.Model
		agentConfig.OrchestrationModel = agentDef.Model
	} else if o.plan.BaseModel != "" {
		agentConfig.EditingModel = o.plan.BaseModel
		agentConfig.OrchestrationModel = o.plan.BaseModel
	}

	// Apply agent-specific settings from config
	if agentDef.Config != nil {
		if skipPrompt, ok := agentDef.Config["skip_prompt"]; ok {
			agentConfig.SkipPrompt = skipPrompt == "true"
		}
	}

	return &agentConfig
}

// executeSteps runs all steps in the correct order
func (o *MultiAgentOrchestrator) executeSteps() error {
	o.logger.LogProcessStep("📋 Executing orchestration steps...")

	// Build quick index of steps
	stepByID := make(map[string]*types.OrchestrationStep)
	for i := range o.plan.Steps {
		stepByID[o.plan.Steps[i].ID] = &o.plan.Steps[i]
	}

	// Progress-making loop
	for {
		runnable := []*types.OrchestrationStep{}
		pending := 0
		for i := range o.plan.Steps {
			s := &o.plan.Steps[i]
			if s.Status == "pending" || s.Status == "in_progress" {
				// Only in_progress if previous run left it; treat as pending again
				if s.Status != "in_progress" {
					pending++
				} else {
					s.Status = "pending"
					pending++
				}
			}
		}

		// Collect runnable (deps satisfied)
		for i := range o.plan.Steps {
			s := &o.plan.Steps[i]
			if s.Status != "pending" {
				continue
			}
			if o.canExecuteStep(s) {
				runnable = append(runnable, s)
			}
		}

		if len(runnable) == 0 {
			// No runnable steps. Check if all done
			allDone := true
			for i := range o.plan.Steps {
				if o.plan.Steps[i].Status != "completed" && o.plan.Steps[i].Status != "failed" {
					allDone = false
					break
				}
			}
			if allDone {
				return nil
			}
			// Deadlock: pending but no runnable
			var unmet []string
			for i := range o.plan.Steps {
				s := &o.plan.Steps[i]
				if s.Status == "pending" {
					unmet = append(unmet, s.ID)
				}
			}
			return fmt.Errorf("no runnable steps; unmet dependencies for: %s", strings.Join(unmet, ", "))
		}

		// Execute runnable steps (sequentially or in parallel)
		if o.concurrency <= 1 || len(runnable) == 1 {
			for _, s := range runnable {
				if err := o.runStepWithRetryAndTimeout(s); err != nil {
					if o.shouldStopOnFailure() {
						return err
					}
				}
				_ = o.saveState()
				o.printProgressTable()
			}
			continue
		}

		// Parallel batch with bounded workers
		sem := make(chan struct{}, o.concurrency)
		var wg sync.WaitGroup
		var firstErr error
		var mu sync.Mutex
		for _, s := range runnable {
			wg.Add(1)
			sem <- struct{}{}
			step := s
			go func() {
				defer wg.Done()
				defer func() { <-sem }()
				if err := o.runStepWithRetryAndTimeout(step); err != nil {
					mu.Lock()
					if firstErr == nil {
						firstErr = err
					}
					mu.Unlock()
				}
				_ = o.saveState()
			}()
		}
		wg.Wait()
		if firstErr != nil && o.shouldStopOnFailure() {
			return firstErr
		}
		o.printProgressTable()
	}
}

func (o *MultiAgentOrchestrator) runStepWithRetryAndTimeout(step *types.OrchestrationStep) error {
	retries := step.Retries
	if retries == 0 && o.settings != nil {
		retries = o.settings.MaxRetries
	}
	if retries < 0 {
		retries = 0
	}

	timeoutSecs := step.Timeout
	if timeoutSecs == 0 && o.settings != nil {
		timeoutSecs = o.settings.StepTimeout
	}
	if timeoutSecs <= 0 {
		timeoutSecs = 0
	}

	var lastErr error
	for attempt := 0; attempt <= retries; attempt++ {
		step.Attempts = attempt + 1
		step.LastAttemptAt = time.Now().Format(time.RFC3339)
		if attempt > 0 {
			backoff := time.Duration(500*(1<<uint(attempt-1))) * time.Millisecond
			o.logger.LogProcessStep(fmt.Sprintf("   🔁 Retry %d/%d after %s", attempt, retries, backoff))
			time.Sleep(backoff)
		}

		done := make(chan error, 1)
		// Run the step
		startedAt := time.Now()
		go func() { done <- o.executeStep(step) }()

		if timeoutSecs == 0 {
			lastErr = <-done
		} else {
			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSecs)*time.Second)
			defer cancel()
			select {
			case err := <-done:
				lastErr = err
			case <-ctx.Done():
				lastErr = fmt.Errorf("step '%s' timed out after %ds", step.Name, timeoutSecs)
				// Mark as failed immediately
				step.Status = "failed"
				o.updateAgentStatus(step.AgentID, "failed", "", 0)
			}
		}

		if lastErr == nil {
			// Append successful attempt history
			step.History = append(step.History, types.StepAttempt{
				Attempt:    attempt + 1,
				Status:     "completed",
				Error:      "",
				StartedAt:  startedAt.Format(time.RFC3339),
				FinishedAt: time.Now().Format(time.RFC3339),
				Files:      step.Result.Files,
			})
			return nil
		}
		o.logger.LogProcessStep(fmt.Sprintf("   ❌ Step error: %v", lastErr))
		step.History = append(step.History, types.StepAttempt{
			Attempt:    attempt + 1,
			Status:     "failed",
			Error:      lastErr.Error(),
			StartedAt:  startedAt.Format(time.RFC3339),
			FinishedAt: time.Now().Format(time.RFC3339),
			Files:      nil,
		})
	}
	return lastErr
}

// executeStep runs a single step using the appropriate agent
func (o *MultiAgentOrchestrator) executeStep(step *types.OrchestrationStep) error {
	startTime := time.Now()

	// Update step status
	step.Status = "in_progress"
	o.updateAgentStatus(step.AgentID, "working", step.ID, 0)

	// Get the agent runner
	agentRunner, exists := o.agents[step.AgentID]
	if !exists {
		return fmt.Errorf("agent '%s' not found", step.AgentID)
	}

	// Optionally enrich input with tool-derived context
	o.enrichStepWithToolContext(step)

	// Prepare the agent's task
	task := o.buildAgentTask(step)

	// If tool-assisted LLM calls are enabled, optionally enrich via a tool call round-trip
	if o.config != nil && shouldUseLLMTools(step) {
		if enhanced, err := o.toolAssistTask(task); err == nil && strings.TrimSpace(enhanced) != "" {
			task = enhanced
			o.logger.LogProcessStep("🛠️ Applied tool-assisted analysis to task input")
		}
	}

	// Execute the agent
	result, err := o.runAgent(agentRunner, task)

	// Record the result
	duration := time.Since(startTime).Seconds()
	step.Result = &types.StepResult{
		Status:     result.Status,
		Output:     result.Output,
		Files:      o.collectChangedFilesSince(startTime),
		Errors:     result.Errors,
		Warnings:   result.Warnings,
		Duration:   duration,
		TokenUsage: result.TokenUsage,
		Logs:       result.Logs,
	}

	// Update step and agent status
	if err != nil {
		step.Status = "failed"
		o.updateAgentStatus(step.AgentID, "failed", "", 0)
		return err
	}

	step.Status = "completed"
	o.updateAgentStatus(step.AgentID, "completed", "", 100)

	return nil
}

// shouldUseLLMTools determines if tool-calling path should be attempted for this step
func shouldUseLLMTools(step *types.OrchestrationStep) bool {
	if step == nil {
		return false
	}
	if step.Tools == nil {
		return false
	}
	v := strings.TrimSpace(step.Tools["llm_tools"])
	return strings.EqualFold(v, "true") || strings.EqualFold(v, "enabled") || v == "1"
}

// toolAssistTask performs an LLM round with tool-call support to refine the task text
func (o *MultiAgentOrchestrator) toolAssistTask(task string) (string, error) {
	model := o.config.OrchestrationModel
	if strings.TrimSpace(model) == "" {
		model = o.config.EditingModel
	}
	sys := "You are refining an orchestration step task. If tools are helpful, call them. Return the improved task text only."
	msgs := []prompts.Message{{Role: "user", Content: task}}
	ctxTimeout := 30 * time.Second
	refined, err := CallLLMWithToolSupport(model, msgs, sys, o.config, ctxTimeout)
	if err != nil {
		return "", err
	}
	return refined, nil
}

// collectChangedFilesSince inspects the change tracker to identify files modified after the given time.
func (o *MultiAgentOrchestrator) collectChangedFilesSince(since time.Time) []string {
	files, err := changetracker.GetChangedFilesSince(since)
	if err != nil {
		o.logger.LogProcessStep(fmt.Sprintf("⚠️ Failed to collect changed files: %v", err))
		return []string{}
	}
	return files
}

// buildAgentTask creates a task description for the agent based on the step
func (o *MultiAgentOrchestrator) buildAgentTask(step *types.OrchestrationStep) string {
	var task strings.Builder

	// Add the step description
	task.WriteString(fmt.Sprintf("TASK: %s\n\n", step.Description))

	// Add expected output
	if step.ExpectedOutput != "" {
		task.WriteString(fmt.Sprintf("EXPECTED OUTPUT: %s\n\n", step.ExpectedOutput))
	}

	// Add input context
	if len(step.Input) > 0 {
		task.WriteString("INPUT CONTEXT:\n")
		for key, value := range step.Input {
			task.WriteString(fmt.Sprintf("- %s: %s\n", key, value))
		}
		task.WriteString("\n")
	}

	// Add agent persona context
	agentDef := o.getAgentDefinition(step.AgentID)
	if agentDef != nil {
		task.WriteString(fmt.Sprintf("YOUR ROLE: %s\n", agentDef.Persona))
		task.WriteString(fmt.Sprintf("YOUR SKILLS: %s\n\n", strings.Join(agentDef.Skills, ", ")))
	}

	return task.String()
}

// runAgent executes an agent with the given task
func (o *MultiAgentOrchestrator) runAgent(agentRunner *AgentRunner, task string) (*types.StepResult, error) {
	o.logger.LogProcessStep(fmt.Sprintf("🤖 Running agent: %s (%s)", agentRunner.definition.Name, agentRunner.definition.Persona))

	// Check budget constraints before execution
	if err := o.checkAgentBudget(agentRunner); err != nil {
		return &types.StepResult{
			Status: "failure",
			Errors: []string{fmt.Sprintf("Budget constraint violated: %v", err)},
		}, err
	}

	// Capture current log file size for tailing logs produced during this run
	logPath := ".ledit/workspace.log"
	var startSize int64
	if fi, err := os.Stat(logPath); err == nil {
		startSize = fi.Size()
	}

	// Execute the agent
	tokenUsage, err := agent.Execute(task, agentRunner.config, agentRunner.logger)

	// Build the result
	// Tail logs since startSize
	logs := []string{}
	if f, e := os.Open(logPath); e == nil {
		defer f.Close()
		if _, e2 := f.Seek(startSize, 0); e2 == nil {
			if b, e3 := io.ReadAll(f); e3 == nil {
				for _, line := range strings.Split(string(b), "\n") {
					l := strings.TrimSpace(line)
					if l != "" {
						logs = append(logs, l)
					}
				}
			}
		}
	}

	result := &types.StepResult{
		Status:   "success",
		Output:   make(map[string]string),
		Files:    []string{},
		Errors:   []string{},
		Warnings: []string{},
		Logs:     logs,
	}

	if err != nil {
		result.Status = "failure"
		result.Errors = append(result.Errors, err.Error())
	}

	// Convert token usage and track budget
	if tokenUsage != nil {
		result.TokenUsage = &types.AgentTokenUsage{
			AgentID:    agentRunner.definition.ID,
			Prompt:     tokenUsage.IntentAnalysis + tokenUsage.Planning,
			Completion: tokenUsage.CodeGeneration + tokenUsage.Validation,
			Total:      tokenUsage.Total,
			Model:      agentRunner.config.EditingModel,
		}

		// Update agent status with budget tracking
		o.updateAgentBudget(agentRunner, result.TokenUsage)

		// Improve cost using category splits and pricing
		// Orchestrator model for intent/planning/progress
		orchModel := agentRunner.config.OrchestrationModel
		if strings.TrimSpace(orchModel) == "" {
			orchModel = agentRunner.config.EditingModel
		}
		editModel := agentRunner.config.EditingModel
		var cost float64
		// Intent
		cost += llm.CalculateCost(llm.TokenUsage{PromptTokens: tokenUsage.IntentSplit.Prompt, CompletionTokens: tokenUsage.IntentSplit.Completion}, orchModel)
		// Planning
		cost += llm.CalculateCost(llm.TokenUsage{PromptTokens: tokenUsage.PlanningSplit.Prompt, CompletionTokens: tokenUsage.PlanningSplit.Completion}, orchModel)
		// Progress evaluation
		cost += llm.CalculateCost(llm.TokenUsage{PromptTokens: tokenUsage.ProgressSplit.Prompt, CompletionTokens: tokenUsage.ProgressSplit.Completion}, orchModel)
		// Codegen
		cost += llm.CalculateCost(llm.TokenUsage{PromptTokens: tokenUsage.CodegenSplit.Prompt, CompletionTokens: tokenUsage.CodegenSplit.Completion}, editModel)
		// Validation
		cost += llm.CalculateCost(llm.TokenUsage{PromptTokens: tokenUsage.ValidationSplit.Prompt, CompletionTokens: tokenUsage.ValidationSplit.Completion}, editModel)
		st := o.plan.AgentStatuses[agentRunner.definition.ID]
		st.Cost += cost
		st.TokenUsage += tokenUsage.Total
		o.plan.AgentStatuses[agentRunner.definition.ID] = st
		// Update per-step tokens and cost
		if result != nil {
			result.Tokens = tokenUsage.Total
			result.Cost = cost
		}
		// Update plan aggregates
		o.plan.TotalTokens += tokenUsage.Total
		o.plan.TotalCost += cost
	}

	return result, err
}

// validateResults runs validation checks on the final results
func (o *MultiAgentOrchestrator) validateResults() error {
	o.logger.LogProcessStep("🔍 Validating final results...")

	// Check if all steps completed successfully
	allStepsCompleted := true
	for _, step := range o.plan.Steps {
		if step.Status != "completed" {
			allStepsCompleted = false
			o.logger.LogProcessStep(fmt.Sprintf("  ⚠️ Step '%s' did not complete (status: %s)", step.Name, step.Status))
		}
	}

	if !allStepsCompleted {
		// Print a concise failure summary
		o.logger.LogProcessStep("❌ Some steps failed or did not complete:")
		for _, step := range o.plan.Steps {
			if step.Status != "completed" {
				msg := step.Status
				if step.Result != nil && len(step.Result.Errors) > 0 {
					msg = msg + ": " + strings.Join(step.Result.Errors, "; ")
				}
				o.logger.LogProcessStep(fmt.Sprintf(" - %s (%s)", step.Name, msg))
			}
		}
		return fmt.Errorf("not all steps completed successfully")
	}

	o.logger.LogProcessStep("  ✅ All steps completed successfully")
	return nil
}

// enrichStepWithToolContext allows configured steps to pull in tool-based context
func (o *MultiAgentOrchestrator) enrichStepWithToolContext(step *types.OrchestrationStep) {
	if o.config == nil {
		return
	}
	if step.Input == nil {
		step.Input = map[string]string{}
	}

	te := NewToolExecutor(o.config)

	// Workspace summary (from input or tools)
	if strings.EqualFold(strings.TrimSpace(step.Input["workspace_summary"]), "true") || (step.Tools != nil && strings.EqualFold(strings.TrimSpace(step.Tools["workspace_summary"]), "true")) {
		if res, err := te.executeWorkspaceContext(map[string]interface{}{"action": "load_summary"}); err == nil {
			step.Input["workspace_summary_content"] = res
			o.logger.LogProcessStep("📥 Added workspace summary to step input")
		}
	}
	// Workspace tree (from input or tools)
	if strings.EqualFold(strings.TrimSpace(step.Input["workspace_tree"]), "true") || (step.Tools != nil && strings.EqualFold(strings.TrimSpace(step.Tools["workspace_tree"]), "true")) {
		if res, err := te.executeWorkspaceContext(map[string]interface{}{"action": "load_tree"}); err == nil {
			step.Input["workspace_tree_content"] = res
			o.logger.LogProcessStep("📥 Added workspace file tree to step input")
		}
	}
	// Workspace keyword search
	if q := firstNonEmpty(step.Input["workspace_search"], opt(step.Tools, "workspace_search")); strings.TrimSpace(q) != "" {
		if res, err := te.executeWorkspaceContext(map[string]interface{}{"action": "search_keywords", "query": q}); err == nil {
			step.Input["workspace_search_results"] = res
			o.logger.LogProcessStep("🔎 Added workspace search results to step input")
		}
	}
	// Workspace embeddings search
	if q := firstNonEmpty(step.Input["workspace_embeddings"], opt(step.Tools, "workspace_embeddings")); strings.TrimSpace(q) != "" {
		if res, err := te.executeWorkspaceContext(map[string]interface{}{"action": "search_embeddings", "query": q}); err == nil {
			step.Input["workspace_embeddings_results"] = res
			o.logger.LogProcessStep("🧠 Added workspace embeddings results to step input")
		}
	}
	// Web search
	if q := firstNonEmpty(step.Input["web_search"], opt(step.Tools, "web_search")); strings.TrimSpace(q) != "" {
		if res, err := te.executeWebSearch(map[string]interface{}{"query": q}); err == nil {
			step.Input["web_search_results"] = res
			o.logger.LogProcessStep("🌐 Added web search results to step input")
		}
	}

	// Read file content
	if p := firstNonEmpty(step.Input["read_file"], opt(step.Tools, "read_file")); strings.TrimSpace(p) != "" {
		if res, err := te.executeReadFile(map[string]interface{}{"file_path": p}); err == nil {
			step.Input["read_file_content"] = res
			o.logger.LogProcessStep("📄 Added read_file content to step input")
		}
	}

	// Run shell command (use with care)
	if cmd := firstNonEmpty(step.Input["run_shell"], opt(step.Tools, "run_shell")); strings.TrimSpace(cmd) != "" {
		if res, err := te.executeShellCommand(map[string]interface{}{"command": cmd}); err == nil {
			step.Input["shell_command_output"] = res
			o.logger.LogProcessStep("🔧 Added shell command output to step input")
		}
	}

	// Ask user (respects non-interactive mode)
	if q := firstNonEmpty(step.Input["ask_user"], opt(step.Tools, "ask_user")); strings.TrimSpace(q) != "" {
		if res, err := te.executeAskUser(map[string]interface{}{"question": q}); err == nil {
			step.Input["ask_user_response"] = res
			o.logger.LogProcessStep("🙋 Added ask_user response to step input")
		}
	}
}

func opt(m map[string]string, k string) string {
	if m == nil {
		return ""
	}
	return m[k]
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// runValidationStage executes build/test/lint/custom checks when configured
func (o *MultiAgentOrchestrator) runValidationStage() error {
	if o.validation == nil {
		return nil
	}
	run := func(name, cmd string) error {
		if strings.TrimSpace(cmd) == "" {
			return nil
		}
		o.logger.LogProcessStep(fmt.Sprintf("🧪 Running %s: %s", name, cmd))
		c := exec.Command("sh", "-c", cmd)
		out, err := c.CombinedOutput()
		outputStr := strings.TrimSpace(string(out))
		if outputStr != "" {
			o.logger.LogProcessStep(fmt.Sprintf("   ⮑ Output (%s):\n%s", name, outputStr))
		}
		if err != nil {
			return fmt.Errorf("%s failed", name)
		}
		return nil
	}

	var errs []string
	if err := run("build", o.validation.BuildCommand); err != nil {
		errs = append(errs, err.Error())
	}
	if err := run("test", o.validation.TestCommand); err != nil {
		errs = append(errs, err.Error())
	}
	if err := run("lint", o.validation.LintCommand); err != nil {
		errs = append(errs, err.Error())
	}
	for i, check := range o.validation.CustomChecks {
		nonBlocking := false
		trimmed := strings.TrimSpace(check)
		if strings.HasPrefix(trimmed, "!") {
			nonBlocking = true
			trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "!"))
		}
		if err := run(fmt.Sprintf("custom_check_%d", i+1), trimmed); err != nil {
			if nonBlocking {
				o.logger.LogProcessStep("⚠️ Non-blocking validation failed: " + err.Error())
			} else {
				errs = append(errs, err.Error())
			}
		}
	}
	if len(errs) > 0 {
		msg := fmt.Sprintf("validation failures: %s", strings.Join(errs, "; "))
		if o.validation.Required {
			return fmt.Errorf("%s", msg)
		}
		o.logger.LogProcessStep("⚠️ " + msg)
	}
	return nil
}

// --- Persistence ---
func (o *MultiAgentOrchestrator) loadStateIfCompatible() error {
	b, err := os.ReadFile(o.statePath)
	if err != nil {
		return err
	}
	var saved types.MultiAgentOrchestrationPlan
	if err := json.Unmarshal(b, &saved); err != nil {
		return err
	}
	if err := o.ensureCompatibility(&saved); err != nil {
		return fmt.Errorf("state incompatible: %w", err)
	}
	o.plan = &saved
	return nil
}

func (o *MultiAgentOrchestrator) saveState() error {
	if err := os.MkdirAll(filepath.Dir(o.statePath), 0755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(o.plan, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(o.statePath, b, 0644)
}

// ensureCompatibility checks that the saved plan matches the current process definition
func (o *MultiAgentOrchestrator) ensureCompatibility(saved *types.MultiAgentOrchestrationPlan) error {
	if saved.Goal != o.plan.Goal {
		return fmt.Errorf("goal differs")
	}
	// Compare agent ID sets
	curAgents := map[string]bool{}
	for _, a := range o.plan.Agents {
		curAgents[a.ID] = true
	}
	for _, a := range saved.Agents {
		if !curAgents[a.ID] {
			return fmt.Errorf("agent set differs (missing %s)", a.ID)
		}
	}
	if len(saved.Agents) != len(curAgents) {
		return fmt.Errorf("agent set size differs")
	}

	// Compare step ID sets
	curSteps := map[string]bool{}
	for _, s := range o.plan.Steps {
		curSteps[s.ID] = true
	}
	for _, s := range saved.Steps {
		if !curSteps[s.ID] {
			return fmt.Errorf("step set differs (missing %s)", s.ID)
		}
	}
	if len(saved.Steps) != len(curSteps) {
		return fmt.Errorf("step set size differs")
	}
	return nil
}

// printProgressTable renders a concise status table of agents and overall progress
func (o *MultiAgentOrchestrator) printProgressTable() {
	// Respect environment flag to suppress progress output when requested via CLI
	if os.Getenv("LEDIT_NO_PROGRESS") == "1" {
		return
	}
	total := len(o.plan.Steps)
	completed := 0
	for _, s := range o.plan.Steps {
		if s.Status == "completed" {
			completed++
		}
	}
	o.logger.LogProcessStep(fmt.Sprintf("📊 Progress: %d/%d steps completed", completed, total))

	// Stable ordering of agents by name
	type row struct {
		Name, Status, Step string
		Progress, Tokens   int
		Cost               float64
	}
	var rows []row
	for id, st := range o.plan.AgentStatuses {
		name := id
		if def := o.getAgentDefinition(id); def != nil && strings.TrimSpace(def.Name) != "" {
			name = def.Name
		}
		step := st.CurrentStep
		rows = append(rows, row{Name: name, Status: st.Status, Step: step, Progress: st.Progress, Tokens: st.TokenUsage, Cost: st.Cost})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })

	// If UI is enabled, publish structured progress snapshot
	if ui.Enabled() {
		evRows := make([]ui.ProgressRow, 0, len(rows))
		for _, r := range rows {
			evRows = append(evRows, ui.ProgressRow{
				Name:   r.Name,
				Status: r.Status,
				Step:   r.Step,
				Tokens: r.Tokens,
				Cost:   r.Cost,
			})
		}
		// Aggregate tokens and cost from agent statuses
		totalTokens := 0
		totalCost := 0.0
		for _, st := range o.plan.AgentStatuses {
			totalTokens += st.TokenUsage
			totalCost += st.Cost
		}
		// Publish base model when known
		if strings.TrimSpace(o.plan.BaseModel) != "" {
			ui.PublishModel(o.plan.BaseModel)
		}
		// Send progress snapshot with aggregates
		ui.Publish(ui.ProgressSnapshotEvent{Completed: completed, Total: total, Rows: evRows, Time: time.Now(), TotalTokens: totalTokens, TotalCost: totalCost, BaseModel: o.plan.BaseModel})
		return
	}

	// Fallback to stdout printing
	ui.Out().Printf("\n%-24s %-12s %-22s %8s %10s\n", "Agent", "Status", "Current Step", "Tokens", "Cost($)")
	ui.Out().Printf("%s\n", strings.Repeat("-", 80))
	for _, r := range rows {
		ui.Out().Printf("%-24s %-12s %-22s %8d %10.4f\n", r.Name, r.Status, r.Step, r.Tokens, r.Cost)
	}
	ui.Out().Printf("\n")
}

// Helper methods

func (o *MultiAgentOrchestrator) getAgentName(agentID string) string {
	if agentDef := o.getAgentDefinition(agentID); agentDef != nil {
		return agentDef.Name
	}
	return "Unknown Agent"
}

func (o *MultiAgentOrchestrator) getAgentDefinition(agentID string) *types.AgentDefinition {
	for _, agent := range o.plan.Agents {
		if agent.ID == agentID {
			return &agent
		}
	}
	return nil
}

func (o *MultiAgentOrchestrator) updateAgentStatus(agentID, status, currentStep string, progress int) {
	if agentStatus, exists := o.plan.AgentStatuses[agentID]; exists {
		agentStatus.Status = status
		agentStatus.CurrentStep = currentStep
		agentStatus.Progress = progress
		agentStatus.LastUpdate = time.Now().Format(time.RFC3339)
		o.plan.AgentStatuses[agentID] = agentStatus
	}
}

func (o *MultiAgentOrchestrator) canExecuteStep(step *types.OrchestrationStep) bool {
	if len(step.DependsOn) == 0 {
		// If agent is halted with stop-on-limit, do not allow step to run
		if st, ok := o.plan.AgentStatuses[step.AgentID]; ok {
			if st.Halted {
				// find budget stop flag
				for _, a := range o.plan.Agents {
					if a.ID == step.AgentID && a.Budget != nil && a.Budget.StopOnLimit {
						return false
					}
				}
			}
		}
		return true
	}

	for _, depID := range step.DependsOn {
		// Find the dependent step
		var depStep *types.OrchestrationStep
		for _, s := range o.plan.Steps {
			if s.ID == depID {
				depStep = &s
				break
			}
		}

		if depStep == nil || depStep.Status != "completed" {
			return false
		}
	}

	// If agent is halted with stop-on-limit, do not allow step to run
	if st, ok := o.plan.AgentStatuses[step.AgentID]; ok {
		if st.Halted {
			for _, a := range o.plan.Agents {
				if a.ID == step.AgentID && a.Budget != nil && a.Budget.StopOnLimit {
					return false
				}
			}
		}
	}

	return true
}

// listRunnableStepIDs returns IDs of steps that are pending and whose dependencies are all completed
func (o *MultiAgentOrchestrator) listRunnableStepIDs() []string {
	var ids []string
	for i := range o.plan.Steps {
		s := &o.plan.Steps[i]
		if s.Status != "pending" {
			continue
		}
		if o.canExecuteStep(s) {
			ids = append(ids, s.ID)
		}
	}
	return ids
}

func (o *MultiAgentOrchestrator) shouldStopOnFailure() bool {
	// Check if any agent has a stop_on_failure config
	for _, agentDef := range o.plan.Agents {
		if stopOnFailure, ok := agentDef.Config["stop_on_failure"]; ok && stopOnFailure == "true" {
			return true
		}
	}
	return false
}

func (o *MultiAgentOrchestrator) sortStepsByDependencies(steps []types.OrchestrationStep) []types.OrchestrationStep {
	// Kahn's algorithm for topological sort
	inDegree := map[string]int{}
	adj := map[string][]string{}
	idToStep := map[string]types.OrchestrationStep{}
	for _, s := range steps {
		idToStep[s.ID] = s
		if _, ok := inDegree[s.ID]; !ok {
			inDegree[s.ID] = 0
		}
		for _, dep := range s.DependsOn {
			inDegree[s.ID]++
			adj[dep] = append(adj[dep], s.ID)
		}
	}
	queue := []string{}
	for id, d := range inDegree {
		if d == 0 {
			queue = append(queue, id)
		}
	}
	var order []types.OrchestrationStep
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		order = append(order, idToStep[id])
		for _, nbr := range adj[id] {
			inDegree[nbr]--
			if inDegree[nbr] == 0 {
				queue = append(queue, nbr)
			}
		}
	}
	// If cycle exists, fall back to original order with a warning
	if len(order) != len(steps) {
		o.logger.LogProcessStep("⚠️ Cycle or unresolved dependencies detected; using original step order")
		return steps
	}
	return order
}

func buildStepDependencies(steps []types.OrchestrationStep) map[string][]string {
	deps := make(map[string][]string)

	for _, step := range steps {
		deps[step.ID] = step.DependsOn
	}

	return deps
}

// checkAgentBudget checks if the agent has exceeded its budget constraints
func (o *MultiAgentOrchestrator) checkAgentBudget(agentRunner *AgentRunner) error {
	if agentRunner.definition.Budget == nil {
		return nil // No budget constraints
	}

	budget := agentRunner.definition.Budget
	status := o.plan.AgentStatuses[agentRunner.definition.ID]

	// Check token limits
	if budget.MaxTokens > 0 && status.TokenUsage > budget.MaxTokens {
		if budget.StopOnLimit {
			return fmt.Errorf("agent '%s' exceeded token limit: %d > %d",
				agentRunner.definition.Name, status.TokenUsage, budget.MaxTokens)
		}
		o.logger.LogProcessStep(fmt.Sprintf("⚠️ Agent '%s' exceeded token limit: %d > %d",
			agentRunner.definition.Name, status.TokenUsage, budget.MaxTokens))
	}

	// Check cost limits (if we have cost tracking)
	if budget.MaxCost > 0 && status.Cost > budget.MaxCost {
		if budget.StopOnLimit {
			return fmt.Errorf("agent '%s' exceeded cost limit: $%.4f > $%.4f",
				agentRunner.definition.Name, status.Cost, budget.MaxCost)
		}
		o.logger.LogProcessStep(fmt.Sprintf("⚠️ Agent '%s' exceeded cost limit: $%.4f > $%.4f",
			agentRunner.definition.Name, status.Cost, budget.MaxCost))
	}

	return nil
}

// updateAgentBudget updates the agent's budget tracking after execution
func (o *MultiAgentOrchestrator) updateAgentBudget(agentRunner *AgentRunner, tokenUsage *types.AgentTokenUsage) {
	if agentRunner.definition.Budget == nil {
		return // No budget tracking needed
	}

	status := o.plan.AgentStatuses[agentRunner.definition.ID]
	budget := agentRunner.definition.Budget

	// Update token usage
	status.TokenUsage += tokenUsage.Total

	// Calculate and update cost using pricing helpers
	// We don't have prompt/completion split per step here; approximate using totals
	// Treat all tokens as prompt for a conservative lower-bound
	tu := llm.TokenUsage{PromptTokens: tokenUsage.Total, CompletionTokens: 0, TotalTokens: tokenUsage.Total}
	status.Cost += llm.CalculateCost(tu, agentRunner.config.EditingModel)

	// Check for warnings
	if budget.TokenWarning > 0 && status.TokenUsage >= budget.TokenWarning {
		o.logger.LogProcessStep(fmt.Sprintf("⚠️ Agent '%s' approaching token limit: %d/%d",
			agentRunner.definition.Name, status.TokenUsage, budget.MaxTokens))
	}

	if budget.CostWarning > 0 && status.Cost >= budget.CostWarning {
		o.logger.LogProcessStep(fmt.Sprintf("⚠️ Agent '%s' approaching cost limit: $%.4f/$%.4f",
			agentRunner.definition.Name, status.Cost, budget.MaxCost))
	}

	// Enforce hard limits
	if (budget.MaxTokens > 0 && status.TokenUsage > budget.MaxTokens) || (budget.MaxCost > 0 && status.Cost > budget.MaxCost) {
		status.Halted = true
		status.HaltReason = fmt.Sprintf("budget exceeded (tokens %d/%d, cost $%.4f/$%.4f)", status.TokenUsage, budget.MaxTokens, status.Cost, budget.MaxCost)
		if budget.StopOnLimit {
			o.logger.LogProcessStep(fmt.Sprintf("🛑 Agent '%s' halted: %s", agentRunner.definition.Name, status.HaltReason))
		} else {
			o.logger.LogProcessStep(fmt.Sprintf("⚠️ Agent '%s' exceeded budget but continuing: %s", agentRunner.definition.Name, status.HaltReason))
		}
	}

	// Log budget status
	o.logger.LogProcessStep(fmt.Sprintf("💰 Agent '%s' budget status: %d tokens, $%.4f cost",
		agentRunner.definition.Name, status.TokenUsage, status.Cost))

	// Update the status in the map
	o.plan.AgentStatuses[agentRunner.definition.ID] = status
}
