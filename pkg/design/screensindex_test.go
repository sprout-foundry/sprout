package design

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// SP-143 §143.5 — the screens.json derived index generator (the pure half):
// attribute derivation, deterministic rendering, and the §5f input hash.
// The validator rules live in screensindex_rules_test.go.
// ---------------------------------------------------------------------------

// indexScreenDoc is a minimal runtime-era screen: device, declared states,
// and two data-nav anchors with triggers.
const indexScreenDoc = `<!doctype html>
<html lang="en" data-device="phone" data-states="empty,error,ready">
<head><title>Home</title>
<style>.screen { width: 393px; }</style>
</head>
<body>
  <div class="screen">
    <a data-nav="to:detail;trigger:tap item">detail</a>
    <a data-nav="to:home;trigger:tap logo">home</a>
    <section data-state="empty">empty</section>
    <section data-state="error">error</section>
  </div>
</body>
</html>
`

// indexDetailDoc is the nav target: same device, back edge, one state.
const indexDetailDoc = `<!doctype html>
<html data-device="phone" data-states="ready">
<head><title>Detail</title></head>
<body>
  <a data-nav="to:home;trigger:tap back">back</a>
  <section data-state="ready">ready</section>
</body>
</html>
`

// writeIndexTree writes a contract-complete small tree: two screens with
// wireframe counterparts, README frames + status markers, and the §1h git
// contract — so a whole-tree run is clean once the index/runtime artifacts
// are added via writeScreensKitArtifacts.
func writeIndexTree(t *testing.T, root string) {
	t.Helper()
	write := func(rel, content string) {
		path := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
	write("design/README.md", "# Design\n\nStatus markers: draft, review, ready.\n\nframes:\n  phone: 393x852\n\n## Screens\n\n- `home` — draft — the kit test home screen\n- `detail` — draft — the kit test detail screen\n")
	write("design/wireframes/home.svg", `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 393 852"><text>home</text></svg>`)
	write("design/wireframes/detail.svg", `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 393 852"><text>detail</text></svg>`)
	write("design/screens/home.html", indexScreenDoc)
	write("design/screens/detail.html", indexDetailDoc)
	write(GitContractFile, GitAttributesDiffHTMLLine+"\n")
	write(GitIgnoreFile, GitIgnoreCacheLine+"\n")
}

// runtimeTemplateText reads the embedded runtime template (the scaffold
// source) for hand-edit fixtures.
func runtimeTemplateText(t *testing.T) string {
	t.Helper()
	data, err := runtimeTemplates.ReadFile(runtimeTemplateDir + RuntimeFilename)
	require.NoError(t, err)
	return string(data)
}

// writeScreensKitArtifacts completes the SP-143 §143.5 screen contract for a
// fixture tree that already carries design/screens/*.html: the derived
// screens.json (rendered from those exact bytes, so it is drift-free) and the
// fixed runtime asset (byte-identical to the embedded template, so its hash
// verifies). Shared by the whole-tree and umbrella fixtures.
func writeScreensKitArtifacts(t *testing.T, root string) {
	t.Helper()
	require.NoError(t, ScaffoldRuntimeAssets(filepath.Join(root, DirName)))
	doc, err := DeriveScreensIndex(root)
	require.NoError(t, err)
	content, err := RenderScreensIndex(doc)
	require.NoError(t, err)
	path := filepath.Join(root, DirName, GeneratedSubdir, ScreensIndexFilename)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, content, 0o644))
}

// findFinding returns the first finding carrying rule, or nil.
func findFinding(t *testing.T, findings []Finding, rule string) *Finding {
	t.Helper()
	for i := range findings {
		if findings[i].Rule == rule {
			return &findings[i]
		}
	}
	return nil
}

// TestParseScreenHTML_DerivesAttributes pins the derivation contract: device,
// declared states (order + dedup), nav edges (sorted, deduped, trigger kept),
// and the declared-frame width.
func TestParseScreenHTML_DerivesAttributes(t *testing.T) {
	entry := ParseScreenHTML([]byte(indexScreenDoc), []Frame{{Name: "phone", Width: 393, Height: 852}})

	assert.Equal(t, "phone", entry.Device)
	assert.Equal(t, []string{"empty", "error", "ready"}, entry.States)
	assert.Equal(t, []ScreenIndexNav{
		{To: "detail", Trigger: "tap item"},
		{To: "home", Trigger: "tap logo"},
	}, entry.Nav, "nav edges keep their triggers")
	assert.Equal(t, 393, entry.Frame, "a container width matching a declared frame is recorded")
}

func TestParseScreenHTML_DefaultsAndDedup(t *testing.T) {
	doc := `<!doctype html>
<html data-states="ready, ready ,draft">
<body>
  <a data-nav="to:detail">go</a>
  <a data-nav="to:detail">go again</a>
  <a data-nav="to:detail;trigger:tap row">row</a>
  <section data-state="ready">r</section>
</body>
</html>`
	entry := ParseScreenHTML([]byte(doc), nil)
	assert.Equal(t, "", entry.Device, "no data-device: the desktop default")
	assert.Equal(t, []string{"ready", "draft"}, entry.States, "states dedup, declaration order kept")
	assert.Equal(t, 0, entry.Frame, "no frames declared: 0")
	assert.Equal(t, []ScreenIndexNav{
		{To: "detail", Trigger: ""},
		{To: "detail", Trigger: "tap row"},
	}, entry.Nav, "identical edges dedup; distinct triggers stay")
}

// TestParseScreenHTML_DeclarationNotASection pins the data-states vs
// data-state distinction: the declaration attribute never surfaces as a used
// state, and the html-element scan sees it only as the declaration.
func TestParseScreenHTML_DeclarationNotASection(t *testing.T) {
	entry := ParseScreenHTML([]byte(indexScreenDoc), nil)
	assert.Equal(t, []string{"empty", "error", "ready"}, entry.States)
	assert.NotContains(t, entry.States, "empty,error,ready")
}

// TestRenderScreensIndex_Deterministic pins byte-identical rendering across
// repeated derivations of the same screen bytes (the drift check's
// foundation).
func TestRenderScreensIndex_Deterministic(t *testing.T) {
	root := t.TempDir()
	writeIndexTree(t, root)

	first, err := DeriveScreensIndex(root)
	require.NoError(t, err)
	one, err := RenderScreensIndex(first)
	require.NoError(t, err)

	// Re-derive in a fresh tree (different directory, different mtimes): the
	// bytes must not move.
	root2 := t.TempDir()
	writeIndexTree(t, root2)
	second, err := DeriveScreensIndex(root2)
	require.NoError(t, err)
	two, err := RenderScreensIndex(second)
	require.NoError(t, err)
	assert.Equal(t, string(one), string(two))

	// Twenty in-place re-derivations, still identical.
	for i := 0; i < 20; i++ {
		again, err := DeriveScreensIndex(root)
		require.NoError(t, err)
		three, err := RenderScreensIndex(again)
		require.NoError(t, err)
		require.Equal(t, string(one), string(three), "run %d", i)
	}

	// The provenance banner carries the required facts.
	assert.Contains(t, string(one), `"source-hash": "fnv1a64:`)
	assert.Contains(t, string(one), "Do not edit by hand")
	assert.Contains(t, string(one), `"stem": "detail"`)
	assert.Contains(t, string(one), `"trigger": "tap item"`)
	assert.Contains(t, string(one), `"device": "phone"`)
	assert.Contains(t, string(one), `"format-version": 1`)
}

// TestScreensIndexInputHash_PinStemsOrder pins the §5f recompute recipe: the
// hash folds the screen bytes in stem order (the TokenExportInputHash shape),
// so a consumer can verify offline by concat + fnv1a64.
func TestScreensIndexInputHash_PinStemsOrder(t *testing.T) {
	sources := []TokenExportSource{
		{Name: "home", Content: []byte("home-bytes")},
		{Name: "detail", Content: []byte("detail-bytes")},
	}
	ordered := append([]TokenExportSource(nil), sources...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Name < ordered[j].Name })
	assert.Equal(t, TokenExportInputHash(ordered), screensIndexInputHash(sources))

	// Read order cannot move the hash.
	reversed := []TokenExportSource{sources[1], sources[0]}
	assert.Equal(t, screensIndexInputHash(sources), screensIndexInputHash(reversed))

	// A changed screen byte moves the hash.
	bumped := []TokenExportSource{
		{Name: "home", Content: []byte("home-bytes!")},
		{Name: "detail", Content: []byte("detail-bytes")},
	}
	assert.NotEqual(t, screensIndexInputHash(sources), screensIndexInputHash(bumped))

	// File partitions cannot concatenate to the same stream (the len fold).
	split := []TokenExportSource{
		{Name: "home", Content: []byte("home-")},
		{Name: "detail", Content: []byte("bytes")},
	}
	assert.NotEqual(t, screensIndexInputHash(sources), screensIndexInputHash(split))
}

// TestScreensIndexSources pin the read shape: sorted by stem, names without
// the extension, workspace-relative paths.
func TestScreensIndexSources(t *testing.T) {
	root := t.TempDir()
	writeIndexTree(t, root)

	sources, err := ScreensIndexSources(root)
	require.NoError(t, err)
	require.Len(t, sources, 2)
	assert.Equal(t, "detail", sources[0].Name, "stem order, not glob accident")
	assert.Equal(t, "design/screens/detail.html", sources[0].Path)
	assert.Equal(t, indexDetailDoc, string(sources[0].Content))

	empty := t.TempDir()
	sources, err = ScreensIndexSources(empty)
	require.NoError(t, err)
	assert.Empty(t, sources)
}

// TestParseScreenHTML_IndexRoundTrip pins the JSON round trip: the rendered
// artifact parses back into the same graph.
func TestParseScreenHTML_IndexRoundTrip(t *testing.T) {
	root := t.TempDir()
	writeIndexTree(t, root)
	doc, err := DeriveScreensIndex(root)
	require.NoError(t, err)
	content, err := RenderScreensIndex(doc)
	require.NoError(t, err)

	parsed, err := ReadScreensIndex(content)
	require.NoError(t, err)
	assert.True(t, screensIndexGraphEqual(doc.Screens, parsed.Screens))
	require.Len(t, parsed.Screens, 2)
	assert.Equal(t, "detail", parsed.Screens[0].Stem, "screens are sorted by stem")
	assert.Equal(t, []ScreenIndexNav{{To: "home", Trigger: "tap back"}}, parsed.Screens[0].Nav)
	assert.Equal(t, []string{"ready"}, parsed.Screens[0].States)
	assert.Equal(t, "phone", parsed.Screens[0].Device)

	// A graph mutation is visible to the comparator (the drift check's teeth).
	parsed.Screens[0].Device = "desktop"
	assert.False(t, screensIndexGraphEqual(doc.Screens, parsed.Screens))
}

// TestScreenIndexEntry_Frame pins the frame derivation against the README
// frames: block (ParseFrames input).
func TestScreenIndexEntry_Frame(t *testing.T) {
	frames, err := ParseFrames("frames:\n  phone: 393x852\n  desktop: 1440x900\n")
	require.NoError(t, err)
	require.Len(t, frames, 2)

	entry := ParseScreenHTML([]byte(indexScreenDoc), frames)
	assert.Equal(t, 393, entry.Frame)

	noMatch := strings.Replace(indexScreenDoc, "393px", "500px", 1)
	entry = ParseScreenHTML([]byte(noMatch), frames)
	assert.Equal(t, 0, entry.Frame, "a width matching no declared frame leaves frame 0")
}
