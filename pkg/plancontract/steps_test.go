package plancontract

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// interactionStepsPlanJSON is a standalone, valid plan document whose single
// acceptance item is an interaction item carrying browse steps. It is the
// "valid plan with interaction steps passes" fixture for SP-148 §148d.
const interactionStepsPlanJSON = `{
  "version": 1,
  "revision": 1,
  "created": "2026-10-03T12:00:00Z",
  "updated": "2026-10-03T12:00:00Z",
  "goal": "Add user authentication to the web app",
  "scope": [
    {"id": "s1", "title": "Session UI"}
  ],
  "steps": [
    {"scope": "s1", "description": "Add login screen"}
  ],
  "acceptance": [
    {
      "id": "a1",
      "scope": "s1",
      "check": "type credentials, submit, land on /home",
      "kind": "interaction",
      "steps": [
        {"action": "fill", "selector": "#email", "value": "alice@example.com"},
        {"action": "click", "selector": "button[type=submit]"},
        {"action": "assert_text", "expect": "Welcome, alice"}
      ]
    }
  ],
  "out_of_scope": []
}`

// TestInteractionStepsValidJSON is the JSON-fixture half of the SP-148 §148d
// table: a valid plan with an interaction acceptance item carrying browse
// steps (fill + click + assert_text) passes.
func TestInteractionStepsValidJSON(t *testing.T) {
	plan, err := ValidateJSON([]byte(interactionStepsPlanJSON))
	require.NoError(t, err, "a plan whose interaction item carries browse steps must be valid")
	require.NotNil(t, plan)
	require.Len(t, plan.Acceptance, 1)
	require.Len(t, plan.Acceptance[0].Steps, 3)
	assert.Equal(t, "fill", plan.Acceptance[0].Steps[0].Action)
	assert.Equal(t, "assert_text", plan.Acceptance[0].Steps[2].Action)
}

// interactionBasePlan returns a minimal valid plan whose single acceptance
// item is an interaction item with a well-formed step list, so the table
// below can mutate the steps (or the kind) in isolation.
func interactionBasePlan() *Plan {
	p := New("Add user authentication to the web app", time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC))
	p.Scope = []ScopeItem{{ID: "s1", Title: "Session UI"}}
	p.Acceptance = []Acceptance{{
		ID:    "a1",
		Scope: "s1",
		Check: "type credentials, submit, land on /home",
		Kind:  KindInteraction,
		Steps: []BrowseStep{
			{Action: "fill", Selector: "#email", Value: "alice@example.com"},
			{Action: "click", Selector: "button[type=submit]"},
			{Action: "assert_text", Expect: "Welcome, alice"},
		},
	}}
	return p
}

// TestInteractionStepsTable covers the SP-148 §148d validator rules at the
// struct level: an interaction item requires a non-empty steps list whose
// steps each carry a non-empty action, and no other kind may carry steps.
func TestInteractionStepsTable(t *testing.T) {
	mutate := func(fn func(a *Acceptance)) *Plan {
		p := interactionBasePlan()
		fn(&p.Acceptance[0])
		return p
	}

	cases := []struct {
		name       string
		plan       *Plan
		wantValid  bool
		wantSubstr string
	}{
		{
			name:      "interaction item with well-formed steps passes",
			plan:      interactionBasePlan(),
			wantValid: true,
		},
		{
			name:       "interaction item with no steps fails",
			plan:       mutate(func(a *Acceptance) { a.Steps = nil }),
			wantValid:  false,
			wantSubstr: `acceptance[0] (id "a1") is missing steps`,
		},
		{
			name:       "interaction item with an empty steps list fails",
			plan:       mutate(func(a *Acceptance) { a.Steps = []BrowseStep{} }),
			wantValid:  false,
			wantSubstr: `acceptance[0] (id "a1") is missing steps`,
		},
		{
			name:       "a step with an empty action fails, naming the index",
			plan:       mutate(func(a *Acceptance) { a.Steps[1].Action = "" }),
			wantValid:  false,
			wantSubstr: `acceptance[0] (id "a1") steps[1].action is required`,
		},
		{
			name: "a step whose action field is absent (JSON missing) fails, naming the index",
			plan: jsonRoundTripPlan(t, func(p *Plan) {
				// Simulate a plan.json that omits the action field of step 0.
				p.Acceptance[0].Steps[0].Action = ""
			}),
			wantValid:  false,
			wantSubstr: `acceptance[0] (id "a1") steps[0].action is required`,
		},
		{
			name: "two bad steps are both reported",
			plan: mutate(func(a *Acceptance) {
				a.Steps[0].Action = ""
				a.Steps[2].Action = "   "
			}),
			wantValid:  false,
			wantSubstr: `steps[2].action is required`,
		},
		{
			name:       "a build item carrying steps fails",
			plan:       mutate(func(a *Acceptance) { a.Kind = KindBuild; a.Check = "make build" }),
			wantValid:  false,
			wantSubstr: `only kind "interaction" may carry steps`,
		},
		{
			name:       "a page item carrying steps fails",
			plan:       mutate(func(a *Acceptance) { a.Kind = KindPage }),
			wantValid:  false,
			wantSubstr: `only kind "interaction" may carry steps`,
		},
		{
			name:       "a manual item carrying steps fails",
			plan:       mutate(func(a *Acceptance) { a.Kind = KindManual }),
			wantValid:  false,
			wantSubstr: `only kind "interaction" may carry steps`,
		},
		{
			name:       "an item with an unknown kind and steps fails on both",
			plan:       mutate(func(a *Acceptance) { a.Kind = Kind("vibe") }),
			wantValid:  false,
			wantSubstr: `unknown kind "vibe"`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(tc.plan)
			if tc.wantValid {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			if tc.wantSubstr != "" {
				assert.Contains(t, err.Error(), tc.wantSubstr, "got: %v", err)
			}
		})
	}
}

// jsonRoundTripPlan applies fn to a plan, then marshals and unmarshals the
// plan through JSON, so the mutation is observed through the wire format
// (an absent field decodes as the zero value, as .sprout/plan.json would).
func jsonRoundTripPlan(t *testing.T, fn func(p *Plan)) *Plan {
	t.Helper()
	p := interactionBasePlan()
	fn(p)
	b, err := json.Marshal(p)
	require.NoError(t, err)
	var out Plan
	require.NoError(t, json.Unmarshal(b, &out))
	return &out
}

// TestInteractionStepsMalformedJSON is the invalid-fixture half of the
// SP-148 §148d table: the same rules over raw .sprout/plan.json documents.
func TestInteractionStepsMalformedJSON(t *testing.T) {
	cases := []struct {
		name       string
		jsonStr    string
		wantSubstr string
	}{
		{
			name:       "interaction item without steps fails",
			jsonStr:    `{"version":1,"revision":1,"created":"2026-10-03T12:00:00Z","updated":"2026-10-03T12:00:00Z","goal":"g","scope":[{"id":"s1","title":"t"}],"steps":[],"acceptance":[{"id":"a1","scope":"s1","kind":"interaction"}],"out_of_scope":[]}`,
			wantSubstr: `acceptance[0] (id "a1") is missing steps`,
		},
		{
			name:       "step with a missing action fails, naming the index",
			jsonStr:    `{"version":1,"revision":1,"created":"2026-10-03T12:00:00Z","updated":"2026-10-03T12:00:00Z","goal":"g","scope":[{"id":"s1","title":"t"}],"steps":[],"acceptance":[{"id":"a1","scope":"s1","kind":"interaction","steps":[{"selector":"#email","value":"a@b.c"},{"action":"click","selector":"button"}]}],"out_of_scope":[]}`,
			wantSubstr: `acceptance[0] (id "a1") steps[0].action is required`,
		},
		{
			name:       "a test item carrying steps fails",
			jsonStr:    `{"version":1,"revision":1,"created":"2026-10-03T12:00:00Z","updated":"2026-10-03T12:00:00Z","goal":"g","scope":[{"id":"s1","title":"t"}],"steps":[],"acceptance":[{"id":"a1","scope":"s1","check":"go test ./...","kind":"test","steps":[{"action":"click","selector":"button"}]}],"out_of_scope":[]}`,
			wantSubstr: `only kind "interaction" may carry steps`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := ValidateJSON([]byte(tc.jsonStr))
			require.Error(t, err, "expected validation to fail")
			require.NotNil(t, plan, "the decoded plan should still be returned")
			var vErr *ValidationError
			require.ErrorAs(t, err, &vErr, "expected a *ValidationError")
			assert.Contains(t, err.Error(), tc.wantSubstr, "got: %v", err)
		})
	}
}

// TestBrowseStepWireFormat pins the JSON wire format of BrowseStep (SP-148
// §148d): the field names, tags, and omitempty rules must mirror
// webcontent.BrowseStep, which the cross-package pin test in pkg/agent
// (plan_browse_compat_test.go) enforces against parseBrowseSteps.
func TestBrowseStepWireFormat(t *testing.T) {
	full := BrowseStep{
		Action:         "fill",
		Selector:       "#email",
		Value:          "a@b.c",
		Key:            "Enter",
		Millis:         250,
		Script:         "document.title",
		Expect:         "Login",
		ScreenshotPath: "shot.png",
	}
	b, err := json.Marshal(full)
	require.NoError(t, err)

	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	assert.Equal(t, map[string]any{
		"action":          "fill",
		"selector":        "#email",
		"value":           "a@b.c",
		"key":             "Enter",
		"millis":          float64(250),
		"script":          "document.title",
		"expect":          "Login",
		"screenshot_path": "shot.png",
	}, m, "every field must use the documented JSON tag")

	// A minimal step omits every non-action field (the same omitempty rules
	// as webcontent.BrowseStep), so a bare click step stays compact on disk.
	min, err := json.Marshal(BrowseStep{Action: "click"})
	require.NoError(t, err)
	assert.JSONEq(t, `{"action":"click"}`, string(min))
}

// TestInteractionStepsRoundTrip confirms browse steps survive a full plan
// marshal/unmarshal cycle unchanged.
func TestInteractionStepsRoundTrip(t *testing.T) {
	p := interactionBasePlan()
	b, err := json.Marshal(p)
	require.NoError(t, err)

	var decoded Plan
	require.NoError(t, json.Unmarshal(b, &decoded))
	require.NoError(t, Validate(&decoded))
	assert.Equal(t, p.Acceptance[0].Steps, decoded.Acceptance[0].Steps,
		"steps must survive a JSON round trip unchanged")
}
