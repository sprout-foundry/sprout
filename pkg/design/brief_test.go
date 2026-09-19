package design

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// design_brief core tests (SP-140-5 §5g, TODO item 5.8)
//
// These tests cover the pure, read-only core in pkg/design/brief.go:
// BuildScreenBrief, the summary/full depths, every brief field populated from a
// seeded fixture tree, the not-found path (never an error), and determinism.
// The ToolHandler seam (Gate-1, no-writes) is covered in
// pkg/agent_tools/design_brief_handler_test.go.
// ---------------------------------------------------------------------------

// briefWrite writes rel (slash-separated) under root, creating parents.
func briefWrite(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
}

// briefFixtureReadme is the seeded README manifest: a Screens listing (purpose +
// status) and a Flows listing.
const briefFixtureReadme = `# Design Workspace

## Device frames

` + "```" + `
frames:
  mobile: 390x844
` + "```" + `

## Screens

- ` + "`login`" + ` — draft — sign-in entry point with credential recovery
- ` + "`home`" + ` — ready — message list with unread badges

## Flows

- ` + "`sign-up`" + ` — draft — account creation from landing to first-run
`

// briefSeedTree seeds a complete design/ tree with the screen `login` in the
// middle of a flow, a token reference, and a pending feedback file.
func briefSeedTree(t *testing.T, root string) {
	t.Helper()
	briefWrite(t, root, "design/README.md", briefFixtureReadme)
	briefWrite(t, root, "design/tokens/color.tokens.json", `{
  "color": { "brand": { "primary": { "$type": "color", "$value": "#0055ff" } } },
  "dimension": { "space": { "medium": { "$type": "dimension", "$value": "8px" } } }
}`)
	// login refers to color.brand.primary (known) and color.legacy.accent
	// (unknown), so the known/unknown split is exercised.
	briefWrite(t, root, "design/wireframes/login.svg",
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844">`+
			`<!-- {color.brand.primary} -->`+
			`<!-- {color.legacy.accent} -->`+
			`<rect id="submit" data-nav="home"/><text>Sign in</text></svg>`)
	briefWrite(t, root, "design/wireframes/home.svg",
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><text>Home</text></svg>`)
	briefWrite(t, root, "design/wireframes/landing.svg",
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><text>Landing</text></svg>`)
	// Two edges touch login: landing --> login (in, no trigger label) and
	// login -- "tap Submit" --> home (out, trigger label).
	briefWrite(t, root, "design/flows/sign-up.mmd",
		"flowchart TD\n  landing --> login\n  login -- \"tap Submit\" --> home\n")
	briefWrite(t, root, "design/feedback/login.json",
		`{"target":"design/wireframes/login.svg","status":"changes-requested","resolution":"",`+
			`"annotations":[`+
			`{"id":"a1","at":{"x":0.42,"y":0.18},"area":"hierarchy","note":"Primary CTA reads as secondary","resolved":false,"created":"2026-09-15T10:36:47Z"},`+
			`{"id":"a2","at":{"x":0.1,"y":0.9},"area":"contrast","note":"Low contrast label","resolved":true,"created":"2026-09-15T10:37:00Z"}`+
			`]}`)
}

// ---------------------------------------------------------------------------
// Full brief — every field populated
// ---------------------------------------------------------------------------

func TestBuildScreenBrief_FullBriefFields(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	briefSeedTree(t, root)

	brief, err := BuildScreenBrief(ScreenBriefInput{Root: root, ScreenName: "login"})
	require.NoError(t, err)
	require.NotNil(t, brief)

	// §5g status + README purpose + listing.
	assert.Equal(t, "login", brief.ScreenName)
	assert.Equal(t, BriefDepthSummary, brief.Depth, "the default depth is summary")
	assert.True(t, brief.Found)
	assert.Equal(t, "draft", brief.Status)
	assert.Equal(t, "sign-in entry point with credential recovery", brief.Purpose)
	assert.True(t, brief.ListedInReadme)

	// §5g wireframe path.
	assert.Equal(t, "design/wireframes/login.svg", brief.Wireframe)
	assert.True(t, brief.WireframeExists)

	// §5g flows in/out with triggers.
	require.Len(t, brief.FlowsIn, 1)
	in := brief.FlowsIn[0]
	assert.Equal(t, "design/flows/sign-up.mmd", in.Flow)
	assert.Equal(t, "sign-up", in.FlowName)
	assert.Equal(t, "landing", in.Source)
	assert.Equal(t, "login", in.Target)
	assert.Equal(t, "in", in.Direction)
	assert.Equal(t, "landing", in.OtherStem)
	assert.Empty(t, in.Trigger, "the landing --> login edge carries no trigger label")

	require.Len(t, brief.FlowsOut, 1)
	out := brief.FlowsOut[0]
	assert.Equal(t, "login", out.Source)
	assert.Equal(t, "home", out.Target)
	assert.Equal(t, "out", out.Direction)
	assert.Equal(t, "home", out.OtherStem)
	assert.Equal(t, "tap Submit", out.Trigger, "the mermaid edge label is the trigger")

	// §5g token paths, split known/unknown.
	require.Len(t, brief.TokenPaths, 2)
	assert.Equal(t, BriefTokenRef{Path: "color.brand.primary", Known: true}, brief.TokenPaths[0])
	assert.Equal(t, BriefTokenRef{Path: "color.legacy.accent", Known: false}, brief.TokenPaths[1])

	// The available token groups come from the tree.
	require.NotEmpty(t, brief.TokenGroups)
	groups := map[string]int{}
	for _, g := range brief.TokenGroups {
		groups[g.Group] = g.Tokens
	}
	assert.Equal(t, 1, groups["color"])
	assert.Equal(t, 1, groups["dimension"])

	// §5g open feedback annotations.
	assert.Equal(t, "design/feedback/login.json", brief.Feedback.Path)
	assert.Equal(t, "changes-requested", brief.Feedback.Status)
	assert.Equal(t, 2, brief.Feedback.Total)
	assert.Equal(t, 1, brief.Feedback.Open, "only the unresolved annotation is open")
	assert.True(t, brief.Feedback.Pending)

	// §5g contract: the brief writes nothing.
	assert.True(t, brief.WritesNothing)

	// The JSON serialization is available and deterministic.
	j1, err := SerializeScreenBrief(brief)
	require.NoError(t, err)
	j2, err := SerializeScreenBrief(brief)
	require.NoError(t, err)
	assert.Equal(t, j1, j2, "serializing the same brief is byte-identical")
	assert.Contains(t, string(j1), `"screenName": "login"`)
	assert.Contains(t, string(j1), `"writesNothing": true`)
}

// ---------------------------------------------------------------------------
// summary vs full depth
// ---------------------------------------------------------------------------

func TestBuildScreenBrief_DepthSummaryVsFull(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	briefSeedTree(t, root)
	briefWrite(t, root, "design/screens/login.html",
		`<!doctype html><html><body><p>delivered</p></body></html>`)

	summary, err := BuildScreenBrief(ScreenBriefInput{Root: root, ScreenName: "login"})
	require.NoError(t, err)
	full, err := BuildScreenBrief(ScreenBriefInput{Root: root, ScreenName: "login", Depth: BriefDepthFull})
	require.NoError(t, err)

	assert.Equal(t, BriefDepthSummary, summary.Depth)
	assert.Equal(t, BriefDepthFull, full.Depth)

	// The full depth carries the unresolved annotation notes; the summary does
	// not (it reports only the count).
	assert.Empty(t, summary.Feedback.Notes, "summary depth carries no annotation notes")
	require.Len(t, full.Feedback.Notes, 1)
	assert.Contains(t, full.Feedback.Notes[0], "hierarchy")
	assert.Contains(t, full.Feedback.Notes[0], "Primary CTA reads as secondary")

	// The full depth names the delivered screen file; the summary does not.
	assert.Empty(t, summary.ScreenFile)
	assert.Equal(t, "design/screens/login.html", full.ScreenFile)

	// The structured fields the agent acts on are identical at both depths.
	assert.Equal(t, summary.Purpose, full.Purpose)
	assert.Equal(t, summary.Status, full.Status)
	assert.Equal(t, summary.TokenPaths, full.TokenPaths)
	assert.Equal(t, summary.FlowsIn, full.FlowsIn)
	assert.Equal(t, summary.FlowsOut, full.FlowsOut)

	// The text rendering includes the open note at full depth only.
	summaryText := RenderScreenBrief(summary)
	fullText := RenderScreenBrief(full)
	assert.NotContains(t, summaryText, "Primary CTA reads as secondary")
	assert.Contains(t, fullText, "Primary CTA reads as secondary")
}

func TestNormaliseBriefDepth(t *testing.T) {
	t.Parallel()
	assert.Equal(t, BriefDepthSummary, normaliseBriefDepth(""))
	assert.Equal(t, BriefDepthSummary, normaliseBriefDepth("  "))
	assert.Equal(t, BriefDepthSummary, normaliseBriefDepth("SUMMARY"))
	assert.Equal(t, BriefDepthSummary, normaliseBriefDepth("bogus"), "an unknown depth falls back to summary")
	assert.Equal(t, BriefDepthFull, normaliseBriefDepth("full"))
	assert.Equal(t, BriefDepthFull, normaliseBriefDepth(" Full "))
}

// ---------------------------------------------------------------------------
// Not-found / unknown screen
// ---------------------------------------------------------------------------

func TestBuildScreenBrief_UnknownScreenIsNotFoundNotError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	briefSeedTree(t, root)

	brief, err := BuildScreenBrief(ScreenBriefInput{Root: root, ScreenName: "settings"})
	require.NoError(t, err, "an unknown screen is a reportable state, never a crash")
	require.NotNil(t, brief)

	assert.False(t, brief.Found)
	assert.False(t, brief.WireframeExists)
	assert.Equal(t, "design/wireframes/settings.svg", brief.Wireframe,
		"the brief still names where the wireframe would live")
	assert.Empty(t, brief.Purpose)
	assert.Empty(t, brief.Status)
	assert.Empty(t, brief.FlowsIn)
	assert.Empty(t, brief.FlowsOut)
	assert.NotEmpty(t, brief.Guidance)
	assert.Contains(t, brief.Guidance, "settings")
	assert.Contains(t, RenderScreenBrief(brief), "not found")
}

func TestBuildScreenBrief_MissingDesignTree(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	brief, err := BuildScreenBrief(ScreenBriefInput{Root: root, ScreenName: "login"})
	require.NoError(t, err, "a workspace with no design/ tree is reportable, not an error")
	require.NotNil(t, brief)

	assert.False(t, brief.Found)
	assert.NotEmpty(t, brief.Guidance)
	assert.Contains(t, brief.Guidance, "design/")
	require.NotEmpty(t, brief.Notes)
	assert.Contains(t, brief.Notes[0], "No design/ tree")
	// Slices stay non-nil so the JSON shape is stable.
	assert.NotNil(t, brief.FlowsIn)
	assert.NotNil(t, brief.FlowsOut)
	assert.NotNil(t, brief.TokenPaths)
}

// ---------------------------------------------------------------------------
// Screen name normalisation
// ---------------------------------------------------------------------------

func TestBuildScreenBrief_AcceptsPathAndExtension(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	briefSeedTree(t, root)

	for _, name := range []string{"login", "login.svg", "design/wireframes/login.svg", "  design/wireframes/login.svg  "} {
		brief, err := BuildScreenBrief(ScreenBriefInput{Root: root, ScreenName: name})
		require.NoError(t, err, "input %q", name)
		assert.Equal(t, "login", brief.ScreenName, "input %q must normalise to the stem", name)
		assert.True(t, brief.Found, "input %q must resolve", name)
	}
}

func TestNormaliseScreenName(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "login", normaliseScreenName("login"))
	assert.Equal(t, "login", normaliseScreenName("login.svg"))
	assert.Equal(t, "login", normaliseScreenName("design/wireframes/login.svg"))
	assert.Equal(t, "login", normaliseScreenName("design/feedback/login.json"))
	assert.Equal(t, "sign-up", normaliseScreenName("design/flows/sign-up.mmd"))
	assert.Equal(t, "", normaliseScreenName("   "))
}

// ---------------------------------------------------------------------------
// Feedback matching by file name and by target
// ---------------------------------------------------------------------------

func TestBuildScreenBrief_FeedbackMatchedByTarget(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	briefWrite(t, root, "design/wireframes/login.svg",
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><text>x</text></svg>`)
	// The feedback file's name differs from the stem; it matches by target.
	briefWrite(t, root, "design/feedback/signin-review.json",
		`{"target":"design/wireframes/login.svg","status":"changes-requested","resolution":"","annotations":[`+
			`{"id":"a1","at":{"x":0,"y":0},"area":"affordance","note":"make the CTA obvious","resolved":false,"created":""}]}`)

	brief, err := BuildScreenBrief(ScreenBriefInput{Root: root, ScreenName: "login", Depth: BriefDepthFull})
	require.NoError(t, err)
	assert.True(t, brief.Found)
	assert.Equal(t, "design/feedback/signin-review.json", brief.Feedback.Path)
	assert.Equal(t, 1, brief.Feedback.Open)
	assert.True(t, brief.Feedback.Pending)
	require.Len(t, brief.Feedback.Notes, 1)
	assert.Contains(t, brief.Feedback.Notes[0], "make the CTA obvious")
}

func TestBuildScreenBrief_ResolvedFeedbackIsNotPending(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	briefWrite(t, root, "design/wireframes/login.svg",
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><text>x</text></svg>`)
	briefWrite(t, root, "design/feedback/login.json",
		`{"target":"design/wireframes/login.svg","status":"resolved","resolution":"fixed the CTA","annotations":[`+
			`{"id":"a1","at":{"x":0,"y":0},"area":"hierarchy","note":"done","resolved":true,"created":""}]}`)

	brief, err := BuildScreenBrief(ScreenBriefInput{Root: root, ScreenName: "login", Depth: BriefDepthFull})
	require.NoError(t, err)
	assert.False(t, brief.Feedback.Pending)
	assert.Equal(t, 0, brief.Feedback.Open)
	assert.Empty(t, brief.Feedback.Notes, "a resolved annotation is not an open note")
	assert.Equal(t, "fixed the CTA", brief.Feedback.Resolution)
	// A screen with only resolved feedback is still Found (via the wireframe).
	assert.True(t, brief.Found)
}

// A screen that exists only as pending feedback (annotated before the wireframe
// landed) is still Found, so the agent can brief it.
func TestBuildScreenBrief_FeedbackOnlyScreenIsFound(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	briefWrite(t, root, "design/feedback/checkout.json",
		`{"target":"design/wireframes/checkout.svg","status":"changes-requested","resolution":"","annotations":[`+
			`{"id":"a1","at":{"x":0,"y":0},"area":"spacing","note":"tighten the padding","resolved":false,"created":""}]}`)

	brief, err := BuildScreenBrief(ScreenBriefInput{Root: root, ScreenName: "checkout"})
	require.NoError(t, err)
	assert.True(t, brief.Found, "feedback targeting the screen makes it briefable")
	assert.False(t, brief.WireframeExists)
	assert.True(t, brief.Feedback.Pending)
	assert.Empty(t, brief.Guidance)
}

// ---------------------------------------------------------------------------
// Flow edges: directions, self-edges, the -->|label| form, multi-flow ordering
// ---------------------------------------------------------------------------

func TestBuildScreenBrief_FlowDirectionsAndTriggers(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	briefWrite(t, root, "design/wireframes/login.svg",
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><text>x</text></svg>`)
	briefWrite(t, root, "design/wireframes/home.svg",
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><text>y</text></svg>`)
	// The `-->|label|` form and a self-edge, plus a plain in-edge.
	briefWrite(t, root, "design/flows/auth.mmd",
		"flowchart TD\n  login -->|tap Forgot| login\n  home --> login\n  login --> home\n")

	brief, err := BuildScreenBrief(ScreenBriefInput{Root: root, ScreenName: "login"})
	require.NoError(t, err)

	// in: home --> login. out: login --> home. A self-edge is both.
	require.Len(t, brief.FlowsIn, 2)
	require.Len(t, brief.FlowsOut, 2)

	var selfIn *BriefFlowEdge
	for i := range brief.FlowsIn {
		if brief.FlowsIn[i].Direction == "both" {
			selfIn = &brief.FlowsIn[i]
		}
	}
	require.NotNil(t, selfIn, "the self-edge must appear in FlowsIn with direction both")
	assert.Equal(t, "tap Forgot", selfIn.Trigger, "the -->|label| form carries the trigger")
	assert.Empty(t, selfIn.OtherStem, "a self-edge has no far endpoint")

	// Ordering is deterministic (by flow, source, target, trigger).
	assert.Equal(t, "home", brief.FlowsIn[0].Source)
	assert.Equal(t, "login", brief.FlowsIn[1].Source)
}

func TestBuildScreenBrief_MultipleFlowsSorted(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	briefWrite(t, root, "design/wireframes/login.svg",
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><text>x</text></svg>`)
	briefWrite(t, root, "design/wireframes/home.svg",
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><text>y</text></svg>`)
	briefWrite(t, root, "design/flows/z-last.mmd", "flowchart TD\n  login --> home\n")
	briefWrite(t, root, "design/flows/a-first.mmd", "flowchart TD\n  home --> login\n")

	brief, err := BuildScreenBrief(ScreenBriefInput{Root: root, ScreenName: "login"})
	require.NoError(t, err)
	require.Len(t, brief.FlowsOut, 1)
	require.Len(t, brief.FlowsIn, 1)
	assert.Equal(t, "design/flows/z-last.mmd", brief.FlowsOut[0].Flow)
	assert.Equal(t, "design/flows/a-first.mmd", brief.FlowsIn[0].Flow)
}

func TestBuildScreenBrief_NoFlowEdges(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	briefWrite(t, root, "design/wireframes/login.svg",
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><text>x</text></svg>`)

	brief, err := BuildScreenBrief(ScreenBriefInput{Root: root, ScreenName: "login"})
	require.NoError(t, err)
	assert.Empty(t, brief.FlowsIn)
	assert.Empty(t, brief.FlowsOut)
	assert.NotNil(t, brief.FlowsIn)
	assert.NotNil(t, brief.FlowsOut)
}

// ---------------------------------------------------------------------------
// README variants
// ---------------------------------------------------------------------------

func TestBuildScreenBrief_UnlistedScreenIsOrphan(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	briefWrite(t, root, "design/README.md", "# Design Workspace\n\n## Screens\n\n- `home` — ready — home\n")
	briefWrite(t, root, "design/wireframes/login.svg",
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><text>x</text></svg>`)

	brief, err := BuildScreenBrief(ScreenBriefInput{Root: root, ScreenName: "login"})
	require.NoError(t, err)
	assert.True(t, brief.Found, "the wireframe exists, so the screen is found")
	assert.False(t, brief.ListedInReadme)
	assert.Empty(t, brief.Status)
	assert.Empty(t, brief.Purpose)
	require.NotEmpty(t, brief.Notes)
	assert.Contains(t, brief.Notes[0], "orphan")
	assert.Contains(t, RenderScreenBrief(brief), "orphan")
}

func TestBuildScreenBrief_MissingReadme(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	briefWrite(t, root, "design/wireframes/login.svg",
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><text>x</text></svg>`)

	brief, err := BuildScreenBrief(ScreenBriefInput{Root: root, ScreenName: "login"})
	require.NoError(t, err)
	assert.True(t, brief.Found)
	assert.False(t, brief.ListedInReadme)
	assert.Empty(t, brief.Status)
	assert.Empty(t, brief.Purpose)
}

// ---------------------------------------------------------------------------
// Wireframe variants
// ---------------------------------------------------------------------------

func TestBuildScreenBrief_WireframeWithoutTokenRefsStillListsGroups(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	briefWrite(t, root, "design/tokens/color.tokens.json",
		`{"color":{"brand":{"primary":{"$type":"color","$value":"#0055ff"}}}}`)
	briefWrite(t, root, "design/wireframes/login.svg",
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><text>x</text></svg>`)

	brief, err := BuildScreenBrief(ScreenBriefInput{Root: root, ScreenName: "login"})
	require.NoError(t, err)
	assert.Empty(t, brief.TokenPaths, "no {token} comments, no token references")
	require.NotEmpty(t, brief.TokenGroups, "the available groups are still reported")
	assert.Contains(t, RenderScreenBrief(brief), "available groups")
}

// ---------------------------------------------------------------------------
// Determinism
// ---------------------------------------------------------------------------

func TestBuildScreenBrief_Deterministic(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	briefSeedTree(t, root)

	first, err := BuildScreenBrief(ScreenBriefInput{Root: root, ScreenName: "login", Depth: BriefDepthFull})
	require.NoError(t, err)
	firstJSON, err := SerializeScreenBrief(first)
	require.NoError(t, err)

	// Re-running against the same tree must be byte-identical, across many
	// runs (map iteration order must not leak).
	for i := 0; i < 25; i++ {
		again, err := BuildScreenBrief(ScreenBriefInput{Root: root, ScreenName: "login", Depth: BriefDepthFull})
		require.NoError(t, err)
		j, err := SerializeScreenBrief(again)
		require.NoError(t, err)
		require.Equal(t, firstJSON, j, "run %d must be byte-identical", i)
	}
}

// ---------------------------------------------------------------------------
// §5g contract: the core writes nothing
// ---------------------------------------------------------------------------

func TestBuildScreenBrief_WritesNothing(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	briefSeedTree(t, root)

	before := briefSnapshot(t, root)

	_, err := BuildScreenBrief(ScreenBriefInput{Root: root, ScreenName: "login", Depth: BriefDepthFull})
	require.NoError(t, err)
	_, err = BuildScreenBrief(ScreenBriefInput{Root: root, ScreenName: "unknown-screen"})
	require.NoError(t, err)

	after := briefSnapshot(t, root)
	assert.Equal(t, before, after, "the brief is read-only: no file created, modified, or removed")
}

// briefSnapshot records every file under root with its content, so a before/
// after comparison detects any write, create, or delete.
func briefSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	snap := map[string]string{}
	require.NoError(t, filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			return relErr
		}
		data, readErr := os.ReadFile(p)
		if readErr != nil {
			return readErr
		}
		snap[filepath.ToSlash(rel)] = string(data)
		return nil
	}))
	return snap
}

// ---------------------------------------------------------------------------
// Rendering helpers
// ---------------------------------------------------------------------------

func TestRenderScreenBrief_NilAndFound(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "design_brief: no brief.", RenderScreenBrief(nil))

	b := &ScreenBrief{
		ScreenName:      "login",
		Depth:           BriefDepthSummary,
		Found:           true,
		Status:          "draft",
		Purpose:         "sign in",
		Wireframe:       "design/wireframes/login.svg",
		WireframeExists: true,
		FlowsIn: []BriefFlowEdge{
			{Source: "landing", Target: "login", Direction: "in"},
		},
		FlowsOut: []BriefFlowEdge{
			{Source: "login", Target: "home", Direction: "out", Trigger: "tap Submit"},
		},
		TokenPaths:     []BriefTokenRef{{Path: "color.brand.primary", Known: true}},
		TokenGroups:    []TokenGroupCount{},
		ListedInReadme: true,
		WritesNothing:  true,
	}
	text := RenderScreenBrief(b)
	assert.Contains(t, text, "design_brief (summary)")
	assert.Contains(t, text, `screen "login"`)
	assert.Contains(t, text, "status draft")
	assert.Contains(t, text, "sign in")
	assert.Contains(t, text, "1 in, 1 out")
	assert.Contains(t, text, "tap Submit")
	assert.Contains(t, text, "{color.brand.primary}")
	assert.Contains(t, text, "contract, not a generator")
}

func TestBriefSegmentLabel(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "tap Submit", briefSegmentLabel(` "tap Submit" `))
	assert.Equal(t, "bare", briefSegmentLabel(" bare "))
	assert.Equal(t, "", briefSegmentLabel("  "))
	assert.Equal(t, "", briefSegmentLabel("-->"), "an operator fragment is not a label")
}

// briefFlowEdgesInFile must resolve a trigger for every supported label syntax.
// The dotted `-.->` forms are not recognised by the shared splitByOperator
// (flowchart.go), so a dotted statement yields no edge — the manifest contract
// uses `-->`/`-- "label" -->`, which are covered here.
func TestBriefFlowEdgesInFile_TriggerSyntaxes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		stmt    string
		trigger string
	}{
		{`login -- "tap Submit" --> home`, "tap Submit"},
		{`login -- "multi-word hyphen-ated label" --> home`, "multi-word hyphen-ated label"},
		{`login -->|tap Submit| home`, "tap Submit"},
		{`login --> home`, ""},
		{`login ==> home`, ""},
	}
	for _, tc := range cases {
		content := "flowchart TD\n  " + tc.stmt + "\n"
		fc := ParseFlowchart(content)
		edges := briefFlowEdgesInFile("design/flows/f.mmd", "f", content, fc)
		require.Lenf(t, edges, 1, "statement %q must yield exactly one edge", tc.stmt)
		assert.Equal(t, "login", edges[0].Source, "statement %q", tc.stmt)
		assert.Equal(t, "home", edges[0].Target, "statement %q", tc.stmt)
		assert.Equal(t, tc.trigger, edges[0].Trigger, "statement %q", tc.stmt)
	}
}

// A chained labelled statement resolves both edges, with the label on the edge
// it belongs to (not leaked onto the other edge).
func TestBriefFlowEdgesInFile_ChainedLabelled(t *testing.T) {
	t.Parallel()
	content := "flowchart TD\n  login -- \"continue\" --> verify --> home\n"
	fc := ParseFlowchart(content)
	edges := briefFlowEdgesInFile("design/flows/f.mmd", "f", content, fc)
	require.Len(t, edges, 2)
	byPair := map[string]string{}
	for _, e := range edges {
		byPair[e.Source+"->"+e.Target] = e.Trigger
	}
	assert.Equal(t, "continue", byPair["login->verify"])
	assert.Equal(t, "", byPair["verify->home"], "the label must not leak onto the following edge")
}
