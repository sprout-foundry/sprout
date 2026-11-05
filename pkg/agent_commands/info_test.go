package commands

import (
	"bytes"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// SP-161e: /info is the one-stop overview, so the active shell-command
// risk profile belongs in it alongside model, persona and subagent config.
func TestInfoCommand_ShowsRiskProfile(t *testing.T) {
	chatAgent := createTestAgentWithTempConfig(t)
	chatAgent.SetRiskProfileOverride(configuration.RiskProfilePermissive)

	var buf bytes.Buffer
	cmd := &InfoCommand{}
	cmd.SetOutput(&buf)

	if err := cmd.Execute(nil, chatAgent); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Risk profile:") {
		t.Errorf("output has no risk-profile line:\n%s", out)
	}
	if !strings.Contains(out, string(configuration.RiskProfilePermissive)) {
		t.Errorf("output does not name the active profile %q:\n%s", configuration.RiskProfilePermissive, out)
	}
}

// The JSON form carries the same field, so scripted consumers see it too.
func TestInfoCommand_JSONIncludesRiskProfile(t *testing.T) {
	chatAgent := createTestAgentWithTempConfig(t)
	chatAgent.SetRiskProfileOverride(configuration.RiskProfileReadonly)

	var buf bytes.Buffer
	cmd := &InfoCommand{}
	cmd.SetOutput(&buf)

	if err := cmd.ExecuteWithJSONOutput(nil, chatAgent, &CommandContext{}); err != nil {
		t.Fatalf("ExecuteWithJSONOutput: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, `"risk_profile"`) {
		t.Errorf("JSON payload has no risk_profile field:\n%s", out)
	}
	if !strings.Contains(out, string(configuration.RiskProfileReadonly)) {
		t.Errorf("JSON payload does not name the active profile %q:\n%s", configuration.RiskProfileReadonly, out)
	}
}
