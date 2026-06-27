package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

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

// ---------------------------------------------------------------------------
// SP-082-1: Round-trip order-preservation tests
// ---------------------------------------------------------------------------

// TestWriteStructuredFile_JSON_OrderPreservation verifies that when the
// handler receives RawArgsJSON, key insertion order from the original JSON
// is preserved in the output — not alphabetically sorted.
func TestWriteStructuredFile_JSON_OrderPreservation(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &writeStructuredFileHandler{}
	ctx := newTestCtx(dir)

	// Raw JSON with a specific key order that is NOT alphabetical.
	// Alphabetical would be: dependencies, description, name, version
	rawArgs := `{"path":"` + filepath.Join(dir, "order.json") +
		`","data":{"name":"test-pkg","version":"1.0.0","dependencies":{"express":"^4.0.0"},"description":"A test package"}}`

	path := filepath.Join(dir, "order.json")
	env := newTestEnv(t, dir)
	env.RawArgsJSON = rawArgs

	res, err := h.Execute(ctx, env, map[string]any{
		"path": path,
		"data": map[string]any{
			"name":         "test-pkg",
			"version":      "1.0.0",
			"dependencies": map[string]any{"express": "^4.0.0"},
			"description":  "A test package",
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	fileData, err := os.ReadFile(path)
	require.NoError(t, err)
	content := string(fileData)

	// The keys should appear in insertion order: name → version → dependencies → description
	// NOT alphabetical order (which would be: dependencies, description, name, version)
	nameIdx := strings.Index(content, `"name"`)
	versionIdx := strings.Index(content, `"version"`)
	depsIdx := strings.Index(content, `"dependencies"`)
	descIdx := strings.Index(content, `"description"`)

	// All keys should be found
	require.NotEqual(t, -1, nameIdx, "name key should exist")
	require.NotEqual(t, -1, versionIdx, "version key should exist")
	require.NotEqual(t, -1, depsIdx, "dependencies key should exist")
	require.NotEqual(t, -1, descIdx, "description key should exist")

	// Verify insertion order: name before version before dependencies before description
	require.Less(t, nameIdx, versionIdx, "name should appear before version (insertion order)")
	require.Less(t, versionIdx, depsIdx, "version should appear before dependencies (insertion order)")
	require.Less(t, depsIdx, descIdx, "dependencies should appear before description (insertion order)")
}

// TestWriteStructuredFile_YAML_OrderPreservation verifies that when
// RawArgsJSON is provided for a .yaml target, key insertion order is
// preserved in the serialized output.
func TestWriteStructuredFile_YAML_OrderPreservation(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &writeStructuredFileHandler{}
	ctx := newTestCtx(dir)

	rawArgs := `{"path":"` + filepath.Join(dir, "order.yaml") +
		`","data":{"name":"test-pkg","version":"1.0.0","dependencies":{"express":"^4.0.0"},"description":"A test package"}}`

	path := filepath.Join(dir, "order.yaml")
	env := newTestEnv(t, dir)
	env.RawArgsJSON = rawArgs

	res, err := h.Execute(ctx, env, map[string]any{
		"path": path,
		"data": map[string]any{
			"name":         "test-pkg",
			"version":      "1.0.0",
			"dependencies": map[string]any{"express": "^4.0.0"},
			"description":  "A test package",
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	fileData, err := os.ReadFile(path)
	require.NoError(t, err)
	content := string(fileData)

	// After stripJSONStyle, the output is native YAML (not JSON).
	// Keys should appear in insertion order: name → version → dependencies → description.
	nameIdx := strings.Index(content, "name:")
	versionIdx := strings.Index(content, "version:")
	depsIdx := strings.Index(content, "dependencies:")
	descIdx := strings.Index(content, "description:")

	require.NotEqual(t, -1, nameIdx, "name key should exist")
	require.NotEqual(t, -1, versionIdx, "version key should exist")
	require.NotEqual(t, -1, depsIdx, "dependencies key should exist")
	require.NotEqual(t, -1, descIdx, "description key should exist")

	require.Less(t, nameIdx, versionIdx, "name should appear before version")
	require.Less(t, versionIdx, depsIdx, "version should appear before dependencies")
	require.Less(t, depsIdx, descIdx, "dependencies should appear before description")

	// Also verify values are correct by parsing back with yaml.v3
	var parsed map[string]interface{}
	require.NoError(t, yaml.Unmarshal(fileData, &parsed))
	parsed = normalizeYAMLValue(parsed).(map[string]interface{})
	require.Equal(t, "test-pkg", parsed["name"])
	require.Equal(t, "1.0.0", parsed["version"])
	require.Equal(t, "A test package", parsed["description"])
}

// TestWriteStructuredFile_YAML_LiteralBlockRoundTrip verifies that
// multi-line string values round-trip correctly through the YAML writer.
func TestWriteStructuredFile_YAML_LiteralBlockRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &writeStructuredFileHandler{}
	ctx := newTestCtx(dir)

	// Multi-line script value with embedded newlines.
	rawArgs := `{"path":"` + filepath.Join(dir, "multiline.yaml") +
		`","data":{"script":"echo hello\necho world","name":"test"}}`

	path := filepath.Join(dir, "multiline.yaml")
	env := newTestEnv(t, dir)
	env.RawArgsJSON = rawArgs

	res, err := h.Execute(ctx, env, map[string]any{
		"path": path,
		"data": map[string]any{
			"script": "echo hello\necho world",
			"name":   "test",
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	fileData, err := os.ReadFile(path)
	require.NoError(t, err)

	// Parse back and verify the multi-line string content is identical
	var parsed map[string]interface{}
	require.NoError(t, yaml.Unmarshal(fileData, &parsed))
	parsed = normalizeYAMLValue(parsed).(map[string]interface{})

	scriptVal, ok := parsed["script"].(string)
	require.True(t, ok, "script should be a string")
	require.Equal(t, "echo hello\necho world", scriptVal,
		"multi-line string should round-trip with identical content")

	require.Equal(t, "test", parsed["name"])
}

// TestWriteStructuredFile_NestedOrderPreservation verifies that nested
// object keys also preserve their insertion order.
func TestWriteStructuredFile_NestedOrderPreservation(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &writeStructuredFileHandler{}
	ctx := newTestCtx(dir)

	// Nested structure with specific key orders at each level.
	rawArgs := `{"path":"` + filepath.Join(dir, "nested.json") +
		`","data":{"project":{"name":"x","version":"1.0"},"build":{"target":"linux","arch":"amd64"}}}`

	path := filepath.Join(dir, "nested.json")
	env := newTestEnv(t, dir)
	env.RawArgsJSON = rawArgs

	res, err := h.Execute(ctx, env, map[string]any{
		"path": path,
		"data": map[string]any{
			"project": map[string]any{
				"name":    "x",
				"version": "1.0",
			},
			"build": map[string]any{
				"target": "linux",
				"arch":   "amd64",
			},
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	fileData, err := os.ReadFile(path)
	require.NoError(t, err)
	content := string(fileData)

	// Top-level: project before build
	projectIdx := strings.Index(content, `"project"`)
	buildIdx := strings.Index(content, `"build"`)
	require.NotEqual(t, -1, projectIdx, "project key should exist")
	require.NotEqual(t, -1, buildIdx, "build key should exist")
	require.Less(t, projectIdx, buildIdx, "project should appear before build (insertion order)")

	// Nested: name before version inside project
	projectBlockStart := projectIdx
	projectBlockEnd := buildIdx
	projectBlock := content[projectBlockStart:projectBlockEnd]
	nameIdx := strings.Index(projectBlock, `"name"`)
	versionIdx := strings.Index(projectBlock, `"version"`)
	require.NotEqual(t, -1, nameIdx, "name key should exist inside project")
	require.NotEqual(t, -1, versionIdx, "version key should exist inside project")
	require.Less(t, nameIdx, versionIdx, "name should appear before version inside project")

	// Nested: target before arch inside build
	buildBlockStart := buildIdx
	buildBlock := content[buildBlockStart:]
	targetIdx := strings.Index(buildBlock, `"target"`)
	archIdx := strings.Index(buildBlock, `"arch"`)
	require.NotEqual(t, -1, targetIdx, "target key should exist inside build")
	require.NotEqual(t, -1, archIdx, "arch key should exist inside build")
	require.Less(t, targetIdx, archIdx, "target should appear before arch inside build")
}
