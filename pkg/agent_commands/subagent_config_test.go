package commands

import (
	"strings"
	"testing"

	"github.com/alantheprice/ledit/pkg/agent"
	"github.com/alantheprice/ledit/pkg/configuration"
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
// SubagentPersonaCommand (/subagent-persona <name> enable|disable)
// ---------------------------------------------------------------------------

func TestSetPersonaEnabled_EnablePersistsViaUpdateConfig(t *testing.T) {
	chatAgent := createTestAgentWithTempConfig(t)
	cm := chatAgent.GetConfigManager()

	// Find an existing persona ID (e.g. "tester").
	config := cm.GetConfig()
	var personaID string
	for id, p := range config.SubagentTypes {
		if p.Enabled {
			// Disable it first, so we can verify enable re-persists.
			personaID = id
			_ = cm.UpdateConfig(func(c *configuration.Config) error {
				c.SubagentTypes[id] = configuration.SubagentType{
					ID:      id,
					Name:    p.Name,
					Enabled: false,
				}
				return nil
			})
			break
		}
	}
	if personaID == "" {
		t.Skip("no enabled persona found to test with")
	}

	// Verify it is disabled before we re-enable.
	if cm.GetConfig().SubagentTypes[personaID].Enabled {
		t.Fatal("precondition failed: persona should be disabled before test")
	}

	err := setPersonaEnabled(personaID, true, cm)
	if err != nil {
		t.Fatalf("setPersonaEnabled returned error: %v", err)
	}

	// The vital regression check: GetConfig() must show Enabled == true.
	if !cm.GetConfig().SubagentTypes[personaID].Enabled {
		t.Fatalf("regression: persona enable not persisted for %q", personaID)
	}
}

func TestSetPersonaEnabled_DisablePersistsViaUpdateConfig(t *testing.T) {
	chatAgent := createTestAgentWithTempConfig(t)
	cm := chatAgent.GetConfigManager()

	config := cm.GetConfig()
	var personaID string
	for id, p := range config.SubagentTypes {
		if p.Enabled {
			personaID = id
			break
		}
	}
	if personaID == "" {
		t.Skip("no enabled persona found to test with")
	}

	err := setPersonaEnabled(personaID, false, cm)
	if err != nil {
		t.Fatalf("setPersonaEnabled returned error: %v", err)
	}

	if cm.GetConfig().SubagentTypes[personaID].Enabled {
		t.Fatalf("regression: persona disable not persisted for %q", personaID)
	}
}

func TestSetPersonaEnabled_NonexistentPersona_ReturnsError(t *testing.T) {
	chatAgent := createTestAgentWithTempConfig(t)
	cm := chatAgent.GetConfigManager()

	err := setPersonaEnabled("nonexistent_persona", true, cm)
	if err == nil {
		t.Fatal("expected error for nonexistent persona, got nil")
	}
	if !strings.Contains(err.Error(), "persona not found") {
		t.Fatalf("expected 'persona not found' in error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// SubagentPersonaCommand (/subagent-persona <name> provider <p>)
// ---------------------------------------------------------------------------

func TestSetPersonaProvider_PersistsViaUpdateConfig(t *testing.T) {
	chatAgent := createTestAgentWithTempConfig(t)
	cm := chatAgent.GetConfigManager()

	config := cm.GetConfig()
	var personaID string
	for id := range config.SubagentTypes {
		personaID = id
		break
	}
	if personaID == "" {
		t.Fatal("no personas found in default config")
	}

	// Clear provider so we can detect the write.
	_ = cm.UpdateConfig(func(c *configuration.Config) error {
		p := c.SubagentTypes[personaID]
		p.Provider = ""
		c.SubagentTypes[personaID] = p
		return nil
	})

	err := setPersonaProvider(personaID, "openai", cm)
	if err != nil {
		t.Fatalf("setPersonaProvider returned error: %v", err)
	}

	if cm.GetConfig().SubagentTypes[personaID].Provider != "openai" {
		t.Fatalf("regression: persona provider not persisted for %q – got %q",
			personaID, cm.GetConfig().SubagentTypes[personaID].Provider)
	}
}

func TestSetPersonaProvider_InvalidProvider_ReturnsError(t *testing.T) {
	chatAgent := createTestAgentWithTempConfig(t)
	cm := chatAgent.GetConfigManager()

	config := cm.GetConfig()
	var personaID string
	for id := range config.SubagentTypes {
		personaID = id
		break
	}

	err := setPersonaProvider(personaID, "no_such_provider_xyz", cm)
	if err == nil {
		t.Fatal("expected error for invalid provider, got nil")
	}
}

// ---------------------------------------------------------------------------
// SubagentPersonaCommand (/subagent-persona <name> model <m>)
// ---------------------------------------------------------------------------

func TestSetPersonaModel_PersistsViaUpdateConfig(t *testing.T) {
	chatAgent := createTestAgentWithTempConfig(t)
	cm := chatAgent.GetConfigManager()

	config := cm.GetConfig()
	var personaID string
	for id := range config.SubagentTypes {
		personaID = id
		break
	}
	if personaID == "" {
		t.Fatal("no personas found in default config")
	}

	// Clear model first.
	_ = cm.UpdateConfig(func(c *configuration.Config) error {
		p := c.SubagentTypes[personaID]
		p.Model = ""
		c.SubagentTypes[personaID] = p
		return nil
	})

	err := setPersonaModel(personaID, "my-persona-test-model", cm)
	if err != nil {
		t.Fatalf("setPersonaModel returned error: %v", err)
	}

	if cm.GetConfig().SubagentTypes[personaID].Model != "my-persona-test-model" {
		t.Fatalf("regression: persona model not persisted for %q – got %q",
			personaID, cm.GetConfig().SubagentTypes[personaID].Model)
	}
}

// ---------------------------------------------------------------------------
// End-to-end: SubagentPersonaCommand.Execute routes to the right helpers
// ---------------------------------------------------------------------------

func TestSubagentPersonaCommand_Execute_EnableThenDisable(t *testing.T) {
	chatAgent := createTestAgentWithTempConfig(t)
	cm := chatAgent.GetConfigManager()

	config := cm.GetConfig()
	var personaName string
	for _, p := range config.SubagentTypes {
		if p.Enabled {
			personaName = p.Name
			break
		}
	}
	if personaName == "" {
		t.Skip("no enabled persona found")
	}

	// Disable via Execute (the high-level command path).
	cmd := &SubagentPersonaCommand{}
	if err := cmd.Execute([]string{personaName, "disable"}, chatAgent); err != nil {
		t.Fatalf("disable failed: %v", err)
	}
	for _, p := range cm.GetConfig().SubagentTypes {
		if p.Name == personaName && p.Enabled {
			t.Fatalf("regression: persona %q still enabled after disable command", personaName)
		}
	}

	// Re-enable via Execute.
	if err := cmd.Execute([]string{personaName, "enable"}, chatAgent); err != nil {
		t.Fatalf("enable failed: %v", err)
	}
	for _, p := range cm.GetConfig().SubagentTypes {
		if p.Name == personaName && !p.Enabled {
			t.Fatalf("regression: persona %q still disabled after enable command", personaName)
		}
	}
}

func TestSubagentPersonaCommand_Execute_Model(t *testing.T) {
	chatAgent := createTestAgentWithTempConfig(t)
	cm := chatAgent.GetConfigManager()

	config := cm.GetConfig()
	var personaName string
	for _, p := range config.SubagentTypes {
		personaName = p.Name
		break
	}

	cmd := &SubagentPersonaCommand{}
	if err := cmd.Execute([]string{personaName, "model", "custom-persona-model-v2"}, chatAgent); err != nil {
		t.Fatalf("set model failed: %v", err)
	}

	for _, p := range cm.GetConfig().SubagentTypes {
		if p.Name == personaName {
			if p.Model != "custom-persona-model-v2" {
				t.Fatalf("regression: model not persisted – expected %q, got %q",
					"custom-persona-model-v2", p.Model)
			}
			return
		}
	}
	t.Fatal("persona not found in config after setting model")
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
	// No args should list personas (showAllPersonas) without error.
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

func TestSubagentPersona_EnableThenReloadFromDisk(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("OPENROUTER_API_KEY", "test-key-for-unit-tests")

	chatAgent, err := agent.NewAgent()
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	cm := chatAgent.GetConfigManager()

	// Find any enabled persona, disable it, re-enable, then reload from disk.
	config := cm.GetConfig()
	var personaName string
	for _, p := range config.SubagentTypes {
		if p.Enabled {
			personaName = p.Name
			break
		}
	}
	if personaName == "" {
		t.Skip("no enabled persona found")
	}

	cmd := &SubagentPersonaCommand{}
	if err := cmd.Execute([]string{personaName, "disable"}, chatAgent); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if err := cmd.Execute([]string{personaName, "enable"}, chatAgent); err != nil {
		t.Fatalf("enable: %v", err)
	}

	mgr2, err := configuration.NewManagerSilent()
	if err != nil {
		t.Fatalf("NewManagerSilent: %v", err)
	}
	for _, p := range mgr2.GetConfig().SubagentTypes {
		if p.Name == personaName {
			if !p.Enabled {
				t.Fatalf("regression: persona %q not enabled after reload from disk", personaName)
			}
			return
		}
	}
	t.Fatalf("persona %q not found after reload from disk", personaName)
}
