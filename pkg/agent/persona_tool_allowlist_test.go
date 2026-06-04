package agent

import (
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPersonaToolsCatalogIsAuthoritative verifies the post-override-removal
// contract: GetSubagentType returns the catalog-defined AllowedTools for built-in
// personas. Users cannot persist a partial override; the catalog is the source
// of truth at load time.
func TestPersonaToolsCatalogIsAuthoritative(t *testing.T) {
	cfg := configuration.NewConfig()

	persona := cfg.GetSubagentType("orchestrator")
	require.NotNil(t, persona, "orchestrator persona should exist in catalog")

	// Verify the orchestrator carries its canonical aliases (SP-050 collapse).
	assert.Equal(t, []string{"orchestration", "repo_orchestrator", "repo_operator", "git_orchestrator"}, persona.Aliases)

	// Verify the catalog ships the full tool list, not a truncated one.
	expectedTools := []string{
		"shell_command",
		"git",
		"read_file",
		"write_file",
		"edit_file",
		"write_structured_file",
		"patch_structured_file",
		"search_files",
		"browse_url",
		"web_search",
		"fetch_url",
		"run_subagent",
		"run_parallel_subagents",
		"view_history",
		"rollback_changes",
		"self_review",
		"list_skills",
		"activate_skill",
		"TodoWrite",
		"TodoRead",
		"manage_memory",
	}
	for _, tool := range expectedTools {
		assert.Contains(t, persona.AllowedTools, tool, "orchestrator catalog should include %s", tool)
	}
}

// TestRuntimePersonaMutationVisibleViaGetSubagentType verifies that in-memory
// mutations of the SubagentTypes map (used by `sprout automate` workflow
// overrides and test fixtures) are visible to GetSubagentType. These mutations
// are not persisted to disk — they live only for the current process.
func TestRuntimePersonaMutationVisibleViaGetSubagentType(t *testing.T) {
	cfg := configuration.NewConfig()

	// Inject a runtime-only persona (the pattern used by workflow overrides
	// and unit tests that need a synthetic fixture).
	cfg.SubagentTypes["test_runtime_only"] = configuration.SubagentType{
		ID:           "test_runtime_only",
		Name:         "Runtime Only",
		Enabled:      true,
		AllowedTools: []string{"shell_command", "read_file"},
	}

	persona := cfg.GetSubagentType("test_runtime_only")
	require.NotNil(t, persona, "runtime-injected persona should be retrievable")
	assert.Equal(t, "Runtime Only", persona.Name)
	assert.Equal(t, []string{"shell_command", "read_file"}, persona.AllowedTools)
}

// TestPersonaAliasesResolveToSameEntry verifies aliases resolve consistently.
func TestPersonaAliasesResolveToSameEntry(t *testing.T) {
	cfg := configuration.NewConfig()

	primary := cfg.GetSubagentType("orchestrator")
	require.NotNil(t, primary)
	alias := cfg.GetSubagentType("repo_orchestrator")
	require.NotNil(t, alias, "legacy alias repo_orchestrator should resolve to orchestrator")

	assert.Equal(t, primary.ID, alias.ID)
	assert.Equal(t, primary.Name, alias.Name)
	assert.Equal(t, primary.AllowedTools, alias.AllowedTools)
}

// TestDisabledPersonaReturnsNil verifies that personas listed in
// Config.DisabledPersonas are filtered out by GetSubagentType.
func TestDisabledPersonaReturnsNil(t *testing.T) {
	cfg := configuration.NewConfig()

	require.NotNil(t, cfg.GetSubagentType("orchestrator"), "orchestrator should exist before disable")

	cfg.SetPersonaDisabled("orchestrator", true)
	assert.Nil(t, cfg.GetSubagentType("orchestrator"), "disabled persona should return nil")

	cfg.SetPersonaDisabled("orchestrator", false)
	assert.NotNil(t, cfg.GetSubagentType("orchestrator"), "re-enabled persona should return non-nil")
}
