package planstore

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sprout-foundry/sprout/pkg/plancontract"
)

func TestRenderMarkdownFullPlan(t *testing.T) {
	p := testPlan()
	p.Design = "design/screens/login.html"
	p.Starter = "web-app"
	// Bump to a non-1 revision so the metadata is unambiguous.
	p.Revision = 5
	p.Updated = fixedNow

	md := RenderMarkdown(p)

	// Title, metadata block, and every section are present and in order.
	assert.Contains(t, md, "# Add user authentication to the web app")
	assert.Contains(t, md, "- **Schema:** v1")
	assert.Contains(t, md, "- **Revision:** 5")
	assert.Contains(t, md, "- **Design:** design/screens/login.html")
	assert.Contains(t, md, "- **Starter:** web-app")

	assert.Contains(t, md, "## Scope")
	assert.Contains(t, md, "- **s1** — Auth API")
	assert.Contains(t, md, "- **s2** — Session UI")

	assert.Contains(t, md, "## Steps")
	assert.Contains(t, md, "1. **s1** Implement /login and /token endpoints")
	assert.Contains(t, md, "2. **s2** Add login screen and wire it to the API")

	assert.Contains(t, md, "## Acceptance")
	assert.Contains(t, md, "- **a1** (s1, build): make build")
	assert.Contains(t, md, "- **a2** (s2, page): /login renders")

	assert.Contains(t, md, "## Out of scope")
	assert.Contains(t, md, "- OAuth providers — deferred to a follow-up plan")
}

func TestRenderMarkdownOmitsEmptySections(t *testing.T) {
	// A minimal valid plan: goal + one scope item covered by one acceptance
	// item, no steps, no out-of-scope, no design/starter.
	p := plancontract.New("Ship it", fixedCreated)
	p.Scope = []plancontract.ScopeItem{{ID: "s1", Title: "The thing"}}
	p.Acceptance = []plancontract.Acceptance{{ID: "a1", Scope: "s1", Check: "go test ./...", Kind: plancontract.KindTest}}

	md := RenderMarkdown(p)

	assert.NotContains(t, md, "## Steps", "an empty steps section must be omitted")
	assert.NotContains(t, md, "## Out of scope", "an empty out-of-scope section must be omitted")
	assert.NotContains(t, md, "**Design:**", "an unset design reference must be omitted")
	assert.NotContains(t, md, "**Starter:**", "an unset starter reference must be omitted")

	// The sections that have content are still present.
	assert.Contains(t, md, "## Scope")
	assert.Contains(t, md, "## Acceptance")
}

func TestRenderMarkdownManualKindWithoutCheck(t *testing.T) {
	p := plancontract.New("Check the vibe", fixedCreated)
	p.Scope = []plancontract.ScopeItem{{ID: "s1", Title: "Polish"}}
	p.Acceptance = []plancontract.Acceptance{{ID: "a1", Scope: "s1", Kind: plancontract.KindManual}}

	md := RenderMarkdown(p)
	assert.Contains(t, md, "- **a1** (s1, manual): (manual: reported, not run)")
}

func TestRenderMarkdownInteractionSteps(t *testing.T) {
	p := plancontract.New("Check the login flow", fixedCreated)
	p.Scope = []plancontract.ScopeItem{{ID: "s1", Title: "Session UI"}}
	p.Acceptance = []plancontract.Acceptance{{
		ID:    "a1",
		Scope: "s1",
		Check: "type credentials, submit, land on /home",
		Kind:  plancontract.KindInteraction,
		Steps: []plancontract.BrowseStep{
			{Action: "fill", Selector: "#email", Value: "alice@example.com"},
			{Action: "click", Selector: "button[type=submit]"},
			{Action: "assert_text", Expect: "Welcome, alice"},
			// A step carrying every parameter pins the fixed parameter order.
			{Action: "eval", Selector: "#app", Value: "v", Key: "Enter", Millis: 250, Script: "document.title", Expect: "Login", ScreenshotPath: "shot.png"},
			// A bare step renders as its action alone.
			{Action: "reload"},
		},
	}}
	p.OutOfScope = []plancontract.OutOfScope{{Item: "OAuth providers", Reason: "deferred"}}

	md := RenderMarkdown(p)

	// The item line is followed by a numbered step sub-list, in execution
	// order, with key parameters in a fixed order and only when set.
	assert.Contains(t, md, "- **a1** (s1, interaction): type credentials, submit, land on /home")
	assert.Contains(t, md, "  1. fill selector: #email value: alice@example.com")
	assert.Contains(t, md, "  2. click selector: button[type=submit]")
	assert.Contains(t, md, "  3. assert_text expect: Welcome, alice")
	assert.Contains(t, md, "  4. eval selector: #app value: v key: Enter millis: 250 script: document.title expect: Login screenshot_path: shot.png")
	assert.Contains(t, md, "  5. reload")

	// The steps render under the item, before the next section.
	require.True(t, strings.Index(md, "  5. reload") < strings.Index(md, "## Out of scope"),
		"the step sub-list must render before the out-of-scope section")
	assert.Equal(t, RenderMarkdown(p), RenderMarkdown(p), "rendering must be deterministic for a given plan")
}

func TestRenderMarkdownNoStepsForNonInteractionKinds(t *testing.T) {
	p := planstoreTestPlanNoInteraction()

	md := RenderMarkdown(p)
	assert.NotContains(t, md, "  1. ", "a plan without interaction steps must not render a step sub-list")
}

// planstoreTestPlanNoInteraction returns a minimal valid plan whose only
// acceptance item is a build item, so no step sub-list can render.
func planstoreTestPlanNoInteraction() *plancontract.Plan {
	p := plancontract.New("Ship it", fixedCreated)
	p.Scope = []plancontract.ScopeItem{{ID: "s1", Title: "The thing"}}
	p.Acceptance = []plancontract.Acceptance{{ID: "a1", Scope: "s1", Check: "make build", Kind: plancontract.KindBuild}}
	return p
}

func TestRenderMarkdownDeterministic(t *testing.T) {
	p := testPlan()
	assert.Equal(t, RenderMarkdown(p), RenderMarkdown(p), "rendering must be deterministic for a given plan")

	// A nil plan renders nothing (no panic, no spurious content).
	assert.Equal(t, "", RenderMarkdown(nil))
}

func TestRenderMarkdownIsStableAcrossCopies(t *testing.T) {
	a := testPlan()
	b := *a // value copy of the same plan
	require.Equal(t, RenderMarkdown(a), RenderMarkdown(&b), "equal plans must render identically")
}
