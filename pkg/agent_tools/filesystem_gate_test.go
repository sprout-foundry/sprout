package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/filesystem"
)

// recordingGate is a FilesystemGate that captures the arguments the
// helper passes and returns a pre-programmed decision. Used by every
// test in this file to drive withFilesystemApproval without standing
// up a real *Agent.
type recordingGate struct {
	calls            int
	lastToolName     string
	lastFilePath     string
	lastResolvedPath string
	lastErr          error
	approveDecision  bool
	returnedCtx      context.Context
}

func (g *recordingGate) RequestPathApproval(ctx context.Context, toolName, filePath, resolvedPath string, err error) (context.Context, bool) {
	g.calls++
	g.lastToolName = toolName
	g.lastFilePath = filePath
	g.lastResolvedPath = resolvedPath
	g.lastErr = err
	if g.returnedCtx != nil {
		return g.returnedCtx, g.approveDecision
	}
	return ctx, g.approveDecision
}

func TestWithFilesystemApproval_NoGate_NoErrorPassesThrough(t *testing.T) {
	// When no gate is wired (the default for handler tests / unit
	// tests), withFilesystemApproval must short-circuit on success
	// without calling the gate.
	called := false
	result, err := withFilesystemApproval[string](
		context.Background(), nil, "write_file", "/tmp/x.txt",
		func(ctx context.Context) (string, error) {
			called = true
			return "ok", nil
		},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "ok" {
		t.Errorf("result = %q, want %q", result, "ok")
	}
	if !called {
		t.Error("op should have been called once")
	}
}

func TestWithFilesystemApproval_NoGate_PreservesError(t *testing.T) {
	// Without a gate, an off-workspace error must propagate to the
	// caller unchanged — historical behavior for tests and internal
	// callers that don't go through the agent dispatch path.
	want := filesystem.ErrOutsideWorkingDirectory
	called := false
	result, err := withFilesystemApproval[string](
		context.Background(), nil, "write_file", "/tmp/x.txt",
		func(ctx context.Context) (string, error) {
			called = true
			return "", want
		},
	)
	if !errors.Is(err, want) {
		t.Errorf("err = %v, want %v", err, want)
	}
	if result != "" {
		t.Errorf("result = %q, want empty", result)
	}
	if !called {
		t.Error("op should have been called once (no retry without gate)")
	}
}

func TestWithFilesystemApproval_ApproveOnce_RetriesWithBypassCtx(t *testing.T) {
	// User approves once: op is retried exactly once with the
	// (possibly bypass-wrapped) ctx the gate returned. The retry
	// succeeds, so the final result reflects the second call.
	gate := &recordingGate{
		approveDecision: true,
		returnedCtx:     filesystem.WithSecurityBypass(context.Background()),
	}
	calls := 0
	result, err := withFilesystemApproval[string](
		context.Background(), gate, "write_file", "/tmp/x.txt",
		func(ctx context.Context) (string, error) {
			calls++
			if calls == 1 {
				return "", filesystem.ErrOutsideWorkingDirectory
			}
			if !filesystem.SecurityBypassEnabled(ctx) {
				t.Error("retry should have been called with a bypass-wrapped ctx")
			}
			return "ok", nil
		},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "ok" {
		t.Errorf("result = %q, want %q", result, "ok")
	}
	if calls != 2 {
		t.Errorf("op should have been called twice, got %d", calls)
	}
	if gate.calls != 1 {
		t.Errorf("gate should have been called once, got %d", gate.calls)
	}
	if gate.lastToolName != "write_file" {
		t.Errorf("gate toolName = %q, want write_file", gate.lastToolName)
	}
	if gate.lastFilePath != "/tmp/x.txt" {
		t.Errorf("gate filePath = %q, want /tmp/x.txt", gate.lastFilePath)
	}
}

func TestWithFilesystemApproval_Deny_PreservesErrorAndSkipsRetry(t *testing.T) {
	// User denies: op is NOT retried; the original error is returned
	// to the caller verbatim. This is the path the model sees when
	// the user clicks Deny — the tool result is the resolve error
	// and the model has to pick a different approach.
	gate := &recordingGate{approveDecision: false}
	calls := 0
	_, err := withFilesystemApproval[string](
		context.Background(), gate, "write_file", "/tmp/x.txt",
		func(ctx context.Context) (string, error) {
			calls++
			return "", filesystem.ErrWriteOutsideWorkingDirectory
		},
	)
	if !errors.Is(err, filesystem.ErrWriteOutsideWorkingDirectory) {
		t.Errorf("err = %v, want ErrWriteOutsideWorkingDirectory", err)
	}
	if calls != 1 {
		t.Errorf("op should have been called once on deny, got %d", calls)
	}
}

func TestWithFilesystemApproval_NonFilesystemError_DoesNotConsultGate(t *testing.T) {
	// Only the two filesystem sentinels trigger the gate; ordinary
	// errors (file-not-found, permission denied, malformed path)
	// propagate unchanged. Without this short-circuit, a bad-mode
	// write would still pop a permission dialog the user can't act
	// on.
	gate := &recordingGate{approveDecision: true}
	other := errors.New("permission denied")
	_, err := withFilesystemApproval[string](
		context.Background(), gate, "write_file", "/tmp/x.txt",
		func(ctx context.Context) (string, error) {
			return "", other
		},
	)
	if !errors.Is(err, other) {
		t.Errorf("err = %v, want %v", err, other)
	}
	if gate.calls != 0 {
		t.Errorf("gate should NOT be consulted for non-filesystem errors, got %d calls", gate.calls)
	}
}

func TestWithFilesystemApproval_GenericResultType(t *testing.T) {
	// Confirm the helper is generic over the result type — used by
	// EditFile's wrapper, which returns (cleanPath, originalMode)
	// rather than a string.
	type editResolve struct {
		path string
		mode int
	}
	gate := &recordingGate{
		approveDecision: true,
		returnedCtx:     filesystem.WithSecurityBypass(context.Background()),
	}
	calls := 0
	res, err := withFilesystemApproval[editResolve](
		context.Background(), gate, "edit_file", "/tmp/x.go",
		func(ctx context.Context) (editResolve, error) {
			calls++
			if calls == 1 {
				return editResolve{}, filesystem.ErrOutsideWorkingDirectory
			}
			return editResolve{path: "/tmp/x.go", mode: 0o644}, nil
		},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.path != "/tmp/x.go" || res.mode != 0o644 {
		t.Errorf("res = %+v, want {/tmp/x.go 0644}", res)
	}
	if calls != 2 {
		t.Errorf("op should have been called twice, got %d", calls)
	}
}

// TestWithFilesystemApproval_RetryFailureReturnsRetryError verifies that
// if the retry itself fails (after approval), the retry's error is
// returned — not the original resolve error. This matters for paths
// where approval is granted but the underlying fs op still fails
// (e.g., the path is read-only, the parent dir is missing on retry).
func TestWithFilesystemApproval_RetryFailureReturnsRetryError(t *testing.T) {
	gate := &recordingGate{
		approveDecision: true,
		returnedCtx:     filesystem.WithSecurityBypass(context.Background()),
	}
	retryErr := errors.New("retry: read-only filesystem")
	calls := 0
	_, err := withFilesystemApproval[string](
		context.Background(), gate, "write_file", "/tmp/x.txt",
		func(ctx context.Context) (string, error) {
			calls++
			if calls == 1 {
				return "", filesystem.ErrOutsideWorkingDirectory
			}
			return "", retryErr
		},
	)
	if !errors.Is(err, retryErr) {
		t.Errorf("err = %v, want retry err %v", err, retryErr)
	}
}

// TestWithFilesystemGateCtxRoundTrip checks that WithFilesystemGate
// and FilesystemGateFromContext actually round-trip the gate through
// ctx. This is the contract every handler depends on.
func TestWithFilesystemGateCtxRoundTrip(t *testing.T) {
	gate := &recordingGate{}
	ctx := WithFilesystemGate(context.Background(), gate)
	got := FilesystemGateFromContext(ctx)
	if got != gate {
		t.Errorf("FilesystemGateFromContext did not return the gate that was set")
	}
}

// TestWithFilesystemGate_NilIsNoOp ensures the no-op behavior on nil
// gates — important because handlers call WithFilesystemGateFromEnv
// unconditionally and the gate may be nil for non-agent contexts.
func TestWithFilesystemGate_NilIsNoOp(t *testing.T) {
	ctx := context.Background()
	got := WithFilesystemGate(ctx, nil)
	if got != ctx {
		t.Errorf("WithFilesystemGate(ctx, nil) should return the same ctx value")
	}
}

// TestWithFilesystemApproval_PassesResolvedPathToGate verifies that
// when the user-supplied path is a symlink, the helper resolves the
// canonical target and passes it to the gate. This is the security
// guarantee that prevents a workspace symlink to /etc/passwd from
// being approved under a benign-looking display string.
func TestWithFilesystemApproval_PassesResolvedPathToGate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix fixtures only")
	}

	// Real symlink: $TMP/<link> → <dir>/real-target
	tmpRoot, err := os.MkdirTemp("", "sprout-resolve-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmpRoot) })

	realTargetDir := t.TempDir()
	realTarget := realTargetDir + "/real-target"
	if err := os.WriteFile(realTarget, []byte("data"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	linkPath := tmpRoot + "/link"
	if err := os.Symlink(realTarget, linkPath); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	gate := &recordingGate{approveDecision: false}
	_, _ = withFilesystemApproval[string](
		context.Background(), gate, "write_file", linkPath,
		func(ctx context.Context) (string, error) {
			return "", filesystem.ErrOutsideWorkingDirectory
		},
	)

	if gate.lastFilePath != linkPath {
		t.Errorf("lastFilePath = %q, want %q", gate.lastFilePath, linkPath)
	}
	// EvalSymlinks may canonicalize /var/folders → /private/var/folders
	// on macOS (because /var is a symlink). Compare canonical-to-canonical
	// so the test passes on both Linux and macOS.
	wantResolved, _ := filepath.EvalSymlinks(realTarget)
	if gate.lastResolvedPath != wantResolved {
		t.Errorf("lastResolvedPath = %q, want %q (symlink should be canonicalized before being shown to the gate)",
			gate.lastResolvedPath, wantResolved)
	}
}

// TestWithFilesystemApproval_ResolvedPathEmptyForUnresolvable verifies
// the fallback behavior when EvalSymlinks can't resolve (missing file,
// broken symlink, permission error on parent). The helper must still
// call the gate; the gate just receives "" for the resolved target
// and falls back to the user-supplied path for display.
func TestWithFilesystemApproval_ResolvedPathEmptyForUnresolvable(t *testing.T) {
	gate := &recordingGate{approveDecision: false}
	_, _ = withFilesystemApproval[string](
		context.Background(), gate, "write_file", "/this/path/does/not/exist/at/all",
		func(ctx context.Context) (string, error) {
			return "", filesystem.ErrOutsideWorkingDirectory
		},
	)
	if gate.calls != 1 {
		t.Fatalf("gate should still be called once, got %d", gate.calls)
	}
	if gate.lastResolvedPath != "" {
		t.Errorf("lastResolvedPath = %q, want empty for unresolvable path", gate.lastResolvedPath)
	}
	if gate.lastFilePath != "/this/path/does/not/exist/at/all" {
		t.Errorf("lastFilePath = %q, want user-supplied path preserved", gate.lastFilePath)
	}
}


// TestListDirectoryHandler_ApproveFlowsThroughGate is the integration
// test for the list_directory gate wiring that the reviewer flagged as
// broken (the handler previously injected the gate into ctx but never
// consulted it because pkg/filesystem doesn't read it from ctx — the
// explicit withFilesystemApproval wrap is required). This test pins
// the corrected behavior: a list_directory call on an off-workspace
// path consults the gate and, on approval, proceeds; on denial, the
// caller sees the original ErrOutsideWorkingDirectory.
//
// We exercise the handler end-to-end (Execute → helper) rather than
// just the helper, because the bug lived in Execute's wiring, not in
// the helper itself.
func TestListDirectoryHandler_ApproveFlowsThroughGate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix fixtures only")
	}

	gate := &recordingGate{
		approveDecision: true,
		returnedCtx:     filesystem.WithSecurityBypass(context.Background()),
	}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	dir, err := os.MkdirTemp(home, "sprout-listdir-gate-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	env := ToolEnv{
		FilesystemGate: gate,
		WorkspaceRoot:  dir, // keep workspace root in dir so targetPath is "external"
	}

	h := &listDirHandler{}
	result, err := h.Execute(context.Background(), env, map[string]any{
		"path": dir, // External: outside the agent's effective cwd
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.IsError {
		t.Errorf("Execute returned IsError=true after approval: %s", result.Output)
	}
	if gate.calls != 1 {
		t.Errorf("expected gate to be called once, got %d", gate.calls)
	}
	if gate.lastToolName != "list_directory" {
		t.Errorf("gate toolName = %q, want list_directory", gate.lastToolName)
	}
}

// TestListDirectoryHandler_DenyPropagatesError confirms the
// corresponding denial path: when the gate says no, the model sees
// the original ErrOutsideWorkingDirectory. Without this guarantee
// the helper would be useless — denial must surface as a meaningful
// error to drive the model's next move.
func TestListDirectoryHandler_DenyPropagatesError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix fixtures only")
	}

	// Real path that exists off-workspace. The deny test needs an
	// off-workspace path that triggers the resolve gate, not a
	// missing-file error — so we create a tempdir under $HOME and
	// point at it.
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	dir, err := os.MkdirTemp(home, "sprout-listdir-deny-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	gate := &recordingGate{approveDecision: false}
	env := ToolEnv{FilesystemGate: gate, WorkspaceRoot: t.TempDir()}

	h := &listDirHandler{}
	_, err = h.Execute(context.Background(), env, map[string]any{
		"path": dir,
	})
	if err == nil {
		t.Fatal("expected error on deny, got nil")
	}
	if !errors.Is(err, filesystem.ErrOutsideWorkingDirectory) {
		t.Errorf("err = %v, want ErrOutsideWorkingDirectory", err)
	}
	if gate.calls != 1 {
		t.Errorf("expected gate to be called once, got %d", gate.calls)
	}
}

// TestListDirectoryHandler_NoGateIsNoop asserts the historical
// no-gate behavior is preserved for non-agent callers (subagent
// internal machinery, tests). Without this contract, subagents or
// background workers would suddenly start blocking on prompts they
// have no surface to answer.
func TestListDirectoryHandler_NoGateIsNoop(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix fixtures only")
	}

	// Real off-workspace path under $HOME — same rationale as the
	// deny test above: missing paths trigger a different error
	// before the gate would be consulted.
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	dir, err := os.MkdirTemp(home, "sprout-listdir-nogate-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	env := ToolEnv{WorkspaceRoot: t.TempDir()}
	h := &listDirHandler{}
	_, err = h.Execute(context.Background(), env, map[string]any{
		"path": dir,
	})
	if err == nil {
		t.Fatal("expected error when no gate wired and path is off-workspace")
	}
	if !errors.Is(err, filesystem.ErrOutsideWorkingDirectory) {
		t.Errorf("err = %v, want ErrOutsideWorkingDirectory", err)
	}
}