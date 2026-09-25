package design

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runtimeAssetOnDisk reads the scaffolded copy of a runtime asset out of a
// scaffolded tree (slash-separated rel, e.g. "base/phone.html").
func runtimeAssetOnDisk(t *testing.T, root, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, DirName, RuntimeSubdir, filepath.FromSlash(rel)))
	require.NoError(t, err)
	return string(data)
}

// TestScaffoldCopiesRuntimeAssets pins SP-143 §143.2: a fresh scaffold
// carries the fixed screen-kit assets into design/runtime/ byte-identical to
// the embedded templates — the copy is the delivery mechanism for versioned,
// tool-owned assets, so fidelity is the contract.
func TestScaffoldCopiesRuntimeAssets(t *testing.T) {
	root := t.TempDir()

	require.NoError(t, Scaffold(root))

	require.Len(t, RuntimeAssets, 3, "chrome + two base documents")
	for _, name := range RuntimeAssets {
		embedded, err := runtimeTemplates.ReadFile(runtimeTemplateDir + name)
		require.NoError(t, err)
		assert.Equal(t, string(embedded), runtimeAssetOnDisk(t, root, name),
			"scaffolded %s must be byte-identical to the embedded template", name)
		assert.FileExists(t, filepath.Join(root, DirName, RuntimeSubdir, filepath.FromSlash(name)))
	}
}

// TestScaffoldRuntimeAssetsIdempotent pins the overwrite-free posture: a
// second scaffold over an existing tree is a no-op for the runtime assets —
// a tree that pinned (hand-modified) its copy must not be reverted by a
// re-scaffold.
func TestScaffoldRuntimeAssetsIdempotent(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, Scaffold(root))

	pinned := "/* pinned by the project — the scaffold must not fight this */\n"
	chrome := filepath.Join(root, DirName, RuntimeSubdir, "chrome.css")
	require.NoError(t, os.WriteFile(chrome, []byte(pinned), 0o644))

	// ScaffoldRuntimeAssets alone is the re-runnable surface (Scaffold is
	// manifest-once): a brownfield re-run must leave pinned assets alone.
	require.NoError(t, ScaffoldRuntimeAssets(filepath.Join(root, DirName)))

	assert.Equal(t, pinned, runtimeAssetOnDisk(t, root, "chrome.css"),
		"re-scaffold must never overwrite an existing runtime asset")
}

// TestRuntimeBaseTemplatesCarryTheContract pins the data-attribute +
// reference contract both base documents ship with (SP-143 §143.2): the
// runtime and tokens references, the version stamp, the body authoring hook,
// and the declared-states attribute. The phone template additionally carries
// data-device="phone", the chrome.css reference, and the frame variables.
func TestRuntimeBaseTemplatesCarryTheContract(t *testing.T) {
	sharedNeedles := []string{
		`<script src="../runtime/sprout-screens.js" defer></script>`,
		`<link rel="stylesheet" href="../generated/tokens.css">`,
		`data-sprout-screens=`,
		`data-states=`,
		`data-sprout-screen=`,
	}
	for _, name := range RuntimeAssets {
		if !strings.HasPrefix(name, "base/") {
			continue
		}
		data, err := runtimeTemplates.ReadFile(runtimeTemplateDir + name)
		require.NoError(t, err)
		for _, needle := range sharedNeedles {
			assert.Contains(t, string(data), needle, "%s must carry %q", name, needle)
		}
	}

	phone, err := runtimeTemplates.ReadFile(runtimeTemplateDir + "base/phone.html")
	require.NoError(t, err)
	for _, needle := range []string{
		`data-device="phone"`,
		`<link rel="stylesheet" href="../runtime/chrome.css">`,
		`--sprout-frame-width`,
		`--sprout-frame-height`,
		`<div data-sprout-home></div>`,
	} {
		assert.Contains(t, string(phone), needle, "phone template must carry %q", needle)
	}

	// The desktop document is the no-chrome default: no data-device attribute
	// and no chrome stylesheet link — chrome.css does nothing to it by
	// contract. (Its comment may still *name* the contract.)
	desktop, err := runtimeTemplates.ReadFile(runtimeTemplateDir + "base/desktop.html")
	require.NoError(t, err)
	assert.NotContains(t, string(desktop), `data-device=`,
		"desktop template must not carry a data-device attribute")
	assert.NotContains(t, string(desktop), `href="../runtime/chrome.css"`,
		"desktop template must not reference chrome.css")
}

// TestChromeTargetsPhoneOnly pins the chrome.css selector contract: every
// rule is scoped under html[data-device="phone"] (fixed hardware realism,
// the default layout stays unframed), so a desktop screen's styles are
// untouched by the chrome sheet.
func TestChromeTargetsPhoneOnly(t *testing.T) {
	data, err := runtimeTemplates.ReadFile(runtimeTemplateDir + "chrome.css")
	require.NoError(t, err)

	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "html") {
			continue
		}
		assert.True(t, strings.HasPrefix(trimmed, `html[data-device="phone"]`),
			"chrome.css rule must be scoped to html[data-device=\"phone\"], got: %s", trimmed)
	}
}

// TestRuntimeAssetsStayDesignValidateClean pins the tree boundary the
// design.go comment declares: runtime/ is scaffolded tool-owned content, not
// an authoring tier — a tree containing only the scaffolded runtime kit
// (no screens yet) validates with zero error findings, so shipping the kit
// can never dirty a workspace.
func TestRuntimeAssetsStayDesignValidateClean(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, Scaffold(root))

	findings, err := ValidateTree(root)
	require.NoError(t, err)
	for _, f := range findings {
		assert.NotEqual(t, SeverityError, f.Severity,
			"runtime assets must not trip the tree validator: %s %s", f.Rule, f.Message)
	}
}
