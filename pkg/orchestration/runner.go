package orchestration

import (
	"fmt"
	"sync"
	"time"

	"github.com/alantheprice/ledit/pkg/agent"
	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/orchestration/types"
	"github.com/alantheprice/ledit/pkg/utils"
)

// AgentRunner manages a single agent instance
type AgentRunner struct {
	definition *types.AgentDefinition
	status     *types.AgentStatus
	config     *config.Config
	logger     *utils.Logger
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
			return fmt.Errorf("no runnable steps; unmet dependencies for: %s", fmt.Sprintf("%v", unmet))
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

	// Execute with retries
	for attempt := 0; attempt <= retries; attempt++ {
		if attempt > 0 {
			o.logger.LogProcessStep(fmt.Sprintf("🔁 Retry %d/%d for step %s", attempt, retries, step.ID))
		}

		// Execute with timeout if configured
		var result error
		if timeoutSecs > 0 {
			timeout := time.Duration(timeoutSecs) * time.Second
			resultChan := make(chan error, 1)

			go func() {
				resultChan <- o.executeStep(step)
			}()

			select {
			case result = <-resultChan:
				// Step completed within timeout
			case <-time.After(timeout):
				result = fmt.Errorf("step %s timed out after %d seconds", step.ID, timeoutSecs)
			}
		} else {
			result = o.executeStep(step)
		}

		if result == nil {
			// Success
			step.Status = "completed"
			step.Attempts = attempt + 1
			return nil
		}

		// Log failure
		o.logger.LogProcessStep(fmt.Sprintf("   Attempt %d error: %v", attempt+1, result))
		o.logger.LogProcessStep(fmt.Sprintf("❌ Step %s failed (attempt %d/%d): %v", step.ID, attempt+1, retries+1, result))

		// If this was the last attempt, mark as failed
		if attempt == retries {
			step.Status = "failed"
			step.Attempts = attempt + 1
			return fmt.Errorf("step %s failed after %d attempts: %w", step.ID, retries+1, result)
		}
	}

	return nil
}

func (o *MultiAgentOrchestrator) executeStep(step *types.OrchestrationStep) error {
	step.Status = "in_progress"
	o.logger.LogProcessStep(fmt.Sprintf("▶️ Executing step: %s (%s)", step.Name, step.ID))

	// Enrich step with tool context if needed
	o.enrichStepWithToolContext(step)

	// Build task for the agent
	task := o.buildAgentTask(step)

	// Get the appropriate agent
	agentRunner := o.agents[step.AgentID]
	if agentRunner == nil {
		return fmt.Errorf("agent %s not found", step.AgentID)
	}

	// Check agent budget before execution
	if err := o.checkAgentBudget(agentRunner); err != nil {
		return fmt.Errorf("agent budget check failed: %w", err)
	}

	// Update agent status
	o.updateAgentStatus(step.AgentID, "in_progress", step.ID, 50)

	// Execute the task
	result, err := o.runAgent(agentRunner, task)
	if err != nil {
		o.updateAgentStatus(step.AgentID, "idle", "", 0)
		return fmt.Errorf("agent execution failed: %w", err)
	}

	// Update agent budget with usage
	if result.TokenUsage != nil {
		o.updateAgentBudget(agentRunner, result.TokenUsage)
	}

	// Store result
	step.Result = result
	step.Status = "completed"
	step.Attempts++

	o.updateAgentStatus(step.AgentID, "idle", "", 100)
	o.logger.LogProcessStep(fmt.Sprintf("✅ Step %s completed successfully", step.ID))

	return nil
}

func (o *MultiAgentOrchestrator) buildAgentTask(step *types.OrchestrationStep) string {
	task := fmt.Sprintf("Step: %s\n", step.Name)
	if step.Description != "" {
		task += fmt.Sprintf("Description: %s\n", step.Description)
	}
	if len(step.Input) > 0 {
		task += "Input/Requirements:\n"
		for key, value := range step.Input {
			task += fmt.Sprintf("  %s: %s\n", key, value)
		}
	}
	if step.ExpectedOutput != "" {
		task += fmt.Sprintf("Expected Output: %s\n", step.ExpectedOutput)
	}

	// Add context about dependencies if any were completed
	var deps []string
	for _, depID := range step.DependsOn {
		for _, s := range o.plan.Steps {
			if s.ID == depID && s.Status == "completed" && s.Result != nil {
				resultOutput := ""
				if len(s.Result.Output) > 0 {
					// Take the first output value as a summary
					for _, output := range s.Result.Output {
						resultOutput = output
						break
					}
				}
				deps = append(deps, fmt.Sprintf("%s: %s", s.Name, resultOutput))
			}
		}
	}

	if len(deps) > 0 {
		task += fmt.Sprintf("\nPrevious work completed:\n%s", fmt.Sprintf("%v", deps))
	}

	return task
}

func (o *MultiAgentOrchestrator) runAgent(agentRunner *AgentRunner, task string) (*types.StepResult, error) {
	o.logger.LogProcessStep(fmt.Sprintf("🤖 Running agent: %s", agentRunner.definition.Name))

	// Execute the agent
	tokenUsage, err := agent.Execute(task, agentRunner.config, agentRunner.logger)
	if err != nil {
		return nil, fmt.Errorf("agent execution error: %w", err)
	}

	// Convert agent token usage to types token usage
	var typesTokenUsage *types.AgentTokenUsage
	if tokenUsage != nil {
		// Calculate prompt and completion from agent usage
		prompt := tokenUsage.IntentAnalysis + tokenUsage.Planning + tokenUsage.ProgressEvaluation
		completion := tokenUsage.CodeGeneration + tokenUsage.Validation

		typesTokenUsage = &types.AgentTokenUsage{
			AgentID:    agentRunner.definition.ID,
			Prompt:     prompt,
			Completion: completion,
			Total:      tokenUsage.Total,
			Model:      agentRunner.config.EditingModel,
		}
	}

	// Create the result with token usage from agent execution
	result := &types.StepResult{
		Status: "success",
		Output: map[string]string{
			"result": fmt.Sprintf("Task completed by %s", agentRunner.definition.Name),
		},
		Files:      []string{},
		Errors:     []string{},
		Warnings:   []string{},
		Logs:       []string{},
		TokenUsage: typesTokenUsage,
		Tokens:     typesTokenUsage.Total,
		Cost:       0, // TODO: Calculate cost from token usage
	}

	return result, nil
}
