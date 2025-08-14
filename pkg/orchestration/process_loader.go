package orchestration

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/alantheprice/ledit/pkg/orchestration/types"
)

// ProcessLoader handles loading and validating process files
type ProcessLoader struct{}

// NewProcessLoader creates a new process loader
func NewProcessLoader() *ProcessLoader {
	return &ProcessLoader{}
}

// LoadProcessFile loads a process file from the given path
func (l *ProcessLoader) LoadProcessFile(filePath string) (*types.ProcessFile, error) {
	// Read the file
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read process file: %w", err)
	}

	// Parse JSON
	var processFile types.ProcessFile
	if err := json.Unmarshal(data, &processFile); err != nil {
		return nil, fmt.Errorf("failed to parse process file JSON: %w", err)
	}

	// Validate the process file
	if err := l.validateProcessFile(&processFile); err != nil {
		return nil, fmt.Errorf("process file validation failed: %w", err)
	}

	// Set defaults for missing fields
	l.setDefaults(&processFile)

	return &processFile, nil
}

// LoadProcessFromBytes parses and validates a process file from raw JSON bytes
func (l *ProcessLoader) LoadProcessFromBytes(data []byte) (*types.ProcessFile, error) {
	var processFile types.ProcessFile
	if err := json.Unmarshal(data, &processFile); err != nil {
		return nil, fmt.Errorf("failed to parse process file JSON: %w", err)
	}
	if err := l.validateProcessFile(&processFile); err != nil {
		return nil, fmt.Errorf("process file validation failed: %w", err)
	}
	l.setDefaults(&processFile)
	return &processFile, nil
}

// validateProcessFile validates the structure and content of a process file
func (l *ProcessLoader) validateProcessFile(processFile *types.ProcessFile) error {
	// Check required fields
	if processFile.Goal == "" {
		return fmt.Errorf("goal is required")
	}

	if len(processFile.Agents) == 0 {
		return fmt.Errorf("at least one agent must be defined")
	}

	if len(processFile.Steps) == 0 {
		return fmt.Errorf("at least one step must be defined")
	}

	// Validate agents
	agentIDs := make(map[string]bool)
	for i, agent := range processFile.Agents {
		if err := l.validateAgent(&agent, i); err != nil {
			return fmt.Errorf("agent %d: %w", i+1, err)
		}
		if agentIDs[agent.ID] {
			return fmt.Errorf("duplicate agent ID: %s", agent.ID)
		}
		agentIDs[agent.ID] = true
	}

	// Build step ID set for dependency validation
	stepIDs := make(map[string]bool)
	for _, s := range processFile.Steps {
		if s.ID != "" {
			stepIDs[s.ID] = true
		}
	}

	// Validate steps
	for i, step := range processFile.Steps {
		if err := l.validateStep(&step, i, agentIDs, stepIDs); err != nil {
			return fmt.Errorf("step %d: %w", i+1, err)
		}
	}

	// Check for circular dependencies
	if err := l.checkCircularDependencies(processFile.Steps); err != nil {
		return fmt.Errorf("circular dependencies detected: %w", err)
	}

	return nil
}

// validateAgent validates a single agent definition
func (l *ProcessLoader) validateAgent(agent *types.AgentDefinition, index int) error {
	if agent.ID == "" {
		return fmt.Errorf("agent ID is required")
	}

	if agent.Name == "" {
		return fmt.Errorf("agent name is required")
	}

	if agent.Persona == "" {
		return fmt.Errorf("agent persona is required")
	}

	if agent.Description == "" {
		return fmt.Errorf("agent description is required")
	}

	// Validate dependencies
	for _, depID := range agent.DependsOn {
		if depID == agent.ID {
			return fmt.Errorf("agent cannot depend on itself")
		}
	}

	return nil
}

// validateStep validates a single step definition
func (l *ProcessLoader) validateStep(step *types.OrchestrationStep, index int, agentIDs map[string]bool, stepIDs map[string]bool) error {
	if step.ID == "" {
		return fmt.Errorf("step ID is required")
	}

	if step.Name == "" {
		return fmt.Errorf("step name is required")
	}

	if step.Description == "" {
		return fmt.Errorf("step description is required")
	}

	if step.AgentID == "" {
		return fmt.Errorf("step agent ID is required")
	}

	// Check if the agent exists
	if !agentIDs[step.AgentID] {
		return fmt.Errorf("agent ID '%s' not found", step.AgentID)
	}

	// Validate dependencies
	for _, depID := range step.DependsOn {
		if depID == step.ID {
			return fmt.Errorf("step cannot depend on itself")
		}
		if !stepIDs[depID] {
			return fmt.Errorf("depends_on references missing step id '%s'", depID)
		}
	}

	return nil
}

// checkCircularDependencies checks for circular dependencies in steps
func (l *ProcessLoader) checkCircularDependencies(steps []types.OrchestrationStep) error {
	// Build adjacency list
	graph := make(map[string][]string)
	for _, step := range steps {
		graph[step.ID] = step.DependsOn
	}

	// Check for cycles using DFS
	visited := make(map[string]bool)
	recStack := make(map[string]bool)

	for _, step := range steps {
		if !visited[step.ID] {
			if l.hasCycle(step.ID, graph, visited, recStack) {
				return fmt.Errorf("circular dependency detected")
			}
		}
	}

	return nil
}

// hasCycle performs DFS to detect cycles in the dependency graph
func (l *ProcessLoader) hasCycle(node string, graph map[string][]string, visited, recStack map[string]bool) bool {
	visited[node] = true
	recStack[node] = true

	for _, neighbor := range graph[node] {
		if !visited[neighbor] {
			if l.hasCycle(neighbor, graph, visited, recStack) {
				return true
			}
		} else if recStack[neighbor] {
			return true
		}
	}

	recStack[node] = false
	return false
}

// setDefaults sets default values for missing fields
func (l *ProcessLoader) setDefaults(processFile *types.ProcessFile) {
	// Set version if not specified
	if processFile.Version == "" {
		processFile.Version = "1.0"
	}

	// Set default settings if not specified
	if processFile.Settings == nil {
		processFile.Settings = &types.ProcessSettings{
			MaxRetries:        3,
			StepTimeout:       300, // 5 minutes
			ParallelExecution: false,
			StopOnFailure:     true,
			LogLevel:          "info",
		}
	}

	// Set default validation if not specified
	if processFile.Validation == nil {
		processFile.Validation = &types.ValidationConfig{
			Required: false,
		}
	}

	// Set defaults for agents
	for i := range processFile.Agents {
		agent := &processFile.Agents[i]
		if agent.Priority == 0 {
			agent.Priority = 100 // Default priority
		}
		if agent.Config == nil {
			agent.Config = make(map[string]string)
		}
		// Set default budget if not specified
		if agent.Budget == nil {
			agent.Budget = &types.AgentBudget{
				MaxTokens:    0,     // No limit by default
				MaxCost:      0.0,   // No limit by default
				TokenWarning: 0,     // No warning by default
				CostWarning:  0.0,   // No warning by default
				AlertOnLimit: false, // No alerts by default
				StopOnLimit:  false, // Don't stop by default
			}
		}
	}

	// Set defaults for steps
	for i := range processFile.Steps {
		step := &processFile.Steps[i]
		if step.Timeout == 0 {
			step.Timeout = processFile.Settings.StepTimeout
		}
		if step.Retries == 0 {
			step.Retries = processFile.Settings.MaxRetries
		}
		if step.Status == "" {
			step.Status = "pending"
		}
	}
}

// CreateExampleProcessFile creates an example process file for reference
func (l *ProcessLoader) CreateExampleProcessFile(filePath string) error {
	exampleProcess := types.ProcessFile{
		Version: "1.0",
		Goal:    "Implement a complete web application with frontend and backend",
		Description: `This process demonstrates multi-agent orchestration where different agents
with specialized personas work together to build a complete application.`,
		BaseModel: "gpt-4",
		Agents: []types.AgentDefinition{
			{
				ID:          "architect",
				Name:        "System Architect",
				Persona:     "system_architect",
				Description: "Designs the overall system architecture and database schema",
				Skills:      []string{"system_design", "database_design", "api_design"},
				Model:       "gpt-4",
				Priority:    1,
				DependsOn:   []string{},
				Config:      map[string]string{"skip_prompt": "false"},
				Budget: &types.AgentBudget{
					MaxTokens:    100000,
					MaxCost:      10.0,
					TokenWarning: 80000,
					CostWarning:  8.0,
					AlertOnLimit: true,
					StopOnLimit:  false,
				},
			},
			{
				ID:          "backend_dev",
				Name:        "Backend Developer",
				Persona:     "backend_developer",
				Description: "Implements the backend API and business logic",
				Skills:      []string{"api_development", "business_logic", "database_operations"},
				Model:       "gpt-4",
				Priority:    2,
				DependsOn:   []string{"architect"},
				Config:      map[string]string{"skip_prompt": "false"},
				Budget: &types.AgentBudget{
					MaxTokens:    150000,
					MaxCost:      15.0,
					TokenWarning: 120000,
					CostWarning:  12.0,
					AlertOnLimit: true,
					StopOnLimit:  false,
				},
			},
			{
				ID:          "frontend_dev",
				Name:        "Frontend Developer",
				Persona:     "frontend_developer",
				Description: "Creates the user interface and frontend components",
				Skills:      []string{"ui_design", "frontend_framework", "user_experience"},
				Model:       "gpt-4",
				Priority:    2,
				DependsOn:   []string{"architect"},
				Config:      map[string]string{"skip_prompt": "false"},
				Budget: &types.AgentBudget{
					MaxTokens:    120000,
					MaxCost:      12.0,
					TokenWarning: 96000,
					CostWarning:  9.6,
					AlertOnLimit: true,
					StopOnLimit:  false,
				},
			},
			{
				ID:          "qa_engineer",
				Name:        "QA Engineer",
				Persona:     "qa_engineer",
				Description: "Tests the application and ensures quality",
				Skills:      []string{"testing", "quality_assurance", "bug_tracking"},
				Model:       "gpt-4",
				Priority:    3,
				DependsOn:   []string{"backend_dev", "frontend_dev"},
				Config:      map[string]string{"skip_prompt": "false"},
				Budget: &types.AgentBudget{
					MaxTokens:    80000,
					MaxCost:      8.0,
					TokenWarning: 64000,
					CostWarning:  6.4,
					AlertOnLimit: true,
					StopOnLimit:  false,
				},
			},
		},
		Steps: []types.OrchestrationStep{
			{
				ID:             "design_architecture",
				Name:           "Design System Architecture",
				Description:    "Create the overall system architecture including database schema, API design, and component structure",
				AgentID:        "architect",
				Input:          map[string]string{"requirements": "web application with user management and data storage"},
				ExpectedOutput: "System architecture document with database schema and API specifications",
				Status:         "pending",
				DependsOn:      []string{},
				Timeout:        600,
				Retries:        2,
			},
			{
				ID:             "implement_backend",
				Name:           "Implement Backend API",
				Description:    "Build the backend API endpoints, business logic, and database operations",
				AgentID:        "backend_dev",
				Input:          map[string]string{"architecture": "Use the architecture from previous step"},
				ExpectedOutput: "Working backend API with all endpoints and database operations",
				Status:         "pending",
				DependsOn:      []string{"design_architecture"},
				Timeout:        900,
				Retries:        3,
			},
			{
				ID:             "implement_frontend",
				Name:           "Implement Frontend UI",
				Description:    "Create the user interface components and frontend application",
				AgentID:        "frontend_dev",
				Input:          map[string]string{"architecture": "Use the architecture from previous step"},
				ExpectedOutput: "Working frontend application with all UI components",
				Status:         "pending",
				DependsOn:      []string{"design_architecture"},
				Timeout:        900,
				Retries:        3,
			},
			{
				ID:             "test_application",
				Name:           "Test Complete Application",
				Description:    "Perform comprehensive testing including unit tests, integration tests, and user acceptance testing",
				AgentID:        "qa_engineer",
				Input:          map[string]string{"backend": "Test the backend API", "frontend": "Test the frontend UI"},
				ExpectedOutput: "Test results report with any issues found and recommendations",
				Status:         "pending",
				DependsOn:      []string{"implement_backend", "implement_frontend"},
				Timeout:        600,
				Retries:        2,
			},
		},
		Validation: &types.ValidationConfig{
			BuildCommand: "npm run build",
			TestCommand:  "npm test",
			LintCommand:  "npm run lint",
			CustomChecks: []string{"npm run security-check"},
			Required:     true,
		},
		Settings: &types.ProcessSettings{
			MaxRetries:        3,
			StepTimeout:       900,
			ParallelExecution: false,
			StopOnFailure:     true,
			LogLevel:          "info",
		},
	}

	// Create directory if it doesn't exist
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Write the example file
	data, err := json.MarshalIndent(exampleProcess, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal example process: %w", err)
	}

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write example process file: %w", err)
	}

	return nil
}
