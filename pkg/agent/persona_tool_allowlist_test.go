package agent

import (
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPersonaToolAllowlistFix verifies that GetSubagentType always returns
// authoritative tool allowlists from defaults, not stale saved config.
func TestPersonaToolAllowlistFix(t *testing.T) {
	// Create a config with a stale persona that has old allowed_tools
	cfg := &configuration.Config{
		SubagentTypes: map[string]configuration.SubagentType{
			"orchestrator": {
				ID:           "orchestrator",
				Name:         "Orchestrator",
				Description:  "Orchestrator persona description",
				Enabled:      true,
				SystemPrompt: "subagent_prompts/orchestrator.md",
				Provider:     "custom-provider", // User override
				Model:        "custom-model",    // User override
				// Stale allowed_tools - missing browse_url, view_history, rollback_changes
				AllowedTools: []string{
					"shell_command",
					"read_file",
					"write_file",
					"edit_file",
					"TodoWrite",
					"TodoRead",
				},
				Aliases: []string{"orch"},
			},
		},
	}

	// Get the orchestrator persona - it should have the authoritative defaults
	// with user overrides applied for provider/model
	persona := cfg.GetSubagentType("orchestrator")
	require.NotNil(t, persona, "orchestrator persona should exist")

	// Verify user overrides are preserved
	assert.Equal(t, "custom-provider", persona.Provider, "user provider override should be preserved")
	assert.Equal(t, "custom-model", persona.Model, "user model override should be preserved")
	assert.Equal(t, "Orchestrator", persona.Name, "name should come from defaults")
	// SystemPrompt is empty in defaults, so it should be empty
	assert.Equal(t, "", persona.SystemPrompt, "system prompt should be empty from defaults")
	// Orchestrator has alias "orchestration" plus legacy aliases from the
	// SP-050 collapse so that pre-existing configs / sessions naming
	// "repo_orchestrator" still resolve.
	assert.Equal(t, []string{"orchestration", "repo_orchestrator", "repo_operator", "git_orchestrator"}, persona.Aliases, "aliases should come from defaults")

	// Verify authoritative allowed_tools include all expected tools
	// The stale config should be ignored and defaults should be used
	expectedTools := []string{
		"shell_command",
		"git",
		"read_file", 
		"write_file",
		"edit_file",
		"write_structured_file",
		"patch_structured_file", 
		"search_files",
		"analyze_ui_screenshot",
		"analyze_image_content",
		"browse_url", // This is in default orchestrator tools!
		"web_search",
		"fetch_url",
		"run_subagent",
		"run_parallel_subagents",
		"mcp_tools",
		"view_history", 
		"rollback_changes",
		"self_review",
		"list_skills",
		"activate_skill",
		"TodoWrite",
		"TodoRead",
		"add_memory",
		"read_memory",
		"list_memories", 
		"delete_memory",
	}

	for _, tool := range expectedTools {
		assert.Contains(t, persona.AllowedTools, tool, "allowed_tools should include %s from defaults", tool)
	}

	// Verify there are no unexpected tools
	for _, tool := range persona.AllowedTools {
		assert.NotNil(t, tool, "tool should not be empty")
	}
}

// TestCustomPersonaPreserved verifies that custom personas (only in user config)
// are preserved as-is without trying to merge with defaults.
func TestCustomPersonaPreserved(t *testing.T) {
	cfg := &configuration.Config{
		SubagentTypes: map[string]configuration.SubagentType{
			"my_custom_persona": {
				ID:           "my_custom_persona",
				Name:         "My Custom Persona",
				Description:  "A custom persona not in defaults",
				Enabled:      true,
				Provider:     "custom-provider",
				Model:        "custom-model",
				SystemPrompt: "my_custom_prompt.md",
				AllowedTools: []string{"shell_command", "read_file"},
				Aliases:      []string{"custom"},
			},
		},
	}

	persona := cfg.GetSubagentType("my_custom_persona")
	require.NotNil(t, persona, "custom persona should exist")

	// All fields should be exactly as provided since this is a custom persona
	assert.Equal(t, "my_custom_persona", persona.ID)
	assert.Equal(t, "My Custom Persona", persona.Name)
	assert.Equal(t, "A custom persona not in defaults", persona.Description)
	assert.Equal(t, "custom-provider", persona.Provider)
	assert.Equal(t, "custom-model", persona.Model)
	assert.Equal(t, "my_custom_prompt.md", persona.SystemPrompt)
	assert.Equal(t, []string{"shell_command", "read_file"}, persona.AllowedTools)
	assert.Equal(t, []string{"custom"}, persona.Aliases)
}

// TestPersonaAliasesAreResolved verifies that aliases work correctly
// and return the same persona as the primary ID.
func TestPersonaAliasesAreResolved(t *testing.T) {
	cfg := &configuration.Config{
		SubagentTypes: map[string]configuration.SubagentType{
			"orchestrator": {
				ID:           "orchestrator",
				Name:         "Orchestrator", 
				Description:  "Orchestrator persona description",
				Enabled:      true,
				SystemPrompt: "subagent_prompts/orchestrator.md",
				Provider:     "user-provider", // User override
				Model:        "user-model",    // User override
				// Don't set AllowedTools here to ensure we get defaults
				Aliases:      []string{"orch"},
			},
		},
	}

	// Let's first check what a default orchestrator looks like
	defaultCfg := configuration.NewConfig()
	defaultOrchestrator := defaultCfg.GetSubagentType("orchestrator")
	require.NotNil(t, defaultOrchestrator)
	t.Logf("Default orchestrator tools: %v", defaultOrchestrator.AllowedTools)

	// Get by primary ID
	primaryPersona := cfg.GetSubagentType("orchestrator")
	require.NotNil(t, primaryPersona)
	t.Logf("Primary persona tools: %v", primaryPersona.AllowedTools)

	// Get by alias 
	aliasPersona := cfg.GetSubagentType("orch")
	require.NotNil(t, aliasPersona)
	t.Logf("Alias persona tools: %v", aliasPersona.AllowedTools)

	// Should be the same persona
	assert.Equal(t, primaryPersona.ID, aliasPersona.ID)
	assert.Equal(t, primaryPersona.Name, aliasPersona.Name)
	assert.Equal(t, primaryPersona.Provider, aliasPersona.Provider)
	assert.Equal(t, primaryPersona.AllowedTools, aliasPersona.AllowedTools)
}

// TestDisabledPersonaReturnsNil verifies that disabled personas return nil
// even when they exist in the config.
func TestDisabledPersonaReturnsNil(t *testing.T) {
	cfg := &configuration.Config{
		SubagentTypes: map[string]configuration.SubagentType{
			"disabled_orchestrator": {
				ID:           "orchestrator",
				Name:         "Orchestrator",
				Description:  "Orchestrator persona description",
				Enabled:      false, // Disabled
				SystemPrompt: "subagent_prompts/orchestrator.md",
				AllowedTools: []string{"shell_command"},
			},
		},
	}

	persona := cfg.GetSubagentType("orchestrator")
	assert.Nil(t, persona, "disabled persona should return nil")

	// Even with a custom override, if the default is disabled it should return nil
	override := cfg.SubagentTypes["disabled_orchestrator"]
	override.Enabled = true
	cfg.SubagentTypes["disabled_orchestrator"] = override
	persona = cfg.GetSubagentType("orchestrator")
	assert.NotNil(t, persona, "enabled persona should not return nil")
}

// TestSystemPromptAppendOverride verifies that system_prompt_append
// can be overridden by users while other system prompt fields come from defaults.
func TestSystemPromptAppendOverride(t *testing.T) {
	cfg := &configuration.Config{
		SubagentTypes: map[string]configuration.SubagentType{
			"coder": {
				ID:                   "coder",
				Name:                 "Coder",
				Description:          "Coder persona description", 
				Enabled:              true,
				SystemPrompt:         "subagent_prompts/coder.md",
				SystemPromptAppend:   "Custom append text from user",
				Provider:             "user-provider",
				Model:                "user-model",
				AllowedTools:         []string{"shell_command"}, // Stale
			},
		},
	}

	persona := cfg.GetSubagentType("coder")
	require.NotNil(t, persona)

	// Verify defaults are used for most fields
	assert.Equal(t, "coder", persona.ID)
	assert.Equal(t, "Coder", persona.Name) 
	assert.Equal(t, "Feature implementation and production code writing specialist", persona.Description)
	// coder should have a system prompt in defaults
	assert.NotEqual(t, "", persona.SystemPrompt, "coder should have a system prompt")

	// Verify user override for system_prompt_append
	assert.Equal(t, "Custom append text from user", persona.SystemPromptAppend)

	// Verify user overrides for provider/model
	assert.Equal(t, "user-provider", persona.Provider)
	assert.Equal(t, "user-model", persona.Model)

	// Verify authoritative allowed_tools includes browse_url
	assert.Contains(t, persona.AllowedTools, "browse_url", "allowed_tools should include browse_url from defaults")
}