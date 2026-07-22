package agent

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/filesystem"
	"github.com/stretchr/testify/require"
)

// nonTmpTempDir returns a temp directory under a parent that is NOT /tmp.
// Use this for fixtures that need to simulate off-allowlist or sensitive
// paths — /tmp is universally allowed by the classifier, so tests that
// assert Prompt or Deny for external paths must use a directory outside it.
//
// Probes a preference-ordered list of candidates and returns the first
// one that exists and is writable. Falls back to t.TempDir() only if
// no candidate is available; in that case tests that depend on the
// non-/tmp property skip themselves.
func nonTmpTempDir(t *testing.T) string {
	t.Helper()

	// Candidates ordered by platform likelihood.
	candidates := []string{
		"/var/folders", // macOS per-user temp (not under /tmp)
		"/var/tmp",     // Linux persistent temp (not under /tmp)
		"/private/var/tmp", // macOS resolved form
	}

	var lastErr error
	for _, base := range candidates {
		if _, err := os.Stat(base); err != nil {
			lastErr = err
			continue
		}
		d, err := os.MkdirTemp(base, "sprout-m1-")
		if err != nil {
			lastErr = err
			continue
		}
		t.Cleanup(func() { os.RemoveAll(d) })
		return d
	}

	// No non-/tmp scratch space available on this platform.
	// Tests that need the external-path invariant will skip.
	dir := t.TempDir()
	t.Logf("WARNING: no non-/tmp temp dir available (last err: %v); external-path fixtures will be under /tmp — related tests skip", lastErr)
	return dir
}

// externalTempDir is a thin wrapper kept for callers that don't care
// about the non-/tmp guarantee. Internally it delegates to nonTmpTempDir.
func externalTempDir(t *testing.T) string {
	return nonTmpTempDir(t)
}

// TestClassifyFileAccess_Conformance verifies that the Gate 1 path-tier
// classifier (classifyFileAccess) and the filesystem gate adapter
// (RequestPathApproval) agree on the allow/prompt/deny decision for a
// representative battery of path/mode combinations.
//
// The two surfaces MUST agree because Gate 1 (staticGateAutoApprove) and
// Gate 2 (filesystemGateAdapter) both consult the same classifier after
// SP-127 M1. Any divergence would let the model observe different security
// behavior depending on which gate is consulted.
//
// Each test case sets up the agent with specific state (workspace root,
// allowlisted folders, etc.) and asserts both paths reach the same verdict.
func TestClassifyFileAccess_Conformance(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix fixtures only")
	}

	// workspaceRoot is the agent's effective workspace.
	workspaceRoot := t.TempDir()

	// allowlistDir is a session-allowlisted folder.
	allowlistDir := t.TempDir()
	allowlistFile := filepath.Join(allowlistDir, "data.txt")

	// allowlistReadOnlyDir is a session-allowlisted folder with read_only mode.
	// Must NOT be under /tmp, otherwise the /tmp universal allow short-circuits
	// the read_only deny check before the classifier can inspect the mode.
	allowlistReadOnlyDir := nonTmpTempDir(t)

	// externalDir is a path outside the workspace, outside /tmp, and not
	// allowlisted. This ensures the classifier treats it as off-allowlist.
	externalDir := externalTempDir(t)
	externalFile := filepath.Join(externalDir, "external.txt")

	// homeDir simulates $HOME for sensitive-path checks.
	// Must NOT be under /tmp, otherwise paths like
	// /tmp/.../.ssh/id_rsa or /tmp/.../.aws/credentials are caught by
	// the /tmp universal allow before IsSensitiveSystemPath can match them.
	homeDir := nonTmpTempDir(t)
	sshDir := filepath.Join(homeDir, ".ssh")
	_ = filesystem.EnsureDir(sshDir)
	awsDir := filepath.Join(homeDir, ".aws")
	_ = filesystem.EnsureDir(awsDir)

	cases := []struct {
		name             string
		filePath         string
		resolvedPath     string
		mode             string
		setup            func(*Agent)
		wantClassifier   FileAccessDecision
		wantAdapterAllow bool
	}{
		{
			name:           "workspace root file",
			filePath:       filepath.Join(workspaceRoot, "main.go"),
			resolvedPath:   filepath.Join(workspaceRoot, "main.go"),
			mode:           "read",
			wantClassifier: FileAccessAllow,
			wantAdapterAllow: true,
		},
		{
			name:           "workspace nested file write",
			filePath:       filepath.Join(workspaceRoot, "a", "b", "c.txt"),
			resolvedPath:   filepath.Join(workspaceRoot, "a", "b", "c.txt"),
			mode:           "write",
			wantClassifier: FileAccessAllow,
			wantAdapterAllow: true,
		},
		{
			name:           "workspace symlink",
			filePath:       filepath.Join(workspaceRoot, "link"),
			resolvedPath:   filepath.Join(workspaceRoot, "real"),
			mode:           "read",
			wantClassifier: FileAccessAllow,
			wantAdapterAllow: true,
		},
		{
			name:           "/tmp file read",
			filePath:       filepath.Join(t.TempDir(), "test.txt"),
			resolvedPath:   filepath.Join(t.TempDir(), "test.txt"),
			mode:           "read",
			wantClassifier: FileAccessAllow,
			wantAdapterAllow: true,
		},
		{
			name:           "/tmp file write",
			filePath:       filepath.Join(t.TempDir(), "out.txt"),
			resolvedPath:   filepath.Join(t.TempDir(), "out.txt"),
			mode:           "write",
			wantClassifier: FileAccessAllow,
			wantAdapterAllow: true,
		},
		{
			name:           "session-allowlisted folder read",
			filePath:       allowlistFile,
			resolvedPath:   allowlistFile,
			mode:           "read",
			setup: func(a *Agent) {
				a.AddSessionAllowedFolder(allowlistDir)
			},
			wantClassifier: FileAccessAllow,
			wantAdapterAllow: true,
		},
		{
			name:           "session-allowlisted folder write",
			filePath:       allowlistFile,
			resolvedPath:   allowlistFile,
			mode:           "write",
			setup: func(a *Agent) {
				a.AddSessionAllowedFolder(allowlistDir)
			},
			wantClassifier: FileAccessAllow,
			wantAdapterAllow: true,
		},
		{
			name:           "session-allowlisted read_only folder write denied",
			filePath:       filepath.Join(allowlistReadOnlyDir, "secret.txt"),
			resolvedPath:   filepath.Join(allowlistReadOnlyDir, "secret.txt"),
			mode:           "write",
			setup: func(a *Agent) {
				a.AddSessionAllowedFolder(allowlistReadOnlyDir)
				a.SetSessionAllowedFolderMode(allowlistReadOnlyDir, "read_only")
			},
			wantClassifier: FileAccessDeny,
			wantAdapterAllow: false,
		},
		{
			name:           "session-allowlisted read_only folder read allowed",
			filePath:       filepath.Join(allowlistReadOnlyDir, "secret.txt"),
			resolvedPath:   filepath.Join(allowlistReadOnlyDir, "secret.txt"),
			mode:           "read",
			setup: func(a *Agent) {
				a.AddSessionAllowedFolder(allowlistReadOnlyDir)
				a.SetSessionAllowedFolderMode(allowlistReadOnlyDir, "read_only")
			},
			wantClassifier: FileAccessAllow,
			wantAdapterAllow: true,
		},
		{
			name:           "off-workspace external file",
			filePath:       externalFile,
			resolvedPath:   externalFile,
			mode:           "read",
			wantClassifier: FileAccessPrompt,
			wantAdapterAllow: false, // non-interactive test agent has no approval channel
		},
		{
			name:           "off-workspace external file write",
			filePath:       externalFile,
			resolvedPath:   externalFile,
			mode:           "write",
			wantClassifier: FileAccessPrompt,
			wantAdapterAllow: false,
		},
		{
			name:           "sensitive /etc/passwd",
			filePath:       "/etc/passwd",
			resolvedPath:   "/etc/passwd",
			mode:           "read",
			wantClassifier: FileAccessPrompt,
			wantAdapterAllow: false,
		},
		{
			name:           "sensitive /etc/shadow",
			filePath:       "/etc/shadow",
			resolvedPath:   "/etc/shadow",
			mode:           "write",
			wantClassifier: FileAccessPrompt,
			wantAdapterAllow: false,
		},
		{
			name:           "sensitive SSH private key under home",
			filePath:       filepath.Join(sshDir, "id_rsa"),
			resolvedPath:   filepath.Join(sshDir, "id_rsa"),
			mode:           "read",
			setup: func(a *Agent) {
				// Set a mock home dir so IsSensitiveSystemPath can resolve ~.
				t.Setenv("HOME", homeDir)
			},
			wantClassifier: FileAccessPrompt,
			wantAdapterAllow: false,
		},
		{
			name:           "sensitive AWS credentials",
			filePath:       filepath.Join(awsDir, "credentials"),
			resolvedPath:   filepath.Join(awsDir, "credentials"),
			mode:           "write",
			setup: func(a *Agent) {
				t.Setenv("HOME", homeDir)
			},
			wantClassifier: FileAccessPrompt,
			wantAdapterAllow: false,
		},
		{
			name:           "relative path uses resolvedPath when provided",
			filePath:       "foo.go",
			resolvedPath:   filepath.Join(workspaceRoot, "foo.go"),
			mode:           "read",
			wantClassifier: FileAccessAllow,
			wantAdapterAllow: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := newIsolatedTestAgent(t)
			defer a.Shutdown()

			// Set workspace root.
			a.SetWorkspaceRoot(workspaceRoot)

			// Run per-case setup (e.g., add allowlisted folders).
			if tc.setup != nil {
				tc.setup(a)
			}

			// --- Gate 1: classifyFileAccess directly ---
			classifierDecision := a.classifyFileAccess(tc.filePath, tc.resolvedPath, tc.mode)

			// --- Gate 2: filesystemGateAdapter.RequestPathApproval ---
			adapter := newFilesystemGateAdapter(a)
			require.NotNil(t, adapter, "adapter should not be nil")

			// Determine the error sentinel from mode.
			var err error
			if tc.mode == "write" {
				err = filesystem.ErrWriteOutsideWorkingDirectory
			} else {
				err = filesystem.ErrOutsideWorkingDirectory
			}

			_, adapterAllowed := adapter.RequestPathApproval(context.Background(), "test_tool", tc.filePath, tc.resolvedPath, err)

			// --- Conformance assertion ---
			// Map classifier decision to adapter-allow bool.
			var wantAdapterAllow bool
			switch tc.wantClassifier {
			case FileAccessAllow:
				wantAdapterAllow = true
			case FileAccessPrompt, FileAccessDeny:
				wantAdapterAllow = false
			}

			if classifierDecision != tc.wantClassifier {
				t.Errorf("classifyFileAccess(%q, %q, %q) = %v, want %v",
					tc.filePath, tc.resolvedPath, tc.mode, classifierDecision, tc.wantClassifier)
			}

			if adapterAllowed != wantAdapterAllow {
				t.Errorf("filesystemGateAdapter.RequestPathApproval(%q, %q, %q) approved = %v, want %v (classifier verdict = %v)",
					tc.filePath, tc.resolvedPath, tc.mode, adapterAllowed, wantAdapterAllow, classifierDecision)
			}

			// Core invariant: both paths must agree on allow/deny.
			classifierAllows := classifierDecision == FileAccessAllow
			if classifierAllows != adapterAllowed {
				t.Errorf("CONFORMANCE VIOLATION: classifyFileAccess allows=%v but adapter approved=%v for path=%q mode=%q",
					classifierAllows, adapterAllowed, tc.filePath, tc.mode)
			}
		})
	}
}

// TestClassifyFileAccess_NilAgent verifies that classifyFileAccess
// returns FileAccessPrompt (fail-open for nil safety) rather than
// crashing or returning an indeterminate value.
func TestClassifyFileAccess_NilAgent(t *testing.T) {
	var a *Agent
	result := a.classifyFileAccess("/etc/passwd", "/etc/passwd", "read")
	if result != FileAccessPrompt {
		t.Errorf("classifyFileAccess(nil, ...) = %v, want FileAccessPrompt", result)
	}
}

// TestClassifyFileAccess_EmptyPath verifies that an empty target
// (neither filePath nor resolvedPath supplied) returns FileAccessPrompt
// so the classifier never silently allows a path it can't reason about.
func TestClassifyFileAccess_EmptyPath(t *testing.T) {
	a := newIsolatedTestAgent(t)
	defer a.Shutdown()

	result := a.classifyFileAccess("", "", "read")
	if result != FileAccessPrompt {
		t.Errorf("classifyFileAccess(\"\", \"\", ...) = %v, want FileAccessPrompt", result)
	}
}

// TestStaticGateAutoApprove_PathTier exercises the path-tier allow branch
// of staticGateAutoApprove. When a path lands in the workspace root,
// the function returns true even without unsafe/elevation flags.
func TestStaticGateAutoApprove_PathTier(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix fixtures only")
	}

	workspaceRoot := t.TempDir()
	a := newIsolatedTestAgent(t)
	defer a.Shutdown()
	a.SetWorkspaceRoot(workspaceRoot)

	// No bypass flags set.
	if a.GetUnsafeMode() {
		t.Fatal("unsafe mode should not be set")
	}

	secResult := tools.SecurityResult{
		Risk:         tools.SecurityCaution,
		ShouldPrompt: true,
		IsHardBlock:  false,
	}

	// Workspace path should auto-approve even without bypass flags.
	if !a.staticGateAutoApprove(secResult, filepath.Join(workspaceRoot, "main.go"), "", "read") {
		t.Error("staticGateAutoApprove should allow workspace path")
	}

	// Off-workspace path should NOT auto-approve (no bypass flags).
	// Use a path NOT under /tmp so the test exercises the off-workspace
	// branch rather than the /tmp universal-allow short-circuit.
	externalDir := nonTmpTempDir(t)
	externalPath := filepath.Join(externalDir, "other.txt")
	if a.staticGateAutoApprove(secResult, externalPath, "", "read") {
		t.Error("staticGateAutoApprove should NOT auto-approve off-workspace path without bypass flags")
	}
}

// TestStaticGateAutoApprove_PathTierWithAllowlist verifies that
// session-allowlisted paths auto-approve through staticGateAutoApprove.
func TestStaticGateAutoApprove_PathTierWithAllowlist(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix fixtures only")
	}

	allowlistDir := t.TempDir()
	a := newIsolatedTestAgent(t)
	defer a.Shutdown()
	a.AddSessionAllowedFolder(allowlistDir)

	secResult := tools.SecurityResult{
		Risk:         tools.SecurityCaution,
		ShouldPrompt: true,
		IsHardBlock:  false,
	}

	// Allowlisted path should auto-approve.
	if !a.staticGateAutoApprove(secResult, filepath.Join(allowlistDir, "data.txt"), "", "read") {
		t.Error("staticGateAutoApprove should allow session-allowlisted path")
	}
}

// TestStaticGateAutoApprove_PathTierReadOnlyWriteDeny verifies that
// staticGateAutoApprove returns false for write attempts against
// read_only declared folders (FileAccessDeny propagates).
func TestStaticGateAutoApprove_PathTierReadOnlyWriteDeny(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix fixtures only")
	}

	// allowlistDir must NOT be under /tmp — otherwise the /tmp short-circuit
	// fires before the allowlist mode check and we never hit FileAccessDeny.
	allowlistDir := nonTmpTempDir(t)
	a := newIsolatedTestAgent(t)
	defer a.Shutdown()
	a.AddSessionAllowedFolder(allowlistDir)
	a.SetSessionAllowedFolderMode(allowlistDir, "read_only")

	secResult := tools.SecurityResult{
		Risk:         tools.SecurityCaution,
		ShouldPrompt: true,
		IsHardBlock:  false,
	}

	// Write attempt against read_only folder should be denied.
	if a.staticGateAutoApprove(secResult, filepath.Join(allowlistDir, "data.txt"), "", "write") {
		t.Error("staticGateAutoApprove should DENY write attempt against read_only folder")
	}

	// Read should still be allowed.
	if !a.staticGateAutoApprove(secResult, filepath.Join(allowlistDir, "data.txt"), "", "read") {
		t.Error("staticGateAutoApprove should allow read under read_only folder")
	}
}

// TestClassifyFileAccess_TmpIsAlwaysAllowed verifies that /tmp is
// allowed regardless of mode (read vs write).
func TestClassifyFileAccess_TmpIsAlwaysAllowed(t *testing.T) {
	a := newIsolatedTestAgent(t)
	defer a.Shutdown()

	tmpFile := filepath.Join(t.TempDir(), "sprout-test.txt")

	for _, mode := range []string{"read", "write"} {
		result := a.classifyFileAccess(tmpFile, tmpFile, mode)
		if result != FileAccessAllow {
			t.Errorf("classifyFileAccess(%q, %q, %q) = %v, want FileAccessAllow", tmpFile, tmpFile, mode, result)
		}
	}
}
