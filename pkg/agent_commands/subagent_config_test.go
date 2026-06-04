package commands

import (
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// createTestAgentWithTempConfig creates an agent that uses an isolated temp
// directory for its config, so tests never touch the user's real config.
func createTestAgentWithTempConfig(t *testing.T) *agent.Agent {
	t.Helper()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", "")
	// Ensure an API key is present so NewManagerSilent doesn't spin up a provider
	// selection prompt.
	t.Setenv("OPENROUTER_API_KEY", "test-key-for-unit-tests")

	a, err := agent.NewAgent()
	if err != nil {
		t.Fatalf("createTestAgentWithTempConfig: NewAgent: %v", err)
	}
	return a
}

// ---------------------------------------------------------------------------
// SubagentConfigCommand (/subagent-provider)
// ---------------------------------------------------------------------------

func TestSubagentProviderCommand_SetPersistsViaUpdateConfig(t *testing.T) {
	// Regression: the old code did GetConfig() (clone) -> mutate clone -> SaveConfig().
	// SaveConfig saved the ORIGINAL (unchanged) config, so the provider was silently lost.
	chatAgent := createTestAgentWithTempConfig(t)
	cm := chatAgent.GetConfigManager()

	// Clear any existing subagent provider so we can detect the write.
	_ = cm.UpdateConfig(func(c *configuration.Config) error {
		c.SubagentProvider = ""
		return nil
	})

	cmd := &SubagentConfigCommand{configType: "provider"}
	err := cmd.Execute([]string{"openai"}, chatAgent)
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}

	// The critical assertion: a *new* call to GetConfig (clone) must reflect the change.
	afterProvider := cm.GetConfig().SubagentProvider
	if afterProvider != "openai" {
		t.Fatalf("regression: provider not persisted – expected %q, got %q", "openai", afterProvider)
	}
}

func TestSubagentProviderCommand_InvalidProvider_ReturnsError(t *testing.T) {
	chatAgent := createTestAgentWithTempConfig(t)

	cmd := &SubagentConfigCommand{configType: "provider"}
	err := cmd.Execute([]string{"nonexistent_provider_xyz"}, chatAgent)
	if err == nil {
		t.Fatal("expected error for invalid provider, got nil")
	}
	if !strings.Contains(err.Error(), "invalid provider") {
		t.Fatalf("expected 'invalid provider' in error, got: %v", err)
	}
}

func TestSubagentProviderCommand_NoArgs_ShowsStatus(t *testing.T) {
	chatAgent := createTestAgentWithTempConfig(t)

	cmd := &SubagentConfigCommand{configType: "provider"}
	// No arguments → show status, should not error.
	err := cmd.Execute(nil, chatAgent)
	if err != nil {
		t.Fatalf("Execute with no args returned unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// SubagentConfigCommand (/subagent-model)
// ---------------------------------------------------------------------------

func TestSubagentModelCommand_SetPersistsViaUpdateConfig(t *testing.T) {
	chatAgent := createTestAgentWithTempConfig(t)
	cm := chatAgent.GetConfigManager()

	// Clear any existing subagent model so we can verify the write.
	_ = cm.UpdateConfig(func(c *configuration.Config) error {
		c.SubagentModel = ""
		return nil
	})

	cmd := &SubagentConfigCommand{configType: "model"}
	err := cmd.Execute([]string{"my-test-model"}, chatAgent)
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}

	afterModel := cm.GetConfig().SubagentModel
	if afterModel != "my-test-model" {
		t.Fatalf("regression: model not persisted – expected %q, got %q", "my-test-model", afterModel)
	}
}

func TestSubagentModelCommand_NoArgs_ShowsStatus(t *testing.T) {
	chatAgent := createTestAgentWithTempConfig(t)

	cmd := &SubagentConfigCommand{configType: "model"}
	err := cmd.Execute(nil, chatAgent)
	if err != nil {
		t.Fatalf("Execute with no args returned unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// SelfReviewGateCommand (/self-review-gate)
// ---------------------------------------------------------------------------

func TestSelfReviewGateCommand_SetCodePersistsViaUpdateConfig(t *testing.T) {
	chatAgent := createTestAgentWithTempConfig(t)
	cm := chatAgent.GetConfigManager()

	cmd := &SelfReviewGateCommand{}
	err := cmd.Execute([]string{"code"}, chatAgent)
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}

	mode := cm.GetConfig().GetSelfReviewGateMode()
	if mode != configuration.SelfReviewGateModeCode {
		t.Fatalf("regression: gate mode not persisted – expected %q, got %q",
			configuration.SelfReviewGateModeCode, mode)
	}
}

func TestSelfReviewGateCommand_SetAlwaysPersistsViaUpdateConfig(t *testing.T) {
	chatAgent := createTestAgentWithTempConfig(t)
	cm := chatAgent.GetConfigManager()

	cmd := &SelfReviewGateCommand{}
	err := cmd.Execute([]string{"always"}, chatAgent)
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}

	mode := cm.GetConfig().GetSelfReviewGateMode()
	if mode != configuration.SelfReviewGateModeAlways {
		t.Fatalf("regression: gate mode not persisted – expected %q, got %q",
			configuration.SelfReviewGateModeAlways, mode)
	}
}

func TestSelfReviewGateCommand_SetOffPersistsViaUpdateConfig(t *testing.T) {
	chatAgent := createTestAgentWithTempConfig(t)
	cm := chatAgent.GetConfigManager()

	// First set to "code" so we have a non-off value to change.
	_ = cm.UpdateConfig(func(c *configuration.Config) error {
		c.SelfReviewGateMode = configuration.SelfReviewGateModeCode
		return nil
	})

	cmd := &SelfReviewGateCommand{}
	err := cmd.Execute([]string{"off"}, chatAgent)
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}

	mode := cm.GetConfig().GetSelfReviewGateMode()
	if mode != configuration.SelfReviewGateModeOff {
		t.Fatalf("regression: gate mode not persisted – expected %q, got %q",
			configuration.SelfReviewGateModeOff, mode)
	}
}

func TestSelfReviewGateCommand_InvalidMode_ReturnsError(t *testing.T) {
	chatAgent := createTestAgentWithTempConfig(t)

	cmd := &SelfReviewGateCommand{}
	err := cmd.Execute([]string{"invalid_mode"}, chatAgent)
	if err == nil {
		t.Fatal("expected error for invalid mode, got nil")
	}
	if !strings.Contains(err.Error(), "invalid") && !strings.Contains(err.Error(), "allowed") {
		t.Fatalf("expected validation error, got: %v", err)
	}
}

func TestSelfReviewGateCommand_TooManyArgs_ReturnsError(t *testing.T) {
	chatAgent := createTestAgentWithTempConfig(t)

	cmd := &SelfReviewGateCommand{}
	err := cmd.Execute([]string{"code", "extra"}, chatAgent)
	if err == nil {
		t.Fatal("expected error for too many args, got nil")
	}
}

func TestSelfReviewGateCommand_NilAgent_ReturnsError(t *testing.T) {
	cmd := &SelfReviewGateCommand{}
	err := cmd.Execute(nil, nil)
	if err == nil {
		t.Fatal("expected error for nil agent, got nil")
	}
}

func TestSelfReviewGateCommand_NoArgs_ShowsStatus(t *testing.T) {
	chatAgent := createTestAgentWithTempConfig(t)

	cmd := &SelfReviewGateCommand{}
	err := cmd.Execute(nil, chatAgent)
	if err != nil {
		t.Fatalf("Execute with no args returned unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// /persona <name> enable|disable — now toggles Config.DisabledPersonas
// ---------------------------------------------------------------------------

func TestSetPersonaEnabled_DisableThenReEnableViaDisabledList(t *testing.T) {
	chatAgent := createTestAgentWithTempConfig(t)
	cm := chatAgent.GetConfigManager()

	config := cm.GetConfig()
	var personaID string
	for id := range config.SubagentTypes {
		personaID = id
		break
	}
	if personaID == "" {
		t.Skip("no persona found to test with")
	}

	if cm.GetConfig().IsPersonaDisabled(personaID) {
		t.Fatal("precondition failed: persona should not be disabled before test")
	}

	if err := (&PersonaCommand{}).Execute([]string{personaID, "disable"}, chatAgent); err != nil {
		t.Fatalf("disable returned error: %v", err)
	}
	if !cm.GetConfig().IsPersonaDisabled(personaID) {
		t.Fatalf("regression: persona %q not flagged as disabled", personaID)
	}

	if err := (&PersonaCommand{}).Execute([]string{personaID, "enable"}, chatAgent); err != nil {
		t.Fatalf("enable returned error: %v", err)
	}
	if cm.GetConfig().IsPersonaDisabled(personaID) {
		t.Fatalf("regression: persona %q still disabled after enable command", personaID)
	}
}

func TestSetPersonaEnabled_NonexistentPersona_ReturnsError(t *testing.T) {
	chatAgent := createTestAgentWithTempConfig(t)
	_ = chatAgent.GetConfigManager()

	err := (&PersonaCommand{}).Execute([]string{"nonexistent_persona", "enable"}, chatAgent)
	if err == nil {
		t.Fatal("expected error for nonexistent persona, got nil")
	}
	if !strings.Contains(err.Error(), "persona not found") {
		t.Fatalf("expected 'persona not found' in error, got: %v", err)
	}
}

func TestSubagentPersonaCommand_Execute_UnknownAction_ReturnsError(t *testing.T) {
	chatAgent := createTestAgentWithTempConfig(t)

	cmd := &SubagentPersonaCommand{}
	err := cmd.Execute([]string{"coder", "explode"}, chatAgent)
	if err == nil {
		t.Fatal("expected error for unknown action, got nil")
	}
	if !strings.Contains(err.Error(), "unknown action") {
		t.Fatalf("expected 'unknown action' in error, got: %v", err)
	}
}

func TestSubagentPersonaCommand_Execute_NoArgs_ListsPersonas(t *testing.T) {
	chatAgent := createTestAgentWithTempConfig(t)

	cmd := &SubagentPersonaCommand{}
	// No args should list personas without error.
	if err := cmd.Execute(nil, chatAgent); err != nil {
		t.Fatalf("Execute with no args returned unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// SubagentPersonasCommand (/subagent-personas list all)
// ---------------------------------------------------------------------------

func TestSubagentPersonasCommand_Execute_NoArgs_ListsAll(t *testing.T) {
	chatAgent := createTestAgentWithTempConfig(t)

	cmd := &SubagentPersonasCommand{}
	if err := cmd.Execute(nil, chatAgent); err != nil {
		t.Fatalf("Execute with no args returned unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Round-trip: set then reload from disk to prove persistence
// ---------------------------------------------------------------------------

func TestSubagentProvider_SetThenReloadFromDisk(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("OPENROUTER_API_KEY", "test-key-for-unit-tests")

	chatAgent, err := agent.NewAgent()
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}

	// Set via the command.
	cmd := &SubagentConfigCommand{configType: "provider"}
	if err := cmd.Execute([]string{"openai"}, chatAgent); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// Create a *second* manager pointing at the same config dir and verify
	// the value was actually written to disk (not just held in memory).
	mgr2, err := configuration.NewManagerSilent()
	if err != nil {
		t.Fatalf("NewManagerSilent: %v", err)
	}
	p := mgr2.GetConfig().SubagentProvider
	if p != "openai" {
		t.Fatalf("regression: provider not persisted to disk – expected %q, got %q", "openai", p)
	}
}

func TestSelfReviewGate_SetThenReloadFromDisk(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("OPENROUTER_API_KEY", "test-key-for-unit-tests")

	chatAgent, err := agent.NewAgent()
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}

	cmd := &SelfReviewGateCommand{}
	if err := cmd.Execute([]string{"always"}, chatAgent); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	mgr2, err := configuration.NewManagerSilent()
	if err != nil {
		t.Fatalf("NewManagerSilent: %v", err)
	}
	mode := mgr2.GetConfig().GetSelfReviewGateMode()
	if mode != configuration.SelfReviewGateModeAlways {
		t.Fatalf("regression: gate mode not persisted to disk – expected %q, got %q",
			configuration.SelfReviewGateModeAlways, mode)
	}
}

func TestSubagentPersona_DisablePersistedToDisk(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("OPENROUTER_API_KEY", "test-key-for-unit-tests")

	chatAgent, err := agent.NewAgent()
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	cm := chatAgent.GetConfigManager()

	var personaID string
	for id := range cm.GetConfig().SubagentTypes {
		personaID = id
		break
	}
	if personaID == "" {
		t.Skip("no persona found")
	}

	cmd := &SubagentPersonaCommand{}
	if err := cmd.Execute([]string{personaID, "disable"}, chatAgent); err != nil {
		t.Fatalf("disable: %v", err)
	}

	mgr2, err := configuration.NewManagerSilent()
	if err != nil {
		t.Fatalf("NewManagerSilent: %v", err)
	}
	if !mgr2.GetConfig().IsPersonaDisabled(personaID) {
		t.Fatalf("regression: persona %q not disabled after reload from disk", personaID)
	}
}
