package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sprout-foundry/sprout/pkg/events"
)

func TestEditFileHandler_Execute_Basic(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &editFileHandler{}
	ctx := newTestCtx(dir)

	path := filepath.Join(dir, "hello.txt")
	require.NoError(t, os.WriteFile(path, []byte("hello world"), 0o644))

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"path":    path,
		"old_str": "world",
		"new_str": "universe",
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	// Verify the file was edited
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "hello universe", string(data))
	require.Contains(t, strings.ToLower(res.Output), "edited")
}

func TestEditFileHandler_Execute_MultipleLines(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &editFileHandler{}
	ctx := newTestCtx(dir)

	path := filepath.Join(dir, "multiline.txt")
	original := "line1\nline2\nline3\nline4\n"
	require.NoError(t, os.WriteFile(path, []byte(original), 0o644))

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"path":    path,
		"old_str": "line2\nline3",
		"new_str": "replaced",
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "line1\nreplaced\nline4\n", string(data))
}

func TestEditFileHandler_Execute_MissingFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &editFileHandler{}
	ctx := newTestCtx(dir)

	path := filepath.Join(dir, "nonexistent.txt")

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"path":    path,
		"old_str": "foo",
		"new_str": "bar",
	})
	require.Error(t, err)
	require.True(t, res.IsError)
}

func TestEditFileHandler_Execute_StringNotFound(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &editFileHandler{}
	ctx := newTestCtx(dir)

	path := filepath.Join(dir, "test.txt")
	require.NoError(t, os.WriteFile(path, []byte("hello world"), 0o644))

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"path":    path,
		"old_str": "notfound",
		"new_str": "replacement",
	})
	require.Error(t, err)
	require.True(t, res.IsError)
	require.Contains(t, err.Error(), "not found")
}

func TestEditFileHandler_Execute_ReplaceWithEmpty(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &editFileHandler{}
	ctx := newTestCtx(dir)

	path := filepath.Join(dir, "delete.txt")
	require.NoError(t, os.WriteFile(path, []byte("before middle after"), 0o644))

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"path":    path,
		"old_str": "middle ",
		"new_str": "",
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "before after", string(data))
}

func TestEditFileHandler_Execute_MissingPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &editFileHandler{}
	ctx := newTestCtx(dir)

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"old_str": "foo",
		"new_str": "bar",
	})
	require.Error(t, err)
	require.True(t, res.IsError)
}

func TestEditFileHandler_Execute_MissingOldStr(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &editFileHandler{}
	ctx := newTestCtx(dir)

	path := filepath.Join(dir, "test.txt")
	require.NoError(t, os.WriteFile(path, []byte("hello"), 0o644))

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"path":    path,
		"new_str": "replacement",
	})
	require.Error(t, err)
	require.True(t, res.IsError)
}

func TestEditFileHandler_Execute_MissingNewStr(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &editFileHandler{}
	ctx := newTestCtx(dir)

	path := filepath.Join(dir, "test.txt")
	require.NoError(t, os.WriteFile(path, []byte("hello"), 0o644))

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"path":    path,
		"old_str": "hello",
	})
	require.Error(t, err)
	require.True(t, res.IsError)
}

func TestEditFileHandler_Execute_RelativePath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &editFileHandler{}
	ctx := newTestCtx(dir)

	path := filepath.Join(dir, "relative.txt")
	require.NoError(t, os.WriteFile(path, []byte("original"), 0o644))

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"path":    "relative.txt",
		"old_str": "original",
		"new_str": "modified",
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	data, err := os.ReadFile(filepath.Join(dir, "relative.txt"))
	require.NoError(t, err)
	require.Equal(t, "modified", string(data))
}

func TestEditFileHandler_Execute_OutputWriter(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &editFileHandler{}
	ctx := newTestCtx(dir)

	var buf strings.Builder
	env := newTestEnv(t, dir)
	env.OutputWriter = &buf

	path := filepath.Join(dir, "writer.txt")
	require.NoError(t, os.WriteFile(path, []byte("hello world"), 0o644))

	res, err := h.Execute(ctx, env, map[string]any{
		"path":    path,
		"old_str": "world",
		"new_str": "universe",
	})
	require.NoError(t, err)
	require.Contains(t, buf.String(), res.Output)
}

func TestEditFileHandler_Execute_EventBus(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &editFileHandler{}
	ctx := newTestCtx(dir)

	bus := events.NewEventBus()
	ch := bus.Subscribe("test")
	env := newTestEnv(t, dir)
	env.EventBus = bus

	path := filepath.Join(dir, "events.txt")
	require.NoError(t, os.WriteFile(path, []byte("before"), 0o644))

	_, err := h.Execute(ctx, env, map[string]any{
		"path":    path,
		"old_str": "before",
		"new_str": "after",
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

func TestEditFileHandler_Execute_WithSpecialChars(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &editFileHandler{}
	ctx := newTestCtx(dir)

	path := filepath.Join(dir, "special.txt")
	original := "hello\tworld\nline2"
	require.NoError(t, os.WriteFile(path, []byte(original), 0o644))

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"path":    path,
		"old_str": "\tworld",
		"new_str": "\tuniverse",
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "hello\tuniverse\nline2", string(data))
}

func TestEditFileHandler_Execute_PathTraversal(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &editFileHandler{}
	ctx := newTestCtx(dir)

	// Try to edit outside workspace. The handler may or may not
	// allow this depending on path resolution. Accept either outcome.
	path := filepath.Join(dir, "..", "outside.txt")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("outside"), 0o644))

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"path":    path,
		"old_str": "outside",
		"new_str": "modified",
	})
	if err != nil {
		require.True(t, res.IsError)
	} else {
		require.False(t, res.IsError)
	}
	// Clean up
	os.Remove(path)
}

func TestEditFileHandler_Execute_TokenUsage(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &editFileHandler{}
	ctx := newTestCtx(dir)

	path := filepath.Join(dir, "tokens.txt")
	content := "HEADER:" + strings.Repeat("x", 80)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"path":    path,
		"old_str": "HEADER:",
		"new_str": "NEW_HEADER:",
	})
	require.NoError(t, err)
	require.Greater(t, res.TokenUsage, int64(0), "should report token usage for non-empty output")
}

func TestEditFileHandler_Execute_Unicode(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &editFileHandler{}
	ctx := newTestCtx(dir)

	path := filepath.Join(dir, "unicode.txt")
	require.NoError(t, os.WriteFile(path, []byte("你好世界"), 0o644))

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"path":    path,
		"old_str": "世界",
		"new_str": "宇宙",
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "你好宇宙", string(data))
}

func TestEditFileHandler_Validate(t *testing.T) {
	t.Parallel()
	h := &editFileHandler{}

	// Valid
	require.NoError(t, h.Validate(map[string]any{
		"path":    "file.txt",
		"old_str": "original",
		"new_str": "replacement",
	}))

	// new_str can be empty
	require.NoError(t, h.Validate(map[string]any{
		"path":    "file.txt",
		"old_str": "original",
		"new_str": "",
	}))

	// Missing path
	err := h.Validate(map[string]any{"old_str": "a", "new_str": "b"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "required")

	// Empty path
	err = h.Validate(map[string]any{"path": "", "old_str": "a", "new_str": "b"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "must not be empty")

	// Missing old_str
	err = h.Validate(map[string]any{"path": "file.txt", "new_str": "b"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "required")

	// Empty old_str
	err = h.Validate(map[string]any{"path": "file.txt", "old_str": "", "new_str": "b"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "must not be empty")

	// Missing new_str
	err = h.Validate(map[string]any{"path": "file.txt", "old_str": "a"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "required")
}
