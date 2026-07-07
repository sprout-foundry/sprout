//go:build !js

package commands

import (
	"sort"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/stretchr/testify/assert"
)

// ---------------------------------------------------------------------------
// HelpCommand.Complete
// ---------------------------------------------------------------------------

func TestHelpCommand_Complete_EmptyArgs(t *testing.T) {
	registry := NewCommandRegistry()
	cmd := &HelpCommand{registry: registry}

	results := cmd.Complete(nil, nil)

	// Must be non-empty, sorted, no duplicates
	assert.NotEmpty(t, results, "should return command names and aliases")
	assert.True(t, sort.StringsAreSorted(results), "results should be sorted alphabetically")

	// Spot-check a few canonical names
	assert.Contains(t, results, "help")
	assert.Contains(t, results, "model")
	assert.Contains(t, results, "provider")
	assert.Contains(t, results, "skill")
	assert.Contains(t, results, "exit")

	// Spot-check a few aliases
	assert.Contains(t, results, "h") // alias for help
	assert.Contains(t, results, "m") // alias for model
	assert.Contains(t, results, "p") // alias for provider
	assert.Contains(t, results, "x") // alias for exit
	assert.Contains(t, results, "q") // alias for exit
	assert.Contains(t, results, "?") // alias for help
	assert.Contains(t, results, "c") // alias for commit
	assert.Contains(t, results, "s") // alias for search

	// Verify no duplicates
	seen := make(map[string]struct{}, len(results))
	for _, r := range results {
		if _, ok := seen[r]; ok {
			t.Errorf("duplicate entry: %s", r)
		}
		seen[r] = struct{}{}
	}
}

func TestHelpCommand_Complete_PrefixMatch(t *testing.T) {
	registry := NewCommandRegistry()
	cmd := &HelpCommand{registry: registry}

	// Prefix "mo" should match "model" but not "provider"
	results := cmd.Complete([]string{"mo"}, nil)
	assert.NotEmpty(t, results, "should match commands starting with 'mo'")
	assert.Contains(t, results, "model")
	assert.NotContains(t, results, "provider")

	// Case insensitive: "MODEL" or "Model" should match the same set
	resultsUpper := cmd.Complete([]string{"MODEL"}, nil)
	resultsLower := cmd.Complete([]string{"model"}, nil)
	assert.Equal(t, resultsLower, resultsUpper, "case-insensitive matching should produce same results")

	// Every result should actually match the prefix
	for _, r := range results {
		assert.True(t, strings.HasPrefix(strings.ToLower(r), "mo"),
			"result %q should have prefix 'mo' (case-insensitive)", r)
	}
	assert.True(t, sort.StringsAreSorted(results), "results should be sorted")
}

func TestHelpCommand_Complete_NoMatch(t *testing.T) {
	registry := NewCommandRegistry()
	cmd := &HelpCommand{registry: registry}

	results := cmd.Complete([]string{"zzzz"}, nil)
	assert.Empty(t, results, "no commands should match 'zzzz'")
}

func TestHelpCommand_Complete_AgentNil(t *testing.T) {
	registry := NewCommandRegistry()
	cmd := &HelpCommand{registry: registry}

	// nil agent must not panic
	results := cmd.Complete(nil, nil)
	assert.NotNil(t, results)
	assert.NotEmpty(t, results)
	results = cmd.Complete([]string{"mo"}, nil)
	assert.NotNil(t, results)
	assert.NotEmpty(t, results)
}

// ---------------------------------------------------------------------------
// ModelsCommand.Complete
// ---------------------------------------------------------------------------

func TestModelsCommand_Complete_EmptyArgs(t *testing.T) {
	cmd := &ModelsCommand{}

	// No args -> should return ["select"]
	results := cmd.Complete(nil, nil)
	assert.Equal(t, []string{"select"}, results, "empty args should return ['select']")
}

func TestModelsCommand_Complete_PrefixMatch(t *testing.T) {
	cmd := &ModelsCommand{}

	// With NewTestAgent, GetProviderType() returns "".
	// api.GetModelsForProvider("") likely returns nil/error.
	a := agent.NewTestAgent()
	results := cmd.Complete([]string{"gp"}, a)
	// Must not panic; should return nil gracefully
	assert.Nil(t, results, "with test agent and unknown prefix should return nil")
}

func TestModelsCommand_Complete_AgentNil(t *testing.T) {
	cmd := &ModelsCommand{}

	// nil agent should return nil for non-empty args
	results := cmd.Complete([]string{"gp"}, nil)
	assert.Nil(t, results, "nil agent should return nil gracefully")
}

func TestModelsCommand_Complete_CaseSensitivity(t *testing.T) {
	cmd := &ModelsCommand{}

	// Both should hit the same path (nil agent -> return nil)
	lower := cmd.Complete([]string{"gp"}, nil)
	upper := cmd.Complete([]string{"GP"}, nil)
	assert.Equal(t, lower, upper, "case should not matter for nil agent path")
	assert.Nil(t, lower, "should return nil with nil agent")
}

// ---------------------------------------------------------------------------
// ProvidersCommand.Complete
// ---------------------------------------------------------------------------

func TestProvidersCommand_Complete_EmptyArgs(t *testing.T) {
	cmd := &ProvidersCommand{}

	// No args + nil agent -> returns ["list", "select", "status"]
	results := cmd.Complete(nil, nil)
	assert.ElementsMatch(t, []string{"list", "select", "status"}, results)
	assert.True(t, sort.StringsAreSorted(results), "results should be sorted")
}

func TestProvidersCommand_Complete_EmptyArgsWithAgent(t *testing.T) {
	cmd := &ProvidersCommand{}

	// NewTestAgent has no config manager (GetConfigManager returns nil)
	// -> returns basic subcommands only.
	a := agent.NewTestAgent()
	results := cmd.Complete(nil, a)
	assert.ElementsMatch(t, []string{"list", "select", "status"}, results)
	assert.True(t, sort.StringsAreSorted(results), "results should be sorted")
}

func TestProvidersCommand_Complete_PrefixMatchNil(t *testing.T) {
	cmd := &ProvidersCommand{}

	// args=["op"] with nil agent -> returns nil (graceful)
	results := cmd.Complete([]string{"op"}, nil)
	assert.Nil(t, results, "nil agent with non-empty args should return nil")
}

func TestProvidersCommand_Complete_AgentNil(t *testing.T) {
	cmd := &ProvidersCommand{}

	// nil agent everywhere returns nil gracefully
	assert.Nil(t, cmd.Complete([]string{"list"}, nil), "nil agent with args should not panic")
	assert.Nil(t, cmd.Complete([]string{"op"}, nil), "nil agent with prefix should not panic")
	assert.NotNil(t, cmd.Complete(nil, nil), "nil agent with no args should return subcommands")
}

func TestProvidersCommand_Complete_NoMatch(t *testing.T) {
	cmd := &ProvidersCommand{}

	// args=["zzzz"] -> returns nil/empty
	results := cmd.Complete([]string{"zzzz"}, nil)
	assert.Nil(t, results, "no matching provider should return nil")
}

// ---------------------------------------------------------------------------
// SkillCommand.Complete
// ---------------------------------------------------------------------------

func TestSkillCommand_Complete_EmptyArgs(t *testing.T) {
	cmd := &SkillCommand{}

	expected := []string{"install", "update", "remove", "list", "enable", "disable"}
	results := cmd.Complete(nil, nil)
	assert.Equal(t, expected, results, "should return subcommands in definition order")
}

func TestSkillCommand_Complete_StaticSubcommandsDefinedOrder(t *testing.T) {
	cmd := &SkillCommand{}

	results := cmd.Complete(nil, nil)
	// The implementation returns subcommands in definition order (not sorted).
	expected := []string{"install", "update", "remove", "list", "enable", "disable"}
	assert.Equal(t, expected, results, "static subcommands should match definition order")
}

func TestSkillCommand_Complete_EnableArg(t *testing.T) {
	cmd := &SkillCommand{}

	// args=["enable"] with nil agent -> should return nil (needs config manager)
	results := cmd.Complete([]string{"enable"}, nil)
	assert.Nil(t, results, "enable with nil agent should return nil")
}

func TestSkillCommand_Complete_DisableArg(t *testing.T) {
	cmd := &SkillCommand{}

	// args=["disable"] with nil agent -> should return nil
	results := cmd.Complete([]string{"disable"}, nil)
	assert.Nil(t, results, "disable with nil agent should return nil")
}

func TestSkillCommand_Complete_RemoveArg(t *testing.T) {
	cmd := &SkillCommand{}

	// args=["remove"] -> needs disk access. If skills dir doesn't exist -> nil.
	// Must not panic regardless.
	results := cmd.Complete([]string{"remove"}, nil)
	assert.Nil(t, results, "remove should not panic with nil agent")
}

func TestSkillCommand_Complete_UnknownSubcommand(t *testing.T) {
	cmd := &SkillCommand{}

	// args=["unknown"] -> falls through the switch -> returns nil
	results := cmd.Complete([]string{"unknown"}, nil)
	assert.Nil(t, results, "unknown subcommand should return nil")
}

func TestSkillCommand_Complete_AgentNil(t *testing.T) {
	cmd := &SkillCommand{}

	// All code paths with nil agent
	assert.NotNil(t, cmd.Complete(nil, nil), "nil agent with no args should return subcommands")
	assert.Nil(t, cmd.Complete([]string{"enable"}, nil), "enable with nil agent should return nil")
	assert.Nil(t, cmd.Complete([]string{"disable"}, nil), "disable with nil agent should return nil")
	assert.Nil(t, cmd.Complete([]string{"remove"}, nil), "remove with nil agent should return nil")
	assert.Nil(t, cmd.Complete([]string{"update"}, nil), "update with nil agent should return nil")
	assert.Nil(t, cmd.Complete([]string{"install"}, nil), "install with nil agent should return nil")
	assert.Nil(t, cmd.Complete([]string{"list"}, nil), "list with nil agent should return nil")
	assert.Nil(t, cmd.Complete([]string{"unknown"}, nil), "unknown subcommand should return nil")
}

func TestSkillCommand_Complete_EnableDisableWithAgent(t *testing.T) {
	cmd := &SkillCommand{}

	// NewTestAgent has no config manager -> returns nil
	a := agent.NewTestAgent()
	results := cmd.Complete([]string{"enable"}, a)
	assert.Nil(t, results, "enable with test agent (no config manager) should return nil")

	results = cmd.Complete([]string{"disable"}, a)
	assert.Nil(t, results, "disable with test agent (no config manager) should return nil")
}

// ---------------------------------------------------------------------------
// SettingsCommand.Complete
// ---------------------------------------------------------------------------

func TestSettingsCommand_Complete_EmptyArgs(t *testing.T) {
	cmd := &SettingsCommand{}

	// No args -> should return ["set"]
	results := cmd.Complete(nil, nil)
	assert.Equal(t, []string{"set"}, results, "empty args should return ['set']")
	assert.True(t, sort.StringsAreSorted(results), "results should be sorted")
}

func TestSettingsCommand_Complete_SetSubcommand(t *testing.T) {
	cmd := &SettingsCommand{}

	// args=["set"] -> returns all setting keys via agent.SupportedSettingKeys()
	results := cmd.Complete([]string{"set"}, nil)
	assert.NotEmpty(t, results, "should return setting keys")
	assert.True(t, sort.StringsAreSorted(results), "results should be sorted")

	// Spot-check known setting keys
	assert.Contains(t, results, "provider")
	assert.Contains(t, results, "model")
	assert.Contains(t, results, "reasoning_effort")
	assert.Contains(t, results, "disable_thinking")
	assert.Contains(t, results, "approved_shell_commands")

	// Verify no duplicates
	seen := make(map[string]struct{}, len(results))
	for _, r := range results {
		if _, ok := seen[r]; ok {
			t.Errorf("duplicate setting key: %s", r)
		}
		seen[r] = struct{}{}
	}
}

func TestSettingsCommand_Complete_SetWithPrefix(t *testing.T) {
	cmd := &SettingsCommand{}

	// args=["set", "sub"] -> returns setting keys matching "sub"
	results := cmd.Complete([]string{"set", "sub"}, nil)
	assert.NotEmpty(t, results, "should match keys with prefix 'sub'")
	assert.Contains(t, results, "subagent_model")
	assert.NotContains(t, results, "provider")

	// Every result should actually match the prefix
	for _, r := range results {
		assert.True(t, strings.HasPrefix(strings.ToLower(r), "sub"),
			"result %q should have prefix 'sub' (case-insensitive)", r)
	}

	// Case insensitive: "SUB" should match the same set
	resultsUpper := cmd.Complete([]string{"set", "SUB"}, nil)
	resultsLower := cmd.Complete([]string{"set", "sub"}, nil)
	assert.Equal(t, resultsLower, resultsUpper, "case-insensitive matching should produce same results")
	assert.True(t, sort.StringsAreSorted(results), "results should be sorted")
}

func TestSettingsCommand_Complete_NoMatch(t *testing.T) {
	cmd := &SettingsCommand{}

	// args=["set", "zzzz"] -> returns nil/empty
	results := cmd.Complete([]string{"set", "zzzz"}, nil)
	assert.Empty(t, results, "no settings should match 'zzzz'")
}

func TestSettingsCommand_Complete_UnknownArg(t *testing.T) {
	cmd := &SettingsCommand{}

	// args=["unknown"] -> returns nil (not "set")
	results := cmd.Complete([]string{"unknown"}, nil)
	assert.Nil(t, results, "unknown first arg should return nil")
}

func TestSettingsCommand_Complete_AgentNil(t *testing.T) {
	cmd := &SettingsCommand{}

	// nil agent doesn't affect settings completion (doesn't use agent)
	results := cmd.Complete(nil, nil)
	assert.Equal(t, []string{"set"}, results, "nil agent with no args should still return ['set']")

	results = cmd.Complete([]string{"set"}, nil)
	assert.NotEmpty(t, results, "nil agent with args=['set'] should still return setting keys")
	assert.Contains(t, results, "provider")
}

// ---------------------------------------------------------------------------
// CodegraphCommand.Complete
// ---------------------------------------------------------------------------

func TestCodegraphCommand_Complete_EmptyArgs(t *testing.T) {
	cmd := &CodegraphCommand{}

	// No args -> should return ["build", "help", "stats", "update"] sorted
	expected := []string{"build", "help", "stats", "update"}
	results := cmd.Complete(nil, nil)
	assert.Equal(t, expected, results, "empty args should return all subcommands")
	assert.True(t, sort.StringsAreSorted(results), "results should be sorted")
}

func TestCodegraphCommand_Complete_PrefixMatch(t *testing.T) {
	cmd := &CodegraphCommand{}

	// args=["b"] -> returns ["build"]
	results := cmd.Complete([]string{"b"}, nil)
	assert.Equal(t, []string{"build"}, results, "prefix 'b' should match 'build'")

	// args=["st"] -> returns ["stats"]
	results = cmd.Complete([]string{"st"}, nil)
	assert.Equal(t, []string{"stats"}, results, "prefix 'st' should match 'stats'")

	// args=["up"] -> returns ["update"]
	results = cmd.Complete([]string{"up"}, nil)
	assert.Equal(t, []string{"update"}, results, "prefix 'up' should match 'update'")

	// Case insensitive: "B" should match "build"
	resultsUpper := cmd.Complete([]string{"B"}, nil)
	assert.Equal(t, []string{"build"}, resultsUpper, "case-insensitive prefix 'B' should match 'build'")
}

func TestCodegraphCommand_Complete_NoMatch(t *testing.T) {
	cmd := &CodegraphCommand{}

	// args=["zzzz"] -> returns nil
	results := cmd.Complete([]string{"zzzz"}, nil)
	assert.Empty(t, results, "no subcommands should match 'zzzz'")
}

func TestCodegraphCommand_Complete_AgentNil(t *testing.T) {
	cmd := &CodegraphCommand{}

	// nil agent = no-op (doesn't use agent)
	results := cmd.Complete(nil, nil)
	assert.NotNil(t, results, "nil agent with no args should return subcommands")
	assert.Equal(t, []string{"build", "help", "stats", "update"}, results)

	results = cmd.Complete([]string{"b"}, nil)
	assert.Equal(t, []string{"build"}, results, "prefix match should work with nil agent")

	results = cmd.Complete([]string{"zzzz"}, nil)
	assert.Empty(t, results, "no match should work with nil agent")
}

// ---------------------------------------------------------------------------
// IndexCommand.Complete
// ---------------------------------------------------------------------------

func TestIndexCommand_Complete_EmptyArgs(t *testing.T) {
	cmd := &IndexCommand{}

	// No args -> should return ["disable", "enable", "off", "on", "status", "toggle"] sorted
	expected := []string{"disable", "enable", "off", "on", "status", "toggle"}
	results := cmd.Complete(nil, nil)
	assert.Equal(t, expected, results, "empty args should return all subcommands")
	assert.True(t, sort.StringsAreSorted(results), "results should be sorted")
}

func TestIndexCommand_Complete_PrefixMatch(t *testing.T) {
	cmd := &IndexCommand{}

	// args=["e"] -> returns ["enable"] (not "disable")
	results := cmd.Complete([]string{"e"}, nil)
	assert.Equal(t, []string{"enable"}, results, "prefix 'e' should match 'enable'")

	// args=["of"] -> returns ["off"]
	results = cmd.Complete([]string{"of"}, nil)
	assert.Equal(t, []string{"off"}, results, "prefix 'of' should match 'off'")

	// args=["d"] -> returns ["disable"]
	results = cmd.Complete([]string{"d"}, nil)
	assert.Equal(t, []string{"disable"}, results, "prefix 'd' should match 'disable'")

	// args=["st"] -> returns ["status"]
	results = cmd.Complete([]string{"st"}, nil)
	assert.Equal(t, []string{"status"}, results, "prefix 'st' should match 'status'")

	// args=["t"] -> returns ["toggle"]
	results = cmd.Complete([]string{"t"}, nil)
	assert.Equal(t, []string{"toggle"}, results, "prefix 't' should match 'toggle'")

	// Case insensitive: "E" should match "enable"
	resultsUpper := cmd.Complete([]string{"E"}, nil)
	assert.Equal(t, []string{"enable"}, resultsUpper, "case-insensitive prefix 'E' should match 'enable'")
}

func TestIndexCommand_Complete_NoMatch(t *testing.T) {
	cmd := &IndexCommand{}

	// args=["zzzz"] -> returns nil
	results := cmd.Complete([]string{"zzzz"}, nil)
	assert.Empty(t, results, "no subcommands should match 'zzzz'")
}

// ---------------------------------------------------------------------------
// MCPCommand.Complete
// ---------------------------------------------------------------------------

func TestMCPCommand_Complete_EmptyArgs(t *testing.T) {
	cmd := &MCPCommand{}

	// No args -> should return ["add", "help", "list", "remove", "test"] sorted
	expected := []string{"add", "help", "list", "remove", "test"}
	results := cmd.Complete(nil, nil)
	assert.Equal(t, expected, results, "empty args should return all subcommands")
	assert.True(t, sort.StringsAreSorted(results), "results should be sorted")
}

func TestMCPCommand_Complete_AddSubcommand(t *testing.T) {
	cmd := &MCPCommand{}

	// args=["add"] -> returns ["http", "stdio"] (server types)
	results := cmd.Complete([]string{"add"}, nil)
	// The implementation returns ["stdio", "http"]; use ElementsMatch for
	// order independence.
	assert.ElementsMatch(t, []string{"http", "stdio"}, results,
		"add subcommand should return server type suggestions")
}

func TestMCPCommand_Complete_PrefixMatch(t *testing.T) {
	cmd := &MCPCommand{}

	// args=["r"] -> should match "remove" (and not "add" or "list")
	results := cmd.Complete([]string{"r"}, nil)
	assert.Equal(t, []string{"remove"}, results, "prefix 'r' should match 'remove'")

	// args=["t"] -> should match "test"
	results = cmd.Complete([]string{"t"}, nil)
	assert.Equal(t, []string{"test"}, results, "prefix 't' should match 'test'")

	// args=["a"] -> should match "add"
	results = cmd.Complete([]string{"a"}, nil)
	assert.Equal(t, []string{"add"}, results, "prefix 'a' should match 'add'")

	// Case insensitive: "R" should match "remove"
	resultsUpper := cmd.Complete([]string{"R"}, nil)
	assert.Equal(t, []string{"remove"}, resultsUpper, "case-insensitive prefix 'R' should match 'remove'")
}

func TestMCPCommand_Complete_NoMatch(t *testing.T) {
	cmd := &MCPCommand{}

	// args=["zzzz"] -> returns nil
	results := cmd.Complete([]string{"zzzz"}, nil)
	assert.Empty(t, results, "no subcommands should match 'zzzz'")
}

func TestMCPCommand_Complete_AgentNil(t *testing.T) {
	cmd := &MCPCommand{}

	// nil agent for subcommands works for static completions
	results := cmd.Complete(nil, nil)
	assert.NotNil(t, results, "nil agent with no args should return subcommands")
	assert.Equal(t, []string{"add", "help", "list", "remove", "test"}, results)

	results = cmd.Complete([]string{"add"}, nil)
	assert.NotNil(t, results, "add with nil agent should return server types")

	results = cmd.Complete([]string{"r"}, nil)
	assert.Equal(t, []string{"remove"}, results, "prefix match with nil agent should work")

	// remove/test with nil agent try to load config from disk, which may fail
	// gracefully (return nil). The important thing is no panic.
	assert.NotPanics(t, func() { cmd.Complete([]string{"remove"}, nil) },
		"remove with nil agent should not panic")

	assert.NotPanics(t, func() { cmd.Complete([]string{"test"}, nil) },
		"test with nil agent should not panic")
}

// ---------------------------------------------------------------------------
// PersonaCommand.Complete
// ---------------------------------------------------------------------------

func TestPersonaCommand_Complete_EmptyArgs(t *testing.T) {
	cmd := &PersonaCommand{}

	// No args + nil agent -> returns ["clear", "list"]
	results := cmd.Complete(nil, nil)
	assert.Equal(t, []string{"clear", "list"}, results,
		"empty args with nil agent should return base commands")
	assert.True(t, sort.StringsAreSorted(results), "results should be sorted")
}

func TestPersonaCommand_Complete_PrefixMatch(t *testing.T) {
	cmd := &PersonaCommand{}

	// args=["l"] -> returns ["list"]
	results := cmd.Complete([]string{"l"}, nil)
	assert.Equal(t, []string{"list"}, results, "prefix 'l' should match 'list'")

	// args=["c"] -> returns ["clear"]
	results = cmd.Complete([]string{"c"}, nil)
	assert.Equal(t, []string{"clear"}, results, "prefix 'c' should match 'clear'")

	// Case insensitive: "L" should match "list"
	resultsUpper := cmd.Complete([]string{"L"}, nil)
	assert.Equal(t, []string{"list"}, resultsUpper, "case-insensitive prefix 'L' should match 'list'")
}

func TestPersonaCommand_Complete_NoMatch(t *testing.T) {
	cmd := &PersonaCommand{}

	// args=["zzzz"] -> returns nil
	results := cmd.Complete([]string{"zzzz"}, nil)
	assert.Empty(t, results, "no candidates should match 'zzzz'")
}

func TestPersonaCommand_Complete_AgentNil(t *testing.T) {
	cmd := &PersonaCommand{}

	// nil agent -> no persona names -> just ["clear", "list"]
	results := cmd.Complete(nil, nil)
	assert.Equal(t, []string{"clear", "list"}, results,
		"nil agent should return just base commands")

	// Prefix matching still works with nil agent
	results = cmd.Complete([]string{"l"}, nil)
	assert.Equal(t, []string{"list"}, results, "prefix match with nil agent should work")

	results = cmd.Complete([]string{"zzzz"}, nil)
	assert.Empty(t, results, "no match with nil agent should return empty")
}

// ---------------------------------------------------------------------------
// RollbackCommand.Complete
// ---------------------------------------------------------------------------

func TestRollbackCommand_Complete_EmptyArgs(t *testing.T) {
	cmd := &RollbackCommand{}

	// NewTestAgent has no change tracker -> GetRevisionID() returns ""
	// -> returns nil
	a := agent.NewTestAgent()
	results := cmd.Complete(nil, a)
	assert.Nil(t, results, "empty args with test agent (no change tracker) should return nil")
}

func TestRollbackCommand_Complete_AgentNil(t *testing.T) {
	cmd := &RollbackCommand{}

	// nil agent -> returns nil gracefully
	results := cmd.Complete(nil, nil)
	assert.Nil(t, results, "nil agent with empty args should return nil")

	results = cmd.Complete([]string{"something"}, nil)
	assert.Nil(t, results, "nil agent with non-empty args should return nil")

	// Must not panic
	assert.NotPanics(t, func() { cmd.Complete(nil, nil) }, "nil agent should not panic")
	assert.NotPanics(t, func() { cmd.Complete([]string{"foo"}, nil) }, "nil agent with args should not panic")
}

func TestRollbackCommand_Complete_WithArgs(t *testing.T) {
	cmd := &RollbackCommand{}

	// args=["something"] -> returns nil (only completes empty args for current revision)
	results := cmd.Complete([]string{"something"}, nil)
	assert.Nil(t, results, "non-empty args should return nil")

	// Also with a real agent
	a := agent.NewTestAgent()
	results = cmd.Complete([]string{"some-revision"}, a)
	assert.Nil(t, results, "non-empty args with test agent should return nil")
}
