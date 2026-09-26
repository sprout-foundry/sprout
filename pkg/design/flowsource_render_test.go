package design

// SP-140-9 §9c pins: the canvas renders the derived graph, and the §7d layout
// sidecar survives a regeneration. The canvas side is the existing client
// renderer (React Flow over the parsed graph, webui/src/design/flowText.ts);
// the Go surfaces a consumer actually reads are (1) ParseFlowchart — what
// design_brief's flows-in/out and the tree validators run over a flow file —
// and (2) the node ids DeriveFlowMMD emits, which the sidecar keys on.
//
// These tests pin the derived .mmd as ordinary mermaid: the banner comments
// stay comments, the |label| triggers parse as edge labels, the step ids are
// the node ids, and a regeneration that adds steps/screens only ever ADDS ids
// (ids(s1) ⊆ ids(s2)), so an existing sidecar still covers the flow.

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fsTestSource() *FlowSource {
	return &FlowSource{
		Name: "sign-up",
		Steps: []FlowStep{
			{ID: "s1", Label: "Start", Screen: "login"},
			{ID: "s2", Label: "Verify email", Screen: "verify", Trigger: "submit code", Next: "s3"},
			{ID: "s3", Label: "Done", Screen: "home"},
		},
	}
}

// fsTestScreens parses as the §9b fixture screen index: login navigates to
// help (an off-path edge the walk does not account for), verify navigates
// back to login.
func fsTestScreens() *ScreenIndexDoc {
	return &ScreenIndexDoc{
		Screens: []ScreenIndexEntry{
			{Stem: "login", Nav: []ScreenIndexNav{{To: "help", Trigger: "tap Forgot"}}},
			{Stem: "verify", Nav: []ScreenIndexNav{{To: "login", Trigger: "resend"}}},
			{Stem: "home"},
			{Stem: "help"},
		},
	}
}

// TestDerivedMMDParsesAsOrdinaryMermaid is the §9c render verification at the
// parser layer: the derived bytes are plain mermaid — comments are comments,
// the declaration is found, step-id nodes are declared, and every trigger the
// generator renders as |label| comes back as that edge's label.
func TestDerivedMMDParsesAsOrdinaryMermaid(t *testing.T) {
	t.Parallel()
	doc := DeriveFlowMMD(fsTestSource(), fsTestScreens())
	mmd := string(RenderFlowMMD(doc, "fnv1a64:deadbeefdeadbeef"))

	fc := ParseFlowchart(mmd)
	require.Equal(t, 1, fc.Declarations, "the banner must stay comments; exactly one declaration")
	assert.Equal(t, "TD", fc.Direction)
	require.Empty(t, fc.BadLines, "the derived form must never produce unparseable lines: %s", mmd)

	// The walk nodes carry their step id, and every step id is present.
	ids := map[string]bool{}
	for _, id := range fc.NodeOrder {
		ids[id] = true
	}
	for _, step := range fsTestSource().Steps {
		assert.Truef(t, ids[step.ID], "step id %q must be a node of the derived graph", step.ID)
	}

	// The walk edges carry their triggers as |label| labels; the off-path
	// data-nav edges source from the step screen's stem (§9c: the step node
	// stands for the screen, the edge id is the stem the sidecar keys on).
	byPair := map[string]string{}
	for _, e := range fc.Edges {
		byPair[e.Source+"\x00"+e.Target] = e.Label
	}
	assert.Equal(t, "submit code", byPair["s2\x00s3"], "the step trigger renders as the edge label")
	assert.Equal(t, "tap Forgot", byPair["login\x00help"], "the off-path nav edge renders with its trigger")
}

// TestDerivedMMDNodeIDStabilityAcrossRegeneration is the §9c sidecar pin:
// the layout sidecar keys on node ids, so a regeneration that adds a step (and
// with it new off-path screens) must only ever add ids — never rename or drop
// one an existing sidecar covers.
func TestDerivedMMDNodeIDStabilityAcrossRegeneration(t *testing.T) {
	t.Parallel()
	before := DeriveFlowMMD(fsTestSource(), fsTestScreens())

	// The regeneration: one more step between verify and done, plus the
	// profile screen's own nav. Same ids for everything the first version had.
	grown := fsTestSource()
	grown.Steps = []FlowStep{
		grown.Steps[0], grown.Steps[1],
		{ID: "s2b", Label: "Profile", Screen: "profile", Trigger: "save", Next: "s3"},
		grown.Steps[2],
	}
	grownScreens := fsTestScreens()
	grownScreens.Screens = append(grownScreens.Screens,
		ScreenIndexEntry{Stem: "profile", Nav: []ScreenIndexNav{{To: "home", Trigger: "done"}}})
	after := DeriveFlowMMD(grown, grownScreens)

	idsOf := func(doc *FlowExportDoc) map[string]bool {
		out := map[string]bool{}
		for _, n := range doc.Nodes {
			out[n.ID] = true
		}
		return out
	}
	idsAfter := idsOf(after)
	for id := range idsOf(before) {
		assert.Truef(t, idsAfter[id], "node id %q vanished in the regeneration; the §7d sidecar keys on it", id)
	}
	assert.True(t, idsAfter["s2b"], "the added step joins the graph under its own id")

	// The same pin, one layer out: the ids as they land in the rendered
	// bytes, read back through the parser the tree validators run.
	nodesOf := func(fc Flowchart) map[string]bool {
		out := map[string]bool{}
		for _, id := range fc.NodeOrder {
			out[id] = true
		}
		return out
	}
	fcBefore := nodesOf(ParseFlowchart(string(RenderFlowMMD(before, "fnv1a64:aaaaaaaaaaaaaaaa"))))
	fcAfter := nodesOf(ParseFlowchart(string(RenderFlowMMD(after, "fnv1a64:bbbbbbbbbbbbbbbb"))))
	for id := range fcBefore {
		assert.Truef(t, fcAfter[id], "rendered node id %q vanished across the regeneration", id)
	}
}

// TestRenderFlowMMDIsDeterministic pins the derived artifact's determinism
// contract: identical inputs render byte-identical output, so a regeneration
// without a source change is a no-op and the drift check's recompute is
// stable.
func TestRenderFlowMMDIsDeterministic(t *testing.T) {
	t.Parallel()
	doc := DeriveFlowMMD(fsTestSource(), fsTestScreens())
	first := string(RenderFlowMMD(doc, "fnv1a64:0123456789abcdef"))
	second := string(RenderFlowMMD(DeriveFlowMMD(fsTestSource(), fsTestScreens()), "fnv1a64:0123456789abcdef"))
	require.Equal(t, first, second)

	// The recorded hash is the only variable input: a different hash must
	// change the bytes (the header is how the validator detects drift).
	assert.NotEqual(t, first, string(RenderFlowMMD(doc, "fnv1a64:ffffffffffffffff")))
}

// TestBriefFlowEdgesReadDerivedMMD pins the design_brief consumer path over
// the derived bytes: flows-in/out resolve their triggers from the |label|
// form the generator writes, so a brief built from a derived flow names the
// triggers instead of blank edges.
func TestBriefFlowEdgesReadDerivedMMD(t *testing.T) {
	t.Parallel()
	doc := DeriveFlowMMD(fsTestSource(), fsTestScreens())
	mmd := string(RenderFlowMMD(doc, "fnv1a64:deadbeefdeadbeef"))
	fc := ParseFlowchart(mmd)

	edges := briefFlowEdgesInFile("design/flows/sign-up.mmd", "sign-up", mmd, fc)
	byPair := map[string]string{}
	for _, e := range edges {
		byPair[e.Source+"\x00"+e.Target] = e.Trigger
	}
	assert.Equal(t, "submit code", byPair["s2\x00s3"])
	assert.Equal(t, "tap Forgot", byPair["login\x00help"])
}

// TestRenderedFlowLabelsQuoteRoundTrip pins the label-escaping contract of
// the renderer: a step label or trigger carrying a double quote renders
// escaped (#quot;, mermaid's own escape) so it cannot terminate the quoted
// node label early, and the statement still parses (mermaid's renderer
// decodes the escape visually; the subset parser keeps the escaped text).
func TestRenderedFlowLabelsQuoteRoundTrip(t *testing.T) {
	t.Parallel()
	src := &FlowSource{Name: "sign-up", Steps: []FlowStep{
		{ID: "s1", Label: `The "Start" step`, Screen: "login"},
		{ID: "s2", Label: "Done", Screen: "login", Trigger: `tap "Next"`, Next: "s2"},
	}}
	doc := DeriveFlowMMD(src, fsTestScreens())
	mmd := string(RenderFlowMMD(doc, "fnv1a64:deadbeefdeadbeef"))

	assert.Contains(t, mmd, `s1["The #quot;Start#quot; step"]`, "a quote in a label renders escaped")
	assert.Contains(t, mmd, `-->|tap #quot;Next#quot;|`, "a quote in a trigger renders escaped")

	fc := ParseFlowchart(mmd)
	require.Empty(t, fc.BadLines, "escaped quotes must not break the statement: %s", mmd)
	assert.True(t, fc.Nodes["s1"].ID == "s1" && fc.Nodes["s2"].ID == "s2", "both step nodes survive")
}
