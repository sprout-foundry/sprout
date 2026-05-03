package agent

import (
	"testing"

	"github.com/sprout-foundry/sprout/pkg/security"
)

// TestIsSecretSensitiveTool2 tests secret sensitive tool detection
func TestIsSecretSensitiveTool2(t *testing.T) {
	sensitiveTools := []string{
		"shell_command",
		"read_file",
		"search_files",
		"write_file",
		"edit_file",
		"write_structured_file",
		"patch_structured_file",
	}

	for _, tool := range sensitiveTools {
		t.Run(tool, func(t *testing.T) {
			if !isSecretSensitiveTool(tool) {
				t.Errorf("expected %q to be sensitive", tool)
			}
		})
	}

	nonSensitiveTools := []string{
		"sleep",
		"list_skills",
		"run_subagent",
		"commit",
		"edit_file_regex",
		"save_memory",
	}

	for _, tool := range nonSensitiveTools {
		t.Run(tool, func(t *testing.T) {
			if isSecretSensitiveTool(tool) {
				t.Errorf("expected %q to NOT be sensitive", tool)
			}
		})
	}
}

// TestSecretPrompterAdapter2 tests prompter adapter behavior
func TestSecretPrompterAdapter2(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()

	adapter := &secretPrompterAdapter{agent: a}

	secrets := []security.DetectedSecret{
		{Type: "API Key", Snippet: "sk-123456", Line: 10},
	}

	// Should not panic even with nil UI
	action, err := adapter.PromptSecretAction(secrets, "file.go")

	// With nil UI, will fail and return SecretRedact
	if action != security.SecretRedact {
		t.Errorf("expected SecretRedact with nil UI, got %d", action)
	}

	// Error is expected with nil UI
	if err == nil {
		t.Error("expected error with nil UI")
	}
}

// TestSetElevationGatePrompter2 tests setting elevation gate
func TestSetElevationGatePrompter2(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()

	// Should not panic
	a.SetElevationGatePrompter()

	gate := a.GetElevationGate()
	if gate == nil {
		t.Error("expected elevation gate to not be nil")
	}
}

// TestGetElevationGate2 tests getting elevation gate
func TestGetElevationGate2(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()

	gate := a.GetElevationGate()
	if gate == nil {
		t.Error("expected elevation gate to not be nil")
	}
}

// TestGetOutputRedactor2 tests getting output redactor
func TestGetOutputRedactor2(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()

	redactor := a.GetOutputRedactor()
	if redactor == nil {
		t.Error("expected output redactor to not be nil")
	}
}
