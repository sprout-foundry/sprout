//go:build !js

package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// setupPolicyTest creates an isolated config directory.
// The temp dir is cleaned up by Go's test framework.
func setupPolicyTest(t *testing.T) {
	t.Helper()
	tmpDir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", tmpDir)
	t.Setenv("SPROUT_CONFIG", tmpDir)

	// Pre-create config so Load() works
	cfg := configuration.NewConfig()
	require.NoError(t, cfg.Save())
}

// setupPolicyTestWithPatterns creates an isolated config with pre-populated patterns.
func setupPolicyTestWithPatterns(t *testing.T, safe, dangerous []configuration.ShellPattern) {
	t.Helper()
	tmpDir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", tmpDir)
	t.Setenv("SPROUT_CONFIG", tmpDir)

	cfg := configuration.NewConfig()
	cfg.Shell.UserSafePatterns = safe
	cfg.Shell.UserDangerousPatterns = dangerous
	require.NoError(t, cfg.Save())
}

// ---------------------------------------------------------------------------
// policy list
// ---------------------------------------------------------------------------

func TestPolicyList_Empty(t *testing.T) {
	setupPolicyTest(t)

	out := testutil.CaptureStdout(t, func() {
		err := policyListCmd.RunE(policyListCmd, nil)
		require.NoError(t, err)
	})

	assert.Contains(t, out, "=== Shell Permission Policy ===")
	assert.Contains(t, out, "User Safe Patterns (0):")
	assert.Contains(t, out, "User Dangerous Patterns (0):")
	assert.Contains(t, out, "Workspace Overlay Mode: tighten_only")
}

func TestPolicyList_WithPatterns(t *testing.T) {
	safe := []configuration.ShellPattern{{Match: "my-tool", Kind: "prefix"}}
	dangerous := []configuration.ShellPattern{{Match: "terraform destroy", Kind: "prefix"}}
	setupPolicyTestWithPatterns(t, safe, dangerous)

	out := testutil.CaptureStdout(t, func() {
		err := policyListCmd.RunE(policyListCmd, nil)
		require.NoError(t, err)
	})

	assert.Contains(t, out, "User Safe Patterns (1):")
	assert.Contains(t, out, "my-tool")
	assert.Contains(t, out, "prefix")
	assert.Contains(t, out, "User Dangerous Patterns (1):")
	assert.Contains(t, out, "terraform destroy")
}

// ---------------------------------------------------------------------------
// policy dump
// ---------------------------------------------------------------------------

func TestPolicyDump_DefaultFormat(t *testing.T) {
	setupPolicyTest(t)

	out := testutil.CaptureStdout(t, func() {
		err := policyDumpCmd.RunE(policyDumpCmd, nil)
		require.NoError(t, err)
	})

	// Default format is JSON
	var result map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	assert.Contains(t, result, "user_safe_patterns")
	assert.Contains(t, result, "user_dangerous_patterns")
	assert.Contains(t, result, "workspace_overlay")
}

func TestPolicyDump_JSON(t *testing.T) {
	safe := []configuration.ShellPattern{{Match: "ls", Kind: "prefix"}}
	setupPolicyTestWithPatterns(t, safe, nil)

	require.NoError(t, policyDumpCmd.Flags().Set("format", "json"))
	out := testutil.CaptureStdout(t, func() {
		err := policyDumpCmd.RunE(policyDumpCmd, nil)
		require.NoError(t, err)
	})

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(out), &result))

	safePatterns := result["user_safe_patterns"].([]interface{})
	assert.Len(t, safePatterns, 1)
}

func TestPolicyDump_YAML(t *testing.T) {
	setupPolicyTest(t)

	require.NoError(t, policyDumpCmd.Flags().Set("format", "yaml"))
	out := testutil.CaptureStdout(t, func() {
		err := policyDumpCmd.RunE(policyDumpCmd, nil)
		require.NoError(t, err)
	})

	var result map[string]interface{}
	require.NoError(t, yaml.Unmarshal([]byte(out), &result))
	assert.Contains(t, result, "user_safe_patterns")
	assert.Contains(t, result, "workspace_overlay")
}

// ---------------------------------------------------------------------------
// policy add
// ---------------------------------------------------------------------------

func TestPolicyAdd_Safe(t *testing.T) {
	setupPolicyTest(t)

	out := testutil.CaptureStdout(t, func() {
		err := policyAddCmd.RunE(policyAddCmd, []string{"safe", "my-tool"})
		require.NoError(t, err)
	})

	assert.Contains(t, out, "Added safe pattern: my-tool")

	// Verify it was persisted
	cfg, err := configuration.Load()
	require.NoError(t, err)
	require.Len(t, cfg.Shell.UserSafePatterns, 1)
	assert.Equal(t, "my-tool", cfg.Shell.UserSafePatterns[0].Match)
	assert.Equal(t, "prefix", cfg.Shell.UserSafePatterns[0].Kind)
}

func TestPolicyAdd_Dangerous(t *testing.T) {
	setupPolicyTest(t)

	out := testutil.CaptureStdout(t, func() {
		err := policyAddCmd.RunE(policyAddCmd, []string{"dangerous", "terraform destroy"})
		require.NoError(t, err)
	})

	assert.Contains(t, out, "Added dangerous pattern: terraform destroy")

	cfg, err := configuration.Load()
	require.NoError(t, err)
	require.Len(t, cfg.Shell.UserDangerousPatterns, 1)
	assert.Equal(t, "terraform destroy", cfg.Shell.UserDangerousPatterns[0].Match)
}

func TestPolicyAdd_InvalidTier(t *testing.T) {
	setupPolicyTest(t)

	err := policyAddCmd.RunE(policyAddCmd, []string{"invalid", "test"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid tier")
}

func TestPolicyAdd_MultipleAppends(t *testing.T) {
	setupPolicyTest(t)

	// Add two safe patterns
	testutil.CaptureStdout(t, func() {
		err := policyAddCmd.RunE(policyAddCmd, []string{"safe", "ls"})
		require.NoError(t, err)
	})
	testutil.CaptureStdout(t, func() {
		err := policyAddCmd.RunE(policyAddCmd, []string{"safe", "cat"})
		require.NoError(t, err)
	})

	cfg, err := configuration.Load()
	require.NoError(t, err)
	require.Len(t, cfg.Shell.UserSafePatterns, 2)
	assert.Equal(t, "ls", cfg.Shell.UserSafePatterns[0].Match)
	assert.Equal(t, "cat", cfg.Shell.UserSafePatterns[1].Match)
}

func TestPolicyAdd_EmptyPattern(t *testing.T) {
	setupPolicyTest(t)

	err := policyAddCmd.RunE(policyAddCmd, []string{"safe", ""})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must not be empty")
}

func TestPolicyAdd_DuplicatePattern(t *testing.T) {
	safe := []configuration.ShellPattern{{Match: "my-tool", Kind: "prefix"}}
	setupPolicyTestWithPatterns(t, safe, nil)

	err := policyAddCmd.RunE(policyAddCmd, []string{"safe", "my-tool"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

// ---------------------------------------------------------------------------
// policy remove
// ---------------------------------------------------------------------------

func TestPolicyRemove_Existing(t *testing.T) {
	safe := []configuration.ShellPattern{{Match: "my-tool", Kind: "prefix"}}
	setupPolicyTestWithPatterns(t, safe, nil)

	out := testutil.CaptureStdout(t, func() {
		err := policyRemoveCmd.RunE(policyRemoveCmd, []string{"safe", "my-tool"})
		require.NoError(t, err)
	})

	assert.Contains(t, out, "Removed safe pattern: my-tool")

	cfg, err := configuration.Load()
	require.NoError(t, err)
	assert.Empty(t, cfg.Shell.UserSafePatterns)
}

func TestPolicyRemove_NotFound(t *testing.T) {
	setupPolicyTest(t)

	err := policyRemoveCmd.RunE(policyRemoveCmd, []string{"safe", "nonexistent"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestPolicyRemove_InvalidTier(t *testing.T) {
	setupPolicyTest(t)

	err := policyRemoveCmd.RunE(policyRemoveCmd, []string{"invalid", "test"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid tier")
}

func TestPolicyRemove_KeepsOthers(t *testing.T) {
	safe := []configuration.ShellPattern{
		{Match: "ls", Kind: "prefix"},
		{Match: "my-tool", Kind: "prefix"},
		{Match: "cat", Kind: "prefix"},
	}
	setupPolicyTestWithPatterns(t, safe, nil)

	testutil.CaptureStdout(t, func() {
		err := policyRemoveCmd.RunE(policyRemoveCmd, []string{"safe", "my-tool"})
		require.NoError(t, err)
	})

	cfg, err := configuration.Load()
	require.NoError(t, err)
	require.Len(t, cfg.Shell.UserSafePatterns, 2)
	assert.Equal(t, "ls", cfg.Shell.UserSafePatterns[0].Match)
	assert.Equal(t, "cat", cfg.Shell.UserSafePatterns[1].Match)
}

// ---------------------------------------------------------------------------
// policy export
// ---------------------------------------------------------------------------

func TestPolicyExport_DefaultYAML(t *testing.T) {
	safe := []configuration.ShellPattern{{Match: "ls", Kind: "prefix"}}
	setupPolicyTestWithPatterns(t, safe, nil)

	out := testutil.CaptureStdout(t, func() {
		err := policyExportCmd.RunE(policyExportCmd, nil)
		require.NoError(t, err)
	})

	// Default format is YAML
	var result map[string]interface{}
	require.NoError(t, yaml.Unmarshal([]byte(out), &result))
	assert.Contains(t, result, "user_safe_patterns")
}

func TestPolicyExport_JSON(t *testing.T) {
	safe := []configuration.ShellPattern{{Match: "ls", Kind: "prefix"}}
	dangerous := []configuration.ShellPattern{{Match: "rm -rf", Kind: "prefix"}}
	setupPolicyTestWithPatterns(t, safe, dangerous)

	require.NoError(t, policyExportCmd.Flags().Set("format", "json"))
	out := testutil.CaptureStdout(t, func() {
		err := policyExportCmd.RunE(policyExportCmd, nil)
		require.NoError(t, err)
	})

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(out), &result))

	safePatterns := result["user_safe_patterns"].([]interface{})
	assert.Len(t, safePatterns, 1)

	dangerPatterns := result["user_dangerous_patterns"].([]interface{})
	assert.Len(t, dangerPatterns, 1)
}

func TestPolicyExport_YAML(t *testing.T) {
	safe := []configuration.ShellPattern{{Match: "ls", Kind: "prefix"}}
	setupPolicyTestWithPatterns(t, safe, nil)

	require.NoError(t, policyExportCmd.Flags().Set("format", "yaml"))
	out := testutil.CaptureStdout(t, func() {
		err := policyExportCmd.RunE(policyExportCmd, nil)
		require.NoError(t, err)
	})

	assert.Contains(t, out, "user_safe_patterns")
}

// ---------------------------------------------------------------------------
// policy import
// ---------------------------------------------------------------------------

func TestPolicyImport_YAML(t *testing.T) {
	setupPolicyTest(t)

	// Create import file
	importDir := t.TempDir()
	yamlContent := `user_safe_patterns:
  - match: imported-safe
    kind: prefix
user_dangerous_patterns:
  - match: imported-dangerous
    kind: prefix
`
	importFile := filepath.Join(importDir, "policy.yaml")
	require.NoError(t, os.WriteFile(importFile, []byte(yamlContent), 0o644))

	out := testutil.CaptureStdout(t, func() {
		err := policyImportCmd.RunE(policyImportCmd, []string{importFile})
		require.NoError(t, err)
	})

	assert.Contains(t, out, "Imported 1 safe patterns, 1 dangerous patterns")

	cfg, err := configuration.Load()
	require.NoError(t, err)
	require.Len(t, cfg.Shell.UserSafePatterns, 1)
	assert.Equal(t, "imported-safe", cfg.Shell.UserSafePatterns[0].Match)
	require.Len(t, cfg.Shell.UserDangerousPatterns, 1)
	assert.Equal(t, "imported-dangerous", cfg.Shell.UserDangerousPatterns[0].Match)
}

func TestPolicyImport_Deduplication(t *testing.T) {
	// Start with existing patterns
	safe := []configuration.ShellPattern{{Match: "existing-tool", Kind: "prefix"}}
	dangerous := []configuration.ShellPattern{{Match: "existing-dangerous", Kind: "prefix"}}
	setupPolicyTestWithPatterns(t, safe, dangerous)

	// Import file contains a mix of new and existing patterns
	importDir := t.TempDir()
	yamlContent := `user_safe_patterns:
  - match: existing-tool
    kind: prefix
  - match: new-tool
    kind: prefix
user_dangerous_patterns:
  - match: existing-dangerous
    kind: prefix
  - match: new-dangerous
    kind: prefix
`
	importFile := filepath.Join(importDir, "policy.yaml")
	require.NoError(t, os.WriteFile(importFile, []byte(yamlContent), 0o644))

	out := testutil.CaptureStdout(t, func() {
		err := policyImportCmd.RunE(policyImportCmd, []string{importFile})
		require.NoError(t, err)
	})

	assert.Contains(t, out, "1 new safe patterns")
	assert.Contains(t, out, "1 duplicates skipped")
	assert.Contains(t, out, "1 new dangerous patterns")

	cfg, err := configuration.Load()
	require.NoError(t, err)
	require.Len(t, cfg.Shell.UserSafePatterns, 2) // existing + 1 new
	assert.Equal(t, "existing-tool", cfg.Shell.UserSafePatterns[0].Match)
	assert.Equal(t, "new-tool", cfg.Shell.UserSafePatterns[1].Match)
	require.Len(t, cfg.Shell.UserDangerousPatterns, 2) // existing + 1 new
	assert.Equal(t, "existing-dangerous", cfg.Shell.UserDangerousPatterns[0].Match)
	assert.Equal(t, "new-dangerous", cfg.Shell.UserDangerousPatterns[1].Match)
}

func TestPolicyImport_MergesWithExisting(t *testing.T) {
	// Start with an existing safe pattern
	safe := []configuration.ShellPattern{{Match: "existing-tool", Kind: "prefix"}}
	setupPolicyTestWithPatterns(t, safe, nil)

	// Create import file with additional patterns
	importDir := t.TempDir()
	yamlContent := `user_safe_patterns:
  - match: imported-tool
    kind: prefix
`
	importFile := filepath.Join(importDir, "policy.yaml")
	require.NoError(t, os.WriteFile(importFile, []byte(yamlContent), 0o644))

	testutil.CaptureStdout(t, func() {
		err := policyImportCmd.RunE(policyImportCmd, []string{importFile})
		require.NoError(t, err)
	})

	cfg, err := configuration.Load()
	require.NoError(t, err)
	require.Len(t, cfg.Shell.UserSafePatterns, 2)
	assert.Equal(t, "existing-tool", cfg.Shell.UserSafePatterns[0].Match)
	assert.Equal(t, "imported-tool", cfg.Shell.UserSafePatterns[1].Match)
}

func TestPolicyImport_JSON(t *testing.T) {
	setupPolicyTest(t)

	importDir := t.TempDir()
	jsonContent := `{"user_safe_patterns": [{"match": "json-tool", "kind": "prefix"}]}`
	importFile := filepath.Join(importDir, "policy.json")
	require.NoError(t, os.WriteFile(importFile, []byte(jsonContent), 0o644))

	out := testutil.CaptureStdout(t, func() {
		err := policyImportCmd.RunE(policyImportCmd, []string{importFile})
		require.NoError(t, err)
	})

	assert.Contains(t, out, "Imported 1 safe patterns, 0 dangerous patterns")

	cfg, err := configuration.Load()
	require.NoError(t, err)
	require.Len(t, cfg.Shell.UserSafePatterns, 1)
	assert.Equal(t, "json-tool", cfg.Shell.UserSafePatterns[0].Match)
}

func TestPolicyImport_NonExistentFile(t *testing.T) {
	setupPolicyTest(t)

	err := policyImportCmd.RunE(policyImportCmd, []string{"/nonexistent/path/file.yaml"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read file")
}

func TestPolicyImport_InvalidFormat(t *testing.T) {
	setupPolicyTest(t)

	importDir := t.TempDir()
	importFile := filepath.Join(importDir, "policy.txt")
	require.NoError(t, os.WriteFile(importFile, []byte("data"), 0o644))

	err := policyImportCmd.RunE(policyImportCmd, []string{importFile})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported file format")
}

func TestPolicyImport_YMLExtension(t *testing.T) {
	setupPolicyTest(t)

	importDir := t.TempDir()
	importFile := filepath.Join(importDir, "policy.yml")
	yamlContent := `user_safe_patterns:
  - match: yml-tool
    kind: prefix
`
	require.NoError(t, os.WriteFile(importFile, []byte(yamlContent), 0o644))

	err := policyImportCmd.RunE(policyImportCmd, []string{importFile})
	require.NoError(t, err)

	cfg, err := configuration.Load()
	require.NoError(t, err)
	require.Len(t, cfg.Shell.UserSafePatterns, 1)
	assert.Equal(t, "yml-tool", cfg.Shell.UserSafePatterns[0].Match)
}

// ---------------------------------------------------------------------------
// findPattern / removePattern helpers
// ---------------------------------------------------------------------------

func TestFindPattern_ExactMatch(t *testing.T) {
	patterns := []configuration.ShellPattern{
		{Match: "ls", Kind: "prefix"},
		{Match: "cat", Kind: "prefix"},
		{Match: "grep", Kind: "prefix"},
	}

	assert.Equal(t, 0, findPattern(patterns, "ls"))
	assert.Equal(t, 1, findPattern(patterns, "cat"))
	assert.Equal(t, 2, findPattern(patterns, "grep"))
	assert.Equal(t, -1, findPattern(patterns, "nonexistent"))
}

func TestFindPattern_EmptySlice(t *testing.T) {
	assert.Equal(t, -1, findPattern(nil, "ls"))
	assert.Equal(t, -1, findPattern([]configuration.ShellPattern{}, "ls"))
}

func TestRemovePattern_Middle(t *testing.T) {
	patterns := []configuration.ShellPattern{
		{Match: "a"},
		{Match: "b"},
		{Match: "c"},
	}
	result := removePattern(patterns, 1)
	require.Len(t, result, 2)
	assert.Equal(t, "a", result[0].Match)
	assert.Equal(t, "c", result[1].Match)
}

func TestRemovePattern_First(t *testing.T) {
	patterns := []configuration.ShellPattern{
		{Match: "a"},
		{Match: "b"},
	}
	result := removePattern(patterns, 0)
	require.Len(t, result, 1)
	assert.Equal(t, "b", result[0].Match)
}

func TestRemovePattern_Last(t *testing.T) {
	patterns := []configuration.ShellPattern{
		{Match: "a"},
		{Match: "b"},
	}
	result := removePattern(patterns, 1)
	require.Len(t, result, 1)
	assert.Equal(t, "a", result[0].Match)
}

// ---------------------------------------------------------------------------
// formatOutput
// ---------------------------------------------------------------------------

func TestFormatOutput_JSON(t *testing.T) {
	data := map[string]string{"key": "value"}
	out := testutil.CaptureStdout(t, func() {
		err := formatOutput(data, "json")
		require.NoError(t, err)
	})

	var result map[string]string
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &result))
	assert.Equal(t, "value", result["key"])
}

func TestFormatOutput_YAML(t *testing.T) {
	data := map[string]string{"key": "value"}
	out := testutil.CaptureStdout(t, func() {
		err := formatOutput(data, "yaml")
		require.NoError(t, err)
	})

	assert.Contains(t, out, "key:")
	assert.Contains(t, out, "value")
}

func TestFormatOutput_InvalidFormat(t *testing.T) {
	data := map[string]string{"key": "value"}
	err := formatOutput(data, "xml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported format")
}
