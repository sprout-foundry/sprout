package configuration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/mcp"
	"github.com/sprout-foundry/sprout/pkg/skills"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name        string
		config      *Config
		expectError bool
		errorMsg    string
	}{
		{
			name: "PDF OCR disabled - should pass",
			config: &Config{
				PDFOCREnabled: false,
			},
			expectError: false,
		},
		{
			name: "PDF OCR enabled with provider and model - should pass",
			config: &Config{
				PDFOCREnabled:  true,
				PDFOCRProvider: "ollama",
				PDFOCRModel:    "glm-ocr",
			},
			expectError: false,
		},
		{
			name: "PDF OCR enabled but empty provider - should fail",
			config: &Config{
				PDFOCREnabled:  true,
				PDFOCRProvider: "",
				PDFOCRModel:    "glm-ocr",
			},
			expectError: true,
			errorMsg:    "PDF OCR provider cannot be empty when PDF OCR is enabled",
		},
		{
			name: "PDF OCR enabled but empty model - should fail",
			config: &Config{
				PDFOCREnabled:  true,
				PDFOCRProvider: "ollama",
				PDFOCRModel:    "",
			},
			expectError: true,
			errorMsg:    "PDF OCR model cannot be empty when PDF OCR is enabled",
		},
		{
			name: "PDF OCR enabled with empty provider and model - should fail",
			config: &Config{
				PDFOCREnabled:  true,
				PDFOCRProvider: "",
				PDFOCRModel:    "",
			},
			expectError: true,
			errorMsg:    "PDF OCR provider cannot be empty when PDF OCR is enabled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestNewConfigIncludesWebScraperPersona(t *testing.T) {
	cfg := NewConfig()
	assert.NotNil(t, cfg.SubagentTypes)

	persona, ok := cfg.SubagentTypes["web_scraper"]
	assert.True(t, ok, "expected web_scraper persona in defaults")
	assert.True(t, persona.Enabled)
	assert.NotEmpty(t, persona.SystemPrompt)
	assert.NotEmpty(t, persona.AllowedTools)
	assert.Contains(t, persona.AllowedTools, "web_search")
	assert.Contains(t, persona.AllowedTools, "fetch_url")
	assert.Contains(t, persona.AllowedTools, "edit_file")
	assert.Contains(t, persona.AllowedTools, "shell_command")
	assert.Contains(t, persona.AllowedTools, "write_structured_file")
	assert.Contains(t, persona.AllowedTools, "patch_structured_file")

	orchestrator, ok := cfg.SubagentTypes["orchestrator"]
	assert.True(t, ok, "expected orchestrator persona in defaults")
	assert.True(t, orchestrator.Enabled)

	coderPersona, ok := cfg.SubagentTypes["coder"]
	assert.True(t, ok, "expected coder persona in defaults")
	assert.True(t, coderPersona.Enabled)
	assert.Contains(t, coderPersona.AllowedTools, "write_structured_file")
	assert.Contains(t, coderPersona.AllowedTools, "patch_structured_file")
	assert.Contains(t, coderPersona.AllowedTools, "browse_url")

	debuggerPersona, ok := cfg.SubagentTypes["debugger"]
	assert.True(t, ok, "expected debugger persona in defaults")
	assert.True(t, debuggerPersona.Enabled)
	assert.Contains(t, debuggerPersona.AllowedTools, "browse_url")

	assert.Contains(t, persona.AllowedTools, "browse_url")

	refactorPersona, ok := cfg.SubagentTypes["refactor"]
	assert.True(t, ok, "expected refactor persona in defaults")
	assert.True(t, refactorPersona.Enabled)
	assert.NotEmpty(t, refactorPersona.SystemPrompt)
	assert.NotEmpty(t, refactorPersona.AllowedTools)
	assert.Contains(t, refactorPersona.AllowedTools, "edit_file")
	assert.Contains(t, refactorPersona.AllowedTools, "write_structured_file")
	assert.Contains(t, refactorPersona.AllowedTools, "patch_structured_file")
	assert.Contains(t, refactorPersona.AllowedTools, "search_files")
}

func TestGetSubagentType_AllowedToolsFromCatalog(t *testing.T) {
	cfg := NewConfig()
	persona := cfg.GetSubagentType("general")
	assert.NotNil(t, persona)
	assert.NotEmpty(t, persona.AllowedTools)
	assert.Contains(t, persona.AllowedTools, "read_file")
}

func TestGetSubagentType_DisabledReturnsNil(t *testing.T) {
	cfg := NewConfig()
	cfg.SetPersonaDisabled("general", true)
	assert.Nil(t, cfg.GetSubagentType("general"))
	cfg.SetPersonaDisabled("general", false)
	assert.NotNil(t, cfg.GetSubagentType("general"))
}

func TestGetDefaultConfigDirPrefersXDGConfigHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg-home")
	t.Setenv("HOME", "/tmp/home-ignored")

	dir, err := getDefaultConfigDir()
	assert.NoError(t, err)
	assert.Equal(t, filepath.Join("/tmp/xdg-home", "sprout"), dir)
}

func TestGetDefaultConfigDirUsesHomeEnvWhenXDGUnset(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/tmp/home-preferred")

	dir, err := getDefaultConfigDir()
	assert.NoError(t, err)
	assert.Equal(t, filepath.Join("/tmp/home-preferred", ".config", "sprout"), dir)
}

func TestGetSubagentMaxParallel(t *testing.T) {
	tests := []struct {
		name     string
		config   *Config
		expected int
	}{
		{
			name: "returns configured value when greater than 0",
			config: &Config{
				SubagentMaxParallel: 5,
			},
			expected: 5,
		},
		{
			name: "returns default 2 when set to 0",
			config: &Config{
				SubagentMaxParallel: 0,
			},
			expected: 2,
		},
		{
			name: "returns default 2 when set to negative value",
			config: &Config{
				SubagentMaxParallel: -1,
			},
			expected: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.GetSubagentMaxParallel()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetSubagentParallelEnabled(t *testing.T) {
	trueVal := true
	falseVal := false

	tests := []struct {
		name     string
		config   *Config
		expected bool
	}{
		{
			name: "returns true when field is explicitly set to true",
			config: &Config{
				SubagentParallelEnabled: &trueVal,
			},
			expected: true,
		},
		{
			name:     "returns false when field is explicitly set to false",
			config:   &Config{SubagentParallelEnabled: &falseVal},
			expected: false,
		},
		{
			name:     "returns true when field not set (default config)",
			config:   &Config{},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.GetSubagentParallelEnabled()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestPersistentContextConfigResolve_NilReturnsDefaults(t *testing.T) {
	var cfg *PersistentContextConfig
	result := cfg.Resolve()

	assert.True(t, result.ProactiveContextEnabled)
	assert.Equal(t, 5, result.MaxContextualResults)
	assert.Equal(t, 0.50, result.MinRelevanceScore)
	assert.Equal(t, 4000, result.MaxContextChars)
	assert.True(t, result.WorkspaceScopedRetrieval, "default is now true (workspace scoping on by default)")
	assert.True(t, result.DriftDetectionEnabled)
	assert.Equal(t, 0.60, result.DriftThreshold)
	assert.Equal(t, 5, result.DriftCheckInterval)
}

func TestPersistentContextConfigResolve_ExplicitValuesPreserved(t *testing.T) {
	cfg := &PersistentContextConfig{
		ProactiveContextEnabled:  false,
		MaxContextualResults:     10,
		MinRelevanceScore:        0.75,
		MaxContextChars:          8000,
		WorkspaceScopedRetrieval: true,
		DriftDetectionEnabled:    false,
		DriftThreshold:           0.80,
		DriftCheckInterval:       10,
	}
	result := cfg.Resolve()

	assert.False(t, result.ProactiveContextEnabled)
	assert.Equal(t, 10, result.MaxContextualResults)
	assert.Equal(t, 0.75, result.MinRelevanceScore)
	assert.Equal(t, 8000, result.MaxContextChars)
	assert.True(t, result.WorkspaceScopedRetrieval)
	assert.False(t, result.DriftDetectionEnabled)
	assert.Equal(t, 0.80, result.DriftThreshold)
	assert.Equal(t, 10, result.DriftCheckInterval)
}

func TestPersistentContextConfigResolve_PartialOverrides(t *testing.T) {
	cfg := &PersistentContextConfig{
		ProactiveContextEnabled:  false,
		MaxContextualResults:     0,   // zero — should get default
		MinRelevanceScore:        0.8, // explicit
		MaxContextChars:          0,   // zero — should get default
		WorkspaceScopedRetrieval: true,
		DriftThreshold:           0.70, // explicit
		DriftCheckInterval:       0,    // zero — should get default
	}
	result := cfg.Resolve()

	assert.False(t, result.ProactiveContextEnabled)
	assert.Equal(t, 5, result.MaxContextualResults) // default
	assert.Equal(t, 0.8, result.MinRelevanceScore)  // explicit
	assert.Equal(t, 4000, result.MaxContextChars)   // default
	assert.True(t, result.WorkspaceScopedRetrieval)
	assert.False(t, result.DriftDetectionEnabled) // false (zero value) treated as explicit
	assert.Equal(t, 0.70, result.DriftThreshold)  // explicit
	assert.Equal(t, 5, result.DriftCheckInterval) // default
}

func TestPersistentContextConfigResolve_DoesNotMutateOriginal(t *testing.T) {
	cfg := &PersistentContextConfig{
		ProactiveContextEnabled:  false,
		MaxContextualResults:     0,
		MinRelevanceScore:        0.8,
		MaxContextChars:          0,
		WorkspaceScopedRetrieval: true,
	}

	// Capture original state
	orig := *cfg

	_ = cfg.Resolve()
	_ = cfg.Resolve() // call multiple times

	assert.Equal(t, orig, *cfg, "original config should not be mutated by Resolve()")
}

// =============================================================================
// PersistentContextConfig RetentionDays tests (SP-033-3c)
// =============================================================================

func TestPersistentContextConfig_Resolve_RetentionDays_Default(t *testing.T) {
	cfg := &PersistentContextConfig{}
	result := cfg.Resolve()

	assert.Equal(t, 0, result.RetentionDays, "RetentionDays should default to 0 (never expire)")
}

func TestPersistentContextConfig_Resolve_RetentionDays_Explicit(t *testing.T) {
	cfg := &PersistentContextConfig{
		RetentionDays: 30,
	}
	result := cfg.Resolve()

	assert.Equal(t, 30, result.RetentionDays, "RetentionDays should preserve explicit value")
}

func TestPersistentContextConfig_Resolve_RetentionDays_Negative(t *testing.T) {
	cfg := &PersistentContextConfig{
		RetentionDays: -1,
	}
	result := cfg.Resolve()

	assert.Equal(t, 0, result.RetentionDays, "Negative RetentionDays should be treated as 0 (never expire)")
}

func TestPersistentContextConfig_JSON_Marshal_Unmarshal_RetentionDays(t *testing.T) {
	cfg := &PersistentContextConfig{
		ProactiveContextEnabled: true,
		RetentionDays:           30,
	}

	data, err := json.Marshal(cfg)
	assert.NoError(t, err)

	// Verify the JSON contains the retentionDays key
	assert.Contains(t, string(data), "retentionDays")
	assert.Contains(t, string(data), "30")

	var decoded PersistentContextConfig
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)
	assert.Equal(t, 30, decoded.RetentionDays, "RetentionDays should survive JSON round-trip")
}

func TestPersistentContextConfig_JSON_OmitsRetentionDaysWhenZero(t *testing.T) {
	cfg := &PersistentContextConfig{
		ProactiveContextEnabled: true,
		RetentionDays:           0,
	}

	data, err := json.Marshal(cfg)
	assert.NoError(t, err)

	// With omitempty, zero RetentionDays should not appear in JSON
	assert.NotContains(t, string(data), "retentionDays",
		"zero RetentionDays should be omitted from JSON due to omitempty tag")
}

// TestPersonaCatalog_Immutable verifies that personas come from the embedded
// catalog and are not mutated by writes to SubagentTypes after construction.
// This is the post-override-removal contract: user code can read SubagentTypes
// but the persistent layer ignores it.
func TestPersonaCatalog_Immutable(t *testing.T) {
	cfg := NewConfig()

	// Mutate a built-in entry in memory.
	general, ok := cfg.SubagentTypes["general"]
	require.True(t, ok, "general persona should exist in catalog")
	general.AllowedTools = []string{"read_file"}
	cfg.SubagentTypes["general"] = general

	// A fresh config should still have the catalog defaults.
	fresh := NewConfig()
	freshGeneral, ok := fresh.SubagentTypes["general"]
	require.True(t, ok)
	assert.Greater(t, len(freshGeneral.AllowedTools), 1,
		"catalog should hydrate full tool list, not be affected by prior mutation")
}

// TestDiscoverProjectSkillsRoundtrip exercises the end-to-end custom-skill
// path that the user owns: drop a SKILL.md under .sprout/skills/<id>/,
// run the configuration boundary that the agent runs at startup, and
// verify the skill survives both the discovery step (gets registered
// into Config.Skills with the right Path + source metadata) and the
// merge/prune step (NOT removed by the built-in cleanup pass).
//
// This is the regression test for the pkg/skills refactor — if the
// prune logic ever generalises its prefix check too aggressively, or
// the discovery layer is rewired through skills.Builtins(), custom
// skills must keep working.
func TestDiscoverProjectSkillsRoundtrip(t *testing.T) {
	tmp := t.TempDir()
	projectSkills := filepath.Join(tmp, ".sprout", "skills", "my-custom")
	require.NoError(t, os.MkdirAll(projectSkills, 0o755))
	skillBody := "---\nname: My Custom\ndescription: A user-supplied test skill.\n---\nbody"
	require.NoError(t, os.WriteFile(filepath.Join(projectSkills, "SKILL.md"), []byte(skillBody), 0o644))

	origWd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	defer os.Chdir(origWd)

	cfg := &Config{}
	discovered := discoverProjectSkills(cfg)
	require.Contains(t, discovered, "My Custom", "discoverProjectSkills should pick up the on-disk skill")

	got, ok := cfg.Skills["my-custom"]
	require.True(t, ok, "Config.Skills should contain the discovered skill")
	assert.Equal(t, "My Custom", got.Name)
	assert.Equal(t, "A user-supplied test skill.", got.Description)
	assert.Equal(t, filepath.Join(".sprout", "skills", "my-custom"), got.Path)
	assert.Equal(t, "project", got.Metadata["source"])
	assert.True(t, got.Enabled)

	// Run the merge+prune step that the real Load() pipeline runs after
	// discovery. The custom skill must survive — its path doesn't match
	// the built-in prefix, and the prune is keyed on that prefix.
	mergeMissingDefaultSkills(cfg)
	stillThere, ok := cfg.Skills["my-custom"]
	require.True(t, ok, "custom skill was pruned by mergeMissingDefaultSkills — built-in prune logic is over-broad")
	assert.Equal(t, "project", stillThere.Metadata["source"], "custom skill metadata was clobbered by merge step")
}

// TestDefaultSkillsCoversEmbeddedLibrary is the cross-package consistency
// gate: every skill shipped under pkg/skills/library/ MUST appear in the
// defaults map that seeds Config.Skills. The whole point of the
// pkg/skills refactor is that there is only one place to register a
// skill (its SKILL.md on disk); if a future change accidentally
// reintroduces a hand-maintained list that drifts from the embedded
// set, this test fails.
func TestDefaultSkillsCoversEmbeddedLibrary(t *testing.T) {
	defaults := defaultSkills()
	embedded := skills.Builtins()
	for id, b := range embedded {
		got, ok := defaults[id]
		if !ok {
			t.Errorf("defaultSkills() missing embedded skill %q — every pkg/skills/library/<id> must register a default", id)
			continue
		}
		if got.Description != b.Description {
			t.Errorf("skill %q description drift: defaults=%q embedded=%q", id, got.Description, b.Description)
		}
		if got.Name != b.Name {
			t.Errorf("skill %q name drift: defaults=%q embedded=%q", id, got.Name, b.Name)
		}
		if !got.Enabled {
			t.Errorf("skill %q registered disabled — built-ins should default to enabled", id)
		}
	}
}

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
			name:     "returns empty when SubagentProvider empty (no fallback)",
			config:   &Config{LastUsedProvider: "deepinfra"},
			expected: "",
		},
		{
			name:     "returns empty with only ProviderPriority set (no fallback)",
			config:   &Config{ProviderPriority: []string{"zai", "openai"}},
			expected: "",
		},
		{
			name:     "returns empty for bare config (no fallback)",
			config:   &Config{},
			expected: "",
		},
		{
			name:     "returns empty with empty ProviderPriority (no fallback)",
			config:   &Config{ProviderPriority: []string{}},
			expected: "",
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

	t.Run("empty model with LastUsedProvider returns empty (no fallback)", func(t *testing.T) {
		cfg := &Config{
			LastUsedProvider: "openai",
			ProviderModels:   map[string]string{"openai": "gpt-5"},
		}
		assert.Equal(t, "", cfg.GetSubagentModel())
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
			id:       "project-planning",
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

	t.Run("updates built-in entries but preserves enabled flag", func(t *testing.T) {
		cfg := &Config{
			Skills: map[string]Skill{
				"project-planning": {ID: "project-planning", Name: "Custom Go", Enabled: false, Path: "pkg/agent/skills/project-planning"},
			},
		}
		mergeMissingDefaultSkills(cfg)
		// Name/description should be updated from current defaults
		defaults := defaultSkills()
		assert.Equal(t, defaults["project-planning"].Name, cfg.Skills["project-planning"].Name)
		// But the user's Enabled preference should be preserved
		assert.False(t, cfg.Skills["project-planning"].Enabled, "user-set Enabled=false should be preserved")
		assert.Equal(t, "builtin", cfg.Skills["project-planning"].Metadata["source"])
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

	t.Run("prunes stale built-in skills no longer in defaults", func(t *testing.T) {
		cfg := &Config{
			Skills: map[string]Skill{
				"go-conventions":   {ID: "go-conventions", Name: "Go Conventions", Path: "pkg/agent/skills/go-conventions", Enabled: true},
				"safe-refactor":    {ID: "safe-refactor", Name: "Safe Refactor", Path: "pkg/agent/skills/safe-refactor", Enabled: true},
				"custom-skill":     {ID: "custom-skill", Name: "My Skill", Path: "~/.config/sprout/skills/custom-skill", Enabled: true},
				"project-planning": {ID: "project-planning", Name: "Project Planning", Path: "pkg/agent/skills/project-planning", Enabled: true},
			},
		}
		mergeMissingDefaultSkills(cfg)

		// Stale built-in skills should be pruned
		_, hasStale1 := cfg.Skills["go-conventions"]
		assert.False(t, hasStale1, "stale built-in go-conventions should be pruned")
		_, hasStale2 := cfg.Skills["safe-refactor"]
		assert.False(t, hasStale2, "stale built-in safe-refactor should be pruned")

		// Current built-in should remain
		_, hasPP := cfg.Skills["project-planning"]
		assert.True(t, hasPP, "current built-in project-planning should remain")

		// User/project skills should never be pruned
		_, hasCustom := cfg.Skills["custom-skill"]
		assert.True(t, hasCustom, "user skill with non-builtin path should never be pruned")
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
		name         string
		content      string
		expectedName string
		expectedDesc string
	}{
		{
			name: "parses name and description from front matter",
			content: `---
name: My Skill
description: Does something useful
---
Some body content here.`,
			expectedName: "My Skill",
			expectedDesc: "Does something useful",
		},
		{
			name: "handles front matter with only name",
			content: `---
name: Just Name
---
Body.`,
			expectedName: "Just Name",
			expectedDesc: "",
		},
		{
			name: "handles front matter with only description",
			content: `---
description: Just Description
---
Body.`,
			expectedName: "",
			expectedDesc: "Just Description",
		},
		{
			name: "handles empty front matter",
			content: `---
---
Body.`,
			expectedName: "",
			expectedDesc: "",
		},
		{
			name: "ignores content outside front matter",
			content: `No front matter here.
name: should not parse
description: should not parse`,
			expectedName: "",
			expectedDesc: "",
		},
		{
			name:         "handles content with no front matter delimiters",
			content:      `Just plain text.`,
			expectedName: "",
			expectedDesc: "",
		},
		{
			name:         "handles empty content",
			content:      "",
			expectedName: "",
			expectedDesc: "",
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
			expectedName: "Skill Name",
			expectedDesc: "Skill Description",
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

func TestNewTestManager_Isolation(t *testing.T) {
	mgr, cleanup := NewTestManager(t)
	defer cleanup()

	// Manager was created successfully.
	require.NotNil(t, mgr)

	// Config comes from the temp directory, not the real user config.
	cfg := mgr.GetConfig()
	require.NotNil(t, cfg)

	// LastUsedProvider starts empty in isolated test configs: the helper
	// deliberately does NOT preload "test" any more — that string used
	// to leak into the user's real config when a test misbehaved. Tests
	// that need a specific provider must set it on the returned mgr.
	assert.Equal(t, "", cfg.LastUsedProvider,
		"isolated test config should start with empty LastUsedProvider")

	// Mutations via UpdateConfigNoSave are visible within the same manager
	// but DO NOT leak to the real config because SPROUT_CONFIG points at the
	// temp dir.
	require.NoError(t, mgr.UpdateConfigNoSave(func(c *Config) error {
		c.LastUsedProvider = "openai"
		return nil
	}))
	assert.Equal(t, "openai", mgr.GetConfig().LastUsedProvider)
}

func TestNewTestManager_DoesNotTouchRealConfig(t *testing.T) {
	// Capture the real config dir before the test.
	realCfgDir, err := GetConfigDir()
	if err != nil {
		t.Skipf("cannot determine real config dir: %v", err)
	}
	realConfigPath := filepath.Join(realCfgDir, ConfigFileName)

	// Snapshot the real config (it may not exist and that's fine).
	realBefore, _ := os.ReadFile(realConfigPath)

	mgr, cleanup := NewTestManager(t)
	defer cleanup()

	// Mutate the isolated config in a way we can detect.
	require.NoError(t, mgr.UpdateConfig(func(c *Config) error {
		c.LastUsedProvider = "zzz-isolated-test-marker-zzz"
		return nil
	}))

	// Re-read the real config — it must be unchanged.
	realAfter, _ := os.ReadFile(realConfigPath)
	assert.Equal(t, string(realBefore), string(realAfter),
		"test must not modify the real user config file")
}

func TestNewTestManager_DoesNotCreateFilesOutsideTempDir(t *testing.T) {
	// After cleanup the temp dir is removed by t.TempDir(), but no files
	// should have been created in the real config location.
	realCfgDir, err := GetConfigDir()
	if err != nil {
		t.Skipf("cannot determine real config dir: %v", err)
	}

	// List files in real config dir before.
	before := listDir(t, realCfgDir)

	_, cleanup := NewTestManager(t)
	cleanup()

	after := listDir(t, realCfgDir)
	assert.Equal(t, before, after,
		"test must not create new files in the real config directory")
}

func listDir(t *testing.T, dir string) map[string]bool {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	m := make(map[string]bool, len(entries))
	for _, e := range entries {
		m[e.Name()] = true
	}
	return m
}
