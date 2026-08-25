package tools

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// SP-127 M2 extension: Handler-level precheck deny tests for the six
// handlers added after the first pass.
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// analyze_image_content_handler — deny on local path
// ---------------------------------------------------------------------------

func TestAnalyzeImageContent_Deny_LocalPath_ReturnsTypedError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &analyzeImageContentHandler{}

	env := ToolEnv{
		WorkspaceRoot:        dir,
		FileAccessClassifier: denyClassifier{},
	}

	result, err := h.Execute(context.Background(), env, map[string]any{
		"image_path": "/etc/hostname",
	})
	require.Error(t, err, "Execute should return an error on deny")
	require.True(t, result.IsError, "result.IsError should be true on deny")
	require.Contains(t, result.Output, "blocked", "output should mention 'blocked'")
	require.Contains(t, result.Output, "/etc/hostname", "output should include the path")
}

// ---------------------------------------------------------------------------
// analyze_image_content_handler — http URL skips gate
// ---------------------------------------------------------------------------

func TestAnalyzeImageContent_HTTPURL_SkipsGate(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &analyzeImageContentHandler{}

	// Even with a deny classifier, an http URL should skip the gate
	// and proceed to AnalyzeImage (which will fail without vision, but
	// should NOT return the "blocked" error).
	env := ToolEnv{
		WorkspaceRoot:        dir,
		FileAccessClassifier: denyClassifier{},
	}

	result, err := h.Execute(context.Background(), env, map[string]any{
		"image_path": "https://example.com/image.png",
	})
	// The deny classifier should NOT have blocked this — it should
	// have fallen through to AnalyzeImage. The result may fail for
	// other reasons (no vision capability), but it must NOT be the
	// "blocked" deny error.
	if err != nil {
		require.NotContains(t, err.Error(), "blocked", "http URL must not trigger deny error")
	}
	require.NotContains(t, result.Output, "blocked", "http URL must not trigger deny error")
}

// ---------------------------------------------------------------------------
// analyze_image_content_handler — https URL skips gate
// ---------------------------------------------------------------------------

func TestAnalyzeImageContent_HTTPSURL_SkipsGate(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &analyzeImageContentHandler{}

	env := ToolEnv{
		WorkspaceRoot:        dir,
		FileAccessClassifier: denyClassifier{},
	}

	result, err := h.Execute(context.Background(), env, map[string]any{
		"image_path": "https://example.com/photo.jpg",
	})
	if err != nil {
		require.NotContains(t, err.Error(), "blocked", "https URL must not trigger deny error")
	}
	require.NotContains(t, result.Output, "blocked", "https URL must not trigger deny error")
}

// ---------------------------------------------------------------------------
// analyze_ui_screenshot_handler — deny on local path
// ---------------------------------------------------------------------------

func TestAnalyzeUIScreenshot_Deny_LocalPath_ReturnsTypedError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &analyzeUIScreenshotHandler{}

	env := ToolEnv{
		WorkspaceRoot:        dir,
		FileAccessClassifier: denyClassifier{},
	}

	result, err := h.Execute(context.Background(), env, map[string]any{
		"image_path": "/etc/hostname",
	})
	require.Error(t, err, "Execute should return an error on deny")
	require.True(t, result.IsError, "result.IsError should be true on deny")
	require.Contains(t, result.Output, "blocked", "output should mention 'blocked'")
	require.Contains(t, result.Output, "/etc/hostname", "output should include the path")
}

// ---------------------------------------------------------------------------
// analyze_ui_screenshot_handler — http URL skips gate
// ---------------------------------------------------------------------------

func TestAnalyzeUIScreenshot_HTTPURL_SkipsGate(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &analyzeUIScreenshotHandler{}

	env := ToolEnv{
		WorkspaceRoot:        dir,
		FileAccessClassifier: denyClassifier{},
	}

	result, err := h.Execute(context.Background(), env, map[string]any{
		"image_path": "https://example.com/page.html",
	})
	// Must not trigger the deny error — the URL should skip the gate.
	if err != nil {
		require.NotContains(t, err.Error(), "blocked", "http URL must not trigger deny error")
	}
	require.NotContains(t, result.Output, "blocked", "http URL must not trigger deny error")
}

// ---------------------------------------------------------------------------
// repo_map_handler — deny on explicit outside-workspace directory
// ---------------------------------------------------------------------------

func TestRepoMap_Deny_ExplicitDirectory_ReturnsTypedError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &repoMapHandler{}

	env := ToolEnv{
		WorkspaceRoot:        dir,
		FileAccessClassifier: denyClassifier{},
	}

	result, err := h.Execute(context.Background(), env, map[string]any{
		"directory": "/etc",
	})
	require.Error(t, err, "Execute should return an error on deny")
	require.True(t, result.IsError, "result.IsError should be true on deny")
	require.Contains(t, result.Output, "blocked", "output should mention 'blocked'")
	require.Contains(t, result.Output, "/etc", "output should include the path")
}

// ---------------------------------------------------------------------------
// repo_map_handler — default "." does NOT trigger gate
// ---------------------------------------------------------------------------

func TestRepoMap_DefaultDirectory_SkipsGate(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &repoMapHandler{}

	// Even with a deny classifier, the default "." should skip the gate
	// and proceed to GenerateRepoMapWithSemanticMatches (which will
	// scan the temp dir and return something).
	env := ToolEnv{
		WorkspaceRoot:        dir,
		FileAccessClassifier: denyClassifier{},
	}

	result, err := h.Execute(context.Background(), env, map[string]any{})
	// Must NOT trigger the deny error — "." is the default and skips the gate.
	require.NotContains(t, result.Output, "blocked", "default directory must not trigger deny error")
	// The result may be empty or contain the directory listing, but not an error.
	if err != nil {
		require.NotContains(t, err.Error(), "blocked", "default directory must not trigger deny error")
	}
}

// ---------------------------------------------------------------------------
// repo_map_handler — prompt with approval on explicit directory
// ---------------------------------------------------------------------------

func TestRepoMap_Prompt_Approved_Proceeds(t *testing.T) {
	outside := outsideWorkspaceTarget(t)
	approved := &fakeFSPrompter{approve: true}
	env := ToolEnv{
		WorkspaceRoot:        t.TempDir(),
		FileAccessClassifier: &fakeClassifier{verdict: "prompt"},
		FileAccessPrompter:   approved,
	}
	h := &repoMapHandler{}

	result, err := h.Execute(context.Background(), env, map[string]any{
		"directory": filepath.Dir(outside),
	})
	// With approval, the gate passes and repo_map runs on the outside dir.
	// It may produce output or an error depending on what's in the dir,
	// but it must NOT be the deny "blocked" error.
	require.NotContains(t, result.Output, "blocked", "approved prompt must not return deny error")
	if err != nil {
		require.NotContains(t, err.Error(), "blocked", "approved prompt must not return deny error")
	}
}

// ---------------------------------------------------------------------------
// repo_map_handler — prompt denial on explicit directory returns typed error
// ---------------------------------------------------------------------------

func TestRepoMap_Prompt_Denied_Blocks(t *testing.T) {
	outside := outsideWorkspaceTarget(t)
	denied := &fakeFSPrompter{}
	env := ToolEnv{
		WorkspaceRoot:        t.TempDir(),
		FileAccessClassifier: &fakeClassifier{verdict: "prompt"},
		FileAccessPrompter:   denied,
	}
	h := &repoMapHandler{}

	result, err := h.Execute(context.Background(), env, map[string]any{
		"directory": filepath.Dir(outside),
	})
	require.Error(t, err, "prompt denial must block the operation")
	require.True(t, result.IsError, "result.IsError should be true on prompt denial")
	require.Contains(t, result.Output, "not approved", "prompt denial must surface the not-approved error")
	require.Equal(t, 1, denied.called, "prompter must have been consulted once")
}

// ---------------------------------------------------------------------------
// find_dead_code — deny on explicit directory
// ---------------------------------------------------------------------------

func TestFindDeadCode_Deny_ExplicitDirectory_ReturnsTypedError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &findDeadCodeHandler{}

	env := ToolEnv{
		WorkspaceRoot:        dir,
		FileAccessClassifier: denyClassifier{},
	}

	result, err := h.Execute(context.Background(), env, map[string]any{
		"directory": "/etc",
	})
	require.Error(t, err, "Execute should return an error on deny")
	require.True(t, result.IsError, "result.IsError should be true on deny")
	require.Contains(t, result.Output, "blocked", "output should mention 'blocked'")
	require.Contains(t, result.Output, "/etc", "output should include the path")
}

// ---------------------------------------------------------------------------
// find_dead_code — no directory arg does NOT trigger gate
// ---------------------------------------------------------------------------

func TestFindDeadCode_NoDirectory_SkipsGate(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &findDeadCodeHandler{}

	env := ToolEnv{
		WorkspaceRoot:        dir,
		FileAccessClassifier: denyClassifier{},
	}

	result, err := h.Execute(context.Background(), env, map[string]any{})
	// Must NOT trigger the deny error — no directory means no gate.
	// The result will likely say "not indexed" since there's no codegraph DB.
	require.NotContains(t, result.Output, "blocked", "no directory must not trigger deny error")
	if err != nil {
		require.NotContains(t, err.Error(), "blocked", "no directory must not trigger deny error")
	}
}

// ---------------------------------------------------------------------------
// search — deny on explicit directory
// ---------------------------------------------------------------------------

func TestSearch_Deny_ExplicitDirectory_ReturnsTypedError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &searchHandler{}

	env := ToolEnv{
		WorkspaceRoot:        dir,
		FileAccessClassifier: denyClassifier{},
	}

	result, err := h.Execute(context.Background(), env, map[string]any{
		"query":     "something",
		"directory": "/etc",
	})
	require.Error(t, err, "Execute should return an error on deny")
	require.True(t, result.IsError, "result.IsError should be true on deny")
	require.Contains(t, result.Output, "blocked", "output should mention 'blocked'")
	require.Contains(t, result.Output, "/etc", "output should include the path")
}

// ---------------------------------------------------------------------------
// search — no directory arg does NOT trigger gate
// ---------------------------------------------------------------------------

func TestSearch_NoDirectory_SkipsGate(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &searchHandler{}

	env := ToolEnv{
		WorkspaceRoot:        dir,
		FileAccessClassifier: denyClassifier{},
	}

	result, err := h.Execute(context.Background(), env, map[string]any{
		"query": "something",
	})
	// Must NOT trigger the deny error — no directory means no gate.
	require.NotContains(t, result.Output, "blocked", "no directory must not trigger deny error")
	if err != nil {
		require.NotContains(t, err.Error(), "blocked", "no directory must not trigger deny error")
	}
}

// ---------------------------------------------------------------------------
// search — "." directory does NOT trigger gate (default)
// ---------------------------------------------------------------------------

func TestSearch_DotDirectory_SkipsGate(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &searchHandler{}

	env := ToolEnv{
		WorkspaceRoot:        dir,
		FileAccessClassifier: denyClassifier{},
	}

	result, err := h.Execute(context.Background(), env, map[string]any{
		"query":     "something",
		"directory": ".",
	})
	// "." is the default; it should NOT trigger the gate.
	require.NotContains(t, result.Output, "blocked", "dot directory must not trigger deny error")
	if err != nil {
		require.NotContains(t, err.Error(), "blocked", "dot directory must not trigger deny error")
	}
}

// ---------------------------------------------------------------------------
// search_files — deny on explicit directory
// ---------------------------------------------------------------------------

func TestSearchFiles_Deny_ExplicitDirectory_ReturnsTypedError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &searchFilesHandler{}

	env := ToolEnv{
		WorkspaceRoot:        dir,
		FileAccessClassifier: denyClassifier{},
	}

	result, err := h.Execute(context.Background(), env, map[string]any{
		"search_pattern": "something",
		"directory":      "/etc",
	})
	require.Error(t, err, "Execute should return an error on deny")
	require.True(t, result.IsError, "result.IsError should be true on deny")
	require.Contains(t, result.Output, "blocked", "output should mention 'blocked'")
	require.Contains(t, result.Output, "/etc", "output should include the path")
}

// ---------------------------------------------------------------------------
// search_files — no directory arg does NOT trigger gate
// ---------------------------------------------------------------------------

func TestSearchFiles_NoDirectory_SkipsGate(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &searchFilesHandler{}

	env := ToolEnv{
		WorkspaceRoot:        dir,
		FileAccessClassifier: denyClassifier{},
	}

	result, err := h.Execute(context.Background(), env, map[string]any{
		"search_pattern": "something",
	})
	// Must NOT trigger the deny error — no directory means no gate.
	require.NotContains(t, result.Output, "blocked", "no directory must not trigger deny error")
	if err != nil {
		require.NotContains(t, err.Error(), "blocked", "no directory must not trigger deny error")
	}
}

// ---------------------------------------------------------------------------
// search_files — "." directory does NOT trigger gate (default)
// ---------------------------------------------------------------------------

func TestSearchFiles_DotDirectory_SkipsGate(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &searchFilesHandler{}

	env := ToolEnv{
		WorkspaceRoot:        dir,
		FileAccessClassifier: denyClassifier{},
	}

	result, err := h.Execute(context.Background(), env, map[string]any{
		"search_pattern": "something",
		"directory":      ".",
	})
	// "." is the default; it should NOT trigger the gate.
	require.NotContains(t, result.Output, "blocked", "dot directory must not trigger deny error")
	if err != nil {
		require.NotContains(t, err.Error(), "blocked", "dot directory must not trigger deny error")
	}
}

// ---------------------------------------------------------------------------
// isHTTPURL helper tests
// ---------------------------------------------------------------------------

func TestIsHTTPURL(t *testing.T) {
	t.Parallel()
	require.True(t, isHTTPURL("http://example.com"))
	require.True(t, isHTTPURL("https://example.com"))
	require.True(t, isHTTPURL("https://example.com/path/to/image.png"))
	require.False(t, isHTTPURL("/etc/hostname"))
	require.False(t, isHTTPURL("./local.png"))
	require.False(t, isHTTPURL(""))
	require.False(t, isHTTPURL("file:///etc/hostname"))
}

// ---------------------------------------------------------------------------
// analyze_image_content — prompt with approval on local path
// ---------------------------------------------------------------------------

func TestAnalyzeImageContent_Prompt_Approved_Proceeds(t *testing.T) {
	outside := outsideWorkspaceTarget(t)
	approved := &fakeFSPrompter{approve: true}
	env := ToolEnv{
		WorkspaceRoot:        t.TempDir(),
		FileAccessClassifier: &fakeClassifier{verdict: "prompt"},
		FileAccessPrompter:   approved,
	}
	h := &analyzeImageContentHandler{}

	result, err := h.Execute(context.Background(), env, map[string]any{
		"image_path": outside,
	})
	// With approval, the gate passes and AnalyzeImage runs.
	// It may fail without vision capability, but it must NOT be the deny "blocked" error.
	require.NotContains(t, result.Output, "blocked", "approved prompt must not return deny error")
	if err != nil {
		require.NotContains(t, err.Error(), "blocked", "approved prompt must not return deny error")
	}
}

// ---------------------------------------------------------------------------
// analyze_ui_screenshot — prompt with approval on local path
// ---------------------------------------------------------------------------

func TestAnalyzeUIScreenshot_Prompt_Approved_Proceeds(t *testing.T) {
	outside := outsideWorkspaceTarget(t)
	approved := &fakeFSPrompter{approve: true}
	env := ToolEnv{
		WorkspaceRoot:        t.TempDir(),
		FileAccessClassifier: &fakeClassifier{verdict: "prompt"},
		FileAccessPrompter:   approved,
	}
	h := &analyzeUIScreenshotHandler{}

	result, err := h.Execute(context.Background(), env, map[string]any{
		"image_path": outside,
	})
	// With approval, the gate passes and AnalyzeImage runs.
	require.NotContains(t, result.Output, "blocked", "approved prompt must not return deny error")
	if err != nil {
		require.NotContains(t, err.Error(), "blocked", "approved prompt must not return deny error")
	}
}

// ---------------------------------------------------------------------------
// analyze_image_content — prompt denial on local path blocks
// ---------------------------------------------------------------------------

func TestAnalyzeImageContent_Prompt_Denied_Blocks(t *testing.T) {
	outside := outsideWorkspaceTarget(t)
	denied := &fakeFSPrompter{}
	env := ToolEnv{
		WorkspaceRoot:        t.TempDir(),
		FileAccessClassifier: &fakeClassifier{verdict: "prompt"},
		FileAccessPrompter:   denied,
	}
	h := &analyzeImageContentHandler{}

	result, err := h.Execute(context.Background(), env, map[string]any{
		"image_path": outside,
	})
	require.Error(t, err, "prompt denial must block the operation")
	require.True(t, result.IsError, "result.IsError should be true on prompt denial")
	require.Contains(t, result.Output, "not approved", "prompt denial must surface the not-approved error")
	require.Equal(t, 1, denied.called, "prompter must have been consulted once")
}

// ---------------------------------------------------------------------------
// analyze_ui_screenshot — prompt denial on local path blocks
// ---------------------------------------------------------------------------

func TestAnalyzeUIScreenshot_Prompt_Denied_Blocks(t *testing.T) {
	outside := outsideWorkspaceTarget(t)
	denied := &fakeFSPrompter{}
	env := ToolEnv{
		WorkspaceRoot:        t.TempDir(),
		FileAccessClassifier: &fakeClassifier{verdict: "prompt"},
		FileAccessPrompter:   denied,
	}
	h := &analyzeUIScreenshotHandler{}

	result, err := h.Execute(context.Background(), env, map[string]any{
		"image_path": outside,
	})
	require.Error(t, err, "prompt denial must block the operation")
	require.True(t, result.IsError, "result.IsError should be true on prompt denial")
	require.Contains(t, result.Output, "not approved", "prompt denial must surface the not-approved error")
	require.Equal(t, 1, denied.called, "prompter must have been consulted once")
}

// ---------------------------------------------------------------------------
// search — prompt with approval on explicit outside directory
// ---------------------------------------------------------------------------

func TestSearch_Prompt_Approved_Proceeds(t *testing.T) {
	outside := outsideWorkspaceTarget(t)
	approved := &fakeFSPrompter{approve: true}
	env := ToolEnv{
		WorkspaceRoot:        t.TempDir(),
		FileAccessClassifier: &fakeClassifier{verdict: "prompt"},
		FileAccessPrompter:   approved,
	}
	h := &searchHandler{}

	result, err := h.Execute(context.Background(), env, map[string]any{
		"query":     "outside",
		"directory": filepath.Dir(outside),
	})
	require.NotContains(t, result.Output, "blocked", "approved prompt must not return deny error")
	if err != nil {
		require.NotContains(t, err.Error(), "blocked", "approved prompt must not return deny error")
	}
}

// ---------------------------------------------------------------------------
// search_files — prompt with approval on explicit outside directory
// ---------------------------------------------------------------------------

func TestSearchFiles_Prompt_Approved_Proceeds(t *testing.T) {
	outside := outsideWorkspaceTarget(t)
	approved := &fakeFSPrompter{approve: true}
	env := ToolEnv{
		WorkspaceRoot:        t.TempDir(),
		FileAccessClassifier: &fakeClassifier{verdict: "prompt"},
		FileAccessPrompter:   approved,
	}
	h := &searchFilesHandler{}

	result, err := h.Execute(context.Background(), env, map[string]any{
		"search_pattern": "outside",
		"directory":      filepath.Dir(outside),
	})
	require.NotContains(t, result.Output, "blocked", "approved prompt must not return deny error")
	if err != nil {
		require.NotContains(t, err.Error(), "blocked", "approved prompt must not return deny error")
	}
}

// ---------------------------------------------------------------------------
// find_dead_code — prompt with approval on explicit directory
// ---------------------------------------------------------------------------

func TestFindDeadCode_Prompt_Approved_Proceeds(t *testing.T) {
	outside := outsideWorkspaceTarget(t)
	approved := &fakeFSPrompter{approve: true}
	env := ToolEnv{
		WorkspaceRoot:        t.TempDir(),
		FileAccessClassifier: &fakeClassifier{verdict: "prompt"},
		FileAccessPrompter:   approved,
	}
	h := &findDeadCodeHandler{}

	result, err := h.Execute(context.Background(), env, map[string]any{
		"directory": filepath.Dir(outside),
	})
	require.NotContains(t, result.Output, "blocked", "approved prompt must not return deny error")
	if err != nil {
		require.NotContains(t, err.Error(), "blocked", "approved prompt must not return deny error")
	}
}

// ---------------------------------------------------------------------------
// Mode verification: each new handler passes "read" mode to the classifier
// ---------------------------------------------------------------------------

func TestNewHandlers_PassReadMode(t *testing.T) {
	t.Parallel()
	// analyze_image_content
	cc := &captureModeClassifier{}
	env := ToolEnv{WorkspaceRoot: t.TempDir(), FileAccessClassifier: cc}
	h := &analyzeImageContentHandler{}
	h.Execute(context.Background(), env, map[string]any{"image_path": "/etc/hosts"})
	require.Equal(t, "read", cc.gotMode, "analyze_image_content should pass read mode")

	// analyze_ui_screenshot
	cc = &captureModeClassifier{}
	env = ToolEnv{WorkspaceRoot: t.TempDir(), FileAccessClassifier: cc}
	h2 := &analyzeUIScreenshotHandler{}
	h2.Execute(context.Background(), env, map[string]any{"image_path": "/etc/hosts"})
	require.Equal(t, "read", cc.gotMode, "analyze_ui_screenshot should pass read mode")

	// repo_map
	cc = &captureModeClassifier{}
	env = ToolEnv{WorkspaceRoot: t.TempDir(), FileAccessClassifier: cc}
	h3 := &repoMapHandler{}
	h3.Execute(context.Background(), env, map[string]any{"directory": "/etc"})
	require.Equal(t, "read", cc.gotMode, "repo_map should pass read mode")

	// find_dead_code
	cc = &captureModeClassifier{}
	env = ToolEnv{WorkspaceRoot: t.TempDir(), FileAccessClassifier: cc}
	h4 := &findDeadCodeHandler{}
	h4.Execute(context.Background(), env, map[string]any{"directory": "/etc"})
	require.Equal(t, "read", cc.gotMode, "find_dead_code should pass read mode")

	// search
	cc = &captureModeClassifier{}
	env = ToolEnv{WorkspaceRoot: t.TempDir(), FileAccessClassifier: cc}
	h5 := &searchHandler{}
	h5.Execute(context.Background(), env, map[string]any{"query": "x", "directory": "/etc"})
	require.Equal(t, "read", cc.gotMode, "search should pass read mode")

	// search_files
	cc = &captureModeClassifier{}
	env = ToolEnv{WorkspaceRoot: t.TempDir(), FileAccessClassifier: cc}
	h6 := &searchFilesHandler{}
	h6.Execute(context.Background(), env, map[string]any{"search_pattern": "x", "directory": "/etc"})
	require.Equal(t, "read", cc.gotMode, "search_files should pass read mode")
}

// ---------------------------------------------------------------------------
// HTTP URL gate-skip verification: deny classifier + http URL = no gate call
// ---------------------------------------------------------------------------

func TestAnalyzeImageContent_HTTPURL_NoClassifierCall(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// neverCallClassifier returns "deny" but panics if actually called —
	// if the gate is skipped, it won't be called.
	ncc := &neverCallClassifier{}
	h := &analyzeImageContentHandler{}

	env := ToolEnv{
		WorkspaceRoot:        dir,
		FileAccessClassifier: ncc,
	}

	// This should NOT call the classifier since it's an http URL.
	_, _ = h.Execute(context.Background(), env, map[string]any{
		"image_path": "https://example.com/image.png",
	})
	require.False(t, ncc.called, "classifier must NOT be called for http URL")
}

func TestAnalyzeUIScreenshot_HTTPURL_NoClassifierCall(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ncc := &neverCallClassifier{}
	h := &analyzeUIScreenshotHandler{}

	env := ToolEnv{
		WorkspaceRoot:        dir,
		FileAccessClassifier: ncc,
	}

	_, _ = h.Execute(context.Background(), env, map[string]any{
		"image_path": "https://example.com/page.html",
	})
	require.False(t, ncc.called, "classifier must NOT be called for https URL")
}

// ---------------------------------------------------------------------------
// Default directory gate-skip verification
// ---------------------------------------------------------------------------

func TestRepoMap_DefaultDirectory_NoClassifierCall(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ncc := &neverCallClassifier{}
	h := &repoMapHandler{}

	env := ToolEnv{
		WorkspaceRoot:        dir,
		FileAccessClassifier: ncc,
	}

	_, _ = h.Execute(context.Background(), env, map[string]any{})
	require.False(t, ncc.called, "classifier must NOT be called for default directory")
}

func TestSearch_NoDirectory_NoClassifierCall(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ncc := &neverCallClassifier{}
	h := &searchHandler{}

	env := ToolEnv{
		WorkspaceRoot:        dir,
		FileAccessClassifier: ncc,
	}

	_, _ = h.Execute(context.Background(), env, map[string]any{"query": "x"})
	require.False(t, ncc.called, "classifier must NOT be called when no directory arg")
}

func TestSearch_DotDirectory_NoClassifierCall(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ncc := &neverCallClassifier{}
	h := &searchHandler{}

	env := ToolEnv{
		WorkspaceRoot:        dir,
		FileAccessClassifier: ncc,
	}

	_, _ = h.Execute(context.Background(), env, map[string]any{"query": "x", "directory": "."})
	require.False(t, ncc.called, "classifier must NOT be called for '.' directory")
}

func TestSearchFiles_NoDirectory_NoClassifierCall(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ncc := &neverCallClassifier{}
	h := &searchFilesHandler{}

	env := ToolEnv{
		WorkspaceRoot:        dir,
		FileAccessClassifier: ncc,
	}

	_, _ = h.Execute(context.Background(), env, map[string]any{"search_pattern": "x"})
	require.False(t, ncc.called, "classifier must NOT be called when no directory arg")
}

func TestSearchFiles_DotDirectory_NoClassifierCall(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ncc := &neverCallClassifier{}
	h := &searchFilesHandler{}

	env := ToolEnv{
		WorkspaceRoot:        dir,
		FileAccessClassifier: ncc,
	}

	_, _ = h.Execute(context.Background(), env, map[string]any{"search_pattern": "x", "directory": "."})
	require.False(t, ncc.called, "classifier must NOT be called for '.' directory")
}

func TestFindDeadCode_NoDirectory_NoClassifierCall(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ncc := &neverCallClassifier{}
	h := &findDeadCodeHandler{}

	env := ToolEnv{
		WorkspaceRoot:        dir,
		FileAccessClassifier: ncc,
	}

	_, _ = h.Execute(context.Background(), env, map[string]any{})
	require.False(t, ncc.called, "classifier must NOT be called when no directory arg")
}

// neverCallClassifier returns "deny" but panics if actually called,
// so tests can verify the gate is truly skipped.
type neverCallClassifier struct {
	called bool
}

func (n *neverCallClassifier) ClassifyFileAccess(_ context.Context, _, _, _ string) string {
	n.called = true
	panic("classifier was called when it should have been skipped")
}
func (neverCallClassifier) IsFolderSessionAllowed(_ string) bool { return false }
