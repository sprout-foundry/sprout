package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sprout-foundry/sprout/pkg/design"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// daWrite writes rel (slash-separated) under root with parent directories.
func daWrite(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

const daTestManifest = `# Design Workspace

frames:
  desktop: 1440x900
  mobile: 390x844

## Screens

- ` + "`login`" + ` — draft — sign-in entry point
- ` + "`home`" + ` — ready — post-sign-in landing

## Flows

- ` + "`sign-up`" + ` — draft — account creation

## Status markers

Screens and flows carry one of: draft, review, ready.

## Links

- [Color tokens](tokens/color.tokens.json)
- [Login wireframe](wireframes/login.svg)
`

const daTestTokenJSON = `{
  "color": {
    "brand": {
      "primary": {"$value": "#0055ff", "$type": "color"},
      "secondary": {"$value": "{color.brand.primary}", "$type": "color"}
    }
  }
}`

const daTestLoginSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844">
  <text x="24" y="64" font-size="28">Login</text>
  <rect id="submit" x="24" y="200" width="342" height="52" data-nav="home" />
</svg>`

const daTestHomeSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844">
  <text x="24" y="64" font-size="28">Home</text>
</svg>`

const daTestFlowMMD = "flowchart TD\n  login --> home\n"

// daFeedbackPendingJSON is a §4d feedback document for the login wireframe:
// status changes-requested with one unresolved annotation (pending).
const daFeedbackPendingJSON = `{
  "target": "design/wireframes/login.svg",
  "status": "changes-requested",
  "resolution": "",
  "annotations": [
    {"id": "a1", "at": {"x": 0.42, "y": 0.18}, "area": "hierarchy",
     "note": "Primary CTA reads as secondary", "resolved": false,
     "created": "2026-09-15T10:36:47Z"}
  ]
}`

// daFeedbackPartialJSON is a §4d document whose status is not
// "changes-requested" but that carries one unresolved annotation out of three,
// so it is still pending (§4d predicate) with the right counts.
const daFeedbackPartialJSON = `{
  "target": "design/wireframes/home.svg",
  "status": "reviewed",
  "resolution": "",
  "annotations": [
    {"id": "a1", "at": {"x": 0.1, "y": 0.1}, "area": "contrast", "note": "quiet", "resolved": true, "created": ""},
    {"id": "a2", "at": {"x": 0.2, "y": 0.2}, "area": "contrast", "note": "loud", "resolved": true, "created": ""},
    {"id": "a3", "at": {"x": 0.3, "y": 0.3}, "area": "spacing", "note": "tight", "resolved": false, "created": ""}
  ]
}`

// daFeedbackResolvedJSON is a §4d document with no pending work: status
// resolved and every annotation resolved.
const daFeedbackResolvedJSON = `{
  "target": "design/flows/sign-up.mmd",
  "status": "resolved",
  "resolution": "Swapped CTA emphasis and rebuilt the form spacing.",
  "annotations": [
    {"id": "a1", "at": {"x": 0.5, "y": 0.5}, "area": "hierarchy", "note": "done", "resolved": true, "created": ""}
  ]
}`

// daWriteValidTree populates root with a small design/ tree that yields zero
// findings on a whole-tree run, including the §1h git contract lines.
func daWriteValidTree(t *testing.T, root string) {
	t.Helper()
	daWrite(t, root, "design/README.md", daTestManifest)
	daWrite(t, root, "design/tokens/color.tokens.json", daTestTokenJSON)
	daWrite(t, root, "design/wireframes/login.svg", daTestLoginSVG)
	daWrite(t, root, "design/wireframes/home.svg", daTestHomeSVG)
	daWrite(t, root, "design/flows/sign-up.mmd", daTestFlowMMD)
	daWrite(t, root, design.GitContractFile, "* text=auto eol=lf\n"+design.GitAttributesDiffHTMLLine+"\n")
	daWrite(t, root, design.GitIgnoreFile, "node_modules/\n"+design.GitIgnoreCacheLine+"\n")
}

// ---------------------------------------------------------------------------
// design_assets handler tests
// ---------------------------------------------------------------------------

func TestDesignAssetsHandler_NameAndDefinition(t *testing.T) {
	t.Parallel()
	h := &designAssetsHandler{}

	require.Equal(t, "design_assets", h.Name())

	def := h.Definition()
	require.Equal(t, "design_assets", def.Name)
	require.NotEmpty(t, def.Description)
	require.Contains(t, strings.ToLower(def.Description), "inventory")
	require.Empty(t, def.Required, "path and format must both be optional")

	params := map[string]bool{}
	for _, p := range def.Parameters {
		params[p.Name] = true
		require.False(t, p.Required, "parameter %q must not be required", p.Name)
		require.Equal(t, "string", p.Type)
	}
	require.True(t, params["path"], "should have a 'path' parameter")
	require.True(t, params["format"], "should have a 'format' parameter")
}

func TestDesignAssetsHandler_Metadata(t *testing.T) {
	t.Parallel()
	h := &designAssetsHandler{}

	require.Nil(t, h.Aliases())
	require.Equal(t, 60*time.Second, h.Timeout())
	require.Equal(t, 0, h.MaxResultSize())
	require.True(t, h.SafeForParallel(), "design_assets is read-only and safe for parallel execution")
	require.False(t, h.Interactive())
}

func TestDesignAssetsHandler_Validate(t *testing.T) {
	t.Parallel()
	h := &designAssetsHandler{}

	require.NoError(t, h.Validate(nil))
	require.NoError(t, h.Validate(map[string]any{}))
	require.NoError(t, h.Validate(map[string]any{"path": "design/wireframes", "format": "wireframe"}))

	require.Error(t, h.Validate(map[string]any{"path": 42}))
	require.Error(t, h.Validate(map[string]any{"format": 42}))
}

func TestDesignAssetsHandler_NoDesignDir(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	h := &designAssetsHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{})
	require.NoError(t, err, "a missing design/ is a scaffold hint, not a failure")
	require.False(t, res.IsError)
	require.Contains(t, res.Output, "no design/")
	require.Contains(t, res.Output, "design-system skill")

	out, ok := res.StructuredOut.(designAssetsOutput)
	require.True(t, ok, "StructuredOut should be designAssetsOutput, got %T", res.StructuredOut)
	assert.False(t, out.Exists)
	assert.NotEmpty(t, out.Guidance, "a missing tree must carry scaffold guidance")
	assert.Contains(t, out.Guidance, "design/README.md")
	assert.Contains(t, out.Guidance, "design_validate")
	assert.Empty(t, out.Assets)
	assert.Empty(t, out.TokenGroups)
	assert.Empty(t, out.Flows)

	// The JSON shape must include exists and the always-present counters.
	data, err := json.Marshal(out)
	require.NoError(t, err)
	var raw map[string]any
	require.NoError(t, json.Unmarshal(data, &raw))
	assert.Equal(t, false, raw["exists"])
	assert.Contains(t, raw, "guidance")
	assert.Contains(t, raw, "assets")
	assert.Contains(t, raw, "tokenGroups")
	assert.Contains(t, raw, "flows")
	assert.Contains(t, raw, "manifest")
}

// TestDesignAssetsHandler_ForeignFolder pins the someone-else's-design-folder
// guidance: a design/ directory holding nothing in Sprout's vocabulary gets
// an inventory-and-ask instruction (never a silent empty roster, never
// scaffold text that implies the folder should be overwritten).
func TestDesignAssetsHandler_ForeignFolder(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, rel := range []string{"design/mockups/homepage.psd", "design/notes.docx", "design/export/v2.png"} {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, rel)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, rel), []byte("x"), 0o644))
	}
	h := &designAssetsHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out, ok := res.StructuredOut.(designAssetsOutput)
	require.True(t, ok)
	assert.True(t, out.Exists, "the folder exists — exists stays honest")
	assert.Empty(t, out.Assets)
	assert.NotEmpty(t, out.Guidance, "a foreign folder must carry inventory-and-ask guidance")
	assert.Contains(t, out.Guidance, "ask the user")
	assert.Contains(t, out.Guidance, "Never move")
}

func TestDesignAssetsHandler_ValidTreeInventory(t *testing.T) {	t.Parallel()
	root := t.TempDir()
	daWriteValidTree(t, root)
	h := &designAssetsHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Contains(t, res.Output, "asset(s)")
	require.Contains(t, res.Output, "No validator findings")

	out, ok := res.StructuredOut.(designAssetsOutput)
	require.True(t, ok)
	require.True(t, out.Exists)

	byPath := map[string]design.AssetRow{}
	for _, r := range out.Assets {
		byPath[r.Path] = r
	}
	require.Contains(t, byPath, "design/README.md")
	require.Contains(t, byPath, "design/wireframes/login.svg")
	require.Contains(t, byPath, "design/tokens/color.tokens.json")
	require.Contains(t, byPath, "design/flows/sign-up.mmd")
	assert.Equal(t, design.KindWireframe, byPath["design/wireframes/login.svg"].Kind)
	assert.Equal(t, "login", byPath["design/wireframes/login.svg"].Name)

	// Manifest summary.
	assert.True(t, out.Manifest.Exists)
	assert.Len(t, out.Manifest.Frames, 2)

	// Token group counts and flow node/edge counts.
	require.Len(t, out.TokenGroups, 1)
	assert.Equal(t, "color", out.TokenGroups[0].Group)
	assert.Equal(t, 2, out.TokenGroups[0].Tokens)

	require.Len(t, out.Flows, 1)
	assert.Equal(t, 2, out.Flows[0].Nodes)
	assert.Equal(t, 1, out.Flows[0].Edges)

	// Findings empty, bySeverity carries every key at zero.
	assert.Empty(t, out.Findings)
	for _, sev := range []string{"error", "warn", "info", "fix"} {
		assert.Equal(t, 0, out.BySeverity[sev], "severity %s", sev)
	}
}

func TestDesignAssetsHandler_CachedValidatorFindings(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	daWriteValidTree(t, root)
	// A dangling token alias (error) plus a raw hex brand (warn).
	daWrite(t, root, "design/tokens/bad.tokens.json",
		`{"a": {"$value": "{nope.missing}", "$type": "color"}}`)
	daWrite(t, root, "design/brand/brand.md", "Primary is #ff0000; use {color.brand.primary}.\n")
	h := &designAssetsHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{})
	require.NoError(t, err)
	require.False(t, res.IsError, "findings are advisory and never block a turn")

	out := res.StructuredOut.(designAssetsOutput)
	require.Len(t, out.Findings, 2)
	assert.Equal(t, 1, out.BySeverity["error"])
	assert.Equal(t, 1, out.BySeverity["warn"])
	require.Contains(t, res.Output, "Findings:")

	// Findings carry the standard {file, line?, severity, message, rule} shape.
	for _, f := range out.Findings {
		assert.NotEmpty(t, f.File)
		assert.NotEmpty(t, f.Severity)
		assert.NotEmpty(t, f.Rule)
		assert.NotEmpty(t, f.Message)
	}
}

func TestDesignAssetsHandler_SubtreeFilter(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	daWriteValidTree(t, root)
	h := &designAssetsHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{"path": "design/wireframes"})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := res.StructuredOut.(designAssetsOutput)
	require.NotEmpty(t, out.Assets)
	for _, r := range out.Assets {
		assert.True(t, strings.HasPrefix(r.Path, "design/wireframes/"),
			"subtree filter must restrict rows to the subtree, got %s", r.Path)
	}
	// Token groups and flows are irrelevant to the wireframes subtree.
	assert.Empty(t, out.TokenGroups)
	assert.Empty(t, out.Flows)
	// Findings always describe the whole tree.
	assert.Empty(t, out.Findings)
}

func TestDesignAssetsHandler_FormatFilter(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	daWriteValidTree(t, root)
	h := &designAssetsHandler{}

	for _, f := range []string{"flow", "flows"} {
		res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{"format": f})
		require.NoError(t, err)
		require.False(t, res.IsError)

		out := res.StructuredOut.(designAssetsOutput)
		require.NotEmpty(t, out.Assets)
		for _, r := range out.Assets {
			assert.Equal(t, design.KindFlow, r.Kind, "format=%s must restrict rows by kind", f)
		}
		require.Len(t, out.Flows, 1, "a flow filter keeps the flow counts")
		assert.Empty(t, out.TokenGroups, "a flow filter drops token groups")
	}
}

func TestDesignAssetsHandler_FormatFilterToken(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	daWriteValidTree(t, root)
	h := &designAssetsHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{"format": "tokens"})
	require.NoError(t, err)

	out := res.StructuredOut.(designAssetsOutput)
	require.Len(t, out.TokenGroups, 1)
	assert.Empty(t, out.Flows)
	for _, r := range out.Assets {
		assert.Equal(t, design.KindToken, r.Kind)
	}
}

func TestDesignAssetsHandler_PartialTreeStillExists(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	// A mid-scaffold tree with no README: design/ exists, so the inventory
	// must report exists:true (from the directory, not the manifest) and
	// still surface the assets it found.
	daWrite(t, root, "design/tokens/color.tokens.json", daTestTokenJSON)
	h := &designAssetsHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := res.StructuredOut.(designAssetsOutput)
	require.True(t, out.Exists, "a partial tree with only design/tokens must still report exists:true")
	require.Len(t, out.TokenGroups, 1)
	require.NotEmpty(t, out.Assets)
	assert.False(t, out.Manifest.Exists, "the manifest summary reports README absence independently")

	// The missing README surfaces as a validator finding, not a tool error.
	require.NotEmpty(t, out.Findings)
}

func TestDesignAssetsHandler_PathNotUnderDesign(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	daWriteValidTree(t, root)
	h := &designAssetsHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{"path": "src"})
	require.Error(t, err)
	require.True(t, res.IsError)
	require.Contains(t, res.Output, "not under design/")
}

func TestDesignAssetsHandler_Gate1Deny(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	daWriteValidTree(t, root)
	h := &designAssetsHandler{}

	env := newTestEnv(t, root)
	env.FileAccessClassifier = denyClassifier{}

	res, err := h.Execute(newTestCtx(root), env, map[string]any{"path": "design/wireframes"})
	require.Error(t, err)
	require.True(t, res.IsError, "a Gate-1 deny is a tool failure")
	require.Contains(t, res.Output, "design_assets blocked")
}

func TestDesignAssetsHandler_Gate1DenyOnImplicitRoot(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	daWriteValidTree(t, root)
	h := &designAssetsHandler{}

	env := newTestEnv(t, root)
	env.FileAccessClassifier = denyClassifier{}

	// With no path argument the tool reads the whole design/ tree, so the
	// canonical design/ path must be prechecked too (SP-140 invariant 7).
	res, err := h.Execute(newTestCtx(root), env, map[string]any{})
	require.Error(t, err)
	require.True(t, res.IsError, "a Gate-1 deny of design/ is a tool failure even without a path arg")
	require.Contains(t, res.Output, "design_assets blocked")
	require.Contains(t, res.Output, design.DirName)
}

// offWorkspaceClassifier is a Gate-1 test double for the production
// classifier (pkg/agent.Agent.ClassifyFileAccess): a resolved path is "allow"
// when it sits inside the workspace root it was built with, "prompt"
// otherwise (no session allowlist is configured, so there is no "deny" case to
// model). It cannot be the Agent itself — pkg/agent imports pkg/agent_tools,
// so the reverse import would cycle.
type offWorkspaceClassifier struct {
	root string
}

func (c offWorkspaceClassifier) ClassifyFileAccess(_ context.Context, filePath, resolvedPath, _ string) string {
	target := resolvedPath
	if target == "" {
		target = filePath
	}
	if c.underRoot(target) {
		return "allow"
	}
	return "prompt"
}

func (offWorkspaceClassifier) IsFolderSessionAllowed(_ string) bool { return false }

// underRoot reports containment of target in the workspace root, mirroring
// Agent.IsUnderWorkspaceRoot: both sides are symlink-resolved before the
// prefix check (this matters on macOS, where the temp root is reachable both
// as /var/... and /private/var/...). A candidate that does not exist cannot be
// symlink-resolved, so it falls back to its symlink-resolved parent plus the
// basename.
func (c offWorkspaceClassifier) underRoot(target string) bool {
	if c.root == "" || target == "" {
		return false
	}
	rel, err := filepath.Rel(resolveCandidate(c.root), resolveCandidate(target))
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func resolveCandidate(p string) string {
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	dir, base := filepath.Split(filepath.Clean(p))
	if resolvedDir, err := filepath.EvalSymlinks(filepath.Clean(dir)); err == nil {
		return filepath.Join(resolvedDir, base)
	}
	return filepath.Clean(p)
}

// TestDesignAssetsHandler_OffWorkspaceDenied is the SP-140-2 §2e negative
// test: a path outside design/ — whether outside the workspace entirely or a
// sensitive system path — is refused before `design.Scan` runs, so the tool can
// never walk (and report on) a tree it should not touch.
//
// design_assets' own design/ subtree guard is the enforcement point: the
// classifier still runs and records the verdict, but an off-design path is
// rejected without an inventory regardless of allow/prompt.
func TestDesignAssetsHandler_OffWorkspaceDenied(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	daWriteValidTree(t, root)

	// A sibling directory beside the workspace, used to build absolute
	// off-workspace paths.
	outsideRoot := t.TempDir()
	require.NotEqual(t, root, outsideRoot)

	env := newTestEnv(t, root)
	env.FileAccessClassifier = offWorkspaceClassifier{root: root}

	// The classifier's containment check must be live: an in-workspace
	// subtree is "allow" and an off-workspace path is "prompt". The
	// off-design refusal below is independent of the verdict — it holds for
	// both — but the two assertions pin the precheck to a single live run.
	inWorkspace, inDecision := PrecheckFileAccess(newTestCtx(root), env.FileAccessClassifier, "design_assets", "design/wireframes")
	require.Equal(t, "allow", inDecision, "an in-workspace subtree is allowed (resolved=%s)", inWorkspace)
	_, outDecision := PrecheckFileAccess(newTestCtx(root), env.FileAccessClassifier, "design_assets", "/etc/design/wireframes")
	require.Equal(t, "prompt", outDecision, "an off-workspace sensitive path prompts")

	h := &designAssetsHandler{}

	for _, path := range []string{
		filepath.Join(outsideRoot, "design", "wireframes"), // absolute, outside the workspace
		"/etc/design/wireframes",                           // sensitive system path
	} {
		res, err := h.Execute(newTestCtx(root), env, map[string]any{"path": path})
		require.Error(t, err, "path %q must fail the tool", path)
		require.True(t, res.IsError, "path %q must be a tool failure", path)
		require.Contains(t, res.Output, "not under design/",
			"path %q must be refused as an off-design path, got: %s", path, res.Output)
		_, ok := res.StructuredOut.(designAssetsOutput)
		assert.False(t, ok, "a refused path must not produce structured output")
	}
}

func TestDesignAssetsHandler_RootPathKeepsCounts(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	daWriteValidTree(t, root)
	h := &designAssetsHandler{}

	// path: "design" is the whole tree: it must keep both token groups and
	// flow counts.
	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{"path": "design"})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := res.StructuredOut.(designAssetsOutput)
	require.Len(t, out.TokenGroups, 1, "the design/ root keeps token groups")
	require.Len(t, out.Flows, 1, "the design/ root keeps flow counts")
	require.NotEmpty(t, out.Assets)
}

func TestDesignAssetsHandler_Gate1AllowResolvesPath(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	daWriteValidTree(t, root)
	h := &designAssetsHandler{}

	env := newTestEnv(t, root)
	env.FileAccessClassifier = allowClassifier{}

	// The allow verdict's resolved absolute path must map back to the
	// workspace-relative subtree the filter expects.
	res, err := h.Execute(newTestCtx(root), env, map[string]any{"path": "design/wireframes"})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := res.StructuredOut.(designAssetsOutput)
	require.NotEmpty(t, out.Assets)
	for _, r := range out.Assets {
		assert.True(t, strings.HasPrefix(r.Path, "design/wireframes/"))
	}
}

func TestDesignAssetsHandler_NoWorkspaceRoot(t *testing.T) {
	t.Parallel()
	// env.WorkspaceRoot empty falls back to "." — the run must not panic
	// whatever the process cwd holds.
	h := &designAssetsHandler{}

	env := ToolEnv{
		EventBus:      events.NewEventBus(),
		OutputWriter:  os.Stderr,
		WorkspaceRoot: "",
	}
	res, err := h.Execute(newTestCtx("."), env, map[string]any{})
	require.NoError(t, err)
	require.False(t, res.IsError)
}

func TestDesignAssetsHandler_ReadOnly(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	daWriteValidTree(t, root)
	h := &designAssetsHandler{}

	before := map[string]string{}
	require.NoError(t, filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			before[path] = string(data)
		}
		return nil
	}))

	_, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{})
	require.NoError(t, err)

	after := map[string]string{}
	require.NoError(t, filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			after[path] = string(data)
		}
		return nil
	}))

	require.Equal(t, before, after, "design_assets must never write to the tree")
}

func TestDesignAssetsHandler_IsDesignSubtree(t *testing.T) {
	t.Parallel()
	require.True(t, isDesignSubtree("design"))
	require.True(t, isDesignSubtree("design/wireframes"))
	require.True(t, isDesignSubtree("design/wireframes/"))
	require.True(t, isDesignSubtree("./design/tokens"))
	require.False(t, isDesignSubtree("designer"))
	require.False(t, isDesignSubtree("src/design"))
}

func TestNormaliseFormatFilter(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"":           "",
		"tokens":     design.KindToken,
		"TOKEN":      design.KindToken,
		"wireframes": design.KindWireframe,
		"Wireframe":  design.KindWireframe,
		"screens":    design.KindScreen,
		"flows":      design.KindFlow,
		"icons":      design.KindIcon,
		"manifest":   design.KindManifest,
		"component":  "component",
	}
	for in, want := range cases {
		assert.Equal(t, want, normaliseFormatFilter(in), "normaliseFormatFilter(%q)", in)
	}
}

// TestDesignAssetsHandler_PendingFeedback reports the SP-140-4 §4d pending
// feedback targets with counts (item 4.7): a fixture design/feedback/<target>.json
// with status changes-requested + an unresolved annotation shows up as pending
// with the right annotation/resolved/pending counts, an unresolved annotation
// alone is pending even when the status is not changes-requested, and a
// resolved/no-pending file does not appear in the pending list.
func TestDesignAssetsHandler_PendingFeedback(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	daWriteValidTree(t, root)
	daWrite(t, root, "design/feedback/login.json", daFeedbackPendingJSON)
	daWrite(t, root, "design/feedback/home.json", daFeedbackPartialJSON)
	daWrite(t, root, "design/feedback/sign-up.json", daFeedbackResolvedJSON)
	h := &designAssetsHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := res.StructuredOut.(designAssetsOutput)
	require.True(t, out.Exists)

	// Every feedback file is reported in All (sorted by path), the pending
	// subset in Pending.
	require.Len(t, out.Feedback.All, 3)
	assert.Equal(t, "design/feedback/home.json", out.Feedback.All[0].Path)
	assert.Equal(t, "design/feedback/login.json", out.Feedback.All[1].Path)
	assert.Equal(t, "design/feedback/sign-up.json", out.Feedback.All[2].Path)

	// login (changes-requested, 1 unresolved) and home (1 of 3 unresolved)
	// are pending; sign-up (resolved, no unresolved) is not.
	require.Equal(t, 2, out.Feedback.PendingCount)
	require.Len(t, out.Feedback.Pending, 2)
	byTarget := map[string]design.FeedbackFileState{}
	for _, p := range out.Feedback.Pending {
		byTarget[p.Target] = p
	}
	login, ok := byTarget["design/wireframes/login.svg"]
	require.True(t, ok, "the changes-requested login target must be pending")
	assert.Equal(t, design.FeedbackStatusChangesRequested, login.Status)
	assert.Equal(t, 1, login.Annotations)
	assert.Equal(t, 0, login.Resolved)
	assert.Equal(t, 1, login.Pending)

	home, ok := byTarget["design/wireframes/home.svg"]
	require.True(t, ok, "an unresolved annotation keeps the target pending")
	assert.Equal(t, 3, home.Annotations)
	assert.Equal(t, 2, home.Resolved)
	assert.Equal(t, 1, home.Pending)

	// The resolved file must not be pending.
	for _, p := range out.Feedback.Pending {
		assert.NotEqual(t, "design/flows/sign-up.mmd", p.Target, "a resolved file must not be pending")
	}

	// The summary names the pending targets and their unresolved counts.
	assert.Contains(t, res.Output, "Pending feedback: 2 target(s)")
	assert.Contains(t, res.Output, "design/wireframes/login.svg (1 unresolved)")
	assert.Contains(t, res.Output, "design/wireframes/home.svg (1 unresolved)")
	assert.Contains(t, res.Output, "read of its feedback file")
}

// TestDesignAssetsHandler_NoPendingFeedback pins the negative: a tree with no
// feedback directory (and with a resolved feedback file) reports an empty,
// non-nil pending section and says so in the summary.
func TestDesignAssetsHandler_NoPendingFeedback(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	daWriteValidTree(t, root)
	h := &designAssetsHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{})
	require.NoError(t, err)

	out := res.StructuredOut.(designAssetsOutput)
	assert.Equal(t, 0, out.Feedback.PendingCount)
	assert.Empty(t, out.Feedback.Pending)
	assert.Empty(t, out.Feedback.All)
	assert.Contains(t, res.Output, "No pending feedback.")

	// A resolved feedback file is reported in All but not Pending.
	daWrite(t, root, "design/feedback/sign-up.json", daFeedbackResolvedJSON)
	res, err = h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{})
	require.NoError(t, err)
	out = res.StructuredOut.(designAssetsOutput)
	assert.Equal(t, 0, out.Feedback.PendingCount)
	assert.Empty(t, out.Feedback.Pending)
	require.Len(t, out.Feedback.All, 1)
	assert.Contains(t, res.Output, "No pending feedback.")
}

// TestDesignAssetsHandler_FeedbackSectionIsStableJSON pins the JSON shape the
// skill loop reads: the feedback section is always present with non-nil
// `pending`/`all` arrays (so `pending: []` reads as "nothing to address").
func TestDesignAssetsHandler_FeedbackSectionIsStableJSON(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	daWriteValidTree(t, root)
	h := &designAssetsHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{})
	require.NoError(t, err)
	out := res.StructuredOut.(designAssetsOutput)

	data, err := json.Marshal(out)
	require.NoError(t, err)
	var raw map[string]any
	require.NoError(t, json.Unmarshal(data, &raw))
	fb, ok := raw["feedback"].(map[string]any)
	require.True(t, ok, "feedback must be present as an object")
	_, isSlice := fb["pending"].([]any)
	assert.True(t, isSlice, "feedback.pending must serialize as an array, got %T", fb["pending"])
	_, isSlice = fb["all"].([]any)
	assert.True(t, isSlice, "feedback.all must serialize as an array, got %T", fb["all"])
	assert.Contains(t, fb, "pendingCount")
	assert.Equal(t, float64(0), fb["pendingCount"])
}

// TestDesignAssetsHandler_FeedbackSurvivesFormatFilter keeps the pending
// section whole-tree under a format filter: findings and pending feedback are
// inventory-level summaries (the skill loop's actionable list), so narrowing
// the asset kinds must never hide pending human feedback.
func TestDesignAssetsHandler_FeedbackSurvivesFormatFilter(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	daWriteValidTree(t, root)
	daWrite(t, root, "design/feedback/login.json", daFeedbackPendingJSON)
	h := &designAssetsHandler{}

	for _, format := range []string{"", "wireframe", "feedback"} {
		res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{"format": format})
		require.NoError(t, err)
		out := res.StructuredOut.(designAssetsOutput)
		assert.Equal(t, 1, out.Feedback.PendingCount, "format=%q must keep the pending-feedback report", format)
		require.Len(t, out.Feedback.Pending, 1)
		assert.Contains(t, res.Output, "Pending feedback: 1 target(s)")
	}
}

// compile-time interface check.
var _ ToolHandler = (*designAssetsHandler)(nil)

// TestDesignAssetsIsInSharedRoster pins the SP-140-2 §2c claim that
// design_assets is a pure-Go inventory tool registered in the shared AllTools
// list (like design_validate) rather than a build-tagged registration: it
// needs no browser or vision tier, so it is available to native and WASM
// builds alike. The WASM roster smoke test (item 2.10) builds on this.
func TestDesignAssetsIsInSharedRoster(t *testing.T) {
	t.Parallel()
	found := false
	for _, h := range AllTools() {
		if h.Name() == "design_assets" {
			found = true
			break
		}
	}
	require.True(t, found, "design_assets must be registered in the shared AllTools list")
}
