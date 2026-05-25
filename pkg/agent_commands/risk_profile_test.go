package commands

import (
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

func TestRiskProfileCommand_NameAndDescription(t *testing.T) {
	cmd := &RiskProfileCommand{}
	if got := cmd.Name(); got != "risk-profile" {
		t.Errorf("Name() = %q, want %q", got, "risk-profile")
	}
	if got := cmd.Description(); !strings.Contains(got, "risk profile") {
		t.Errorf("Description() = %q, want it to mention 'risk profile'", got)
	}
	if got := cmd.Usage(); !strings.Contains(got, "/risk-profile") {
		t.Errorf("Usage() missing command invocation example: %q", got)
	}
}

func TestRiskProfileCommand_ExecuteNilAgent(t *testing.T) {
	cmd := &RiskProfileCommand{}
	if err := cmd.Execute(nil, nil); err == nil {
		t.Error("expected error for nil agent")
	}
}

func TestRiskProfileCommand_ExecuteSetAndClear(t *testing.T) {
	chatAgent, err := agent.NewAgentWithModel("")
	if err != nil {
		t.Fatalf("NewAgentWithModel failed: %v", err)
	}
	cmd := &RiskProfileCommand{}

	// Set override
	if err := cmd.Execute([]string{"readonly"}, chatAgent); err != nil {
		t.Fatalf("set readonly failed: %v", err)
	}
	if got := chatAgent.GetActiveRiskProfile(); got != configuration.RiskProfileReadonly {
		t.Errorf("after set: active = %q, want %q", got, configuration.RiskProfileReadonly)
	}

	// Clear override — should fall back to config / default
	if err := cmd.Execute([]string{"clear"}, chatAgent); err != nil {
		t.Fatalf("clear failed: %v", err)
	}
	if got := chatAgent.GetActiveRiskProfile(); got == configuration.RiskProfileReadonly {
		t.Errorf("after clear: active should not still be readonly, got %q", got)
	}
}

func TestRiskProfileCommand_ExecuteUnknownProfile(t *testing.T) {
	chatAgent, err := agent.NewAgentWithModel("")
	if err != nil {
		t.Fatalf("NewAgentWithModel failed: %v", err)
	}
	cmd := &RiskProfileCommand{}

	err = cmd.Execute([]string{"bogus-not-a-real-profile"}, chatAgent)
	if err == nil {
		t.Fatal("expected error for unknown profile")
	}
	if !strings.Contains(err.Error(), "unknown risk profile") {
		t.Errorf("error message should mention 'unknown risk profile', got %q", err.Error())
	}
}

func TestRiskProfileCommand_ExecuteShowDoesNotError(t *testing.T) {
	chatAgent, err := agent.NewAgentWithModel("")
	if err != nil {
		t.Fatalf("NewAgentWithModel failed: %v", err)
	}
	cmd := &RiskProfileCommand{}

	// No args → show
	if err := cmd.Execute(nil, chatAgent); err != nil {
		t.Errorf("show (no args) failed: %v", err)
	}
	// Explicit "list"
	if err := cmd.Execute([]string{"list"}, chatAgent); err != nil {
		t.Errorf("show (list) failed: %v", err)
	}
}

func TestBuiltinProfileNamesAreValid(t *testing.T) {
	for _, name := range builtinProfileNames() {
		if !configuration.IsValidRiskProfile(name) {
			t.Errorf("builtinProfileNames includes %q but IsValidRiskProfile rejects it — registry drift", name)
		}
	}
}
