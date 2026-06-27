package tools

import (
	"encoding/json"
	"fmt"
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

// ---------------------------------------------------------------------------
// SP-082-3: Additional round-trip order-preservation tests
// ---------------------------------------------------------------------------

// TestWriteStructuredFile_PackageJSON_Order verifies the canonical package.json
// acceptance criterion: key order from RawArgsJSON is preserved through write,
// including nested objects, and the result is valid JSON.
func TestWriteStructuredFile_PackageJSON_Order(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &writeStructuredFileHandler{}
	ctx := newTestCtx(dir)

	path := filepath.Join(dir, "package.json")

	// RawArgsJSON with the exact key order from the spec.
	// Top-level: name → version → description → dependencies → scripts
	// Inside dependencies: express → lodash
	// Inside scripts: build → test → start
	rawArgs := `{"path":"` + path + `","data":{"name":"my-pkg","version":"1.0.0","description":"A sample package","dependencies":{"express":"^4.18.0","lodash":"^4.17.0"},"scripts":{"build":"tsc","test":"jest","start":"node index.js"}}}`

	env := newTestEnv(t, dir)
	env.RawArgsJSON = rawArgs

	res, err := h.Execute(ctx, env, map[string]any{
		"path": path,
		"data": map[string]any{
			"name":        "my-pkg",
			"version":     "1.0.0",
			"description": "A sample package",
			"dependencies": map[string]any{
				"express": "^4.18.0",
				"lodash":  "^4.17.0",
			},
			"scripts": map[string]any{
				"build": "tsc",
				"test":  "jest",
				"start": "node index.js",
			},
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	fileData, err := os.ReadFile(path)
	require.NoError(t, err)
	content := string(fileData)

	// Verify the file is valid JSON (round-trip parse).
	var parsed map[string]any
	require.NoError(t, json.Unmarshal(fileData, &parsed), "output must be valid JSON")

	// Top-level key order: name → version → description → dependencies → scripts
	nameIdx := strings.Index(content, `"name"`)
	versionIdx := strings.Index(content, `"version"`)
	descIdx := strings.Index(content, `"description"`)
	depsIdx := strings.Index(content, `"dependencies"`)
	scriptsIdx := strings.Index(content, `"scripts"`)

	require.NotEqual(t, -1, nameIdx, "name key should exist")
	require.NotEqual(t, -1, versionIdx, "version key should exist")
	require.NotEqual(t, -1, descIdx, "description key should exist")
	require.NotEqual(t, -1, depsIdx, "dependencies key should exist")
	require.NotEqual(t, -1, scriptsIdx, "scripts key should exist")

	require.Less(t, nameIdx, versionIdx, "name should appear before version")
	require.Less(t, versionIdx, descIdx, "version should appear before description")
	require.Less(t, descIdx, depsIdx, "description should appear before dependencies")
	require.Less(t, depsIdx, scriptsIdx, "dependencies should appear before scripts (NOT alphabetical)")

	// Nested: express before lodash inside dependencies.
	// Find the dependencies block and check ordering within it.
	// Since express and lodash don't appear elsewhere, simple Index works.
	expressIdx := strings.Index(content, `"express"`)
	lodashIdx := strings.Index(content, `"lodash"`)
	require.NotEqual(t, -1, expressIdx, "express should exist inside dependencies")
	require.NotEqual(t, -1, lodashIdx, "lodash should exist inside dependencies")
	require.Less(t, expressIdx, lodashIdx, "express should appear before lodash")

	// Nested: build before test before start inside scripts.
	buildIdx := strings.Index(content, `"build"`)
	testIdx := strings.Index(content, `"test"`)
	startIdx := strings.Index(content, `"start"`)
	require.NotEqual(t, -1, buildIdx, "build should exist inside scripts")
	require.NotEqual(t, -1, testIdx, "test should exist inside scripts")
	require.NotEqual(t, -1, startIdx, "start should exist inside scripts")
	require.Less(t, buildIdx, testIdx, "build should appear before test")
	require.Less(t, testIdx, startIdx, "test should appear before start")
}

// TestWriteStructuredFile_DockerCompose_RoundTrip verifies a docker-compose.yml
// shaped document: key order is preserved (version → services → volumes, NOT
// alphabetical) and values round-trip correctly through yaml.Unmarshal.
func TestWriteStructuredFile_DockerCompose_RoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &writeStructuredFileHandler{}
	ctx := newTestCtx(dir)

	path := filepath.Join(dir, "docker-compose.yml")

	// RawArgsJSON with version → services → volumes order.
	// Inside services: web → db, inside web: image → ports → volumes.
	rawArgs := `{"path":"` + path + `","data":{"version":"3.8","services":{"web":{"image":"nginx:latest","ports":["80:80"],"volumes":["./html:/usr/share/nginx/html"]},"db":{"image":"postgres:15","environment":{"POSTGRES_DB":"myapp"}}},"volumes":{"data":{"driver":"local"}}}}`

	env := newTestEnv(t, dir)
	env.RawArgsJSON = rawArgs

	res, err := h.Execute(ctx, env, map[string]any{
		"path": path,
		"data": map[string]any{
			"version": "3.8",
			"services": map[string]any{
				"web": map[string]any{
					"image":   "nginx:latest",
					"ports":   []any{"80:80"},
					"volumes": []any{"./html:/usr/share/nginx/html"},
				},
				"db": map[string]any{
					"image": "postgres:15",
					"environment": map[string]any{
						"POSTGRES_DB": "myapp",
					},
				},
			},
			"volumes": map[string]any{
				"data": map[string]any{
					"driver": "local",
				},
			},
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	fileData, err := os.ReadFile(path)
	require.NoError(t, err)
	content := string(fileData)

	// Key order: version → services → volumes (NOT alphabetical: services, version, volumes).
	versionIdx := strings.Index(content, "version:")
	servicesIdx := strings.Index(content, "services:")
	// Match top-level "volumes:" (starts at beginning of line with no indentation).
	// The nested "volumes:" inside the web service is indented, so searching for
	// "\nvolumes:" ensures we get the top-level key, not the nested one.
	volumesIdx := strings.Index(content, "\nvolumes:")

	require.NotEqual(t, -1, versionIdx, "version key should exist")
	require.NotEqual(t, -1, servicesIdx, "services key should exist")
	require.NotEqual(t, -1, volumesIdx, "volumes key should exist")

	require.Less(t, versionIdx, servicesIdx, "version should appear before services (NOT alphabetical)")
	require.Less(t, servicesIdx, volumesIdx, "services should appear before volumes")

	// Nested: web before db inside services.
	webIdx := strings.Index(content, "web:")
	dbIdx := strings.Index(content, "db:")
	require.NotEqual(t, -1, webIdx, "web service should exist")
	require.NotEqual(t, -1, dbIdx, "db service should exist")
	require.Less(t, webIdx, dbIdx, "web should appear before db")

	// Round-trip: parse back with yaml.Unmarshal and verify values.
	var parsed map[string]interface{}
	require.NoError(t, yaml.Unmarshal(fileData, &parsed), "output must be valid YAML")
	parsed = normalizeYAMLValue(parsed).(map[string]interface{})

	// Verify top-level values.
	ver, ok := parsed["version"].(string)
	require.True(t, ok, "version should be a string, got %T", parsed["version"])
	require.Equal(t, "3.8", ver, "version should round-trip as string")

	// Verify services structure.
	services, ok := parsed["services"].(map[string]interface{})
	require.True(t, ok, "services should be a map")
	require.Contains(t, services, "web", "web service should exist")
	require.Contains(t, services, "db", "db service should exist")

	// Verify web service details.
	web, ok := services["web"].(map[string]interface{})
	require.True(t, ok, "web should be a map")
	require.Equal(t, "nginx:latest", web["image"], "web image should match")

	// Verify db service details.
	db, ok := services["db"].(map[string]interface{})
	require.True(t, ok, "db should be a map")
	require.Equal(t, "postgres:15", db["image"], "db image should match")

	// Verify volumes structure.
	vols, ok := parsed["volumes"].(map[string]interface{})
	require.True(t, ok, "volumes should be a map")
	require.Contains(t, vols, "data", "data volume should exist")
}

// TestWriteStructuredFile_LargeDocument_Order writes a document with 50 keys
// in deterministic non-alphabetical order (k01…k50) via RawArgsJSON and
// verifies every key appears in the exact insertion order in the output.
func TestWriteStructuredFile_LargeDocument_Order(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &writeStructuredFileHandler{}
	ctx := newTestCtx(dir)

	path := filepath.Join(dir, "large.json")

	// Build a RawArgsJSON with 50 keys k01..k50 and values v01..v50.
	// Using raw JSON string so order is preserved from the source (NOT a Go map).
	var keys []string
	var parts []string
	for i := 1; i <= 50; i++ {
		k := fmt.Sprintf("k%02d", i)
		keys = append(keys, k)
		parts = append(parts, fmt.Sprintf(`"k%02d":"v%02d"`, i, i))
	}
	dataJSON := "{" + strings.Join(parts, ",") + "}"
	rawArgs := `{"path":"` + path + `","data":` + dataJSON + "}"

	env := newTestEnv(t, dir)
	env.RawArgsJSON = rawArgs

	res, err := h.Execute(ctx, env, map[string]any{
		"path": path,
		"data": nil, // Execute requires "data" to be present; ignored when RawArgsJSON is set
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	fileData, err := os.ReadFile(path)
	require.NoError(t, err)
	content := string(fileData)

	// Verify each key's byte offset is strictly less than the next key's.
	// This proves insertion order k01 → k02 → ... → k50 is preserved.
	for i := 0; i < len(keys)-1; i++ {
		currIdx := strings.Index(content, `"`+keys[i]+`"`)
		nextIdx := strings.Index(content, `"`+keys[i+1]+`"`)
		require.NotEqual(t, -1, currIdx, "key %s should exist in output", keys[i])
		require.NotEqual(t, -1, nextIdx, "key %s should exist in output", keys[i+1])
		require.Less(t, currIdx, nextIdx,
			"key %s (pos %d) should appear before key %s (pos %d) — insertion order must be preserved",
			keys[i], currIdx, keys[i+1], nextIdx)
	}
}
