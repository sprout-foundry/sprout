package events

import (
	"encoding/json"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestProgressEventConstants pins the SP-151 §151a event type strings.
// They are the public wire contract (SP-151 §151d — mirrored by the
// @sprout/events TypeScript union), so a rename must be a deliberate spec
// change, not a typo.
func TestProgressEventConstants(t *testing.T) {
	for _, tc := range []struct {
		name string
		got  string
		want string
	}{
		{"milestone", EventTypeProgressMilestone, "progress_milestone"},
		{"question", EventTypeProgressQuestion, "progress_question"},
		{"verification", EventTypeProgressVerification, "progress_verification"},
		{"complete", EventTypeProgressComplete, "progress_complete"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.got)
		})
	}
}

// marshalEventPayload marshals a UIEvent with the given type and payload and
// returns its `data` object as a map, ready for key assertions.
func marshalEventPayload(t *testing.T, evtType string, data any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(UIEvent{Type: evtType, Data: data})
	require.NoError(t, err)
	var envelope map[string]any
	require.NoError(t, json.Unmarshal(raw, &envelope))
	payload, ok := envelope["data"].(map[string]any)
	require.True(t, ok, "data must marshal to a JSON object")
	return payload
}

// sortedKeys returns the map keys in a stable order for key-set assertions.
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// TestProgressMilestone_MarshalJSONFieldNames pins the JSON wire names of
// the progress_milestone payload (SP-151 §151d). A json-tag change is a
// breaking change for consumers of the public event schema.
func TestProgressMilestone_MarshalJSONFieldNames(t *testing.T) {
	payload := marshalEventPayload(t, EventTypeProgressMilestone, ProgressMilestoneData{
		RunID:        "run-1",
		PlanRevision: 3,
		ScopeID:      "scope-signup",
		ScopeTitle:   "Sign-up form",
		Phase:        MilestonePhaseFinished,
		FilesTouched: 4,
		ElapsedMs:    42000,
	})

	assert.Equal(t, "run-1", payload["run_id"])
	assert.Equal(t, float64(3), payload["plan_revision"])
	assert.Equal(t, "scope-signup", payload["scope_id"])
	assert.Equal(t, "Sign-up form", payload["scope_title"])
	assert.Equal(t, "finished", payload["phase"])
	assert.Equal(t, float64(4), payload["files_touched"])
	assert.Equal(t, float64(42000), payload["elapsed_ms"])
	assert.Equal(t,
		[]string{"elapsed_ms", "files_touched", "phase", "plan_revision", "run_id", "scope_id", "scope_title"},
		sortedKeys(payload),
	)
}

// TestProgressMilestone_OmitemptyWithoutScope pins the wire shape when a
// run has no plan scope: scope_id/scope_title/files_touched are omitted,
// the correlation IDs and phase are still present.
func TestProgressMilestone_OmitemptyWithoutScope(t *testing.T) {
	payload := marshalEventPayload(t, EventTypeProgressMilestone, ProgressMilestoneData{
		RunID:        "run-1",
		PlanRevision: 0,
		Phase:        MilestonePhaseStarted,
	})

	assert.Equal(t, "run-1", payload["run_id"])
	assert.Equal(t, float64(0), payload["plan_revision"])
	assert.Equal(t, "started", payload["phase"])
	assert.Equal(t, float64(0), payload["elapsed_ms"])
	assert.Equal(t,
		[]string{"elapsed_ms", "phase", "plan_revision", "run_id"},
		sortedKeys(payload),
	)
}

// TestProgressQuestion_MarshalJSONFieldNames pins the JSON wire names of
// the progress_question payload, including the re-used ask_user option
// shape.
func TestProgressQuestion_MarshalJSONFieldNames(t *testing.T) {
	payload := marshalEventPayload(t, EventTypeProgressQuestion, ProgressQuestionData{
		RunID:        "run-1",
		PlanRevision: 3,
		ScopeID:      "scope-signup",
		Question:     "Which auth method?",
		Header:       "Auth",
		Options: []AskUserRequestOption{
			{Label: "OAuth", Description: "external IdP"},
			{Label: "Magic link"},
		},
		WhyItMatters: "locks in the session design",
	})

	assert.Equal(t, "run-1", payload["run_id"])
	assert.Equal(t, float64(3), payload["plan_revision"])
	assert.Equal(t, "scope-signup", payload["scope_id"])
	assert.Equal(t, "Which auth method?", payload["question"])
	assert.Equal(t, "Auth", payload["header"])
	assert.Equal(t, "locks in the session design", payload["why_it_matters"])
	opts := payload["options"].([]any)
	require.Len(t, opts, 2)
	assert.Equal(t, map[string]any{"label": "OAuth", "description": "external IdP"}, opts[0])
	assert.Equal(t, map[string]any{"label": "Magic link"}, opts[1])
	assert.Equal(t,
		[]string{"header", "options", "plan_revision", "question", "run_id", "scope_id", "why_it_matters"},
		sortedKeys(payload),
	)
}

// TestProgressQuestion_OmitemptyFreeform pins the wire shape of a
// freeform question: options and why_it_matters omitted.
func TestProgressQuestion_OmitemptyFreeform(t *testing.T) {
	payload := marshalEventPayload(t, EventTypeProgressQuestion, ProgressQuestionData{
		RunID:    "run-1",
		Question: "How should this be named?",
	})

	assert.Equal(t, []string{"plan_revision", "question", "run_id"}, sortedKeys(payload))
}

// TestProgressVerification_MarshalJSONFieldNames pins the JSON wire names
// of the progress_verification payload: the checks, pass/fail, and
// evidence references (SP-149 result, compacted).
func TestProgressVerification_MarshalJSONFieldNames(t *testing.T) {
	payload := marshalEventPayload(t, EventTypeProgressVerification, ProgressVerificationData{
		RunID:        "run-1",
		PlanRevision: 3,
		Passed:       true,
		Checks: []ProgressVerificationCheck{
			{Kind: "build", Items: []string{"acc-1"}, Command: "make build", Passed: true},
			{Kind: "test", Skipped: true, Reason: "no trusted test command"},
		},
	})

	assert.Equal(t, "run-1", payload["run_id"])
	assert.Equal(t, float64(3), payload["plan_revision"])
	assert.Equal(t, true, payload["passed"])
	checks := payload["checks"].([]any)
	require.Len(t, checks, 2)
	assert.Equal(t, map[string]any{
		"kind": "build", "items": []any{"acc-1"}, "command": "make build", "passed": true,
	}, checks[0])
	assert.Equal(t, map[string]any{
		"kind": "test", "skipped": true, "reason": "no trusted test command",
	}, checks[1])
	assert.Equal(t,
		[]string{"checks", "passed", "plan_revision", "run_id"},
		sortedKeys(payload),
	)
}

// TestProgressVerification_BaselineErrors pins the baseline-mode fields.
func TestProgressVerification_BaselineErrors(t *testing.T) {
	payload := marshalEventPayload(t, EventTypeProgressVerification, ProgressVerificationData{
		RunID:    "run-2",
		Baseline: true,
		Checks:   []ProgressVerificationCheck{},
		Errors:   []string{"unreadable manifest"},
	})

	assert.Equal(t, true, payload["baseline"])
	assert.Equal(t, []any{"unreadable manifest"}, payload["errors"])
}

// TestProgressComplete_Verified pins the JSON wire names of the
// progress_complete payload when a passing verification result exists.
func TestProgressComplete_Verified(t *testing.T) {
	payload := marshalEventPayload(t, EventTypeProgressComplete, ProgressCompleteData{
		RunID:        "run-1",
		PlanRevision: 3,
		Verified:     true,
		Verification: &ProgressVerificationData{
			RunID:        "run-1",
			PlanRevision: 3,
			Passed:       true,
			Checks:       []ProgressVerificationCheck{{Kind: "build", Passed: true}},
		},
	})

	assert.Equal(t, "run-1", payload["run_id"])
	assert.Equal(t, float64(3), payload["plan_revision"])
	assert.Equal(t, true, payload["verified"])
	verification := payload["verification"].(map[string]any)
	assert.Equal(t, "run-1", verification["run_id"])
	assert.Equal(t, true, verification["passed"])
	assert.Len(t, verification["checks"].([]any), 1)
	assert.Equal(t,
		[]string{"plan_revision", "run_id", "verification", "verified"},
		sortedKeys(payload),
	)
}

// TestProgressComplete_NotVerified pins the wire shape when SP-149 is
// disabled or was not run: verification is absent (nil pointer), and
// not_verified_reason explains why.
func TestProgressComplete_NotVerified(t *testing.T) {
	payload := marshalEventPayload(t, EventTypeProgressComplete, ProgressCompleteData{
		RunID:             "run-2",
		PlanRevision:      0,
		NotVerifiedReason: "verification disabled",
	})

	assert.Equal(t, "verification disabled", payload["not_verified_reason"])
	assert.NotContains(t, payload, "verification", "nil Verification must be omitted, not null")
	assert.NotContains(t, payload, "verified", "false Verified must be omitted")
	assert.Equal(t,
		[]string{"not_verified_reason", "plan_revision", "run_id"},
		sortedKeys(payload),
	)
}
