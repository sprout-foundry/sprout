package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// WASM design-tool roster (SP-140 invariant 7 as amended by SP-158)
//
// Every design tool is shared: its handler file carries no build constraint
// and all.go constructs it in the unconditional AllTools list, so native and
// browser builds advertise the same design tools. What differs per build is
// below the handlers:
//
//	rasterization  native: headless browser    browser: host page (pkg/webcontent)
//	critique text  native: vision tier         browser: the primary model reads
//	                                                     the attached render
//
// These tests are *_test.go with no build constraint so they run in the
// native suite and read the sources off disk: the pkg/agent_tools test files
// as a whole are not WASM-clean, so a //go:build js test could never compile.
// scripts/wasm-tool-roster-smoke.sh asserts the same split on the real WASM
// file selection.
// ---------------------------------------------------------------------------

const (
	designAllToolsFile      = "all.go"
	designWasmVariantSuffix = "_js.go"
)

// readToolSource reads a file from the package directory.
func readToolSource(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(".", name))
	require.NoErrorf(t, err, "reading %s", name)
	return string(b)
}

// buildConstraints returns the leading //go:build directives of a Go file.
func buildConstraints(src string) []string {
	var out []string
	for _, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//go:build") {
			out = append(out, trimmed)
			continue
		}
		// Constraints must be in the file's leading comment block.
		if trimmed != "" && !strings.HasPrefix(trimmed, "//") {
			break
		}
	}
	return out
}

// designHandlerType extracts the handler type name from a
// `type designXHandler struct{}` declaration, or "" when the file declares no
// such type.
func designHandlerType(src string) string {
	for _, line := range strings.Split(src, "\n") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), "type ")
		if !ok {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) >= 2 && strings.HasPrefix(fields[0], "design") &&
			strings.HasSuffix(fields[0], "Handler") && fields[1] == "struct{}" {
			return fields[0]
		}
	}
	return ""
}

// TestDesignToolsAreAllShared walks the package so a new design tool cannot
// land native-only without this test changing: each handler file has no build
// constraint and is constructed in all.go's unconditional list.
func TestDesignToolsAreAllShared(t *testing.T) {
	t.Parallel()

	entries, err := os.ReadDir(".")
	require.NoError(t, err)
	all := readToolSource(t, designAllToolsFile)

	found := map[string]bool{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, "design_") || !strings.HasSuffix(name, ".go") ||
			strings.HasSuffix(name, "_test.go") {
			continue
		}
		src := readToolSource(t, name)
		handlerType := designHandlerType(src)
		if handlerType == "" {
			continue
		}
		found[handlerType] = true
		assert.Empty(t, buildConstraints(src),
			"%s declares %s; design tools ship to every build, so it must carry no build constraint", name, handlerType)
		assert.Contains(t, all, "&"+handlerType+"{}",
			"%s must be constructed in all.go's unconditional list", handlerType)
	}

	for _, want := range []string{
		"designValidateHandler", "designAssetsHandler", "designBriefHandler",
		"designExportHandler", "designSyncHandler", "designRenderHandler",
		"designCritiqueHandler", "designImportSketchHandler",
	} {
		assert.True(t, found[want], "expected to discover %s", want)
	}
}

// TestDesignToolsHaveNoWasmStubs: a nil-returning registrar stub would take a
// tool off the browser roster again. The only design *_js.go file is the
// browser variant of the critique's vision pass, which declares no handler.
func TestDesignToolsHaveNoWasmStubs(t *testing.T) {
	t.Parallel()

	entries, err := os.ReadDir(".")
	require.NoError(t, err)
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "design_") || !strings.HasSuffix(name, designWasmVariantSuffix) {
			continue
		}
		assert.Equal(t, "design_critique_vision_js.go", name, "unexpected design WASM variant %s", name)
		src := readToolSource(t, name)
		assert.NotContains(t, src, "func registerDesign", "%s must not register or unregister a tool", name)
		assert.Empty(t, designHandlerType(src), "%s must not declare a handler", name)
	}
}

// TestPageRendererBacksBrowserScreenshots pins the seam the shared render
// tools rely on in the browser build: the WASM renderer is the page-backed
// one, not the always-failing no-op.
func TestPageRendererBacksBrowserScreenshots(t *testing.T) {
	t.Parallel()

	src, err := os.ReadFile(filepath.Join("..", "webcontent", "browser_none.go"))
	require.NoError(t, err)
	assert.Contains(t, string(src), "//go:build js")
	assert.Contains(t, string(src), "return &pageRenderer{}")
}
