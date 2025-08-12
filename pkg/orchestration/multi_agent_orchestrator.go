package orchestration

import (
	"fmt"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/agent"
	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/orchestration/types"
	"github.com/alantheprice/ledit/pkg/utils"
)

// MultiAgentOrchestrator manages the execution of multiple agents with different personas
type MultiAgentOrchestrator struct {
	plan     *types.MultiAgentOrchestrationPlan
	config   *config.Config
	logger   *utils.Logger
	agents   map[string]*AgentRunner
	stepDeps map[string][]string // step ID -> dependent step IDs
}

// AgentRunner manages a single agent instance
type AgentRunner struct {
	definition *types.AgentDefinition
	status     *types.AgentStatus
	config     *config.Config
	logger     *utils.Logger
}

// NewMultiAgentOrchestrator creates a new orchestrator for multi-agent processes
func NewMultiAgentOrchestrator(processFile *types.ProcessFile, cfg *config.Config, logger *utils.Logger) *MultiAgentOrchestrator {
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

	return &MultiAgentOrchestrator{
		plan:     plan,
		config:   cfg,
		logger:   logger,
		agents:   make(map[string]*AgentRunner),
		stepDeps: stepDeps,
	}
}

// Execute runs the multi-agent orchestration process
func (o *MultiAgentOrchestrator) Execute() error {
	o.logger.LogProcessStep("🚀 Starting multi-agent orchestration process")
	o.logger.LogProcessStep(fmt.Sprintf("Goal: %s", o.plan.Goal))
	o.logger.LogProcessStep(fmt.Sprintf("Agents: %d, Steps: %d", len(o.plan.Agents), len(o.plan.Steps)))

	// Initialize all agents
	if err := o.initializeAgents(); err != nil {
		return fmt.Errorf("failed to initialize agents: %w", err)
	}

	// Execute steps in dependency order
	if err := o.executeSteps(); err != nil {
		return fmt.Errorf("failed to execute steps: %w", err)
	}

	// Validate final results
	if err := o.validateResults(); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	o.plan.Status = "completed"
	o.plan.CompletedAt = time.Now().Format(time.RFC3339)
	o.logger.LogProcessStep("✅ Multi-agent orchestration completed successfully")

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

	// Sort steps by priority and dependencies
	sortedSteps := o.sortStepsByDependencies(o.plan.Steps)

	for i := range sortedSteps {
		step := &sortedSteps[i]
		o.logger.LogProcessStep(fmt.Sprintf("\n🔄 Step %d/%d: %s", i+1, len(sortedSteps), step.Name))
		o.logger.LogProcessStep(fmt.Sprintf("   Agent: %s", o.getAgentName(step.AgentID)))
		o.logger.LogProcessStep(fmt.Sprintf("   Description: %s", step.Description))

		// Check if step can run (dependencies satisfied)
		if !o.canExecuteStep(step) {
			o.logger.LogProcessStep(fmt.Sprintf("   ⏳ Waiting for dependencies..."))
			continue
		}

		// Execute the step
		if err := o.executeStep(step); err != nil {
			if o.shouldStopOnFailure() {
				return fmt.Errorf("step '%s' failed: %w", step.Name, err)
			}
			o.logger.LogProcessStep(fmt.Sprintf("   ❌ Step failed but continuing: %v", err))
		} else {
			o.logger.LogProcessStep(fmt.Sprintf("   ✅ Step completed successfully"))
		}
	}

	return nil
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
		Files:      result.Files,
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
