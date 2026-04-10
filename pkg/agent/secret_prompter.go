package agent

import (
	"fmt"
	"strings"

	"github.com/alantheprice/ledit/pkg/security"
)

// secretPrompterAdapter implements security.SecretPrompter using the Agent's interactive UI.
type secretPrompterAdapter struct {
	agent *Agent
}

func (a *secretPrompterAdapter) PromptSecretAction(secrets []security.DetectedSecret, source string) (security.SecretAction, error) {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Secrets detected in %s:\n", source)
	for i, s := range secrets {
		fmt.Fprintf(&sb, "  %d. [%s] %s", i+1, s.Type, s.Snippet)
		if s.Line > 0 {
			fmt.Fprintf(&sb, " (line %d)", s.Line)
		}
		sb.WriteString("\n")
	}

	prompt := sb.String()

	redactLabel := "Redact & Continue"
	if source == "commit" {
		redactLabel = "Allow with Warning"
	}
	choices := []ChoiceOption{
		{Label: redactLabel, Value: "redact"},
		{Label: "Allow as-is", Value: "allow"},
		{Label: "Block", Value: "block"},
	}

	choice, err := a.agent.PromptChoice(prompt, choices)
	if err != nil {
		return security.SecretRedact, fmt.Errorf("prompt failed: %w", err)
	}

	switch choice {
	case "allow":
		return security.SecretAllow, nil
	case "block":
		return security.SecretBlock, nil
	default:
		return security.SecretRedact, nil
	}
}

// isSecretSensitiveTool returns true for tools that can return secrets in their output.
func isSecretSensitiveTool(toolName string) bool {
	switch toolName {
	case "shell_command", "read_file", "search_files",
		"write_file", "edit_file", "write_structured_file", "patch_structured_file":
		return true
	}
	return false
}

// SetElevationGatePrompter wires the agent's interactive UI into the elevation gate.
// Call this after agent.ui is initialized (done automatically by SetUI).
func (a *Agent) SetElevationGatePrompter() {
	if a.elevationGate != nil {
		a.elevationGate = security.NewElevationGate(&secretPrompterAdapter{agent: a})
	}
}

// GetElevationGate returns the agent's elevation gate for external use (e.g., commit flows).
func (a *Agent) GetElevationGate() *security.ElevationGate {
	return a.elevationGate
}

// GetOutputRedactor returns the agent's output redactor for external use.
func (a *Agent) GetOutputRedactor() *security.OutputRedactor {
	return a.outputRedactor
}
