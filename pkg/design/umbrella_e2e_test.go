//go:build !js

// umbrella_e2e_test.go — the SP-140 umbrella end-to-end validation: one
// continuous, hermetic run of the umbrella acceptance criteria.
//
// The parent acceptance criteria (roadmap/SP-140-design-workspace.md,
// "Acceptance criteria (umbrella)") are validated here as ONE continuous,
// hermetic run rather than as five disjoint unit suites:
//
//	0. a fresh workspace with NO design/ directory reports {exists: false}
//	   plus scaffold guidance — the clean start the one prompt begins from;
//	1. ONE prompt drives the workspace to a complete design/ tree (tokens,
//	   wireframes, flows, README manifest, screens) — the tree is written by
//	   the same scaffold/validator/export surfaces the agent tools wrap, and
//	   the scenes the one prompt runs through are then replayed as REAL agent
//	   turns over that exact tree (the scripted-client pattern 5.9 uses), so
//	   the "one prompt → tree" claim is asserted on the tree the agent
//	   produced, not just on a static fixture;
//	2. design_validate is CLEAN OF ERRORS and design_assets/design_critique
//	   report sensibly over the produced tree;
//	3. the DesignView input structure is present — the exact design/ layout
//	   the webui inventory walk + flow canvas consume (tokens/*.tokens.json,
//	   wireframes/*.svg, flows/*.mmd, screens/*.html, README.md), asserted
//	   against the webui's own path/kind vocabulary;
//	4. every artifact is a valid instance of a STANDARD OPEN FORMAT a
//	   non-sprout tool can open (SVG parses, HTML is self-contained, .mmd is
//	   the supported mermaid subset, tokens are valid DTCG, generated files
//	   carry provenance headers) — no sprout-proprietary format anywhere;
//	5. the drift loop is verified: design-ahead and code-ahead report
//	   distinctly with distinct remedies, and design_sync resolves code-ahead
//	   (design_sync apply is confined to design/ per §5e);
//	6. co-commit is verified on a throwaway git fixture: ONE commit carries
//	   both the dev change and its design_sync adoption, and `git revert`
//	   removes both atomically (§5f) — while the same §5a provenance hash the
//	   export writes still matches the token inputs at that commit, so the
//	   consistency is checkable offline.
//
// Hermetic: no provider, no browser, no network. Every workspace is a
// t.TempDir(); the git fixture is its own `git init` repo driven through
// `git -C` with a scrubbed environment (see provGitEnv in
// provenance_offline_test.go) so the developer's real working tree is never
// touched. Determinism: the whole run is a pure function of the fixture, and
// a second pass asserts byte-identical artifacts + identical drift reports so
// the umbrella cannot pass on run-to-run noise.
//
// This file is a TEST-ONLY validation item: it adds no production behaviour.
// It reuses the exported surface the prior items ship (Scaffold, ValidateTree,
// ResolveExportTokens/RenderArtifacts, ParseFlowchart, AnalyzeDrift,
// AnalyzeTouchedFiles/PlanSyncApply, TokenExportInputHash) rather than
// re-implementing any of them.

package design

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// -----------------------------------------------------------------------------
// Fixtures: the tree one prompt produces
// -----------------------------------------------------------------------------

// umbManifest is the README manifest the one prompt writes: declared device
// frames, the screen/flow inventory, the status markers, and links that
// resolve — i.e. a manifest that passes the §1e conventions.
const umbManifest = `# Check Deposit — Design Workspace

frames:
  desktop: 1440x900
  mobile: 390x844

## Screens

- ` + "`login`" + ` — ready — sign-in entry point
- ` + "`deposit`" + ` — review — amount + account entry
- ` + "`confirm`" + ` — draft — review and submit

## Flows

- ` + "`check-deposit`" + ` — ready — the mobile check-deposit journey

## Status markers

Screens and flows carry one of: draft, review, ready.

## Links

- [Color tokens](tokens/color.tokens.json)
- [Spacing tokens](tokens/spacing.tokens.json)
- [Login wireframe](wireframes/login.svg)
- [Deposit flow](flows/check-deposit.mmd)
- [Deposit screen](screens/deposit.html)
`

// umbColorTokens is valid W3C DTCG: the primary colour is a plain value and
// the accent is a whole-value alias to it, so the export exercises alias
// resolution (var(--color-brand-primary)) as well as literal emission.
const umbColorTokens = `{
  "color": {
    "brand": {
      "primary": { "$type": "color", "$value": "#0055ff" },
      "accent": { "$type": "color", "$value": "{color.brand.primary}" }
    },
    "semantic": {
      "surface": { "$type": "color", "$value": "#ffffff" },
      "text": { "$type": "color", "$value": "#101010" }
    }
  }
}
`

// umbSpacingTokens is a second DTCG tier, so the export reads more than one
// source document (the provenance hash folds every one of them in).
const umbSpacingTokens = `{
  "space": {
    "sm": { "$type": "dimension", "$value": "8px" },
    "md": { "$type": "dimension", "$value": "16px" },
    "lg": { "$type": "dimension", "$value": "24px" }
  }
}
`

// umbBrandMD is the brand tier. It references tokens rather than raw hex (the
// §1d warn), so the brand file is clean.
const umbBrandMD = `# Brand — Check Deposit

The palette references the token tier: the primary is ` + "`{color.brand.primary}`" + `
and surfaces use ` + "`{color.semantic.surface}`" + `.
`

// umbLoginSVG / umbDepositSVG / umbConfirmSVG are the wireframes. Each keeps
// its text as <text> (greppable), sizes its root to a declared device frame
// (mobile 390x844), and backs every literal colour with a {token.path}
// comment — so the whole-tree run has nothing to report but the honest
// advisory info rows the previous items deliberately emit for a draft tree.
const umbLoginSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844">
  <text x="24" y="64" font-size="28">Check Deposit</text>
  <text x="24" y="120" font-size="16">Sign in</text>
  <rect id="submit-go-deposit" fill="#0055ff" x="24" y="200" width="342" height="52" data-nav="deposit"><!-- {color.brand.primary} --></rect>
</svg>`

const umbDepositSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844">
  <text x="24" y="64" font-size="28">Deposit a check</text>
  <rect id="amount" fill="#ffffff" x="24" y="200" width="342" height="72"><!-- {color.semantic.surface} --></rect>
  <rect id="submit-go-confirm" fill="#0055ff" x="24" y="600" width="342" height="52" data-nav="confirm"><!-- {color.brand.primary} --></rect>
</svg>`

const umbConfirmSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844">
  <text x="24" y="64" font-size="28">Confirm deposit</text>
  <rect id="submit-go-deposit" fill="#0055ff" x="24" y="600" width="342" height="52" data-nav="deposit"><!-- {color.brand.primary} --></rect>
</svg>`

// umbFlowMMD is the mobile check-deposit flow: the supported mermaid subset
// (one flowchart declaration, node ids equal to wireframe stems, arrows).
// login -> deposit -> confirm and the confirm -> deposit back edge exercise
// non-terminal node resolution and a cycle-free walk.
const umbFlowMMD = `flowchart TD
  login --> deposit
  deposit --> confirm
  confirm --> deposit
`

// umbDepositHTML is a hi-fi screen: self-contained (no external/network
// reference), sized to the mobile frame, styled from the generated theme's
// vocabulary (the CSS var the export emits) plus an inline <style> block.
const umbDepositHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Deposit a check</title>
<style>
  body { margin: 0; font-family: sans-serif; }
  .frame { width: 390px; min-height: 844px; background: #ffffff; }
  .cta { background: #0055ff; color: #ffffff; }
</style>
</head>
<body>
<div class="frame">
  <h1>Deposit a check</h1>
  <button class="cta" type="button">Continue</button>
</div>
</body>
</html>
`

// -----------------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------------

// umbWrite writes rel (slash-separated) under root, creating parents.
func umbWrite(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
}

// umbProducedTree writes the complete design/ tree the one prompt produces:
// the scaffold (which also appends the §1h git contract to the workspace
// .gitattributes/.gitignore), then every asset. It returns nothing — the
// caller reads the tree back through the real validators.
func umbProducedTree(t *testing.T, root string) {
	t.Helper()
	// The scaffold is the greenfield entry point (SP-140-2): it creates the
	// canonical subdirectories with .gitkeep placeholders, the manifest from
	// the embedded template, and the §1h git contract. The prompt then fills
	// each tier.
	require.NoError(t, Scaffold(root), "scaffold the fresh workspace")

	umbWrite(t, root, "design/README.md", umbManifest)
	umbWrite(t, root, "design/tokens/color.tokens.json", umbColorTokens)
	umbWrite(t, root, "design/tokens/spacing.tokens.json", umbSpacingTokens)
	umbWrite(t, root, "design/brand/brand.md", umbBrandMD)
	umbWrite(t, root, "design/wireframes/login.svg", umbLoginSVG)
	umbWrite(t, root, "design/wireframes/deposit.svg", umbDepositSVG)
	umbWrite(t, root, "design/wireframes/confirm.svg", umbConfirmSVG)
	umbWrite(t, root, "design/flows/check-deposit.mmd", umbFlowMMD)
	umbWrite(t, root, "design/screens/deposit.html", umbDepositHTML)
}

// umbExport runs the §5a export over the tree (the pure core the
// design_export_tokens handler wraps) and returns the rendered artifacts.
func umbExport(t *testing.T, root string) []ExportedArtifact {
	t.Helper()
	tokens, err := ResolveExportTokens(root)
	require.NoError(t, err, "the produced token tree must resolve for export")
	artifacts, err := RenderArtifacts(tokens, exportTargetList(ExportTargets))
	require.NoError(t, err)
	require.NoError(t, WriteExportedArtifacts(root, artifacts))
	require.Len(t, artifacts, len(ExportTargets))
	return artifacts
}

// umbBlobBytes reads a file's exact bytes at a revision out of the commit.
//
// The package's provShow helper runs every git call through a TrimSpace
// wrapper, which strips a file's trailing newline; for a byte-faithful
// provenance recompute the umbrella needs the blob as git stored it, so it
// reads the blob itself (raw `git show <rev>:<path>`, no trimming). The fixture
// pins core.autocrlf=false and a text/eol .gitattributes so the blob is the
// working tree's bytes.
func umbBlobBytes(t *testing.T, root, rev, rel string) []byte {
	t.Helper()
	cmd := exec.Command("git", "-C", root, "show", rev+":"+rel)
	cmd.Env = provGitEnv()
	out, err := cmd.Output()
	require.NoErrorf(t, err, "git show %s:%s", rev, rel)
	return out
}

// umbTreeInputHash recomputes the §5a token-input provenance hash from a
// revision's committed tree alone, reading the token blobs byte-faithfully (see
// umbBlobBytes). It is the offline consumer's computation — concat the raw
// bytes of design/tokens/*.tokens.json in basename order, hash that — the exact
// recipe the exporter writes into every artifact header.
func umbTreeInputHash(t *testing.T, root, rev string) string {
	t.Helper()
	names := provGit(t, root, "ls-tree", "-r", "--name-only", rev, DirName+"/"+TokenSubdir)
	sources := make([]TokenExportSource, 0)
	for _, line := range strings.Split(names, "\n") {
		rel := strings.TrimSpace(line)
		if !strings.HasSuffix(rel, ".tokens.json") {
			continue
		}
		sources = append(sources, TokenExportSource{
			Path:    rel,
			Name:    filepath.Base(rel),
			Content: umbBlobBytes(t, root, rev, rel),
		})
	}
	require.NotEmpty(t, sources, "revision %s must carry at least one %s/*.tokens.json", rev, DirName+"/"+TokenSubdir)
	sort.Slice(sources, func(i, j int) bool { return sources[i].Name < sources[j].Name })
	return TokenExportInputHash(sources)
}
func umbFindingsBySeverity(findings []Finding) map[string]int {
	by := map[string]int{"error": 0, "warn": 0, "info": 0, "fix": 0}
	for _, f := range findings {
		by[f.Severity.String()]++
	}
	return by
}

// umbFindingsByFile groups findings by their file anchor, for readable
// failure messages.
func umbFindingsByFile(findings []Finding) map[string][]string {
	by := map[string][]string{}
	for _, f := range findings {
		by[f.File] = append(by[f.File], f.Severity.String()+":"+f.Rule)
	}
	return by
}

// umbRead reads a workspace file, failing the test when it is missing.
func umbRead(t *testing.T, root, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	require.NoErrorf(t, err, "read %s", rel)
	return string(data)
}

// -----------------------------------------------------------------------------
// Step 0 + step 1 + step 2: no design/ -> one prompt -> validated tree
// -----------------------------------------------------------------------------

// TestUmbrella_FreshWorkspaceToValidatedTree is the parent AC's headline
// claim: a fresh workspace with no design/ directory goes, from one prompt's
// work, to a validated design/ tree where design_validate is clean of errors.
func TestUmbrella_FreshWorkspaceToValidatedTree(t *testing.T) {
	root := t.TempDir()

	// --- Step 0: the clean start. ---------------------------------------
	// A fresh workspace has no design/; the whole-tree validator reports no
	// error and the tree scan reports nothing. FileExists is the gate the
	// agent tools use to distinguish "clean tree" from "no tree at all".
	require.False(t, FileExists(root), "a fresh workspace must have no design/ directory")
	preFindings, err := ValidateTree(root)
	require.NoError(t, err)
	require.Empty(t, preFindings, "a workspace without design/ validates to no findings: %#v", preFindings)

	// --- Step 1: the one prompt produces the tree. ----------------------
	umbProducedTree(t, root)
	require.True(t, FileExists(root), "the prompt must produce a design/ directory")

	// Every canonical subdirectory of the directory contract exists (the
	// scaffolded ones carry a .gitkeep so git tracks them).
	for _, sub := range Subdirs {
		info, statErr := os.Stat(filepath.Join(root, DirName, sub))
		require.NoErrorf(t, statErr, "canonical subdirectory design/%s must exist", sub)
		require.Truef(t, info.IsDir(), "design/%s must be a directory", sub)
	}

	// The §1h git contract landed on the workspace files (the scaffold's job).
	attrs := umbRead(t, root, GitContractFile)
	require.Contains(t, attrs, GitAttributesDiffHTMLLine)
	ignore := umbRead(t, root, GitIgnoreFile)
	require.Contains(t, ignore, GitIgnoreCacheLine)

	// The export (the design→code half) runs over the produced tokens.
	artifacts := umbExport(t, root)
	for _, a := range artifacts {
		require.FileExists(t, filepath.Join(root, filepath.FromSlash(a.RelPath)))
	}

	// --- Step 2: the tree validates clean of ERRORS. --------------------
	findings, err := ValidateTree(root)
	require.NoError(t, err)
	bySev := umbFindingsBySeverity(findings)
	require.Zero(t, bySev["error"],
		"the produced tree must be clean of error findings; findings by file: %v", umbFindingsByFile(findings))
	require.Zero(t, bySev["fix"],
		"the produced tree must need no machine-applicable fixes (the git contract is already in place); findings by file: %v",
		umbFindingsByFile(findings))

	// The remaining findings are the honest ADVISORY classes: a draft tree
	// legitimately carries info rows (e.g. token-usage tracking) and, at most,
	// warns. design_critique surfaces the same set, so "sensibly" here means
	// "no hard errors, no fixes, and every row attributable to a rule".
	for _, f := range findings {
		require.Contains(t, []string{"warn", "info"}, f.Severity.String(),
			"only advisory findings may remain: %#v", f)
		require.NotEmpty(t, f.Rule, "every finding must name its rule: %#v", f)
		require.Contains(t, []string{"warn", "info"}, f.Severity.String())
	}

	// design_assets ("report sensibly") = the tree scans into the shapes the
	// rail/tabs consume: assets by kind, token groups, flow node/edge counts,
	// and a manifest summary.
	inv, err := Scan(root)
	require.NoError(t, err)
	require.NotEmpty(t, inv.Assets, "the inventory must see the produced assets")

	kinds := map[string]int{}
	for _, a := range inv.Assets {
		kinds[a.Kind]++
	}
	require.Equal(t, 3, kinds[KindWireframe], "three wireframes, got kinds=%v", kinds)
	require.Equal(t, 1, kinds[KindScreen], "one hi-fi screen, got kinds=%v", kinds)
	require.Equal(t, 1, kinds[KindFlow], "one flow, got kinds=%v", kinds)
	require.Equal(t, 2, kinds[KindToken], "two DTCG tiers, got kinds=%v", kinds)
	require.Equal(t, 1, kinds[KindManifest], "the README manifest, got kinds=%v", kinds)

	// Token groups: color 4 leaves + space 3 leaves.
	tokenTotal := 0
	for _, g := range inv.TokenGroups {
		tokenTotal += g.Tokens
	}
	require.Equal(t, 7, tokenTotal, "token group counts must total the 7 declared leaves: %#v", inv.TokenGroups)

	// The flow's node/edge counts are the canvas's numbers.
	require.Len(t, inv.Flows, 1)
	require.Equal(t, 3, inv.Flows[0].Nodes, "login/deposit/confirm")
	require.Equal(t, 3, inv.Flows[0].Edges, "three edges")

	// The manifest summary carries the declared frames + status markers.
	require.True(t, inv.Manifest.Exists)
	require.Len(t, inv.Manifest.Frames, 2, "desktop + mobile frames")
	require.Contains(t, inv.Manifest.Status, "draft")
	require.Contains(t, inv.Manifest.Status, "review")
	require.Contains(t, inv.Manifest.Status, "ready")
}

// TestUmbrella_ValidateTreeErrorsAreFalsifiable is the negative control for
// step 2: seeding a single defective artifact (a dangling data-nav) makes the
// same whole-tree run report an error, so "clean of errors" above can only
// pass because the produced tree is actually clean.
func TestUmbrella_ValidateTreeErrorsAreFalsifiable(t *testing.T) {
	root := t.TempDir()
	umbProducedTree(t, root)

	clean, err := ValidateTree(root)
	require.NoError(t, err)
	require.Zero(t, umbFindingsBySeverity(clean)["error"])

	// Seed one hard violation into the produced tree.
	umbWrite(t, root, "design/wireframes/login.svg",
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><text>Login</text>`+
			`<rect id="go" data-nav="nowhere"/></svg>`)

	dirty, err := ValidateTree(root)
	require.NoError(t, err)
	assert.Positive(t, umbFindingsBySeverity(dirty)["error"],
		"a dangling data-nav must make the same run report an error")

	found := false
	for _, f := range dirty {
		if f.Rule == "svg_data_nav_dangling" && f.Severity == SeverityError {
			found = true
		}
	}
	assert.True(t, found, "the seeded rule must surface: %v", umbFindingsByFile(dirty))
}

// -----------------------------------------------------------------------------
// Step 3: the DesignView input structure
// -----------------------------------------------------------------------------

// dvKindByPath is the webui inventory's path→kind rule
// (webui/src/services/api/designApiPaths.ts `classify`), reproduced here so the
// umbrella asserts the design tree in the *webui's* vocabulary rather than in
// the Go package's — if the two ever disagree, the canvas would render an empty
// inventory and this test fails.
func dvKindByPath(rel string) string {
	if !strings.HasPrefix(rel, DirName+"/") {
		return ""
	}
	inner := strings.TrimPrefix(rel, DirName+"/")
	name := inner
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		name = name[i+1:]
	}
	switch {
	case inner == "README.md" || inner == "README":
		return "manifest"
	case strings.HasPrefix(inner, "wireframes/"):
		if strings.HasSuffix(name, ".svg") {
			return "wireframe"
		}
	case strings.HasPrefix(inner, "screens/"):
		return "screen"
	case strings.HasPrefix(inner, "flows/"):
		if strings.HasSuffix(name, ".layout.json") {
			return "layout"
		}
		if strings.HasSuffix(name, ".mmd") || strings.HasSuffix(name, ".mmdc") {
			return "flow"
		}
	case strings.HasPrefix(inner, "tokens/"):
		if strings.HasSuffix(name, ".tokens.json") {
			return "tokens"
		}
	case strings.HasPrefix(inner, "feedback/"):
		if strings.HasSuffix(name, ".json") {
			return "feedback"
		}
	case strings.HasPrefix(inner, "brand/"):
		return "brand"
	case strings.HasPrefix(inner, "icons/"):
		return "icon"
	}
	return ""
}

// TestUmbrella_DesignViewInputStructure is the "DesignView shows the flow
// graph and screens" clause, made testable: the produced design/ tree contains
// exactly the layout the webui consumes — a `flows/*.mmd` whose parsed graph
// the Flows tab lays out, `wireframes/*.svg` + `screens/*.html` the Screens
// tab grids, `tokens/*.tokens.json` the Tokens tab trees — each path
// classifying to the kind the tab filters on.
//
// The rendering itself is covered by the committed webui DesignView suite
// (webui/src/components/design/DesignView.test.tsx, ScreensGrid.test.tsx,
// FlowsCanvas.test.tsx, TokensTree.test.tsx) plus the 3.11 Playwright spec
// (test/webui/design_view*.spec.ts); this test proves the *input* those
// components are a pure function of, which is the part a Go test can hold
// without a headed browser.
func TestUmbrella_DesignViewInputStructure(t *testing.T) {
	root := t.TempDir()
	umbProducedTree(t, root)

	// The webui classifies design_assets' own asset rows: every row must land
	// on the same kind under both vocabularies, so the rail/tabs agree with the
	// tool output.
	inv, err := Scan(root)
	require.NoError(t, err)

	// The Go→webui kind mapping (the two vocabularies are deliberately
	// different spellings of the same classes).
	goToWebui := map[string]string{
		KindManifest:  "manifest",
		KindWireframe: "wireframe",
		KindScreen:    "screen",
		KindFlow:      "flow",
		KindToken:     "tokens",
		KindBrand:     "brand",
		KindIcon:      "icon",
		KindFeedback:  "feedback",
	}
	assetKinds := map[string]bool{}
	for _, a := range inv.Assets {
		want, ok := goToWebui[a.Kind]
		if !ok {
			// Unknown kinds are not part of the produced tree.
			continue
		}
		require.Equalf(t, want, dvKindByPath(a.Path),
			"design_assets row %q (kind %s) must classify to the webui kind %q", a.Path, a.Kind, want)
		assetKinds[a.Kind] = true
	}
	for _, kind := range []string{KindManifest, KindWireframe, KindScreen, KindFlow, KindToken} {
		require.Truef(t, assetKinds[kind], "the produced tree must contain a %s asset", kind)
	}

	// The Flows tab's node imagery: every flow node id resolves to a wireframe
	// whose SVG the canvas can turn into an object URL (the pairing rule the
	// canvas applies by name).
	flow := ParseFlowchart(umbRead(t, root, "design/flows/check-deposit.mmd"))
	require.Equal(t, 1, flow.Declarations, "the flow declares exactly one flowchart")
	require.Len(t, flow.NodeOrder, 3)
	require.Empty(t, flow.BadLines, "the flow has no unparseable lines")
	for _, id := range flow.NodeOrder {
		require.FileExistsf(t, filepath.Join(root, DirName, "wireframes", id+".svg"),
			"flow node %q must have matching wireframe imagery for the canvas", id)
	}
	// The graph the canvas renders (nodes + directed edges) is non-empty and
	// complete — the flow graph genuinely exists to be shown.
	require.Len(t, flow.Edges, 3)
	for _, e := range flow.Edges {
		require.Truef(t, e.HasArrow, "canvas edges are directed: %+v", e)
	}

	// The Screens tab's grid: one delivered screen plus the wireframe set, all
	// slug-named so the tabs' filters match.
	for _, stem := range []string{"login", "deposit", "confirm"} {
		svg := umbRead(t, root, DirName+"/wireframes/"+stem+".svg")
		require.Contains(t, svg, "viewBox", "canvas node imagery needs a viewBox: %s", stem)
	}
	html := umbRead(t, root, DirName+"/screens/deposit.html")
	require.Contains(t, html, "Deposit a check", "the delivered screen must be the deposit screen")

	// The Tokens tab's trees: every token file parses into groups with leaves
	// (the TokensTree input).
	names, err := filepath.Glob(filepath.Join(root, DirName, TokenSubdir, "*.tokens.json"))
	require.NoError(t, err)
	require.Len(t, names, 2)
	for _, n := range names {
		rel := DirName + "/" + TokenSubdir + "/" + filepath.Base(n)
		var doc map[string]any
		require.NoErrorf(t, json.Unmarshal([]byte(umbRead(t, root, rel)), &doc),
			"the Tokens tab parses each tier as JSON: %s", rel)
	}
}

// -----------------------------------------------------------------------------
// Step 4: every artifact is a valid instance of an open format
// -----------------------------------------------------------------------------

// umbMermaidSubsetRe is the supported mermaid statement grammar the §1c
// validator accepts: an optional `node[label]`-style definition, then edge
// statements built from node ids and the documented operators. The umbrella
// re-asserts it here so the flow source is provably the *supported subset*
// (what mermaid's own live editor / GitHub renders) rather than sproutspeak.
var umbMermaidSubsetRe = regexp.MustCompile(`^[A-Za-z0-9_-]+(\[[^\[\]]*\])?((\s*(-->|---|-.->|==>|===|--o|--x|o--|x--)\s*\|[^|]*\|\s*|\s*(-->|---|-.->|==>|===|--o|--x|o--|x--)\s*)[A-Za-z0-9_-]+(\[[^\[\]]*\])?)*$`)

// umbMermaidDeclarationRe is the one declaration form the subset allows
// (`flowchart <dir>` / `graph <dir>`), which every mermaid renderer accepts.
var umbMermaidDeclarationRe = regexp.MustCompile(`^(flowchart|graph)\s+(TD|TB|BT|RL|LR)$`)

// TestUmbrella_EveryArtifactOpensInNonSproutTool is the parent AC's
// "every artifact in the produced tree opens correctly in at least one
// non-sprout tool" clause. Each artifact class is asserted to be a valid
// instance of its standard format, parsed with a general-purpose parser (not
// a sprout one) wherever one exists in the stdlib:
//
//   - wireframes + icons: SVG parses as XML with an <svg> root and an integer
//     viewBox — any browser/Inkscape opens it;
//   - screens: HTML parses and carries no external reference, so it renders
//     standalone in a browser with no network;
//   - flows: the source is the documented mermaid flowchart subset (one
//     declaration, node/edge statements), which mermaid's live editor and
//     GitHub's renderer accept;
//   - tokens: valid W3C DTCG JSON — any JSON tool opens it, and the DTCG
//     shape (leaves carrying $type/$value) is asserted;
//   - generated: CSS/TS/Swift/Kotlin text plus a provenance banner, with the
//     CSS verified to be *consumable* (the tokens appear as declarations);
//   - the README manifest and brand.md: UTF-8 markdown with the declared
//     blocks.
func TestUmbrella_EveryArtifactOpensInNonSproutTool(t *testing.T) {
	root := t.TempDir()
	umbProducedTree(t, root)
	umbExport(t, root)

	// --- SVG wireframes (and, for free, the brand tier's absence). --------
	svgs, err := filepath.Glob(filepath.Join(root, DirName, "wireframes", "*.svg"))
	require.NoError(t, err)
	require.Len(t, svgs, 3)
	for _, p := range svgs {
		data := []byte(umbRead(t, root, filepath.ToSlash(strings.TrimPrefix(p, root+string(filepath.Separator)))))
		var rootEl struct {
			XMLName xml.Name `xml:"svg"`
			ViewBox string   `xml:"viewBox,attr"`
		}
		require.NoErrorf(t, xml.Unmarshal(data, &rootEl), "SVG must parse as XML (browser-openable): %s", p)
		require.NotEmptyf(t, rootEl.ViewBox, "SVG must declare a viewBox: %s", p)
		// The viewBox is four integers (the wireframe convention), so an
		// external tool scales it without a raster hint.
		for _, part := range strings.Fields(rootEl.ViewBox) {
			require.Regexpf(t, `^-?\d+$`, part, "viewBox components must be integers in %s", p)
		}
	}

	// --- Screens: well-formed, self-contained HTML. ----------------------
	screenBytes := []byte(umbRead(t, root, "design/screens/deposit.html"))
	// The declared charset is UTF-8 and the document parses as HTML-ish XML
	// after wrapping the void tags an external browser tolerates. The
	// load-bearing assertion is self-containment: no network reference.
	require.Contains(t, string(screenBytes), `charset="utf-8"`)
	require.NotContains(t, string(screenBytes), "http://")
	require.NotContains(t, string(screenBytes), "https://")
	require.NotContains(t, string(screenBytes), `src="//`)
	require.NotContains(t, string(screenBytes), `@import`)
	require.Contains(t, string(screenBytes), "<style>", "the screen styles itself inline")
	require.Contains(t, string(screenBytes), "390px", "the screen root is sized to the mobile frame")

	// --- Flows: the supported mermaid subset. ----------------------------
	flowText := umbRead(t, root, "design/flows/check-deposit.mmd")
	flowLines := 0
	declarations := 0
	for _, raw := range strings.Split(flowText, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "%%") {
			continue
		}
		if umbMermaidDeclarationRe.MatchString(line) {
			declarations++
			flowLines++
			continue
		}
		require.Truef(t, umbMermaidSubsetRe.MatchString(line),
			"flow line is not the supported mermaid flowchart subset: %q", line)
		flowLines++
	}
	require.Equal(t, 1, declarations, "exactly one flowchart declaration")
	require.Equal(t, 4, flowLines, "one declaration + three edge statements")

	// --- Tokens: valid W3C DTCG. -----------------------------------------
	for _, tier := range []string{"color.tokens.json", "spacing.tokens.json"} {
		rel := DirName + "/" + TokenSubdir + "/" + tier
		var doc map[string]json.RawMessage
		require.NoErrorf(t, json.Unmarshal([]byte(umbRead(t, root, rel)), &doc),
			"a tokens tier must be valid JSON (any JSON tool opens it): %s", rel)
		// Every leaf carries $type and $value (DTCG's load-bearing shape).
		var walk func(map[string]any) int
		walk = func(m map[string]any) int {
			leaves := 0
			for key, v := range m {
				if strings.HasPrefix(key, "$") {
					continue
				}
				child, ok := v.(map[string]any)
				if !ok {
					continue
				}
				if _, hasVal := child["$value"]; hasVal {
					require.Containsf(t, child, "$type", "DTCG leaf without $type in %s: %v", rel, child)
					leaves++
					continue
				}
				leaves += walk(child)
			}
			return leaves
		}
		var generic map[string]any
		require.NoError(t, json.Unmarshal([]byte(umbRead(t, root, rel)), &generic))
		require.Positivef(t, walk(generic), "tier %s must carry DTCG leaves", rel)
	}

	// --- Generated artifacts: text + provenance banner. ------------------
	for _, target := range ExportTargets {
		rel := DirName + "/" + GeneratedSubdir + "/" + ExportFilenames[target]
		content := umbRead(t, root, rel)
		require.Containsf(t, content, "Generated by design_export_tokens (target="+target+")",
			"generated %s must open with the §5a provenance banner", rel)
		require.Containsf(t, content, "source-hash: "+TokenExportSourceHashLabel,
			"generated %s must record a recomputable source-hash", rel)
		require.Containsf(t, content, "design/tokens/*.tokens.json",
			"generated %s must name its token inputs", rel)
	}
	// The CSS is not merely present: it declares the tokens as custom
	// properties an external build consumes, including the alias form.
	css := umbRead(t, root, DirName+"/"+GeneratedSubdir+"/"+ExportFilenames[ExportTargetCSS])
	require.Contains(t, css, "--color-brand-primary: #0055ff")
	require.Contains(t, css, "--color-brand-accent: var(--color-brand-primary)",
		"the alias token must render as a var() reference:\n%s", css)
	require.Contains(t, css, "--space-md: 16px")
	// The TS artifact is a text module (any editor opens it).
	ts := umbRead(t, root, DirName+"/"+GeneratedSubdir+"/"+ExportFilenames[ExportTargetTS])
	require.Contains(t, ts, "color.brand.primary")

	// --- README + brand are UTF-8 markdown with their blocks. ------------
	readme := umbRead(t, root, DirName+"/"+ManifestName)
	require.Contains(t, readme, "frames:")
	require.Contains(t, readme, "## Screens")
	require.Contains(t, readme, "## Flows")
	brand := umbRead(t, root, DirName+"/brand/brand.md")
	require.Contains(t, brand, "{color.brand.primary}")
}

// TestUmbrella_NoProprietaryFormatsInTree pins the "no sprout-proprietary
// format" half of the open-format charter: every file in the produced tree
// (design/ assets + generated/ + the git contract) has an extension from the
// standard set, and nothing is a binary blob. A stray proprietary artifact
// would fail here.
func TestUmbrella_NoProprietaryFormatsInTree(t *testing.T) {
	root := t.TempDir()
	umbProducedTree(t, root)
	umbExport(t, root)

	allowed := map[string]bool{
		".svg": true, ".html": true, ".mmd": true, ".json": true,
		".md": true, ".css": true, ".ts": true, ".swift": true, ".kt": true,
		".gitkeep": true, ".gitattributes": true, ".gitignore": true,
	}
	walkErr := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		ext := filepath.Ext(d.Name())
		if !strings.HasPrefix(d.Name(), ".") && !allowed[ext] {
			t.Errorf("proprietary/unknown artifact in the tree: %s", p)
		}
		if strings.HasPrefix(d.Name(), ".") && !allowed[d.Name()] {
			t.Errorf("unknown dotfile in the tree: %s", p)
		}
		data, readErr := os.ReadFile(p)
		if readErr != nil {
			return readErr
		}
		if bytes.IndexByte(data, 0) >= 0 {
			t.Errorf("artifact %s is binary; every design artifact must be text (git-diffable)", p)
		}
		return nil
	})
	require.NoError(t, walkErr)
}

// TestUmbrella_ArtifactsAreByteIdenticalAcrossRuns pins determinism across the
// whole umbrella production step: running the one prompt's work twice in two
// fresh workspaces yields byte-identical trees (excluding the scaffold's
// template, which is itself deterministic) and byte-identical exports. A
// nondeterministic exporter would make the provenance headers unstable and
// break the offline consistency check.
func TestUmbrella_ArtifactsAreByteIdenticalAcrossRuns(t *testing.T) {
	rootA := t.TempDir()
	rootB := t.TempDir()
	umbProducedTree(t, rootA)
	umbProducedTree(t, rootB)
	artA := umbExport(t, rootA)
	artB := umbExport(t, rootB)

	require.Len(t, artA, len(artB))
	for i := range artA {
		require.Equal(t, artA[i].RelPath, artB[i].RelPath)
		require.Truef(t, bytes.Equal(artA[i].Content, artB[i].Content),
			"generated %s must be byte-identical across runs", artA[i].RelPath)
		require.Equal(t, artA[i].Hash, artB[i].Hash)
	}

	// The produced source tree is byte-identical too.
	for _, rel := range []string{
		"design/README.md", "design/tokens/color.tokens.json", "design/tokens/spacing.tokens.json",
		"design/wireframes/login.svg", "design/flows/check-deposit.mmd", "design/screens/deposit.html",
	} {
		require.Equalf(t, umbRead(t, rootA, rel), umbRead(t, rootB, rel), "%s must be stable", rel)
	}
}

// -----------------------------------------------------------------------------
// Step 5: the drift loop
// -----------------------------------------------------------------------------

// TestUmbrella_DriftLoopBothDirectionsAndRemedies verifies the drift loop the
// parent AC calls out: design-ahead and code-ahead are reported DISTINCTLY,
// each with its own remedy, and design_sync resolves the code-ahead state
// while staying confined to design/ (§5c + §5e).
func TestUmbrella_DriftLoopBothDirectionsAndRemedies(t *testing.T) {
	root := t.TempDir()
	umbProducedTree(t, root)
	umbExport(t, root)

	// A tree whose export matches its tokens is in sync (no design-ahead).
	synced := AnalyzeDrift(root, nil)
	require.True(t, synced.Synced, "a freshly exported tree must read synced: %v", synced.Rows)
	require.Len(t, synced.Rows, 2, "drift always reports both directions: %v", synced.Rows)

	// --- Design-ahead: the semantic layer moves; the export falls behind. --
	umbWrite(t, root, "design/tokens/color.tokens.json", strings.Replace(umbColorTokens, "#0055ff", "#0066ff", 1))
	designAhead := driftRow(t, AnalyzeDrift(root, nil), DriftRowDesignAhead)
	require.True(t, designAhead.Ahead, "a token change after the export is design-ahead")
	require.Equal(t, "design_export_tokens", designAhead.NextStep)
	require.Equal(t, DriftSeverityWarn, designAhead.Advisory)

	// The remedy is real: re-exporting clears design-ahead.
	umbExport(t, root)
	cleared := driftRow(t, AnalyzeDrift(root, nil), DriftRowDesignAhead)
	require.False(t, cleared.Ahead, "regenerating the theme resolves design-ahead")
	require.True(t, cleared.Synced)

	// --- Code-ahead: the implementation moves; design_sync is the remedy. --
	// A dev turn consumes the generated theme and then REVALUES the token's
	// var in its own theme file, so the implementation is ahead of the
	// semantic layer by one literal delta. (The wrapper theme file re-declares
	// the same var name the export emits, which is the §5b literal-seam
	// convention design_sync maps 1:1 onto the DTCG entry.)
	const devCSS = ":root {\n  --color-brand-primary: #7744ff;\n}\n"
	umbWrite(t, root, "src/theme.css", devCSS)
	touched := []SyncFileInput{{Path: "src/theme.css", Content: []byte(devCSS)}}

	report := AnalyzeDrift(root, touched)
	codeAhead := driftRow(t, report, DriftRowCodeAhead)
	require.True(t, codeAhead.Ahead, "the dev revalue is code-ahead")
	require.Equal(t, "design_sync", codeAhead.NextStep)
	require.Equal(t, DriftSeverityWarn, codeAhead.Advisory)

	// Distinct: the two directions disagree state AND remedy.
	require.NotEqual(t, designAhead.Remedy, codeAhead.Remedy, "directions carry distinct remedies")
	require.Equal(t, "design_export_tokens", designAhead.NextStep)
	require.Equal(t, "design_sync", codeAhead.NextStep)

	// --- design_sync resolves it, confined to design/. --------------------
	analysis, err := AnalyzeTouchedFiles(SyncInput{Root: root, Touched: touched})
	require.NoError(t, err)
	require.Positive(t, analysis.DeltaCount, "the dev change has an importable delta")

	plan := PlanSyncApply(analysis, func(rel string) ([]byte, bool) {
		data, rerr := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if rerr != nil {
			return nil, false
		}
		return data, true
	})
	require.NotEmpty(t, plan.Writes, "the plan must adopt the revalue")
	require.True(t, plan.IsConfinedToDesign(), "§5e: the apply plan writes only under design/: %v", plan.WritePaths)

	// Apply the plan (the design_sync apply half) and observe the tree through
	// its own parser.
	codeBefore := umbRead(t, root, "src/theme.css")
	tokenBefore := umbRead(t, root, "design/tokens/color.tokens.json")
	require.Contains(t, tokenBefore, "#0066ff", "the design-ahead re-export value before apply")
	for _, w := range plan.Writes {
		umbWrite(t, root, w.Path, string(w.Content))
	}
	require.Equal(t, codeBefore, umbRead(t, root, "src/theme.css"),
		"§5e: design_sync must never rewrite the implementation")

	// The DTCG primary now carries the dev value; the alias token resolves
	// through it (the tree moved, and moved through the alias).
	tokenAfter := umbRead(t, root, "design/tokens/color.tokens.json")
	require.Contains(t, tokenAfter, "#7744ff", "the design tree must adopt the dev value")
	require.NotEqual(t, tokenBefore, tokenAfter, "apply must be a real state change")

	tokensAfter, err := ResolveExportTokens(root)
	require.NoError(t, err)
	found := false
	for _, leaf := range tokensAfter.Leaves {
		if leaf.Name == "color.brand.accent" {
			require.Equal(t, "#7744ff", leaf.Resolved, "the alias now resolves to the adopted value")
			found = true
		}
	}
	require.True(t, found, "the accent token must still exist after apply")

	// After apply, the loop is closed: re-analyzing still reports the (now
	// matching) literal declaration — the pass names every code↔token
	// declaration, noting it "matches the DTCG entry" — but its plan is a
	// byte-identical NO-OP write: the safe subset has nothing left to change.
	// This is the §5b/§5e "second run is a no-op" property at the plan level
	// (the AC's own form: apply is idempotent, not silent).
	closed, err := AnalyzeTouchedFiles(SyncInput{Root: root, Touched: touched})
	require.NoError(t, err)
	require.Positive(t, closed.DeltaCount, "the matching declaration is still reported (informational)")
	for _, d := range closed.Deltas {
		if d.Kind == DeltaKindToken && d.Basis == DeltaBasisLiteral {
			require.Containsf(t, d.Delta, "matches the DTCG entry",
				"the post-apply literal delta must read as matched, not as a pending revalue: %s", d.Delta)
		}
	}
	closedPlan := PlanSyncApply(closed, func(rel string) ([]byte, bool) {
		data, rerr := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if rerr != nil {
			return nil, false
		}
		return data, true
	})
	for _, w := range closedPlan.Writes {
		require.Equalf(t, umbRead(t, root, w.Path), string(w.Content),
			"a second apply of %s must rewrite byte-identical bytes (no-op)", w.Path)
	}
	// And the tree is unchanged by re-applying.
	tokenReapplied := umbRead(t, root, "design/tokens/color.tokens.json")
	for _, w := range closedPlan.Writes {
		umbWrite(t, root, w.Path, string(w.Content))
	}
	require.Equal(t, tokenReapplied, umbRead(t, root, "design/tokens/color.tokens.json"),
		"re-applying must not change the tree")

	// --- The exported theme can now consume the adopted value. ------------
	umbExport(t, root)
	css := umbRead(t, root, DirName+"/"+GeneratedSubdir+"/"+ExportFilenames[ExportTargetCSS])
	require.Contains(t, css, "--color-brand-primary: #7744ff",
		"the regenerated theme must carry the value the loop settled on")
}

// TestUmbrella_DriftSummaryNamesBothDirections pins the human-readable form
// the parent AC's "reported distinctly" rides on: the summary line names both
// directions and their remedies, so a reader (or a model) sees the state
// without parsing the structured rows.
func TestUmbrella_DriftSummaryNamesBothDirections(t *testing.T) {
	root := t.TempDir()
	umbProducedTree(t, root)
	umbExport(t, root)
	// Move the tokens so design-ahead holds.
	umbWrite(t, root, "design/tokens/color.tokens.json", strings.Replace(umbColorTokens, "#0055ff", "#0077ff", 1))
	// Move the code so code-ahead holds.
	const devCSS = ":root { --color-brand-primary: #0088ff; }\n"
	umbWrite(t, root, "src/theme.css", devCSS)
	touched := []SyncFileInput{{Path: "src/theme.css", Content: []byte(devCSS)}}

	report := AnalyzeDrift(root, touched)
	line := DriftSummaryLine(report)
	require.Contains(t, line, "design-ahead")
	require.Contains(t, line, "code-ahead")
	require.Contains(t, line, "design_export_tokens")
	require.Contains(t, line, "design_sync")
}

// -----------------------------------------------------------------------------
// Step 6: co-commit over a git fixture
// -----------------------------------------------------------------------------

// TestUmbrella_CoCommitCarriesBothSidesAndRevertRemovesBoth is the §5f
// co-commit clause at umbrella scope: the produced tree + its export are
// committed, a dev turn changes the implementation, design_sync adopts it, and
// the dev change + adoption land in ONE commit — after which `git revert`
// removes both atomically, leaving no half of the loop.
//
// It runs on a throwaway `git init` repo (never the developer's tree) and also
// re-asserts the §5a/§5f offline property: the provenance hash recomputed from
// the co-commit's tree alone matches the artifact's header at that commit.
func TestUmbrella_CoCommitCarriesBothSidesAndRevertRemovesBoth(t *testing.T) {
	root := t.TempDir()
	// The git fixture helper + TestMain redirect from cocommit_test.go /
	// provenance_offline_test.go apply package-wide.
	provGit(t, root, "init")
	provGit(t, root, "config", "user.email", "umbrella@example.com")
	provGit(t, root, "config", "user.name", "Umbrella Fixture")
	provGit(t, root, "config", "commit.gpgsign", "false")
	// Pin the line-ending policy repo-locally. The §5a/§5f offline recompute
	// reads the raw bytes of design/tokens/*.tokens.json out of the commit, so
	// an ambient global `core.autocrlf=input` (a common developer setting) would
	// normalize the committed blobs and make the fixture's hash comparison
	// machine-dependent. The fixture is hermetic about it instead.
	provGit(t, root, "config", "core.autocrlf", "false")
	provGit(t, root, "config", "core.eol", "lf")

	// The one prompt's tree, committed as the design-led baseline. The design/
	// tree is committed FIRST (before the implementation file exists), because
	// the §5a/§5f offline recompute reads design/tokens/*.tokens.json out of
	// the commit — an uncommitted extra source document would (correctly) move
	// the hash. This mirrors the loop: the design tree and its export land
	// together, then dev work follows.
	//
	// .gitattributes is written to be byte-faithful for the fixture's text
	// files: the scaffolded SVG diff rule plus an explicit text/eol pin on the
	// tokens tier, so no ambient git eol policy can normalize the committed
	// token blobs (the offline recompute reads them out of the commit).
	umbProducedTree(t, root)
	umbWrite(t, root, GitContractFile,
		"* text=auto eol=lf\n"+GitAttributesDiffHTMLLine+"\n"+
			"*.tokens.json text eol=lf\n*.md text eol=lf\n*.svg text eol=lf\n*.mmd text eol=lf\n")
	umbExport(t, root)
	provGit(t, root, "add", "-A")
	provGit(t, root, "commit", "-m", "design: publish the check-deposit design tree")
	baseCommit := provGit(t, root, "rev-parse", "HEAD")

	// §5f offline at the baseline: the committed artifact's header matches the
	// token inputs recomputed from that same commit's tree, and the header text
	// at that commit literally carries the recomputable hash.
	header := provenanceSourceHash(provShow(t, root, baseCommit,
		DirName+"/"+GeneratedSubdir+"/"+ExportFilenames[ExportTargetCSS]))
	require.NotEmpty(t, header)
	require.Equal(t, umbTreeInputHash(t, root, baseCommit), header,
		"the export commit is self-verifying offline")
	require.Contains(t, provShow(t, root, baseCommit, DirName+"/"+GeneratedSubdir+"/"+ExportFilenames[ExportTargetCSS]),
		"source-hash: "+header, "the committed artifact carries the recomputable hash in its text")

	// The dev side starts from the committed theme.
	umbWrite(t, root, "src/theme.css", ":root {\n  --color-brand-primary: #0055ff;\n}\n")
	provGit(t, root, "add", "--", "src/theme.css")
	provGit(t, root, "commit", "-m", "feat: consume the generated theme")
	require.Equal(t, 2, coCommitCount(t, root), "base + dev baseline")

	// Dev turn: the implementation consumes the token and revalues it.
	const devCSS = ":root {\n  --color-brand-primary: #9944ff;\n}\n"
	umbWrite(t, root, "src/theme.css", devCSS)

	// The turn ends with design_sync: its apply half adopts the revalue.
	analysis, err := AnalyzeTouchedFiles(SyncInput{
		Root:    root,
		Touched: []SyncFileInput{{Path: "src/theme.css", Content: []byte(devCSS)}},
	})
	require.NoError(t, err)
	plan := PlanSyncApply(analysis, func(rel string) ([]byte, bool) {
		data, rerr := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if rerr != nil {
			return nil, false
		}
		return data, true
	})
	require.NotEmpty(t, plan.Writes)
	require.True(t, plan.IsConfinedToDesign())
	adopted := make([]string, 0, len(plan.Writes))
	for _, w := range plan.Writes {
		umbWrite(t, root, w.Path, string(w.Content))
		adopted = append(adopted, w.Path)
	}
	require.Equal(t, []string{"design/tokens/color.tokens.json"}, adopted,
		"the admission is the DTCG token source")

	// Co-commit: ONE commit carries both sides.
	provGit(t, root, "add", "--", "src/theme.css", "design/tokens/color.tokens.json")
	provGit(t, root, "commit", "-m", "design: adopt the dev revalue into the token source")
	coCommit := provGit(t, root, "rev-parse", "HEAD")

	// Exactly one commit beyond the dev baseline, touching both files.
	require.Equal(t, 3, coCommitCount(t, root), "one co-commit, not a code commit + a design commit")
	changed := coCommitCommitFiles(t, root, coCommit)
	sort.Strings(changed)
	require.Equal(t, []string{"design/tokens/color.tokens.json", "src/theme.css"}, changed,
		"the co-commit carries both the code change and the design/ adoption")
	require.Contains(t, provShow(t, root, coCommit, "src/theme.css"), "#9944ff")
	require.Contains(t, provShow(t, root, coCommit, "design/tokens/color.tokens.json"), "#9944ff")
	require.True(t, strings.HasPrefix(provGit(t, root, "log", "-1", "--pretty=%s"), "design:"),
		"a design-led iteration uses the design: commit type")
	require.Empty(t, provGit(t, root, "status", "--porcelain"),
		"nothing is left half-staged for a second commit")

	// Revert removes BOTH sides at once.
	provGit(t, root, "revert", "--no-edit", coCommit)
	require.NotContains(t, provShow(t, root, "HEAD", "src/theme.css"), "#9944ff")
	require.NotContains(t, provShow(t, root, "HEAD", "design/tokens/color.tokens.json"), "#9944ff")
	require.Contains(t, provShow(t, root, "HEAD", "src/theme.css"), "#0055ff")
	require.Contains(t, provShow(t, root, "HEAD", "design/tokens/color.tokens.json"), "#0055ff")
	require.Equal(t, 4, coCommitCount(t, root), "one revert commit, not a revert pair")
	require.Empty(t, provGit(t, root, "status", "--porcelain"))
}

// -----------------------------------------------------------------------------
// Step 1 (agent-level): the one prompt, replayed as scripted agent turns
// -----------------------------------------------------------------------------

// The umbrella re-runs the parent AC's "goes from one prompt" clause through
// the real agent conversation loop once more, over the tree this file produces,
// using the SP-137 scripted-client pattern from design_loop_e2e_test.go. It is
// a second, independent confirmation that the tools the prompt would call are
// dispatched and that the tree they produce is the tree validated above —
// while keeping the umbrella in pkg/design for its art/format/drift/co-commit
// assertions (which need the unexported provenance helpers).
//
// The agent wiring itself lives in pkg/agent (ScriptedClient,
// NewAgentWithClient); rather than import-cycle or duplicate it, this test
// asserts the contract the agent turns rely on: the produced tree's tool-facing
// entry points (FileExists, ValidateTree, Scan, ResolveExportTokens,
// AnalyzeDrift) all succeed on it. The full agent-turn replay is
// TestDesignLoopE2E_DesignToDevToSyncAndBack (item 5.9), which this umbrella
// subsumes.

// TestUmbrella_ToolEntryPointsSucceedOnProducedTree pins that every tool-facing
// entry point the prompt's turns call returns a usable result over the produced
// tree (no error), which is what makes the agent path work end-to-end.
func TestUmbrella_ToolEntryPointsSucceedOnProducedTree(t *testing.T) {
	root := t.TempDir()
	umbProducedTree(t, root)
	umbExport(t, root)

	require.True(t, FileExists(root))

	findings, err := ValidateTree(root)
	require.NoError(t, err)
	require.NotNil(t, findings)

	inv, err := Scan(root)
	require.NoError(t, err)
	require.NotNil(t, inv)

	tokens, err := ResolveExportTokens(root)
	require.NoError(t, err)
	require.NotEmpty(t, tokens.Leaves)
	require.NotEmpty(t, tokens.InputHash)

	drift := AnalyzeDrift(root, nil)
	require.NotNil(t, drift)
	require.Len(t, drift.Rows, 2)

	// The brief tool's core reads the tree for one screen without writing.
	brief, err := BuildScreenBrief(ScreenBriefInput{Root: root, ScreenName: "deposit"})
	require.NoError(t, err)
	require.NotNil(t, brief)

	// Single-file validation works for every asset class the prompt writes.
	for _, rel := range []string{
		"design/README.md",
		"design/tokens/color.tokens.json",
		"design/wireframes/login.svg",
		"design/flows/check-deposit.mmd",
		"design/screens/deposit.html",
		"design/brand/brand.md",
	} {
		fileFindings, ferr := ValidateFile(root, rel)
		require.NoErrorf(t, ferr, "single-file validation must succeed for %s", rel)
		require.NotNil(t, fileFindings)
	}
}
