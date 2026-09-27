// Package workflow is the in-process TODO-loop workflow runner extracted
// from pkg/agent (SP-141 phase 1). It runs the loop against an already
// constructed workflow agent via a narrow Agent interface, so agent
// construction — which needs unexported Agent fields — stays in pkg/agent.
// This package deliberately does not import pkg/agent; the Agent and
// Budget interfaces keep the dependency direction one-way.
package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// Config types — lightweight subset of cmd/AgentWorkflowConfig, parsed
// directly from the workflow JSON so the agent package has no import cycle
// with cmd/.

// LoopConfig is parsed from the "loop" section of a workflow JSON
// file. Only the fields relevant to the in-process runner are included.
type LoopConfig struct {
	TodoFile       string `json:"todo_file,omitempty"`
	GatePromptFile string `json:"gate_prompt_file,omitempty"`
	MaxRetries     int    `json:"max_retries,omitempty"`
	MaxIterations  int    `json:"max_iterations,omitempty"`
	BuildCommand   string `json:"build_command,omitempty"`
}

// ApplyDefaults fills in zero-value fields with the same defaults used by
// cmd/agent_workflow_loader.go so the runner behaves identically.
func (c *LoopConfig) ApplyDefaults() {
	if c.TodoFile == "" {
		c.TodoFile = "TODO.md"
	}
	if c.MaxRetries <= 0 {
		c.MaxRetries = 2
	}
	if c.MaxIterations <= 0 {
		c.MaxIterations = 50
	}
	if c.BuildCommand == "" {
		c.BuildCommand = "go build ./..."
	}
}

// BudgetConfig is parsed from the "budget" section of a workflow JSON.
type BudgetConfig struct {
	USD    float64   `json:"usd,omitempty"`
	WarnAt []float64 `json:"warn_at,omitempty"`
}

// ProgressConfig is parsed from the "progress" section.
type ProgressConfig struct {
	HeartbeatSeconds int `json:"heartbeat_seconds,omitempty"`
}

// FileConfig is the top-level structure parsed from the workflow JSON
// file. It mirrors only the fields the in-process runner cares about.
type FileConfig struct {
	Description string          `json:"description,omitempty"`
	Loop        *LoopConfig     `json:"loop,omitempty"`
	Budget      *BudgetConfig   `json:"budget,omitempty"`
	Progress    *ProgressConfig `json:"progress,omitempty"`
}

// ParseFile reads and parses a workflow JSON file for its loop
// configuration. Returns only the fields the in-process runner needs.
func ParseFile(path string) (*FileConfig, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, agenterrors.NewAgent("workflow_runner", fmt.Sprintf("failed to read %q", path), err)
	}
	var cfg FileConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, agenterrors.NewInvalidInputError(fmt.Sprintf("failed to parse %q", path), err)
	}
	return &cfg, nil
}
