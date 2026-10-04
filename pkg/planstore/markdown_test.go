package planstore

import (
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
