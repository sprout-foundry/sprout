package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// setupTrustStore swaps the global trust store path to a temp file so tests
// don't clobber the real ~/.sprout/trusted-workspaces.json. ALL access to
// trustedWorkspacesPath and trustedWorkspacesCache happens inside the lock
// to avoid data races. Returns a cleanup function.
func setupTrustStore(t *testing.T, tmpDir string) func() {
	t.Helper()

	trustedWorkspacesMu.Lock()
	originalPath := trustedWorkspacesPath
	originalCache := trustedWorkspacesCache
	trustedWorkspacesCache = nil
	trustedWorkspacesPath = filepath.Join(tmpDir, "trusted-workspaces.json")
	trustedWorkspacesMu.Unlock()

	return func() {
		trustedWorkspacesMu.Lock()
		trustedWorkspacesCache = originalCache
		trustedWorkspacesPath = originalPath
		trustedWorkspacesMu.Unlock()
	}
}

// writeWorkspacePolicy writes a JSON shell-policy file to .sprout/shell-policy.json
// under the given workspace root.
func writeWorkspacePolicy(t *testing.T, workspaceRoot string, cfg configuration.ShellConfig) {
	t.Helper()
	policyDir := filepath.Join(workspaceRoot, ".sprout")
	if err := os.MkdirAll(policyDir, 0o755); err != nil {
		t.Fatalf("mkdir policy dir: %v", err)
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal policy: %v", err)
	}
	path := filepath.Join(policyDir, "shell-policy.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write policy: %v", err)
	}
}

// patternsToConfig builds a ShellConfig with the given safe and dangerous patterns.
func patternsToConfig(safe, dangerous []configuration.ShellPattern) configuration.ShellConfig {
	return configuration.ShellConfig{
		UserSafePatterns:      safe,
		UserDangerousPatterns: dangerous,
	}
}

func TestLoadWorkspaceOverlay_NoWorkspaceFile(t *testing.T) {
	tmpDir := t.TempDir()
	cleanup := setupTrustStore(t, tmpDir)
	defer cleanup()

	wsRoot := t.TempDir()
	userCfg := configuration.ShellConfig{
		WorkspaceOverlay: configuration.WorkspaceOverlayConfig{Mode: "tighten_only"},
		UserSafePatterns: []configuration.ShellPattern{{Match: "user-safe", Kind: "prefix"}},
	}

	merged, warnings := LoadWorkspaceOverlay(wsRoot, userCfg)
	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got %d: %v", len(warnings), warnings)
	}
	if len(merged.UserSafePatterns) != 1 || merged.UserSafePatterns[0].Match != "user-safe" {
		t.Errorf("expected user-safe pattern unchanged, got %v", merged.UserSafePatterns)
	}
}

func TestLoadWorkspaceOverlay_ModeIgnore(t *testing.T) {
	tmpDir := t.TempDir()
	cleanup := setupTrustStore(t, tmpDir)
	defer cleanup()

	wsRoot := t.TempDir()
	wsCfg := patternsToConfig(
		[]configuration.ShellPattern{{Match: "ws-safe", Kind: "prefix"}},
		[]configuration.ShellPattern{{Match: "ws-danger", Kind: "prefix"}},
	)
	writeWorkspacePolicy(t, wsRoot, wsCfg)

	userCfg := configuration.ShellConfig{
		WorkspaceOverlay: configuration.WorkspaceOverlayConfig{Mode: "ignore"},
		UserSafePatterns: []configuration.ShellPattern{{Match: "user-safe", Kind: "prefix"}},
	}

	merged, warnings := LoadWorkspaceOverlay(wsRoot, userCfg)
	if len(warnings) != 0 {
		t.Errorf("expected no warnings in ignore mode, got %d: %v", len(warnings), warnings)
	}
	if len(merged.UserSafePatterns) != 1 || merged.UserSafePatterns[0].Match != "user-safe" {
		t.Errorf("expected user config unchanged in ignore mode, got %v", merged.UserSafePatterns)
	}
	if len(merged.UserDangerousPatterns) != 0 {
		t.Errorf("expected no dangerous patterns in ignore mode, got %v", merged.UserDangerousPatterns)
	}
}

func TestLoadWorkspaceOverlay_TightenOnly(t *testing.T) {
	tmpDir := t.TempDir()
	cleanup := setupTrustStore(t, tmpDir)
	defer cleanup()

	wsRoot := t.TempDir()
	wsCfg := patternsToConfig(
		[]configuration.ShellPattern{{Match: "ws-safe", Kind: "prefix"}},
		[]configuration.ShellPattern{{Match: "ws-danger", Kind: "prefix"}},
	)
	writeWorkspacePolicy(t, wsRoot, wsCfg)

	userCfg := configuration.ShellConfig{
		WorkspaceOverlay: configuration.WorkspaceOverlayConfig{Mode: "tighten_only"},
		UserDangerousPatterns: []configuration.ShellPattern{{Match: "user-danger", Kind: "prefix"}},
	}

	merged, warnings := LoadWorkspaceOverlay(wsRoot, userCfg)

	// Safe patterns should be ignored.
	if len(merged.UserSafePatterns) != 0 {
		t.Errorf("expected no safe patterns in tighten_only mode, got %v", merged.UserSafePatterns)
	}

	// Dangerous patterns should be merged (user + workspace).
	if len(merged.UserDangerousPatterns) != 2 {
		t.Errorf("expected 2 dangerous patterns (user + workspace), got %d: %v", len(merged.UserDangerousPatterns), merged.UserDangerousPatterns)
	}

	// Should have a warning about ignored safe patterns.
	foundWarning := false
	for _, w := range warnings {
		if contains(w, "tighten_only") || contains(w, "ignoring") {
			foundWarning = true
		}
	}
	if !foundWarning {
		t.Errorf("expected a warning about ignored safe patterns, got: %v", warnings)
	}
}

func TestLoadWorkspaceOverlay_TightenOnlyDefaultMode(t *testing.T) {
	tmpDir := t.TempDir()
	cleanup := setupTrustStore(t, tmpDir)
	defer cleanup()

	wsRoot := t.TempDir()
	wsCfg := patternsToConfig(
		[]configuration.ShellPattern{{Match: "ws-safe", Kind: "prefix"}},
		[]configuration.ShellPattern{{Match: "ws-danger", Kind: "prefix"}},
	)
	writeWorkspacePolicy(t, wsRoot, wsCfg)

	// Empty mode should default to tighten_only.
	userCfg := configuration.ShellConfig{
		WorkspaceOverlay: configuration.WorkspaceOverlayConfig{Mode: ""},
	}

	merged, warnings := LoadWorkspaceOverlay(wsRoot, userCfg)

	// Safe patterns ignored, dangerous patterns honored.
	if len(merged.UserSafePatterns) != 0 {
		t.Errorf("expected no safe patterns with default mode, got %v", merged.UserSafePatterns)
	}
	if len(merged.UserDangerousPatterns) != 1 {
		t.Errorf("expected 1 dangerous pattern (workspace), got %d", len(merged.UserDangerousPatterns))
	}
	if len(warnings) == 0 {
		t.Errorf("expected warning about ignored safe patterns with default mode")
	}
}

func TestLoadWorkspaceOverlay_TrustedMatchingHash(t *testing.T) {
	tmpDir := t.TempDir()
	cleanup := setupTrustStore(t, tmpDir)
	defer cleanup()

	wsRoot := t.TempDir()
	wsCfg := patternsToConfig(
		[]configuration.ShellPattern{{Match: "ws-safe", Kind: "prefix"}},
		[]configuration.ShellPattern{{Match: "ws-danger", Kind: "prefix"}},
	)
	writeWorkspacePolicy(t, wsRoot, wsCfg)

	// Trust the workspace first.
	if err := TrustWorkspace(wsRoot); err != nil {
		t.Fatalf("TrustWorkspace failed: %v", err)
	}

	userCfg := configuration.ShellConfig{
		WorkspaceOverlay: configuration.WorkspaceOverlayConfig{Mode: "trusted"},
		UserSafePatterns: []configuration.ShellPattern{{Match: "user-safe", Kind: "prefix"}},
	}

	merged, warnings := LoadWorkspaceOverlay(wsRoot, userCfg)

	// Full overlay — both safe and dangerous should be present.
	if len(merged.UserSafePatterns) != 2 {
		t.Errorf("expected 2 safe patterns (user + workspace), got %d: %v", len(merged.UserSafePatterns), merged.UserSafePatterns)
	}
	if len(merged.UserDangerousPatterns) != 1 {
		t.Errorf("expected 1 dangerous pattern (workspace), got %d: %v", len(merged.UserDangerousPatterns), merged.UserDangerousPatterns)
	}
	if len(warnings) != 0 {
		t.Errorf("expected no warnings when trusted, got %d: %v", len(warnings), warnings)
	}
}

func TestLoadWorkspaceOverlay_TrustedHashMismatch(t *testing.T) {
	tmpDir := t.TempDir()
	cleanup := setupTrustStore(t, tmpDir)
	defer cleanup()

	wsRoot := t.TempDir()
	wsCfg := patternsToConfig(
		[]configuration.ShellPattern{{Match: "ws-safe", Kind: "prefix"}},
		[]configuration.ShellPattern{{Match: "ws-danger", Kind: "prefix"}},
	)
	writeWorkspacePolicy(t, wsRoot, wsCfg)

	// Trust the workspace first.
	if err := TrustWorkspace(wsRoot); err != nil {
		t.Fatalf("TrustWorkspace failed: %v", err)
	}

	// Modify the workspace policy file — hash will change.
	wsCfg2 := patternsToConfig(
		[]configuration.ShellPattern{{Match: "ws-safe-MODIFIED", Kind: "prefix"}},
		[]configuration.ShellPattern{{Match: "ws-danger", Kind: "prefix"}},
	)
	writeWorkspacePolicy(t, wsRoot, wsCfg2)

	userCfg := configuration.ShellConfig{
		WorkspaceOverlay: configuration.WorkspaceOverlayConfig{Mode: "trusted"},
		UserSafePatterns: []configuration.ShellPattern{{Match: "user-safe", Kind: "prefix"}},
	}

	merged, warnings := LoadWorkspaceOverlay(wsRoot, userCfg)

	// Hash mismatch → falls back to tighten_only: safe patterns ignored.
	if len(merged.UserSafePatterns) != 1 {
		t.Errorf("expected only user safe pattern (hash mismatch reverts to tighten_only), got %d: %v", len(merged.UserSafePatterns), merged.UserSafePatterns)
	}
	if merged.UserSafePatterns[0].Match != "user-safe" {
		t.Errorf("expected only user-safe pattern, got %v", merged.UserSafePatterns)
	}

	// Dangerous patterns still honored (tighten_only fallback).
	if len(merged.UserDangerousPatterns) != 1 {
		t.Errorf("expected 1 dangerous pattern (workspace, tighten_only fallback), got %d", len(merged.UserDangerousPatterns))
	}

	// Should have a warning about hash mismatch.
	foundWarning := false
	for _, w := range warnings {
		if contains(w, "not trusted") || contains(w, "hash") {
			foundWarning = true
		}
	}
	if !foundWarning {
		t.Errorf("expected warning about hash mismatch, got: %v", warnings)
	}
}

func TestLoadWorkspaceOverlay_TrustedNoRecordedHash(t *testing.T) {
	tmpDir := t.TempDir()
	cleanup := setupTrustStore(t, tmpDir)
	defer cleanup()

	wsRoot := t.TempDir()
	wsCfg := patternsToConfig(
		[]configuration.ShellPattern{{Match: "ws-safe", Kind: "prefix"}},
		[]configuration.ShellPattern{{Match: "ws-danger", Kind: "prefix"}},
	)
	writeWorkspacePolicy(t, wsRoot, wsCfg)

	// Do NOT trust the workspace — mode is trusted but no hash recorded.
	userCfg := configuration.ShellConfig{
		WorkspaceOverlay: configuration.WorkspaceOverlayConfig{Mode: "trusted"},
		UserSafePatterns: []configuration.ShellPattern{{Match: "user-safe", Kind: "prefix"}},
	}

	merged, warnings := LoadWorkspaceOverlay(wsRoot, userCfg)

	// No recorded hash → falls back to tighten_only: safe patterns ignored.
	if len(merged.UserSafePatterns) != 1 {
		t.Errorf("expected only user safe pattern (no recorded hash → tighten_only), got %d: %v", len(merged.UserSafePatterns), merged.UserSafePatterns)
	}

	// Dangerous patterns still honored.
	if len(merged.UserDangerousPatterns) != 1 {
		t.Errorf("expected 1 dangerous pattern (workspace, tighten_only fallback), got %d", len(merged.UserDangerousPatterns))
	}

	foundWarning := false
	for _, w := range warnings {
		if contains(w, "not trusted") {
			foundWarning = true
		}
	}
	if !foundWarning {
		t.Errorf("expected warning about no recorded hash, got: %v", warnings)
	}
}

func TestLoadWorkspaceOverlay_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	cleanup := setupTrustStore(t, tmpDir)
	defer cleanup()

	wsRoot := t.TempDir()
	policyDir := filepath.Join(wsRoot, ".sprout")
	if err := os.MkdirAll(policyDir, 0o755); err != nil {
		t.Fatalf("mkdir policy dir: %v", err)
	}
	// Write invalid JSON.
	invalidPath := filepath.Join(policyDir, "shell-policy.json")
	if err := os.WriteFile(invalidPath, []byte("{invalid"), 0o644); err != nil {
		t.Fatalf("write invalid policy: %v", err)
	}

	userCfg := configuration.ShellConfig{
		WorkspaceOverlay: configuration.WorkspaceOverlayConfig{Mode: "tighten_only"},
		UserSafePatterns: []configuration.ShellPattern{{Match: "user-safe", Kind: "prefix"}},
	}

	merged, warnings := LoadWorkspaceOverlay(wsRoot, userCfg)

	// Invalid JSON should not crash; user config returned unchanged with a warning.
	if len(merged.UserSafePatterns) != 1 || merged.UserSafePatterns[0].Match != "user-safe" {
		t.Errorf("expected user config unchanged on invalid JSON, got %v", merged.UserSafePatterns)
	}
	if len(warnings) == 0 {
		t.Errorf("expected a warning for invalid JSON, got none")
	}
}

func TestLoadWorkspaceOverlay_EmptyWorkspaceFile(t *testing.T) {
	tmpDir := t.TempDir()
	cleanup := setupTrustStore(t, tmpDir)
	defer cleanup()

	wsRoot := t.TempDir()
	// Write an empty (zero-value) policy.
	writeWorkspacePolicy(t, wsRoot, configuration.ShellConfig{})

	userCfg := configuration.ShellConfig{
		WorkspaceOverlay: configuration.WorkspaceOverlayConfig{Mode: "tighten_only"},
		UserSafePatterns: []configuration.ShellPattern{{Match: "user-safe", Kind: "prefix"}},
	}

	merged, warnings := LoadWorkspaceOverlay(wsRoot, userCfg)

	// No workspace patterns to merge, no warnings expected.
	if len(merged.UserSafePatterns) != 1 {
		t.Errorf("expected user config unchanged with empty workspace file, got %v", merged.UserSafePatterns)
	}
	if len(warnings) != 0 {
		t.Errorf("expected no warnings for empty workspace file, got %d: %v", len(warnings), warnings)
	}
}

func TestLoadWorkspaceOverlay_PatternsAppendedNotReplaced(t *testing.T) {
	tmpDir := t.TempDir()
	cleanup := setupTrustStore(t, tmpDir)
	defer cleanup()

	wsRoot := t.TempDir()
	wsCfg := patternsToConfig(
		nil,
		[]configuration.ShellPattern{{Match: "ws-danger1", Kind: "prefix"}, {Match: "ws-danger2", Kind: "prefix"}},
	)
	writeWorkspacePolicy(t, wsRoot, wsCfg)

	userCfg := configuration.ShellConfig{
		WorkspaceOverlay: configuration.WorkspaceOverlayConfig{Mode: "tighten_only"},
		UserDangerousPatterns: []configuration.ShellPattern{{Match: "user-danger1", Kind: "prefix"}},
	}

	merged, _ := LoadWorkspaceOverlay(wsRoot, userCfg)

	// User patterns should be preserved, workspace patterns appended.
	if len(merged.UserDangerousPatterns) != 3 {
		t.Errorf("expected 3 dangerous patterns (1 user + 2 workspace), got %d: %v", len(merged.UserDangerousPatterns), merged.UserDangerousPatterns)
	}
	if merged.UserDangerousPatterns[0].Match != "user-danger1" {
		t.Errorf("expected user pattern first, got %s", merged.UserDangerousPatterns[0].Match)
	}
}

func TestLoadWorkspaceOverlay_DeepCopy(t *testing.T) {
	tmpDir := t.TempDir()
	cleanup := setupTrustStore(t, tmpDir)
	defer cleanup()

	wsRoot := t.TempDir()
	wsCfg := patternsToConfig(
		nil,
		[]configuration.ShellPattern{{Match: "ws-danger", Kind: "prefix"}},
	)
	writeWorkspacePolicy(t, wsRoot, wsCfg)

	userPatterns := []configuration.ShellPattern{{Match: "user-danger", Kind: "prefix"}}
	userCfg := configuration.ShellConfig{
		WorkspaceOverlay: configuration.WorkspaceOverlayConfig{Mode: "tighten_only"},
		UserDangerousPatterns: userPatterns,
	}

	merged, _ := LoadWorkspaceOverlay(wsRoot, userCfg)

	// Mutate the merged config.
	merged.UserDangerousPatterns[0].Match = "MUTATED"

	// Original user config should not be affected.
	if userCfg.UserDangerousPatterns[0].Match != "user-danger" {
		t.Errorf("deep copy failed: original user config was mutated to %s", userCfg.UserDangerousPatterns[0].Match)
	}
	if userPatterns[0].Match != "user-danger" {
		t.Errorf("deep copy failed: original slice was mutated to %s", userPatterns[0].Match)
	}
}

func TestLoadWorkspaceOverlay_UnrecognizedMode(t *testing.T) {
	tmpDir := t.TempDir()
	cleanup := setupTrustStore(t, tmpDir)
	defer cleanup()

	wsRoot := t.TempDir()
	wsCfg := patternsToConfig(
		[]configuration.ShellPattern{{Match: "ws-safe", Kind: "prefix"}},
		[]configuration.ShellPattern{{Match: "ws-danger", Kind: "prefix"}},
	)
	writeWorkspacePolicy(t, wsRoot, wsCfg)

	userCfg := configuration.ShellConfig{
		WorkspaceOverlay: configuration.WorkspaceOverlayConfig{Mode: "bogus-mode"},
	}

	merged, warnings := LoadWorkspaceOverlay(wsRoot, userCfg)

	// Unrecognized mode → tighten_only behavior.
	if len(merged.UserSafePatterns) != 0 {
		t.Errorf("expected no safe patterns with unrecognized mode, got %v", merged.UserSafePatterns)
	}
	if len(merged.UserDangerousPatterns) != 1 {
		t.Errorf("expected 1 dangerous pattern (tighten_only fallback), got %d", len(merged.UserDangerousPatterns))
	}
	foundFallbackWarning := false
	for _, w := range warnings {
		if contains(w, "unrecognized mode") {
			foundFallbackWarning = true
		}
	}
	if !foundFallbackWarning {
		t.Errorf("expected warning about unrecognized mode, got: %v", warnings)
	}
}

func TestLoadWorkspaceOverlay_TightenOnlyNoSafePatternsInWorkspace(t *testing.T) {
	tmpDir := t.TempDir()
	cleanup := setupTrustStore(t, tmpDir)
	defer cleanup()

	wsRoot := t.TempDir()
	// Workspace has only dangerous patterns, no safe patterns.
	wsCfg := patternsToConfig(
		nil,
		[]configuration.ShellPattern{{Match: "ws-danger", Kind: "prefix"}},
	)
	writeWorkspacePolicy(t, wsRoot, wsCfg)

	userCfg := configuration.ShellConfig{
		WorkspaceOverlay: configuration.WorkspaceOverlayConfig{Mode: "tighten_only"},
	}

	merged, warnings := LoadWorkspaceOverlay(wsRoot, userCfg)

	if len(merged.UserDangerousPatterns) != 1 {
		t.Errorf("expected 1 dangerous pattern, got %d", len(merged.UserDangerousPatterns))
	}
	// No safe patterns in workspace → no warning expected about ignoring them.
	for _, w := range warnings {
		if contains(w, "ignoring") && contains(w, "safe") {
			t.Errorf("should not warn about ignoring safe patterns when workspace has none, got: %s", w)
		}
	}
}

func TestLoadWorkspaceOverlay_Trusted_FullIntegration(t *testing.T) {
	tmpDir := t.TempDir()
	cleanup := setupTrustStore(t, tmpDir)
	defer cleanup()

	wsRoot := t.TempDir()
	wsCfg := patternsToConfig(
		[]configuration.ShellPattern{{Match: "ws-safe1", Kind: "prefix"}, {Match: "ws-safe2", Kind: "prefix"}},
		[]configuration.ShellPattern{{Match: "ws-danger1", Kind: "prefix"}},
	)
	writeWorkspacePolicy(t, wsRoot, wsCfg)

	// 1. Trust the workspace.
	if err := TrustWorkspace(wsRoot); err != nil {
		t.Fatalf("TrustWorkspace failed: %v", err)
	}

	// 2. Verify it's trusted.
	trusted, err := IsWorkspaceTrusted(wsRoot)
	if err != nil {
		t.Fatalf("IsWorkspaceTrusted failed: %v", err)
	}
	if !trusted {
		t.Error("expected workspace to be trusted after TrustWorkspace")
	}

	// 3. Load overlay with trusted mode.
	userCfg := configuration.ShellConfig{
		WorkspaceOverlay: configuration.WorkspaceOverlayConfig{Mode: "trusted"},
		UserSafePatterns: []configuration.ShellPattern{{Match: "user-safe", Kind: "prefix"}},
		UserDangerousPatterns: []configuration.ShellPattern{{Match: "user-danger", Kind: "prefix"}},
	}

	merged, warnings := LoadWorkspaceOverlay(wsRoot, userCfg)
	if len(warnings) != 0 {
		t.Errorf("expected no warnings in full trusted flow, got %d: %v", len(warnings), warnings)
	}
	if len(merged.UserSafePatterns) != 3 {
		t.Errorf("expected 3 safe patterns (1 user + 2 workspace), got %d: %v", len(merged.UserSafePatterns), merged.UserSafePatterns)
	}
	if len(merged.UserDangerousPatterns) != 2 {
		t.Errorf("expected 2 dangerous patterns (1 user + 1 workspace), got %d: %v", len(merged.UserDangerousPatterns), merged.UserDangerousPatterns)
	}

	// 4. Untrust.
	if err := UntrustWorkspace(wsRoot); err != nil {
		t.Fatalf("UntrustWorkspace failed: %v", err)
	}

	trusted, err = IsWorkspaceTrusted(wsRoot)
	if err != nil {
		t.Fatalf("IsWorkspaceTrusted after untrust failed: %v", err)
	}
	if trusted {
		t.Error("expected workspace to NOT be trusted after UntrustWorkspace")
	}

	// 5. After untrust, loading with trusted mode should fall back to tighten_only.
	merged2, warnings2 := LoadWorkspaceOverlay(wsRoot, userCfg)
	if len(merged2.UserSafePatterns) != 1 {
		t.Errorf("expected only user safe pattern after untrust (tighten_only fallback), got %d: %v", len(merged2.UserSafePatterns), merged2.UserSafePatterns)
	}
	if len(warnings2) == 0 {
		t.Error("expected warning about untrusted workspace")
	}
}

// --- Trust management tests ---

func TestTrustWorkspace_NoFile(t *testing.T) {
	tmpDir := t.TempDir()
	cleanup := setupTrustStore(t, tmpDir)
	defer cleanup()

	wsRoot := t.TempDir()
	// No policy file created.

	err := TrustWorkspace(wsRoot)
	if err == nil {
		t.Error("expected error when trusting workspace with no policy file")
	}
}

func TestUntrustAllWorkspaces(t *testing.T) {
	tmpDir := t.TempDir()
	cleanup := setupTrustStore(t, tmpDir)
	defer cleanup()

	// Trust two workspaces.
	wsRoot1 := t.TempDir()
	wsRoot2 := t.TempDir()
	writeWorkspacePolicy(t, wsRoot1, configuration.ShellConfig{})
	writeWorkspacePolicy(t, wsRoot2, configuration.ShellConfig{})

	if err := TrustWorkspace(wsRoot1); err != nil {
		t.Fatalf("trust ws1: %v", err)
	}
	if err := TrustWorkspace(wsRoot2); err != nil {
		t.Fatalf("trust ws2: %v", err)
	}

	// Untrust all.
	if err := UntrustAllWorkspaces(); err != nil {
		t.Fatalf("UntrustAllWorkspaces: %v", err)
	}

	// Both should be untrusted.
	trusted1, _ := IsWorkspaceTrusted(wsRoot1)
	trusted2, _ := IsWorkspaceTrusted(wsRoot2)
	if trusted1 {
		t.Error("ws1 should be untrusted after UntrustAllWorkspaces")
	}
	if trusted2 {
		t.Error("ws2 should be untrusted after UntrustAllWorkspaces")
	}
}

func TestLoadTrustedWorkspaces_ForceReload(t *testing.T) {
	tmpDir := t.TempDir()
	cleanup := setupTrustStore(t, tmpDir)
	defer cleanup()

	// Trust a workspace.
	wsRoot := t.TempDir()
	writeWorkspacePolicy(t, wsRoot, configuration.ShellConfig{})
	if err := TrustWorkspace(wsRoot); err != nil {
		t.Fatalf("trust: %v", err)
	}

	// Verify cache is populated.
	trustedWorkspacesMu.RLock()
	cachePopulated := trustedWorkspacesCache != nil
	trustedWorkspacesMu.RUnlock()
	if !cachePopulated {
		t.Error("expected cache to be populated after TrustWorkspace")
	}

	// Write a new trust store directly to disk, bypassing the cache.
	absWs, _ := filepath.Abs(wsRoot)
	newStore := map[string]string{absWs: "stale-hash-12345"}
	data, _ := json.Marshal(newStore)
	os.WriteFile(trustedWorkspacesPath, data, 0o600)

	// Force reload.
	if err := LoadTrustedWorkspaces(); err != nil {
		t.Fatalf("LoadTrustedWorkspaces: %v", err)
	}

	// Cache should now reflect the new data.
	trustedWorkspacesMu.RLock()
	recordedHash := trustedWorkspacesCache[absWs]
	trustedWorkspacesMu.RUnlock()

	if recordedHash != "stale-hash-12345" {
		t.Errorf("expected cache to show stale-hash-12345 after force reload, got %s", recordedHash)
	}
}

func TestTrustWorkspace_IdempotentSameHash(t *testing.T) {
	tmpDir := t.TempDir()
	cleanup := setupTrustStore(t, tmpDir)
	defer cleanup()

	wsRoot := t.TempDir()
	writeWorkspacePolicy(t, wsRoot, configuration.ShellConfig{})

	// Trust twice with the same file.
	if err := TrustWorkspace(wsRoot); err != nil {
		t.Fatalf("first trust: %v", err)
	}
	if err := TrustWorkspace(wsRoot); err != nil {
		t.Fatalf("second trust: %v", err)
	}

	// Should still be trusted.
	trusted, err := IsWorkspaceTrusted(wsRoot)
	if err != nil {
		t.Fatalf("IsWorkspaceTrusted: %v", err)
	}
	if !trusted {
		t.Error("expected trusted after double-trust with same file")
	}
}

func TestIsWorkspaceTrusted_NoFile(t *testing.T) {
	tmpDir := t.TempDir()
	cleanup := setupTrustStore(t, tmpDir)
	defer cleanup()

	wsRoot := t.TempDir()
	// No policy file.

	trusted, err := IsWorkspaceTrusted(wsRoot)
	if err != nil {
		t.Fatalf("IsWorkspaceTrusted with no file: %v", err)
	}
	if trusted {
		t.Error("expected not trusted when policy file doesn't exist")
	}
}

func TestSaveTrustedWorkspaces_EmptyCache(t *testing.T) {
	tmpDir := t.TempDir()
	cleanup := setupTrustStore(t, tmpDir)
	defer cleanup()

	// nil cache → SaveTrustedWorkspaces should be a no-op.
	trustedWorkspacesMu.Lock()
	trustedWorkspacesCache = nil
	trustedWorkspacesMu.Unlock()

	if err := SaveTrustedWorkspaces(); err != nil {
		t.Errorf("SaveTrustedWorkspaces with nil cache should not error, got: %v", err)
	}
}

// contains is a helper for substring matching in warnings.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || (len(s) > 0 && stringContains(s, substr)))
}

// stringContains checks if s contains substr.
func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
