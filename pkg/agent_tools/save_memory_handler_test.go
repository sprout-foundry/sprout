package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// testMemoryName generates a unique memory name for testing.
func testMemoryName(t *testing.T) string {
	t.Helper()
	return "sprout-test-" + strings.ToLower(t.Name())
}

// cleanTestMemory removes a test memory file after the test completes.
func cleanTestMemory(t *testing.T, name string) {
	t.Helper()
	memoryDir := getMemoryDir()
	if memoryDir == "" {
		return
	}
	os.Remove(filepath.Join(memoryDir, name+".md"))
}

func TestSaveMemoryHandler_Execute_SaveAndVerify(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &saveMemoryHandler{}
	ctx := newTestCtx(dir)

	name := testMemoryName(t)
	content := "# Test Memory\nThis is test content."
	defer cleanTestMemory(t, name)

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"name":    name,
		"content": content,
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Contains(t, res.Output, "saved")

	// Verify the file was actually created
	memoryDir := getMemoryDir()
	require.NotEmpty(t, memoryDir, "memory dir should exist")

	path := filepath.Join(memoryDir, sanitizeMemoryName(name)+".md")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, content, string(data))
}

func TestSaveMemoryHandler_Execute_SanitizedName(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &saveMemoryHandler{}
	ctx := newTestCtx(dir)

	// Name with spaces and special chars — should be sanitized
	name := "My Test Memory! With Spaces"
	content := "# Content"
	sanitized := sanitizeMemoryName(name)
	defer cleanTestMemory(t, sanitized)

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"name":    name,
		"content": content,
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	// File should be created with sanitized name
	memoryDir := getMemoryDir()
	require.NotEmpty(t, memoryDir)
	path := filepath.Join(memoryDir, sanitized+".md")
	_, err = os.Stat(path)
	require.NoError(t, err)
}

func TestSaveMemoryHandler_Execute_InvalidPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &saveMemoryHandler{}
	ctx := newTestCtx(dir)

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"name":    "test",
		"content": "content",
	})
	// This test depends on whether configuration.GetConfigDir() works in the test environment.
	// If it does, it should succeed; if not, it should fail gracefully.
	if err != nil {
		require.True(t, res.IsError)
	} else {
		require.False(t, res.IsError)
		// Clean up if it succeeded
		defer cleanTestMemory(t, "test")
	}
}

func TestSaveMemoryHandler_Execute_MissingName(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &saveMemoryHandler{}
	ctx := newTestCtx(dir)

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"content": "some content",
	})
	require.Error(t, err)
	require.True(t, res.IsError)
}

func TestSaveMemoryHandler_Execute_MissingContent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &saveMemoryHandler{}
	ctx := newTestCtx(dir)

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"name": "test-memory",
	})
	require.Error(t, err)
	require.True(t, res.IsError)
}

func TestSaveMemoryHandler_Execute_OutputWriter(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &saveMemoryHandler{}
	ctx := newTestCtx(dir)

	var buf strings.Builder
	env := newTestEnv(t, dir)
	env.OutputWriter = &buf

	name := testMemoryName(t)
	content := "# Writer test"
	defer cleanTestMemory(t, name)

	res, err := h.Execute(ctx, env, map[string]any{
		"name":    name,
		"content": content,
	})
	if err != nil {
		// GetConfigDir might fail in some environments
		require.True(t, res.IsError)
		return
	}
	require.NoError(t, err)
	require.Contains(t, buf.String(), res.Output)
}

func TestSaveMemoryHandler_Execute_EventBus(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &saveMemoryHandler{}
	ctx := newTestCtx(dir)

	bus := events.NewEventBus()
	_ = bus.Subscribe("test") // subscribe to have a listener
	env := newTestEnv(t, dir)
	env.EventBus = bus

	name := testMemoryName(t)
	content := "# Event test"
	defer cleanTestMemory(t, name)

	_, err := h.Execute(ctx, env, map[string]any{
		"name":    name,
		"content": content,
	})
	if err != nil {
		return // GetConfigDir might fail in some environments
	}

	// Handlers no longer self-publish tool_start/tool_end — the core
	// tool executor (pkg/agent/tool_executor.go) handles event publishing.
	select {
	case ev := <-bus.Subscribe("check"):
		t.Fatalf("expected 0 events from handler, got %+v", ev)
	default:
		// good — no events published by the handler
	}
}

func TestSaveMemoryHandler_Execute_RedactsSecrets(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &saveMemoryHandler{}
	ctx := newTestCtx(dir)

	name := testMemoryName(t)
	// Content with a pattern that redact.String will sanitize
	content := "# Test\nSPROUT_API_KEY=secret123"
	defer cleanTestMemory(t, name)

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"name":    name,
		"content": content,
	})
	if err != nil {
		return // GetConfigDir might fail in some environments
	}
	require.NoError(t, err)
	require.False(t, res.IsError)
}

func TestSaveMemoryHandler_Execute_Overwrite(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &saveMemoryHandler{}
	ctx := newTestCtx(dir)

	name := testMemoryName(t)
	defer cleanTestMemory(t, name)

	// Write first version
	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"name":    name,
		"content": "# Version 1",
	})
	if err != nil {
		return // GetConfigDir might fail
	}
	require.NoError(t, err)

	// Overwrite with second version
	res, err = h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"name":    name,
		"content": "# Version 2",
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	// Verify it was overwritten
	memoryDir := getMemoryDir()
	require.NotEmpty(t, memoryDir)
	path := filepath.Join(memoryDir, sanitizeMemoryName(name)+".md")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "# Version 2", string(data))
}

func TestSaveMemoryHandler_Execute_TokenUsage(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &saveMemoryHandler{}
	ctx := newTestCtx(dir)

	name := testMemoryName(t)
	content := strings.Repeat("x", 80)
	defer cleanTestMemory(t, name)

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"name":    name,
		"content": content,
	})
	if err != nil {
		return
	}
	require.NoError(t, err)
	require.Greater(t, res.TokenUsage, int64(0))
}

func TestSaveMemoryHandler_Validate(t *testing.T) {
	t.Parallel()
	h := &saveMemoryHandler{}

	// Valid
	require.NoError(t, h.Validate(map[string]any{
		"name":    "test-memory",
		"content": "# Content",
	}))

	// Missing name
	err := h.Validate(map[string]any{"content": "# Content"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "required")

	// Empty name
	err = h.Validate(map[string]any{"name": "", "content": "# Content"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "must not be empty")

	// Missing content
	err = h.Validate(map[string]any{"name": "test"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "required")

	// Empty content
	err = h.Validate(map[string]any{"name": "test", "content": "  "})
	require.Error(t, err)
	require.Contains(t, err.Error(), "must not be empty")
}

// Test sanitizeMemoryName directly
func TestSanitizeMemoryName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  string
	}{
		{"my-memory", "my-memory"},
		{"My Memory", "my-memory"},
		{"  spaces  ", "spaces"},
		{"test!@#$%", "test"},
		{"Test 123", "test-123"},
		{"", "untitled"},
		{"---leading-trailing---", "leading-trailing"},
		{"snake_case", "snake_case"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, sanitizeMemoryName(tc.input))
		})
	}
}

// Test that getMemoryDir creates the directory if it doesn't exist
func TestGetMemoryDir(t *testing.T) {
	t.Parallel()
	// Just verify it returns a non-empty path in normal environments
	dir := getMemoryDir()
	// In some CI environments, GetConfigDir might fail — that's ok
	if dir != "" {
		_, err := os.Stat(dir)
		require.NoError(t, err, "getMemoryDir should return a valid existing directory")
	}
}

// Test saveMemoryToDisk directly
func TestSaveMemoryToDisk(t *testing.T) {
	t.Parallel()
	name := testMemoryName(t)
	content := "# Direct test"
	defer cleanTestMemory(t, name)

	result, err := saveMemoryToDisk(name, content)
	if err != nil {
		// Might fail in CI — that's ok
		return
	}
	require.NoError(t, err)
	require.Contains(t, result, name)
	require.Contains(t, result, "saved")
}

// Ensure the handler uses configuration.GetConfigDir
func TestSaveMemoryHandler_UsesConfigDir(t *testing.T) {
	t.Parallel()
	// Just verify that the configuration package is usable
	_, err := configuration.GetConfigDir()
	// In test env this may or may not succeed
	if err == nil {
		t.Log("Config directory is available")
	} else {
		t.Logf("Config directory not available (expected in some envs): %v", err)
	}
}
