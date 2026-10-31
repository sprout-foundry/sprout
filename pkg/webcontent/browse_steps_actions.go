// browse_steps_actions.go is the single definition of the browse step
// vocabulary: the canonical action verbs, their accepted aliases, and the
// shared parser that turns raw model-supplied step maps into BrowseStep
// values.
//
// It is deliberately untagged (no //go:build constraint): the browse_url
// tool schema (pkg/agent_tools/browse_url_handler.go) advertises these verbs
// as a JSON-schema enum, and the schema must be identical in native and js
// builds. Only the executor that dispatches the verbs
// (browser_rod_interaction.go) is browser-only.
package webcontent

import (
	"encoding/json"
	"fmt"
	"strings"
)

// browseStepActions is the canonical set of browse step verbs, sorted
// alphabetically. It is the one definition of the step vocabulary: the tool
// schema's enum, the alias resolver, and the executor's handler map are all
// kept in step with it (pkg/webcontent/browse_steps_actions_browser_test.go
// asserts the handler map matches, in both directions).
var browseStepActions = []string{
	"assert_selector",
	"assert_text",
	"assert_title",
	"assert_url",
	"back",
	"click",
	"eval",
	"fill",
	"forward",
	"hover",
	"navigate",
	"press",
	"reload",
	"screenshot_selector",
	"scroll_to",
	"sleep",
	"type",
	"wait_for",
	"wait_for_function",
	"wait_for_text",
}

// browseActions is the canonical set of top-level browse actions (the
// browse_url `action` parameter), sorted alphabetically. The empty action
// resolves to "text" (see NormalizeBrowseAction).
var browseActions = []string{"dom", "inspect", "interact", "screenshot", "text"}

// BrowseStepActions returns the canonical browse step verbs, sorted. The
// result is a defensive copy: callers (tool schemas, docs) may keep or sort
// it without mutating the canonical set.
func BrowseStepActions() []string {
	out := make([]string, len(browseStepActions))
	copy(out, browseStepActions)
	return out
}

// BrowseActions returns the canonical top-level browse actions, sorted. The
// result is a defensive copy.
func BrowseActions() []string {
	out := make([]string, len(browseActions))
	copy(out, browseActions)
	return out
}

// NormalizeBrowseAction resolves a top-level browse action to its canonical
// form. It trims, lowercases, and resolves the empty string to "text" (the
// documented default). The boolean reports whether the value is recognized;
// an unknown action returns ("", false).
func NormalizeBrowseAction(action string) (string, bool) {
	normalized := strings.ToLower(strings.TrimSpace(action))
	if normalized == "" {
		return "text", true
	}
	for _, canonical := range browseActions {
		if normalized == canonical {
			return canonical, true
		}
	}
	return "", false
}

// NormalizeBrowseStepAction resolves a step verb to its canonical form,
// resolving the accepted aliases. It trims and lowercases the input, so
// "Click" and " click " both resolve to "click".
//
// Aliases (exact):
//
//	wait               → wait_for when selector is non-empty, else sleep
//	wait_for_selector  → wait_for
//	screenshot         → screenshot_selector when selector is non-empty
//	goto               → navigate
//	keypress           → press
//	delay              → sleep
//
// The selector is needed because `wait` and `screenshot` are ambiguous
// without it. On failure the error names every valid action and, when a
// near-miss exists, suggests it ("did you mean ...").
func NormalizeBrowseStepAction(action, selector string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(action))
	if normalized == "" {
		return "", fmt.Errorf("browse step action is required (valid actions: %s)", strings.Join(browseStepActions, ", "))
	}

	hasSelector := strings.TrimSpace(selector) != ""

	// Aliases first: they are accepted but never canonical.
	switch normalized {
	case "wait":
		if hasSelector {
			return "wait_for", nil
		}
		return "sleep", nil
	case "wait_for_selector":
		return "wait_for", nil
	case "screenshot":
		if hasSelector {
			return "screenshot_selector", nil
		}
		return "", fmt.Errorf(
			"%q is not a browse step action: a screenshot step needs a selector. "+
				"Use the top-level action:\"screenshot\" with screenshot_path to capture the whole page, "+
				"or the step action \"screenshot_selector\" with a selector and screenshot_path to capture one element",
			action)
	case "goto":
		return "navigate", nil
	case "keypress":
		return "press", nil
	case "delay":
		return "sleep", nil
	}

	for _, canonical := range browseStepActions {
		if normalized == canonical {
			return canonical, nil
		}
	}

	msg := fmt.Sprintf("unknown browse step action %q (valid actions: %s)", action, strings.Join(browseStepActions, ", "))
	if suggestion := SuggestBrowseStepAction(normalized); suggestion != "" {
		msg += fmt.Sprintf("; did you mean %q?", suggestion)
	}
	return "", fmt.Errorf("%s", msg)
}

// SuggestBrowseStepAction returns the canonical step verb closest to input,
// or "" when nothing is close enough. "Close enough" is a Levenshtein
// distance of at most max(2, len(input)/3), which tolerates typos and
// truncations ("asert_text", "wait_for_txt") without suggesting a verb for
// an unrelated word.
func SuggestBrowseStepAction(input string) string {
	normalized := strings.ToLower(strings.TrimSpace(input))
	if normalized == "" {
		return ""
	}

	threshold := len(normalized) / 3
	if threshold < 2 {
		threshold = 2
	}

	best := ""
	bestDistance := 0
	for _, canonical := range browseStepActions {
		distance := levenshtein(normalized, canonical)
		if distance > threshold {
			continue
		}
		// Ties break alphabetically so the suggestion is deterministic.
		if best == "" || distance < bestDistance || (distance == bestDistance && canonical < best) {
			best = canonical
			bestDistance = distance
		}
	}
	return best
}

// levenshtein returns the edit distance between a and b (insertions,
// deletions, substitutions), using the two-row dynamic-programming form.
// Dependency-free by design: the suggestion path must work in js builds too.
func levenshtein(a, b string) int {
	ar := []rune(a)
	br := []rune(b)
	if len(ar) == 0 {
		return len(br)
	}
	if len(br) == 0 {
		return len(ar)
	}

	prev := make([]int, len(br)+1)
	curr := make([]int, len(br)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ar); i++ {
		curr[0] = i
		for j := 1; j <= len(br); j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			curr[j] = min3(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[len(br)]
}

func min3(a, b, c int) int {
	if b < a {
		a = b
	}
	if c < a {
		a = c
	}
	return a
}

// ParseBrowseSteps converts the raw []interface{} a model supplies in the
// browse_url `steps` argument into typed BrowseStep values, normalizing each
// step's action to its canonical verb.
//
// It is the single shared implementation behind the two thin wrappers in
// pkg/agent (tool_handlers_browse.go) and pkg/agent_tools
// (browser_adapter.go): the step language must parse identically whichever
// entry point the caller used, and a bad verb must be rejected here — before
// any browser is launched — rather than costing a full Chromium start plus
// navigation.
//
// Errors are plain fmt.Errorf values prefixed "browse_url steps[%d]: ";
// callers wrap them into agent errors (agenterrors.NewInvalidInputError) so
// the taxonomy stays in the agent layer and this package keeps no dependency
// on it.
func ParseBrowseSteps(rawSteps []interface{}) ([]BrowseStep, error) {
	steps := make([]BrowseStep, 0, len(rawSteps))
	for idx, rawStep := range rawSteps {
		stepMap, ok := rawStep.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("browse_url steps[%d]: must be an object", idx)
		}

		// A "text" key is a documented trap: the browse-debugging skill once
		// taught {"action":"wait_for_text","text":"..."}, but BrowseStep has
		// no text field, so encoding/json dropped it silently and the step
		// waited for nothing. Catch it here, where the fix is nameable.
		if _, hasText := stepMap["text"]; hasText {
			if _, hasExpect := stepMap["expect"]; !hasExpect {
				if _, hasValue := stepMap["value"]; !hasValue {
					return nil, fmt.Errorf(`browse_url steps[%d]: "text" is not a recognized field; use "expect" for wait_for_text/assert_text`, idx)
				}
			}
		}

		encoded, err := json.Marshal(stepMap)
		if err != nil {
			return nil, fmt.Errorf("browse_url steps[%d]: marshal failed: %w", idx, err)
		}
		var step BrowseStep
		if err := json.Unmarshal(encoded, &step); err != nil {
			return nil, fmt.Errorf("browse_url steps[%d]: parse failed: %w", idx, err)
		}

		action, err := NormalizeBrowseStepAction(step.Action, step.Selector)
		if err != nil {
			return nil, fmt.Errorf("browse_url steps[%d]: %w", idx, err)
		}
		step.Action = action
		steps = append(steps, step)
	}
	return steps, nil
}
