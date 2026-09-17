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
// WASM design-tool roster (SP-140 invariant 7, SP-140-2 AC)
//
// The design tier splits along one line: pure-Go tools ship on every platform,
// browser/vision-dependent tools ship only on native.
//
//	design_validate       pure Go  → shared AllTools (no build tag)
//	design_assets         pure Go  → shared AllTools (no build tag)
//	design_render         browser  → //go:build !js + js stub
//	design_import_sketch  vision   → //go:build !js + js stub
//	design_critique       browser+vision → //go:build !js + js stub
//
// SP-140 invariant 7 states it as: "Browser- and vision-dependent tools
// (design_render, design_import_sketch, design_critique) are //go:build !js
// with WASM stubs mirroring all_vision.go; only design_assets and
// design_validate ship WASM variants."
//
// These tests are named *_test.go with no build constraint so they run in the
// ordinary native suite, where they read the build-tagged sources off disk.
// That is deliberate and matches scripts/wasm-tool-roster-smoke.sh: the
// pkg/agent_tools *test* files as a whole are not WASM-clean (binary_fetch_test.go,
// codegraph_handler_test.go, handle_*_test.go reference native-only symbols),
// so a //go:build js test file here could never be compiled, let alone run.
// Asserting over the sources is the assertion that actually executes.
// ---------------------------------------------------------------------------

const (
	designValidateHandlerFile    = "design_validate_handler.go"
	designAssetsHandlerFile      = "design_assets_handler.go"
	designRenderHandlerFile      = "design_render_handler.go"
	designRenderWasmStubFile     = "design_render_handler_js.go"
	designSketchHandlerFile      = "design_import_sketch_handler.go"
	designSketchWasmStubFile     = "design_import_sketch_handler_js.go"
	designCritiqueHandlerFile    = "design_critique_handler.go"
	designCritiqueWasmStubFile   = "design_critique_handler_js.go"
	designRenderRegistrar        = "registerDesignRenderTools"
	designSketchRegistrar        = "registerDesignImportSketchTools"
	designCritiqueRegistrar      = "registerDesignCritiqueTools"
	designAllToolsFile           = "all.go"
	designWasmBuildTag           = "//go:build js"
	designNativeBuildTag         = "//go:build !js"
	designWasmVariantSuffix      = "_js.go"
	designWasmVariantAltSuffix   = "_wasm.go"
	designUnconditionalBuildLine = "//go:build"
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

// TestDesignAssets_IsWASMEligibleSharedTool pins that design_assets carries no
// build constraint: it is registered in the shared AllTools list, so it is on
// the roster for native and WASM builds alike (SP-140-2 AC, SP-140 invariant 7).
//
// If someone adds a //go:build !js tag to design_assets_handler.go without also
// adding a registrar, the tool silently vanishes from WASM — this fails first.
func TestDesignAssets_IsWASMEligibleSharedTool(t *testing.T) {
	t.Parallel()

	src := readToolSource(t, designAssetsHandlerFile)
	assert.Empty(t, buildConstraints(src),
		"%s must stay build-constraint-free: design_assets is pure Go and is "+
			"registered in the shared AllTools list (SP-140-2 AC)", designAssetsHandlerFile)

	all := readToolSource(t, designAllToolsFile)
	assert.Contains(t, all, "&designAssetsHandler{}",
		"design_assets must be constructed in the shared AllTools list, not behind a registrar")

	// The handler's own doc comment is the contract other readers trust.
	assert.Contains(t, src, "Pure Go with no browser or vision dependencies",
		"%s should keep documenting why it needs no WASM stub", designAssetsHandlerFile)
}

// TestDesignValidate_IsWASMEligibleSharedTool pins the same property for
// design_validate, the other tool SP-140 invariant 7 keeps on WASM.
func TestDesignValidate_IsWASMEligibleSharedTool(t *testing.T) {
	t.Parallel()

	src := readToolSource(t, designValidateHandlerFile)
	assert.Empty(t, buildConstraints(src),
		"%s must stay build-constraint-free: design_validate is pure Go and is "+
			"registered in the shared AllTools list (SP-140 invariant 7)", designValidateHandlerFile)

	all := readToolSource(t, designAllToolsFile)
	assert.Contains(t, all, "&designValidateHandler{}",
		"design_validate must be constructed in the shared AllTools list")
}

// TestDesignRender_ExcludedFromWASM is the SP-140-2 AC check that
// design_render is absent from the WASM roster: the native handler is
// //go:build !js and the js stub returns nil.
func TestDesignRender_ExcludedFromWASM(t *testing.T) {
	t.Parallel()
	assertExcludedFromWASM(t, designRenderHandlerFile, designRenderWasmStubFile, designRenderRegistrar)
}

// TestDesignImportSketch_ExcludedFromWASM does the same for
// design_import_sketch, which needs the vision tier for extraction.
func TestDesignImportSketch_ExcludedFromWASM(t *testing.T) {
	t.Parallel()
	assertExcludedFromWASM(t, designSketchHandlerFile, designSketchWasmStubFile, designSketchRegistrar)
}

// TestDesignCritique_ExcludedFromWASM does the same for design_critique
// (SP-140-4 §4a), which needs both the browser tier (rasterization) and the
// vision tier (the critique itself).
func TestDesignCritique_ExcludedFromWASM(t *testing.T) {
	t.Parallel()
	assertExcludedFromWASM(t, designCritiqueHandlerFile, designCritiqueWasmStubFile, designCritiqueRegistrar)
}

// assertExcludedFromWASM checks the three-part contract for a
// browser/vision-dependent tool:
//
//  1. the native implementation is built only for !js;
//  2. a js-tagged stub file defines the same registrar and returns nil;
//  3. every registrar name the stub defines is actually called from all.go,
//     so the exclusion is wired in rather than merely declared.
func assertExcludedFromWASM(t *testing.T, handlerFile, stubFile, registrar string) {
	t.Helper()

	handler := readToolSource(t, handlerFile)
	require.Contains(t, buildConstraints(handler), designNativeBuildTag,
		"%s must carry `%s` and ship a registered WASM stub (SP-140 invariant 7)",
		handlerFile, designNativeBuildTag)
	assert.Contains(t, handler, "func "+registrar+"()",
		"%s must define %s", handlerFile, registrar)

	stub := readToolSource(t, stubFile)
	assert.Contains(t, buildConstraints(stub), designWasmBuildTag,
		"%s must carry `%s`", stubFile, designWasmBuildTag)
	assert.Contains(t, stub, "func "+registrar+"()",
		"%s must define the same registrar %s", stubFile, registrar)
	assert.Contains(t, stub, "return nil",
		"%s must return nil: the tool must be unregistered, not registered-but-broken "+
			"(SP-112 Tier 2 precedent)", stubFile)

	// The stub must not construct a native handler — that would not compile on js.
	handlerType := designHandlerType(handler)
	require.NotEmpty(t, handlerType, "%s should declare a design*Handler struct", handlerFile)
	assert.NotContains(t, stub, "&"+handlerType+"{}",
		"%s must not construct %s; that type is declared only in the !js file", stubFile, handlerType)

	all := readToolSource(t, designAllToolsFile)
	assert.Contains(t, all, registrar+"()",
		"all.go must call %s() so the WASM exclusion is wired into the roster", registrar)
}

// TestDesignWasmStubNamingConvention pins the file-naming half of
// "per the all_*_wasm.go pattern": each WASM stub is either _js.go or _wasm.go
// so `go list` build-tag filtering and the roster smoke script can find them.
func TestDesignWasmStubNamingConvention(t *testing.T) {
	t.Parallel()

	for _, stub := range []string{designRenderWasmStubFile, designSketchWasmStubFile, designCritiqueWasmStubFile} {
		ok := strings.HasSuffix(stub, designWasmVariantSuffix) ||
			strings.HasSuffix(stub, designWasmVariantAltSuffix)
		assert.True(t, ok, "%s should follow the _js.go / _wasm.go WASM-stub convention", stub)
	}
}

// TestDesignToolWasmSplitIsComplete is the aggregate guard: every design tool
// handler in the package must be in exactly one of the two buckets — shared
// AllTools (no constraint) or native-only with a nil-returning stub.
//
// It walks the package directory rather than a hardcoded list, so a new design
// tool added without a decision about the WASM roster fails here. The handler
// type is read out of the source (the `type designXHandler struct{}`
// declaration) and matched against all.go, so the check follows renames.
func TestDesignToolWasmSplitIsComplete(t *testing.T) {
	t.Parallel()

	entries, err := os.ReadDir(".")
	require.NoError(t, err)

	all := readToolSource(t, designAllToolsFile)

	var shared, nativeOnly int
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, "design_") || !strings.HasSuffix(name, ".go") {
			continue
		}
		// Test files declare no production handler.
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		// Stubs are the js side of a pair; they define no handler type.
		if strings.HasSuffix(name, designWasmVariantSuffix) || strings.HasSuffix(name, designWasmVariantAltSuffix) {
			continue
		}

		src := readToolSource(t, name)
		handlerType := designHandlerType(src)
		if handlerType == "" {
			// Not a handler file (e.g. design_render_mermaid.go helpers).
			continue
		}

		if len(buildConstraints(src)) == 0 {
			shared++
			assert.Contains(t, all, "&"+handlerType+"{}",
				"%s declares %s with no build constraint, so it is a shared "+
					"(WASM-eligible) design tool and must be constructed in all.go's "+
					"unconditional list", name, handlerType)
			continue
		}

		// Native-only: the !js constraint must come with a js stub that
		// defines a registrar all.go calls, and all.go must not construct the
		// handler directly (it is declared in a !js file).
		nativeOnly++
		registrar := designRegistrarFor(handlerType)
		assert.NotContains(t, all, "&"+handlerType+"{}",
			"%s is native-only (%v); all.go must reach %s through its registrar, not "+
				"construct it directly", name, buildConstraints(src), handlerType)
		assert.Contains(t, all, registrar+"()",
			"%s is native-only, so all.go must call %s() (whose js stub returns nil)",
			name, registrar)
	}

	assert.GreaterOrEqual(t, shared, 2,
		"expected design_validate and design_assets to be discovered as shared design handlers")
	assert.GreaterOrEqual(t, nativeOnly, 3,
		"expected design_render, design_import_sketch, and design_critique to be discovered "+
			"as native-only design handlers")
}

// designHandlerType extracts the handler type name from a
// `type designXHandler struct{}` declaration, or "" when the file declares no
// such type.
func designHandlerType(src string) string {
	for _, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(line)
		rest, ok := strings.CutPrefix(trimmed, "type ")
		if !ok {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) < 2 {
			continue
		}
		if strings.HasPrefix(fields[0], "design") && strings.HasSuffix(fields[0], "Handler") &&
			fields[1] == "struct{}" {
			return fields[0]
		}
	}
	return ""
}

// designRegistrarFor maps a handler type to the registrar naming convention
// the design-tier stubs use: designRenderHandler → registerDesignRenderTools.
func designRegistrarFor(handlerType string) string {
	base := strings.TrimSuffix(strings.TrimPrefix(handlerType, "design"), "Handler")
	return "registerDesign" + base + "Tools"
}

// TestDesignAssetsHasNoWasmRegistrar documents the negative: there is no
// registerDesignAssetsTools registrar, because design_assets needs no WASM
// variant — it is shared. A future edit that "adds a WASM variant" by
// introducing such a registrar would be a regression against SP-140-2 AC, so
// the absence is asserted explicitly, including for the all_assets_wasm.go
// filename the AC anticipated (it is unnecessary: there is no per-tool design
// registrar file, and the shared registration already covers WASM).
func TestDesignAssetsHasNoWasmRegistrar(t *testing.T) {
	t.Parallel()

	const absent = "registerDesignAssetsTools"
	for _, name := range []string{
		designAllToolsFile,
		designAssetsHandlerFile,
		"all_assets_wasm.go",
	} {
		if _, err := os.Stat(filepath.Join(".", name)); err != nil {
			continue
		}
		assert.NotContains(t, readToolSource(t, name), absent,
			"%s must not introduce %s: design_assets is shared/WASM-eligible and needs "+
				"no build-tagged registrar (SP-140-2 AC)", name, absent)
	}
}

// TestDesignWasmStubFilesAreNotOrphaned verifies each _js stub file's
// excluded-by-native counterpart exists, so the build-tag pair is real.
func TestDesignWasmStubPairsExist(t *testing.T) {
	t.Parallel()

	pairs := map[string]string{
		designRenderHandlerFile:   designRenderWasmStubFile,
		designSketchHandlerFile:   designSketchWasmStubFile,
		designCritiqueHandlerFile: designCritiqueWasmStubFile,
	}
	for native, stub := range pairs {
		_, nativeErr := os.Stat(filepath.Join(".", native))
		_, stubErr := os.Stat(filepath.Join(".", stub))
		assert.NoError(t, nativeErr, "native implementation %s must exist", native)
		assert.NoError(t, stubErr, "WASM stub %s must exist", stub)
		assert.NotContains(t, buildConstraints(readToolSource(t, stub)), "!js",
			"%s is the js side of the pair and must not carry a !js constraint", stub)
		assert.NotContains(t, buildConstraints(readToolSource(t, native)), designWasmBuildTag,
			"%s must not build on js", native)
	}
}
