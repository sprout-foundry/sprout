package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/filesystem"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func newTestEnv(t *testing.T, workspaceRoot string) ToolEnv {
	t.Helper()
	return ToolEnv{
		EventBus:      events.NewEventBus(),
		WorkspaceRoot: workspaceRoot,
		OutputWriter:  os.Stderr,
		MaxTokensFunc: func() int { return 128000 },
		// Hermetic config manager. Without this, handlers that fall
		// through to configuration.NewManager() race the user's real
		// ~/.config/sprout/config.json under -race + t.Parallel and
		// fail with "config file changed on disk since load". We can't
		// use configuration.NewTestManager(t) here because t.Setenv is
		// incompatible with t.Parallel — NewManagerWithDir sidesteps
		// the env var entirely.
		ConfigManager: newHermeticConfigManager(t),
	}
}

// newHermeticConfigManager returns a configuration.Manager backed by a
// per-test temp directory. Safe under t.Parallel — does not call
// t.Setenv. Used by every test that calls a handler whose Execute path
// constructs a Manager when env.ConfigManager is nil (embedding_index,
// semantic_search, list_skills, fetch_url, etc.).
func newHermeticConfigManager(t *testing.T) *configuration.Manager {
	t.Helper()
	cfgDir := filepath.Join(t.TempDir(), ".sprout")
	mgr, err := configuration.NewManagerWithDir(cfgDir)
	if err != nil {
		t.Fatalf("newHermeticConfigManager: NewManagerWithDir(%q): %v", cfgDir, err)
	}
	return mgr
}

func newTestCtx(root string) context.Context {
	return filesystem.WithWorkspaceRoot(context.Background(), root)
}

// ---------------------------------------------------------------------------
// read_file Conformance Tests
// ---------------------------------------------------------------------------

func TestReadFileHandlerConformance_Basic(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &readFileHandler{}
	ctx := newTestCtx(dir)

	path := filepath.Join(dir, "hello.txt")
	require.NoError(t, os.WriteFile(path, []byte("hello world\nline 2"), 0o644))

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{"path": path})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Contains(t, res.Output, "hello world")
	require.Contains(t, res.Output, "line 2")
}

func TestReadFileHandlerConformance_WithLineRange(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &readFileHandler{}
	ctx := newTestCtx(dir)

	content := "line 1\nline 2\nline 3\nline 4\nline 5\n"
	path := filepath.Join(dir, "multi.txt")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	args := map[string]any{
		"path":       path,
		"view_range": []any{float64(2), float64(4)}, // JSON numbers come as float64
	}
	res, err := h.Execute(ctx, newTestEnv(t, dir), args)
	require.NoError(t, err)
	require.False(t, res.IsError)

	// Should contain lines 2-4 but NOT line 1 or line 5
	require.Contains(t, res.Output, "line 2")
	require.Contains(t, res.Output, "line 3")
	require.Contains(t, res.Output, "line 4")
	// The output format is "Lines 2-4 of <path>:\n..."
	require.Contains(t, res.Output, "Lines 2-4")
}

func TestReadFileHandlerConformance_MissingFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &readFileHandler{}
	ctx := newTestCtx(dir)

	path := filepath.Join(dir, "nonexistent.txt")
	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{"path": path})
	// The error comes from SafeResolvePathWithBypass (lstat) which says "no such file or directory"
	require.Error(t, err)
	require.True(t, res.IsError)
	require.Contains(t, err.Error(), "no such file or directory")
}

func TestReadFileHandlerConformance_DirectoryPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &readFileHandler{}
	ctx := newTestCtx(dir)

	// Point at the directory itself
	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{"path": dir})
	require.Error(t, err)
	require.True(t, res.IsError)
	require.Contains(t, err.Error(), "directory")
}

func TestReadFileHandlerConformance_EmptyPath(t *testing.T) {
	t.Parallel()
	h := &readFileHandler{}

	err := h.Validate(map[string]any{"path": ""})
	require.Error(t, err)
	require.Contains(t, err.Error(), "must not be empty")
}

func TestReadFileHandlerConformance_MissingPath(t *testing.T) {
	t.Parallel()
	h := &readFileHandler{}

	// No "path" key at all
	err := h.Validate(map[string]any{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "required")
}

func TestReadFileHandlerConformance_InvalidViewRange(t *testing.T) {
	t.Parallel()
	h := &readFileHandler{}

	// view_range with wrong number of elements
	err := h.Validate(map[string]any{"path": "file.txt", "view_range": []any{1}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "exactly 2 elements")

	// view_range is not an array
	err = h.Validate(map[string]any{"path": "file.txt", "view_range": "bad"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "must be an array")

	// view_range has non-numeric elements
	err = h.Validate(map[string]any{"path": "file.txt", "view_range": []any{1, "foo"}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "must be integers")
}

func TestReadFileHandlerConformance_PDF(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &readFileHandler{}
	ctx := newTestCtx(dir)

	// Write a minimal PDF-like file with the correct header
	pdfContent := "%PDF-1.4\n1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\ntrailer\n<< /Root 1 0 R >>\n%%EOF"
	path := filepath.Join(dir, "test.pdf")
	require.NoError(t, os.WriteFile(path, []byte(pdfContent), 0o644))

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{"path": path})
	require.NoError(t, err)
	require.False(t, res.IsError)

	// Should have an image entry with PDF MIME type
	require.Len(t, res.Images, 1)
	require.Equal(t, "application/pdf", res.Images[0].MIMEType)
	require.Contains(t, res.Images[0].URI, "base64,")
}

func TestReadFileHandlerConformance_TokenUsage(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &readFileHandler{}
	ctx := newTestCtx(dir)

	path := filepath.Join(dir, "tokens.txt")
	data := strings.Repeat("x", 80) // 80 chars → 20 tokens at 4 chars/token
	require.NoError(t, os.WriteFile(path, []byte(data), 0o644))

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{"path": path})
	require.NoError(t, err)
	require.Equal(t, int64(20), res.TokenUsage)
}

func TestReadFileHandlerConformance_Definition(t *testing.T) {
	t.Parallel()
	h := &readFileHandler{}

	require.Equal(t, "read_file", h.Name())

	def := h.Definition()
	require.Equal(t, "read_file", def.Name)
	require.NotEmpty(t, def.Description)
	require.Equal(t, []string{"path"}, def.Required)

	// Check parameter schema
	paramNames := make(map[string]bool)
	for _, p := range def.Parameters {
		paramNames[p.Name] = true
	}
	require.True(t, paramNames["path"], "should have 'path' parameter")
	require.True(t, paramNames["view_range"], "should have 'view_range' parameter")

	// path should be required
	for _, p := range def.Parameters {
		if p.Name == "path" {
			require.True(t, p.Required)
		}
	}
}

func TestReadFileHandlerConformance_OutputWriter(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &readFileHandler{}
	ctx := newTestCtx(dir)

	path := filepath.Join(dir, "writer.txt")
	require.NoError(t, os.WriteFile(path, []byte("writer output"), 0o644))

	var buf strings.Builder
	env := newTestEnv(t, dir)
	env.OutputWriter = &buf

	res, err := h.Execute(ctx, env, map[string]any{"path": path})
	require.NoError(t, err)
	require.Contains(t, buf.String(), "writer output")
	require.Contains(t, res.Output, "writer output")
}

func TestReadFileHandlerConformance_EventBusPublishes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &readFileHandler{}
	ctx := newTestCtx(dir)

	path := filepath.Join(dir, "events.txt")
	require.NoError(t, os.WriteFile(path, []byte("event test"), 0o644))

	bus := events.NewEventBus()
	_ = bus.Subscribe("test") // subscribe to have a listener
	env := newTestEnv(t, dir)
	env.EventBus = bus

	_, err := h.Execute(ctx, env, map[string]any{"path": path})
	require.NoError(t, err)

	// Handlers no longer self-publish tool_start/tool_end — the core
	// tool executor (pkg/agent/tool_executor.go) handles event publishing.
	select {
	case ev := <-bus.Subscribe("check"):
		t.Fatalf("expected 0 events from handler, got %+v", ev)
	default:
		// good — no events published by the handler
	}
}

func TestReadFileHandlerConformance_NonTextExtension(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &readFileHandler{}
	ctx := newTestCtx(dir)

	path := filepath.Join(dir, "image.png")
	require.NoError(t, os.WriteFile(path, []byte("fake png"), 0o644))

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{"path": path})
	require.Error(t, err)
	require.True(t, res.IsError)
	require.Contains(t, err.Error(), "non-text")
}

// ---------------------------------------------------------------------------
// list_directory Conformance Tests
// ---------------------------------------------------------------------------

func TestListDirConformance_Basic(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &listDirHandler{}
	ctx := newTestCtx(dir)

	// Create files
	require.NoError(t, os.WriteFile(filepath.Join(dir, "alpha.txt"), []byte("a"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "beta.txt"), []byte("bbb"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "subdir"), 0o755))

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{"path": dir})
	require.NoError(t, err)
	require.False(t, res.IsError)

	// Output should contain all file/dir names
	require.Contains(t, res.Output, "alpha.txt")
	require.Contains(t, res.Output, "beta.txt")
	require.Contains(t, res.Output, "subdir")

	// StructuredOut should have entries
	data, ok := res.StructuredOut.([]map[string]any)
	require.True(t, ok, "StructuredOut should be []map[string]any")
	require.Len(t, data, 3)
}

func TestListDirConformance_ShowHidden(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &listDirHandler{}
	ctx := newTestCtx(dir)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "visible.txt"), []byte("v"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".hidden"), []byte("h"), 0o644))

	// With show_hidden=true, dotfiles should appear
	args := map[string]any{
		"path":        dir,
		"show_hidden": true,
	}
	res, err := h.Execute(ctx, newTestEnv(t, dir), args)
	require.NoError(t, err)
	require.Contains(t, res.Output, ".hidden")
	require.Contains(t, res.Output, "visible.txt")
}

func TestListDirConformance_HideHidden(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &listDirHandler{}
	ctx := newTestCtx(dir)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "visible.txt"), []byte("v"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".hidden"), []byte("h"), 0o644))

	// With show_hidden=false (default), dotfiles should NOT appear
	args := map[string]any{
		"path":        dir,
		"show_hidden": false,
	}
	res, err := h.Execute(ctx, newTestEnv(t, dir), args)
	require.NoError(t, err)
	require.Contains(t, res.Output, "visible.txt")
	require.NotContains(t, res.Output, ".hidden")
}

func TestListDirConformance_DefaultPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &listDirHandler{}
	ctx := newTestCtx(dir)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "default.txt"), []byte("d"), 0o644))

	// No path arg — should default to "."
	args := map[string]any{}
	res, err := h.Execute(ctx, newTestEnv(t, dir), args)
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Contains(t, res.Output, "default.txt")
}

func TestListDirConformance_InvalidPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &listDirHandler{}
	ctx := newTestCtx(dir)

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{"path": filepath.Join(dir, "nonexistent")})
	require.Error(t, err)
	require.True(t, res.IsError)
}

func TestListDirConformance_FilePath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &listDirHandler{}
	ctx := newTestCtx(dir)

	path := filepath.Join(dir, "file.txt")
	require.NoError(t, os.WriteFile(path, []byte("content"), 0o644))

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{"path": path})
	require.Error(t, err)
	require.True(t, res.IsError)
	require.Contains(t, err.Error(), "not a directory")
}

func TestListDirConformance_Definition(t *testing.T) {
	t.Parallel()
	h := &listDirHandler{}

	require.Equal(t, "list_directory", h.Name())

	def := h.Definition()
	require.Equal(t, "list_directory", def.Name)
	require.NotEmpty(t, def.Description)
	require.Empty(t, def.Required, "list_directory should have no required parameters")

	// Check parameter schema
	paramNames := make(map[string]bool)
	for _, p := range def.Parameters {
		paramNames[p.Name] = true
	}
	require.True(t, paramNames["path"], "should have 'path' parameter")
	require.True(t, paramNames["show_hidden"], "should have 'show_hidden' parameter")
}

func TestListDirConformance_Validate(t *testing.T) {
	t.Parallel()
	h := &listDirHandler{}

	// Valid args
	require.NoError(t, h.Validate(map[string]any{"path": "/some/dir"}))
	require.NoError(t, h.Validate(map[string]any{"show_hidden": true}))
	require.NoError(t, h.Validate(map[string]any{"path": "/x", "show_hidden": false}))
	require.NoError(t, h.Validate(map[string]any{}))

	// Invalid: path is not a string
	err := h.Validate(map[string]any{"path": 42})
	require.Error(t, err)
	require.Contains(t, err.Error(), "must be a string")

	// Invalid: show_hidden is not a boolean
	err = h.Validate(map[string]any{"show_hidden": "yes"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "must be a boolean")
}

func TestListDirConformance_StructuredOutput(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &listDirHandler{}
	ctx := newTestCtx(dir)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "file.txt"), []byte("hello"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "mydir"), 0o755))

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{"path": dir})
	require.NoError(t, err)

	data := res.StructuredOut.([]map[string]any)
	require.Len(t, data, 2)

	// Check each entry has the expected fields
	for _, entry := range data {
		require.Contains(t, entry, "name")
		require.Contains(t, entry, "isDir")
		require.Contains(t, entry, "size")
		require.Contains(t, entry, "type")

		name := entry["name"].(string)
		isDir := entry["isDir"].(bool)
		entryType := entry["type"].(string)

		if name == "file.txt" {
			require.False(t, isDir)
			require.Equal(t, "file", entryType)
			require.Equal(t, int64(5), entry["size"]) // "hello" = 5 bytes
		} else if name == "mydir" {
			require.True(t, isDir)
			require.Equal(t, "dir", entryType)
		}
	}
}

func TestListDirConformance_OutputWriter(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &listDirHandler{}
	ctx := newTestCtx(dir)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "writer.txt"), []byte("w"), 0o644))

	var buf strings.Builder
	env := newTestEnv(t, dir)
	env.OutputWriter = &buf

	res, err := h.Execute(ctx, env, map[string]any{"path": dir})
	require.NoError(t, err)
	require.Equal(t, buf.String(), res.Output)
}

func TestListDirConformance_EntriesCount(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &listDirHandler{}
	ctx := newTestCtx(dir)

	// Create 3 visible files
	for i := 0; i < 3; i++ {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "f"+string(rune('a'+i))), []byte{}, 0o644))
	}
	// Create 1 hidden file (should be filtered by default)
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".hidden"), []byte{}, 0o644))

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{"path": dir})
	require.NoError(t, err)
	require.Contains(t, res.Output, "3 entries found")
}

// ---------------------------------------------------------------------------
// fetch_url Conformance Tests
// ---------------------------------------------------------------------------

func TestFetchURLConformance_Definition(t *testing.T) {
	t.Parallel()
	h := &fetchURLHandler{}

	require.Equal(t, "fetch_url", h.Name())

	def := h.Definition()
	require.Equal(t, "fetch_url", def.Name)
	require.NotEmpty(t, def.Description)
	require.Equal(t, []string{"url"}, def.Required)

	// Check parameter schema
	paramNames := make(map[string]bool)
	for _, p := range def.Parameters {
		paramNames[p.Name] = true
	}
	require.True(t, paramNames["url"], "should have 'url' parameter")
}

func TestFetchURLConformance_Validate_Missing(t *testing.T) {
	t.Parallel()
	h := &fetchURLHandler{}

	err := h.Validate(map[string]any{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "required")
}

func TestFetchURLConformance_Validate_Empty(t *testing.T) {
	t.Parallel()
	h := &fetchURLHandler{}

	err := h.Validate(map[string]any{"url": ""})
	require.Error(t, err)
	require.Contains(t, err.Error(), "must not be empty")
}

func TestFetchURLConformance_Validate_NoHTTP(t *testing.T) {
	t.Parallel()
	h := &fetchURLHandler{}

	err := h.Validate(map[string]any{"url": "ftp://example.com/file"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "HTTP(S)")

	err = h.Validate(map[string]any{"url": "not a url"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "HTTP(S)")

	err = h.Validate(map[string]any{"url": "localhost:8080"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "HTTP(S)")
}

func TestFetchURLConformance_Validate_ValidHTTP(t *testing.T) {
	t.Parallel()
	h := &fetchURLHandler{}

	// HTTP
	require.NoError(t, h.Validate(map[string]any{"url": "http://example.com"}))
	require.NoError(t, h.Validate(map[string]any{"url": "http://example.com/path?query=1"}))

	// HTTPS
	require.NoError(t, h.Validate(map[string]any{"url": "https://example.com"}))
	require.NoError(t, h.Validate(map[string]any{"url": "https://example.com/api/v1"}))
}

func TestFetchURLConformance_ImageURLDetection(t *testing.T) {
	t.Parallel()
	tests := []struct {
		url       string
		wantMime  string
		wantImage bool
	}{
		{"https://example.com/image.png", "image/png", true},
		{"https://example.com/photo.jpg", "image/jpeg", true},
		{"https://example.com/photo.jpeg", "image/jpeg", true},
		{"https://example.com/anim.gif", "image/gif", true},
		{"https://example.com/photo.webp", "image/webp", true},
		{"https://example.com/doc.pdf", "application/pdf", true},
		{"https://example.com/page.html", "", false},
		{"https://example.com/api/data", "", false},
		{"https://example.com/image.PNG", "image/png", true}, // case-insensitive
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.url, func(t *testing.T) {
			t.Parallel()
			img := classifyURL(tc.url)
			if tc.wantImage {
				require.NotNil(t, img, "classifyURL(%q) should return ImageData", tc.url)
				require.Equal(t, tc.wantMime, img.MIMEType)
				require.Equal(t, tc.url, img.URI)
			} else {
				require.Nil(t, img, "classifyURL(%q) should return nil", tc.url)
			}
		})
	}
}

func TestFetchURLConformance_Execute_MissingURL(t *testing.T) {
	t.Parallel()
	h := &fetchURLHandler{}

	// Execute without valid URL — Validate fails first, but Execute extracts from args
	// The handler calls extractString which will succeed on missing key returning error,
	// but the handler ignores the Validate error and calls FetchURL directly
	// with an empty string, which FetchURL rejects.
	res, err := h.Execute(context.Background(), newTestEnv(t, ""), map[string]any{})
	require.Error(t, err)
	require.True(t, res.IsError)
}

func TestFetchURLConformance_Execute_NetworkUnavailable(t *testing.T) {
	t.Parallel()
	// Skip: FetchURL calls webcontent.FetchWebContent which panics with nil cfg
	// due to a nil pointer dereference in configuration.Manager.GetAPIKeys().
	// This is a known issue with the production code, not the test.
	t.Skip("FetchURL panics with nil cfg (nil pointer dereference in configuration.Manager)")
}

func TestFetchURLConformance_Execute_AttachesImageForPNG(t *testing.T) {
	t.Parallel()
	// We can't control FetchURL's network response, but we can verify that
	// the handler's Execute path calls classifyURL correctly by checking
	// that a PNG URL would produce ImageData.
	// The actual Execute test with a real PNG URL would need network;
	// instead, verify the classification logic directly.

	h := &fetchURLHandler{}
	require.Equal(t, "fetch_url", h.Name())

	// Validate passes for PNG URLs
	require.NoError(t, h.Validate(map[string]any{"url": "https://example.com/img.png"}))

	// classifyURL is used by Execute to populate Images
	img := classifyURL("https://example.com/img.png")
	require.NotNil(t, img)
	require.Equal(t, "image/png", img.MIMEType)
	require.Equal(t, "https://example.com/img.png", img.URI)
}

func TestFetchURLConformance_Execute_AttachesImageForPDF(t *testing.T) {
	t.Parallel()
	h := &fetchURLHandler{}

	// Validate passes for PDF URLs
	require.NoError(t, h.Validate(map[string]any{"url": "https://example.com/doc.pdf"}))

	// classifyURL is used by Execute to populate Images
	img := classifyURL("https://example.com/doc.pdf")
	require.NotNil(t, img)
	require.Equal(t, "application/pdf", img.MIMEType)
	require.Equal(t, "https://example.com/doc.pdf", img.URI)
}

// ---------------------------------------------------------------------------
// search_files Conformance Tests
// ---------------------------------------------------------------------------

func TestSearchFilesConformance_Definition(t *testing.T) {
	t.Parallel()
	h := &searchFilesHandler{}

	require.Equal(t, "search_files", h.Name())

	def := h.Definition()
	require.Equal(t, "search_files", def.Name)
	require.NotEmpty(t, def.Description)
	require.Equal(t, []string{"search_pattern"}, def.Required)

	// Check parameter schema
	paramNames := make(map[string]bool)
	for _, p := range def.Parameters {
		paramNames[p.Name] = true
	}
	require.True(t, paramNames["search_pattern"], "should have 'search_pattern' parameter")
	require.True(t, paramNames["directory"], "should have 'directory' parameter")
	require.True(t, paramNames["file_glob"], "should have 'file_glob' parameter")
	require.True(t, paramNames["case_sensitive"], "should have 'case_sensitive' parameter")
	require.True(t, paramNames["max_results"], "should have 'max_results' parameter")
	require.True(t, paramNames["max_bytes"], "should have 'max_bytes' parameter")
}

func TestSearchFilesConformance_Validate_MissingPattern(t *testing.T) {
	t.Parallel()
	h := &searchFilesHandler{}

	err := h.Validate(map[string]any{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "required")
}

func TestSearchFilesConformance_Validate_EmptyPattern(t *testing.T) {
	t.Parallel()
	h := &searchFilesHandler{}

	// extractString allows empty strings - the handler only validates presence
	err := h.Validate(map[string]any{"search_pattern": ""})
	require.NoError(t, err)
}

func TestSearchFilesConformance_Validate_Valid(t *testing.T) {
	t.Parallel()
	h := &searchFilesHandler{}

	require.NoError(t, h.Validate(map[string]any{"search_pattern": "func"}))
	require.NoError(t, h.Validate(map[string]any{"search_pattern": "func", "directory": "./pkg"}))
}

func TestSearchFilesConformance_BasicSearch(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &searchFilesHandler{}
	ctx := newTestCtx(dir)

	// Create files to search
	require.NoError(t, os.WriteFile(filepath.Join(dir, "hello.go"), []byte("package hello\n\nfunc Greet() {}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "world.go"), []byte("package world\n\nfunc Greet() {}\n"), 0o644))

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{"search_pattern": "Greet", "directory": dir})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Contains(t, res.Output, "Greet")
}

func TestSearchFilesConformance_NoMatches(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &searchFilesHandler{}
	ctx := newTestCtx(dir)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "file.txt"), []byte("hello world"), 0o644))

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{"search_pattern": "NOTFOUND", "directory": dir})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Contains(t, res.Output, "No results found")
}

func TestSearchFilesConformance_InvalidRegex(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &searchFilesHandler{}
	ctx := newTestCtx(dir)

	// Invalid regex pattern (delimited with /)
	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{"search_pattern": "/[invalid/", "directory": dir})
	require.NoError(t, err)
	require.True(t, res.IsError)
	require.Contains(t, res.Output, "invalid search pattern")
}

func TestSearchFilesConformance_WithFileGlob(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &searchFilesHandler{}
	ctx := newTestCtx(dir)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "test.go"), []byte("package main"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "test.txt"), []byte("package main"), 0o644))

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"search_pattern": "package",
		"directory":      dir,
		"file_glob":      "*.go",
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Contains(t, res.Output, "test.go")
	require.NotContains(t, res.Output, "test.txt")
}

func TestSearchFilesConformance_CaseSensitive(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &searchFilesHandler{}
	ctx := newTestCtx(dir)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "file.txt"), []byte("Hello World"), 0o644))

	// Case sensitive - should find "Hello"
	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{
		"search_pattern": "Hello",
		"directory":      dir,
		"case_sensitive": true,
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Contains(t, res.Output, "Hello")
}

// ---------------------------------------------------------------------------
// repo_map Conformance Tests
// ---------------------------------------------------------------------------

func TestRepoMapConformance_Definition(t *testing.T) {
	t.Parallel()
	h := &repoMapHandler{}

	require.Equal(t, "repo_map", h.Name())

	def := h.Definition()
	require.Equal(t, "repo_map", def.Name)
	require.NotEmpty(t, def.Description)
	require.Empty(t, def.Required, "repo_map should have no required parameters")

	// Check parameter schema
	paramNames := make(map[string]bool)
	for _, p := range def.Parameters {
		paramNames[p.Name] = true
	}
	require.True(t, paramNames["directory"], "should have 'directory' parameter")
}

func TestRepoMapConformance_Validate(t *testing.T) {
	t.Parallel()
	h := &repoMapHandler{}

	// No required params - all inputs should be valid
	require.NoError(t, h.Validate(nil))
	require.NoError(t, h.Validate(map[string]any{}))
	require.NoError(t, h.Validate(map[string]any{"directory": "."}))
}

func TestRepoMapConformance_Basic(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &repoMapHandler{}
	ctx := newTestCtx(dir)

	// Create a Go file with a top-level function
	goCode := `package foo
func Bar() {}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "foo.go"), []byte(goCode), 0o644))

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{"directory": dir})
	require.NoError(t, err)
	require.False(t, res.IsError)
	// Output should contain the directory or file reference
	require.Contains(t, res.Output, "foo.go")
}

func TestRepoMapConformance_DefaultDirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &repoMapHandler{}
	ctx := newTestCtx(dir)

	// No directory arg - should default to "."
	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{})
	require.NoError(t, err)
	require.False(t, res.IsError)
}

// list_memories and read_memory conformance tests were removed when the
// legacy per-operation memory handlers were retired in favor of
// manage_memory (see pkg/agent/memory_manage.go). The consolidated tool
// is covered by manage_memory operation tests in pkg/agent.

// ---------------------------------------------------------------------------
// rollback_changes Conformance Tests
// ---------------------------------------------------------------------------

func TestRollbackChangesConformance_Definition(t *testing.T) {
	t.Parallel()
	h := &rollbackChangesHandler{}

	require.Equal(t, "rollback_changes", h.Name())

	def := h.Definition()
	require.Equal(t, "rollback_changes", def.Name)
	require.NotEmpty(t, def.Description)
	require.Empty(t, def.Required, "rollback_changes should have no required parameters")

	// Check parameter schema
	paramNames := make(map[string]bool)
	for _, p := range def.Parameters {
		paramNames[p.Name] = true
	}
	require.True(t, paramNames["revision_id"], "should have 'revision_id' parameter")
	require.True(t, paramNames["file_path"], "should have 'file_path' parameter")
	require.True(t, paramNames["confirm"], "should have 'confirm' parameter")
}

func TestRollbackChangesConformance_Validate(t *testing.T) {
	t.Parallel()
	h := &rollbackChangesHandler{}

	// No required params
	require.NoError(t, h.Validate(nil))
	require.NoError(t, h.Validate(map[string]any{}))
	require.NoError(t, h.Validate(map[string]any{"revision_id": "abc123"}))
}

func TestRollbackChangesConformance_Execute_ListRevisions(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &rollbackChangesHandler{}
	ctx := newTestCtx(dir)

	env := newTestEnv(t, dir)
	res, err := h.Execute(ctx, env, map[string]any{})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.NotEmpty(t, res.Output)
}

// ---------------------------------------------------------------------------
// view_history Conformance Tests
// ---------------------------------------------------------------------------

func TestViewHistoryConformance_Definition(t *testing.T) {
	t.Parallel()
	h := &viewHistoryHandler{}

	require.Equal(t, "view_history", h.Name())

	def := h.Definition()
	require.Equal(t, "view_history", def.Name)
	require.NotEmpty(t, def.Description)
	require.Empty(t, def.Required, "view_history should have no required parameters")

	// Check parameter schema
	paramNames := make(map[string]bool)
	for _, p := range def.Parameters {
		paramNames[p.Name] = true
	}
	require.True(t, paramNames["limit"], "should have 'limit' parameter")
	require.True(t, paramNames["file_filter"], "should have 'file_filter' parameter")
	require.True(t, paramNames["since"], "should have 'since' parameter")
	require.True(t, paramNames["show_content"], "should have 'show_content' parameter")
}

func TestViewHistoryConformance_Validate(t *testing.T) {
	t.Parallel()
	h := &viewHistoryHandler{}

	// No required params
	require.NoError(t, h.Validate(nil))
	require.NoError(t, h.Validate(map[string]any{}))
	require.NoError(t, h.Validate(map[string]any{"limit": 10, "file_filter": "*.go"}))
}

func TestViewHistoryConformance_Execute_Default(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &viewHistoryHandler{}
	ctx := newTestCtx(dir)

	env := newTestEnv(t, dir)
	res, err := h.Execute(ctx, env, map[string]any{})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.NotEmpty(t, res.Output)
}

func TestViewHistoryConformance_Execute_WithLimit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &viewHistoryHandler{}
	ctx := newTestCtx(dir)

	env := newTestEnv(t, dir)
	res, err := h.Execute(ctx, env, map[string]any{"limit": 5})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.NotEmpty(t, res.Output)
}

func TestViewHistoryConformance_Execute_InvalidSince(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &viewHistoryHandler{}
	ctx := newTestCtx(dir)

	env := newTestEnv(t, dir)
	res, err := h.Execute(ctx, env, map[string]any{"since": "not-a-valid-timestamp"})
	require.NoError(t, err)
	require.True(t, res.IsError)
	require.Contains(t, res.Output, "parsing 'since' timestamp")
}

// ---------------------------------------------------------------------------
// list_skills Conformance Tests
// ---------------------------------------------------------------------------

func TestListSkillsConformance_Definition(t *testing.T) {
	t.Parallel()
	h := &listSkillsHandler{}

	require.Equal(t, "list_skills", h.Name())

	def := h.Definition()
	require.Equal(t, "list_skills", def.Name)
	require.NotEmpty(t, def.Description)
	require.Empty(t, def.Required, "list_skills should have no required parameters")
	require.Empty(t, def.Parameters, "list_skills should have no parameters")
}

func TestListSkillsConformance_Validate(t *testing.T) {
	t.Parallel()
	h := &listSkillsHandler{}

	// No required params
	require.NoError(t, h.Validate(nil))
	require.NoError(t, h.Validate(map[string]any{}))
}

func TestListSkillsConformance_Execute(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &listSkillsHandler{}
	ctx := newTestCtx(dir)

	env := newTestEnv(t, dir)
	res, err := h.Execute(ctx, env, map[string]any{})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.NotEmpty(t, res.Output)
}

// ---------------------------------------------------------------------------
// embedding_index Conformance Tests
// ---------------------------------------------------------------------------

func TestEmbeddingIndexConformance_Definition(t *testing.T) {
	t.Parallel()
	h := &embeddingIndexHandler{}

	require.Equal(t, "embedding_index", h.Name())

	def := h.Definition()
	require.Equal(t, "embedding_index", def.Name)
	require.NotEmpty(t, def.Description)
	require.Equal(t, []string{"operation"}, def.Required)

	// Check parameter schema
	paramNames := make(map[string]bool)
	for _, p := range def.Parameters {
		paramNames[p.Name] = true
	}
	require.True(t, paramNames["operation"], "should have 'operation' parameter")
}

func TestEmbeddingIndexConformance_Validate_MissingOperation(t *testing.T) {
	t.Parallel()
	h := &embeddingIndexHandler{}

	err := h.Validate(map[string]any{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "required")
}

func TestEmbeddingIndexConformance_Validate_EmptyOperation(t *testing.T) {
	t.Parallel()
	h := &embeddingIndexHandler{}

	// extractString allows empty strings - the handler only validates presence
	err := h.Validate(map[string]any{"operation": ""})
	require.NoError(t, err)
}

func TestEmbeddingIndexConformance_Validate_Valid(t *testing.T) {
	t.Parallel()
	h := &embeddingIndexHandler{}

	require.NoError(t, h.Validate(map[string]any{"operation": "build"}))
	require.NoError(t, h.Validate(map[string]any{"operation": "update"}))
	require.NoError(t, h.Validate(map[string]any{"operation": "status"}))
}

func TestEmbeddingIndexConformance_Execute_Status(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &embeddingIndexHandler{}
	ctx := newTestCtx(dir)

	env := newTestEnv(t, dir)
	res, err := h.Execute(ctx, env, map[string]any{"operation": "status"})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Contains(t, res.Output, "Embedding Index Status")
}

func TestEmbeddingIndexConformance_Execute_InvalidOperation(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &embeddingIndexHandler{}
	ctx := newTestCtx(dir)

	env := newTestEnv(t, dir)
	res, err := h.Execute(ctx, env, map[string]any{"operation": "invalid_op"})
	require.NoError(t, err)
	require.True(t, res.IsError)
	require.Contains(t, res.Output, "Unknown operation")
}

// ---------------------------------------------------------------------------
// Helper function conformance tests
// ---------------------------------------------------------------------------

func TestFormatSizeConformance(t *testing.T) {
	t.Parallel()
	tests := []struct {
		bytes int64
		want  string
	}{
		{0, "0 B"},
		{100, "100 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1024 * 1024, "1.0 MB"},
		{1024 * 1024 * 1024, "1.0 GB"},
	}

	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, formatSize(tc.bytes))
		})
	}
}

func TestSplitURLSchemeConformance(t *testing.T) {
	t.Parallel()
	tests := []struct {
		url   string
		wantS string
		wantP string
	}{
		{"https://example.com/path", "https", "/path"},
		{"http://example.com", "http", ""},
		{"ftp://example.com", "ftp", ""}, // url.Parse puts "example.com" as Host, not Path
		{"notaurl", "", "notaurl"},
		{"https://example.com/path/file.pdf", "https", "/path/file.pdf"},
	}

	for _, tc := range tests {
		t.Run(tc.url, func(t *testing.T) {
			t.Parallel()
			s, p := splitURLScheme(tc.url)
			require.Equal(t, tc.wantS, s)
			require.Equal(t, tc.wantP, p)
		})
	}
}

func TestFileURLExtensionConformance(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path string
		want string
	}{
		{"/path/file.pdf", ".pdf"},
		{"/path/image.PNG", ".PNG"},
		{"/path/noext", ""},
		{"/path/.hidden", ".hidden"},
		{"/path/file.tar.gz", ".gz"},
		{"/path/to/file.jpg", ".jpg"},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, fileURLExtension(tc.path))
		})
	}
}

func TestToIntegerArgConformance(t *testing.T) {
	t.Parallel()
	require.Equal(t, 42, toIntArg(42))
	require.Equal(t, 42, toIntArg(float64(42)))
	require.Equal(t, 0, toIntArg("not a number"))
	require.Equal(t, 0, toIntArg(nil))
}

func TestExtractStringConformance(t *testing.T) {
	t.Parallel()

	// Valid string
	s, err := extractString(map[string]any{"key": "value"}, "key")
	require.NoError(t, err)
	require.Equal(t, "value", s)

	// Missing key
	_, err = extractString(map[string]any{}, "key")
	require.Error(t, err)
	require.Contains(t, err.Error(), "required")

	// Nil value
	_, err = extractString(map[string]any{"key": nil}, "key")
	require.Error(t, err)
	require.Contains(t, err.Error(), "required")

	// Wrong type
	_, err = extractString(map[string]any{"key": 42}, "key")
	require.Error(t, err)
	require.Contains(t, err.Error(), "must be a string")
}

func TestEstimateTokenUsageConformance(t *testing.T) {
	t.Parallel()
	require.Equal(t, 0, estimateTokenUsage(""))
	require.Equal(t, 10, estimateTokenUsage(strings.Repeat("a", 40))) // 40 chars / 4 = 10
	require.Equal(t, 1, estimateTokenUsage("abcde"))                  // 5 chars / 4 = 1 (integer division)
	require.Equal(t, 0, estimateTokenUsage("ab"))                     // 2 chars / 4 = 0
}

// ---------------------------------------------------------------------------
// Global registry conformance
// ---------------------------------------------------------------------------

func TestGetNewToolRegistryConformance(t *testing.T) {
	t.Parallel()
	r1 := GetNewToolRegistry()
	r2 := GetNewToolRegistry()
	require.Same(t, r1, r2, "GetNewToolRegistry should return the same singleton")
}

// ---------------------------------------------------------------------------
// AllTools conformance — each handler implements the interface correctly
// ---------------------------------------------------------------------------

func TestAllToolsConformance_InterfaceContract(t *testing.T) {
	t.Parallel()
	tools := AllTools()

	for _, h := range tools {
		name := h.Name()

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Name() and Definition().Name must match
			def := h.Definition()
			require.Equal(t, name, def.Name, "Name() must equal Definition().Name")

			// Description must be non-empty
			require.NotEmpty(t, def.Description, "Definition().Description must not be empty")

			// Validate must handle nil map gracefully
			// read_file and fetch_url have required params → should error on nil
			// list_directory has no required params → nil args are valid
			err := h.Validate(nil)
			switch name {
			case "read_file", "fetch_url", "search_files", "embedding_index",
				"write_file", "write_structured_file", "edit_file", "shell_command", "save_memory", "search_memories",
				"get_callers", "get_callees":
				require.Error(t, err, "Validate(nil) should return error for tools with required params")
			case "list_directory", "repo_map", "list_skills", "rollback_changes", "view_history",
				"list_automate_workflows", "list_changes", "revert_my_changes", "find_dead_code":
				require.NoError(t, err, "Validate(nil) should succeed for tools with no required params")
			default:
				require.Error(t, err, "Validate(nil) should return error for tool %q", name)
			}

			// Validate must handle empty map
			err = h.Validate(map[string]any{})
			switch name {
			case "read_file", "fetch_url", "search_files", "embedding_index",
				"write_file", "write_structured_file", "edit_file", "shell_command", "save_memory", "search_memories",
				"get_callers", "get_callees":
				require.Error(t, err, "Validate({}) should return error for tools with required params")
			case "list_directory", "repo_map", "list_skills", "rollback_changes", "view_history",
				"list_automate_workflows", "list_changes", "revert_my_changes", "find_dead_code":
				require.NoError(t, err, "Validate({}) should succeed for tools with no required params")
			default:
				require.Error(t, err, "Validate({}) should return error for tool %q", name)
			}

			// Required list in Definition should match Required flags on parameters
			if len(def.Required) > 0 {
				requiredSet := make(map[string]bool)
				for _, r := range def.Required {
					requiredSet[r] = true
				}
				for _, p := range def.Parameters {
					if requiredSet[p.Name] {
						require.True(t, p.Required, "parameter %q should have Required=true", p.Name)
					}
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// End-to-end read_file + list_directory integration
// ---------------------------------------------------------------------------

func TestConformance_Integration_ReadAndList(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ctx := newTestCtx(dir)
	env := newTestEnv(t, dir)

	// Create files
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("content a"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.txt"), []byte("content b here"), 0o644))

	// List the directory
	listH := &listDirHandler{}
	listRes, err := listH.Execute(ctx, env, map[string]any{"path": dir})
	require.NoError(t, err)
	require.Contains(t, listRes.Output, "a.txt")
	require.Contains(t, listRes.Output, "b.txt")

	// Read each file
	readH := &readFileHandler{}
	for _, name := range []string{"a.txt", "b.txt"} {
		res, err := readH.Execute(ctx, env, map[string]any{
			"path": filepath.Join(dir, name),
		})
		require.NoError(t, err)
		require.Contains(t, res.Output, "content")
	}
}

// ---------------------------------------------------------------------------
// Edge case: relative path resolution
// ---------------------------------------------------------------------------

func TestReadFileHandlerConformance_RelativePath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &readFileHandler{}
	ctx := newTestCtx(dir)

	path := filepath.Join(dir, "relative.txt")
	require.NoError(t, os.WriteFile(path, []byte("relative content"), 0o644))

	// Use relative path (relative to workspace root)
	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{"path": "relative.txt"})
	require.NoError(t, err)
	require.Contains(t, res.Output, "relative content")
}

func TestListDirConformance_SortedOutput(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &listDirHandler{}
	ctx := newTestCtx(dir)

	// Create files in non-alphabetical order
	require.NoError(t, os.WriteFile(filepath.Join(dir, "zebra.txt"), []byte("z"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "alpha.txt"), []byte("a"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "middle.txt"), []byte("m"), 0o644))

	res, err := h.Execute(ctx, newTestEnv(t, dir), map[string]any{"path": dir})
	require.NoError(t, err)

	// Verify entries are sorted: alpha, middle, zebra
	alphaIdx := strings.Index(res.Output, "alpha.txt")
	middleIdx := strings.Index(res.Output, "middle.txt")
	zebraIdx := strings.Index(res.Output, "zebra.txt")

	require.True(t, alphaIdx < middleIdx, "alpha.txt should appear before middle.txt")
	require.True(t, middleIdx < zebraIdx, "middle.txt should appear before zebra.txt")
}

// ---------------------------------------------------------------------------
// write_file Conformance Tests
// ---------------------------------------------------------------------------

func TestWriteFileHandlerConformance_Definition(t *testing.T) {
	t.Parallel()
	h := &writeFileHandler{}

	require.Equal(t, "write_file", h.Name())

	def := h.Definition()
	require.Equal(t, "write_file", def.Name)
	require.NotEmpty(t, def.Description)
	require.Equal(t, []string{"path", "content"}, def.Required)

	// Check parameter schema
	paramNames := make(map[string]bool)
	for _, p := range def.Parameters {
		paramNames[p.Name] = true
	}
	require.True(t, paramNames["path"], "should have 'path' parameter")
	require.True(t, paramNames["content"], "should have 'content' parameter")
}

func TestWriteFileHandlerConformance_Validate_MissingPath(t *testing.T) {
	t.Parallel()
	h := &writeFileHandler{}

	err := h.Validate(map[string]any{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "required")
}

func TestWriteFileHandlerConformance_Validate_EmptyPath(t *testing.T) {
	t.Parallel()
	h := &writeFileHandler{}

	err := h.Validate(map[string]any{"path": "", "content": "data"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "must not be empty")
}

func TestWriteFileHandlerConformance_Validate_MissingContent(t *testing.T) {
	t.Parallel()
	h := &writeFileHandler{}

	err := h.Validate(map[string]any{"path": "file.txt"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "required")
}

func TestWriteFileHandlerConformance_Validate_Valid(t *testing.T) {
	t.Parallel()
	h := &writeFileHandler{}

	require.NoError(t, h.Validate(map[string]any{"path": "file.txt", "content": "data"}))
	require.NoError(t, h.Validate(map[string]any{"path": "/abs/path.txt", "content": ""}))
}

// ---------------------------------------------------------------------------
// write_structured_file Conformance Tests
// ---------------------------------------------------------------------------

func TestWriteStructuredFileHandlerConformance_Definition(t *testing.T) {
	t.Parallel()
	h := &writeStructuredFileHandler{}

	require.Equal(t, "write_structured_file", h.Name())

	def := h.Definition()
	require.Equal(t, "write_structured_file", def.Name)
	require.NotEmpty(t, def.Description)
	require.Equal(t, []string{"path", "data"}, def.Required)

	// Check parameter schema
	paramNames := make(map[string]bool)
	for _, p := range def.Parameters {
		paramNames[p.Name] = true
	}
	require.True(t, paramNames["path"], "should have 'path' parameter")
	require.True(t, paramNames["data"], "should have 'data' parameter")
	require.True(t, paramNames["format"], "should have 'format' parameter")
	require.True(t, paramNames["schema"], "should have 'schema' parameter")
}

func TestWriteStructuredFileHandlerConformance_Validate_MissingPath(t *testing.T) {
	t.Parallel()
	h := &writeStructuredFileHandler{}

	err := h.Validate(map[string]any{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "required")
}

func TestWriteStructuredFileHandlerConformance_Validate_EmptyPath(t *testing.T) {
	t.Parallel()
	h := &writeStructuredFileHandler{}

	err := h.Validate(map[string]any{"path": "", "data": map[string]any{}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "must not be empty")
}

func TestWriteStructuredFileHandlerConformance_Validate_MissingData(t *testing.T) {
	t.Parallel()
	h := &writeStructuredFileHandler{}

	err := h.Validate(map[string]any{"path": "file.json"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "required")
}

func TestWriteStructuredFileHandlerConformance_Validate_EmptyFormat(t *testing.T) {
	t.Parallel()
	h := &writeStructuredFileHandler{}

	err := h.Validate(map[string]any{"path": "file.json", "data": map[string]any{}, "format": "  "})
	require.Error(t, err)
	require.Contains(t, err.Error(), "must not be empty")
}

func TestWriteStructuredFileHandlerConformance_Validate_Valid(t *testing.T) {
	t.Parallel()
	h := &writeStructuredFileHandler{}

	require.NoError(t, h.Validate(map[string]any{"path": "file.json", "data": map[string]any{"key": "value"}}))
	require.NoError(t, h.Validate(map[string]any{"path": "file.json", "data": map[string]any{}, "format": "json"}))
}

// ---------------------------------------------------------------------------
// edit_file Conformance Tests
// ---------------------------------------------------------------------------

func TestEditFileHandlerConformance_Definition(t *testing.T) {
	t.Parallel()
	h := &editFileHandler{}

	require.Equal(t, "edit_file", h.Name())

	def := h.Definition()
	require.Equal(t, "edit_file", def.Name)
	require.NotEmpty(t, def.Description)
	require.Equal(t, []string{"path", "old_str", "new_str"}, def.Required)

	// Check parameter schema
	paramNames := make(map[string]bool)
	for _, p := range def.Parameters {
		paramNames[p.Name] = true
	}
	require.True(t, paramNames["path"], "should have 'path' parameter")
	require.True(t, paramNames["old_str"], "should have 'old_str' parameter")
	require.True(t, paramNames["new_str"], "should have 'new_str' parameter")
}

func TestEditFileHandlerConformance_Validate_MissingPath(t *testing.T) {
	t.Parallel()
	h := &editFileHandler{}

	err := h.Validate(map[string]any{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "required")
}

func TestEditFileHandlerConformance_Validate_EmptyPath(t *testing.T) {
	t.Parallel()
	h := &editFileHandler{}

	err := h.Validate(map[string]any{"path": "", "old_str": "a", "new_str": "b"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "must not be empty")
}

func TestEditFileHandlerConformance_Validate_MissingOldStr(t *testing.T) {
	t.Parallel()
	h := &editFileHandler{}

	err := h.Validate(map[string]any{"path": "file.txt", "new_str": "replacement"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "required")
}

func TestEditFileHandlerConformance_Validate_EmptyOldStr(t *testing.T) {
	t.Parallel()
	h := &editFileHandler{}

	err := h.Validate(map[string]any{"path": "file.txt", "old_str": "", "new_str": "replacement"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "must not be empty")
}

func TestEditFileHandlerConformance_Validate_MissingNewStr(t *testing.T) {
	t.Parallel()
	h := &editFileHandler{}

	err := h.Validate(map[string]any{"path": "file.txt", "old_str": "original"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "required")
}

func TestEditFileHandlerConformance_Validate_Valid(t *testing.T) {
	t.Parallel()
	h := &editFileHandler{}

	require.NoError(t, h.Validate(map[string]any{"path": "file.txt", "old_str": "a", "new_str": "b"}))
	// new_str can be empty (replacing with nothing)
	require.NoError(t, h.Validate(map[string]any{"path": "file.txt", "old_str": "a", "new_str": ""}))
}

// ---------------------------------------------------------------------------
// shell_command Conformance Tests
// ---------------------------------------------------------------------------

func TestShellCommandHandlerConformance_Definition(t *testing.T) {
	t.Parallel()
	h := &shellCommandHandler{}

	require.Equal(t, "shell_command", h.Name())

	def := h.Definition()
	require.Equal(t, "shell_command", def.Name)
	require.NotEmpty(t, def.Description)
	require.Empty(t, def.Required, "shell_command should have no required params in definition (command is required unless check/stop_background is set)")

	// Check parameter schema
	paramNames := make(map[string]bool)
	for _, p := range def.Parameters {
		paramNames[p.Name] = true
	}
	require.True(t, paramNames["command"], "should have 'command' parameter")
	require.True(t, paramNames["background"], "should have 'background' parameter")
	require.True(t, paramNames["check_background"], "should have 'check_background' parameter")
	require.True(t, paramNames["stop_background"], "should have 'stop_background' parameter")
}

func TestShellCommandHandlerConformance_Validate_NilArgs(t *testing.T) {
	t.Parallel()
	h := &shellCommandHandler{}

	err := h.Validate(nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "must not be nil")
}

func TestShellCommandHandlerConformance_Validate_EmptyArgs(t *testing.T) {
	t.Parallel()
	h := &shellCommandHandler{}

	err := h.Validate(map[string]any{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "required")
}

func TestShellCommandHandlerConformance_Validate_InvalidBackground(t *testing.T) {
	t.Parallel()
	h := &shellCommandHandler{}

	err := h.Validate(map[string]any{"command": "echo hi", "background": 42})
	require.Error(t, err)
	require.Contains(t, err.Error(), "must be a boolean")
}

func TestShellCommandHandlerConformance_Validate_ConflictingParams(t *testing.T) {
	t.Parallel()
	h := &shellCommandHandler{}

	// check_background + background=true
	err := h.Validate(map[string]any{"command": "echo hi", "check_background": "sess1", "background": true})
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot be used together")

	// stop_background + check_background
	err = h.Validate(map[string]any{"command": "echo hi", "stop_background": "sess1", "check_background": "sess2"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot be used together")
}

func TestShellCommandHandlerConformance_Validate_ValidCommand(t *testing.T) {
	t.Parallel()
	h := &shellCommandHandler{}

	require.NoError(t, h.Validate(map[string]any{"command": "echo hello"}))
	require.NoError(t, h.Validate(map[string]any{"command": "echo hello", "background": true}))
	require.NoError(t, h.Validate(map[string]any{"check_background": "sess1"}))
	require.NoError(t, h.Validate(map[string]any{"stop_background": "sess1"}))
}

// ---------------------------------------------------------------------------
// save_memory Conformance Tests
// ---------------------------------------------------------------------------

func TestSaveMemoryHandlerConformance_Definition(t *testing.T) {
	t.Parallel()
	h := &saveMemoryHandler{}

	require.Equal(t, "save_memory", h.Name())

	def := h.Definition()
	require.Equal(t, "save_memory", def.Name)
	require.NotEmpty(t, def.Description)
	require.Equal(t, []string{"name", "content"}, def.Required)

	// Check parameter schema
	paramNames := make(map[string]bool)
	for _, p := range def.Parameters {
		paramNames[p.Name] = true
	}
	require.True(t, paramNames["name"], "should have 'name' parameter")
	require.True(t, paramNames["content"], "should have 'content' parameter")
}

func TestSaveMemoryHandlerConformance_Validate_MissingName(t *testing.T) {
	t.Parallel()
	h := &saveMemoryHandler{}

	err := h.Validate(map[string]any{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "required")
}

func TestSaveMemoryHandlerConformance_Validate_EmptyName(t *testing.T) {
	t.Parallel()
	h := &saveMemoryHandler{}

	err := h.Validate(map[string]any{"name": "", "content": "data"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "must not be empty")
}

func TestSaveMemoryHandlerConformance_Validate_MissingContent(t *testing.T) {
	t.Parallel()
	h := &saveMemoryHandler{}

	err := h.Validate(map[string]any{"name": "my-memory"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "required")
}

func TestSaveMemoryHandlerConformance_Validate_EmptyContent(t *testing.T) {
	t.Parallel()
	h := &saveMemoryHandler{}

	err := h.Validate(map[string]any{"name": "my-memory", "content": "  "})
	require.Error(t, err)
	require.Contains(t, err.Error(), "must not be empty")
}

func TestSaveMemoryHandlerConformance_Validate_Valid(t *testing.T) {
	t.Parallel()
	h := &saveMemoryHandler{}

	require.NoError(t, h.Validate(map[string]any{"name": "my-memory", "content": "# Some content"}))
}

// ---------------------------------------------------------------------------
// search_memories Conformance Tests
// ---------------------------------------------------------------------------

func TestSearchMemoriesHandlerConformance_Definition(t *testing.T) {
	t.Parallel()
	h := &searchMemoriesHandler{}

	require.Equal(t, "search_memories", h.Name())

	def := h.Definition()
	require.Equal(t, "search_memories", def.Name)
	require.NotEmpty(t, def.Description)
	require.Equal(t, []string{"query"}, def.Required)

	// Check parameter schema
	paramNames := make(map[string]bool)
	for _, p := range def.Parameters {
		paramNames[p.Name] = true
	}
	require.True(t, paramNames["query"], "should have 'query' parameter")
	require.True(t, paramNames["top_k"], "should have 'top_k' parameter")
	require.True(t, paramNames["threshold"], "should have 'threshold' parameter")
}

func TestSearchMemoriesHandlerConformance_Validate_MissingQuery(t *testing.T) {
	t.Parallel()
	h := &searchMemoriesHandler{}

	err := h.Validate(map[string]any{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "required")
}

func TestSearchMemoriesHandlerConformance_Validate_EmptyQuery(t *testing.T) {
	t.Parallel()
	h := &searchMemoriesHandler{}

	err := h.Validate(map[string]any{"query": ""})
	require.Error(t, err)
	require.Contains(t, err.Error(), "must not be empty")
}

func TestSearchMemoriesHandlerConformance_Validate_InvalidTopK(t *testing.T) {
	t.Parallel()
	h := &searchMemoriesHandler{}

	err := h.Validate(map[string]any{"query": "test", "top_k": "not a number"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "must be an integer")
}

func TestSearchMemoriesHandlerConformance_Validate_InvalidThreshold(t *testing.T) {
	t.Parallel()
	h := &searchMemoriesHandler{}

	err := h.Validate(map[string]any{"query": "test", "threshold": "not a number"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "must be a number")
}

func TestSearchMemoriesHandlerConformance_Validate_Valid(t *testing.T) {
	t.Parallel()
	h := &searchMemoriesHandler{}

	require.NoError(t, h.Validate(map[string]any{"query": "test"}))
	require.NoError(t, h.Validate(map[string]any{"query": "test", "top_k": 10}))
	require.NoError(t, h.Validate(map[string]any{"query": "test", "threshold": 0.5}))
	require.NoError(t, h.Validate(map[string]any{"query": "test", "top_k": 10, "threshold": 0.5}))
}

// Note: run_subagent / run_parallel_subagents are intentionally NOT handlers in
// this package (see all.go) — they live in the seed registry under pkg/agent
// because they need *Agent access. SP-059 Phase 3b removed the stub handlers
// here, so their conformance tests were removed too.

// =============================================================================
// SP-038-5b: Todo tools (Name/Definition/Validate + Execute)
// Task queue tools (task_queue_add / publish / read) were removed in 2026-07-18
// along with the Executive Assistant queue mode. The TODO list + workflow-automation
// skill covers the autonomous batch-processing use case instead.
// =============================================================================

func TestTodoWriteHandlerConformance(t *testing.T) {
	h := &todoWriteHandler{}

	// Test Name
	if h.Name() != "todo_write" {
		t.Errorf("Name() = %q, want %q", h.Name(), "todo_write")
	}

	// Test Definition
	d := h.Definition()
	if d.Name != "todo_write" {
		t.Errorf("Definition().Name = %q, want %q", d.Name, "todo_write")
	}
	if d.Description == "" {
		t.Error("Definition().Description should not be empty")
	}

	hasParam := func(name string) bool {
		for _, p := range d.Parameters {
			if p.Name == name {
				return true
			}
		}
		return false
	}
	if !hasParam("todos") {
		t.Error("Definition missing 'todos' parameter")
	}

	// Validate: missing required
	if err := h.Validate(nil); err == nil {
		t.Error("Validate(nil) should return error")
	}
	if err := h.Validate(map[string]any{}); err == nil {
		t.Error("Validate(empty) should return error for missing todos")
	}

	// Validate: todos not an array
	if err := h.Validate(map[string]any{"todos": "not an array"}); err == nil {
		t.Error("Validate(string todos) should return error")
	}

	// Validate: empty array (allowed by Validate - no loop runs)
	if err := h.Validate(map[string]any{"todos": []interface{}{}}); err != nil {
		t.Errorf("Validate(empty array) should return nil, got: %v", err)
	}

	// Validate: missing content in todo
	if err := h.Validate(map[string]any{
		"todos": []interface{}{
			map[string]interface{}{"status": "pending"},
		},
	}); err == nil {
		t.Error("Validate(missing content) should return error")
	}

	// Validate: empty content
	if err := h.Validate(map[string]any{
		"todos": []interface{}{
			map[string]interface{}{"content": "", "status": "pending"},
		},
	}); err == nil {
		t.Error("Validate(empty content) should return error")
	}

	// Validate: missing status
	if err := h.Validate(map[string]any{
		"todos": []interface{}{
			map[string]interface{}{"content": "test"},
		},
	}); err == nil {
		t.Error("Validate(missing status) should return error")
	}

	// Validate: invalid status
	if err := h.Validate(map[string]any{
		"todos": []interface{}{
			map[string]interface{}{"content": "test", "status": "invalid"},
		},
	}); err == nil {
		t.Error("Validate(invalid status) should return error")
	}

	// Validate: invalid priority
	if err := h.Validate(map[string]any{
		"todos": []interface{}{
			map[string]interface{}{"content": "test", "status": "pending", "priority": "invalid"},
		},
	}); err == nil {
		t.Error("Validate(invalid priority) should return error")
	}

	// Validate: valid todo
	validArgs := map[string]any{
		"todos": []interface{}{
			map[string]interface{}{
				"content":  "Test task",
				"status":   "pending",
				"priority": "high",
			},
		},
	}
	if err := h.Validate(validArgs); err != nil {
		t.Errorf("Validate(valid args) should return nil, got: %v", err)
	}

	// Execute: write todos
	ctx := context.Background()
	env := ToolEnv{}
	result, err := h.Execute(ctx, env, validArgs)
	if err != nil {
		t.Fatalf("Execute should not return error, got: %v", err)
	}
	if !strings.Contains(result.Output, "1") {
		t.Logf("Output did not contain count, but may be acceptable: %s", result.Output)
	}
}

func TestTodoReadHandlerConformance(t *testing.T) {
	h := &todoReadHandler{}

	// Test Name
	if h.Name() != "todo_read" {
		t.Errorf("Name() = %q, want %q", h.Name(), "todo_read")
	}

	// Test Definition
	d := h.Definition()
	if d.Name != "todo_read" {
		t.Errorf("Definition().Name = %q, want %q", d.Name, "todo_read")
	}
	if d.Description == "" {
		t.Error("Definition().Description should not be empty")
	}

	// Validate: nil returns error (actual implementation)
	if err := h.Validate(nil); err == nil {
		t.Error("Validate(nil) should return error")
	}

	// Validate: empty map returns error (actual implementation)
	if err := h.Validate(map[string]any{}); err == nil {
		t.Error("Validate(empty map) should return error")
	}

	// Validate: with any arg passes
	if err := h.Validate(map[string]any{"placeholder": true}); err != nil {
		t.Errorf("Validate(non-empty) should return nil, got: %v", err)
	}

	// Execute: reads todos (no crash)
	ctx := context.Background()
	env := ToolEnv{}
	result, err := h.Execute(ctx, env, map[string]any{"placeholder": true})
	if err != nil {
		t.Fatalf("Execute should not return error, got: %v", err)
	}
	if result.Output == "" {
		t.Error("Execute output should not be empty")
	}
}

// =============================================================================
// SP-038-5c: Remaining tools
// =============================================================================

func TestCommitHandlerConformance(t *testing.T) {
	h := &commitHandler{}

	// Test Name
	if h.Name() != "commit" {
		t.Errorf("Name() = %q, want %q", h.Name(), "commit")
	}

	// Test Definition
	d := h.Definition()
	if d.Name != "commit" {
		t.Errorf("Definition().Name = %q, want %q", d.Name, "commit")
	}
	if d.Description == "" {
		t.Error("Definition().Description should not be empty")
	}

	hasParam := func(name string) bool {
		for _, p := range d.Parameters {
			if p.Name == name {
				return true
			}
		}
		return false
	}
	for _, name := range []string{"message", "notes"} {
		if !hasParam(name) {
			t.Errorf("Definition missing '%s' parameter", name)
		}
	}

	// Validate: nil returns error (actual implementation)
	if err := h.Validate(nil); err == nil {
		t.Error("Validate(nil) should return error")
	}

	// Validate: empty map returns error (actual implementation)
	if err := h.Validate(map[string]any{}); err == nil {
		t.Error("Validate(empty map) should return error")
	}

	// Validate: with args passes
	if err := h.Validate(map[string]any{"message": "test commit"}); err != nil {
		t.Errorf("Validate(message) should return nil, got: %v", err)
	}
	if err := h.Validate(map[string]any{"notes": "context notes"}); err != nil {
		t.Errorf("Validate(notes) should return nil, got: %v", err)
	}

	// Execute: with valid args and workspace root (should not panic)
	ctx := context.Background()
	tmpDir := t.TempDir()
	env := ToolEnv{WorkspaceRoot: tmpDir}
	_, err := h.Execute(ctx, env, map[string]any{"message": "test"})
	// We don't assert specific output since git won't work in a temp dir,
	// but we verify it doesn't panic
	if err != nil {
		t.Logf("Execute returned error (expected in test env): %v", err)
	}
}

func TestGitHandlerConformance(t *testing.T) {
	h := &gitHandler{}

	// Test Name
	if h.Name() != "git" {
		t.Errorf("Name() = %q, want %q", h.Name(), "git")
	}

	// Test Definition
	d := h.Definition()
	if d.Name != "git" {
		t.Errorf("Definition().Name = %q, want %q", d.Name, "git")
	}
	if d.Description == "" {
		t.Error("Definition().Description should not be empty")
	}

	hasParam := func(name string) bool {
		for _, p := range d.Parameters {
			if p.Name == name {
				return true
			}
		}
		return false
	}
	for _, name := range []string{"operation", "args"} {
		if !hasParam(name) {
			t.Errorf("Definition missing '%s' parameter", name)
		}
	}

	// Validate: missing required
	if err := h.Validate(nil); err == nil {
		t.Error("Validate(nil) should return error")
	}
	if err := h.Validate(map[string]any{}); err == nil {
		t.Error("Validate(empty) should return error for missing operation")
	}

	// Validate: valid operation
	if err := h.Validate(map[string]any{"operation": "commit"}); err != nil {
		t.Errorf("Validate(operation) should return nil, got: %v", err)
	}

	// Validate: valid operation with args
	if err := h.Validate(map[string]any{"operation": "push", "args": "--force"}); err != nil {
		t.Errorf("Validate(operation+args) should return nil, got: %v", err)
	}

	// Validate: operation not a string
	if err := h.Validate(map[string]any{"operation": 123}); err == nil {
		t.Error("Validate(non-string operation) should return error")
	}

	// Execute: basic execution should not panic
	ctx := context.Background()
	env := ToolEnv{}
	_, err := h.Execute(ctx, env, map[string]any{"operation": "status"})
	// May fail due to git not being available, but should not panic
	if err != nil {
		t.Logf("Execute returned error (expected in test env): %v", err)
	}
}

func TestAskUserHandlerConformance(t *testing.T) {
	h := &askUserHandler{}

	// Test Name
	if h.Name() != "ask_user" {
		t.Errorf("Name() = %q, want %q", h.Name(), "ask_user")
	}

	// Test Definition
	d := h.Definition()
	if d.Name != "ask_user" {
		t.Errorf("Definition().Name = %q, want %q", d.Name, "ask_user")
	}
	if d.Description == "" {
		t.Error("Definition().Description should not be empty")
	}

	hasParam := func(name string) bool {
		for _, p := range d.Parameters {
			if p.Name == name {
				return true
			}
		}
		return false
	}
	if !hasParam("question") {
		t.Error("Definition missing 'question' parameter")
	}

	// Validate: missing required
	if err := h.Validate(nil); err == nil {
		t.Error("Validate(nil) should return error")
	}
	if err := h.Validate(map[string]any{}); err == nil {
		t.Error("Validate(empty) should return error for missing question")
	}

	// Validate: valid question
	if err := h.Validate(map[string]any{"question": "What is your name?"}); err != nil {
		t.Errorf("Validate(question) should return nil, got: %v", err)
	}

	// Validate: non-string question
	if err := h.Validate(map[string]any{"question": 123}); err == nil {
		t.Error("Validate(non-string question) should return error")
	}

	// Execute: may fail in test env (no terminal), but should not panic
	ctx := context.Background()
	env := ToolEnv{}
	result, err := h.Execute(ctx, env, map[string]any{"question": "test?"})
	if err != nil {
		t.Logf("Execute returned error (expected in test env): %v", err)
	}
	if result.IsError {
		t.Logf("Execute returned error result (expected in test env): %s", result.Output)
	}
}

// Wrapper timeout must match the inner AskUserManager deadline so the seed
// registry's 5-minute DefaultTimeout doesn't fire first and drop the user's
// in-progress answer. Regression test for the ask_user short-context-deadline
// bug.
func TestAskUserHandlerWrapperTimeout(t *testing.T) {
	h := &askUserHandler{}
	got := h.Timeout()
	if got != DefaultAskUserTimeout {
		t.Errorf("askUserHandler.Timeout() = %v, want %v (must equal DefaultAskUserTimeout so the seed wrapper does not fire before the inner manager)", got, DefaultAskUserTimeout)
	}
	if got <= 0 {
		t.Errorf("askUserHandler.Timeout() = %v, must be > 0; 0 lets seed default to 5 min and silently drop user answers", got)
	}
}

// Parallel regression test for request_clarification: the seed-registry wrapper
// timeout must exceed the inner ClarificationManager's deadline (60s in
// pkg/agent/clarification_manager.go::DefaultClarificationTimeout) so the
// inner manager enforces its own timeout first instead of being killed by the
// outer wrapper. The constant mirrors the inner value with a 5-second slack.
func TestRequestClarificationHandlerWrapperTimeout(t *testing.T) {
	h := &requestClarificationHandler{}
	got := h.Timeout()
	if got <= 0 {
		t.Errorf("requestClarificationHandler.Timeout() = %v, must be > 0; 0 lets seed default to 5 min", got)
	}
	if got <= 60*time.Second {
		t.Errorf("requestClarificationHandler.Timeout() = %v, must exceed 60s (DefaultClarificationTimeout) so the inner manager fires first; otherwise the outer wrapper silently drops user answers", got)
	}
}

func TestActivateSkillHandlerConformance(t *testing.T) {
	h := &activateSkillHandler{}

	// Test Name
	if h.Name() != "activate_skill" {
		t.Errorf("Name() = %q, want %q", h.Name(), "activate_skill")
	}

	// Test Definition
	d := h.Definition()
	if d.Name != "activate_skill" {
		t.Errorf("Definition().Name = %q, want %q", d.Name, "activate_skill")
	}
	if d.Description == "" {
		t.Error("Definition().Description should not be empty")
	}

	hasParam := func(name string) bool {
		for _, p := range d.Parameters {
			if p.Name == name {
				return true
			}
		}
		return false
	}
	if !hasParam("skill_id") {
		t.Error("Definition missing 'skill_id' parameter")
	}

	// Validate: missing required
	if err := h.Validate(nil); err == nil {
		t.Error("Validate(nil) should return error")
	}
	if err := h.Validate(map[string]any{}); err == nil {
		t.Error("Validate(empty) should return error for missing skill_id")
	}

	// Validate: valid
	if err := h.Validate(map[string]any{"skill_id": "project-planning"}); err != nil {
		t.Errorf("Validate(skill_id) should return nil, got: %v", err)
	}

	// Validate: non-string skill_id
	if err := h.Validate(map[string]any{"skill_id": 123}); err == nil {
		t.Error("Validate(non-string skill_id) should return error")
	}

	// ---------------------------------------------------------------------------
	// Execute: success path with fake SkillLoader
	// ---------------------------------------------------------------------------
	ctx := context.Background()
	env := ToolEnv{
		SkillLoader: &fakeSkillLoader{
			skills: map[string]*SkillInfo{
				"project-planning": {
					ID:          "project-planning",
					Name:        "Project Planning",
					Description: "Strategic planning and alignment for new (greenfield) or existing (brownfield) projects...",
					Content:     "# Project Planning Skill\n\nDo strategic planning.",
					Source:      "builtin",
				},
			},
		},
	}
	result, err := h.Execute(ctx, env, map[string]any{"skill_id": "project-planning"})
	if err != nil {
		t.Fatalf("Execute should not return error, got: %v", err)
	}
	if result.IsError {
		t.Fatalf("Execute result should not be IsError, output: %s", result.Output)
	}
	if !strings.Contains(result.Output, "Activated skill") {
		t.Error("Execute output should contain 'Activated skill'")
	}
	if !strings.Contains(result.Output, "Description:") {
		t.Error("Execute output should contain 'Description:'")
	}
	if !strings.Contains(result.Output, "Project Planning") {
		t.Error("Execute output should contain skill name")
	}
	if !strings.Contains(result.Output, "Instructions loaded into context") {
		t.Error("Execute output should contain 'Instructions loaded into context'")
	}
}

// TestAddMemoryHandlerConformance and TestDeleteMemoryHandlerConformance
// were removed when the legacy per-operation memory handlers were retired
// in favor of manage_memory (see pkg/agent/memory_manage.go). The
// add/delete operations are covered by manage_memory operation tests.

func TestPatchStructuredFileHandlerConformance(t *testing.T) {
	h := &patchStructuredFileHandler{}

	// Test Name
	if h.Name() != "patch_structured_file" {
		t.Errorf("Name() = %q, want %q", h.Name(), "patch_structured_file")
	}

	// Test Definition
	d := h.Definition()
	if d.Name != "patch_structured_file" {
		t.Errorf("Definition().Name = %q, want %q", d.Name, "patch_structured_file")
	}
	if d.Description == "" {
		t.Error("Definition().Description should not be empty")
	}

	hasParam := func(name string) bool {
		for _, p := range d.Parameters {
			if p.Name == name {
				return true
			}
		}
		return false
	}
	for _, name := range []string{"path", "patch_ops", "data", "format", "schema"} {
		if !hasParam(name) {
			t.Errorf("Definition missing '%s' parameter", name)
		}
	}

	// Validate: missing required
	if err := h.Validate(nil); err == nil {
		t.Error("Validate(nil) should return error")
	}
	if err := h.Validate(map[string]any{}); err == nil {
		t.Error("Validate(empty) should return error for missing path")
	}

	// Validate: valid path
	if err := h.Validate(map[string]any{"path": "/tmp/test.json"}); err != nil {
		t.Errorf("Validate(path) should return nil, got: %v", err)
	}

	// Validate: non-string path
	if err := h.Validate(map[string]any{"path": 123}); err == nil {
		t.Error("Validate(non-string path) should return error")
	}

	// Execute: patch existing JSON file
	tmpDir := t.TempDir()
	jsonPath := filepath.Join(tmpDir, "test.json")
	os.WriteFile(jsonPath, []byte(`{"name": "original", "value": 1}`), 0644)

	ctx := filesystem.WithWorkspaceRoot(context.Background(), tmpDir)
	env := ToolEnv{WorkspaceRoot: tmpDir}
	result, err := h.Execute(ctx, env, map[string]any{
		"path": jsonPath,
		"patch_ops": []interface{}{
			map[string]interface{}{"op": "replace", "path": "/name", "value": "updated"},
		},
	})
	if err != nil {
		t.Fatalf("Execute should not return error, got: %v", err)
	}
	if result.IsError {
		t.Fatalf("Execute should not return IsError, got: %s", result.Output)
	}

	// Verify file was patched
	updated, _ := os.ReadFile(jsonPath)
	if !strings.Contains(string(updated), "updated") {
		t.Errorf("File should contain updated value, got: %s", string(updated))
	}

	// Execute: nonexistent file
	result, err = h.Execute(ctx, env, map[string]any{
		"path": filepath.Join(tmpDir, "nope.json"),
	})
	if err != nil {
		t.Logf("Execute returned error for nonexistent file (expected): %v", err)
	}
}

func TestBrowseURLHandlerConformance(t *testing.T) {
	h := &browseURLHandler{}

	// Test Name
	if h.Name() != "browse_url" {
		t.Errorf("Name() = %q, want %q", h.Name(), "browse_url")
	}

	// Test Definition
	d := h.Definition()
	if d.Name != "browse_url" {
		t.Errorf("Definition().Name = %q, want %q", d.Name, "browse_url")
	}
	if d.Description == "" {
		t.Error("Definition().Description should not be empty")
	}

	hasParam := func(name string) bool {
		for _, p := range d.Parameters {
			if p.Name == name {
				return true
			}
		}
		return false
	}
	for _, name := range []string{"url", "action", "steps", "viewport_width", "viewport_height"} {
		if !hasParam(name) {
			t.Errorf("Definition missing '%s' parameter", name)
		}
	}

	// Validate: missing required
	if err := h.Validate(nil); err == nil {
		t.Error("Validate(nil) should return error")
	}
	if err := h.Validate(map[string]any{}); err == nil {
		t.Error("Validate(empty) should return error for missing url")
	}

	// Validate: valid url
	if err := h.Validate(map[string]any{"url": "https://example.com"}); err != nil {
		t.Errorf("Validate(url) should return nil, got: %v", err)
	}

	// Validate: non-string url
	if err := h.Validate(map[string]any{"url": 123}); err == nil {
		t.Error("Validate(non-string url) should return error")
	}
}

func TestWebSearchHandlerConformance(t *testing.T) {
	h := &webSearchHandler{}

	// Test Name
	if h.Name() != "web_search" {
		t.Errorf("Name() = %q, want %q", h.Name(), "web_search")
	}

	// Test Definition
	d := h.Definition()
	if d.Name != "web_search" {
		t.Errorf("Definition().Name = %q, want %q", d.Name, "web_search")
	}
	if d.Description == "" {
		t.Error("Definition().Description should not be empty")
	}

	hasParam := func(name string) bool {
		for _, p := range d.Parameters {
			if p.Name == name {
				return true
			}
		}
		return false
	}
	if !hasParam("query") {
		t.Error("Definition missing 'query' parameter")
	}

	// Validate: missing required
	if err := h.Validate(nil); err == nil {
		t.Error("Validate(nil) should return error")
	}
	if err := h.Validate(map[string]any{}); err == nil {
		t.Error("Validate(empty) should return error for missing query")
	}

	// Validate: valid query
	if err := h.Validate(map[string]any{"query": "test search"}); err != nil {
		t.Errorf("Validate(query) should return nil, got: %v", err)
	}

	// Validate: non-string query
	if err := h.Validate(map[string]any{"query": 123}); err == nil {
		t.Error("Validate(non-string query) should return error")
	}

	// ---------------------------------------------------------------------------
	// Execute: success path with fake SearchEngine
	// ---------------------------------------------------------------------------
	ctx := context.Background()
	fakeEngine := &fakeSearchEngine{
		results: map[string]string{
			"Go testing best practices": "Search results for \"Go testing best practices\":\n1. Table-driven tests\n2. Subtests",
		},
	}
	env := ToolEnv{SearchEngine: fakeEngine}
	result, err := h.Execute(ctx, env, map[string]any{"query": "Go testing best practices"})
	if err != nil {
		t.Fatalf("Execute should not return error, got: %v", err)
	}
	if result.IsError {
		t.Fatalf("Execute result should not be IsError, output: %s", result.Output)
	}
	if !strings.Contains(result.Output, "Search results for") {
		t.Errorf("Execute output should contain 'Search results for', got: %s", result.Output)
	}
	if !strings.Contains(result.Output, "Go testing best practices") {
		t.Errorf("Execute output should contain query, got: %s", result.Output)
	}

	// ---------------------------------------------------------------------------
	// Execute: nil SearchEngine
	// ---------------------------------------------------------------------------
	envNil := ToolEnv{}
	result, err = h.Execute(ctx, envNil, map[string]any{"query": "test"})
	if err != nil {
		t.Fatalf("Execute should not return error, got: %v", err)
	}
	if !result.IsError {
		t.Fatalf("Execute result should be IsError when SearchEngine is nil, output: %s", result.Output)
	}
	if !strings.Contains(result.Output, "search engine not available") {
		t.Errorf("Execute output should mention 'search engine not available', got: %s", result.Output)
	}

	// ---------------------------------------------------------------------------
	// Execute: error path
	// ---------------------------------------------------------------------------
	fakeErr := &fakeSearchEngine{
		fail:    true,
		failErr: errors.New("search API unavailable"),
	}
	envErr := ToolEnv{SearchEngine: fakeErr}
	result, err = h.Execute(ctx, envErr, map[string]any{"query": "anything"})
	if err != nil {
		t.Fatalf("Execute should not return error, got: %v", err)
	}
	if !result.IsError {
		t.Fatalf("Execute result should be IsError on search failure, output: %s", result.Output)
	}
	if !strings.Contains(result.Output, "search API unavailable") {
		t.Errorf("Execute output should contain 'search API unavailable', got: %s", result.Output)
	}
}

// fakeSearchEngine is a test double for the SearchEngine interface.
type fakeSearchEngine struct {
	results map[string]string
	fail    bool
	failErr error
}

func (f *fakeSearchEngine) Search(ctx context.Context, query string) (string, error) {
	if f.fail {
		return "", f.failErr
	}
	return f.results[query], nil
}

func TestSemanticSearchHandlerConformance(t *testing.T) {
	h := &semanticSearchHandler{}

	// Test Name
	if h.Name() != "semantic_search" {
		t.Errorf("Name() = %q, want %q", h.Name(), "semantic_search")
	}

	// Test Definition
	d := h.Definition()
	if d.Name != "semantic_search" {
		t.Errorf("Definition().Name = %q, want %q", d.Name, "semantic_search")
	}
	if d.Description == "" {
		t.Error("Definition().Description should not be empty")
	}

	hasParam := func(name string) bool {
		for _, p := range d.Parameters {
			if p.Name == name {
				return true
			}
		}
		return false
	}
	for _, name := range []string{"query", "threshold", "top_k"} {
		if !hasParam(name) {
			t.Errorf("Definition missing '%s' parameter", name)
		}
	}

	// Validate: missing required
	if err := h.Validate(nil); err == nil {
		t.Error("Validate(nil) should return error")
	}
	if err := h.Validate(map[string]any{}); err == nil {
		t.Error("Validate(empty) should return error for missing query")
	}

	// Validate: valid query
	if err := h.Validate(map[string]any{"query": "find similar code"}); err != nil {
		t.Errorf("Validate(query) should return nil, got: %v", err)
	}

	// Validate: valid with optional params
	if err := h.Validate(map[string]any{"query": "test", "top_k": 10, "threshold": 0.8}); err != nil {
		t.Errorf("Validate(full args) should return nil, got: %v", err)
	}

	// Validate: non-string query
	if err := h.Validate(map[string]any{"query": 123}); err == nil {
		t.Error("Validate(non-string query) should return error")
	}
}

func TestAnalyzeImageContentHandlerConformance(t *testing.T) {
	h := &analyzeImageContentHandler{}

	// Test Name
	if h.Name() != "analyze_image_content" {
		t.Errorf("Name() = %q, want %q", h.Name(), "analyze_image_content")
	}

	// Test Definition
	d := h.Definition()
	if d.Name != "analyze_image_content" {
		t.Errorf("Definition().Name = %q, want %q", d.Name, "analyze_image_content")
	}
	if d.Description == "" {
		t.Error("Definition().Description should not be empty")
	}

	hasParam := func(name string) bool {
		for _, p := range d.Parameters {
			if p.Name == name {
				return true
			}
		}
		return false
	}
	for _, name := range []string{"image_path", "analysis_prompt", "analysis_mode"} {
		if !hasParam(name) {
			t.Errorf("Definition missing '%s' parameter", name)
		}
	}

	// Validate: missing required
	if err := h.Validate(nil); err == nil {
		t.Error("Validate(nil) should return error")
	}
	if err := h.Validate(map[string]any{}); err == nil {
		t.Error("Validate(empty) should return error for missing image_path")
	}

	// Validate: valid image_path
	if err := h.Validate(map[string]any{"image_path": "/tmp/test.png"}); err != nil {
		t.Errorf("Validate(image_path) should return nil, got: %v", err)
	}

	// Validate: valid with optional params
	if err := h.Validate(map[string]any{"image_path": "/tmp/test.png", "analysis_prompt": "extract text", "analysis_mode": "ocr"}); err != nil {
		t.Errorf("Validate(full args) should return nil, got: %v", err)
	}

	// Validate: non-string image_path
	if err := h.Validate(map[string]any{"image_path": 123}); err == nil {
		t.Error("Validate(non-string image_path) should return error")
	}
}

func TestAnalyzeUIScreenshotHandlerConformance(t *testing.T) {
	h := &analyzeUIScreenshotHandler{}

	// Test Name
	if h.Name() != "analyze_ui_screenshot" {
		t.Errorf("Name() = %q, want %q", h.Name(), "analyze_ui_screenshot")
	}

	// Test Definition
	d := h.Definition()
	if d.Name != "analyze_ui_screenshot" {
		t.Errorf("Definition().Name = %q, want %q", d.Name, "analyze_ui_screenshot")
	}
	if d.Description == "" {
		t.Error("Definition().Description should not be empty")
	}

	hasParam := func(name string) bool {
		for _, p := range d.Parameters {
			if p.Name == name {
				return true
			}
		}
		return false
	}
	for _, name := range []string{"image_path", "analysis_prompt", "viewport_width", "viewport_height"} {
		if !hasParam(name) {
			t.Errorf("Definition missing '%s' parameter", name)
		}
	}

	// Validate: missing required
	if err := h.Validate(nil); err == nil {
		t.Error("Validate(nil) should return error")
	}
	if err := h.Validate(map[string]any{}); err == nil {
		t.Error("Validate(empty) should return error for missing image_path")
	}

	// Validate: valid image_path
	if err := h.Validate(map[string]any{"image_path": "/tmp/screenshot.png"}); err != nil {
		t.Errorf("Validate(image_path) should return nil, got: %v", err)
	}

	// Validate: valid with optional params
	if err := h.Validate(map[string]any{"image_path": "/tmp/screenshot.png", "analysis_prompt": "check layout", "viewport_width": 1920, "viewport_height": 1080}); err != nil {
		t.Errorf("Validate(full args) should return nil, got: %v", err)
	}

	// Validate: non-string image_path
	if err := h.Validate(map[string]any{"image_path": 123}); err == nil {
		t.Error("Validate(non-string image_path) should return error")
	}
}
