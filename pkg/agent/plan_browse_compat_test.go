package agent

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sprout-foundry/sprout/pkg/plancontract"
)

// TestPlanInteractionStepsParseBrowseCompatible pins the wire-format
// contract between the structured plan and the browse tool: the
// browse steps carried by an interaction acceptance item
// (plancontract.BrowseStep) must parse cleanly through this handler's
// parseBrowseSteps, field for field.
//
// plancontract deliberately does not import pkg/webcontent (which drags in a
// headless browser): it must stay a pure package usable by every reader and
// writer of the plan, including WASM builds. Compatibility is therefore a
// wire-format contract, and this test is the pin that keeps the two
// formats in sync — when webcontent.BrowseStep gains a field or a tag
// changes, plancontract.BrowseStep and this test must change with it.
func TestPlanInteractionStepsParseBrowseCompatible(t *testing.T) {
	// A valid plan whose interaction item exercises every step field.
	p := plancontract.New("Verify the login flow", time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC))
	p.Scope = []plancontract.ScopeItem{{ID: "s1", Title: "Session UI"}}
	p.Acceptance = []plancontract.Acceptance{{
		ID:    "a1",
		Scope: "s1",
		Check: "type credentials, submit, land on /home",
		Kind:  plancontract.KindInteraction,
		Steps: []plancontract.BrowseStep{
			{Action: "fill", Selector: "#email", Value: "alice@example.com"},
			{Action: "press", Key: "Enter"},
			{Action: "sleep", Millis: 500},
			{Action: "eval", Script: "document.title"},
			{Action: "assert_text", Expect: "Welcome, alice"},
			{Action: "screenshot_selector", Selector: "header", ScreenshotPath: "shot.png"},
		},
	}}
	require.NoError(t, plancontract.Validate(p), "the fixture plan must be valid")

	// Round-trip through the plan's JSON wire form: this is the shape the
	// steps have when read back from .sprout/plan.json.
	b, err := json.Marshal(p)
	require.NoError(t, err)
	var wire map[string]any
	require.NoError(t, json.Unmarshal(b, &wire))
	rawAcceptance, ok := wire["acceptance"].([]any)
	require.True(t, ok, "acceptance must be a JSON array")
	rawItem, ok := rawAcceptance[0].(map[string]any)
	require.True(t, ok, "an acceptance item must be a JSON object")
	rawSteps, ok := rawItem["steps"].([]any)
	require.True(t, ok, "the interaction item's steps must be a JSON array")

	// The exact raw form parseBrowseSteps consumes (the same shape the
	// browse tool receives from model-supplied "steps" arguments).
	steps, err := parseBrowseSteps(rawSteps)
	require.NoError(t, err, "plan steps must parse through the browse step parser")
	require.Len(t, steps, len(p.Acceptance[0].Steps))

	for i := range steps {
		want := p.Acceptance[0].Steps[i]
		assert.Equal(t, want.Action, steps[i].Action, "step %d: action", i)
		assert.Equal(t, want.Selector, steps[i].Selector, "step %d: selector", i)
		assert.Equal(t, want.Value, steps[i].Value, "step %d: value", i)
		assert.Equal(t, want.Key, steps[i].Key, "step %d: key", i)
		assert.Equal(t, want.Millis, steps[i].Millis, "step %d: millis", i)
		assert.Equal(t, want.Script, steps[i].Script, "step %d: script", i)
		assert.Equal(t, want.Expect, steps[i].Expect, "step %d: expect", i)
		assert.Equal(t, want.ScreenshotPath, steps[i].ScreenshotPath, "step %d: screenshot_path", i)
	}
}
