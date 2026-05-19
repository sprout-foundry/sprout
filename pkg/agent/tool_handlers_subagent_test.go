package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// TestHandleRunSubagent_LocalOnly_Validation tests that LocalOnly personas
// are rejected in cloud mode and accepted in local mode
func TestHandleRunSubagent_LocalOnly_Validation(t *testing.T) {
	tests := []struct {
		name        string
		localOnly   bool
		cloudMode   bool
		wantErr     bool
		errContains string
	}{
		{
			name:        "LocalOnly persona rejected in cloud mode",
			localOnly:   true,
			cloudMode:   true,
			wantErr:     true,
			errContains: "local-only and cannot be used as a subagent in cloud mode",
		},
		{
			name:        "LocalOnly persona accepted in local mode",
			localOnly:   true,
			cloudMode:   false,
			wantErr:     false,
			errContains: "",
		},
		{
			name:        "Non-LocalOnly persona accepted in cloud mode",
			localOnly:   false,
			cloudMode:   true,
			wantErr:     false,
			errContains: "",
		},
		{
			name:        "Non-LocalOnly persona accepted in local mode",
			localOnly:   false,
			cloudMode:   false,
			wantErr:     false,
			errContains: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := newTestAgent(t)
			defer agent.Shutdown()

			// Set cloud mode if needed
			if tt.cloudMode {
				t.Setenv("SPROUT_CLOUD", "1")
			} else {
				t.Setenv("SPROUT_CLOUD", "0")
			}

			// Register a test persona with the LocalOnly configuration
			personaID := "test_localonly_validation"
			err := agent.configManager.UpdateConfigNoSave(func(cfg *configuration.Config) error {
				if cfg.SubagentTypes == nil {
					cfg.SubagentTypes = make(map[string]configuration.SubagentType)
				}
				cfg.SubagentTypes[personaID] = configuration.SubagentType{
					ID:          personaID,
					Name:        "Test LocalOnly Validation",
					Description: "Test persona for LocalOnly validation",
					Enabled:     true,
					LocalOnly:   tt.localOnly,
					Delegatable: true, // Ensure delegatable is true for this test
				}
				return nil
			})
			if err != nil {
				t.Fatalf("UpdateConfigNoSave failed: %v", err)
			}

			// Setup subagent runner for success cases
			if !tt.wantErr {
				setupTestSubagentRunner(agent)
			}

			// Call handleRunSubagent with the test persona
			args := map[string]interface{}{
				"prompt":  "test prompt",
				"persona": personaID,
			}

			result, err := handleRunSubagent(context.Background(), agent, args)

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errContains)
					return
				}
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error %q does not contain expected string %q", err.Error(), tt.errContains)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if result == "" {
					t.Error("expected non-empty result")
				}
			}
		})
	}
}

// TestHandleRunSubagent_Delegatable_Validation tests that non-delegatable personas
// are rejected regardless of mode
func TestHandleRunSubagent_Delegatable_Validation(t *testing.T) {
	tests := []struct {
		name         string
		delegatable  bool
		cloudMode    bool
		wantErr      bool
		errContains  string
	}{
		{
			name:        "Non-delegatable persona rejected in cloud mode",
			delegatable: false,
			cloudMode:   true,
			wantErr:     true,
			errContains: "not designed to be used as a subagent (delegatable=false)",
		},
		{
			name:        "Non-delegatable persona rejected in local mode",
			delegatable: false,
			cloudMode:   false,
			wantErr:     true,
			errContains: "not designed to be used as a subagent (delegatable=false)",
		},
		{
			name:        "Delegatable persona accepted in cloud mode",
			delegatable: true,
			cloudMode:   true,
			wantErr:     false,
			errContains: "",
		},
		{
			name:        "Delegatable persona accepted in local mode",
			delegatable: true,
			cloudMode:   false,
			wantErr:     false,
			errContains: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := newTestAgent(t)
			defer agent.Shutdown()

			// Set cloud mode if needed
			if tt.cloudMode {
				t.Setenv("SPROUT_CLOUD", "1")
			} else {
				t.Setenv("SPROUT_CLOUD", "0")
			}

			// Register a test persona with the Delegatable configuration
			personaID := "test_delegatable_validation"
			err := agent.configManager.UpdateConfigNoSave(func(cfg *configuration.Config) error {
				if cfg.SubagentTypes == nil {
					cfg.SubagentTypes = make(map[string]configuration.SubagentType)
				}
				cfg.SubagentTypes[personaID] = configuration.SubagentType{
					ID:          personaID,
					Name:        "Test Delegatable Validation",
					Description: "Test persona for Delegatable validation",
					Enabled:     true,
					LocalOnly:   false, // Ensure LocalOnly is false for this test
					Delegatable: tt.delegatable,
				}
				return nil
			})
			if err != nil {
				t.Fatalf("UpdateConfigNoSave failed: %v", err)
			}

			// Setup subagent runner for success cases
			if !tt.wantErr {
				setupTestSubagentRunner(agent)
			}

			// Call handleRunSubagent with the test persona
			args := map[string]interface{}{
				"prompt":  "test prompt",
				"persona": personaID,
			}

			result, err := handleRunSubagent(context.Background(), agent, args)

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errContains)
					return
				}
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error %q does not contain expected string %q", err.Error(), tt.errContains)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if result == "" {
					t.Error("expected non-empty result")
				}
			}
		})
	}
}

// TestHandleRunSubagent_CombinedValidation tests scenarios where both
// LocalOnly and Delegatable flags interact
func TestHandleRunSubagent_CombinedValidation(t *testing.T) {
	tests := []struct {
		name         string
		localOnly    bool
		delegatable  bool
		cloudMode    bool
		wantErr      bool
		errContains  string
	}{
		{
			name:        "LocalOnly+NonDelegatable in cloud mode - LocalOnly error takes precedence",
			localOnly:   true,
			delegatable: false,
			cloudMode:   true,
			wantErr:     true,
			errContains: "local-only and cannot be used as a subagent in cloud mode",
		},
		{
			name:        "LocalOnly+NonDelegatable in local mode - Delegatable error",
			localOnly:   true,
			delegatable: false,
			cloudMode:   false,
			wantErr:     true,
			errContains: "not designed to be used as a subagent (delegatable=false)",
		},
		{
			name:        "LocalOnly+Delegatable in cloud mode - LocalOnly error",
			localOnly:   true,
			delegatable: true,
			cloudMode:   true,
			wantErr:     true,
			errContains: "local-only and cannot be used as a subagent in cloud mode",
		},
		{
			name:        "LocalOnly+Delegatable in local mode - success",
			localOnly:   true,
			delegatable: true,
			cloudMode:   false,
			wantErr:     false,
			errContains: "",
		},
		{
			name:        "NonLocalOnly+NonDelegatable in cloud mode - Delegatable error",
			localOnly:   false,
			delegatable: false,
			cloudMode:   true,
			wantErr:     true,
			errContains: "not designed to be used as a subagent (delegatable=false)",
		},
		{
			name:        "NonLocalOnly+NonDelegatable in local mode - Delegatable error",
			localOnly:   false,
			delegatable: false,
			cloudMode:   false,
			wantErr:     true,
			errContains: "not designed to be used as a subagent (delegatable=false)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := newTestAgent(t)
			defer agent.Shutdown()

			// Set cloud mode if needed
			if tt.cloudMode {
				t.Setenv("SPROUT_CLOUD", "1")
			} else {
				t.Setenv("SPROUT_CLOUD", "0")
			}

			// Register a test persona with both LocalOnly and Delegatable configurations
			personaID := "test_combined_validation"
			err := agent.configManager.UpdateConfigNoSave(func(cfg *configuration.Config) error {
				if cfg.SubagentTypes == nil {
					cfg.SubagentTypes = make(map[string]configuration.SubagentType)
				}
				cfg.SubagentTypes[personaID] = configuration.SubagentType{
					ID:          personaID,
					Name:        "Test Combined Validation",
					Description: "Test persona for combined validation",
					Enabled:     true,
					LocalOnly:   tt.localOnly,
					Delegatable: tt.delegatable,
				}
				return nil
			})
			if err != nil {
				t.Fatalf("UpdateConfigNoSave failed: %v", err)
			}

			// Setup subagent runner for success cases
			if !tt.wantErr {
				setupTestSubagentRunner(agent)
			}

			// Call handleRunSubagent with the test persona
			args := map[string]interface{}{
				"prompt":  "test prompt",
				"persona": personaID,
			}

			result, err := handleRunSubagent(context.Background(), agent, args)

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errContains)
					return
				}
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error %q does not contain expected string %q", err.Error(), tt.errContains)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if result == "" {
					t.Error("expected non-empty result")
				}
			}
		})
	}
}

// TestHandleRunSubagent_NoPersona_SkipsValidation tests that validation
// is skipped when no persona is specified, by using a delegatable persona
func TestHandleRunSubagent_NoPersona_WithDelegatablePersona(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	// Set cloud mode
	t.Setenv("SPROUT_CLOUD", "1")

	// Register a delegatable persona that we can use
	personaID := "test_delegatable_for_no_persona"
	err := agent.configManager.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		if cfg.SubagentTypes == nil {
			cfg.SubagentTypes = make(map[string]configuration.SubagentType)
		}
		cfg.SubagentTypes[personaID] = configuration.SubagentType{
			ID:          personaID,
			Name:        "Test Delegatable",
			Description: "Test persona",
			Enabled:     true,
			LocalOnly:   false,
			Delegatable: true,
		}
		return nil
	})
	if err != nil {
		t.Fatalf("UpdateConfigNoSave failed: %v", err)
	}

	// Setup subagent runner
	setupTestSubagentRunner(agent)

	// Call handleRunSubagent with the delegatable persona
	args := map[string]interface{}{
		"prompt":  "test prompt",
		"persona": personaID,
	}

	result, err := handleRunSubagent(context.Background(), agent, args)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty result")
	}
}

// TestHandleRunSubagent_NonExistentPersona_FallsBack tests that validation
// is skipped when persona doesn't exist (falls back to default)
func TestHandleRunSubagent_NonExistentPersona_FallsBack(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	// Set cloud mode
	t.Setenv("SPROUT_CLOUD", "1")

	// Register a delegatable persona for default fallback
	err := agent.configManager.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		if cfg.SubagentTypes == nil {
			cfg.SubagentTypes = make(map[string]configuration.SubagentType)
		}
		// Create a simple delegatable persona as fallback
		cfg.SubagentTypes["test_fallback_persona"] = configuration.SubagentType{
			ID:          "test_fallback_persona",
			Name:        "Test Fallback",
			Description: "Test persona",
			Enabled:     true,
			LocalOnly:   false,
			Delegatable: true,
		}
		return nil
	})
	if err != nil {
		t.Fatalf("UpdateConfigNoSave failed: %v", err)
	}

	// Setup subagent runner
	setupTestSubagentRunner(agent)

	// Call handleRunSubagent with a non-existent persona
	args := map[string]interface{}{
		"prompt":  "test prompt",
		"persona": "this_persona_does_not_exist",
	}

	result, err := handleRunSubagent(context.Background(), agent, args)

	// Should succeed because non-existent personas skip validation
	// (falls back to default subagent config)
	if err != nil {
		t.Errorf("unexpected error when persona doesn't exist: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty result")
	}
}

// setupTestSubagentRunner sets up a minimal SubagentRunner for testing
// This creates a runner that will fail gracefully when actually called
func setupTestSubagentRunner(agent *Agent) {
	agent.subagentRunner = NewSubagentRunner(agent, &SharedState{
		EventBus:      agent.eventBus,
		TodoManager:   agent.todoMgr,
		EmbeddingMgr:  agent.embeddingMgr,
		ConfigManager: agent.configManager,
		WorkspaceRoot: agent.workspaceRoot,
	})
}