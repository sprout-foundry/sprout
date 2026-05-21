package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sprout-foundry/sprout/pkg/events"
)

func TestWriteStructuredFileHandler_Execute_JSON(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &writeStructuredFileHandler{}
	ctx := newTestCtx(dir)

	path := filepath.Join(dir, "data.json")
	data := map[string]any{
		"name":    "test",
		"version": float64(1),
		"tags":    []any{"a", "b", "c"},
	}

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"path": path,
		"data": data,
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	// Verify file was created with valid JSON
	fileData, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(fileData), `"name"`)
	require.Contains(t, string(fileData), `"test"`)
	require.Contains(t, string(fileData), `"version"`)
}

func TestWriteStructuredFileHandler_Execute_YAML(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &writeStructuredFileHandler{}
	ctx := newTestCtx(dir)

	path := filepath.Join(dir, "config.yaml")
	data := map[string]any{
		"name":    "my-config",
		"enabled": true,
		"items":   []any{"one", "two"},
	}

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"path": path,
		"data": data,
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	// Verify file was created with YAML content
	fileData, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(fileData), "name:")
	require.Contains(t, string(fileData), "my-config")
}

func TestWriteStructuredFileHandler_Execute_YMLExtension(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &writeStructuredFileHandler{}
	ctx := newTestCtx(dir)

	path := filepath.Join(dir, "config.yml")
	data := map[string]any{"key": "value"}

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"path": path,
		"data": data,
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
}

func TestWriteStructuredFileHandler_Execute_ExplicitFormatOverride(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &writeStructuredFileHandler{}
	ctx := newTestCtx(dir)

	// Write JSON explicitly even though extension is .yaml
	path := filepath.Join(dir, "output.yaml")
	data := map[string]any{"format": "json", "value": float64(42)}

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"path":   path,
		"data":   data,
		"format": "json",
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	// Verify it's actually JSON despite .yaml extension
	fileData, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(fileData), "\"format\"")
	require.Contains(t, string(fileData), "\"json\"")
}

func TestWriteStructuredFileHandler_Execute_InvalidExtension(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &writeStructuredFileHandler{}
	ctx := newTestCtx(dir)

	path := filepath.Join(dir, "data.txt")
	data := map[string]any{"key": "value"}

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"path": path,
		"data": data,
	})
	require.Error(t, err)
	require.True(t, res.IsError)
	require.Contains(t, res.Output, "unsupported structured format")
}

func TestWriteStructuredFileHandler_Execute_WithSchemaValidation(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &writeStructuredFileHandler{}
	ctx := newTestCtx(dir)

	path := filepath.Join(dir, "validated.json")
	data := map[string]any{
		"name":  "valid",
		"count": float64(42),
	}
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":  map[string]any{"type": "string"},
			"count": map[string]any{"type": "integer"},
		},
		"required": []any{"name"},
	}

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"path":   path,
		"data":   data,
		"schema": schema,
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
}

func TestWriteStructuredFileHandler_Execute_SchemaValidationError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &writeStructuredFileHandler{}
	ctx := newTestCtx(dir)

	path := filepath.Join(dir, "invalid.json")
	// name should be string but is a number
	data := map[string]any{
		"name": float64(123),
	}
	schema := map[string]any{
		"type":       "object",
		"properties": map[string]any{"name": map[string]any{"type": "string"}},
	}

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"path":   path,
		"data":   data,
		"schema": schema,
	})
	require.Error(t, err)
	require.True(t, res.IsError)
	require.Contains(t, res.Output, "schema validation failed")
}

func TestWriteStructuredFileHandler_Execute_MissingData(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &writeStructuredFileHandler{}
	ctx := newTestCtx(dir)

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"path": "file.json",
	})
	require.Error(t, err)
	require.True(t, res.IsError)
	require.Contains(t, res.Output, "required")
}

func TestWriteStructuredFileHandler_Execute_InvalidPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &writeStructuredFileHandler{}
	ctx := newTestCtx(dir)

	// Write to a path inside a non-existent subdirectory.
	// The underlying WriteFile function creates parent directories,
	// so this should succeed.
	path := filepath.Join(dir, "nonexistent", "data.json")
	data := map[string]any{"key": "value"}

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"path": path,
		"data": data,
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	// Verify the file was created (parent dirs created automatically)
	fileData, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(fileData), "\"key\"")
}

func TestWriteStructuredFileHandler_Execute_OutputWriter(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &writeStructuredFileHandler{}
	ctx := newTestCtx(dir)

	var buf strings.Builder
	env := newTestEnv(t, dir)
	env.OutputWriter = &buf

	path := filepath.Join(dir, "writer.json")
	data := map[string]any{"message": "hello"}

	res, err := h.Execute(ctx, env, map[string]any{
		"path": path,
		"data": data,
	})
	require.NoError(t, err)
	require.Contains(t, buf.String(), res.Output)
}

func TestWriteStructuredFileHandler_Execute_EventBus(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &writeStructuredFileHandler{}
	ctx := newTestCtx(dir)

	bus := events.NewEventBus()
	ch := bus.Subscribe("test")
	env := newTestEnv(t, dir)
	env.EventBus = bus

	path := filepath.Join(dir, "events.json")
	data := map[string]any{"event": true}

	_, err := h.Execute(ctx, env, map[string]any{
		"path": path,
		"data": data,
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

func TestWriteStructuredFileHandler_Execute_NestedData(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &writeStructuredFileHandler{}
	ctx := newTestCtx(dir)

	path := filepath.Join(dir, "nested.json")
	data := map[string]any{
		"config": map[string]any{
			"database": map[string]any{
				"host":     "localhost",
				"port":     float64(5432),
				"ssl_mode": "require",
			},
			"cache": map[string]any{
				"enabled": true,
				"ttl":     float64(300),
			},
		},
	}

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"path": path,
		"data": data,
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	fileData, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(fileData), "localhost")
	require.Contains(t, string(fileData), "5432")
}

func TestWriteStructuredFileHandler_Execute_ArrayData(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &writeStructuredFileHandler{}
	ctx := newTestCtx(dir)

	path := filepath.Join(dir, "list.json")
	data := []any{"first", "second", "third"}

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"path": path,
		"data": data,
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	fileData, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(fileData), "first")
	require.Contains(t, string(fileData), "third")
}

func TestWriteStructuredFileHandler_Execute_EmptyObject(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &writeStructuredFileHandler{}
	ctx := newTestCtx(dir)

	path := filepath.Join(dir, "empty.json")
	data := map[string]any{}

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"path": path,
		"data": data,
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
}

func TestWriteStructuredFileHandler_Execute_MissingPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &writeStructuredFileHandler{}
	ctx := newTestCtx(dir)

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"data": map[string]any{"key": "value"},
	})
	require.Error(t, err)
	require.True(t, res.IsError)
}

func TestWriteStructuredFileHandler_Execute_RelativePath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &writeStructuredFileHandler{}
	ctx := newTestCtx(dir)

	data := map[string]any{"relative": true}
	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"path": "relative.json",
		"data": data,
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	// Verify file was created relative to workspace root
	_, err = os.ReadFile(filepath.Join(dir, "relative.json"))
	require.NoError(t, err)
}

func TestWriteStructuredFileHandler_Validate(t *testing.T) {
	t.Parallel()
	h := &writeStructuredFileHandler{}

	// Valid
	require.NoError(t, h.Validate(map[string]any{
		"path": "file.json",
		"data": map[string]any{"key": "value"},
	}))

	// Missing path
	err := h.Validate(map[string]any{"data": map[string]any{}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "required")

	// Empty path
	err = h.Validate(map[string]any{"path": "", "data": map[string]any{}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "must not be empty")

	// Missing data
	err = h.Validate(map[string]any{"path": "file.json"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "required")

	// Empty format
	err = h.Validate(map[string]any{"path": "file.json", "data": map[string]any{}, "format": "  "})
	require.Error(t, err)
	require.Contains(t, err.Error(), "must not be empty")
}
