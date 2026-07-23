package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sprout-foundry/sprout/pkg/filesystem"
)

// ---------------------------------------------------------------------------
// SP-127 M2: Handler-level precheck deny tests
//
// These tests verify that the typed-error-on-deny path in each handler's
// Execute function is exercised and produces the expected error output.
// ---------------------------------------------------------------------------

// stubClassifier is a test double for FileAccessClassifier that returns
// a configurable decision string.
type stubClassifier struct {
	decision string
}

func (s stubClassifier) ClassifyFileAccess(_ context.Context, _, _, _ string) string {
	return s.decision
}

// denyClassifier always returns "deny" for any access.
type denyClassifier struct{}

func (denyClassifier) ClassifyFileAccess(_ context.Context, _, _, _ string) string {
	return "deny"
}

// allowClassifier always returns "allow" for any access.
type allowClassifier struct{}

func (allowClassifier) ClassifyFileAccess(_ context.Context, _, _, _ string) string {
	return "allow"
}

// newTestEnvWithClassifier builds a ToolEnv backed by a temp workspace and
// optionally injects a FileAccessClassifier. Nil classifier means no
// classifier is available (handlers fall through and return the raw filesystem error).
func newTestEnvWithClassifier(t *testing.T, classifier FileAccessClassifier) ToolEnv {
	t.Helper()
	return ToolEnv{
		WorkspaceRoot:         t.TempDir(),
		OutputWriter:          os.Stderr,
		FileAccessClassifier:  classifier,
	}
}

// ---------------------------------------------------------------------------
// PrecheckFileAccess unit tests
// ---------------------------------------------------------------------------

func TestPrecheckFileAccess_NilClassifier_ReturnsPrompt(t *testing.T) {
	t.Parallel()
	gotPath, gotDecision := PrecheckFileAccess(context.Background(), nil, "write_file", "/etc/passwd")
	if gotPath != "" {
		t.Errorf("resolvedPath = %q, want %q", gotPath, "")
	}
	if gotDecision != "prompt" {
		t.Errorf("decision = %q, want %q", gotDecision, "prompt")
	}
}

func TestPrecheckFileAccess_Allow_ReturnsAllow(t *testing.T) {
	t.Parallel()
	classifier := &allowClassifier{}
	gotPath, gotDecision := PrecheckFileAccess(context.Background(), classifier, "write_file", "/etc/passwd")
	if gotPath == "" {
		t.Error("resolvedPath should not be empty")
	}
	if gotDecision != "allow" {
		t.Errorf("decision = %q, want %q", gotDecision, "allow")
	}
}

func TestPrecheckFileAccess_Deny_ReturnsDeny(t *testing.T) {
	t.Parallel()
	classifier := &denyClassifier{}
	gotPath, gotDecision := PrecheckFileAccess(context.Background(), classifier, "write_file", "/etc/passwd")
	if gotPath == "" {
		t.Error("resolvedPath should not be empty")
	}
	if gotDecision != "deny" {
		t.Errorf("decision = %q, want %q", gotDecision, "deny")
	}
}

func TestPrecheckFileAccess_WriteTools_UsesWriteMode(t *testing.T) {
	t.Parallel()
	// captureClassifier records the mode argument passed to ClassifyFileAccess.
	captureClassifier := &captureModeClassifier{}
	PrecheckFileAccess(context.Background(), captureClassifier, "write_file", "/etc/passwd")
	if captureClassifier.gotMode != "write" {
		t.Errorf("mode = %q, want %q", captureClassifier.gotMode, "write")
	}

	captureClassifier = &captureModeClassifier{}
	PrecheckFileAccess(context.Background(), captureClassifier, "edit_file", "/etc/passwd")
	if captureClassifier.gotMode != "write" {
		t.Errorf("mode = %q, want %q", captureClassifier.gotMode, "write")
	}

	captureClassifier = &captureModeClassifier{}
	PrecheckFileAccess(context.Background(), captureClassifier, "write_structured_file", "/etc/passwd")
	if captureClassifier.gotMode != "write" {
		t.Errorf("mode = %q, want %q", captureClassifier.gotMode, "write")
	}

	captureClassifier = &captureModeClassifier{}
	PrecheckFileAccess(context.Background(), captureClassifier, "patch_structured_file", "/etc/passwd")
	if captureClassifier.gotMode != "write" {
		t.Errorf("mode = %q, want %q", captureClassifier.gotMode, "write")
	}
}

func TestPrecheckFileAccess_ReadTools_UsesReadMode(t *testing.T) {
	t.Parallel()
	captureClassifier := &captureModeClassifier{}
	PrecheckFileAccess(context.Background(), captureClassifier, "read_file", "/etc/passwd")
	if captureClassifier.gotMode != "read" {
		t.Errorf("mode = %q, want %q", captureClassifier.gotMode, "read")
	}

	captureClassifier = &captureModeClassifier{}
	PrecheckFileAccess(context.Background(), captureClassifier, "list_directory", "/tmp")
	if captureClassifier.gotMode != "read" {
		t.Errorf("mode = %q, want %q", captureClassifier.gotMode, "read")
	}
}

type captureModeClassifier struct {
	gotMode string
}

func (c *captureModeClassifier) ClassifyFileAccess(_ context.Context, _, _, mode string) string {
	c.gotMode = mode
	return "prompt"
}

// ---------------------------------------------------------------------------
// Handler-level deny tests
// ---------------------------------------------------------------------------

func TestPrecheckWriteFile_Deny_ReturnsTypedError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &writeFileHandler{}
	ctx := filesystem.WithWorkspaceRoot(context.Background(), dir)

	// Use a classifier that denies all writes to /etc.
	env := ToolEnv{
		WorkspaceRoot:        dir,
		FileAccessClassifier: denyClassifier{},
	}

	result, err := h.Execute(ctx, env, map[string]any{
		"path":    "/etc/hostname",
		"content": "test",
	})
	// Deny returns a typed error as both ToolResult.IsError and err.
	require.Error(t, err, "Execute should return an error on deny")
	require.True(t, result.IsError, "result.IsError should be true on deny")
	require.Contains(t, result.Output, "blocked", "output should mention 'blocked'")
	require.Contains(t, result.Output, "/etc/hostname", "output should include the path")
}

func TestPrecheckEditFile_Deny_ReturnsTypedError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// Create a file inside the workspace first so edit has something to work with.
	// We use a classifier that denies the specific /etc path.
	h := &editFileHandler{}
	ctx := filesystem.WithWorkspaceRoot(context.Background(), dir)

	env := ToolEnv{
		WorkspaceRoot:        dir,
		FileAccessClassifier: denyClassifier{},
	}

	result, err := h.Execute(ctx, env, map[string]any{
		"path":    "/etc/hostname",
		"old_str": "old",
		"new_str": "new",
	})
	// Deny returns a typed error as both ToolResult.IsError and err.
	require.Error(t, err, "Execute should return an error on deny")
	require.True(t, result.IsError, "result.IsError should be true on deny")
	require.Contains(t, result.Output, "blocked", "output should mention 'blocked'")
	require.Contains(t, result.Output, "/etc/hostname", "output should include the path")
}

func TestPrecheckWriteStructuredFile_Deny_ReturnsTypedError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &writeStructuredFileHandler{}
	ctx := filesystem.WithWorkspaceRoot(context.Background(), dir)

	env := ToolEnv{
		WorkspaceRoot:        dir,
		FileAccessClassifier: denyClassifier{},
	}

	result, err := h.Execute(ctx, env, map[string]any{
		"path": "/etc/config.json",
		"data": map[string]any{"key": "value"},
	})
	// Deny returns a typed error as both ToolResult.IsError and err.
	require.Error(t, err, "Execute should return an error on deny")
	require.True(t, result.IsError, "result.IsError should be true on deny")
	require.Contains(t, result.Output, "blocked", "output should mention 'blocked'")
	require.Contains(t, result.Output, "/etc/config.json", "output should include the path")
}

func TestPrecheckPatchStructuredFile_Deny_ReturnsTypedError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &patchStructuredFileHandler{}
	ctx := filesystem.WithWorkspaceRoot(context.Background(), dir)

	env := ToolEnv{
		WorkspaceRoot:        dir,
		FileAccessClassifier: denyClassifier{},
	}

	result, err := h.Execute(ctx, env, map[string]any{
		"path": "/etc/config.json",
		"data": map[string]any{"key": "value"},
	})
	// Deny returns a typed error as both ToolResult.IsError and err.
	require.Error(t, err, "Execute should return an error on deny")
	require.True(t, result.IsError, "result.IsError should be true on deny")
	require.Contains(t, result.Output, "blocked", "output should mention 'blocked'")
	require.Contains(t, result.Output, "/etc/config.json", "output should include the path")
}

// ---------------------------------------------------------------------------
// Verify read handlers with deny still proceed (read_only returns allow)
// ---------------------------------------------------------------------------

func TestPrecheckReadFile_Allow_ProceedsSuccessfully(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	require.NoError(t, os.WriteFile(path, []byte("hello"), 0o644))

	h := &readFileHandler{}
	ctx := filesystem.WithWorkspaceRoot(context.Background(), dir)

	// An allow classifier means the path is workspace/tmp/allowlisted.
	env := ToolEnv{
		WorkspaceRoot:        dir,
		FileAccessClassifier: allowClassifier{},
	}

	result, err := h.Execute(ctx, env, map[string]any{"path": path})
	require.NoError(t, err)
	require.False(t, result.IsError, "result.IsError should be false on allow")
	require.Contains(t, result.Output, "hello")
}

func TestPrecheckListDirectory_Allow_ProceedsSuccessfully(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	subdir := filepath.Join(dir, "subdir")
	require.NoError(t, os.MkdirAll(subdir, 0o755))

	h := &listDirHandler{}
	ctx := filesystem.WithWorkspaceRoot(context.Background(), dir)

	// An allow classifier means the path is workspace/tmp/allowlisted.
	env := ToolEnv{
		WorkspaceRoot:        dir,
		FileAccessClassifier: allowClassifier{},
	}

	result, err := h.Execute(ctx, env, map[string]any{"path": subdir})
	require.NoError(t, err)
	require.False(t, result.IsError, "result.IsError should be false on allow")
	require.Contains(t, result.Output, "subdir")
}

// ---------------------------------------------------------------------------
// Verify nil classifier falls through (returns raw filesystem error)
// ---------------------------------------------------------------------------

func TestPrecheckWriteFile_NilClassifier_NoError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &writeFileHandler{}
	ctx := filesystem.WithWorkspaceRoot(context.Background(), dir)

	// nil classifier means no precheck context; write should proceed
	// (outside the gate, may fail due to off-workspace, but not panic).
	env := ToolEnv{
		WorkspaceRoot: dir,
		// FileAccessClassifier is nil by default
	}

	// With a nil classifier, the handler should fall through without panic.
	// The actual result depends on whether the path is in-workspace or not.
	_, _ = h.Execute(ctx, env, map[string]any{
		"path":    filepath.Join(dir, "test.txt"),
		"content": "hello",
	})
}
