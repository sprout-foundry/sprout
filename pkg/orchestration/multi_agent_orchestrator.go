package orchestration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/alantheprice/ledit/pkg/agent"
	"github.com/alantheprice/ledit/pkg/changetracker"
	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/orchestration/types"
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

	// Attempt resume from previous state
	if err := o.loadStateIfCompatible(); err == nil {
		o.logger.LogProcessStep("♻️ Loaded previous orchestration state; resuming...")
	}

	// Initialize all agents
	if err := o.initializeAgents(); err != nil {
		return fmt.Errorf("failed to initialize agents: %w", err)
	}

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
		if attempt > 0 {
			backoff := time.Duration(500*(1<<uint(attempt-1))) * time.Millisecond
			o.logger.LogProcessStep(fmt.Sprintf("   🔁 Retry %d/%d after %s", attempt, retries, backoff))
			time.Sleep(backoff)
		}

		done := make(chan error, 1)
		// Run the step
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
			return nil
		}
		o.logger.LogProcessStep(fmt.Sprintf("   ❌ Step error: %v", lastErr))
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

	// Prepare the agent's task
	task := o.buildAgentTask(step)

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

	// Create a temporary agent context for this execution
	agentCtx := &agent.AgentContext{
		UserIntent:         task,
		ExecutedOperations: []string{},
		Errors:             []string{},
		ValidationResults:  []string{},
		IterationCount:     0,
		MaxIterations:      10, // Limit iterations for orchestrated agents
		StartTime:          time.Now(),
		TokenUsage:         &agent.AgentTokenUsage{},
		Config:             agentRunner.config,
		Logger:             agentRunner.logger,
	}

	// Execute the agent
	tokenUsage, err := agent.Execute(task, agentRunner.config, agentRunner.logger)

	// Build the result
	result := &types.StepResult{
		Status:   "success",
		Output:   make(map[string]string),
		Files:    []string{},
		Errors:   []string{},
		Warnings: []string{},
		Logs:     agentCtx.ExecutedOperations,
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
		return fmt.Errorf("not all steps completed successfully")
	}

	o.logger.LogProcessStep("  ✅ All steps completed successfully")
	return nil
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
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		if err := c.Run(); err != nil {
			return fmt.Errorf("%s failed: %w", name, err)
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
		if err := run(fmt.Sprintf("custom_check_%d", i+1), check); err != nil {
			errs = append(errs, err.Error())
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
	// Basic compatibility check: goal and number of steps
	if saved.Goal != o.plan.Goal || len(saved.Steps) != len(o.plan.Steps) {
		return fmt.Errorf("state incompatible")
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

	return true
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
	// Simple topological sort - in practice, you might want a more sophisticated algorithm
	var sorted []types.OrchestrationStep
	visited := make(map[string]bool)

	// First add steps with no dependencies
	for _, step := range steps {
		if len(step.DependsOn) == 0 {
			sorted = append(sorted, step)
			visited[step.ID] = true
		}
	}

	// Then add dependent steps
	for _, step := range steps {
		if !visited[step.ID] {
			sorted = append(sorted, step)
		}
	}

	return sorted
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

	// Calculate and update cost (rough estimate based on model pricing)
	// This is a simplified calculation - in practice you'd use actual pricing tables
	costPerToken := 0.00001 // Rough estimate: $0.00001 per token
	status.Cost += float64(tokenUsage.Total) * costPerToken

	// Check for warnings
	if budget.TokenWarning > 0 && status.TokenUsage >= budget.TokenWarning {
		o.logger.LogProcessStep(fmt.Sprintf("⚠️ Agent '%s' approaching token limit: %d/%d",
			agentRunner.definition.Name, status.TokenUsage, budget.MaxTokens))
	}

	if budget.CostWarning > 0 && status.Cost >= budget.CostWarning {
		o.logger.LogProcessStep(fmt.Sprintf("⚠️ Agent '%s' approaching cost limit: $%.4f/$%.4f",
			agentRunner.definition.Name, status.Cost, budget.MaxCost))
	}

	// Log budget status
	o.logger.LogProcessStep(fmt.Sprintf("💰 Agent '%s' budget status: %d tokens, $%.4f cost",
		agentRunner.definition.Name, status.TokenUsage, status.Cost))

	// Update the status in the map
	o.plan.AgentStatuses[agentRunner.definition.ID] = status
}
