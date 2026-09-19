//go:build !js

package tools

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sprout-foundry/sprout/pkg/design"
)

// ---------------------------------------------------------------------------
// design_brief handler tests (SP-140-5 §5g, TODO item 5.8)
//
// The pure brief core is unit tested in pkg/design/brief_test.go. These tests
// cover the ToolHandler seam: definition/validate, argument resolution (the
// required screen_name, the depth default, path/extension reduction), the
// Gate-1 precheck (including off-workspace denial), the structured brief at
// summary and full depth, the not-found (not error) result, and the §5g
// hard contract that the tool WRITES NO FILES.
// ---------------------------------------------------------------------------

// dbSeedTree seeds a design/ tree for the screen `login` (mirrors the pure
// core's fixture, kept small here). Uses the shared dsWrite helper.
func dbSeedTree(t *testing.T, root string) {
	t.Helper()
	dsWrite(t, root, "design/README.md", "# Design Workspace\n\n## Screens\n\n"+
		"- `login` — draft — sign-in entry point with credential recovery\n"+
		"- `home` — ready — message list\n\n## Flows\n\n- `sign-up` — draft — account creation\n")
	dsWrite(t, root, "design/tokens/color.tokens.json",
		`{"color":{"brand":{"primary":{"$type":"color","$value":"#0055ff"}}}}`)
	dsWrite(t, root, "design/wireframes/login.svg",
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844">`+
			`<!-- {color.brand.primary} --><rect data-nav="home"/><text>Sign in</text></svg>`)
	dsWrite(t, root, "design/wireframes/home.svg",
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><text>Home</text></svg>`)
	dsWrite(t, root, "design/flows/sign-up.mmd",
		"flowchart TD\n  login -- \"tap Submit\" --> home\n")
	dsWrite(t, root, "design/feedback/login.json",
		`{"target":"design/wireframes/login.svg","status":"changes-requested","resolution":"","annotations":[`+
			`{"id":"a1","at":{"x":0.4,"y":0.2},"area":"hierarchy","note":"Primary CTA reads as secondary","resolved":false,"created":"2026-09-15T10:36:47Z"}]}`)
}

// dbOutput extracts the structured result of a design_brief run.
func dbOutput(t *testing.T, res ToolResult) designBriefOutput {
	t.Helper()
	out, ok := res.StructuredOut.(designBriefOutput)
	require.True(t, ok, "StructuredOut must be a designBriefOutput, got %T", res.StructuredOut)
	require.NotNil(t, out.Brief)
	return out
}

// ---------------------------------------------------------------------------
// Definition / Validate
// ---------------------------------------------------------------------------

func TestDesignBriefHandler_NameAndDefinition(t *testing.T) {
	t.Parallel()
	h := &designBriefHandler{}

	require.Equal(t, "design_brief", h.Name())

	def := h.Definition()
	require.Equal(t, "design_brief", def.Name)
	require.NotEmpty(t, def.Description)
	for _, want := range []string{"screen_name", "depth", "summary", "full", "WRITES NO FILES", "CONTRACT, NOT A GENERATOR"} {
		require.Contains(t, def.Description, want, "the description must name %q", want)
	}
	require.Equal(t, []string{"screen_name"}, def.Required, "screen_name is the only required argument")

	params := map[string]bool{}
	for _, p := range def.Parameters {
		params[p.Name] = true
	}
	assert.True(t, params["screen_name"])
	assert.True(t, params["depth"])
}

func TestDesignBriefHandler_Validate(t *testing.T) {
	t.Parallel()
	h := &designBriefHandler{}

	// screen_name is required: nil/empty is an error (matching the interface
	// contract the conformance suite asserts).
	require.Error(t, h.Validate(nil))
	require.Error(t, h.Validate(map[string]any{}))
	require.Error(t, h.Validate(map[string]any{"screen_name": "  "}))
	require.NoError(t, h.Validate(map[string]any{"screen_name": "login"}))
	require.NoError(t, h.Validate(map[string]any{"screen_name": "login", "depth": "full"}))
	assert.Error(t, h.Validate(map[string]any{"screen_name": 5}))
	assert.Error(t, h.Validate(map[string]any{"screen_name": "login", "depth": 5}))
}

// ---------------------------------------------------------------------------
// Happy path — every brief field at the tool boundary
// ---------------------------------------------------------------------------

func TestDesignBriefHandler_BriefFields(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dbSeedTree(t, root)
	h := &designBriefHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{"screen_name": "login"})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := dbOutput(t, res)
	assert.Equal(t, "login", out.ScreenName)
	assert.Equal(t, design.BriefDepthSummary, out.Depth, "depth defaults to summary")
	assert.True(t, out.DesignTreeExists)
	assert.True(t, out.WritesNothing, "the §5g contract is asserted at the result level")
	assert.Empty(t, out.Guidance)

	b := out.Brief
	assert.True(t, b.Found)
	assert.Equal(t, "draft", b.Status)
	assert.Equal(t, "sign-in entry point with credential recovery", b.Purpose)
	assert.Equal(t, "design/wireframes/login.svg", b.Wireframe)
	assert.True(t, b.WireframeExists)

	require.Len(t, b.FlowsOut, 1)
	assert.Equal(t, "tap Submit", b.FlowsOut[0].Trigger)
	assert.Equal(t, "home", b.FlowsOut[0].OtherStem)
	assert.Equal(t, "out", b.FlowsOut[0].Direction)

	require.Len(t, b.TokenPaths, 1)
	assert.Equal(t, "color.brand.primary", b.TokenPaths[0].Path)
	assert.True(t, b.TokenPaths[0].Known)

	require.NotEmpty(t, b.TokenGroups)

	assert.Equal(t, "design/feedback/login.json", b.Feedback.Path)
	assert.Equal(t, 1, b.Feedback.Open)
	assert.True(t, b.Feedback.Pending)

	assert.True(t, b.WritesNothing)

	// The text output leads with the structured summary.
	assert.Contains(t, res.Output, "design_brief")
	assert.Contains(t, res.Output, "tap Submit")
	assert.Contains(t, res.Output, "sign-in entry point with credential recovery")
}

// ---------------------------------------------------------------------------
// Depth: summary vs full
// ---------------------------------------------------------------------------

func TestDesignBriefHandler_DepthSummaryVsFull(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dbSeedTree(t, root)
	h := &designBriefHandler{}

	summaryRes, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{"screen_name": "login"})
	require.NoError(t, err)
	summary := dbOutput(t, summaryRes)

	fullRes, err := h.Execute(newTestCtx(root), newTestEnv(t, root),
		map[string]any{"screen_name": "login", "depth": "full"})
	require.NoError(t, err)
	full := dbOutput(t, fullRes)

	assert.Equal(t, design.BriefDepthSummary, summary.Depth)
	assert.Equal(t, design.BriefDepthFull, full.Depth)

	assert.Empty(t, summary.Brief.Feedback.Notes, "summary depth carries no annotation notes")
	require.Len(t, full.Brief.Feedback.Notes, 1)
	assert.Contains(t, full.Brief.Feedback.Notes[0], "Primary CTA reads as secondary")

	assert.NotContains(t, summaryRes.Output, "Primary CTA reads as secondary")
	assert.Contains(t, fullRes.Output, "Primary CTA reads as secondary")

	// The actionable structured fields are identical at both depths.
	assert.Equal(t, summary.Brief.Purpose, full.Brief.Purpose)
	assert.Equal(t, summary.Brief.TokenPaths, full.Brief.TokenPaths)
	assert.Equal(t, summary.Brief.FlowsOut, full.Brief.FlowsOut)
}

// ---------------------------------------------------------------------------
// Argument handling
// ---------------------------------------------------------------------------

func TestDesignBriefHandler_MissingScreenNameIsAnError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dbSeedTree(t, root)
	h := &designBriefHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{})
	require.Error(t, err)
	require.True(t, res.IsError)
	assert.Contains(t, res.Output, "screen_name")
}

func TestDesignBriefHandler_AcceptsWireframePath(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dbSeedTree(t, root)
	h := &designBriefHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root),
		map[string]any{"screen_name": "design/wireframes/login.svg"})
	require.NoError(t, err)
	out := dbOutput(t, res)
	assert.Equal(t, "login", out.ScreenName, "a wireframe path reduces to its stem")
	assert.True(t, out.Brief.Found)
}

// ---------------------------------------------------------------------------
// Not-found — a clear result, never an error crash
// ---------------------------------------------------------------------------

func TestDesignBriefHandler_UnknownScreenIsNotFoundNotError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dbSeedTree(t, root)
	h := &designBriefHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{"screen_name": "settings"})
	require.NoError(t, err, "an unknown screen is a reportable result, not an error")
	require.False(t, res.IsError)

	out := dbOutput(t, res)
	assert.False(t, out.Brief.Found)
	assert.False(t, out.Brief.WireframeExists)
	assert.NotEmpty(t, out.Guidance)
	assert.Empty(t, out.Brief.FlowsIn)
	assert.Empty(t, out.Brief.FlowsOut)
	assert.Contains(t, res.Output, "not found")
}

func TestDesignBriefHandler_MissingDesignTree(t *testing.T) {
	t.Parallel()
	root := t.TempDir() // no design/
	h := &designBriefHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{"screen_name": "login"})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := dbOutput(t, res)
	assert.False(t, out.DesignTreeExists)
	assert.False(t, out.Brief.Found)
	assert.NotEmpty(t, out.Guidance)
}

// ---------------------------------------------------------------------------
// Gate-1 precheck (SP-140 invariant 7)
// ---------------------------------------------------------------------------

func TestDesignBriefHandler_Gate1Deny(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dbSeedTree(t, root)
	h := &designBriefHandler{}

	env := newTestEnv(t, root)
	env.FileAccessClassifier = denyClassifier{}

	res, err := h.Execute(newTestCtx(root), env, map[string]any{"screen_name": "login"})
	require.Error(t, err, "a Gate-1 deny is a hard failure")
	require.True(t, res.IsError)
	assert.Contains(t, res.Output, "design_brief blocked")
}

// A deny scoped to the design/ tree (not wholesale) still blocks the run: the
// tool resolves paths under design/.
func TestDesignBriefHandler_Gate1DenyOnDesignPath(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dbSeedTree(t, root)
	h := &designBriefHandler{}

	env := newTestEnv(t, root)
	env.FileAccessClassifier = dsDenyPathClassifier{substr: "design"}

	res, err := h.Execute(newTestCtx(root), env, map[string]any{"screen_name": "login"})
	require.Error(t, err)
	require.True(t, res.IsError)
	assert.Contains(t, res.Output, "design_brief blocked")
}

// ---------------------------------------------------------------------------
// §5g hard contract: the tool writes NO files
// ---------------------------------------------------------------------------

func TestDesignBriefHandler_WritesNoFiles(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dbSeedTree(t, root)
	before := dsTreeSnapshot(t, root)

	h := &designBriefHandler{}
	// A found screen (summary + full) and a not-found screen: none may write.
	_, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{"screen_name": "login"})
	require.NoError(t, err)
	_, err = h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{"screen_name": "login", "depth": "full"})
	require.NoError(t, err)
	_, err = h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{"screen_name": "nope"})
	require.NoError(t, err)

	after := dsTreeSnapshot(t, root)
	assert.Equal(t, before, after, "design_brief must not create, modify, or delete any file")
}

// The output path must not be reachable as a write: assert the brief's own
// fields name no generated artifact and the structured result carries the
// contract flag.
func TestDesignBriefHandler_ContractNotGenerator(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dbSeedTree(t, root)
	h := &designBriefHandler{}

	res, err := h.Execute(newTestCtx(root), newTestEnv(t, root), map[string]any{"screen_name": "login"})
	require.NoError(t, err)

	data, err := json.Marshal(res.StructuredOut)
	require.NoError(t, err)
	var generic map[string]any
	require.NoError(t, json.Unmarshal(data, &generic))
	assert.Equal(t, true, generic["writesNothing"], "the result states the read-only contract")

	brief, ok := generic["brief"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, brief["writesNothing"])
	// No component/generated artifact is produced.
	assert.NotContains(t, string(data), "design/generated")
	assert.NotContains(t, string(data), ".tsx")
	assert.NotContains(t, string(data), ".css")
}

// ---------------------------------------------------------------------------
// Determinism
// ---------------------------------------------------------------------------

func TestDesignBriefHandler_Deterministic(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dbSeedTree(t, root)
	h := &designBriefHandler{}

	var reference string
	for run := 0; run < 8; run++ {
		res, err := h.Execute(newTestCtx(root), newTestEnv(t, root),
			map[string]any{"screen_name": "login", "depth": "full"})
		require.NoError(t, err)
		data, err := json.Marshal(dbOutput(t, res))
		require.NoError(t, err)
		if run == 0 {
			reference = string(data)
			continue
		}
		assert.Equal(t, reference, string(data), "run %d must be byte-identical", run+1)
	}
}

// ---------------------------------------------------------------------------
// Registration surface (SP-140 invariant 7)
// ---------------------------------------------------------------------------

// TestDesignBrief_RegistrarIsBuildTagged pins the build-tag split: the native
// handler is !js with a nil-returning js stub, and all.go reaches it through
// the registrar (never constructs the handler, which is declared only in the
// !js file).
func TestDesignBrief_RegistrarIsBuildTagged(t *testing.T) {
	t.Parallel()

	const handlerFile = "design_brief_handler.go"
	const stubFile = "design_brief_handler_js.go"
	const registrar = "registerDesignBriefTools"

	handler := readToolSource(t, handlerFile)
	assert.Contains(t, buildConstraints(handler), designNativeBuildTag,
		"%s must carry `%s` (SP-140 invariant 7)", handlerFile, designNativeBuildTag)
	assert.Contains(t, handler, "func "+registrar+"()")

	stub := readToolSource(t, stubFile)
	assert.Contains(t, buildConstraints(stub), designWasmBuildTag,
		"%s must carry `%s`", stubFile, designWasmBuildTag)
	assert.Contains(t, stub, "func "+registrar+"()")
	assert.Contains(t, stub, "return nil",
		"%s must return nil: the tool is unregistered on WASM", stubFile)

	all := readToolSource(t, designAllToolsFile)
	assert.Contains(t, all, registrar+"()",
		"all.go must call %s() so the WASM exclusion is wired in", registrar)
	assert.NotContains(t, all, "&designBriefHandler{}",
		"all.go must reach design_brief through its registrar, not construct the !js handler")
}

// TestDesignBrief_OnRosterOnlyThroughRegistrar asserts the tool is genuinely
// reachable on native via the registrar the roster splits on.
func TestDesignBrief_OnRosterOnlyThroughRegistrar(t *testing.T) {
	t.Parallel()
	handlers := registerDesignBriefTools()
	require.Len(t, handlers, 1)
	assert.Equal(t, "design_brief", handlers[0].Name())

	names := map[string]bool{}
	for _, h := range AllTools() {
		names[h.Name()] = true
	}
	assert.True(t, names["design_brief"], "design_brief must be registered on native")
}
