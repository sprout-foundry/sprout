package orchestration

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"time"

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
		} else {
			o.logger.LogProcessStep(fmt.Sprintf("ℹ️ Could not resume from state: %v (starting fresh)", err))
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

// shouldUseLLMTools checks if LLM tools should be used based on step configuration
func shouldUseLLMTools(step *types.OrchestrationStep) bool {
	if step == nil || step.Tools == nil {
		return false
	}

	llmTools, exists := step.Tools["llm_tools"]
	if !exists {
		return false
	}

	// Check various true values
	switch strings.ToLower(strings.TrimSpace(llmTools)) {
	case "true", "1", "enabled", "yes", "on":
		return true
	default:
		return false
	}
}
