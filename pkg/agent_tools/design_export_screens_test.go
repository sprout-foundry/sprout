package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sprout-foundry/sprout/pkg/design"
)

// ---------------------------------------------------------------------------
// design_export_tokens `screens` target (SP-143 §143.5): the derived index
// write path through the ToolHandler seam.
// ---------------------------------------------------------------------------

// dxScreenHTML is a minimal runtime-era screen pair for the index tests.
const (
	dxScreenHome = `<!doctype html>
<html data-device="phone" data-states="ready">
<head><title>Home</title></head>
<body><a data-nav="to:detail;trigger:tap item">detail</a></body>
</html>
`
	dxScreenDetail = `<!doctype html>
<html data-device="phone" data-states="ready">
<head><title>Detail</title></head>
<body><a data-nav="to:home;trigger:tap back">back</a></body>
</html>
`
)

// dxWriteScreens writes the screen pair (and nothing else).
func dxWriteScreens(t *testing.T, root string) {
	t.Helper()
	daWrite(t, root, "design/screens/home.html", dxScreenHome)
	daWrite(t, root, "design/screens/detail.html", dxScreenDetail)
}

// TestDesignExportHandler_ScreensTargetWritesIndex pins the happy path: a
// screens-only run writes design/generated/screens.json derived from the
// screens' data-attributes, does not demand tokens, and reports the artifact.
func TestDesignExportHandler_ScreensTargetWritesIndex(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dxWriteScreens(t, root)
	h := &designExportHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{"targets": "screens"})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := res.StructuredOut.(designExportOutput)
	require.Len(t, out.Files, 1)
	assert.Equal(t, design.ExportTargetScreens, out.Files[0].Target)
	assert.Equal(t, "design/generated/screens.json", out.Files[0].Path)
	assert.Equal(t, []string{"screens.json"}, dxGeneratedFiles(t, root))

	data, err := os.ReadFile(filepath.Join(root, "design", "generated", "screens.json"))
	require.NoError(t, err)
	assert.Contains(t, string(data), `"stem": "home"`)
	assert.Contains(t, string(data), `"to": "detail"`)
	assert.Contains(t, string(data), `"trigger": "tap item"`)
	assert.Contains(t, string(data), `"source-hash": "fnv1a64:`)
	assert.Contains(t, res.Output, "screens.json")
}

// TestDesignExportHandler_ScreensTargetDeterministic pins byte-identical
// reruns (the drift check's foundation through the real handler).
func TestDesignExportHandler_ScreensTargetDeterministic(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dxWriteScreens(t, root)
	h := &designExportHandler{}

	args := map[string]any{"targets": "screens"}
	first, err := h.Execute(newTestCtx(root), newTestEnv(t, root), args)
	require.NoError(t, err)
	require.False(t, first.IsError)
	one, err := os.ReadFile(filepath.Join(root, "design", "generated", "screens.json"))
	require.NoError(t, err)

	for i := 0; i < 5; i++ {
		again, err := h.Execute(newTestCtx(root), newTestEnv(t, root), args)
		require.NoError(t, err)
		require.False(t, again.IsError)
		two, err := os.ReadFile(filepath.Join(root, "design", "generated", "screens.json"))
		require.NoError(t, err)
		require.Equal(t, string(one), string(two), "run %d", i)
	}
}

// TestDesignExportHandler_ScreensTargetStales pin the drift loop through the
// handler: after a screen edit the written index changes bytes (a re-run
// repairs what the validator flags).
func TestDesignExportHandler_ScreensTargetStales(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dxWriteScreens(t, root)
	h := &designExportHandler{}

	_, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{"targets": "screens"})
	require.NoError(t, err)
	before, err := os.ReadFile(filepath.Join(root, "design", "generated", "screens.json"))
	require.NoError(t, err)

	grown := strings.Replace(dxScreenDetail, `data-states="ready"`, `data-states="ready,loading"`, 1)
	daWrite(t, root, "design/screens/detail.html", grown)
	_, err = h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{"targets": "screens"})
	require.NoError(t, err)
	after, err := os.ReadFile(filepath.Join(root, "design", "generated", "screens.json"))
	require.NoError(t, err)
	assert.Contains(t, string(after), "loading")
	assert.NotEqual(t, string(before), string(after))
}

// TestDesignExportHandler_ScreensTargetEmptyRefused pins the sentinel: no
// screens, no index — a usage error, not an empty artifact.
func TestDesignExportHandler_ScreensTargetEmptyRefused(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	daWrite(t, root, "design/tokens/color.tokens.json", dxTokenJSON)
	h := &designExportHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{"targets": "screens"})
	require.Error(t, err)
	require.True(t, res.IsError)
	assert.Contains(t, res.Output, "no screens found")
	assert.Empty(t, dxGeneratedFiles(t, root), "nothing is written")
}

// TestDesignExportHandler_ScreensTargetNotMixed pin the isolation rule: the
// screens target is refused mixed with token targets (a token re-theme must
// never silently rewrite the screen graph).
func TestDesignExportHandler_ScreensTargetNotMixed(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	daWrite(t, root, "design/tokens/color.tokens.json", dxTokenJSON)
	dxWriteScreens(t, root)
	h := &designExportHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{"targets": "css,screens"})
	require.Error(t, err)
	require.True(t, res.IsError)
	assert.Contains(t, res.Output, "alone")
	assert.Empty(t, dxGeneratedFiles(t, root), "nothing is written from the refused run")

	// `all` remains token-only even with screens present.
	res, err = h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{})
	require.NoError(t, err)
	require.False(t, res.IsError)
	assert.NotContains(t, dxGeneratedFiles(t, root), "screens.json")
}

// TestDesignExportHandler_ScreensTargetGate1Deny pins Gate-1 on the index
// write path: a policy deny on the artifact path blocks the run.
func TestDesignExportHandler_ScreensTargetGate1Deny(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dxWriteScreens(t, root)
	h := &designExportHandler{}
	env := newTestEnv(t, root)
	env.FileAccessClassifier = denyGeneratedClassifier{}

	res, err := h.Execute(newTestCtx(root), env, map[string]any{"targets": "screens"})
	require.Error(t, err)
	require.True(t, res.IsError)
	assert.Contains(t, res.Output, "denied")
	assert.Empty(t, dxGeneratedFiles(t, root))
}
