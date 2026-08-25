package commands

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createTestAgentWithTempConfig creates an agent that uses an isolated temp
// directory for its config, so tests never touch the user's real config.
func createTestAgentWithTempConfig(t *testing.T) *agent.Agent {
	t.Helper()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("SPROUT_CONFIG", "") // Ensure real config dir is not used
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
// /persona <name> enable|disable — now toggles Config.DisabledPersonas
// ---------------------------------------------------------------------------

func TestSetPersonaEnabled_DisableThenReEnableViaDisabledList(t *testing.T) {
	chatAgent := createTestAgentWithTempConfig(t)
	cm := chatAgent.GetConfigManager()

	config := cm.GetConfig()
	var personaID string
	for id := range config.SubagentTypes {
		if !cm.GetConfig().IsPersonaDisabled(id) {
			personaID = id
			break
		}
	}
	if personaID == "" {
		t.Skip("no enabled persona found to test with")
	}

	// Defensive: should always be true given the selection above, but kept
	// as a clear precondition marker.
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

	// Pick an enabled persona (skip any that are disabled by default) so
	// the test is deterministic regardless of Go's map iteration order.
	var personaID string
	for id := range cm.GetConfig().SubagentTypes {
		if !cm.GetConfig().IsPersonaDisabled(id) {
			personaID = id
			break
		}
	}
	if personaID == "" {
		t.Skip("no enabled persona found")
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

// ---------------------------------------------------------------------------
// SubagentConfigCommand.Complete (/subagent-provider)
// ---------------------------------------------------------------------------

func TestSubagentConfigCommand_Complete_Provider_NilAgent(t *testing.T) {
	cmd := &SubagentConfigCommand{configType: "provider"}

	assert.Nil(t, cmd.Complete(nil, nil), "nil agent with no args should return nil")
	assert.Nil(t, cmd.Complete([]string{"ollama"}, nil), "nil agent with prefix should return nil")
}

func TestSubagentConfigCommand_Complete_Provider_NilConfigManager(t *testing.T) {
	cmd := &SubagentConfigCommand{configType: "provider"}

	// NewTestAgent has no config manager — Complete must bail out gracefully.
	assert.Nil(t, cmd.Complete(nil, agent.NewTestAgent()))
}

func TestSubagentConfigCommand_Complete_Provider_NoArgs_ReturnsAllProvidersSorted(t *testing.T) {
	chatAgent := createTestAgentWithTempConfig(t)
	cmd := &SubagentConfigCommand{configType: "provider"}

	got := cmd.Complete(nil, chatAgent)
	require.NotEmpty(t, got, "should return available providers")
	assert.True(t, sort.StringsAreSorted(got), "results should be sorted alphabetically")

	// Spot-check deterministic providers (embedded factory + special providers).
	for _, want := range []string{"editor", "ollama-local", "openai", "openrouter"} {
		assert.Contains(t, got, want, "provider list should contain %q", want)
	}

	// No duplicates.
	seen := make(map[string]bool, len(got))
	for _, p := range got {
		if seen[p] {
			t.Errorf("duplicate provider %q", p)
		}
		seen[p] = true
	}
}

func TestSubagentConfigCommand_Complete_Provider_PrefixFilter(t *testing.T) {
	chatAgent := createTestAgentWithTempConfig(t)
	cmd := &SubagentConfigCommand{configType: "provider"}

	got := cmd.Complete([]string{"ollama"}, chatAgent)
	require.NotEmpty(t, got, "should match ollama providers")
	assert.True(t, sort.StringsAreSorted(got), "results should be sorted")
	for _, p := range got {
		assert.True(t, strings.HasPrefix(p, "ollama"), "result %q should start with 'ollama'", p)
	}
	// Both special ollama providers are always available.
	assert.Contains(t, got, "ollama-local")
	assert.Contains(t, got, "ollama-cloud")

	// Filtering must reduce the list compared to the unfiltered set.
	all := cmd.Complete(nil, chatAgent)
	require.NotEmpty(t, all)
	assert.Less(t, len(got), len(all), "prefix filtering should narrow the provider list")
}

func TestSubagentConfigCommand_Complete_Provider_PrefixCaseInsensitive(t *testing.T) {
	chatAgent := createTestAgentWithTempConfig(t)
	cmd := &SubagentConfigCommand{configType: "provider"}

	lower := cmd.Complete([]string{"ollama"}, chatAgent)
	upper := cmd.Complete([]string{"OLLAMA"}, chatAgent)
	mixed := cmd.Complete([]string{"Ollama"}, chatAgent)
	require.NotEmpty(t, lower)
	assert.Equal(t, lower, upper, "uppercase prefix should match the same providers")
	assert.Equal(t, lower, mixed, "mixed-case prefix should match the same providers")
}

func TestSubagentConfigCommand_Complete_Provider_UsesLastArgAsPrefix(t *testing.T) {
	chatAgent := createTestAgentWithTempConfig(t)
	cmd := &SubagentConfigCommand{configType: "provider"}

	// The prefix comes from the last argument, not the first.
	got := cmd.Complete([]string{"openai", "ollama"}, chatAgent)
	require.NotEmpty(t, got)
	for _, p := range got {
		assert.True(t, strings.HasPrefix(p, "ollama"), "result %q should start with 'ollama'", p)
	}
	assert.Contains(t, got, "ollama-local")
	assert.NotContains(t, got, "openai")
}

func TestSubagentConfigCommand_Complete_Provider_NoMatch(t *testing.T) {
	chatAgent := createTestAgentWithTempConfig(t)
	cmd := &SubagentConfigCommand{configType: "provider"}

	assert.Empty(t, cmd.Complete([]string{"zzzz-no-such-provider"}, chatAgent))
}

// ---------------------------------------------------------------------------
// SubagentConfigCommand.Complete (/subagent-model)
// ---------------------------------------------------------------------------

// seedModelCompleteCache pre-populates the package-level stale-while-revalidate
// model cache (models.go) so cachedModelsForProvider returns immediately from
// cache instead of fetching over the network. The entry is removed on cleanup
// so tests never leak state into each other.
func seedModelCompleteCache(t *testing.T, clientType api.ClientType, models []api.ModelInfo) {
	t.Helper()
	key := string(clientType)

	modelCompleteMu.Lock()
	modelCompleteCache[key] = modelCompleteEntry{
		models:    models,
		fetchedAt: time.Now(),
	}
	modelCompleteMu.Unlock()

	t.Cleanup(func() {
		modelCompleteMu.Lock()
		delete(modelCompleteCache, key)
		delete(modelCompleteRefresh, key)
		modelCompleteMu.Unlock()
	})
}

func TestSubagentConfigCommand_Complete_Model_NilAgent(t *testing.T) {
	cmd := &SubagentConfigCommand{configType: "model"}

	assert.Nil(t, cmd.Complete(nil, nil), "nil agent with no args should return nil")
	assert.Nil(t, cmd.Complete([]string{"gpt"}, nil), "nil agent with prefix should return nil")
}

func TestSubagentConfigCommand_Complete_Model_NilConfigManager(t *testing.T) {
	cmd := &SubagentConfigCommand{configType: "model"}

	// NewTestAgent has no config manager — Complete must bail out gracefully.
	assert.Nil(t, cmd.Complete(nil, agent.NewTestAgent()))
}

func TestSubagentConfigCommand_Complete_Model_UsesConfiguredSubagentProvider(t *testing.T) {
	chatAgent := createTestAgentWithTempConfig(t)
	cm := chatAgent.GetConfigManager()
	require.NoError(t, cm.UpdateConfig(func(c *configuration.Config) error {
		c.SetSubagentProvider("openai")
		return nil
	}))

	// Seed distinct model sets: the subagent provider's (openai) and the
	// parent agent's fallback provider (TestClientType under `go test`).
	// Complete must prefer the configured subagent provider.
	seedModelCompleteCache(t, api.OpenAIClientType, []api.ModelInfo{
		{ID: "openai-model-b"},
		{ID: "openai-model-a"},
	})
	seedModelCompleteCache(t, chatAgent.GetProviderType(), []api.ModelInfo{
		{ID: "parent-fallback-model"},
	})

	cmd := &SubagentConfigCommand{configType: "model"}
	got := cmd.Complete(nil, chatAgent)
	require.NotNil(t, got, "should return models for the configured subagent provider")
	assert.Equal(t, []string{"openai-model-a", "openai-model-b"}, got)
	assert.NotContains(t, got, "parent-fallback-model", "must not fall back to the parent provider")
}

func TestSubagentConfigCommand_Complete_Model_FallsBackToParentProvider(t *testing.T) {
	chatAgent := createTestAgentWithTempConfig(t)
	cm := chatAgent.GetConfigManager()

	// Fresh configs have no subagent provider; assert the precondition.
	require.Empty(t, cm.GetConfig().GetSubagentProvider())

	parentProvider := chatAgent.GetProviderType()
	require.NotEmpty(t, parentProvider, "test agent should resolve a provider type")

	seedModelCompleteCache(t, parentProvider, []api.ModelInfo{
		{ID: "parent-model-b"},
		{ID: "parent-model-a"},
	})

	cmd := &SubagentConfigCommand{configType: "model"}
	got := cmd.Complete(nil, chatAgent)
	require.NotNil(t, got, "should return models for the parent agent's provider")
	assert.Equal(t, []string{"parent-model-a", "parent-model-b"}, got)
}

func TestSubagentConfigCommand_Complete_Model_FiltersByPrefix(t *testing.T) {
	chatAgent := createTestAgentWithTempConfig(t)
	cm := chatAgent.GetConfigManager()
	require.NoError(t, cm.UpdateConfig(func(c *configuration.Config) error {
		c.SetSubagentProvider("openai")
		return nil
	}))

	seedModelCompleteCache(t, api.OpenAIClientType, []api.ModelInfo{
		{ID: "gpt-4"},
		{ID: "gpt-3.5-turbo"},
		{ID: "claude-3-opus"},
		{ID: "gemini-pro"},
		// Contains "gpt" but does not start with it — must NOT match a
		// prefix filter (guards against a substring-match regression).
		{ID: "custom-gptx"},
	})

	cmd := &SubagentConfigCommand{configType: "model"}
	got := cmd.Complete([]string{"gpt"}, chatAgent)
	require.NotNil(t, got)
	assert.Equal(t, []string{"gpt-3.5-turbo", "gpt-4"}, got, "should filter by prefix and sort")
	assert.NotContains(t, got, "custom-gptx", "substring match must not be treated as a prefix match")

	// Case-insensitive prefix must produce the same results.
	upper := cmd.Complete([]string{"GPT"}, chatAgent)
	assert.Equal(t, got, upper, "case-insensitive prefix should match the same models")
}

func TestSubagentConfigCommand_Complete_Model_UsesLastArgAsPrefix(t *testing.T) {
	chatAgent := createTestAgentWithTempConfig(t)
	cm := chatAgent.GetConfigManager()
	require.NoError(t, cm.UpdateConfig(func(c *configuration.Config) error {
		c.SetSubagentProvider("openai")
		return nil
	}))

	seedModelCompleteCache(t, api.OpenAIClientType, []api.ModelInfo{
		{ID: "gpt-4"},
		{ID: "claude-3"},
	})

	cmd := &SubagentConfigCommand{configType: "model"}
	// The prefix comes from the last argument, not the first.
	got := cmd.Complete([]string{"gpt", "claude"}, chatAgent)
	assert.Equal(t, []string{"claude-3"}, got)
}

func TestSubagentConfigCommand_Complete_Model_CapsAt20(t *testing.T) {
	chatAgent := createTestAgentWithTempConfig(t)
	cm := chatAgent.GetConfigManager()
	require.NoError(t, cm.UpdateConfig(func(c *configuration.Config) error {
		c.SetSubagentProvider("openai")
		return nil
	}))

	models := make([]api.ModelInfo, 0, 25)
	for i := 1; i <= 25; i++ {
		models = append(models, api.ModelInfo{ID: fmt.Sprintf("model-%02d", i)})
	}
	seedModelCompleteCache(t, api.OpenAIClientType, models)

	cmd := &SubagentConfigCommand{configType: "model"}

	// Unfiltered completion must be capped at 20 and sorted.
	got := cmd.Complete(nil, chatAgent)
	require.Len(t, got, 20, "should cap results at 20")
	assert.True(t, sort.StringsAreSorted(got), "results should be sorted")
	assert.Equal(t, "model-01", got[0])
	assert.Equal(t, "model-20", got[19])

	// A prefix that narrows below the cap returns everything matching.
	gotNarrow := cmd.Complete([]string{"model-1"}, chatAgent)
	require.Len(t, gotNarrow, 10, "prefix 'model-1' should match model-10..model-19")
	for _, m := range gotNarrow {
		assert.True(t, strings.HasPrefix(m, "model-1"), "result %q should start with 'model-1'", m)
	}
}

func TestSubagentConfigCommand_Complete_Model_UnknownSubagentProvider(t *testing.T) {
	chatAgent := createTestAgentWithTempConfig(t)
	cm := chatAgent.GetConfigManager()
	require.NoError(t, cm.UpdateConfig(func(c *configuration.Config) error {
		c.SetSubagentProvider("no-such-provider-xyz")
		return nil
	}))

	cmd := &SubagentConfigCommand{configType: "model"}
	assert.Nil(t, cmd.Complete(nil, chatAgent), "unknown subagent provider should yield no completions")
}

func TestSubagentConfigCommand_Complete_Model_EmptyModelList_ReturnsNil(t *testing.T) {
	chatAgent := createTestAgentWithTempConfig(t)
	cm := chatAgent.GetConfigManager()
	require.NoError(t, cm.UpdateConfig(func(c *configuration.Config) error {
		c.SetSubagentProvider("openai")
		return nil
	}))

	seedModelCompleteCache(t, api.OpenAIClientType, []api.ModelInfo{})

	cmd := &SubagentConfigCommand{configType: "model"}
	assert.Nil(t, cmd.Complete(nil, chatAgent), "empty model list should yield no completions")
}

func TestSubagentConfigCommand_Complete_Model_NoMatch(t *testing.T) {
	chatAgent := createTestAgentWithTempConfig(t)
	cm := chatAgent.GetConfigManager()
	require.NoError(t, cm.UpdateConfig(func(c *configuration.Config) error {
		c.SetSubagentProvider("openai")
		return nil
	}))

	seedModelCompleteCache(t, api.OpenAIClientType, []api.ModelInfo{
		{ID: "gpt-4"},
		{ID: "claude-3"},
	})

	cmd := &SubagentConfigCommand{configType: "model"}
	assert.Empty(t, cmd.Complete([]string{"zzz-no-such-model"}, chatAgent))
}
