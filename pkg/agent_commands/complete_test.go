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
