package orchestration

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/orchestration/types"
)

// loadStateIfCompatible loads previous orchestration state if compatible
func (o *MultiAgentOrchestrator) loadStateIfCompatible() error {
	if _, err := os.Stat(o.statePath); os.IsNotExist(err) {
		return err // No state file to load
	}

	data, err := os.ReadFile(o.statePath)
	if err != nil {
		return fmt.Errorf("failed to read state file: %w", err)
	}

	var saved types.MultiAgentOrchestrationPlan
	if err := json.Unmarshal(data, &saved); err != nil {
		return fmt.Errorf("failed to parse state file: %w", err)
	}

	// Check compatibility
	if err := o.ensureCompatibility(&saved); err != nil {
		return fmt.Errorf("state file is not compatible: %w", err)
	}

	// Restore state
	o.plan = &saved

	// Rebuild agent runners from restored state
	for _, agentDef := range o.plan.Agents {
		if status, exists := o.plan.AgentStatuses[agentDef.ID]; exists {
			agentConfig := o.createAgentConfig(&agentDef)
			agentRunner := &AgentRunner{
				definition: &agentDef,
				status:     &status,
				config:     agentConfig,
				logger:     o.logger,
			}
			o.agents[agentDef.ID] = agentRunner
		}
	}

	// Rebuild step dependencies
	o.stepDeps = buildStepDependencies(o.plan.Steps)

	return nil
}

// saveState persists the current orchestration state to disk
func (o *MultiAgentOrchestrator) saveState() error {
	// Ensure directory exists
	dir := filepath.Dir(o.statePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create state directory: %w", err)
	}

	// Serialize current state
	data, err := json.MarshalIndent(o.plan, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize state: %w", err)
	}

	// Write to temporary file first, then rename for atomicity
	tempPath := o.statePath + ".tmp"
	if err := os.WriteFile(tempPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write temporary state file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tempPath, o.statePath); err != nil {
		os.Remove(tempPath) // Clean up temp file
		return fmt.Errorf("failed to save state file: %w", err)
	}

	return nil
}

// ensureCompatibility checks if saved state is compatible with current configuration
func (o *MultiAgentOrchestrator) ensureCompatibility(saved *types.MultiAgentOrchestrationPlan) error {
	// Check basic compatibility
	if saved.Goal != o.plan.Goal {
		return fmt.Errorf("goal mismatch: saved='%s', current='%s'", saved.Goal, o.plan.Goal)
	}

	if saved.BaseModel != o.plan.BaseModel {
		return fmt.Errorf("base model mismatch: saved='%s', current='%s'", saved.BaseModel, o.plan.BaseModel)
	}

	// Check agents compatibility
	if len(saved.Agents) != len(o.plan.Agents) {
		return fmt.Errorf("agent count mismatch: saved=%d, current=%d", len(saved.Agents), len(o.plan.Agents))
	}

	// Create maps for agent lookup
	savedAgents := make(map[string]types.AgentDefinition)
	currentAgents := make(map[string]types.AgentDefinition)

	for _, agent := range saved.Agents {
		savedAgents[agent.ID] = agent
	}
	for _, agent := range o.plan.Agents {
		currentAgents[agent.ID] = agent
	}

	// Check each agent for compatibility
	for id, savedAgent := range savedAgents {
		currentAgent, exists := currentAgents[id]
		if !exists {
			return fmt.Errorf("agent %s not found in current configuration", id)
		}

		if savedAgent.Name != currentAgent.Name {
			return fmt.Errorf("agent %s name mismatch: saved='%s', current='%s'", id, savedAgent.Name, currentAgent.Name)
		}

		if savedAgent.Persona != currentAgent.Persona {
			return fmt.Errorf("agent %s persona mismatch: saved='%s', current='%s'", id, savedAgent.Persona, currentAgent.Persona)
		}

		if savedAgent.Model != currentAgent.Model {
			return fmt.Errorf("agent %s model mismatch: saved='%s', current='%s'", id, savedAgent.Model, currentAgent.Model)
		}
	}

	// Check steps compatibility
	if len(saved.Steps) != len(o.plan.Steps) {
		return fmt.Errorf("step count mismatch: saved=%d, current=%d", len(saved.Steps), len(o.plan.Steps))
	}

	// Create maps for step lookup
	savedSteps := make(map[string]types.OrchestrationStep)
	currentSteps := make(map[string]types.OrchestrationStep)

	for _, step := range saved.Steps {
		savedSteps[step.ID] = step
	}
	for _, step := range o.plan.Steps {
		currentSteps[step.ID] = step
	}

	// Check each step for compatibility
	for id, savedStep := range savedSteps {
		currentStep, exists := currentSteps[id]
		if !exists {
			return fmt.Errorf("step %s not found in current configuration", id)
		}

		if savedStep.Name != currentStep.Name {
			return fmt.Errorf("step %s name mismatch: saved='%s', current='%s'", id, savedStep.Name, currentStep.Name)
		}

		if savedStep.AgentID != currentStep.AgentID {
			return fmt.Errorf("step %s agent mismatch: saved='%s', current='%s'", id, savedStep.AgentID, currentStep.AgentID)
		}

		// Check dependencies
		if len(savedStep.DependsOn) != len(currentStep.DependsOn) {
			return fmt.Errorf("step %s dependency count mismatch: saved=%d, current=%d", id, len(savedStep.DependsOn), len(currentStep.DependsOn))
		}

		savedDeps := make(map[string]bool)
		for _, dep := range savedStep.DependsOn {
			savedDeps[dep] = true
		}

		for _, dep := range currentStep.DependsOn {
			if !savedDeps[dep] {
				return fmt.Errorf("step %s missing dependency %s in saved state", id, dep)
			}
		}
	}

	return nil
}

// getStateSummary returns a human-readable summary of the current state
func (o *MultiAgentOrchestrator) getStateSummary() string {
	completed := 0
	inProgress := 0
	pending := 0
	failed := 0

	for _, step := range o.plan.Steps {
		switch step.Status {
		case "completed":
			completed++
		case "in_progress":
			inProgress++
		case "pending":
			pending++
		case "failed":
			failed++
		}
	}

	summary := fmt.Sprintf("State: %s\n", o.plan.Status)
	summary += fmt.Sprintf("Progress: %d/%d steps completed\n", completed, len(o.plan.Steps))
	if inProgress > 0 {
		summary += fmt.Sprintf("In Progress: %d steps\n", inProgress)
	}
	if pending > 0 {
		summary += fmt.Sprintf("Pending: %d steps\n", pending)
	}
	if failed > 0 {
		summary += fmt.Sprintf("Failed: %d steps\n", failed)
	}

	if o.plan.LastError != "" {
		summary += fmt.Sprintf("Last Error: %s\n", o.plan.LastError)
	}

	if o.plan.CreatedAt != "" {
		summary += fmt.Sprintf("Started: %s\n", o.plan.CreatedAt)
	}

	if o.plan.CompletedAt != "" {
		summary += fmt.Sprintf("Completed: %s\n", o.plan.CompletedAt)
	}

	return strings.TrimSpace(summary)
}

// cleanupState removes the state file
func (o *MultiAgentOrchestrator) cleanupState() error {
	if _, err := os.Stat(o.statePath); err == nil {
		return os.Remove(o.statePath)
	}
	return nil // File doesn't exist, nothing to clean up
}

// backupState creates a backup of the current state file
func (o *MultiAgentOrchestrator) backupState() error {
	if _, err := os.Stat(o.statePath); os.IsNotExist(err) {
		return nil // No state file to backup
	}

	backupPath := o.statePath + ".backup"
	data, err := os.ReadFile(o.statePath)
	if err != nil {
		return fmt.Errorf("failed to read state file for backup: %w", err)
	}

	if err := os.WriteFile(backupPath, data, 0644); err != nil {
		return fmt.Errorf("failed to create backup: %w", err)
	}

	return nil
}

// restoreFromBackup restores state from backup file
func (o *MultiAgentOrchestrator) restoreFromBackup() error {
	backupPath := o.statePath + ".backup"
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		return fmt.Errorf("no backup file found")
	}

	if err := os.Rename(backupPath, o.statePath); err != nil {
		return fmt.Errorf("failed to restore from backup: %w", err)
	}

	return o.loadStateIfCompatible()
}

// isStateFileValid checks if the state file exists and is valid JSON
func (o *MultiAgentOrchestrator) isStateFileValid() bool {
	if _, err := os.Stat(o.statePath); os.IsNotExist(err) {
		return false
	}

	data, err := os.ReadFile(o.statePath)
	if err != nil {
		return false
	}

	var state types.MultiAgentOrchestrationPlan
	return json.Unmarshal(data, &state) == nil
}

// getStateAge returns how long ago the state file was last modified
func (o *MultiAgentOrchestrator) getStateAge() (time.Duration, error) {
	stat, err := os.Stat(o.statePath)
	if err != nil {
		return 0, err
	}
	return time.Since(stat.ModTime()), nil
}
