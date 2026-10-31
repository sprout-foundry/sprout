package webcontent

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBrowseStepActions_CanonicalSet pins the canonical vocabulary: sorted,
// unique, and exactly the 20 verbs the executor dispatches.
func TestBrowseStepActions_CanonicalSet(t *testing.T) {
	t.Parallel()

	actions := BrowseStepActions()
	require.Len(t, actions, 20, "the canonical step vocabulary has 20 verbs")

	for i := 1; i < len(actions); i++ {
		assert.Less(t, actions[i-1], actions[i], "actions must be sorted and unique")
	}

	expected := []string{
		"assert_selector", "assert_text", "assert_title", "assert_url", "back",
		"click", "eval", "fill", "forward", "hover", "navigate", "press",
		"reload", "screenshot_selector", "scroll_to", "sleep", "type",
		"wait_for", "wait_for_function", "wait_for_text",
	}
	assert.Equal(t, expected, actions)
}

// TestBrowseStepActions_DefensiveCopy guards against a caller (tool schema,
// docs) mutating the canonical set.
func TestBrowseStepActions_DefensiveCopy(t *testing.T) {
	t.Parallel()

	first := BrowseStepActions()
	first[0] = "mutated"

	assert.NotEqual(t, "mutated", BrowseStepActions()[0], "BrowseStepActions must return a copy")
}

func TestBrowseActions_CanonicalSet(t *testing.T) {
	t.Parallel()

	actions := BrowseActions()
	assert.Equal(t, []string{"dom", "inspect", "interact", "screenshot", "text"}, actions)

	actions[0] = "mutated"
	assert.Equal(t, "dom", BrowseActions()[0], "BrowseActions must return a copy")
}

func TestNormalizeBrowseAction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
		ok    bool
	}{
		{"empty defaults to text", "", "text", true},
		{"whitespace defaults to text", "   ", "text", true},
		{"canonical", "inspect", "inspect", true},
		{"uppercase", "DOM", "dom", true},
		{"mixed case and padding", "  Interact  ", "interact", true},
		{"screenshot", "screenshot", "screenshot", true},
		{"unknown", "bogus", "", false},
		{"step verb is not a top-level action", "click", "", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := NormalizeBrowseAction(tc.input)
			assert.Equal(t, tc.ok, ok, "ok")
			assert.Equal(t, tc.want, got, "action")
		})
	}
}

func TestNormalizeBrowseStepAction_Canonical(t *testing.T) {
	t.Parallel()

	for _, action := range BrowseStepActions() {
		t.Run(action, func(t *testing.T) {
			t.Parallel()
			got, err := NormalizeBrowseStepAction(action, "")
			require.NoError(t, err)
			assert.Equal(t, action, got)
		})
	}
}

func TestNormalizeBrowseStepAction_Aliases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		action   string
		selector string
		want     string
	}{
		{"wait with selector is wait_for", "wait", "#result", "wait_for"},
		{"wait without selector is sleep", "wait", "", "sleep"},
		{"wait with whitespace selector is sleep", "wait", "   ", "sleep"},
		{"wait_for_selector is wait_for", "wait_for_selector", "", "wait_for"},
		{"screenshot with selector is screenshot_selector", "screenshot", "#chart", "screenshot_selector"},
		{"goto is navigate", "goto", "", "navigate"},
		{"keypress is press", "keypress", "", "press"},
		{"delay is sleep", "delay", "", "sleep"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := NormalizeBrowseStepAction(tc.action, tc.selector)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestNormalizeBrowseStepAction_ScreenshotWithoutSelector pins the helpful
// error: "screenshot" is not a step action on its own, and the message must
// point at both real alternatives.
func TestNormalizeBrowseStepAction_ScreenshotWithoutSelector(t *testing.T) {
	t.Parallel()

	got, err := NormalizeBrowseStepAction("screenshot", "")
	require.Error(t, err)
	assert.Empty(t, got)
	assert.Contains(t, err.Error(), "not a browse step action")
	assert.Contains(t, err.Error(), `action:"screenshot"`)
	assert.Contains(t, err.Error(), "screenshot_selector")
}

func TestNormalizeBrowseStepAction_CaseAndWhitespace(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{"Click", "click"},
		{"  CLICK  ", "click"},
		{"Wait_For", "wait_for"},
		{"\tSLEEP\n", "sleep"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			got, err := NormalizeBrowseStepAction(tc.input, "")
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestNormalizeBrowseStepAction_Empty(t *testing.T) {
	t.Parallel()

	for _, input := range []string{"", "   ", "\t"} {
		got, err := NormalizeBrowseStepAction(input, "")
		require.Error(t, err, "empty action %q must be rejected", input)
		assert.Empty(t, got)
		assert.Contains(t, err.Error(), "action is required")
		assert.Contains(t, err.Error(), "click", "the error names the valid actions")
	}
}

func TestNormalizeBrowseStepAction_Unknown(t *testing.T) {
	t.Parallel()

	got, err := NormalizeBrowseStepAction("bogus", "")
	require.Error(t, err)
	assert.Empty(t, got)
	assert.Contains(t, err.Error(), "unknown browse step action")
	assert.Contains(t, err.Error(), "click", "the error lists the valid actions")
}

func TestNormalizeBrowseStepAction_UnknownSuggestsNearMiss(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{"asert_text", "assert_text"},
		{"wait_for_txt", "wait_for_text"},
		{"clik", "click"},
		{"navigat", "navigate"},
		{"screenshot_selectr", "screenshot_selector"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			_, err := NormalizeBrowseStepAction(tc.input, "")
			require.Error(t, err)
			assert.Contains(t, err.Error(), "did you mean")
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestSuggestBrowseStepAction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty has no suggestion", "", ""},
		{"whitespace has no suggestion", "   ", ""},
		{"exact verb suggests itself", "click", "click"},
		{"case-insensitive exact", "CLICK", "click"},
		{"one-char typo", "clik", "click"},
		{"transposition", "clcik", "click"},
		{"truncation", "navigat", "navigate"},
		{"prefix of a longer verb", "wait_for_tex", "wait_for_text"},
		{"unrelated word has no suggestion", "banana", ""},
		{"far-off word has no suggestion", "zzzzzzzzzzzz", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, SuggestBrowseStepAction(tc.input))
		})
	}
}

func TestLevenshtein(t *testing.T) {
	t.Parallel()

	tests := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"", "abc", 3},
		{"abc", "", 3},
		{"abc", "abc", 0},
		{"abc", "abd", 1},
		{"kitten", "sitting", 3},
		{"clik", "click", 1},
		{"clcik", "click", 2},
	}

	for _, tc := range tests {
		t.Run(tc.a+"_"+tc.b, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, levenshtein(tc.a, tc.b))
		})
	}
}

func TestParseBrowseSteps_Valid(t *testing.T) {
	t.Parallel()

	raw := []interface{}{
		map[string]interface{}{"action": "click", "selector": "#submit"},
		map[string]interface{}{"action": "fill", "selector": "#email", "value": "a@b.c"},
		map[string]interface{}{"action": "wait_for_text", "expect": "Dashboard"},
		map[string]interface{}{"action": "sleep", "millis": float64(500)},
	}

	steps, err := ParseBrowseSteps(raw)
	require.NoError(t, err)
	require.Len(t, steps, 4)

	assert.Equal(t, "click", steps[0].Action)
	assert.Equal(t, "#submit", steps[0].Selector)
	assert.Equal(t, "fill", steps[1].Action)
	assert.Equal(t, "a@b.c", steps[1].Value)
	assert.Equal(t, "wait_for_text", steps[2].Action)
	assert.Equal(t, "Dashboard", steps[2].Expect)
	assert.Equal(t, "sleep", steps[3].Action)
	assert.Equal(t, 500, steps[3].Millis)
}

func TestParseBrowseSteps_NormalizesAliases(t *testing.T) {
	t.Parallel()

	raw := []interface{}{
		map[string]interface{}{"action": "wait", "selector": "#result"},
		map[string]interface{}{"action": "wait", "millis": float64(250)},
		map[string]interface{}{"action": "goto", "value": "https://example.com"},
		map[string]interface{}{"action": "delay", "millis": float64(100)},
		map[string]interface{}{"action": "screenshot", "selector": "#chart"},
	}

	steps, err := ParseBrowseSteps(raw)
	require.NoError(t, err)
	require.Len(t, steps, 5)

	assert.Equal(t, "wait_for", steps[0].Action, "wait + selector normalizes to wait_for")
	assert.Equal(t, "sleep", steps[1].Action, "wait without selector normalizes to sleep")
	assert.Equal(t, "navigate", steps[2].Action, "goto normalizes to navigate")
	assert.Equal(t, "sleep", steps[3].Action, "delay normalizes to sleep")
	assert.Equal(t, "screenshot_selector", steps[4].Action, "screenshot + selector normalizes to screenshot_selector")
}

func TestParseBrowseSteps_Empty(t *testing.T) {
	t.Parallel()

	steps, err := ParseBrowseSteps(nil)
	require.NoError(t, err)
	assert.Empty(t, steps)
}

func TestParseBrowseSteps_NonObjectStep(t *testing.T) {
	t.Parallel()

	_, err := ParseBrowseSteps([]interface{}{"not-a-map"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "browse_url steps[0]: must be an object")
}

func TestParseBrowseSteps_UnknownAction(t *testing.T) {
	t.Parallel()

	raw := []interface{}{
		map[string]interface{}{"action": "click", "selector": "#ok"},
		map[string]interface{}{"action": "bogus"},
	}

	_, err := ParseBrowseSteps(raw)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "browse_url steps[1]:")
	assert.Contains(t, err.Error(), "unknown browse step action")
	// The error names the valid verbs so the caller can fix the step without
	// another round-trip to the browser.
	for _, action := range BrowseStepActions() {
		assert.Contains(t, err.Error(), action, "the error lists every valid action")
	}
}

func TestParseBrowseSteps_MissingAction(t *testing.T) {
	t.Parallel()

	_, err := ParseBrowseSteps([]interface{}{
		map[string]interface{}{"selector": "#btn"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "browse_url steps[0]:")
	assert.Contains(t, err.Error(), "action is required")
}

// TestParseBrowseSteps_TextFieldHint pins the targeted hint for the field the
// bundled skill doc used to teach: "text" is silently dropped by
// encoding/json, so the parser rejects it by name.
func TestParseBrowseSteps_TextFieldHint(t *testing.T) {
	t.Parallel()

	_, err := ParseBrowseSteps([]interface{}{
		map[string]interface{}{"action": "wait_for_text", "text": "Dashboard"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "browse_url steps[0]:")
	assert.Contains(t, err.Error(), `"text" is not a recognized field`)
	assert.Contains(t, err.Error(), `"expect"`)
}

// TestParseBrowseSteps_TextFieldAcceptedWithExpect ensures the hint does not
// fire when the step also carries a real field — a stray "text" key alongside
// "expect" is ignored, not fatal.
func TestParseBrowseSteps_TextFieldAcceptedWithExpect(t *testing.T) {
	t.Parallel()

	steps, err := ParseBrowseSteps([]interface{}{
		map[string]interface{}{"action": "wait_for_text", "text": "ignored", "expect": "Dashboard"},
	})
	require.NoError(t, err)
	require.Len(t, steps, 1)
	assert.Equal(t, "Dashboard", steps[0].Expect)
}

func TestParseBrowseSteps_TextFieldAcceptedWithValue(t *testing.T) {
	t.Parallel()

	steps, err := ParseBrowseSteps([]interface{}{
		map[string]interface{}{"action": "assert_text", "text": "ignored", "value": "Welcome"},
	})
	require.NoError(t, err)
	require.Len(t, steps, 1)
	assert.Equal(t, "Welcome", steps[0].Value)
}

func TestParseBrowseSteps_UnmarshalableStep(t *testing.T) {
	t.Parallel()

	_, err := ParseBrowseSteps([]interface{}{
		map[string]interface{}{"action": "click", "bad": make(chan int)},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "browse_url steps[0]:")
	assert.Contains(t, err.Error(), "marshal failed")
}

// TestParseBrowseSteps_UnknownFieldIgnored documents that an unknown key
// other than "text" is still dropped silently (encoding/json behavior); only
// the "text" trap is called out by name.
func TestParseBrowseSteps_UnknownFieldIgnored(t *testing.T) {
	t.Parallel()

	steps, err := ParseBrowseSteps([]interface{}{
		map[string]interface{}{"action": "click", "selector": "#btn", "unknown_field": "x"},
	})
	require.NoError(t, err)
	require.Len(t, steps, 1)
	assert.Equal(t, "click", steps[0].Action)
}

// TestParseBrowseSteps_ErrorHasNoAgentPrefix keeps the package free of the
// agent error taxonomy: callers wrap these errors, so they must be plain.
func TestParseBrowseSteps_ErrorHasNoAgentPrefix(t *testing.T) {
	t.Parallel()

	_, err := ParseBrowseSteps([]interface{}{map[string]interface{}{"action": "bogus"}})
	require.Error(t, err)
	assert.False(t, strings.Contains(err.Error(), "[InvalidInput]"),
		"webcontent must not construct agent errors; the wrapper does that")
}
