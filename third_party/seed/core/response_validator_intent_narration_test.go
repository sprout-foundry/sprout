package core

import (
	"testing"
)

// TestLooksLikeIntentNarration covers the dropped-tool-call-delta stall
// class: the model narrates an action it was about to take but the
// structured tool_calls field never arrives. The two motivating cases are
// real transcripts:
//
//   - mid-text intent after a factual preamble, with verbatim repetition
//     ("Now let me commit… Let me commit the fix. … Let me commit the fix.")
//   - short narration after a user continuation prompt
//     ("Let me continue — commit the staged fix using the commit tool.")
func TestLooksLikeIntentNarration(t *testing.T) {
	rv := NewResponseValidator(ResponseValidatorOptions{})

	cases := []struct {
		name    string
		content string
		want    bool
	}{
		// --- Should be caught: intent narration, no substance ---
		{"mid-text intent with repetition",
			"No upstream changes — `HEAD..FETCH_HEAD` is empty. Now let me commit using the commit tool. " +
				"I've already run all the CI gates (`vet`, `fmt-check`, `lint`, `build-all`), so the build is verified.\n\n" +
				"Let me commit the fix.No upstream changes. I've already verified all CI gates. Let me commit the fix.\n\n" +
				"Let me commit with the `commit` tool.", true},
		{"short leading intent", "Let me continue — commit the staged fix using the commit tool.", true},
		{"let me start", "The diff looks good. Let me start the build now.", true},
		{"i'll mid-text", "Everything is staged. I'll run the push next.", true},
		{"i need to mid-text", "The working tree is clean. I need to tag the release now.", true},
		{"repeated sentence long response", strings_RepeatForTest("Now I will run the test suite. ", 12), true},

		// --- Should pass: rhetorical openers ---
		{"let me know", "All tests pass locally. Let me know if you want me to fix the warnings too.", false},
		{"let me be clear", "Let me be clear: the fix is already committed and pushed.", false},
		{"let me explain", "Let me explain what happened. The bug was a stale cache entry.", false},
		{"i will keep in mind with substance", "handler.go:42 was the culprit. I will keep the regression risk in mind.", false},

		// --- Should pass: substance markers exempt intent ---
		{"intent with file ref", "Found it. Let me summarize: parser.go:120 drops the last delta.", false},
		{"intent with code block", "Here is the plan recap:\n```go\nfmt.Println(\"done\")\n```\nLet me know.", false},
		{"intent with list", "Status:\n- committed\n- pushed\nLet me know about tagging.", false},
		{"intent with test language", "All 30 tests passed. I'll leave the flaky one alone.", false},

		// --- Should pass: plain completions ---
		{"done report", "The commit is on origin/main as a5e76c09c.", false},
		{"empty", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := rv.LooksLikeIntentNarration(tc.content)
			if got != tc.want {
				t.Errorf("LooksLikeIntentNarration(%q) = %v, want %v",
					truncateForTest(tc.content), got, tc.want)
			}
		})
	}
}

func strings_RepeatForTest(s string, n int) string {
	out := ""
	for i := 0; i < n; i++ {
		out += s
	}
	return out
}

func truncateForTest(s string) string {
	if len(s) > 60 {
		return s[:60] + "..."
	}
	return s
}

func TestHasRepeatedSentence(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    bool
	}{
		{"verbatim repeat", "Let me commit the fix. Some text. Let me commit the fix.", true},
		{"case and space insensitive", "Let me commit the fix.\n\n  let me commit the fix.", true},
		{"single mention", "Let me commit the fix. Then the release will be tagged.", false},
		{"short sentences excluded", "No. No. Yes.", false},
		{"two word sentences excluded", "Build passed. Build passed. Build passed.", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasRepeatedSentence(tc.content); got != tc.want {
				t.Errorf("hasRepeatedSentence(%q) = %v, want %v", tc.content, got, tc.want)
			}
		})
	}
}

func TestContainsMidTextIntent(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    bool
	}{
		{"start", "let me run the tests", true},
		{"mid after sentence", "all good. let me run the tests", true},
		{"rhetorical know", "let me know when you are free", false},
		{"rhetorical explain", "let me explain the whole design", false},
		{"word boundary", "millet me run", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := containsMidTextIntent(tc.content); got != tc.want {
				t.Errorf("containsMidTextIntent(%q) = %v, want %v", tc.content, got, tc.want)
			}
		})
	}
}

func TestIsBareContinuationPrompt(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    bool
	}{
		{"continue", "continue", true},
		{"Continue.", "Continue.", true},
		{"please continue", "please continue", true},
		{"go ahead", "go ahead!", true},
		{"keep going", "Keep going", true},
		{"substantive", "continue with the release tagging but skip the changelog", false},
		{"empty", "", false},
		{"question", "continue?", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isBareContinuationPrompt(tc.content); got != tc.want {
				t.Errorf("isBareContinuationPrompt(%q) = %v, want %v", tc.content, got, tc.want)
			}
		})
	}
}
