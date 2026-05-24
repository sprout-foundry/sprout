package agent

import (
	"context"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/security"
)

func TestIsSecretSensitiveTool(t *testing.T) {
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
		if !isSecretSensitiveTool(tool) {
			t.Errorf("expected %q to be sensitive", tool)
		}
	}

	nonSensitiveTools := []string{
		"sleep",
		"list_skills",
		"run_subagent",
		"commit",
		"edit_file_regex",
		"",
		"save_memory",
	}

	for _, tool := range nonSensitiveTools {
		if isSecretSensitiveTool(tool) {
			t.Errorf("expected %q to NOT be sensitive", tool)
		}
	}
}

// mockSecretPrompterUI provides a mock UI for testing secretPrompterAdapter
type mockSecretPrompterUI struct {
	interactive   bool
	quickPromptFn func(prompt string, options []QuickOption, horizontal bool) (QuickOption, error)
}

func (m *mockSecretPrompterUI) IsInteractive() bool {
	return m.interactive
}

func (m *mockSecretPrompterUI) ShowQuickPrompt(ctx context.Context, prompt string, options []QuickOption, horizontal bool) (QuickOption, error) {
	if m.quickPromptFn != nil {
		return m.quickPromptFn(prompt, options, horizontal)
	}
	return QuickOption{}, ErrUINotAvailable
}

func (m *mockSecretPrompterUI) ShowDropdown(ctx context.Context, items interface{}, options DropdownOptions) (interface{}, error) {
	return nil, ErrUINotAvailable
}

func TestSecretPrompterAdapter_Allow(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()
	// Set up a mock UI that returns "allow"
	a.ui = &mockSecretPrompterUI{
		interactive: true,
		quickPromptFn: func(prompt string, options []QuickOption, horizontal bool) (QuickOption, error) {
			return QuickOption{Value: "allow"}, nil
		},
	}

	adapter := &secretPrompterAdapter{agent: a}

	action, err := adapter.PromptSecretAction([]security.DetectedSecret{
		{Type: "API Key", Snippet: "sk-123456", Line: 10},
	}, "file.go")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action != security.SecretAllow {
		t.Errorf("expected SecretAllow, got %d", action)
	}
}

func TestSecretPrompterAdapter_Block(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()
	a.ui = &mockSecretPrompterUI{
		interactive: true,
		quickPromptFn: func(prompt string, options []QuickOption, horizontal bool) (QuickOption, error) {
			return QuickOption{Value: "block"}, nil
		},
	}

	adapter := &secretPrompterAdapter{agent: a}

	action, err := adapter.PromptSecretAction([]security.DetectedSecret{
		{Type: "Bearer Token", Snippet: "ghp_xxxx", Line: 5},
	}, "config.yaml")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action != security.SecretBlock {
		t.Errorf("expected SecretBlock, got %d", action)
	}
}

func TestSecretPrompterAdapter_Redact(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()
	a.ui = &mockSecretPrompterUI{
		interactive: true,
		quickPromptFn: func(prompt string, options []QuickOption, horizontal bool) (QuickOption, error) {
			return QuickOption{Value: "redact"}, nil
		},
	}

	adapter := &secretPrompterAdapter{agent: a}

	action, err := adapter.PromptSecretAction([]security.DetectedSecret{
		{Type: "Env Var", Snippet: "secret_value", Line: 1},
	}, "shell")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action != security.SecretRedact {
		t.Errorf("expected SecretRedact, got %d", action)
	}
}

func TestSecretPrompterAdapter_DefaultRedact(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()
	a.ui = &mockSecretPrompterUI{
		interactive: true,
		quickPromptFn: func(prompt string, options []QuickOption, horizontal bool) (QuickOption, error) {
			// Return unknown value — should default to redact
			return QuickOption{Value: "unknown"}, nil
		},
	}

	adapter := &secretPrompterAdapter{agent: a}

	action, err := adapter.PromptSecretAction([]security.DetectedSecret{
		{Type: "Password", Snippet: "p@ssw0rd", Line: 3},
	}, "input")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action != security.SecretRedact {
		t.Errorf("expected SecretRedact (default), got %d", action)
	}
}

func TestSecretPrompterAdapter_PromptFailed(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()
	a.ui = nil // no UI

	adapter := &secretPrompterAdapter{agent: a}

	action, err := adapter.PromptSecretAction([]security.DetectedSecret{
		{Type: "API Key", Snippet: "sk-xxxx", Line: 1},
	}, "source")

	if err == nil {
		t.Fatal("expected error when prompt fails")
	}
	if action != security.SecretRedact {
		t.Errorf("expected SecretRedact on failure, got %d", action)
	}
}

func TestSecretPrompterAdapter_CommitSource(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()
	var receivedOptions []QuickOption
	a.ui = &mockSecretPrompterUI{
		interactive: true,
		quickPromptFn: func(prompt string, options []QuickOption, horizontal bool) (QuickOption, error) {
			receivedOptions = options
			return QuickOption{Value: "redact"}, nil
		},
	}

	adapter := &secretPrompterAdapter{agent: a}

	_, err := adapter.PromptSecretAction([]security.DetectedSecret{
		{Type: "API Key", Snippet: "sk-123", Line: 1},
	}, "commit")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// For "commit" source, the first option should be "Allow with Warning"
	if len(receivedOptions) < 1 {
		t.Fatal("expected at least one option")
	}
	if receivedOptions[0].Label != console.BoldText("Allow with Warning") {
		t.Errorf("expected first label 'Allow with Warning' (bolded), got %q", receivedOptions[0].Label)
	}
}

func TestSecretPrompterAdapter_NonCommitSource(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()
	var receivedOptions []QuickOption
	a.ui = &mockSecretPrompterUI{
		interactive: true,
		quickPromptFn: func(prompt string, options []QuickOption, horizontal bool) (QuickOption, error) {
			receivedOptions = options
			return QuickOption{Value: "redact"}, nil
		},
	}

	adapter := &secretPrompterAdapter{agent: a}

	_, err := adapter.PromptSecretAction([]security.DetectedSecret{
		{Type: "Token", Snippet: "token-abc", Line: 1},
	}, "file.go")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// For non-"commit" source, the first option should be "Redact & Continue"
	if len(receivedOptions) < 1 {
		t.Fatal("expected at least one option")
	}
	if receivedOptions[0].Label != console.BoldText("Redact & Continue") {
		t.Errorf("expected first label 'Redact & Continue' (bolded), got %q", receivedOptions[0].Label)
	}
}

func TestSecretPrompterAdapter_MultipleSecrets(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()
	a.ui = &mockSecretPrompterUI{
		interactive: true,
		quickPromptFn: func(prompt string, options []QuickOption, horizontal bool) (QuickOption, error) {
			// Verify prompt contains both secrets
			if prompt == "" {
				t.Error("prompt should not be empty")
			}
			return QuickOption{Value: "allow"}, nil
		},
	}

	adapter := &secretPrompterAdapter{agent: a}

	secrets := []security.DetectedSecret{
		{Type: "API Key", Snippet: "sk-123", Line: 10},
		{Type: "Bearer Token", Snippet: "ghp_xxx", Line: 20},
		{Type: "Password", Snippet: "pass123", Line: 0}, // Line 0 should not show line number
	}

	action, err := adapter.PromptSecretAction(secrets, "file.go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action != security.SecretAllow {
		t.Errorf("expected SecretAllow, got %d", action)
	}
}

func TestSetElevationGatePrompter(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()

	// Should not panic even with nil UI
	a.SetElevationGatePrompter()

	// The elevation gate should still exist (just with nil prompter or same gate)
	if a.security.GetElevationGate() == nil {
		t.Error("expected elevation gate to not be nil")
	}
}

func TestGetElevationGate(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()

	gate := a.GetElevationGate()
	if gate == nil {
		t.Error("expected elevation gate to not be nil after initSubManagers")
	}
}

func TestGetOutputRedactor(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()

	redactor := a.GetOutputRedactor()
	if redactor == nil {
		t.Error("expected output redactor to not be nil after initSubManagers")
	}
}
