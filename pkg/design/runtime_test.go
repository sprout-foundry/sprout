package design

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runtimeRecordedSourceHashRe extracts the digest from the runtime's
// `source-hash: fnv1a64:<hex>` header line.
var runtimeRecordedSourceHashRe = regexp.MustCompile(`source-hash: (fnv1a64:[0-9a-f]{16})`)

// runtimeSourceHashLineRe matches the whole hash line value for the
// self-zeroing recompute (the digest is replaced with the zero digest
// before hashing, removing the circularity of hashing a file that
// contains its own hash).
var runtimeSourceHashLineRe = regexp.MustCompile(`source-hash: fnv1a64:[0-9a-f]{16}`)

// runtimeAssetOnDisk reads the scaffolded copy of a runtime asset out of a
// scaffolded tree (slash-separated rel, e.g. "base/phone.html").
func runtimeAssetOnDisk(t *testing.T, root, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, DirName, RuntimeSubdir, filepath.FromSlash(rel)))
	require.NoError(t, err)
	return string(data)
}

// runtimeSelfZeroedHash recomputes the screen runtime's source-hash the way
// the file's header documents: fnv1a64 over the runtime's bytes with the 16
// hex digits of the source-hash line itself replaced by the zero digest —
// the SP-140-5 §5f offline-recompute convention adapted to a fixed asset, so
// a hand-edited copy is detectable at any checkout without trusting the
// recorded digest. The zero placeholder must match the one used when the
// header was authored.
func runtimeSelfZeroedHash(content []byte) string {
	zeroed := runtimeSourceHashLineRe.ReplaceAll(content,
		[]byte("source-hash: fnv1a64:0000000000000000"))
	return tokenExportHash(zeroed)
}

// TestSproutScreensRuntimeStamps pins the runtime's identity stamps
// (SP-143 §143.3): a self-consistent self-zeroing source-hash, the version
// line, and the version the runtime stamps onto <html data-sprout-screens>.
func TestSproutScreensRuntimeStamps(t *testing.T) {
	data, err := runtimeTemplates.ReadFile(runtimeTemplateDir + "sprout-screens.js")
	require.NoError(t, err)
	text := string(data)

	recorded := runtimeRecordedSourceHashRe.FindStringSubmatch(text)
	require.Len(t, recorded, 2, "runtime must carry a source-hash: fnv1a64:<hex> header line")
	assert.Equal(t, recorded[1], runtimeSelfZeroedHash(data),
		"runtime source-hash must match the self-zeroing recompute — if the "+
			"runtime changed, recompute the header hash (zero the hex digits, "+
			"fnv1a64 the bytes, write the digest back)")

	assert.Regexp(t, `(?m)^\s*\*\s+version: 1$`, text, "runtime header must declare its version")
	assert.Contains(t, text, `var VERSION = '1'`,
		"runtime must carry the version it stamps onto <html data-sprout-screens>")
}

// TestSproutScreensRuntimeIsClassicScript pins the no-ESM rule (SP-143
// Premise §2): file:// pages run under a null origin where ES modules are
// CORS-blocked, so the runtime must stay a classic script with zero
// imports/exports/dynamic-imports; its single network act is the one fetch
// a swap performs.
func TestSproutScreensRuntimeIsClassicScript(t *testing.T) {
	data, err := runtimeTemplates.ReadFile(runtimeTemplateDir + "sprout-screens.js")
	require.NoError(t, err)
	text := string(data)

	for _, banned := range []string{"import ", "export ", "import(", "require(", "XMLHttpRequest"} {
		assert.NotContains(t, text, banned, "runtime must not use %q", banned)
	}
	assert.LessOrEqual(t, strings.Count(text, "fetch("), 1,
		"the swap's fetch is the runtime's only network act")
}

// TestSproutScreensRuntimeSurface pins the behavioral surface the SP-143
// acceptance criteria exercise: delegated data-nav interception, in-place
// swap + pushState/popstate, the #state= hash contract, the preview-gated
// switcher, and the window.SproutScreens API — the markers 143.7's E2E and
// the 143.4 preview rely on, so a refactor that renames them out fails here,
// not in a browser.
func TestSproutScreensRuntimeSurface(t *testing.T) {
	data, err := runtimeTemplates.ReadFile(runtimeTemplateDir + "sprout-screens.js")
	require.NoError(t, err)
	text := string(data)

	for label, needle := range map[string]string{
		"nav attribute parse":     `to:`,
		"delegated click":         "addEventListener('click', onClick",
		"history integration":     "history.pushState",
		"back/forward":            "popstate",
		"state hash contract":     "#state=",
		"preview gate":            "data-sprout-preview",
		"state sections":          "[data-state]",
		"switcher element":        "sprout-state-switcher",
		"api export":              "window.SproutScreens",
		"nav API":                 "nav: nav",
		"setState API":            "setState: setState",
		"in-place swap":           "replaceWith",
		"standalone fallback":     "standaloneFallback",
		"version stamp on html":   `setAttribute('data-sprout-screens'`,
		"source-hash stamp doc":   "self-zeroing recipe",
		"screen URL from script":  "sprout-screens\\.js",
		"screens dir derivation":  `'../screens/'`,
		"never full reload first": "preventDefault",
	} {
		assert.Contains(t, text, needle, "runtime must keep: %s", label)
	}
}

// TestScaffoldCopiesRuntimeAssets pins SP-143 §143.2: a fresh scaffold
// carries the fixed screen-kit assets into design/runtime/ byte-identical to
// the embedded templates — the copy is the delivery mechanism for versioned,
// tool-owned assets, so fidelity is the contract.
func TestScaffoldCopiesRuntimeAssets(t *testing.T) {
	root := t.TempDir()

	require.NoError(t, Scaffold(root))

	require.Len(t, RuntimeAssets, 4, "runtime js + chrome + two base documents")
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
