package agent

import (
	"fmt"
	"strings"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/secretdetect"
	"github.com/sprout-foundry/sprout/pkg/security"
)

// secretPrompterAdapter implements security.SecretPrompter using the Agent's interactive UI.
type secretPrompterAdapter struct {
	agent *Agent
}

func (a *secretPrompterAdapter) PromptSecretAction(secrets []security.DetectedSecret, source string) (security.SecretAction, error) {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Secrets detected in %s:\n", source)

	for i, s := range secrets {
		fmt.Fprintf(&sb, "  %d. [%s] %s", i+1, secretdetect.DisplayName(s.Type), s.Snippet)
		if s.Line > 0 {
			fmt.Fprintf(&sb, " (line %d)", s.Line)
		}
		// Hint when the match is from gitleaks' generic catch-all rule, which
		// has the highest false-positive rate of any rule.
		if s.Type == "generic-api-key" {
			sb.WriteString("  (heuristic match — likely false-positive on placeholder/example content)")
		}
		sb.WriteString("\n")
	}

	prompt := sb.String()

	redactLabel := "Redact & Continue"
	if source == "commit" {
		redactLabel = "Allow with Warning"
	}

	// Bold the safe default when running in an interactive terminal.
	var safeLabel string
	if a.agent.ui != nil && a.agent.ui.IsInteractive() {
		safeLabel = console.BoldText(redactLabel)
	} else {
		safeLabel = redactLabel
	}

	choices := []ChoiceOption{
		{Label: safeLabel, Value: "redact"},
		{Label: "Allow this batch only", Value: "allow"},
		{Label: allowSourceLabel(source), Value: "allow_source"},
		{Label: "Block", Value: "block"},
	}

	choice, err := a.agent.PromptChoice(prompt, choices)
	if err != nil {
		return security.SecretRedact, agenterrors.Wrap(err, "prompt failed")
	}

	switch choice {
	case "allow":
		return security.SecretAllow, nil
	case "allow_source":
		return security.SecretAllowSource, nil
	case "block":
		return security.SecretBlock, nil
	default:
		return security.SecretRedact, nil
	}
}

// allowSourceLabel returns a context-sensitive label for the "whitelist this
// source for the session" choice, based on the tool name embedded in source.
func allowSourceLabel(source string) string {
	switch {
	case strings.HasPrefix(source, "read_file:"):
		return "Allow all secrets in this file (rest of session)"
	case strings.HasPrefix(source, "shell_command:"):
		return "Allow all secrets from this command (rest of session)"
	case strings.HasPrefix(source, "search_files:"):
		return "Allow all secrets from this search (rest of session)"
	case source == "commit":
		return "Allow all secrets in this commit (rest of session)"
	default:
		return "Allow all secrets from this source (rest of session)"
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
	if a.security.GetElevationGate() != nil {
		a.security.SetElevationGate(security.NewElevationGate(&secretPrompterAdapter{agent: a}))
	}
}

// GetElevationGate returns the agent's elevation gate for external use (e.g., commit flows).
func (a *Agent) GetElevationGate() *security.ElevationGate {
	return a.security.GetElevationGate()
}

// GetOutputRedactor returns the agent's output redactor for external use.
func (a *Agent) GetOutputRedactor() *security.OutputRedactor {
	return a.security.GetOutputRedactor()
}
