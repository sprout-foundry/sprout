package orchestration

import (
	"fmt"

	"github.com/alantheprice/ledit/pkg/orchestration/types"
)

// buildStepDependencies creates a dependency graph from the steps
func buildStepDependencies(steps []types.OrchestrationStep) map[string][]string {
	deps := make(map[string][]string)

	// Build reverse dependencies (step ID -> steps that depend on it)
	reverseDeps := make(map[string][]string)
	for _, step := range steps {
		for _, dep := range step.DependsOn {
			reverseDeps[dep] = append(reverseDeps[dep], step.ID)
		}
	}

	// Convert to forward dependencies (step ID -> steps it must complete before)
	for _, step := range steps {
		var stepDeps []string
		for _, dependentStep := range reverseDeps[step.ID] {
			stepDeps = append(stepDeps, dependentStep)
		}
		deps[step.ID] = stepDeps
	}

	return deps
}

// canExecuteStep checks if a step's dependencies are satisfied
func (o *MultiAgentOrchestrator) canExecuteStep(step *types.OrchestrationStep) bool {
	// If no dependencies, can always execute
	if len(step.DependsOn) == 0 {
		return true
	}

	// Check that all dependencies are completed
	for _, depID := range step.DependsOn {
		depStep := o.findStepByID(depID)
		if depStep == nil {
			o.logger.LogProcessStep(fmt.Sprintf("⚠️ Step %s has unknown dependency %s", step.ID, depID))
			return false
		}
		if depStep.Status != "completed" {
			return false
		}
	}

	return true
}

// findStepByID finds a step by its ID
func (o *MultiAgentOrchestrator) findStepByID(stepID string) *types.OrchestrationStep {
	for i := range o.plan.Steps {
		if o.plan.Steps[i].ID == stepID {
			return &o.plan.Steps[i]
		}
	}
	return nil
}

// listRunnableStepIDs returns IDs of steps that can currently be executed
func (o *MultiAgentOrchestrator) listRunnableStepIDs() []string {
	var runnable []string
	for _, step := range o.plan.Steps {
		if step.Status == "pending" && o.canExecuteStep(&step) {
			runnable = append(runnable, step.ID)
		}
	}
	return runnable
}

// shouldStopOnFailure determines if orchestration should stop on step failures
func (o *MultiAgentOrchestrator) shouldStopOnFailure() bool {
	if o.settings != nil {
		return o.settings.StopOnFailure
	}
	return true // Default to stopping on failure
}

// sortStepsByDependencies sorts steps to respect dependencies (topological sort)
func (o *MultiAgentOrchestrator) sortStepsByDependencies(steps []types.OrchestrationStep) []types.OrchestrationStep {
	// Create a map for quick lookup
	stepMap := make(map[string]*types.OrchestrationStep)
	for i := range steps {
		stepMap[steps[i].ID] = &steps[i]
	}

	// Track visited steps to detect cycles
	visited := make(map[string]bool)
	recStack := make(map[string]bool)
	var result []types.OrchestrationStep

	// Helper function for topological sort
	var topologicalSort func(stepID string) error
	topologicalSort = func(stepID string) error {
		if recStack[stepID] {
			return fmt.Errorf("circular dependency detected involving step %s", stepID)
		}
		if visited[stepID] {
			return nil
		}

		visited[stepID] = true
		recStack[stepID] = true

		// Visit all dependencies first
		step := stepMap[stepID]
		if step != nil {
			for _, depID := range step.DependsOn {
				if err := topologicalSort(depID); err != nil {
					return err
				}
			}
		}

		recStack[stepID] = false
		if step != nil {
			result = append(result, *step)
		}
		return nil
	}

	// Perform topological sort on all steps
	for _, step := range steps {
		if !visited[step.ID] {
			if err := topologicalSort(step.ID); err != nil {
				o.logger.LogProcessStep(fmt.Sprintf("⚠️ Dependency sorting failed: %v", err))
				// Fall back to original order if there's a cycle
				return steps
			}
		}
	}

	// Reverse the result to get correct order (dependencies first)
	for i := 0; i < len(result)/2; i++ {
		j := len(result) - 1 - i
		result[i], result[j] = result[j], result[i]
	}

	return result
}

// validateDependencies checks for dependency issues
func (o *MultiAgentOrchestrator) validateDependencies() error {
	// Check for circular dependencies
	stepMap := make(map[string]*types.OrchestrationStep)
	for i := range o.plan.Steps {
		stepMap[o.plan.Steps[i].ID] = &o.plan.Steps[i]
	}

	// Check that all dependencies exist
	for _, step := range o.plan.Steps {
		for _, depID := range step.DependsOn {
			if _, exists := stepMap[depID]; !exists {
				return fmt.Errorf("step %s depends on unknown step %s", step.ID, depID)
			}
		}
	}

	// Check for cycles using DFS
	visited := make(map[string]bool)
	recStack := make(map[string]bool)

	var hasCycle func(string) bool
	hasCycle = func(stepID string) bool {
		visited[stepID] = true
		recStack[stepID] = true

		step := stepMap[stepID]
		if step != nil {
			for _, depID := range step.DependsOn {
				if !visited[depID] && hasCycle(depID) {
					return true
				} else if recStack[depID] {
					return true
				}
			}
		}

		recStack[stepID] = false
		return false
	}

	for _, step := range o.plan.Steps {
		if !visited[step.ID] && hasCycle(step.ID) {
			return fmt.Errorf("circular dependency detected in orchestration steps")
		}
	}

	return nil
}

// getExecutionOrder returns the optimal execution order for all steps
func (o *MultiAgentOrchestrator) getExecutionOrder() ([]string, error) {
	sortedSteps := o.sortStepsByDependencies(o.plan.Steps)
	var order []string
	for _, step := range sortedSteps {
		order = append(order, step.ID)
	}
	return order, nil
}

// getStepsByAgent groups steps by their assigned agent
func (o *MultiAgentOrchestrator) getStepsByAgent() map[string][]*types.OrchestrationStep {
	stepsByAgent := make(map[string][]*types.OrchestrationStep)

	for i := range o.plan.Steps {
		step := &o.plan.Steps[i]
		stepsByAgent[step.AgentID] = append(stepsByAgent[step.AgentID], step)
	}

	return stepsByAgent
}

// getCriticalPath identifies the longest dependency chain
func (o *MultiAgentOrchestrator) getCriticalPath() []string {
	if len(o.plan.Steps) == 0 {
		return nil
	}

	// Build dependency chains
	chains := o.buildDependencyChains()

	// Find the longest chain
	var longest []string
	for _, chain := range chains {
		if len(chain) > len(longest) {
			longest = chain
		}
	}

	return longest
}

// buildDependencyChains creates all dependency chains from steps to their roots
func (o *MultiAgentOrchestrator) buildDependencyChains() [][]string {
	var chains [][]string
	stepMap := make(map[string]*types.OrchestrationStep)
	for i := range o.plan.Steps {
		stepMap[o.plan.Steps[i].ID] = &o.plan.Steps[i]
	}

	var buildChain func(string, []string) []string
	buildChain = func(stepID string, currentChain []string) []string {
		// Add current step to chain
		chain := append(currentChain, stepID)

		step := stepMap[stepID]
		if step == nil || len(step.DependsOn) == 0 {
			// End of chain
			return chain
		}

		// Continue with dependencies
		for _, depID := range step.DependsOn {
			newChain := buildChain(depID, chain)
			chains = append(chains, newChain)
		}

		return chain
	}

	// Build chains from all steps
	for _, step := range o.plan.Steps {
		chain := buildChain(step.ID, nil)
		if len(chain) > 1 { // Only include chains with dependencies
			chains = append(chains, chain)
		}
	}

	return chains
}
