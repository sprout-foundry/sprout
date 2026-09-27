package design

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// SP-143 §143.5 — the validator rules over fixture trees: nav-target
// existence, states-declared-before-use, the runtime source-hash check,
// screens.json drift, the single-file dispatch registration, and the export
// artifact/target surface.
// ---------------------------------------------------------------------------

// TestValidateScreensIndex_NavTargets pins rule (a): a data-nav to: stem that
// resolves to no screen in the tree is a hard error, with the target named.
func TestValidateScreensIndex_NavTargets(t *testing.T) {
	root := t.TempDir()
	writeIndexTree(t, root)

	findings, err := ValidateScreensDir(root)
	require.NoError(t, err)
	for _, f := range findings {
		assert.NotEqual(t, ruleScreenNavTarget, f.Rule, "all targets resolve here: %+v", f)
	}

	// Break one target.
	broken := strings.Replace(indexScreenDoc, "to:detail", "to:missing-screen", 1)
	require.NoError(t, os.WriteFile(filepath.Join(root, DirName, "screens", "home.html"), []byte(broken), 0o644))
	findings, err = ValidateScreensDir(root)
	require.NoError(t, err)
	var hits []Finding
	for _, f := range findings {
		if f.Rule == ruleScreenNavTarget {
			hits = append(hits, f)
		}
	}
	require.Len(t, hits, 1)
	assert.Equal(t, SeverityError, hits[0].Severity)
	assert.Contains(t, hits[0].Message, `"missing-screen"`)
	assert.Equal(t, "design/screens/home.html", hits[0].File)
}

// TestValidateScreensIndex_NavFormat pins the companion format rule: a
// data-nav with no to: stem is a hard error, line-anchored.
func TestValidateScreensIndex_NavFormat(t *testing.T) {
	root := t.TempDir()
	writeIndexTree(t, root)
	broken := strings.Replace(indexScreenDoc, `data-nav="to:detail;trigger:tap item"`, `data-nav="trigger:tap item"`, 1)
	require.NoError(t, os.WriteFile(filepath.Join(root, DirName, "screens", "home.html"), []byte(broken), 0o644))

	findings, err := ValidateScreensDir(root)
	require.NoError(t, err)
	var hits []Finding
	for _, f := range findings {
		if f.Rule == ruleScreenNavFormat {
			hits = append(hits, f)
		}
	}
	require.Len(t, hits, 1)
	assert.Equal(t, SeverityError, hits[0].Severity)
	assert.Equal(t, 8, hits[0].Line)
}

// TestValidateScreensIndex_StatesDeclaredBeforeUse pins rule (b): a
// data-state section on a screen that does not declare the name in
// data-states is a hard error; the undeclared name is in the message.
func TestValidateScreensIndex_StatesDeclaredBeforeUse(t *testing.T) {
	root := t.TempDir()
	writeIndexTree(t, root)

	findings, err := ValidateScreensDir(root)
	require.NoError(t, err)
	for _, f := range findings {
		assert.NotEqual(t, ruleScreenStateDeclared, f.Rule, "declared before use here: %+v", f)
	}

	// Drop a declaration: the section becomes an error.
	undeclared := strings.Replace(indexScreenDoc, `data-states="empty,error,ready"`, `data-states="error,ready"`, 1)
	require.NoError(t, os.WriteFile(filepath.Join(root, DirName, "screens", "home.html"), []byte(undeclared), 0o644))
	findings, err = ValidateScreensDir(root)
	require.NoError(t, err)
	hits := findingsFor(t, findings, ruleScreenStateDeclared)
	require.Len(t, hits, 1)
	assert.Equal(t, SeverityError, hits[0].Severity)
	assert.Contains(t, hits[0].Message, `"empty"`)

	// No declaration at all: every section is an error.
	nodecl := strings.Replace(indexScreenDoc, ` data-states="empty,error,ready"`, "", 1)
	require.NoError(t, os.WriteFile(filepath.Join(root, DirName, "screens", "home.html"), []byte(nodecl), 0o644))
	findings, err = ValidateScreensDir(root)
	require.NoError(t, err)
	hits = findingsFor(t, findings, ruleScreenStateDeclared)
	assert.Len(t, hits, 2, "both undeclared sections are reported")
	for _, f := range hits {
		assert.Contains(t, f.Message, "declares no data-states")
	}
}

// TestValidateScreensIndex_RuntimeHash pins rule (c): a present runtime
// verifies by its self-zeroing hash (clean); a hand-edit is an error; a
// headerless copy is an error; a missing runtime with screens on disk is
// info only — old trees stay clean.
func TestValidateScreensIndex_RuntimeHash(t *testing.T) {
	root := t.TempDir()
	writeIndexTree(t, root)
	require.NoError(t, ScaffoldRuntimeAssets(filepath.Join(root, DirName)))

	findings, err := ValidateScreensIndex(root)
	require.NoError(t, err)
	assert.Nil(t, findFinding(t, findings, ruleScreenRuntimeHash), "intact runtime verifies: %+v", findings)

	// Hand-edit the runtime: hard error naming the recorded digest.
	edited := strings.Replace(runtimeTemplateText(t), "var VERSION = '1';", "var VERSION = '1';\n// tweaked", 1)
	require.NoError(t, os.WriteFile(filepath.Join(root, DirName, RuntimeSubdir, RuntimeFilename), []byte(edited), 0o644))
	findings, err = ValidateScreensIndex(root)
	require.NoError(t, err)
	hit := findFinding(t, findings, ruleScreenRuntimeHash)
	require.NotNil(t, hit)
	assert.Equal(t, SeverityError, hit.Severity)
	assert.Contains(t, hit.Message, "fnv1a64:")

	// Truncate the header away: hard error (no parsable hash).
	require.NoError(t, os.WriteFile(filepath.Join(root, DirName, RuntimeSubdir, RuntimeFilename), []byte("// stripped\n"), 0o644))
	findings, err = ValidateScreensIndex(root)
	require.NoError(t, err)
	hit = findFinding(t, findings, ruleScreenRuntimeHash)
	require.NotNil(t, hit)
	assert.Equal(t, SeverityError, hit.Severity)
	assert.Contains(t, hit.Message, "no parsable source-hash")

	// Remove the runtime: info only.
	require.NoError(t, os.Remove(filepath.Join(root, DirName, RuntimeSubdir, RuntimeFilename)))
	findings, err = ValidateScreensIndex(root)
	require.NoError(t, err)
	hit = findFinding(t, findings, ruleScreenRuntimeMissing)
	require.NotNil(t, hit)
	assert.Equal(t, SeverityInfo, hit.Severity)

	// No screens at all: the runtime check contributes nothing.
	empty := t.TempDir()
	findings, err = ValidateScreensIndex(empty)
	require.NoError(t, err)
	assert.Empty(t, findings)
}

// TestValidateScreensIndex_Drift pins rule (d): a missing index, a
// hand-edited (provenance-lying) index, a stale index, and an unparsable one
// are each a hard error with the regenerate remedy; a current index is clean.
func TestValidateScreensIndex_Drift(t *testing.T) {
	root := t.TempDir()
	writeIndexTree(t, root)

	findings, err := ValidateScreensIndex(root)
	require.NoError(t, err)
	hit := findFinding(t, findings, ruleScreenIndexDrift)
	require.NotNil(t, hit, "a screens tier with no index must drift")
	assert.Equal(t, SeverityError, hit.Severity)
	assert.Contains(t, hit.Message, "design_export_tokens targets:screens")

	writeScreensKitArtifacts(t, root)
	findings, err = ValidateScreensIndex(root)
	require.NoError(t, err)
	assert.Nil(t, findFinding(t, findings, ruleScreenIndexDrift), "a fresh index must be clean: %+v", findings)

	// Hand-edit a value while keeping the recorded hash: the graph comparison
	// still catches the provenance lie.
	indexPath := filepath.Join(root, DirName, GeneratedSubdir, ScreensIndexFilename)
	data, err := os.ReadFile(indexPath)
	require.NoError(t, err)
	lied := strings.Replace(string(data), `"device": "phone"`, `"device": "desktop"`, 1)
	require.NoError(t, os.WriteFile(indexPath, []byte(lied), 0o644))
	findings, err = ValidateScreensIndex(root)
	require.NoError(t, err)
	hit = findFinding(t, findings, ruleScreenIndexDrift)
	require.NotNil(t, hit, "a hand-edited index must drift even with a matching recorded hash")
	assert.Equal(t, SeverityError, hit.Severity)

	// Stale: a screen changes after the index was written.
	require.NoError(t, os.WriteFile(indexPath, data, 0o644))
	grown := strings.Replace(indexDetailDoc, `data-states="ready"`, `data-states="ready,loading"`, 1)
	require.NoError(t, os.WriteFile(filepath.Join(root, DirName, "screens", "detail.html"), []byte(grown), 0o644))
	findings, err = ValidateScreensIndex(root)
	require.NoError(t, err)
	hit = findFinding(t, findings, ruleScreenIndexDrift)
	require.NotNil(t, hit, "a screen edit must stale the index")
	assert.Contains(t, hit.Message, "stale or hand-edited")

	// Unparsable JSON: error naming the fault.
	require.NoError(t, os.WriteFile(indexPath, []byte("{not json"), 0o644))
	findings, err = ValidateScreensIndex(root)
	require.NoError(t, err)
	hit = findFinding(t, findings, ruleScreenIndexDrift)
	require.NotNil(t, hit)
	assert.Contains(t, hit.Message, "does not parse")

	// An empty screens tier: no findings at all (old trees stay clean).
	empty := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(empty, DirName, "screens"), 0o755))
	findings, err = ValidateScreensIndex(empty)
	require.NoError(t, err)
	assert.Empty(t, findings)
}

// TestValidateTree_IndexAndRuntimeWired pins the whole-tree wiring: a
// contract-complete kit is clean; the missing runtime is info; the drift
// error surfaces tree-wide.
func TestValidateTree_IndexAndRuntimeWired(t *testing.T) {
	root := t.TempDir()
	writeIndexTree(t, root)
	writeScreensKitArtifacts(t, root)

	findings, err := ValidateTree(root)
	require.NoError(t, err)
	assert.Empty(t, dropDeprecationFindings(findings), "a contract-complete screen kit must validate clean: %+v", findings)

	require.NoError(t, os.Remove(filepath.Join(root, DirName, RuntimeSubdir, RuntimeFilename)))
	findings, err = ValidateTree(root)
	require.NoError(t, err)
	hit := findFinding(t, dropDeprecationFindings(findings), ruleScreenRuntimeMissing)
	require.NotNil(t, hit)
	assert.Equal(t, SeverityInfo, hit.Severity)

	// Full scaffold + one screen with no index: the drift error surfaces
	// through the whole-tree run.
	scaffolded := t.TempDir()
	require.NoError(t, Scaffold(scaffolded))
	require.NoError(t, os.WriteFile(filepath.Join(scaffolded, DirName, "screens", "solo.html"),
		[]byte("<!doctype html><html><body><p>solo</p></body></html>"), 0o644))
	findings, err = ValidateTree(scaffolded)
	require.NoError(t, err)
	require.NotNil(t, findFinding(t, findings, ruleScreenIndexDrift))
}

// TestValidateFile_RuntimeAndIndexRegistered pins the single-file dispatch:
// every runtime asset is a recognized design asset, the runtime file runs
// the hash check, and screens.json runs the drift check.
func TestValidateFile_RuntimeAndIndexRegistered(t *testing.T) {
	root := t.TempDir()
	writeIndexTree(t, root)
	writeScreensKitArtifacts(t, root)

	findings, err := ValidateFile(root, "design/runtime/sprout-screens.js")
	require.NoError(t, err)
	assert.Empty(t, findings)

	edited := strings.Replace(runtimeTemplateText(t), "var VERSION = '1';", "var VERSION = '2';", 1)
	require.NoError(t, os.WriteFile(filepath.Join(root, DirName, RuntimeSubdir, RuntimeFilename), []byte(edited), 0o644))
	findings, err = ValidateFile(root, "design/runtime/sprout-screens.js")
	require.NoError(t, err)
	require.NotNil(t, findFinding(t, findings, ruleScreenRuntimeHash))
	require.NoError(t, os.WriteFile(filepath.Join(root, DirName, RuntimeSubdir, RuntimeFilename), []byte(runtimeTemplateText(t)), 0o644))

	for _, rel := range []string{"design/runtime/chrome.css", "design/runtime/base/phone.html", "design/runtime/base/desktop.html"} {
		findings, err := ValidateFile(root, rel)
		require.NoError(t, err, rel)
		assert.Empty(t, findings, rel)
	}

	findings, err = ValidateFile(root, "design/generated/screens.json")
	require.NoError(t, err)
	assert.Empty(t, findings)

	stale := strings.Replace(indexDetailDoc, `data-states="ready"`, `data-states="ready,loading"`, 1)
	require.NoError(t, os.WriteFile(filepath.Join(root, DirName, "screens", "detail.html"), []byte(stale), 0o644))
	findings, err = ValidateFile(root, "design/generated/screens.json")
	require.NoError(t, err)
	require.NotNil(t, findFinding(t, findings, ruleScreenIndexDrift))
}

// TestScreensTierInventoryListsRuntime pins the inventory listing: the
// runtime tier rows carry kind "runtime" (design_assets lists the tier); a
// tree without the tier lists none.
func TestScreensTierInventoryListsRuntime(t *testing.T) {
	root := t.TempDir()
	writeIndexTree(t, root)
	require.NoError(t, ScaffoldRuntimeAssets(filepath.Join(root, DirName)))
	assert.True(t, screensTierHasScreens(root))

	inv, err := Scan(root)
	require.NoError(t, err)
	var runtimeRows []AssetRow
	for _, row := range inv.Assets {
		if row.Kind == KindRuntime {
			runtimeRows = append(runtimeRows, row)
		}
	}
	require.Len(t, runtimeRows, 4, "runtime js + chrome + two base documents: %+v", runtimeRows)
	for _, row := range runtimeRows {
		assert.True(t, strings.HasPrefix(row.Path, "design/runtime/"), row.Path)
	}

	bare := t.TempDir()
	writeIndexTree(t, bare)
	inv, err = Scan(bare)
	require.NoError(t, err)
	for _, row := range inv.Assets {
		assert.NotEqual(t, KindRuntime, row.Kind)
	}
}

// TestRenderScreensIndexArtifact pins the export-tool artifact: derived
// bytes carry the canonical RelPath, a content hash, and refuse an empty
// tier.
func TestRenderScreensIndexArtifact(t *testing.T) {
	root := t.TempDir()
	writeIndexTree(t, root)

	artifact, err := RenderScreensIndexArtifact(root)
	require.NoError(t, err)
	assert.Equal(t, ExportTargetScreens, artifact.Target)
	assert.Equal(t, "design/generated/screens.json", artifact.RelPath)
	assert.Regexp(t, `^fnv1a64:[0-9a-f]{16}$`, artifact.Hash)
	assert.Contains(t, string(artifact.Content), `"stem": "home"`)

	root2 := t.TempDir()
	writeIndexTree(t, root2)
	artifact2, err := RenderScreensIndexArtifact(root2)
	require.NoError(t, err)
	assert.Equal(t, artifact.Hash, artifact2.Hash)

	empty := t.TempDir()
	_, err = RenderScreensIndexArtifact(empty)
	require.ErrorIs(t, err, ErrNoScreensForIndex)
}

// TestResolveExportTargets_ScreensTarget pins the target surface: `screens`
// resolves alone, rides last in a mixed list, and is never part of `all`.
func TestResolveExportTargets_ScreensTarget(t *testing.T) {
	targets, err := ResolveExportTargets("screens")
	require.NoError(t, err)
	require.Len(t, targets, 1)
	assert.Equal(t, ExportTargetScreens, targets[0].Name)
	assert.Equal(t, ScreensIndexFilename, targets[0].File)

	targets, err = ResolveExportTargets("css, screens")
	require.NoError(t, err)
	require.Len(t, targets, 2)
	assert.Equal(t, "css", targets[0].Name, "token targets stay in canonical order")
	assert.Equal(t, ExportTargetScreens, targets[1].Name, "screens rides last")

	targets, err = ResolveExportTargets("all")
	require.NoError(t, err)
	for _, target := range targets {
		assert.NotEqual(t, ExportTargetScreens, target.Name, "all never rewrites the screen graph")
	}

	_, err = ResolveExportTargets("nope")
	require.ErrorContains(t, err, "unknown export target")
}

// findingsFor collects the findings carrying rule (rule-test convenience).
func findingsFor(t *testing.T, findings []Finding, rule string) []Finding {
	t.Helper()
	var out []Finding
	for _, f := range findings {
		if f.Rule == rule {
			out = append(out, f)
		}
	}
	return out
}
