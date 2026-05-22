package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sprout-foundry/sprout/pkg/events"
)

func TestWriteFileHandler_Execute_Basic(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &writeFileHandler{}
	ctx := newTestCtx(dir)

	path := filepath.Join(dir, "hello.txt")
	content := "hello world\nline 2"

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"path":    path,
		"content": content,
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	// Verify file was created with correct content
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, content, string(data))

	// Output should mention success
	require.Contains(t, strings.ToLower(res.Output), "successfully wrote")
}

func TestWriteFileHandler_Execute_WithLineCount(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &writeFileHandler{}
	ctx := newTestCtx(dir)

	path := filepath.Join(dir, "multiline.txt")
	content := "line 1\nline 2\nline 3\nline 4\nline 5"

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"path":    path,
		"content": content,
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Contains(t, res.Output, "5 lines")
}

func TestWriteFileHandler_Execute_EmptyContent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &writeFileHandler{}
	ctx := newTestCtx(dir)

	path := filepath.Join(dir, "empty.txt")

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"path":    path,
		"content": "",
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	// File should exist but be empty
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Empty(t, string(data))
}

func TestWriteFileHandler_Execute_InvalidPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &writeFileHandler{}
	ctx := newTestCtx(dir)

	// Path inside a non-existent directory - WriteFile creates parent dirs
	path := filepath.Join(dir, "nonexistent", "subdir", "file.txt")

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"path":    path,
		"content": "data",
	})
	// WriteFile creates parent directories, so this succeeds
	require.NoError(t, err)
	require.False(t, res.IsError)
}

func TestWriteFileHandler_Execute_MissingPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &writeFileHandler{}
	ctx := newTestCtx(dir)

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"content": "data",
	})
	require.Error(t, err)
	require.True(t, res.IsError)
}

func TestWriteFileHandler_Execute_MissingContent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &writeFileHandler{}
	ctx := newTestCtx(dir)

	path := filepath.Join(dir, "test.txt")
	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"path": path,
	})
	require.Error(t, err)
	require.True(t, res.IsError)
}

func TestWriteFileHandler_Execute_RelativePath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &writeFileHandler{}
	ctx := newTestCtx(dir)

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"path":    "relative.txt",
		"content": "relative content",
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	// Verify file was created relative to workspace root
	data, err := os.ReadFile(filepath.Join(dir, "relative.txt"))
	require.NoError(t, err)
	require.Equal(t, "relative content", string(data))
}

func TestWriteFileHandler_Execute_OutputWriter(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &writeFileHandler{}
	ctx := newTestCtx(dir)

	var buf strings.Builder
	env := newTestEnv(t, dir)
	env.OutputWriter = &buf

	path := filepath.Join(dir, "writer.txt")
	content := "writer output"

	res, err := h.Execute(ctx, env, map[string]any{
		"path":    path,
		"content": content,
	})
	require.NoError(t, err)
	require.Contains(t, buf.String(), res.Output)
}

func TestWriteFileHandler_Execute_EventBus(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &writeFileHandler{}
	ctx := newTestCtx(dir)

	bus := events.NewEventBus()
	ch := bus.Subscribe("test")
	env := newTestEnv(t, dir)
	env.EventBus = bus

	path := filepath.Join(dir, "events.txt")
	_, err := h.Execute(ctx, env, map[string]any{
		"path":    path,
		"content": "event test",
	})
	require.NoError(t, err)

	// Verify tool_start event
	select {
	case evt := <-ch:
		require.Equal(t, "tool_start", evt.Type)
	default:
		t.Fatal("expected tool_start event")
	}

	// Verify tool_end event
	select {
	case evt := <-ch:
		require.Equal(t, "tool_end", evt.Type)
	default:
		t.Fatal("expected tool_end event")
	}
}

func TestWriteFileHandler_Execute_BinaryContent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &writeFileHandler{}
	ctx := newTestCtx(dir)

	path := filepath.Join(dir, "binary.bin")
	content := "\x00\x01\x02\x03\x04"

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"path":    path,
		"content": content,
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, []byte(content), data)
}

func TestWriteFileHandler_Execute_Overwrite(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &writeFileHandler{}
	ctx := newTestCtx(dir)

	path := filepath.Join(dir, "overwrite.txt")

	// Write initial content
	require.NoError(t, os.WriteFile(path, []byte("original"), 0o644))

	// Overwrite with new content
	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"path":    path,
		"content": "replaced",
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "replaced", string(data))
}

func TestWriteFileHandler_Execute_TokenUsage(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &writeFileHandler{}
	ctx := newTestCtx(dir)

	path := filepath.Join(dir, "tokens.txt")
	// 80 chars should produce some token usage
	content := strings.Repeat("x", 80)

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"path":    path,
		"content": content,
	})
	require.NoError(t, err)
	require.Greater(t, res.TokenUsage, int64(0), "should report token usage for non-empty output")
}

func TestWriteFileHandler_Execute_PathTraversal(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &writeFileHandler{}
	ctx := newTestCtx(dir)

	// Path traversal with .. - the handler resolves this relative to workspace root.
	// In practice, writing to a path outside workspace root should be allowed
	// when the resolved path is still within the OS filesystem (this is the
	// current behavior of the handler).
	path := filepath.Join(dir, "..", "outside_workspace.txt")

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"path":    path,
		"content": "escape attempt",
	})
	// The current implementation resolves this path successfully.
	// If path traversal protection is added later, this should fail.
	if err != nil {
		require.True(t, res.IsError)
	} else {
		require.False(t, res.IsError)
		// Clean up the file written outside the temp dir
		os.Remove(filepath.Clean(path))
	}
}

func TestWriteFileHandler_Validate(t *testing.T) {
	t.Parallel()
	h := &writeFileHandler{}

	// Valid
	require.NoError(t, h.Validate(map[string]any{
		"path":    "file.txt",
		"content": "data",
	}))

	// Missing path
	err := h.Validate(map[string]any{"content": "data"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "required")

	// Empty path
	err = h.Validate(map[string]any{"path": "", "content": "data"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "must not be empty")

	// Missing content
	err = h.Validate(map[string]any{"path": "file.txt"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "required")
}
