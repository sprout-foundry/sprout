package configuration

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// APIKeys helpers
// =============================================================================

func TestAPIKeys_Get(t *testing.T) {
	tests := []struct {
		name     string
		keys     APIKeys
		provider string
		expected string
	}{
		{
			name:     "returns value when provider exists",
			keys:     APIKeys{"openai": "sk-123"},
			provider: "openai",
			expected: "sk-123",
		},
		{
			name:     "returns empty string when provider missing",
			keys:     APIKeys{"openai": "sk-123"},
			provider: "deepinfra",
			expected: "",
		},
		{
			name:     "returns empty string from empty map",
			keys:     APIKeys{},
			provider: "openai",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.keys.Get(tt.provider)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestAPIKeys_Set(t *testing.T) {
	t.Run("sets value in existing map", func(t *testing.T) {
		keys := APIKeys{"openai": "sk-old"}
		keys.Set("openai", "sk-new")
		assert.Equal(t, "sk-new", keys["openai"])
	})

	t.Run("adds new key to existing map", func(t *testing.T) {
		keys := APIKeys{"openai": "sk-123"}
		keys.Set("deepinfra", "sk-456")
		assert.Equal(t, "sk-456", keys["deepinfra"])
		assert.Equal(t, "sk-123", keys["openai"])
	})

	t.Run("initializes nil map before setting", func(t *testing.T) {
		var keys APIKeys
		require.Nil(t, keys)
		keys.Set("openai", "sk-123")
		require.NotNil(t, keys)
		assert.Equal(t, "sk-123", keys["openai"])
	})
}

// =============================================================================
// SetModelForProvider
// =============================================================================

func TestSetModelForProvider(t *testing.T) {
	t.Run("sets model and last used provider", func(t *testing.T) {
		cfg := NewConfig()
		cfg.SetModelForProvider("openai", "gpt-5")
		assert.Equal(t, "gpt-5", cfg.ProviderModels["openai"])
		assert.Equal(t, "openai", cfg.LastUsedProvider)
	})

	t.Run("rejects test provider silently", func(t *testing.T) {
		cfg := NewConfig()
		cfg.SetModelForProvider("test", "some-model")
		_, exists := cfg.ProviderModels["test"]
		assert.False(t, exists, "test provider should not be set")
		assert.Empty(t, cfg.LastUsedProvider, "last used provider should not change")
	})

	t.Run("initializes ProviderModels when nil", func(t *testing.T) {
		cfg := &Config{}
		cfg.SetModelForProvider("openai", "gpt-5")
		require.NotNil(t, cfg.ProviderModels)
		assert.Equal(t, "gpt-5", cfg.ProviderModels["openai"])
	})
}

// =============================================================================
// GetMCPTimeout
// =============================================================================

func TestGetMCPTimeout(t *testing.T) {
	tests := []struct {
		name     string
		timeout  time.Duration
		expected time.Duration
	}{
		{
			name:     "returns configured timeout",
			timeout:  45 * time.Second,
			expected: 45 * time.Second,
		},
		{
			name:     "returns default 30s when zero",
			timeout:  0,
			expected: 30 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				MCP: mcp.MCPConfig{Timeout: tt.timeout},
			}
			result := cfg.GetMCPTimeout()
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// GetSubagentProvider / SetSubagentProvider
// =============================================================================

func TestGetSubagentProvider(t *testing.T) {
	tests := []struct {
		name     string
		config   *Config
		expected string
	}{
		{
			name:     "returns explicit SubagentProvider",
			config:   &Config{SubagentProvider: "openai"},
			expected: "openai",
		},
		{
			name:     "falls back to LastUsedProvider when SubagentProvider empty",
			config:   &Config{LastUsedProvider: "deepinfra"},
			expected: "deepinfra",
		},
		{
			name:     "falls back to ProviderPriority[0] when both empty",
			config:   &Config{ProviderPriority: []string{"zai", "openai"}},
			expected: "zai",
		},
		{
			name:     "ultimate fallback to ollama-local",
			config:   &Config{},
			expected: "ollama-local",
		},
		{
			name:     "ultimate fallback with empty ProviderPriority",
			config:   &Config{ProviderPriority: []string{}},
			expected: "ollama-local",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.GetSubagentProvider()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSetSubagentProvider(t *testing.T) {
	cfg := &Config{}
	cfg.SetSubagentProvider("openai")
	assert.Equal(t, "openai", cfg.SubagentProvider)
}

// =============================================================================
// GetSubagentModel / SetSubagentModel
// =============================================================================

func TestGetSubagentModel(t *testing.T) {
	t.Run("returns explicit SubagentModel", func(t *testing.T) {
		cfg := &Config{SubagentModel: "gpt-5-mini"}
		assert.Equal(t, "gpt-5-mini", cfg.GetSubagentModel())
	})

	t.Run("falls back to provider default model when SubagentModel empty", func(t *testing.T) {
		cfg := &Config{
			LastUsedProvider: "openai",
			ProviderModels:   map[string]string{"openai": "gpt-5"},
		}
		assert.Equal(t, "gpt-5", cfg.GetSubagentModel())
	})

	t.Run("falls back through GetSubagentProvider chain", func(t *testing.T) {
		cfg := &Config{
			SubagentProvider: "deepinfra",
			ProviderModels:   map[string]string{"deepinfra": "deepseek-v3"},
		}
		assert.Equal(t, "deepseek-v3", cfg.GetSubagentModel())
	})
}

func TestSetSubagentModel(t *testing.T) {
	cfg := &Config{}
	cfg.SetSubagentModel("gpt-5-mini")
	assert.Equal(t, "gpt-5-mini", cfg.SubagentModel)
}

// =============================================================================
// GetSubagentTypeProvider / GetSubagentTypeModel
// =============================================================================

func TestGetSubagentTypeProvider(t *testing.T) {
	t.Run("returns subagent type provider when set", func(t *testing.T) {
		cfg := &Config{
			SubagentTypes: map[string]SubagentType{
				"coder": {ID: "coder", Provider: "openai", Enabled: true},
			},
			SubagentProvider: "deepinfra",
		}
		assert.Equal(t, "openai", cfg.GetSubagentTypeProvider("coder"))
	})

	t.Run("falls back to general subagent provider when type has no provider", func(t *testing.T) {
		cfg := &Config{
			SubagentTypes: map[string]SubagentType{
				"coder": {ID: "coder", Enabled: true},
			},
			SubagentProvider: "deepinfra",
		}
		assert.Equal(t, "deepinfra", cfg.GetSubagentTypeProvider("coder"))
	})

	t.Run("falls back to general subagent provider when type not found", func(t *testing.T) {
		cfg := &Config{
			SubagentProvider: "ollama-local",
		}
		assert.Equal(t, "ollama-local", cfg.GetSubagentTypeProvider("nonexistent"))
	})
}

func TestGetSubagentTypeModel(t *testing.T) {
	t.Run("returns subagent type model when set", func(t *testing.T) {
		cfg := &Config{
			SubagentTypes: map[string]SubagentType{
				"coder": {ID: "coder", Model: "gpt-5", Enabled: true},
			},
			SubagentModel: "gpt-4",
		}
		assert.Equal(t, "gpt-5", cfg.GetSubagentTypeModel("coder"))
	})

	t.Run("falls back to general subagent model when type has no model", func(t *testing.T) {
		cfg := &Config{
			SubagentTypes: map[string]SubagentType{
				"coder": {ID: "coder", Enabled: true},
			},
			SubagentModel: "gpt-4",
		}
		assert.Equal(t, "gpt-4", cfg.GetSubagentTypeModel("coder"))
	})

	t.Run("falls back to general subagent model when type not found", func(t *testing.T) {
		cfg := &Config{
			SubagentModel: "gpt-4",
		}
		assert.Equal(t, "gpt-4", cfg.GetSubagentTypeModel("nonexistent"))
	})
}

// =============================================================================
// GetSkill
// =============================================================================

func TestGetSkill(t *testing.T) {
	tests := []struct {
		name     string
		config   *Config
		id       string
		expected *Skill
	}{
		{
			name: "returns enabled skill",
			config: &Config{
				Skills: map[string]Skill{
					"project-planning": {ID: "project-planning", Name: "Project Planning", Enabled: true},
				},
			},
			id: "project-planning",
			expected: &Skill{ID: "project-planning", Name: "Project Planning", Enabled: true},
		},
		{
			name: "returns nil for disabled skill",
			config: &Config{
				Skills: map[string]Skill{
					"browse-debugging": {ID: "browse-debugging", Name: "Browse Debugging", Enabled: false},
				},
			},
			id:       "browse-debugging",
			expected: nil,
		},
		{
			name: "returns nil for missing skill",
			config: &Config{
				Skills: map[string]Skill{
					"project-planning": {ID: "project-planning", Name: "Project Planning", Enabled: true},
				},
			},
			id:       "nonexistent",
			expected: nil,
		},
		{
			name:     "returns nil when Skills is nil",
			config:   &Config{},
			id:       "anything",
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.GetSkill(tt.id)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// GetSkillPath
// =============================================================================

func TestGetSkillPath(t *testing.T) {
	t.Run("returns path from enabled skill", func(t *testing.T) {
		cfg := &Config{
			Skills: map[string]Skill{
				"project-planning": {
					ID:      "project-planning",
					Path:    "pkg/agent/skills/project-planning",
					Enabled: true,
				},
			},
		}
		assert.Equal(t, "pkg/agent/skills/project-planning", cfg.GetSkillPath("project-planning"))
	})

	t.Run("returns empty string for disabled skill", func(t *testing.T) {
		cfg := &Config{
			Skills: map[string]Skill{
				"browse-debugging": {
					ID:      "browse-debugging",
					Path:    "pkg/agent/skills/browse-debugging",
					Enabled: false,
				},
			},
		}
		assert.Empty(t, cfg.GetSkillPath("browse-debugging"))
	})

	t.Run("returns empty string for missing skill", func(t *testing.T) {
		cfg := &Config{
			Skills: map[string]Skill{},
		}
		assert.Empty(t, cfg.GetSkillPath("nonexistent"))
	})

	t.Run("returns empty string when skill has no path", func(t *testing.T) {
		cfg := &Config{
			Skills: map[string]Skill{
				"empty-path": {ID: "empty-path", Path: "", Enabled: true},
			},
		}
		assert.Empty(t, cfg.GetSkillPath("empty-path"))
	})
}

// =============================================================================
// GetAllEnabledSkills
// =============================================================================

func TestGetAllEnabledSkills(t *testing.T) {
	t.Run("returns only enabled skills", func(t *testing.T) {
		cfg := &Config{
			Skills: map[string]Skill{
				"project-planning": {ID: "project-planning", Name: "Project Planning", Enabled: true},
				"browse-debugging": {ID: "browse-debugging", Name: "Browse Debugging", Enabled: false},
				"repo-onboarding":  {ID: "repo-onboarding", Name: "Repo Onboarding", Enabled: true},
			},
		}
		result := cfg.GetAllEnabledSkills()
		assert.Len(t, result, 2)
		assert.Contains(t, result, "project-planning")
		assert.Contains(t, result, "repo-onboarding")
		assert.NotContains(t, result, "browse-debugging")
	})

	t.Run("returns nil when Skills is nil", func(t *testing.T) {
		cfg := &Config{}
		assert.Nil(t, cfg.GetAllEnabledSkills())
	})

	t.Run("returns empty map when all skills disabled", func(t *testing.T) {
		cfg := &Config{
			Skills: map[string]Skill{
				"disabled": {ID: "disabled", Enabled: false},
			},
		}
		result := cfg.GetAllEnabledSkills()
		assert.NotNil(t, result)
		assert.Empty(t, result)
	})
}

// =============================================================================
// mergeMissingDefaultSubagentTypes
// =============================================================================

func TestMergeMissingDefaultSubagentTypes(t *testing.T) {
	t.Run("merges missing defaults into existing config", func(t *testing.T) {
		cfg := &Config{
			SubagentTypes: map[string]SubagentType{
				"custom_persona": {ID: "custom_persona", Name: "Custom", Enabled: true},
			},
		}
		mergeMissingDefaultSubagentTypes(cfg)

		// Default personas should now be present
		_, hasCoder := cfg.SubagentTypes["coder"]
		assert.True(t, hasCoder, "coder should be merged in")
		_, hasTester := cfg.SubagentTypes["tester"]
		assert.True(t, hasTester, "tester should be merged in")
		// Custom persona should still be present
		_, hasCustom := cfg.SubagentTypes["custom_persona"]
		assert.True(t, hasCustom, "custom persona should remain")
	})

	t.Run("does not overwrite existing entries", func(t *testing.T) {
		cfg := &Config{
			SubagentTypes: map[string]SubagentType{
				"coder": {ID: "coder", Name: "Custom Coder", Enabled: true},
			},
		}
		mergeMissingDefaultSubagentTypes(cfg)
		assert.Equal(t, "Custom Coder", cfg.SubagentTypes["coder"].Name)
	})

	t.Run("initializes SubagentTypes when nil", func(t *testing.T) {
		cfg := &Config{}
		mergeMissingDefaultSubagentTypes(cfg)
		assert.NotNil(t, cfg.SubagentTypes)
		assert.NotEmpty(t, cfg.SubagentTypes)
	})

	t.Run("handles nil config gracefully", func(t *testing.T) {
		mergeMissingDefaultSubagentTypes(nil)
	})
}

// =============================================================================
// mergeMissingDefaultSkills
// =============================================================================

func TestMergeMissingDefaultSkills(t *testing.T) {
	t.Run("merges missing defaults into existing config", func(t *testing.T) {
		cfg := &Config{
			Skills: map[string]Skill{
				"custom-skill": {ID: "custom-skill", Name: "Custom", Enabled: true},
			},
		}
		mergeMissingDefaultSkills(cfg)

		// Default skills should now be present
		_, hasGo := cfg.Skills["project-planning"]
		assert.True(t, hasGo, "project-planning should be merged in")
		_, hasTest := cfg.Skills["browse-debugging"]
		assert.True(t, hasTest, "browse-debugging should be merged in")
		// Custom skill should still be present
		_, hasCustom := cfg.Skills["custom-skill"]
		assert.True(t, hasCustom, "custom skill should remain")
	})

	t.Run("does not overwrite existing entries", func(t *testing.T) {
		cfg := &Config{
			Skills: map[string]Skill{
				"project-planning": {ID: "project-planning", Name: "Custom Go", Enabled: true},
			},
		}
		mergeMissingDefaultSkills(cfg)
		assert.Equal(t, "Custom Go", cfg.Skills["project-planning"].Name)
	})

	t.Run("initializes Skills when nil", func(t *testing.T) {
		cfg := &Config{}
		mergeMissingDefaultSkills(cfg)
		assert.NotNil(t, cfg.Skills)
		assert.NotEmpty(t, cfg.Skills)
	})

	t.Run("handles nil config gracefully", func(t *testing.T) {
		mergeMissingDefaultSkills(nil)
	})
}

// =============================================================================
// GetWorkspaceConfigPath
// =============================================================================

func TestGetWorkspaceConfigPath(t *testing.T) {
	tests := []struct {
		name     string
		root     string
		expected string
	}{
		{
			name:     "joins workspace root with .sprout/config.json",
			root:     "/home/user/project",
			expected: "/home/user/project/.sprout/config.json",
		},
		{
			name:     "handles nested paths",
			root:     "/a/b/c/d",
			expected: "/a/b/c/d/.sprout/config.json",
		},
		{
			name:     "handles relative paths",
			root:     "myproject",
			expected: filepath.Join("myproject", ".sprout", "config.json"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetWorkspaceConfigPath(tt.root)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// IsWorkspaceConfigPresent
// =============================================================================

func TestIsWorkspaceConfigPresent(t *testing.T) {
	t.Run("returns true when config file exists", func(t *testing.T) {
		tmpDir := t.TempDir()
		// Create the .sprout/config.json path
		configDir := filepath.Join(tmpDir, ".sprout")
		require.NoError(t, os.MkdirAll(configDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.json"), []byte("{}"), 0644))

		assert.True(t, IsWorkspaceConfigPresent(tmpDir))
	})

	t.Run("returns false when config file does not exist", func(t *testing.T) {
		tmpDir := t.TempDir()
		assert.False(t, IsWorkspaceConfigPresent(tmpDir))
	})

	t.Run("returns false when .sprout dir does not exist", func(t *testing.T) {
		tmpDir := t.TempDir()
		assert.False(t, IsWorkspaceConfigPresent(tmpDir))
	})
}

// =============================================================================
// parseSkillFrontMatter
// =============================================================================

func TestParseSkillFrontMatter(t *testing.T) {
	tests := []struct {
		name          string
		content       string
		expectedName  string
		expectedDesc  string
	}{
		{
			name: "parses name and description from front matter",
			content: `---
name: My Skill
description: Does something useful
---
Some body content here.`,
			expectedName:  "My Skill",
			expectedDesc:  "Does something useful",
		},
		{
			name: "handles front matter with only name",
			content: `---
name: Just Name
---
Body.`,
			expectedName:  "Just Name",
			expectedDesc:  "",
		},
		{
			name: "handles front matter with only description",
			content: `---
description: Just Description
---
Body.`,
			expectedName:  "",
			expectedDesc:  "Just Description",
		},
		{
			name: "handles empty front matter",
			content: `---
---
Body.`,
			expectedName:  "",
			expectedDesc:  "",
		},
		{
			name: "ignores content outside front matter",
			content: `No front matter here.
name: should not parse
description: should not parse`,
			expectedName:  "",
			expectedDesc:  "",
		},
		{
			name: "handles content with no front matter delimiters",
			content: `Just plain text.`,
			expectedName:  "",
			expectedDesc:  "",
		},
		{
			name: "handles empty content",
			content: "",
			expectedName:  "",
			expectedDesc:  "",
		},
		{
			name: "preserves extra fields in front matter without interference",
			content: `---
author: Someone
name: Skill Name
version: 1.0
description: Skill Description
---
Body.`,
			expectedName:  "Skill Name",
			expectedDesc:  "Skill Description",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, desc := parseSkillFrontMatter(tt.content)
			assert.Equal(t, tt.expectedName, name)
			assert.Equal(t, tt.expectedDesc, desc)
		})
	}
}

// =============================================================================
// normalizePersonaID
// =============================================================================

func TestNormalizePersonaID(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "lowercases and replaces hyphens with underscores",
			input:    "Web-Scraper",
			expected: "web_scraper",
		},
		{
			name:     "trims whitespace",
			input:    "  coder  ",
			expected: "coder",
		},
		{
			name:     "handles already normalized input",
			input:    "tester",
			expected: "tester",
		},
		{
			name:     "handles multiple hyphens",
			input:    "my-super-cool-agent",
			expected: "my_super_cool_agent",
		},
		{
			name:     "handles mixed case with hyphens",
			input:    "Computer_User",
			expected: "computer_user",
		},
		{
			name:     "handles empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "handles whitespace-only input",
			input:    "   ",
			expected: "",
		},
		{
			name:     "handles all caps",
			input:    "ORCHESTRATOR",
			expected: "orchestrator",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizePersonaID(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
