//go:build !js

// Integration tests for SP-049 Phase 2 — workspace overlay and trust system.
//
// These tests exercise the full overlay/trust workflow end-to-end, including
// pattern merging across modes, trust store persistence, and edge cases.
//
// NOTE: Tests are NOT parallel because they mutate shared global state
// (trustedWorkspacesPath, trustedWorkspacesCache) via setupTrustStore().
package tools

import (
	"os"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// ============================================================================
// Overlay + config integration
// ============================================================================

func TestIntegration_Overlay_MergedConfigContainsWorkspacePatterns(t *testing.T) {
	tmpDir := t.TempDir()
	cleanup := setupTrustStore(t, tmpDir)
	defer cleanup()

	wsRoot := t.TempDir()
	writeWorkspacePolicy(t, wsRoot, patternsToConfig(
		[]configuration.ShellPattern{{Match: "ws-safe", Kind: "prefix"}},
		[]configuration.ShellPattern{{Match: "ws-danger", Kind: "prefix"}, {Match: "sudo ", Kind: "prefix"}},
	))

	userCfg := configuration.ShellConfig{
		WorkspaceOverlay: configuration.WorkspaceOverlayConfig{Mode: "tighten_only"},
		UserSafePatterns: []configuration.ShellPattern{{Match: "cat", Kind: "prefix"}},
		UserDangerousPatterns: []configuration.ShellPattern{{Match: "rm ", Kind: "prefix"}},
	}

	merged, warnings := LoadWorkspaceOverlay(wsRoot, userCfg)

	// Dangerous patterns: user's "rm " + workspace's "ws-danger" + "sudo ".
	if len(merged.UserDangerousPatterns) != 3 {
		t.Errorf("expected 3 dangerous patterns, got %d: %v", len(merged.UserDangerousPatterns), merged.UserDangerousPatterns)
	}

	// Safe patterns: only user's "cat" (workspace safe patterns ignored in tighten_only).
	if len(merged.UserSafePatterns) != 1 {
		t.Errorf("expected 1 safe pattern (user only), got %d: %v", len(merged.UserSafePatterns), merged.UserSafePatterns)
	}
	if merged.UserSafePatterns[0].Match != "cat" {
		t.Errorf("expected 'cat' safe pattern, got %q", merged.UserSafePatterns[0].Match)
	}

	// Should warn about ignored safe patterns.
	if len(warnings) != 1 {
		t.Errorf("expected 1 warning, got %d: %v", len(warnings), warnings)
	}
	if !strings.Contains(warnings[0], "ignoring") {
		t.Errorf("expected warning about ignoring safe patterns, got: %s", warnings[0])
	}
}

func TestIntegration_Overlay_TrustWorkflow(t *testing.T) {
	tmpDir := t.TempDir()
	cleanup := setupTrustStore(t, tmpDir)
	defer cleanup()

	wsRoot := t.TempDir()
	writeWorkspacePolicy(t, wsRoot, patternsToConfig(
		[]configuration.ShellPattern{{Match: "make", Kind: "prefix"}, {Match: "go test", Kind: "prefix"}},
		[]configuration.ShellPattern{{Match: "make clean", Kind: "prefix"}},
	))

	// Step 1: Trust the workspace.
	if err := TrustWorkspace(wsRoot); err != nil {
		t.Fatalf("TrustWorkspace: %v", err)
	}

	// Step 2: Verify trust.
	trusted, err := IsWorkspaceTrusted(wsRoot)
	if err != nil {
		t.Fatalf("IsWorkspaceTrusted: %v", err)
	}
	if !trusted {
		t.Fatal("expected workspace to be trusted")
	}

	// Step 3: Load overlay in trusted mode — full merge.
	userCfg := configuration.ShellConfig{
		WorkspaceOverlay: configuration.WorkspaceOverlayConfig{Mode: "trusted"},
		UserSafePatterns: []configuration.ShellPattern{{Match: "ls", Kind: "prefix"}},
		UserDangerousPatterns: []configuration.ShellPattern{{Match: "rm ", Kind: "prefix"}},
	}

	merged, warnings := LoadWorkspaceOverlay(wsRoot, userCfg)

	if len(warnings) != 0 {
		t.Errorf("expected no warnings in trusted mode, got %d: %v", len(warnings), warnings)
	}

	// Safe: user's "ls" + workspace's "make" + "go test" = 3.
	if len(merged.UserSafePatterns) != 3 {
		t.Errorf("expected 3 safe patterns (user + workspace), got %d: %v", len(merged.UserSafePatterns), merged.UserSafePatterns)
	}

	// Dangerous: user's "rm " + workspace's "make clean" = 2.
	if len(merged.UserDangerousPatterns) != 2 {
		t.Errorf("expected 2 dangerous patterns, got %d: %v", len(merged.UserDangerousPatterns), merged.UserDangerousPatterns)
	}
}

func TestIntegration_Overlay_TrustHashChange(t *testing.T) {
	tmpDir := t.TempDir()
	cleanup := setupTrustStore(t, tmpDir)
	defer cleanup()

	wsRoot := t.TempDir()
	writeWorkspacePolicy(t, wsRoot, patternsToConfig(
		[]configuration.ShellPattern{{Match: "safe-v1", Kind: "prefix"}},
		[]configuration.ShellPattern{{Match: "danger-v1", Kind: "prefix"}},
	))

	// Trust with v1.
	if err := TrustWorkspace(wsRoot); err != nil {
		t.Fatalf("TrustWorkspace: %v", err)
	}

	// Verify trusted.
	trusted, _ := IsWorkspaceTrusted(wsRoot) // Error only on path resolution failure (unlikely for t.TempDir())
	if !trusted {
		t.Fatal("expected trusted after TrustWorkspace")
	}

	// Modify the policy file (hash changes).
	writeWorkspacePolicy(t, wsRoot, patternsToConfig(
		[]configuration.ShellPattern{{Match: "safe-v2", Kind: "prefix"}},
		[]configuration.ShellPattern{{Match: "danger-v2", Kind: "prefix"}},
	))

	// Trust check should now fail (hash mismatch).
	trusted, _ = IsWorkspaceTrusted(wsRoot) // Error only on path resolution failure (unlikely for t.TempDir())
	if trusted {
		t.Error("expected NOT trusted after file modification")
	}

	// Load in trusted mode — should fall back to tighten_only.
	userCfg := configuration.ShellConfig{
		WorkspaceOverlay: configuration.WorkspaceOverlayConfig{Mode: "trusted"},
		UserSafePatterns: []configuration.ShellPattern{{Match: "user-safe", Kind: "prefix"}},
	}

	merged, warnings := LoadWorkspaceOverlay(wsRoot, userCfg)

	// Safe: only user's pattern (workspace safe ignored due to fallback).
	if len(merged.UserSafePatterns) != 1 {
		t.Errorf("expected 1 safe pattern (fallback to tighten_only), got %d: %v", len(merged.UserSafePatterns), merged.UserSafePatterns)
	}
	if merged.UserSafePatterns[0].Match != "user-safe" {
		t.Errorf("expected user-safe pattern, got %q", merged.UserSafePatterns[0].Match)
	}

	// Dangerous: workspace's dangerous patterns still honored (v2).
	if len(merged.UserDangerousPatterns) != 1 || merged.UserDangerousPatterns[0].Match != "danger-v2" {
		t.Errorf("expected danger-v2 pattern, got %v", merged.UserDangerousPatterns)
	}

	// Should warn about trust failure.
	found := false
	for _, w := range warnings {
		if strings.Contains(w, "not trusted") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected warning about untrusted workspace, got: %v", warnings)
	}
}

func TestIntegration_Overlay_IgnoreMode(t *testing.T) {
	tmpDir := t.TempDir()
	cleanup := setupTrustStore(t, tmpDir)
	defer cleanup()

	wsRoot := t.TempDir()
	writeWorkspacePolicy(t, wsRoot, patternsToConfig(
		[]configuration.ShellPattern{{Match: "ws-safe", Kind: "prefix"}},
		[]configuration.ShellPattern{{Match: "ws-danger", Kind: "prefix"}},
	))

	userCfg := configuration.ShellConfig{
		WorkspaceOverlay: configuration.WorkspaceOverlayConfig{Mode: "ignore"},
		UserSafePatterns: []configuration.ShellPattern{{Match: "user-safe", Kind: "prefix"}},
		UserDangerousPatterns: []configuration.ShellPattern{{Match: "user-danger", Kind: "prefix"}},
	}

	merged, warnings := LoadWorkspaceOverlay(wsRoot, userCfg)

	if len(warnings) != 0 {
		t.Errorf("expected no warnings in ignore mode, got %d: %v", len(warnings), warnings)
	}
	if len(merged.UserSafePatterns) != 1 {
		t.Errorf("expected user-safe only, got %v", merged.UserSafePatterns)
	}
	if len(merged.UserDangerousPatterns) != 1 {
		t.Errorf("expected user-danger only, got %v", merged.UserDangerousPatterns)
	}
}

func TestIntegration_Overlay_DefaultModeEmptyString(t *testing.T) {
	tmpDir := t.TempDir()
	cleanup := setupTrustStore(t, tmpDir)
	defer cleanup()

	wsRoot := t.TempDir()
	writeWorkspacePolicy(t, wsRoot, patternsToConfig(
		[]configuration.ShellPattern{{Match: "ws-safe", Kind: "prefix"}},
		[]configuration.ShellPattern{{Match: "ws-danger", Kind: "prefix"}},
	))

	// Empty mode should default to tighten_only.
	userCfg := configuration.ShellConfig{
		WorkspaceOverlay: configuration.WorkspaceOverlayConfig{Mode: ""},
		UserSafePatterns: []configuration.ShellPattern{{Match: "user-safe", Kind: "prefix"}},
	}

	merged, warnings := LoadWorkspaceOverlay(wsRoot, userCfg)

	if len(merged.UserSafePatterns) != 1 || merged.UserSafePatterns[0].Match != "user-safe" {
		t.Errorf("expected only user safe pattern (default → tighten_only), got %v", merged.UserSafePatterns)
	}
	if len(merged.UserDangerousPatterns) != 1 || merged.UserDangerousPatterns[0].Match != "ws-danger" {
		t.Errorf("expected ws-danger pattern, got %v", merged.UserDangerousPatterns)
	}
	if len(warnings) == 0 {
		t.Error("expected warning about ignored safe patterns with default mode")
	}
}

func TestIntegration_Overlay_TrustedModeNoFile(t *testing.T) {
	tmpDir := t.TempDir()
	cleanup := setupTrustStore(t, tmpDir)
	defer cleanup()

	wsRoot := t.TempDir()
	// No policy file created.

	userCfg := configuration.ShellConfig{
		WorkspaceOverlay: configuration.WorkspaceOverlayConfig{Mode: "trusted"},
		UserSafePatterns: []configuration.ShellPattern{{Match: "user-safe", Kind: "prefix"}},
	}

	merged, warnings := LoadWorkspaceOverlay(wsRoot, userCfg)

	// Missing file in trusted mode should return user config unchanged.
	if len(merged.UserSafePatterns) != 1 || merged.UserSafePatterns[0].Match != "user-safe" {
		t.Errorf("expected user config unchanged, got %v", merged.UserSafePatterns)
	}
	// Missing file is silently ignored (no warning) — consistent with existing behavior.
	if len(warnings) != 0 {
		t.Errorf("expected no warnings for missing file, got %d: %v", len(warnings), warnings)
	}
}

// ============================================================================
// Pattern resolution order (deferred — wired up in SP-068)
// ============================================================================

func TestIntegration_Resolution_UserSafeOverridesCaution(t *testing.T) {
	t.Skip("deferred to SP-068: user pattern resolution not yet wired into classifier")
}

func TestIntegration_Resolution_UserSafeDoesNotOverrideDangerous(t *testing.T) {
	t.Skip("deferred to SP-068: user pattern resolution not yet wired into classifier")
}

func TestIntegration_Resolution_HardInvariant(t *testing.T) {
	t.Skip("deferred to SP-068: user pattern resolution not yet wired into classifier")
}

func TestIntegration_Resolution_UserDangerousEscalatesCaution(t *testing.T) {
	t.Skip("deferred to SP-068: user pattern resolution not yet wired into classifier")
}

// ============================================================================
// CLI + trust management
// ============================================================================

func TestIntegration_CLI_ExportImportRoundTrip(t *testing.T) {
	t.Skip("deferred: ExportShellPolicy/ImportShellPolicy not yet implemented")
}

func TestIntegration_CLI_TrustUntrust(t *testing.T) {
	tmpDir := t.TempDir()
	cleanup := setupTrustStore(t, tmpDir)
	defer cleanup()

	wsRoot := t.TempDir()
	writeWorkspacePolicy(t, wsRoot, patternsToConfig(
		[]configuration.ShellPattern{{Match: "make", Kind: "prefix"}},
		[]configuration.ShellPattern{{Match: "make clean", Kind: "prefix"}},
	))

	// 1. Trust.
	if err := TrustWorkspace(wsRoot); err != nil {
		t.Fatalf("TrustWorkspace: %v", err)
	}
	trusted, _ := IsWorkspaceTrusted(wsRoot) // Error only on path resolution failure (unlikely for t.TempDir())
	if !trusted {
		t.Fatal("expected trusted after TrustWorkspace")
	}

	// 2. Untrust.
	if err := UntrustWorkspace(wsRoot); err != nil {
		t.Fatalf("UntrustWorkspace: %v", err)
	}
	trusted, _ = IsWorkspaceTrusted(wsRoot) // Error only on path resolution failure (unlikely for t.TempDir())
	if trusted {
		t.Error("expected NOT trusted after UntrustWorkspace")
	}

	// 3. Re-trust.
	if err := TrustWorkspace(wsRoot); err != nil {
		t.Fatalf("re-TrustWorkspace: %v", err)
	}
	trusted, _ = IsWorkspaceTrusted(wsRoot) // Error only on path resolution failure (unlikely for t.TempDir())
	if !trusted {
		t.Error("expected trusted after re-trust")
	}

	// 4. Verify trust store file exists on disk.
	if _, err := os.Stat(trustedWorkspacesPath); err != nil {
		t.Fatalf("trust store file should exist on disk: %v", err)
	}
}

func TestIntegration_CLI_UntrustAll(t *testing.T) {
	tmpDir := t.TempDir()
	cleanup := setupTrustStore(t, tmpDir)
	defer cleanup()

	// Create and trust three workspaces.
	var wsRoots []string
	for i := 0; i < 3; i++ {
		wsRoot := t.TempDir()
		writeWorkspacePolicy(t, wsRoot, configuration.ShellConfig{})
		if err := TrustWorkspace(wsRoot); err != nil {
			t.Fatalf("trust ws%d: %v", i, err)
		}
		wsRoots = append(wsRoots, wsRoot)
	}

	// Verify all trusted.
	for i, ws := range wsRoots {
		trusted, _ := IsWorkspaceTrusted(ws) // Error only on path resolution failure (unlikely for t.TempDir())
		if !trusted {
			t.Errorf("ws%d should be trusted", i)
		}
	}

	// Untrust all.
	if err := UntrustAllWorkspaces(); err != nil {
		t.Fatalf("UntrustAllWorkspaces: %v", err)
	}

	// Verify all untrusted.
	for i, ws := range wsRoots {
		trusted, _ := IsWorkspaceTrusted(ws) // Error only on path resolution failure (unlikely for t.TempDir())
		if trusted {
			t.Errorf("ws%d should be untrusted after UntrustAllWorkspaces", i)
		}
	}
}

// ============================================================================
// E2E overlay classification
// ============================================================================

func TestIntegration_E2E_TightenOnlyBlocksSafePatterns(t *testing.T) {
	tmpDir := t.TempDir()
	cleanup := setupTrustStore(t, tmpDir)
	defer cleanup()

	wsRoot := t.TempDir()
	writeWorkspacePolicy(t, wsRoot, patternsToConfig(
		[]configuration.ShellPattern{{Match: "make", Kind: "prefix"}},
		[]configuration.ShellPattern{{Match: "make clean", Kind: "prefix"}},
	))

	userCfg := configuration.ShellConfig{
		WorkspaceOverlay: configuration.WorkspaceOverlayConfig{Mode: "tighten_only"},
		UserSafePatterns: []configuration.ShellPattern{{Match: "ls", Kind: "prefix"}},
	}

	merged, _ := LoadWorkspaceOverlay(wsRoot, userCfg)

	// The workspace's safe pattern ("make") must NOT appear in the merged config.
	for _, p := range merged.UserSafePatterns {
		if p.Match == "make" {
			t.Error("tighten_only should block workspace safe patterns, but found 'make' in safe list")
		}
	}

	// Only the user's safe pattern should remain.
	if len(merged.UserSafePatterns) != 1 {
		t.Errorf("expected 1 safe pattern, got %d: %v", len(merged.UserSafePatterns), merged.UserSafePatterns)
	}
}

func TestIntegration_E2E_TrustedAllowsSafePatterns(t *testing.T) {
	tmpDir := t.TempDir()
	cleanup := setupTrustStore(t, tmpDir)
	defer cleanup()

	wsRoot := t.TempDir()
	writeWorkspacePolicy(t, wsRoot, patternsToConfig(
		[]configuration.ShellPattern{{Match: "npm test", Kind: "prefix"}, {Match: "npm run lint", Kind: "prefix"}},
		[]configuration.ShellPattern{{Match: "npm prune", Kind: "prefix"}},
	))

	// Trust before loading.
	if err := TrustWorkspace(wsRoot); err != nil {
		t.Fatalf("TrustWorkspace: %v", err)
	}

	userCfg := configuration.ShellConfig{
		WorkspaceOverlay: configuration.WorkspaceOverlayConfig{Mode: "trusted"},
		UserSafePatterns: []configuration.ShellPattern{{Match: "git", Kind: "prefix"}},
	}

	merged, warnings := LoadWorkspaceOverlay(wsRoot, userCfg)

	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got %d: %v", len(warnings), warnings)
	}

	// Safe: user's "git" + workspace's "npm test" + "npm run lint" = 3.
	if len(merged.UserSafePatterns) != 3 {
		t.Errorf("expected 3 safe patterns, got %d: %v", len(merged.UserSafePatterns), merged.UserSafePatterns)
	}

	// Verify all expected patterns are present.
	safeMatches := make(map[string]bool)
	for _, p := range merged.UserSafePatterns {
		safeMatches[p.Match] = true
	}
	for _, expected := range []string{"git", "npm test", "npm run lint"} {
		if !safeMatches[expected] {
			t.Errorf("expected safe pattern %q, got: %v", expected, merged.UserSafePatterns)
		}
	}

	// Dangerous: workspace's "npm prune".
	if len(merged.UserDangerousPatterns) != 1 {
		t.Errorf("expected 1 dangerous pattern, got %d: %v", len(merged.UserDangerousPatterns), merged.UserDangerousPatterns)
	}
}

func TestIntegration_E2E_PatternMatchingDirectly(t *testing.T) {
	// Table-driven test verifying that workspace pattern strings from merged
	// configs contain expected match strings (sanity check on pattern merging).
	tmpDir := t.TempDir()
	cleanup := setupTrustStore(t, tmpDir)
	defer cleanup()

	tests := []struct {
		name      string
		mode      string
		userSafe  []string
		userDanger []string
		wsSafe    []string
		wsDanger  []string
		wantSafe  []string
		wantDanger []string
		trust     bool
	}{
		{
			name:       "simple tighten_only merge",
			mode:       "tighten_only",
			userSafe:   []string{"cat", "ls"},
			userDanger: []string{"rm "},
			wsSafe:     []string{"make"},
			wsDanger:   []string{"make clean"},
			wantSafe:   []string{"cat", "ls"},
			wantDanger: []string{"rm ", "make clean"},
			trust:      false,
		},
		{
			name:       "trusted with match",
			mode:       "trusted",
			userSafe:   []string{"ls"},
			userDanger: []string{"rm "},
			wsSafe:     []string{"make"},
			wsDanger:   []string{"make clean"},
			wantSafe:   []string{"ls", "make"},
			wantDanger: []string{"rm ", "make clean"},
			trust:      true,
		},
		{
			name:       "trusted without trust",
			mode:       "trusted",
			userSafe:   []string{"ls"},
			userDanger: []string{"rm "},
			wsSafe:     []string{"make"},
			wsDanger:   []string{"make clean"},
			wantSafe:   []string{"ls"},
			wantDanger: []string{"rm ", "make clean"},
			trust:      false,
		},
		{
			name:       "ignore mode",
			mode:       "ignore",
			userSafe:   []string{"ls"},
			userDanger: []string{"rm "},
			wsSafe:     []string{"make"},
			wsDanger:   []string{"make clean"},
			wantSafe:   []string{"ls"},
			wantDanger: []string{"rm "},
			trust:      false,
		},
		{
			name:       "empty workspace patterns",
			mode:       "tighten_only",
			userSafe:   []string{"ls"},
			userDanger: []string{"rm "},
			wsSafe:     nil,
			wsDanger:   nil,
			wantSafe:   []string{"ls"},
			wantDanger: []string{"rm "},
			trust:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wsRoot := t.TempDir()
			wsSafeP := make([]configuration.ShellPattern, len(tt.wsSafe))
			for i, m := range tt.wsSafe {
				wsSafeP[i] = configuration.ShellPattern{Match: m, Kind: "prefix"}
			}
			wsDangerP := make([]configuration.ShellPattern, len(tt.wsDanger))
			for i, m := range tt.wsDanger {
				wsDangerP[i] = configuration.ShellPattern{Match: m, Kind: "prefix"}
			}
			writeWorkspacePolicy(t, wsRoot, patternsToConfig(wsSafeP, wsDangerP))

			if tt.trust {
				if err := TrustWorkspace(wsRoot); err != nil {
					t.Fatalf("TrustWorkspace: %v", err)
				}
			}

			userSafeP := make([]configuration.ShellPattern, len(tt.userSafe))
			for i, m := range tt.userSafe {
				userSafeP[i] = configuration.ShellPattern{Match: m, Kind: "prefix"}
			}
			userDangerP := make([]configuration.ShellPattern, len(tt.userDanger))
			for i, m := range tt.userDanger {
				userDangerP[i] = configuration.ShellPattern{Match: m, Kind: "prefix"}
			}

			userCfg := configuration.ShellConfig{
				WorkspaceOverlay:      configuration.WorkspaceOverlayConfig{Mode: tt.mode},
				UserSafePatterns:      userSafeP,
				UserDangerousPatterns: userDangerP,
			}

			merged, _ := LoadWorkspaceOverlay(wsRoot, userCfg)

			// Verify safe patterns.
			if len(merged.UserSafePatterns) != len(tt.wantSafe) {
				t.Errorf("expected %d safe patterns, got %d: %v", len(tt.wantSafe), len(merged.UserSafePatterns), merged.UserSafePatterns)
			}
			if len(merged.UserSafePatterns) == len(tt.wantSafe) {
				for i, want := range tt.wantSafe {
					if merged.UserSafePatterns[i].Match != want {
						t.Errorf("safe pattern[%d]: expected %q, got %q", i, want, merged.UserSafePatterns[i].Match)
					}
				}
			}

			// Verify dangerous patterns.
			if len(merged.UserDangerousPatterns) != len(tt.wantDanger) {
				t.Errorf("expected %d dangerous patterns, got %d: %v", len(tt.wantDanger), len(merged.UserDangerousPatterns), merged.UserDangerousPatterns)
			}
			if len(merged.UserDangerousPatterns) == len(tt.wantDanger) {
				for i, want := range tt.wantDanger {
					if merged.UserDangerousPatterns[i].Match != want {
						t.Errorf("dangerous pattern[%d]: expected %q, got %q", i, want, merged.UserDangerousPatterns[i].Match)
					}
				}
			}
		})
	}
}

// ============================================================================
// Additional edge cases
// ============================================================================

func TestIntegration_Overlay_MultipleLoadsWithSameConfig(t *testing.T) {
	tmpDir := t.TempDir()
	cleanup := setupTrustStore(t, tmpDir)
	defer cleanup()

	wsRoot := t.TempDir()
	writeWorkspacePolicy(t, wsRoot, patternsToConfig(
		[]configuration.ShellPattern{{Match: "make", Kind: "prefix"}},
		[]configuration.ShellPattern{{Match: "make clean", Kind: "prefix"}},
	))

	if err := TrustWorkspace(wsRoot); err != nil {
		t.Fatalf("TrustWorkspace: %v", err)
	}

	userCfg := configuration.ShellConfig{
		WorkspaceOverlay: configuration.WorkspaceOverlayConfig{Mode: "trusted"},
		UserSafePatterns: []configuration.ShellPattern{{Match: "ls", Kind: "prefix"}},
	}

	// Baseline: load once to establish expected counts.
	merged, warnings := LoadWorkspaceOverlay(wsRoot, userCfg)
	if len(warnings) != 0 {
		t.Fatalf("first load: unexpected warnings: %v", warnings)
	}
	wantSafe := len(merged.UserSafePatterns)
	wantDanger := len(merged.UserDangerousPatterns)

	// Subsequent loads must return identical counts.
	for i := 1; i < 5; i++ {
		merged, warnings = LoadWorkspaceOverlay(wsRoot, userCfg)
		if len(warnings) != 0 {
			t.Errorf("iteration %d: unexpected warnings: %v", i, warnings)
		}
		if len(merged.UserSafePatterns) != wantSafe {
			t.Errorf("iteration %d: safe pattern count changed from %d to %d", i, wantSafe, len(merged.UserSafePatterns))
		}
		if len(merged.UserDangerousPatterns) != wantDanger {
			t.Errorf("iteration %d: dangerous pattern count changed from %d to %d", i, wantDanger, len(merged.UserDangerousPatterns))
		}
	}
}

func TestIntegration_Overlay_UserConfigNotMutated(t *testing.T) {
	tmpDir := t.TempDir()
	cleanup := setupTrustStore(t, tmpDir)
	defer cleanup()

	wsRoot := t.TempDir()
	writeWorkspacePolicy(t, wsRoot, patternsToConfig(
		[]configuration.ShellPattern{{Match: "ws-safe", Kind: "prefix"}},
		[]configuration.ShellPattern{{Match: "ws-danger", Kind: "prefix"}},
	))

	userSafeOrig := []configuration.ShellPattern{{Match: "user-safe", Kind: "prefix"}}
	userDangerOrig := []configuration.ShellPattern{{Match: "user-danger", Kind: "prefix"}}
	userCfg := configuration.ShellConfig{
		WorkspaceOverlay: configuration.WorkspaceOverlayConfig{Mode: "trusted"},
		UserSafePatterns: userSafeOrig,
		UserDangerousPatterns: userDangerOrig,
	}

	// Trust so we get full overlay.
	if err := TrustWorkspace(wsRoot); err != nil {
		t.Fatalf("TrustWorkspace: %v", err)
	}

	merged, _ := LoadWorkspaceOverlay(wsRoot, userCfg)

	// Mutate the merged result.
	merged.UserSafePatterns[0].Match = "MUTATED-SAFE"
	merged.UserDangerousPatterns[0].Match = "MUTATED-DANGER"

	// Original user config must be unchanged.
	if userCfg.UserSafePatterns[0].Match != "user-safe" {
		t.Errorf("user safe pattern was mutated: %q", userCfg.UserSafePatterns[0].Match)
	}
	if userCfg.UserDangerousPatterns[0].Match != "user-danger" {
		t.Errorf("user dangerous pattern was mutated: %q", userCfg.UserDangerousPatterns[0].Match)
	}
	if userSafeOrig[0].Match != "user-safe" {
		t.Errorf("original safe slice was mutated: %q", userSafeOrig[0].Match)
	}
	if userDangerOrig[0].Match != "user-danger" {
		t.Errorf("original dangerous slice was mutated: %q", userDangerOrig[0].Match)
	}

	// Mutating merged safe[1] (workspace's "ws-safe") should not affect user config.
	merged.UserSafePatterns[1].Match = "MUTATED-WS-SAFE"
	if userCfg.UserSafePatterns[0].Match != "user-safe" {
		t.Error("workspace safe pattern mutation leaked into user config")
	}
}
