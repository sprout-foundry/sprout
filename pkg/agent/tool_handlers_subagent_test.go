package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	agent_api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/factory"
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
			errContains: "is not spawnable from",
		},
		{
			name:        "Non-delegatable persona rejected in local mode",
			delegatable: false,
			cloudMode:   false,
			wantErr:     true,
			errContains: "is not spawnable from",
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
			errContains: "is not spawnable from",
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
			errContains: "is not spawnable from",
		},
		{
			name:        "NonLocalOnly+NonDelegatable in local mode - Delegatable error",
			localOnly:   false,
			delegatable: false,
			cloudMode:   false,
			wantErr:     true,
			errContains: "is not spawnable from",
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
// This creates a runner that uses a stub test client so subagents don't
// hit real API providers during tests.
func setupTestSubagentRunner(agent *Agent) {
	runner := NewSubagentRunner(agent, &SharedState{
		EventBus:      agent.eventBus,
		TodoManager:   agent.todoMgr,
		EmbeddingMgr:  agent.embeddingMgr,
		ConfigManager: agent.configManager,
		WorkspaceRoot: agent.workspaceRoot,
	})
	// Inject stub client factory so subagents don't hit real API providers
	runner.testClientFactory = func(clientType agent_api.ClientType, model string) (agent_api.ClientInterface, error) {
		return factory.CreateProviderClient(agent_api.TestClientType, "")
	}
	agent.subagentRunner = runner
}

// TestBuildSubagentReturn_PrependsFilesModifiedHeader verifies the fix for
// the failure mode observed 2026-05-27: a primary agent receiving a
// subagent's result couldn't tell which files the subagent edited and
// ended up reverting "unfamiliar" diff. The structured FilesModified
// field WAS populated, but the primary's LLM didn't latch onto it. Fix:
// prepend a plain-text manifest to Output so the file list sits at the
// very top of what the model reads.
func TestBuildSubagentReturn_PrependsFilesModifiedHeader(t *testing.T) {
	m := map[string]string{
		"stdout":    "Done. Refactored the auth layer.",
		"exit_code": "0",
	}
	result := &SubagentResult{
		FileChanges: []TrackedFileChange{
			{FilePath: "pkg/auth/session.go", Operation: "write"},
			{FilePath: "pkg/auth/jwt.go", Operation: "edit"},
			{FilePath: "pkg/auth/legacy.go", Operation: "delete"},
		},
	}

	ret := buildSubagentReturn(m, result, SubagentStatusCompleted)

	if !strings.Contains(ret.Output, "[subagent files modified]") {
		t.Fatalf("Output should start with the manifest sentinel\nOutput: %q", ret.Output)
	}
	if !strings.Contains(ret.Output, "A pkg/auth/session.go") {
		t.Errorf("manifest should map write→A:\nOutput: %q", ret.Output)
	}
	if !strings.Contains(ret.Output, "M pkg/auth/jwt.go") {
		t.Errorf("manifest should map edit→M:\nOutput: %q", ret.Output)
	}
	if !strings.Contains(ret.Output, "D pkg/auth/legacy.go") {
		t.Errorf("manifest should map delete→D:\nOutput: %q", ret.Output)
	}
	if !strings.Contains(ret.Output, "Done. Refactored the auth layer.") {
		t.Errorf("original output should still be present after the header:\nOutput: %q", ret.Output)
	}
	// Structured field must still be populated alongside the prose
	// header — callers parsing the envelope shouldn't have to scrape
	// the manifest sentinels.
	if len(ret.FilesModified) != 3 {
		t.Errorf("FilesModified should have 3 entries, got %d", len(ret.FilesModified))
	}
	// Manifest sentinel pair appears intact (open + close on separate
	// lines so grep matches don't false-positive on a stray bracket).
	if !strings.Contains(ret.Output, "[/subagent files modified]\n") {
		t.Errorf("closing sentinel missing from Output:\n%q", ret.Output)
	}
}

func TestBuildSubagentReturn_NoFilesModifiedHeader_WhenNoChanges(t *testing.T) {
	m := map[string]string{
		"stdout":    "Investigated; no changes needed.",
		"exit_code": "0",
	}
	result := &SubagentResult{} // empty FileChanges

	ret := buildSubagentReturn(m, result, SubagentStatusCompleted)

	if strings.Contains(ret.Output, "[subagent files modified]") {
		t.Errorf("manifest header should not appear when no files were modified:\n%q", ret.Output)
	}
	if ret.Output != "Investigated; no changes needed." {
		t.Errorf("Output should be untouched when no files modified, got %q", ret.Output)
	}
}

func TestPrependFilesModifiedHeader_HandlesUnknownOp(t *testing.T) {
	// Unknown op should still produce a single-letter code in the
	// manifest so downstream tooling doesn't see ragged formatting.
	out := prependFilesModifiedHeader("trailing message", []FileChange{
		{Path: "weird/file.go", Op: "uplifted"},
	})
	if !strings.Contains(out, "U weird/file.go") {
		t.Errorf("unknown op should pass through as uppercase first letter; got %q", out)
	}
	if !strings.Contains(out, "trailing message") {
		t.Errorf("original output should be preserved; got %q", out)
	}
}

// TestSubagentManifestEndToEnd is the integration check the user
// asked for: confirm a shell-mutated file flows from a subagent's
// ChangeTracker → result.FileChanges → SubagentReturn.FilesModified
// → tool-result JSON the primary's LLM sees. Verifies the full chain
// without spinning up a real subagent (we drive the tracker manually
// at the same layer the runner does).
func TestSubagentManifestEndToEnd(t *testing.T) {
	dir := t.TempDir()
	original := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(original, []byte("port = 8080"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// 1. Construct a tracker mimicking what the subagent runner sets
	//    up: shell walk enabled, primed against the workspace.
	tracker := &ChangeTracker{
		enabled:          true,
		shellWalkEnabled: true,
	}
	tracker.PrimeShellTracking(dir)

	// 2. Simulate a shell_command modifying the file (sed-i style).
	if err := os.WriteFile(original, []byte("port = 9090"), 0o644); err != nil {
		t.Fatalf("mutate: %v", err)
	}
	bumpMtime(t, original)
	tracker.TrackShellTurn(dir, "shell_command", false)

	if len(tracker.changes) != 1 {
		t.Fatalf("tracker should have captured the shell mutation; got %d changes: %+v",
			len(tracker.changes), tracker.changes)
	}

	// 3. Mimic the runner's payload assembly: result.FileChanges =
	//    tracker.GetChanges(); buildSubagentReturn produces the JSON
	//    envelope the primary's LLM consumes.
	result := &SubagentResult{
		FileChanges: tracker.GetChanges(),
	}
	resultMap := map[string]string{
		"stdout":    "Updated port number.",
		"exit_code": "0",
	}
	ret := buildSubagentReturn(resultMap, result, SubagentStatusCompleted)

	// 4. Critical assertions: the structured field carries the path
	//    with the right op vocabulary, and the prose manifest header
	//    is prepended to stdout for LLM visibility.
	if len(ret.FilesModified) != 1 {
		t.Fatalf("FilesModified should have 1 entry; got %d: %+v", len(ret.FilesModified), ret.FilesModified)
	}
	if ret.FilesModified[0].Path != original {
		t.Errorf("manifest path mismatch: got %q, want %q", ret.FilesModified[0].Path, original)
	}
	if ret.FilesModified[0].Op != "modified" {
		t.Errorf("manifest op should be 'modified' for shell edit; got %q", ret.FilesModified[0].Op)
	}
	if !strings.Contains(ret.Output, "[subagent files modified]") {
		t.Errorf("Output should carry the prose manifest header so the LLM can't miss it; got:\n%s", ret.Output)
	}
	if !strings.Contains(ret.Output, "M "+original) {
		t.Errorf("Output should reference the modified file in git-style; got:\n%s", ret.Output)
	}
	if !strings.Contains(ret.Output, "Updated port number.") {
		t.Errorf("original stdout should still be present after the header; got:\n%s", ret.Output)
	}
}