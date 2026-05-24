package configuration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// MergeRoleConfig tests
// ---------------------------------------------------------------------------

func TestMergeRoleConfig_AllFields(t *testing.T) {
	t.Parallel()

	base := RoleConfig{
		Name:        "base-role",
		Description: "Base description",
		SystemPrompt: "Base prompt",
		Tools: RoleToolsConfig{
			AllowedTools: []string{"read_file", "shell_command"},
			DeniedTools:  []string{"git"},
		},
		Skills: RoleSkillsConfig{
			Skills: []string{"skill-a"},
		},
		Constraints: RoleConstraints{
			MaxIterations: 10,
			MaxTokens:     1000,
			AllowedPaths:  []string{"/base/path"},
		},
		Provider: "openai",
		Model:    "gpt-4",
	}

	override := RoleConfig{
		Name:        "override-role",
		Description: "Override description",
		SystemPrompt: "Override prompt",
		Tools: RoleToolsConfig{
			AllowedTools: []string{"browse_url"},
			DeniedTools:  []string{"write_file"},
		},
		Skills: RoleSkillsConfig{
			Skills: []string{"skill-b"},
		},
		Constraints: RoleConstraints{
			MaxIterations: 20,
			MaxTokens:     2000,
			AllowedPaths:  []string{"/override/path"},
		},
		Provider: "chutes",
		Model:    "claude-3",
	}

	result := MergeRoleConfig(base, override)

	// Name is always from base
	assert.Equal(t, "base-role", result.Name)

	// All other string fields overridden
	assert.Equal(t, "Override description", result.Description)
	assert.Equal(t, "Override prompt", result.SystemPrompt)
	assert.Equal(t, "chutes", result.Provider)
	assert.Equal(t, "claude-3", result.Model)

	// All int fields overridden
	assert.Equal(t, 20, result.Constraints.MaxIterations)
	assert.Equal(t, 2000, result.Constraints.MaxTokens)

	// All slices overridden
	assert.Equal(t, []string{"browse_url"}, result.Tools.AllowedTools)
	assert.Equal(t, []string{"write_file"}, result.Tools.DeniedTools)
	assert.Equal(t, []string{"skill-b"}, result.Skills.Skills)
	assert.Equal(t, []string{"/override/path"}, result.Constraints.AllowedPaths)
}

func TestMergeRoleConfig_PartialOverride(t *testing.T) {
	t.Parallel()

	base := RoleConfig{
		Name:         "base-role",
		Description:  "Base description",
		SystemPrompt: "Base prompt",
		Provider:     "openai",
		Model:        "gpt-4",
		Tools: RoleToolsConfig{
			AllowedTools: []string{"read_file"},
		},
		Skills: RoleSkillsConfig{
			Skills: []string{"skill-a"},
		},
		Constraints: RoleConstraints{
			MaxIterations: 10,
			MaxTokens:     1000,
			AllowedPaths:  []string{"/base/path"},
		},
	}

	// Only override Provider and Model
	override := RoleConfig{
		Name:     "ignored-name",
		Provider: "chutes",
		Model:    "claude-3",
	}

	result := MergeRoleConfig(base, override)

	assert.Equal(t, "base-role", result.Name)
	assert.Equal(t, "Base description", result.Description)
	assert.Equal(t, "Base prompt", result.SystemPrompt)
	assert.Equal(t, "chutes", result.Provider)
	assert.Equal(t, "claude-3", result.Model)
	assert.Equal(t, []string{"read_file"}, result.Tools.AllowedTools)
	assert.Equal(t, []string{"skill-a"}, result.Skills.Skills)
	assert.Equal(t, 10, result.Constraints.MaxIterations)
	assert.Equal(t, 1000, result.Constraints.MaxTokens)
	assert.Equal(t, []string{"/base/path"}, result.Constraints.AllowedPaths)
}

func TestMergeRoleConfig_EmptyOverride(t *testing.T) {
	t.Parallel()

	base := RoleConfig{
		Name:         "base-role",
		Description:  "Base description",
		SystemPrompt: "Base prompt",
		Provider:     "openai",
		Model:        "gpt-4",
		Tools: RoleToolsConfig{
			AllowedTools: []string{"read_file"},
			DeniedTools:  []string{"git"},
		},
		Skills: RoleSkillsConfig{
			Skills: []string{"skill-a"},
		},
		Constraints: RoleConstraints{
			MaxIterations: 10,
			MaxTokens:     1000,
			AllowedPaths:  []string{"/base/path"},
		},
	}

	result := MergeRoleConfig(base, RoleConfig{})

	assert.Equal(t, base.Name, result.Name)
	assert.Equal(t, base.Description, result.Description)
	assert.Equal(t, base.SystemPrompt, result.SystemPrompt)
	assert.Equal(t, base.Provider, result.Provider)
	assert.Equal(t, base.Model, result.Model)
	assert.Equal(t, base.Tools.AllowedTools, result.Tools.AllowedTools)
	assert.Equal(t, base.Tools.DeniedTools, result.Tools.DeniedTools)
	assert.Equal(t, base.Skills.Skills, result.Skills.Skills)
	assert.Equal(t, base.Constraints.MaxIterations, result.Constraints.MaxIterations)
	assert.Equal(t, base.Constraints.MaxTokens, result.Constraints.MaxTokens)
	assert.Equal(t, base.Constraints.AllowedPaths, result.Constraints.AllowedPaths)
}

func TestMergeRoleConfig_NamePreserved(t *testing.T) {
	t.Parallel()

	base := RoleConfig{Name: "original-name"}
	override := RoleConfig{Name: "different-name", Description: "desc"}

	result := MergeRoleConfig(base, override)

	assert.Equal(t, "original-name", result.Name, "base.Name must always be preserved")
	assert.Equal(t, "desc", result.Description)
}

func TestMergeRoleConfig_SliceOverride(t *testing.T) {
	t.Parallel()

	base := RoleConfig{
		Name: "base",
		Tools: RoleToolsConfig{
			AllowedTools: []string{"read_file", "shell_command"},
			DeniedTools:  []string{"git"},
		},
		Skills: RoleSkillsConfig{
			Skills: []string{"skill-a", "skill-b"},
		},
		Constraints: RoleConstraints{
			AllowedPaths: []string{"/base"},
		},
	}

	override := RoleConfig{
		Tools: RoleToolsConfig{
			AllowedTools: []string{"browse_url"},
		},
	}

	result := MergeRoleConfig(base, override)

	// AllowedTools overridden, DeniedTools falls back to base
	assert.Equal(t, []string{"browse_url"}, result.Tools.AllowedTools)
	assert.Equal(t, []string{"git"}, result.Tools.DeniedTools)
	// Skills from base (override has empty/nil)
	assert.Equal(t, []string{"skill-a", "skill-b"}, result.Skills.Skills)
	// AllowedPaths from base
	assert.Equal(t, []string{"/base"}, result.Constraints.AllowedPaths)
}

func TestMergeRoleConfig_NestedStructs(t *testing.T) {
	t.Parallel()

	base := RoleConfig{
		Name: "base",
		Tools: RoleToolsConfig{
			AllowedTools: []string{"read_file"},
			DeniedTools:  []string{"git"},
		},
		Skills: RoleSkillsConfig{
			Skills: []string{"skill-a"},
		},
		Constraints: RoleConstraints{
			MaxIterations: 10,
			MaxTokens:     1000,
			AllowedPaths:  []string{"/base"},
		},
	}

	override := RoleConfig{
		Tools: RoleToolsConfig{
			AllowedTools: []string{"shell_command"},
		},
		Skills: RoleSkillsConfig{
			Skills: []string{"skill-b"},
		},
		Constraints: RoleConstraints{
			MaxTokens: 2000,
		},
	}

	result := MergeRoleConfig(base, override)

	// Tools: AllowedTools overridden, DeniedTools from base
	assert.Equal(t, []string{"shell_command"}, result.Tools.AllowedTools)
	assert.Equal(t, []string{"git"}, result.Tools.DeniedTools)

	// Skills: fully overridden
	assert.Equal(t, []string{"skill-b"}, result.Skills.Skills)

	// Constraints: MaxTokens overridden, MaxIterations and AllowedPaths from base
	assert.Equal(t, 10, result.Constraints.MaxIterations)
	assert.Equal(t, 2000, result.Constraints.MaxTokens)
	assert.Equal(t, []string{"/base"}, result.Constraints.AllowedPaths)
}

func TestMergeRoleConfig_WhitespaceOnlyOverride(t *testing.T) {
	t.Parallel()

	base := RoleConfig{
		Name:         "base",
		Description:  "Base desc",
		SystemPrompt: "Base prompt",
		Provider:     "openai",
		Model:        "gpt-4",
	}

	override := RoleConfig{
		Description:  "   ", // whitespace only
		SystemPrompt: "",
		Provider:     "  ",  // whitespace only
		Model:        "claude",
	}

	result := MergeRoleConfig(base, override)

	// Whitespace-only strings should fall back to base
	assert.Equal(t, "Base desc", result.Description)
	assert.Equal(t, "Base prompt", result.SystemPrompt)
	assert.Equal(t, "openai", result.Provider)
	// Non-whitespace override should be used
	assert.Equal(t, "claude", result.Model)
}

func TestMergeRoleConfig_IntOverride(t *testing.T) {
	t.Parallel()

	base := RoleConfig{
		Constraints: RoleConstraints{
			MaxIterations: 10,
			MaxTokens:     1000,
		},
	}

	override := RoleConfig{
		Constraints: RoleConstraints{
			MaxIterations: 0, // zero should fall back to base
			MaxTokens:     2000,
		},
	}

	result := MergeRoleConfig(base, override)

	assert.Equal(t, 10, result.Constraints.MaxIterations) // fallback to base
	assert.Equal(t, 2000, result.Constraints.MaxTokens)     // overridden
}

func TestMergeRoleConfig_EmptySliceVsNilSlice(t *testing.T) {
	t.Parallel()

	base := RoleConfig{
		Name: "base",
		Tools: RoleToolsConfig{
			AllowedTools: []string{"read_file"},
		},
	}

	// Explicitly empty slice (length 0) should fall back to base
	override := RoleConfig{
		Tools: RoleToolsConfig{
			AllowedTools: []string{},
		},
	}

	result := MergeRoleConfig(base, override)
	assert.Equal(t, []string{"read_file"}, result.Tools.AllowedTools)

	// Nil slice should also fall back to base
	override2 := RoleConfig{
		Tools: RoleToolsConfig{
			AllowedTools: nil,
		},
	}

	result2 := MergeRoleConfig(base, override2)
	assert.Equal(t, []string{"read_file"}, result2.Tools.AllowedTools)
}

// ---------------------------------------------------------------------------
// coalesce helper function tests
// ---------------------------------------------------------------------------

func TestCoalesceString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		a        string
		b        string
		expected string
	}{
		{"both non-empty", "hello", "world", "hello"},
		{"a empty", "", "world", "world"},
		{"a whitespace", "  ", "world", "world"},
		{"both empty", "", "", ""},
		{"a has value b empty", "hello", "", "hello"},
		{"newlines only", "\n\t  ", "fallback", "fallback"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := coalesceString(tc.a, tc.b)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestCoalesceInt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		a        int
		b        int
		expected int
	}{
		{"both non-zero", 5, 10, 5},
		{"a zero", 0, 10, 10},
		{"both zero", 0, 0, 0},
		{"a non-zero b zero", 5, 0, 5},
		{"negative a", -1, 10, -1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := coalesceInt(tc.a, tc.b)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestCoalesceStrings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		a        []string
		b        []string
		expected []string
	}{
		{"both non-empty", []string{"a", "b"}, []string{"c", "d"}, []string{"a", "b"}},
		{"a empty slice", []string{}, []string{"c", "d"}, []string{"c", "d"}},
		{"a nil", nil, []string{"c", "d"}, []string{"c", "d"}},
		{"both empty", []string{}, []string{}, []string{}},
		{"both nil", nil, nil, nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := coalesceStrings(tc.a, tc.b)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// ---------------------------------------------------------------------------
// RoleFileFromPath tests
// ---------------------------------------------------------------------------

func TestRoleFileFromPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{"simple yaml file", "/home/user/roles/coder.yaml", "coder"},
		{"simple yaml file relative", "roles/coder.yaml", "coder"},
		{"just filename", "myrole.yaml", "myrole"},
		{"no extension", "myrole.txt", "myrole.txt"},
		{"nested path", "/home/user/.config/sprout/roles/deep/debugger.yaml", "debugger"},
		{"complex name", "test-role_v2.yaml", "test-role_v2"},
		{"empty path", "", "."},
		{"only yaml", "role.yaml", "role"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := RoleFileFromPath(tc.path)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// ---------------------------------------------------------------------------
// LoadRoleFromFile / SaveRoleToFile tests
// ---------------------------------------------------------------------------

func TestLoadRoleFromFile_InvalidPath(t *testing.T) {
	t.Parallel()

	_, err := LoadRoleFromFile("/nonexistent/path/role.yaml")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "read role file")
}

func TestLoadRoleFromFile_MalformedYAML(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "bad.yaml")
	err := os.WriteFile(path, []byte(":\n  - invalid: yaml::: content"), 0600)
	require.NoError(t, err)

	_, err = LoadRoleFromFile(path)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parse role file")
}

func TestLoadRoleFromFile_ValidFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "coder.yaml")
	yamlContent := `name: coder
description: A coding role
system_prompt: You are a helpful coding assistant.
provider: openai
model: gpt-4
tools:
  allowed_tools:
    - read_file
    - write_file
  denied_tools:
    - shell_command
skills:
  skills:
    - project-planning
constraints:
  max_iterations: 20
  max_tokens: 4000
  allowed_paths:
    - /src
    - /test
`
	err := os.WriteFile(path, []byte(yamlContent), 0600)
	require.NoError(t, err)

	cfg, err := LoadRoleFromFile(path)
	require.NoError(t, err)

	assert.Equal(t, "coder", cfg.Name)
	assert.Equal(t, "A coding role", cfg.Description)
	assert.Equal(t, "You are a helpful coding assistant.", cfg.SystemPrompt)
	assert.Equal(t, "openai", cfg.Provider)
	assert.Equal(t, "gpt-4", cfg.Model)
	assert.Equal(t, []string{"read_file", "write_file"}, cfg.Tools.AllowedTools)
	assert.Equal(t, []string{"shell_command"}, cfg.Tools.DeniedTools)
	assert.Equal(t, []string{"project-planning"}, cfg.Skills.Skills)
	assert.Equal(t, 20, cfg.Constraints.MaxIterations)
	assert.Equal(t, 4000, cfg.Constraints.MaxTokens)
	assert.Equal(t, []string{"/src", "/test"}, cfg.Constraints.AllowedPaths)
}

func TestLoadRoleFromFile_DeriveNameFromFilename(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "my-special-role.yaml")
	// YAML with no name field
	yamlContent := `description: A role without a name field
`
	err := os.WriteFile(path, []byte(yamlContent), 0600)
	require.NoError(t, err)

	cfg, err := LoadRoleFromFile(path)
	require.NoError(t, err)

	assert.Equal(t, "my-special-role", cfg.Name)
	assert.Equal(t, "A role without a name field", cfg.Description)
}

func TestLoadRoleFromFile_WhitespaceNameDerivesFromFilename(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "fallback-name.yaml")
	yamlContent := `name:   
description: Has whitespace-only name
`
	err := os.WriteFile(path, []byte(yamlContent), 0600)
	require.NoError(t, err)

	cfg, err := LoadRoleFromFile(path)
	require.NoError(t, err)

	assert.Equal(t, "fallback-name", cfg.Name)
}

func TestSaveRoleToFile_EmptyName(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	err := SaveRoleToFile(tmpDir, RoleConfig{Name: "   "})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "role name cannot be empty")
}

func TestSaveRoleToFile_Success(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	cfg := RoleConfig{
		Name:         "test-role",
		Description:  "A test role",
		SystemPrompt: "You are a test assistant.",
		Provider:     "openai",
		Model:        "gpt-4",
		Tools: RoleToolsConfig{
			AllowedTools: []string{"read_file"},
		},
	}

	err := SaveRoleToFile(tmpDir, cfg)
	require.NoError(t, err)

	// Verify file was created
	path := filepath.Join(tmpDir, "test-role.yaml")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "name: test-role")
	assert.Contains(t, string(data), "description: A test role")

	// Verify it can be parsed back
	loaded, err := LoadRoleFromFile(path)
	require.NoError(t, err)
	assert.Equal(t, "test-role", loaded.Name)
	assert.Equal(t, "A test role", loaded.Description)
	assert.Equal(t, "gpt-4", loaded.Model)
}

func TestSaveRoleToFile_CreatesDirectory(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	nestedDir := filepath.Join(tmpDir, "nested", "roles")

	cfg := RoleConfig{
		Name: "nested-role",
	}

	err := SaveRoleToFile(nestedDir, cfg)
	require.NoError(t, err)

	// Verify file was created in the nested directory
	path := filepath.Join(nestedDir, "nested-role.yaml")
	_, err = os.Stat(path)
	assert.NoError(t, err)
}

// ---------------------------------------------------------------------------
// RoleMetaFromFile tests
// ---------------------------------------------------------------------------

func TestRoleMetaFromFile_NotFound(t *testing.T) {
	t.Parallel()

	_, err := RoleMetaFromFile("/nonexistent/role.yaml", "global")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "stat role file")
}

func TestRoleMetaFromFile_ValidFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "my-role.yaml")
	err := os.WriteFile(path, []byte("name: my-role"), 0600)
	require.NoError(t, err)

	meta, err := RoleMetaFromFile(path, "global")
	require.NoError(t, err)

	assert.Equal(t, "my-role", meta.Name)
	assert.Equal(t, "global", meta.Source)
	assert.NotEmpty(t, meta.CreatedAt)
	assert.NotEmpty(t, meta.UpdatedAt)
}

func TestRoleMetaFromFile_WorkspaceSource(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "ws-role.yaml")
	err := os.WriteFile(path, []byte("name: ws-role"), 0600)
	require.NoError(t, err)

	meta, err := RoleMetaFromFile(path, "workspace")
	require.NoError(t, err)

	assert.Equal(t, "ws-role", meta.Name)
	assert.Equal(t, "workspace", meta.Source)
}

// ---------------------------------------------------------------------------
// EncodeRoleToYAML / DecodeRoleFromYAML tests
// ---------------------------------------------------------------------------

func TestEncodeDecodeRoundTrip(t *testing.T) {
	t.Parallel()

	original := RoleConfig{
		Name:         "roundtrip-role",
		Description:  "Test round trip",
		SystemPrompt: "You are a round trip tester.",
		Provider:     "openai",
		Model:        "gpt-4",
		Tools: RoleToolsConfig{
			AllowedTools: []string{"read_file", "write_file"},
			DeniedTools:  []string{"shell_command"},
		},
		Skills: RoleSkillsConfig{
			Skills: []string{"skill-a"},
		},
		Constraints: RoleConstraints{
			MaxIterations: 10,
			MaxTokens:     1000,
			AllowedPaths:  []string{"/path"},
		},
	}

	data, err := EncodeRoleToYAML(original)
	require.NoError(t, err)

	decoded, err := DecodeRoleFromYAML(data)
	require.NoError(t, err)

	assert.Equal(t, original.Name, decoded.Name)
	assert.Equal(t, original.Description, decoded.Description)
	assert.Equal(t, original.Provider, decoded.Provider)
	assert.Equal(t, original.Model, decoded.Model)
	assert.Equal(t, original.Tools.AllowedTools, decoded.Tools.AllowedTools)
	assert.Equal(t, original.Tools.DeniedTools, decoded.Tools.DeniedTools)
	assert.Equal(t, original.Skills.Skills, decoded.Skills.Skills)
	assert.Equal(t, original.Constraints.MaxIterations, decoded.Constraints.MaxIterations)
	assert.Equal(t, original.Constraints.MaxTokens, decoded.Constraints.MaxTokens)
	assert.Equal(t, original.Constraints.AllowedPaths, decoded.Constraints.AllowedPaths)
}

func TestDecodeRoleFromYAML_InvalidYAML(t *testing.T) {
	t.Parallel()

	_, err := DecodeRoleFromYAML([]byte(":\n  - bad yaml:::"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "decode role YAML")
}

func TestDecodeRoleFromYAML_EmptyData(t *testing.T) {
	t.Parallel()

	cfg, err := DecodeRoleFromYAML([]byte(""))
	require.NoError(t, err)
	assert.NotNil(t, cfg)
	assert.Equal(t, RoleConfig{}, *cfg)
}

func TestEncodeRoleToYAML_HumanReadable(t *testing.T) {
	t.Parallel()

	cfg := RoleConfig{
		Name:        "human-readable",
		Description: "A role with all fields",
		Provider:    "openai",
		Model:       "gpt-4",
	}

	data, err := EncodeRoleToYAML(cfg)
	require.NoError(t, err)

	yamlStr := string(data)
	// Verify it contains expected YAML keys
	assert.Contains(t, yamlStr, "name: human-readable")
	assert.Contains(t, yamlStr, "description: A role with all fields")
	assert.Contains(t, yamlStr, "provider: openai")
	assert.Contains(t, yamlStr, "model: gpt-4")
}

// ---------------------------------------------------------------------------
// RoleManager tests
// ---------------------------------------------------------------------------

// setupRoleManager creates a RoleManager with temp directories for testing.
func setupRoleManager(t *testing.T, withWorkspace bool) (*RoleManager, string, string) {
	t.Helper()

	globalDir := t.TempDir()
	var workspaceDir string
	if withWorkspace {
		workspaceDir = t.TempDir()
	}

	rm := NewRoleManager(globalDir, workspaceDir)
	return rm, globalDir, workspaceDir
}

func writeRoleFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name+".yaml")
	err := os.WriteFile(path, []byte(content), 0600)
	require.NoError(t, err)
}

// --- Resolve tests ---

func TestRoleManager_ResolveWorkspaceFirst(t *testing.T) {
	t.Parallel()

	rm, globalDir, workspaceDir := setupRoleManager(t, true)

	// Write same role in both dirs with different content
	writeRoleFile(t, globalDir, "coder", `name: coder
description: Global coder
provider: openai
model: gpt-4
`)
	writeRoleFile(t, workspaceDir, "coder", `name: coder
description: Workspace coder
provider: chutes
`)

	cfg, err := rm.Resolve("coder")
	require.NoError(t, err)

	// Workspace overrides global
	assert.Equal(t, "Workspace coder", cfg.Description)
	assert.Equal(t, "chutes", cfg.Provider)
	// Non-overridden fields come from global
	assert.Equal(t, "gpt-4", cfg.Model)
}

func TestRoleManager_ResolveGlobalOnly(t *testing.T) {
	t.Parallel()

	rm, globalDir, _ := setupRoleManager(t, true)

	writeRoleFile(t, globalDir, "reviewer", `name: reviewer
description: Code reviewer
provider: openai
model: gpt-3.5
`)

	cfg, err := rm.Resolve("reviewer")
	require.NoError(t, err)

	assert.Equal(t, "reviewer", cfg.Name)
	assert.Equal(t, "Code reviewer", cfg.Description)
	assert.Equal(t, "gpt-3.5", cfg.Model)
}

func TestRoleManager_ResolveWorkspaceOnly(t *testing.T) {
	t.Parallel()

	rm, _, workspaceDir := setupRoleManager(t, true)

	writeRoleFile(t, workspaceDir, "debugger", `name: debugger
description: Debug specialist
provider: chutes
`)

	cfg, err := rm.Resolve("debugger")
	require.NoError(t, err)

	assert.Equal(t, "debugger", cfg.Name)
	assert.Equal(t, "Debug specialist", cfg.Description)
	assert.Equal(t, "chutes", cfg.Provider)
}

func TestRoleManager_ResolveNotFound(t *testing.T) {
	t.Parallel()

	rm, _, _ := setupRoleManager(t, true)

	_, err := rm.Resolve("nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestRoleManager_ResolveEmptyName(t *testing.T) {
	t.Parallel()

	rm, _, _ := setupRoleManager(t, true)

	_, err := rm.Resolve("")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "role name cannot be empty")
}

func TestRoleManager_ResolveWhitespaceName(t *testing.T) {
	t.Parallel()

	rm, _, _ := setupRoleManager(t, true)

	_, err := rm.Resolve("   ")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "role name cannot be empty")
}

func TestRoleManager_ResolveCaseInsensitive(t *testing.T) {
	t.Parallel()

	rm, globalDir, _ := setupRoleManager(t, true)

	writeRoleFile(t, globalDir, "coder", `name: coder
description: A coder
`)

	// Try resolving with different cases
	for _, name := range []string{"coder", "CODER", "Coder", "CoDeR"} {
		t.Run(name, func(t *testing.T) {
			cfg, err := rm.Resolve(name)
			require.NoError(t, err)
			assert.Equal(t, "coder", cfg.Name)
		})
	}
}

func TestRoleManager_ResolveGlobalOnlyMode(t *testing.T) {
	t.Parallel()

	rm, globalDir, _ := setupRoleManager(t, false)

	writeRoleFile(t, globalDir, "tester", `name: tester
description: Test automation role
provider: openai
`)

	cfg, err := rm.Resolve("tester")
	require.NoError(t, err)

	assert.Equal(t, "tester", cfg.Name)
	assert.Equal(t, "Test automation role", cfg.Description)
}

// --- Save tests ---

func TestRoleManager_SaveWorkspace(t *testing.T) {
	t.Parallel()

	rm, _, workspaceDir := setupRoleManager(t, true)

	cfg := RoleConfig{
		Name:        "new-role",
		Description: "A new workspace role",
		Provider:    "chutes",
	}

	err := rm.Save(cfg, "workspace")
	require.NoError(t, err)

	// Verify file exists in workspace dir
	path := filepath.Join(workspaceDir, "new-role.yaml")
	_, err = os.Stat(path)
	assert.NoError(t, err)

	// Verify it can be loaded back
	loaded, err := LoadRoleFromFile(path)
	require.NoError(t, err)
	assert.Equal(t, "new-role", loaded.Name)
	assert.Equal(t, "A new workspace role", loaded.Description)
}

func TestRoleManager_SaveToGlobal(t *testing.T) {
	t.Parallel()

	rm, globalDir, _ := setupRoleManager(t, false)

	cfg := RoleConfig{
		Name:        "global-role",
		Description: "A global role",
	}

	err := rm.Save(cfg, "global")
	require.NoError(t, err)

	path := filepath.Join(globalDir, "global-role.yaml")
	_, err = os.Stat(path)
	assert.NoError(t, err)
}

func TestRoleManager_SaveSavesToWorkspaceWhenDirSet(t *testing.T) {
	t.Parallel()

	rm, _, workspaceDir := setupRoleManager(t, true)

	// When workspaceDir is set, Save always writes to workspace
	// even when source is "global" (this is the documented behavior)
	cfg := RoleConfig{
		Name:        "workspace-role",
		Description: "Saved to workspace",
	}

	err := rm.Save(cfg, "global")
	require.NoError(t, err)

	// Should be in workspace dir because workspaceDir is set
	path := filepath.Join(workspaceDir, "workspace-role.yaml")
	_, err = os.Stat(path)
	assert.NoError(t, err)
}

func TestRoleManager_SaveEmptyName(t *testing.T) {
	t.Parallel()

	rm, _, _ := setupRoleManager(t, false)

	cfg := RoleConfig{Name: "   "}
	err := rm.Save(cfg, "global")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "role name cannot be empty")
}

// --- Delete tests ---

func TestRoleManager_DeleteGlobal(t *testing.T) {
	t.Parallel()

	rm, globalDir, _ := setupRoleManager(t, true)

	writeRoleFile(t, globalDir, "to-delete", `name: to-delete`)

	err := rm.Delete("to-delete")
	require.NoError(t, err)

	// Verify it's gone from global
	path := filepath.Join(globalDir, "to-delete.yaml")
	_, err = os.Stat(path)
	assert.True(t, os.IsNotExist(err))

	// Verify it no longer resolves
	_, err = rm.Resolve("to-delete")
	assert.Error(t, err)
}

func TestRoleManager_DeleteWorkspace(t *testing.T) {
	t.Parallel()

	rm, _, workspaceDir := setupRoleManager(t, true)

	writeRoleFile(t, workspaceDir, "ws-delete", `name: ws-delete`)

	err := rm.Delete("ws-delete")
	require.NoError(t, err)

	path := filepath.Join(workspaceDir, "ws-delete.yaml")
	_, err = os.Stat(path)
	assert.True(t, os.IsNotExist(err))
}

func TestRoleManager_DeleteFromBoth(t *testing.T) {
	t.Parallel()

	rm, globalDir, workspaceDir := setupRoleManager(t, true)

	// Create in both dirs
	writeRoleFile(t, globalDir, "both", `name: both
description: Global version
`)
	writeRoleFile(t, workspaceDir, "both", `name: both
description: Workspace version
`)

	err := rm.Delete("both")
	require.NoError(t, err)

	// Both should be deleted (workspace tried first, then global)
	_, wsErr := os.Stat(filepath.Join(workspaceDir, "both.yaml"))
	assert.True(t, os.IsNotExist(wsErr))
	_, globErr := os.Stat(filepath.Join(globalDir, "both.yaml"))
	assert.True(t, os.IsNotExist(globErr))
}

func TestRoleManager_DeleteNotFound(t *testing.T) {
	t.Parallel()

	rm, _, _ := setupRoleManager(t, true)

	err := rm.Delete("does-not-exist")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestRoleManager_DeleteEmptyName(t *testing.T) {
	t.Parallel()

	rm, _, _ := setupRoleManager(t, true)

	err := rm.Delete("")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "role name cannot be empty")
}

func TestRoleManager_DeleteWhitespaceName(t *testing.T) {
	t.Parallel()

	rm, _, _ := setupRoleManager(t, true)

	err := rm.Delete("   ")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "role name cannot be empty")
}

// --- List tests ---

func TestRoleManager_ListEmpty(t *testing.T) {
	t.Parallel()

	rm, _, _ := setupRoleManager(t, true)

	result, err := rm.List()
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestRoleManager_ListGlobalOnly(t *testing.T) {
	t.Parallel()

	rm, globalDir, _ := setupRoleManager(t, true)

	writeRoleFile(t, globalDir, "alpha", `name: alpha`)
	writeRoleFile(t, globalDir, "beta", `name: beta`)

	result, err := rm.List()
	require.NoError(t, err)

	names := extractNames(result)
	assert.Len(t, names, 2)
	assert.Contains(t, names, "alpha")
	assert.Contains(t, names, "beta")

	// All should have "global" source
	for _, meta := range result {
		assert.Equal(t, "global", meta.Source)
	}
}

func TestRoleManager_ListWorkspaceOnly(t *testing.T) {
	t.Parallel()

	rm, _, workspaceDir := setupRoleManager(t, true)

	writeRoleFile(t, workspaceDir, "gamma", `name: gamma`)

	result, err := rm.List()
	require.NoError(t, err)

	assert.Len(t, result, 1)
	assert.Equal(t, "gamma", result[0].Name)
	assert.Equal(t, "workspace", result[0].Source)
}

func TestRoleManager_ListWorkspaceOverridesGlobal(t *testing.T) {
	t.Parallel()

	rm, globalDir, workspaceDir := setupRoleManager(t, true)

	// Role exists in both dirs
	writeRoleFile(t, globalDir, "shared", `name: shared`)
	writeRoleFile(t, workspaceDir, "shared", `name: shared`)

	// Unique to global
	writeRoleFile(t, globalDir, "global-only", `name: global-only`)

	// Unique to workspace
	writeRoleFile(t, workspaceDir, "ws-only", `name: ws-only`)

	result, err := rm.List()
	require.NoError(t, err)

	names := extractNames(result)
	assert.Len(t, names, 3)

	// Verify sources
	for _, meta := range result {
		switch meta.Name {
		case "shared":
			assert.Equal(t, "workspace", meta.Source, "shared role should show workspace source")
		case "global-only":
			assert.Equal(t, "global", meta.Source)
		case "ws-only":
			assert.Equal(t, "workspace", meta.Source)
		}
	}
}

func TestRoleManager_ListIgnoresNonYAMLFiles(t *testing.T) {
	t.Parallel()

	rm, globalDir, _ := setupRoleManager(t, true)

	writeRoleFile(t, globalDir, "valid", `name: valid`)
	err := os.WriteFile(filepath.Join(globalDir, "readme.txt"), []byte("not a role"), 0600)
	require.NoError(t, err)

	result, err := rm.List()
	require.NoError(t, err)

	assert.Len(t, result, 1)
	assert.Equal(t, "valid", result[0].Name)
}

func TestRoleManager_ListGlobalOnlyMode(t *testing.T) {
	t.Parallel()

	rm, globalDir, _ := setupRoleManager(t, false)

	writeRoleFile(t, globalDir, "solo", `name: solo`)

	result, err := rm.List()
	require.NoError(t, err)

	assert.Len(t, result, 1)
	assert.Equal(t, "solo", result[0].Name)
	assert.Equal(t, "global", result[0].Source)
}

// --- Exists tests ---

func TestRoleManager_ExistsGlobal(t *testing.T) {
	t.Parallel()

	rm, globalDir, _ := setupRoleManager(t, true)

	writeRoleFile(t, globalDir, "existing", `name: existing`)

	assert.True(t, rm.Exists("existing"))
	assert.False(t, rm.Exists("nonexistent"))
}

func TestRoleManager_ExistsWorkspace(t *testing.T) {
	t.Parallel()

	rm, _, workspaceDir := setupRoleManager(t, true)

	writeRoleFile(t, workspaceDir, "ws-existing", `name: ws-existing`)

	assert.True(t, rm.Exists("ws-existing"))
	assert.False(t, rm.Exists("nonexistent"))
}

func TestRoleManager_ExistsCaseInsensitive(t *testing.T) {
	t.Parallel()

	rm, globalDir, _ := setupRoleManager(t, true)

	writeRoleFile(t, globalDir, "coder", `name: coder`)

	assert.True(t, rm.Exists("coder"))
	assert.True(t, rm.Exists("CODER"))
	assert.True(t, rm.Exists("Coder"))
}

func TestRoleManager_ExistsEmptyName(t *testing.T) {
	t.Parallel()

	rm, _, _ := setupRoleManager(t, true)

	assert.False(t, rm.Exists(""))
	assert.False(t, rm.Exists("   "))
}

func TestRoleManager_ExistsGlobalOnlyMode(t *testing.T) {
	t.Parallel()

	rm, globalDir, _ := setupRoleManager(t, false)

	writeRoleFile(t, globalDir, "solo", `name: solo`)

	assert.True(t, rm.Exists("solo"))
	assert.False(t, rm.Exists("missing"))
}

// --- LoadRaw tests ---

func TestRoleManager_LoadRawGlobal(t *testing.T) {
	t.Parallel()

	rm, globalDir, _ := setupRoleManager(t, true)

	writeRoleFile(t, globalDir, "raw-role", `name: raw-role
description: Raw global role
provider: openai
model: gpt-4
`)

	cfg, err := rm.LoadRaw("raw-role", "global")
	require.NoError(t, err)

	assert.Equal(t, "raw-role", cfg.Name)
	assert.Equal(t, "Raw global role", cfg.Description)
	assert.Equal(t, "openai", cfg.Provider)
}

func TestRoleManager_LoadRawWorkspace(t *testing.T) {
	t.Parallel()

	rm, _, workspaceDir := setupRoleManager(t, true)

	writeRoleFile(t, workspaceDir, "raw-ws", `name: raw-ws
description: Raw workspace role
provider: chutes
`)

	cfg, err := rm.LoadRaw("raw-ws", "workspace")
	require.NoError(t, err)

	assert.Equal(t, "raw-ws", cfg.Name)
	assert.Equal(t, "Raw workspace role", cfg.Description)
	assert.Equal(t, "chutes", cfg.Provider)
}

func TestRoleManager_LoadRawNotFound(t *testing.T) {
	t.Parallel()

	rm, _, _ := setupRoleManager(t, true)

	_, err := rm.LoadRaw("missing", "global")
	assert.Error(t, err)
}

func TestRoleManager_LoadRawEmptyName(t *testing.T) {
	t.Parallel()

	rm, _, _ := setupRoleManager(t, true)

	_, err := rm.LoadRaw("", "global")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "role name cannot be empty")
}

func TestRoleManager_LoadRawInvalidSource(t *testing.T) {
	t.Parallel()

	rm, _, _ := setupRoleManager(t, true)

	_, err := rm.LoadRaw("any-role", "invalid-source")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid source")
}

func TestRoleManager_LoadRawWorkspaceWithoutWorkspaceDir(t *testing.T) {
	t.Parallel()

	rm, _, _ := setupRoleManager(t, false)

	_, err := rm.LoadRaw("any-role", "workspace")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no workspace directory configured")
}

// --- GlobalDir / WorkspaceDir accessor tests ---

func TestRoleManager_DirAccessors(t *testing.T) {
	t.Parallel()

	globalDir := t.TempDir()
	workspaceDir := t.TempDir()

	rm := NewRoleManager(globalDir, workspaceDir)

	assert.Equal(t, globalDir, rm.GlobalDir())
	assert.Equal(t, workspaceDir, rm.WorkspaceDir())
}

func TestRoleManager_DirAccessorsNoWorkspace(t *testing.T) {
	t.Parallel()

	globalDir := t.TempDir()

	rm := NewRoleManager(globalDir, "")

	assert.Equal(t, globalDir, rm.GlobalDir())
	assert.Equal(t, "", rm.WorkspaceDir())
}

// --- Role file format test ---

func TestRoleManager_RoleFileFormat(t *testing.T) {
	t.Parallel()

	rm, _, workspaceDir := setupRoleManager(t, true)

	cfg := RoleConfig{
		Name:         "format-test",
		Description:  "A role for testing YAML format",
		SystemPrompt: "You are a helpful assistant that tests file formats.",
		Provider:     "openai",
		Model:        "gpt-4",
		Tools: RoleToolsConfig{
			AllowedTools: []string{"read_file", "write_file", "shell_command"},
			DeniedTools:  []string{"git"},
		},
		Skills: RoleSkillsConfig{
			Skills: []string{"project-planning", "browse-debugging"},
		},
		Constraints: RoleConstraints{
			MaxIterations: 50,
			MaxTokens:     8000,
			AllowedPaths:  []string{"/src", "/test", "/docs"},
		},
	}

	err := rm.Save(cfg, "workspace")
	require.NoError(t, err)

	// Read the raw file content
	path := filepath.Join(workspaceDir, "format-test.yaml")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	yamlContent := string(data)

	// Verify human-readable YAML format
	assert.Contains(t, yamlContent, "name: format-test")
	assert.Contains(t, yamlContent, "description: A role for testing YAML format")
	assert.Contains(t, yamlContent, "provider: openai")
	assert.Contains(t, yamlContent, "model: gpt-4")
	assert.Contains(t, yamlContent, "max_iterations: 50")
	assert.Contains(t, yamlContent, "max_tokens: 8000")

	// Verify it has YAML list syntax for slices
	assert.Contains(t, yamlContent, "allowed_tools")
	assert.Contains(t, yamlContent, "- read_file")

	// Verify it can be round-tripped
	loaded, err := LoadRoleFromFile(path)
	require.NoError(t, err)
	assert.Equal(t, cfg.Name, loaded.Name)
	assert.Equal(t, cfg.Description, loaded.Description)
	assert.Equal(t, cfg.Provider, loaded.Provider)
	assert.Equal(t, cfg.Model, loaded.Model)
	assert.Equal(t, cfg.Tools.AllowedTools, loaded.Tools.AllowedTools)
	assert.Equal(t, cfg.Tools.DeniedTools, loaded.Tools.DeniedTools)
	assert.Equal(t, cfg.Skills.Skills, loaded.Skills.Skills)
	assert.Equal(t, cfg.Constraints.MaxIterations, loaded.Constraints.MaxIterations)
	assert.Equal(t, cfg.Constraints.MaxTokens, loaded.Constraints.MaxTokens)
	assert.Equal(t, cfg.Constraints.AllowedPaths, loaded.Constraints.AllowedPaths)
}

// --- Save + Resolve integration tests ---

func TestRoleManager_SaveThenResolve(t *testing.T) {
	t.Parallel()

	rm, _, _ := setupRoleManager(t, true)

	cfg := RoleConfig{
		Name:         "int-role",
		Description:  "Integration test role",
		Provider:     "chutes",
		Model:        "claude-3",
	}

	err := rm.Save(cfg, "workspace")
	require.NoError(t, err)

	resolved, err := rm.Resolve("int-role")
	require.NoError(t, err)

	assert.Equal(t, "int-role", resolved.Name)
	assert.Equal(t, "Integration test role", resolved.Description)
	assert.Equal(t, "chutes", resolved.Provider)
	assert.Equal(t, "claude-3", resolved.Model)
}

func TestRoleManager_UpdateRole(t *testing.T) {
	t.Parallel()

	rm, _, _ := setupRoleManager(t, true)

	// Initial save
	err := rm.Save(RoleConfig{Name: "updatable", Description: "v1"}, "workspace")
	require.NoError(t, err)

	// Verify initial state
	cfg1, err := rm.Resolve("updatable")
	require.NoError(t, err)
	assert.Equal(t, "v1", cfg1.Description)

	// Update
	err = rm.Save(RoleConfig{Name: "updatable", Description: "v2", Provider: "chutes"}, "workspace")
	require.NoError(t, err)

	// Verify updated state
	cfg2, err := rm.Resolve("updatable")
	require.NoError(t, err)
	assert.Equal(t, "v2", cfg2.Description)
	assert.Equal(t, "chutes", cfg2.Provider)
}

// --- Merge on resolve integration ---

func TestRoleManager_ResolveMergeCombinesFields(t *testing.T) {
	t.Parallel()

	rm, globalDir, workspaceDir := setupRoleManager(t, true)

	// Global has full definition
	writeRoleFile(t, globalDir, "merge-role", `name: merge-role
description: Base role
system_prompt: You are a base assistant.
provider: openai
model: gpt-4
tools:
  allowed_tools:
    - read_file
    - write_file
  denied_tools:
    - shell_command
skills:
  skills:
    - project-planning
constraints:
  max_iterations: 10
  max_tokens: 1000
  allowed_paths:
    - /src
`)

	// Workspace has partial overrides
	writeRoleFile(t, workspaceDir, "merge-role", `name: merge-role
provider: chutes
model: claude-3
`)

	result, err := rm.Resolve("merge-role")
	require.NoError(t, err)

	// Overridden fields
	assert.Equal(t, "chutes", result.Provider)
	assert.Equal(t, "claude-3", result.Model)

	// Preserved from base
	assert.Equal(t, "Base role", result.Description)
	assert.Equal(t, "You are a base assistant.", result.SystemPrompt)
	assert.Equal(t, []string{"read_file", "write_file"}, result.Tools.AllowedTools)
	assert.Equal(t, []string{"shell_command"}, result.Tools.DeniedTools)
	assert.Equal(t, []string{"project-planning"}, result.Skills.Skills)
	assert.Equal(t, 10, result.Constraints.MaxIterations)
	assert.Equal(t, 1000, result.Constraints.MaxTokens)
	assert.Equal(t, []string{"/src"}, result.Constraints.AllowedPaths)
}

// --- NewRoleManager directory creation test ---

func TestNewRoleManager_CreatesDirectories(t *testing.T) {
	t.Parallel()

	globalDir := filepath.Join(t.TempDir(), "global", "roles")
	workspaceDir := filepath.Join(t.TempDir(), "workspace", "roles")

	rm := NewRoleManager(globalDir, workspaceDir)

	// Both directories should have been created
	_, err := os.Stat(globalDir)
	assert.NoError(t, err)
	_, err = os.Stat(workspaceDir)
	assert.NoError(t, err)
	assert.Equal(t, globalDir, rm.GlobalDir())
	assert.Equal(t, workspaceDir, rm.WorkspaceDir())
}

func TestNewRoleManager_GlobalOnlyMode(t *testing.T) {
	t.Parallel()

	globalDir := filepath.Join(t.TempDir(), "global", "roles")

	rm := NewRoleManager(globalDir, "")

	_, err := os.Stat(globalDir)
	assert.NoError(t, err)
	assert.Equal(t, globalDir, rm.GlobalDir())
	assert.Equal(t, "", rm.WorkspaceDir())
}

// ---------------------------------------------------------------------------
// Security and validation tests
// ---------------------------------------------------------------------------

func TestSaveRoleToFile_PathTraversal(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	tests := []struct {
		name  string
		input string
	}{
		{"dot-dot-slash", "../escape"},
		{"dot-dot-backslash", "..\\escape"},
		{"absolute-path", "/etc/passwd"},
		{"name-with-slash", "name/with/slash"},
		{"name-with-backslash", "name\\with\\backslash"},
		{"name-with-space", "name with space"},
		{"name-with-special", "name@#$%"},
		{"empty-name", ""},
		{"whitespace-only", "   "},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := RoleConfig{Name: tc.input, Description: "test"}
			err := SaveRoleToFile(tmpDir, cfg)
			require.Error(t, err, "expected error for role name %q", tc.input)
		})
	}
}

func TestIsValidRoleName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"simple", "coder", true},
		{"with-hyphen", "code-reviewer", true},
		{"with_underscore", "code_reviewer", true},
		{"with-dot", "reviewer.v2", true},
		{"uppercase", "CodeReviewer", true},
		{"numeric", "reviewer2", true},
		{"complex", "my-role_v2.0", true},
		{"empty", "", false},
		{"with-space", "code reviewer", false},
		{"with-slash", "path/role", false},
		{"with-backslash", "path\\role", false},
		{"with-dot-dot", "../role", false},
		{"with-at", "role@org", false},
		{"with-colon", "role:name", false},
		{"with-semicolon", "role;name", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := isValidRoleName(tc.input)
			assert.Equal(t, tc.expected, result, "isValidRoleName(%q) = %v, want %v", tc.input, result, tc.expected)
		})
	}
}

func TestRoleManager_ResolveRejectsPathTraversal(t *testing.T) {
	t.Parallel()

	rm, _, _ := setupRoleManager(t, false)

	_, err := rm.Resolve("../escape")
	require.Error(t, err)

	_, err = rm.Resolve("/etc/passwd")
	require.Error(t, err)

	_, err = rm.Resolve("name/with/slash")
	require.Error(t, err)
}

func TestRoleManager_DeleteRejectsPathTraversal(t *testing.T) {
	t.Parallel()

	rm, _, _ := setupRoleManager(t, false)

	err := rm.Delete("../escape")
	require.Error(t, err)

	err = rm.Delete("/etc/passwd")
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func extractNames(metas []RoleMeta) []string {
	names := make([]string, 0, len(metas))
	for _, m := range metas {
		names = append(names, m.Name)
	}
	return names
}
