package tools

import (
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

// ---------------------------------------------------------------------------
// design_validate handler fixtures
// ---------------------------------------------------------------------------

const dvTestManifest = `# Design Workspace

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
- [Login screen](screens/login.html)
`

const dvTestTokenJSON = `{
  "color": {
    "brand": {
      "primary": {"$value": "#0055ff", "$type": "color"}
    },
    "semantic": {
      "surface": {"$value": "#ffffff", "$type": "color"}
    }
  }
}`

// The literal fill is backed by a {token.path} comment (SP-140-4 §4b "Token
// usage"), so the clean fixture also satisfies the token-usage rule: literal
// values are allowed when the intended token is recorded alongside.
// dvTestLoginScreen/dvTestHomeScreen are the §9a primary-tier screens the
// valid fixture ships: HTML with the kit's data attributes (identity,
// runtime, states) so the tree validates clean under SP-140-9 — wireframes
// are the deprecated tier and would earn deprecation infos instead.
const dvTestLoginScreen = `<!doctype html>
<html lang="en" data-screen="login" data-sprout-screens="1" data-states="">
<head><meta charset="utf-8"><title>login</title></head>
<body data-sprout-screen="login">
  <h1>Login</h1>
  <a href="../screens/home.html" data-nav="to:home;trigger:submit">Sign in</a>
  <script src="../runtime/sprout-screens.js" defer></script>
</body>
</html>`

const dvTestHomeScreen = `<!doctype html>
<html lang="en" data-screen="home" data-sprout-screens="1" data-states="">
<head><meta charset="utf-8"><title>home</title></head>
<body data-sprout-screen="home">
  <h1>Home</h1>
  <script src="../runtime/sprout-screens.js" defer></script>
</body>
</html>`

const dvTestFlowMMD = "flowchart TD\n  login --> home\n"

// dvWrite writes rel (slash-separated) under root with parent directories.
func dvWrite(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

// dvWriteValidTree populates root with a small design/ tree that yields zero
// findings on a whole-tree run, including the §1h git contract lines.
func dvWriteValidTree(t *testing.T, root string) {
	t.Helper()
	dvWrite(t, root, "design/README.md", dvTestManifest)
	dvWrite(t, root, "design/tokens/color.tokens.json", dvTestTokenJSON)
	dvWrite(t, root, "design/screens/login.html", dvTestLoginScreen)
	dvWrite(t, root, "design/screens/home.html", dvTestHomeScreen)
	dvWrite(t, root, "design/generated/screens.json", dvTestScreensIndex(root))
	writeTestRuntimeAsset(t, root)
	writeDerivedFlows(t, root)
	dvWrite(t, root, design.GitContractFile, "* text=auto eol=lf\n"+design.GitAttributesDiffHTMLLine+"\n")
	dvWrite(t, root, design.GitIgnoreFile, "node_modules/\n"+design.GitIgnoreCacheLine+"\n")
}

// dvValidTreeWireframeInfos is the count of wireframe deprecation infos
// the valid fixture earns under SP-140-9 §9a (info severity: the tier is
// deprecated pending 9.4's migration; a warn would hold every pre-migration
// tree in a perpetual advisory state).

const dvTestLoginSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844">
  <text x="24" y="64" font-size="28">Login</text>
  <rect id="submit" fill="#fff" x="24" y="200" width="342" height="52" data-nav="home"><!-- {color.semantic.surface} --></rect>
</svg>`

const dvTestHomeSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844">
  <text x="24" y="64" font-size="28">Home</text>
</svg>`

// dvAddLegacyWireframes adds the two legacy wireframes to a valid tree —
// for tests that pin wireframe-rule behavior itself (data-nav, data-URI
// size). Under §9a each earns a deprecation info; those tests assert on
// specific rules, not clean trees, so the extra infos are fine.
func dvAddLegacyWireframes(t *testing.T, root string) {
	t.Helper()
	dvWrite(t, root, "design/wireframes/login.svg", dvTestLoginSVG)
	dvWrite(t, root, "design/wireframes/home.svg", dvTestHomeSVG)
}

// dvTestScreensIndex derives the generated screens index over the fixture's
// two screens (the 143.5 export pipeline) so the valid tree carries a
// non-drifted index.

// writeTestRuntimeAsset scaffolds the embedded runtime kit into the fixture
// tree so kit-member screens satisfy the runtime-hash rule.
func writeTestRuntimeAsset(t *testing.T, root string) {
	t.Helper()
	if err := design.ScaffoldRuntimeAssets(filepath.Join(root, design.DirName)); err != nil {
		t.Fatalf("runtime asset: %v", err)
	}
}

func dvTestScreensIndex(root string) string {
	art, err := design.RenderScreensIndexArtifact(root)
	if err != nil {
		panic(err)
	}
	return string(art.Content)
}

// writeDerivedFlows seeds the flow tier's derived form: the .json source plus
// the generator-emitted .mmd (provenance header included), so the fixture is
// clean under SP-140-9 §9b (hand-authored .mmd earns a legacy info).
func writeDerivedFlows(t *testing.T, root string) {
	t.Helper()
	dvWrite(t, root, "design/flows/sign-up.json", dvTestFlowSourceJSON)
	arts, err := design.RenderAllFlowMDMArtifacts(root)
	if err != nil {
		t.Fatalf("render derived flows: %v", err)
	}
	for _, a := range arts {
		dvWrite(t, root, a.RelPath, string(a.Content))
	}
}

const dvTestFlowSourceJSON = `{
  "name": "sign-up",
  "steps": [
    {"id": "s1", "label": "Start", "screen": "login"},
    {"id": "s2", "label": "Signed in", "screen": "home", "trigger": "submit", "next": "s3"},
    {"id": "s3", "label": "Done", "screen": "home"}
  ]
}`

// dvValidTreeWireframeInfos is kept for the wireframe-deprecation tests
// that assert the info channel directly.
const dvValidTreeWireframeInfos = 2

// ---------------------------------------------------------------------------
// design_validate tests
// ---------------------------------------------------------------------------

func TestDesignValidateHandler_NameAndDefinition(t *testing.T) {
	t.Parallel()
	h := &designValidateHandler{}

	require.Equal(t, "design_validate", h.Name())

	def := h.Definition()
	require.Equal(t, "design_validate", def.Name)
	require.NotEmpty(t, def.Description)
	require.Contains(t, def.Description, "tokens")
	require.Contains(t, def.Description, "wireframes")
	require.Empty(t, def.Required, "path must be optional — the tool is runnable with no args")

	paramNames := make(map[string]bool)
	for _, p := range def.Parameters {
		paramNames[p.Name] = true
		if p.Name == "path" {
			require.False(t, p.Required, "the path parameter must not be required")
			require.Equal(t, "string", p.Type)
		}
	}
	require.True(t, paramNames["path"], "should have a 'path' parameter")
}

func TestDesignValidateHandler_Metadata(t *testing.T) {
	t.Parallel()
	h := &designValidateHandler{}

	require.Nil(t, h.Aliases())
	require.Equal(t, designValidateTimeout, h.Timeout())
	require.Equal(t, 0, h.MaxResultSize())
	require.True(t, h.SafeForParallel(), "design_validate is read-only and safe for parallel execution")
	require.False(t, h.Interactive())
}

func TestDesignValidateHandler_Validate(t *testing.T) {
	t.Parallel()
	h := &designValidateHandler{}

	// No args, empty args, and a string path are all valid.
	require.NoError(t, h.Validate(nil))
	require.NoError(t, h.Validate(map[string]any{}))
	require.NoError(t, h.Validate(map[string]any{"path": "design/wireframes/login.svg"}))

	// path must be a string when present.
	err := h.Validate(map[string]any{"path": 42})
	require.Error(t, err)
	require.Contains(t, err.Error(), "must be a string")
}

func TestDesignValidateHandler_NoArgsValidTree(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dvWriteValidTree(t, root)
	h := &designValidateHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{})
	require.NoError(t, err)
	require.False(t, res.IsError, "findings are advisory; a clean run must never be an error")
	// The fixture's flows are §9b-derived (source .json + regenerated
	// export), so the valid tree validates fully clean.
	require.Contains(t, res.Output, "design_validate: 0 findings")

	// StructuredOut carries findings + count + bySeverity.
	out, ok := res.StructuredOut.(findingsOutput)
	require.True(t, ok, "StructuredOut should be findingsOutput, got %T", res.StructuredOut)
	require.Equal(t, 0, out.Count)
	require.Empty(t, out.Findings)
	require.Equal(t, 0, out.BySeverity["error"])
	require.Equal(t, 0, out.BySeverity["warn"])
	require.Equal(t, 0, out.BySeverity["info"])
}

func TestDesignValidateHandler_NoArgsNoDesignDir(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	h := &designValidateHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{})
	require.NoError(t, err)
	require.False(t, res.IsError, "a missing design/ is a scaffold hint, not a failure")
	require.Contains(t, res.Output, "No design/")
	require.Contains(t, res.Output, "scaffold")

	out, ok := res.StructuredOut.(findingsOutput)
	require.True(t, ok)
	require.Equal(t, 0, out.Count)
}

func TestDesignValidateHandler_NoArgsSeededBadTree(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dvWriteValidTree(t, root)
	dvAddLegacyWireframes(t, root)
	// Seed a bad wireframe: a dangling data-nav target. The stem is new, so
	// declare it in the README's Screens listing to keep it out of the §4b
	// orphan rule and let this fixture pin the dangling data-nav class alone.
	dvWrite(t, root, "design/wireframes/signup.svg",
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><text x="0" y="0">Sign up</text><rect id="go" data-nav="nowhere" /></svg>`)
	dvWrite(t, root, "design/README.md", strings.Replace(dvTestManifest,
		"## Flows", "- `signup` — draft — seeded\n\n## Flows", 1))
	h := &designValidateHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{})
	require.NoError(t, err)
	// Advisory semantics: findings never block a turn.
	require.False(t, res.IsError, "error-severity findings must not set IsError — findings are advisory (SP-140-1 §1g)")
	require.Contains(t, res.Output, "4 finding(s)")
	require.Contains(t, res.Output, "1 error(s)")
	require.Contains(t, res.Output, "3 info(s)")

	out, ok := res.StructuredOut.(findingsOutput)
	require.True(t, ok)
	require.Equal(t, 4, out.Count)
	require.Equal(t, 1, out.BySeverity["error"])
	require.Equal(t, 3, out.BySeverity["info"])
	byRule := map[string]findingOut{}
	for _, f := range out.Findings {
		byRule[f.Rule] = f
	}
	bad := byRule["svg_data_nav_dangling"]
	require.Equal(t, "design/wireframes/signup.svg", bad.File)
	require.Equal(t, "error", bad.Severity)
	require.Equal(t, "info", byRule["wireframe_deprecated"].Severity)
	require.NotEmpty(t, bad.Message)
}

func TestDesignValidateHandler_StructuredOutJSONShape(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dvWriteValidTree(t, root)
	// A brand.md with a raw hex color produces a warn with no line number.
	dvWrite(t, root, "design/brand/brand.md", "Primary is #ff0000 and accent is {color.brand.secondary}.\n")
	h := &designValidateHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := res.StructuredOut.(findingsOutput)
	// One brand_raw_hex warn; the derived-flow fixture adds nothing else.
	require.Equal(t, 1, out.Count)
	require.Equal(t, 1, out.BySeverity["warn"])
	require.Equal(t, 0, out.BySeverity["info"])

	// The structured output must be JSON-friendly, with "line" omitted at 0
	// on the warn (the legacy info anchors on no line either).
	data, err := json.Marshal(out)
	require.NoError(t, err)
	var raw struct {
		Findings []map[string]any `json:"findings"`
	}
	require.NoError(t, json.Unmarshal(data, &raw))
	require.Len(t, raw.Findings, 1)
	for _, f := range raw.Findings {
		require.NotContains(t, f, "line", "line must be omitted when 0")
		require.Contains(t, f, "file")
		require.Contains(t, f, "severity")
		require.Contains(t, f, "message")
		require.Contains(t, f, "rule")
	}
}

func TestDesignValidateHandler_PathArgGoodAsset(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dvWriteValidTree(t, root)
	h := &designValidateHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root),
		map[string]any{"path": "design/screens/login.html"})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Contains(t, res.Output, "0 findings")

	out := res.StructuredOut.(findingsOutput)
	require.Equal(t, 0, out.Count)
}

func TestDesignValidateHandler_PathArgDesignPrefixOptional(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dvWriteValidTree(t, root)
	h := &designValidateHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root),
		map[string]any{"path": "screens/login.html"})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := res.StructuredOut.(findingsOutput)
	require.Equal(t, 0, out.Count)
}

func TestDesignValidateHandler_PathArgBadAsset(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dvWriteValidTree(t, root)
	dvWrite(t, root, "design/tokens/bad.tokens.json",
		`{"a": {"$value": "{nope.missing}", "$type": "color"}}`)
	h := &designValidateHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root),
		map[string]any{"path": "design/tokens/bad.tokens.json"})
	require.NoError(t, err)
	require.False(t, res.IsError, "rule violations are findings, not tool errors")
	require.Contains(t, res.Output, "1 finding(s)")

	out := res.StructuredOut.(findingsOutput)
	require.Equal(t, 1, out.Count)
	require.Equal(t, "token_alias_dangling", out.Findings[0].Rule)
	require.Equal(t, "design/tokens/bad.tokens.json", out.Findings[0].File)
}

func TestDesignValidateHandler_PathArgREADME(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dvWrite(t, root, "design/README.md", "# Design\n\nNo frames block, no markers.\n\n- [Missing](tokens/nope.tokens.json)\n")
	h := &designValidateHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{"path": "design/README.md"})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := res.StructuredOut.(findingsOutput)
	require.True(t, out.Count >= 2, "expected frames + dangling-link findings, got %#v", out)
	require.Equal(t, 2, out.BySeverity["error"])
}

func TestDesignValidateHandler_PathArgUnrecognized(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dvWrite(t, root, "design/notes.txt", "not a design asset")
	h := &designValidateHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{"path": "design/notes.txt"})
	require.Error(t, err)
	require.True(t, res.IsError, "an unrecognized design asset is a tool error, not a finding")
	require.Contains(t, res.Output, "not a recognized design asset")
}

func TestDesignValidateHandler_PathArgMissing(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	h := &designValidateHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root),
		map[string]any{"path": "design/wireframes/nope.svg"})
	require.Error(t, err)
	require.True(t, res.IsError)
}

func TestDesignValidateHandler_EmptyPathMeansWholeTree(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dvWriteValidTree(t, root)
	h := &designValidateHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{"path": "   "})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Contains(t, res.Output, "0 findings")
}

func TestDesignValidateHandler_Gate1Deny(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dvWriteValidTree(t, root)
	h := &designValidateHandler{}

	env := newTestEnv(t, root)
	env.FileAccessClassifier = denyClassifier{}

	res, err := h.Execute(newTestCtx(root), env, map[string]any{"path": "design/wireframes/login.svg"})
	require.Error(t, err)
	require.True(t, res.IsError, "a Gate-1 deny is a tool failure, not a finding")
	require.Contains(t, res.Output, "design_validate blocked")
}

func TestDesignValidateHandler_Gate1AllowResolvesPath(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dvWriteValidTree(t, root)
	h := &designValidateHandler{}

	env := newTestEnv(t, root)
	env.FileAccessClassifier = allowClassifier{}

	res, err := h.Execute(newTestCtx(root), env, map[string]any{"path": "design/screens/login.html"})
	require.NoError(t, err)
	require.False(t, res.IsError)
	// The allow verdict's absolute resolved path must be converted back to
	// the workspace-relative form ValidateFile expects.
	require.Contains(t, res.Output, "0 findings")
}

func TestDesignValidateHandler_NoArgsNoWorkspaceRoot(t *testing.T) {
	t.Parallel()
	// env.WorkspaceRoot empty falls back to "." — the run must not panic
	// whatever the process cwd holds: a missing design/ is not an error.
	h := &designValidateHandler{}

	env := ToolEnv{
		EventBus:      events.NewEventBus(),
		OutputWriter:  os.Stderr,
		WorkspaceRoot: "",
	}
	res, err := h.Execute(newTestCtx("."), env, map[string]any{})
	require.NoError(t, err)
	require.False(t, res.IsError)
}

func TestDesignValidateHandler_ReadOnly(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dvWriteValidTree(t, root)
	h := &designValidateHandler{}

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

	require.Equal(t, before, after, "design_validate must never write to the tree")
}

// ---------------------------------------------------------------------------
// §4b inventory & naming consistency pack (SP-140-4 item 4.5)
// ---------------------------------------------------------------------------

// TestDesignValidateHandler_ConsistencyPackFindings proves design_validate (a
// whole-tree run) surfaces the §4b inventory/naming/token rules introduced by
// SP-140-4 item 4.5, each with its specified severity: literal token usage
// (info), an orphan screen (info), and a screen name mismatch (warn).
func TestDesignValidateHandler_ConsistencyPackFindings(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dvWriteValidTree(t, root)
	// Token usage + orphan: a wireframe with a literal fill and no token
	// comment, declared nowhere.
	dvWrite(t, root, "design/wireframes/billing.svg",
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><text fill="#ff0000">Billing</text></svg>`)
	// Naming mismatch (§9a transitional, info): a delivered screen with no
	// wireframe counterpart. Identity attribute keeps the identity rule
	// quiet so this fixture pins the counterpart rule alone.
	dvWrite(t, root, "design/screens/receipt.html",
		`<!DOCTYPE html><html data-screen="receipt"><head><style>body{width:390px}</style></head><body>x</body></html>`)
	// The index must cover the new screen or drift becomes the loud error.
	dvWrite(t, root, "design/generated/screens.json", dvTestScreensIndex(root))
	h := &designValidateHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{})
	require.NoError(t, err)
	require.False(t, res.IsError, "consistency findings are advisory — they never block a turn")

	out := res.StructuredOut.(findingsOutput)
	require.Equal(t, 0, out.BySeverity["warn"], "no warn on this tree under §9a", out.Findings)
	require.GreaterOrEqual(t, out.BySeverity["info"], 2, "token usage + orphan are infos, got %#v", out.Findings)

	byRule := map[string]string{}
	for _, f := range out.Findings {
		byRule[f.Rule] = f.Severity
	}
	require.Equal(t, "info", byRule["svg_token_usage"], "got %#v", out.Findings)
	require.Equal(t, "info", byRule["consistency_screen_orphan"], "got %#v", out.Findings)
	require.Equal(t, "info", byRule["consistency_screen_name_mismatch"], "the §9a transitional severity, got %#v", out.Findings)
}

func TestDesignValidateHandler_MultipleFindingsTallies(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dvWriteValidTree(t, root)
	// One bad token file (dangling alias) and one bad brand.md (raw hex →
	// warn) produce findings in two severity classes.
	dvWrite(t, root, "design/tokens/bad.tokens.json",
		`{"a": {"$value": "{nope.missing}", "$type": "color"}}`)
	dvWrite(t, root, "design/brand/brand.md", "Primary is #ff0000; use {color.brand.primary}.\n")
	h := &designValidateHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := res.StructuredOut.(findingsOutput)
	// The two seeded findings; the derived-flow fixture adds nothing.
	require.Equal(t, 2, out.Count)
	require.Equal(t, 1, out.BySeverity["error"])
	require.Equal(t, 1, out.BySeverity["warn"])
	require.Equal(t, 0, out.BySeverity["info"])
	require.Contains(t, res.Output, "2 finding(s)")
	require.Contains(t, res.Output, "1 error(s)")
	require.Contains(t, res.Output, "1 warn(s)")
}

// ---------------------------------------------------------------------------
// git contract (§1h)
// ---------------------------------------------------------------------------

func TestDesignValidateHandler_GitContractFixFindings(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	// A complete, valid tree so only the git contract is at issue.
	dvWriteValidTree(t, root)
	// The repo already carries .gitattributes rules that must be preserved.
	dvWrite(t, root, ".gitattributes", "* text=auto eol=lf\n*.go text eol=lf\n")
	dvWrite(t, root, ".gitignore", "node_modules/\n")
	h := &designValidateHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{})
	require.NoError(t, err)
	require.False(t, res.IsError, "fix findings are advisory — they never block a turn")

	out := res.StructuredOut.(findingsOutput)
	// The two git-contract fixes; the derived-flow fixture adds nothing.
	require.Len(t, out.Findings, 2, "expected the .gitattributes + .gitignore fixes, got %#v", out.Findings)
	require.Equal(t, 2, out.BySeverity["fix"])
	require.Equal(t, 0, out.BySeverity["info"])
	require.Contains(t, res.Output, "2 fix(es) to apply")
	require.Equal(t, 0, out.BySeverity["error"])

	byFile := map[string]findingOut{}
	for _, f := range out.Findings {
		byFile[f.File] = f
		assert.Equal(t, "fix", f.Severity)
	}
	attrs, ok := byFile[".gitattributes"]
	require.True(t, ok, "expected a .gitattributes finding, got %#v", out.Findings)
	assert.Equal(t, "gitattributes_diff_html_fix", attrs.Rule)
	assert.Contains(t, attrs.Message, "design/**/*.svg diff=html",
		"the fix must carry the exact line to append")

	ignore, ok := byFile[".gitignore"]
	require.True(t, ok, "expected a .gitignore finding, got %#v", out.Findings)
	assert.Equal(t, "gitignore_design_cache_fix", ignore.Rule)
	assert.Contains(t, ignore.Message, "design/.cache/")

	// The validator must not have modified the repo files.
	attrsBody, err := os.ReadFile(filepath.Join(root, ".gitattributes"))
	require.NoError(t, err)
	require.Equal(t, "* text=auto eol=lf\n*.go text eol=lf\n", string(attrsBody),
		"design_validate must report the fix, never apply it")
}

func TestDesignValidateHandler_GitContractSatisfied(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dvWriteValidTree(t, root)
	h := &designValidateHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := res.StructuredOut.(findingsOutput)
	// The satisfied contract clears every git finding; the fixture's flows
	// are §9b-derived (source .json + regenerated export), so the tree is
	// fully clean — no legacy notices either.
	require.Equal(t, 0, out.Count)
	require.Equal(t, 0, out.BySeverity["fix"])
	require.Equal(t, 0, out.BySeverity["error"])
}

func TestDesignValidateHandler_PathArgGitContractFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dvWriteValidTree(t, root)
	dvWrite(t, root, ".gitattributes", "*.png binary\n")
	h := &designValidateHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{"path": ".gitattributes"})
	require.NoError(t, err, "the repository-level git-contract files are valid path arguments")
	require.False(t, res.IsError)

	out := res.StructuredOut.(findingsOutput)
	require.Equal(t, 1, out.Count)
	require.Equal(t, ".gitattributes", out.Findings[0].File)
	require.Equal(t, "gitattributes_diff_html_fix", out.Findings[0].Rule)
	require.Equal(t, "fix", out.Findings[0].Severity)
}

func TestDesignValidateHandler_DataURISizeWarn(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dvWriteValidTree(t, root)
	dvAddLegacyWireframes(t, root)
	// An oversized embedded raster keeps the SVG self-contained but is a warn.
	dvWrite(t, root, "design/wireframes/photo.svg",
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><text x="0" y="0">Photo</text><image href="data:image/png;base64,`+
			strings.Repeat("A", (1<<20)+8)+`" /></svg>`)
	h := &designValidateHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{"path": "design/wireframes/photo.svg"})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := res.StructuredOut.(findingsOutput)
	require.Equal(t, 2, out.Count)
	byRule := map[string]findingOut{}
	for _, f := range out.Findings {
		byRule[f.Rule] = f
	}
	require.Equal(t, "svg_data_uri_size", byRule["svg_data_uri_size"].Rule)
	require.Equal(t, "warn", byRule["svg_data_uri_size"].Severity)
	require.Equal(t, "info", byRule["wireframe_deprecated"].Severity)
	require.Contains(t, out.Findings[0].Message, "brand/")
}

// compile-time interface check + import guard for the design package.
var (
	_ ToolHandler = (*designValidateHandler)(nil)
	_             = design.DirName
)

// designValidateTimeout mirrors the handler's timeout so the metadata test
// asserts against a named constant rather than a magic number.
const designValidateTimeout = 60 * time.Second
