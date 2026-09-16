package design

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestScanValidTree covers the SP-140-2 §2c inventory contract on a tree that
// satisfies every convention: manifest summary, per-asset rows, token group
// counts, flow node/edge counts, and (empty) findings.
func TestScanValidTree(t *testing.T) {
	root := t.TempDir()
	writeValidDesignTree(t, root)

	inv, err := Scan(root)
	require.NoError(t, err)
	require.NotNil(t, inv)

	// Manifest summary: exists, frames parsed, status markers detected.
	assert.True(t, inv.Manifest.Exists)
	assert.Equal(t, []Frame{
		{Name: "desktop", Width: 1440, Height: 900},
		{Name: "mobile", Width: 390, Height: 844},
	}, inv.Manifest.Frames)
	assert.Equal(t, []string{"draft", "review", "ready"}, inv.Manifest.Status)

	// Per-asset rows: the manifest plus every canonical asset, sorted by path.
	byPath := map[string]AssetRow{}
	for _, r := range inv.Assets {
		byPath[r.Path] = r
	}
	wantKinds := map[string]string{
		"design/README.md":                KindManifest,
		"design/tokens/color.tokens.json": KindToken,
		"design/wireframes/login.svg":     KindWireframe,
		"design/wireframes/home.svg":      KindWireframe,
		"design/flows/sign-up.mmd":        KindFlow,
		"design/screens/login.html":       KindScreen,
		"design/icons/home.svg":           KindIcon,
		"design/brand/brand.md":           KindBrand,
	}
	for path, kind := range wantKinds {
		row, ok := byPath[path]
		require.True(t, ok, "expected an asset row for %s, got %#v", path, inv.Assets)
		assert.Equal(t, kind, row.Kind, "asset %s kind", path)
		assert.NotEmpty(t, row.Name, "asset %s name", path)
	}

	// Names are stems, stripped of known design extensions.
	assert.Equal(t, "login", byPath["design/wireframes/login.svg"].Name)
	assert.Equal(t, "color", byPath["design/tokens/color.tokens.json"].Name)
	assert.Equal(t, "sign-up", byPath["design/flows/sign-up.mmd"].Name)

	// Manifest enrichment: the login screen is listed with a status + summary.
	login := byPath["design/screens/login.html"]
	assert.Equal(t, "draft", login.Status)
	assert.Equal(t, "sign-in entry point", login.Summary)

	assert.True(t, sort.StringsAreSorted(assetRowPaths(inv.Assets)), "asset rows must be sorted by path")

	// Token group counts: color.brand has two leaves.
	require.Len(t, inv.TokenGroups, 1)
	assert.Equal(t, TokenGroupCount{Group: "color", Tokens: 2}, inv.TokenGroups[0])

	// Flow node/edge counts: login -> home is 2 nodes, 1 edge.
	require.Len(t, inv.Flows, 1)
	assert.Equal(t, "design/flows/sign-up.mmd", inv.Flows[0].Path)
	assert.Equal(t, "sign-up", inv.Flows[0].Name)
	assert.Equal(t, 2, inv.Flows[0].Nodes)
	assert.Equal(t, 1, inv.Flows[0].Edges)

	// Findings: the valid tree validates clean.
	assert.Empty(t, inv.Findings)
	for _, sev := range []string{"error", "warn", "info", "fix"} {
		assert.Equal(t, 0, inv.BySeverity[sev], "severity %s", sev)
	}
}

func TestScanMissingDesignDir(t *testing.T) {
	root := t.TempDir()

	inv, err := Scan(root)
	require.NoError(t, err)
	require.NotNil(t, inv)
	assert.False(t, inv.Manifest.Exists)
	assert.Empty(t, inv.Manifest.Frames)
	assert.Empty(t, inv.Assets)
	assert.Empty(t, inv.TokenGroups)
	assert.Empty(t, inv.Flows)
	assert.Empty(t, inv.Findings)
}

func TestScanCountsFindingsBySeverity(t *testing.T) {
	root := t.TempDir()
	writeValidDesignTree(t, root)
	// An error (bad token) and a warn (raw hex brand) simultaneously.
	seedFixture(t, root, "design/tokens/bad.tokens.json",
		`{"a": {"$value": "{nope.missing}", "$type": "color"}}`)
	seedFixture(t, root, "design/brand/brand.md", "Primary is #ff0000; use {color.brand.primary}.\n")

	inv, err := Scan(root)
	require.NoError(t, err)
	assert.Equal(t, 1, inv.BySeverity["error"])
	assert.Equal(t, 1, inv.BySeverity["warn"])
	assert.Len(t, inv.Findings, 2)
}

func TestScanTokenGroupCountsAcrossFiles(t *testing.T) {
	root := t.TempDir()
	writeValidDesignTree(t, root)
	// A second file in the same top-level group must sum into one row; a new
	// group gets its own row. Counts are leaf tokens, nested groups included.
	seedFixture(t, root, "design/tokens/spacing.tokens.json", `{
  "spacing": {
    "sm": {"$value": "4px", "$type": "dimension"},
    "md": {"$value": "8px", "$type": "dimension"}
  }
}`)
	seedFixture(t, root, "design/tokens/color.tokens.json", `{
  "color": {
    "brand": {
      "primary": {"$value": "#0055ff", "$type": "color"},
      "secondary": {"$value": "#0033aa", "$type": "color"},
      "accent": {"$value": "#ffaa00", "$type": "color"}
    }
  }
}`)
	// A malformed file contributes nothing and must not fail the scan.
	seedFixture(t, root, "design/tokens/broken.tokens.json", `{"oops": `)

	inv, err := Scan(root)
	require.NoError(t, err)

	got := map[string]int{}
	for _, g := range inv.TokenGroups {
		got[g.Group] = g.Tokens
	}
	assert.Equal(t, 3, got["color"])
	assert.Equal(t, 2, got["spacing"])
	assert.NotContains(t, got, "oops", "an unparsable file must not contribute a group")

	// Sorted by group name.
	assert.True(t, sort.StringsAreSorted(tokenGroupNames(inv.TokenGroups)), "token groups must be sorted by name")
}

func TestScanUnknownFilesReported(t *testing.T) {
	root := t.TempDir()
	writeValidDesignTree(t, root)
	seedFixture(t, root, "design/tokens/notes.txt", "scratch")

	inv, err := Scan(root)
	require.NoError(t, err)
	found := false
	for _, r := range inv.Assets {
		if r.Path == "design/tokens/notes.txt" {
			found = true
			assert.Equal(t, KindUnkn, r.Kind)
		}
	}
	assert.True(t, found, "an unknown file inside design/ must still be surfaced as an unknown row")
}

func TestAssetName(t *testing.T) {
	cases := map[string]string{
		"color.tokens.json": "color",
		"login.svg":         "login",
		"sign-up.mmd":       "sign-up",
		"login.html":        "login",
		"target.json":       "target",
		"brand.md":          "brand",
		"notes.txt":         "notes.txt",
	}
	for in, want := range cases {
		assert.Equal(t, want, assetName(in), "assetName(%q)", in)
	}
}

func TestParseManifestListings(t *testing.T) {
	text := "# Design\n\n## Screens\n\n" +
		"- `login` — draft — sign-in entry point\n" +
		"- `home` — ready\n" +
		"- `shop` - review - browse catalog\n" +
		"- `about` — just prose, no status\n" +
		"- not a listing\n"

	statuses, summaries := parseManifestListings(text)
	assert.Equal(t, "draft", statuses["login"])
	assert.Equal(t, "sign-in entry point", summaries["login"])
	assert.Equal(t, "ready", statuses["home"])
	assert.NotContains(t, summaries, "home")
	assert.Equal(t, "review", statuses["shop"])
	assert.Equal(t, "browse catalog", summaries["shop"])
	assert.NotContains(t, statuses, "about")
	assert.Equal(t, "just prose, no status", summaries["about"])
}

func TestIncTokenGroupCountsSkipsMalformed(t *testing.T) {
	counts := map[string]int{}
	incTokenGroupCounts([]byte(`{"a": {"$value": "{x.y}", "$type": "color"}}`), counts)
	assert.Equal(t, 1, counts["a"])

	incTokenGroupCounts([]byte(`not json`), counts)
	assert.Equal(t, 1, counts["a"], "a malformed file must not change counts")
}

func TestScanDoesNotWriteTree(t *testing.T) {
	root := t.TempDir()
	writeValidDesignTree(t, root)

	before := snapshotTree(t, root)
	_, err := Scan(root)
	require.NoError(t, err)
	after := snapshotTree(t, root)
	assert.Equal(t, before, after, "Scan must be read-only")
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func assetRowPaths(rows []AssetRow) []string {
	paths := make([]string, 0, len(rows))
	for _, r := range rows {
		paths = append(paths, r.Path)
	}
	return paths
}

func tokenGroupNames(groups []TokenGroupCount) []string {
	names := make([]string, 0, len(groups))
	for _, g := range groups {
		names = append(names, g.Group)
	}
	return names
}

func snapshotTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	require.NoError(t, filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			out[path] = string(data)
		}
		return nil
	}))
	return out
}
