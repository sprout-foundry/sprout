package plancontract

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// basePlan returns a valid plan: two scope items each covered by at least one
// acceptance item, well-formed steps, and a documented out_of_scope item.
func basePlan() *Plan {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	p := New("Add user authentication to the web app", now)
	p.Revision = 3
	p.Updated = time.Date(2026, 10, 3, 14, 30, 0, 0, time.UTC)
	p.Design = "design/screens/login.html"
	p.Starter = "web-app"
	p.Scope = []ScopeItem{
		{ID: "s1", Title: "Auth API", Description: "Login and token endpoints"},
		{ID: "s2", Title: "Session UI", Description: "Login screen"},
	}
	p.Steps = []Step{
		{Scope: "s1", Description: "Implement /login and /token endpoints"},
		{Scope: "s2", Description: "Add login screen and wire it to the API"},
	}
	p.Acceptance = []Acceptance{
		{ID: "a1", Scope: "s1", Check: "make build", Kind: KindBuild},
		{ID: "a2", Scope: "s1", Check: "go test ./pkg/auth/...", Kind: KindTest},
		{ID: "a3", Scope: "s2", Check: "/login renders", Kind: KindPage},
		{ID: "a4", Scope: "s2", Check: "type credentials, submit, land on /home", Kind: KindInteraction},
		{ID: "a5", Scope: "s2", Check: "verify session persists across reload", Kind: KindManual},
	}
	p.OutOfScope = []OutOfScope{
		{Item: "OAuth providers", Reason: "deferred to a follow-up plan"},
	}
	return p
}

// fixtureValid is the canonical on-disk plan document: a fully-populated,
// valid .sprout/plan.json. It doubles as the fixture the "valid plan passes"
// case validates.
const fixtureValid = `{
  "version": 1,
  "revision": 3,
  "created": "2026-10-03T12:00:00Z",
  "updated": "2026-10-03T14:30:00Z",
  "goal": "Add user authentication to the web app",
  "scope": [
    {"id": "s1", "title": "Auth API", "description": "Login and token endpoints"},
    {"id": "s2", "title": "Session UI", "description": "Login screen"}
  ],
  "steps": [
    {"scope": "s1", "description": "Implement /login and /token endpoints"},
    {"scope": "s2", "description": "Add login screen and wire it to the API"}
  ],
  "design": "design/screens/login.html",
  "starter": "web-app",
  "acceptance": [
    {"id": "a1", "scope": "s1", "check": "make build", "kind": "build"},
    {"id": "a2", "scope": "s1", "check": "go test ./pkg/auth/...", "kind": "test"},
    {"id": "a3", "scope": "s2", "check": "/login renders", "kind": "page"},
    {"id": "a4", "scope": "s2", "check": "type credentials, submit, land on /home", "kind": "interaction"},
    {"id": "a5", "scope": "s2", "check": "verify session persists across reload", "kind": "manual"}
  ],
  "out_of_scope": [
    {"item": "OAuth providers", "reason": "deferred to a follow-up plan"}
  ]
}`

// fixtureScopeNoAcceptance is a plan whose second scope item (s2) has no
// acceptance item covering it.
const fixtureScopeNoAcceptance = `{
  "version": 1,
  "revision": 1,
  "created": "2026-10-03T12:00:00Z",
  "updated": "2026-10-03T12:00:00Z",
  "goal": "Add user authentication",
  "scope": [
    {"id": "s1", "title": "Auth API"},
    {"id": "s2", "title": "Session UI"}
  ],
  "acceptance": [
    {"id": "a1", "scope": "s1", "check": "make build", "kind": "build"}
  ]
}`

// TestValidateJSON is the table test over JSON fixtures required by SP-148
// §148a: a valid plan passes, and duplicate IDs, an acceptance item without a
// kind, an unknown kind, and a scope item with no acceptance item each fail
// with a clear message.
func TestValidateJSON(t *testing.T) {
	cases := []struct {
		name       string
		jsonStr    string
		wantValid  bool
		wantSubstr string // when set, the error must contain this substring
	}{
		{
			name:      "valid plan passes",
			jsonStr:   fixtureValid,
			wantValid: true,
		},
		{
			name:       "duplicate scope id fails",
			jsonStr:    strings.Replace(fixtureValid, `"id": "s2"`, `"id": "s1"`, 1),
			wantValid:  false,
			wantSubstr: `duplicate scope id "s1"`,
		},
		{
			name:       "duplicate acceptance id fails",
			jsonStr:    strings.Replace(fixtureValid, `"id": "a2"`, `"id": "a1"`, 1),
			wantValid:  false,
			wantSubstr: `duplicate acceptance id "a1"`,
		},
		{
			name:       "acceptance item without a kind fails",
			jsonStr:    strings.Replace(fixtureValid, `"kind": "build"`, `"kind": ""`, 1),
			wantValid:  false,
			wantSubstr: `is missing kind`,
		},
		{
			name:       "unknown kind fails",
			jsonStr:    strings.Replace(fixtureValid, `"kind": "test"`, `"kind": "vibe"`, 1),
			wantValid:  false,
			wantSubstr: `unknown kind "vibe"`,
		},
		{
			name:       "scope item with no acceptance item fails",
			jsonStr:    fixtureScopeNoAcceptance,
			wantValid:  false,
			wantSubstr: `scope item "s2" has no acceptance item`,
		},
		{
			name:       "out_of_scope item without a reason fails",
			jsonStr:    strings.Replace(fixtureValid, `"reason": "deferred to a follow-up plan"`, `"reason": ""`, 1),
			wantValid:  false,
			wantSubstr: `reason is required`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := ValidateJSON([]byte(tc.jsonStr))
			if tc.wantValid {
				require.NoError(t, err, "expected a valid plan")
				require.NotNil(t, plan)
				return
			}
			require.Error(t, err, "expected validation to fail")
			require.NotNil(t, plan, "the decoded plan should still be returned")
			var vErr *ValidationError
			require.ErrorAs(t, err, &vErr, "expected a *ValidationError")
			assert.NotEmpty(t, vErr.Problems, "expected at least one problem")
			if tc.wantSubstr != "" {
				assert.Contains(t, err.Error(), tc.wantSubstr,
					"error message should explain the problem: %v", err)
			}
		})
	}
}

// TestValidateTable is a struct-level table test covering the structural
// invariants beyond the JSON-fixture cases above.
func TestValidateTable(t *testing.T) {
	mutate := func(fn func(p *Plan)) *Plan {
		p := basePlan()
		fn(p)
		return p
	}

	cases := []struct {
		name       string
		plan       *Plan
		wantValid  bool
		wantSubstr string
	}{
		{
			name:      "valid plan passes",
			plan:      basePlan(),
			wantValid: true,
		},
		{
			name:       "nil plan fails",
			plan:       nil,
			wantValid:  false,
			wantSubstr: "plan is nil",
		},
		{
			name:       "unsupported version fails",
			plan:       mutate(func(p *Plan) { p.Version = 99 }),
			wantValid:  false,
			wantSubstr: "version 99 is not a supported schema version",
		},
		{
			name:       "revision below 1 fails",
			plan:       mutate(func(p *Plan) { p.Revision = 0 }),
			wantValid:  false,
			wantSubstr: "revision must be >= 1",
		},
		{
			name:       "missing created timestamp fails",
			plan:       mutate(func(p *Plan) { p.Created = time.Time{} }),
			wantValid:  false,
			wantSubstr: "created timestamp is required",
		},
		{
			name:       "missing goal fails",
			plan:       mutate(func(p *Plan) { p.Goal = "   " }),
			wantValid:  false,
			wantSubstr: "goal is required",
		},
		{
			name: "scope item without a title fails",
			plan: mutate(func(p *Plan) {
				p.Scope[0].Title = ""
				p.Acceptance = p.Acceptance[:1] // keep only a1 (covers s1) so no coverage error
				p.Steps = p.Steps[:1]
			}),
			wantValid:  false,
			wantSubstr: `scope[0] (id "s1") title is required`,
		},
		{
			name:       "step referencing an unknown scope fails",
			plan:       mutate(func(p *Plan) { p.Steps[0].Scope = "nope" }),
			wantValid:  false,
			wantSubstr: `references unknown scope id "nope"`,
		},
		{
			name:       "step without a description fails",
			plan:       mutate(func(p *Plan) { p.Steps[0].Description = "" }),
			wantValid:  false,
			wantSubstr: "description is required",
		},
		{
			name:       "acceptance referencing an unknown scope fails",
			plan:       mutate(func(p *Plan) { p.Acceptance[0].Scope = "nope" }),
			wantValid:  false,
			wantSubstr: `references unknown scope id "nope"`,
		},
		{
			name:       "out_of_scope item without a name fails",
			plan:       mutate(func(p *Plan) { p.OutOfScope[0].Item = "" }),
			wantValid:  false,
			wantSubstr: "out_of_scope[0].item is required",
		},
		{
			name:       "out_of_scope item without a reason fails",
			plan:       mutate(func(p *Plan) { p.OutOfScope[0].Reason = "" }),
			wantValid:  false,
			wantSubstr: `reason is required`,
		}, {
			name: "multiple problems are all reported",
			plan: mutate(func(p *Plan) {
				p.Goal = ""
				p.Acceptance[0].Kind = Kind("vibe")
				p.Acceptance[1].ID = p.Acceptance[0].ID // duplicate a1
			}),
			wantValid:  false,
			wantSubstr: "problem(s)",
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

// TestValidateReportsAllProblems confirms the validator surfaces every
// problem at once (not just the first), which is what makes the messages
// actionable in a single edit pass.
func TestValidateReportsAllProblems(t *testing.T) {
	p := basePlan()
	p.Goal = ""                     // goal required
	p.Version = 7                   // unsupported version
	p.Acceptance[0].ID = ""         // acceptance id required
	p.Acceptance[1].Kind = ""       // missing kind
	p.Acceptance[2].Scope = "ghost" // unknown scope ref

	err := Validate(p)
	require.Error(t, err)
	var vErr *ValidationError
	require.ErrorAs(t, err, &vErr)
	assert.GreaterOrEqual(t, len(vErr.Problems), 5,
		"expected every problem to be reported, got: %v", vErr.Problems)
}

// TestJSONFieldNamesMatchSpec pins the on-disk contract: the top-level JSON
// keys must be exactly the spec's field list, and nested items must use the
// documented field names.
func TestJSONFieldNamesMatchSpec(t *testing.T) {
	b, err := json.Marshal(basePlan())
	require.NoError(t, err)

	var m map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(b, &m))

	wantTopLevel := []string{
		"version", "revision", "created", "updated", "goal",
		"scope", "steps", "design", "starter", "acceptance", "out_of_scope",
	}
	for _, k := range wantTopLevel {
		assert.Contains(t, m, k, "missing top-level JSON key %q", k)
	}
	assert.Equal(t, len(wantTopLevel), len(m), "unexpected top-level JSON keys: %v", keys(m))

	var scopeItems []struct {
		ID          string `json:"id"`
		Title       string `json:"title"`
		Description string `json:"description"`
	}
	require.NoError(t, json.Unmarshal(m["scope"], &scopeItems))
	assert.Equal(t, "s1", scopeItems[0].ID)
	assert.Equal(t, "Auth API", scopeItems[0].Title)

	var accItems []struct {
		ID    string `json:"id"`
		Scope string `json:"scope"`
		Check string `json:"check"`
		Kind  Kind   `json:"kind"`
	}
	require.NoError(t, json.Unmarshal(m["acceptance"], &accItems))
	assert.Equal(t, "a1", accItems[0].ID)
	assert.Equal(t, KindBuild, accItems[0].Kind)

	var oosItems []struct {
		Item   string `json:"item"`
		Reason string `json:"reason"`
	}
	require.NoError(t, json.Unmarshal(m["out_of_scope"], &oosItems))
	assert.Equal(t, "OAuth providers", oosItems[0].Item)
}

// TestNewInitializesEmptyPlan checks New sets the lifecycle fields and
// initializes every collection to an empty (non-nil) slice.
func TestNewInitializesEmptyPlan(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	p := New("Ship the new plan format", now)

	assert.Equal(t, SchemaVersion, p.Version)
	assert.Equal(t, 1, p.Revision)
	assert.Equal(t, now, p.Created)
	assert.Equal(t, now, p.Updated)
	assert.NotNil(t, p.Scope)
	assert.NotNil(t, p.Steps)
	assert.NotNil(t, p.Acceptance)
	assert.NotNil(t, p.OutOfScope)
	assert.Empty(t, p.Scope)
	assert.Empty(t, p.Steps)
	assert.Empty(t, p.Acceptance)
	assert.Empty(t, p.OutOfScope)
}

// TestBumpRevisionAndSummary covers the small lifecycle helpers.
func TestBumpRevisionAndSummary(t *testing.T) {
	p := basePlan()
	before := p.Revision
	updated := p.Updated.Add(time.Hour)
	p.BumpRevision(updated)
	assert.Equal(t, before+1, p.Revision)
	assert.Equal(t, updated, p.Updated)

	summary := p.Summary()
	assert.Contains(t, summary, "Auth API")
	assert.Contains(t, summary, "Session UI")
	assert.Contains(t, summary, "Add user authentication to the web app")
}

// TestScopeHelpers covers the plan accessors.
func TestScopeHelpers(t *testing.T) {
	p := basePlan()
	got, ok := p.ScopeByID("s2")
	require.True(t, ok)
	assert.Equal(t, "Session UI", got.Title)
	_, ok = p.ScopeByID("zzz")
	assert.False(t, ok)

	// nil receiver is safe.
	var nilPlan *Plan
	_, ok = nilPlan.ScopeByID("s1")
	assert.False(t, ok)
	assert.Empty(t, nilPlan.AcceptanceForScope("s1"))

	a2 := p.AcceptanceForScope("s2")
	require.Len(t, a2, 3)
	assert.Equal(t, []string{"a3", "a4", "a5"}, ids(a2))
}

// TestKindHelpers covers the kind introspection helpers.
func TestKindHelpers(t *testing.T) {
	assert.Equal(t,
		[]Kind{KindBuild, KindTest, KindPage, KindInteraction, KindManual},
		AllKinds())
	for _, k := range AllKinds() {
		assert.True(t, ValidKind(k), "expected %s to be a valid kind", k)
		assert.NotEmpty(t, KindDescription(k), "expected a description for %s", k)
	}
	assert.False(t, ValidKind(Kind("vibe")))
	assert.False(t, ValidKind(""))
	assert.Equal(t, "", KindDescription(Kind("vibe")))
}

// TestRoundTrip confirms a plan survives a JSON marshal/unmarshal cycle
// unchanged (the contract that makes .sprout/plan.json a stable file).
func TestRoundTrip(t *testing.T) {
	p := basePlan()
	b, err := json.Marshal(p)
	require.NoError(t, err)

	var decoded Plan
	require.NoError(t, json.Unmarshal(b, &decoded))
	require.NoError(t, Validate(&decoded))
	assert.Equal(t, *p, decoded, "round-tripped plan should be identical")
}

func keys(m map[string]json.RawMessage) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}

func ids(as []Acceptance) []string {
	out := make([]string, len(as))
	for i, a := range as {
		out[i] = a.ID
	}
	return out
}
