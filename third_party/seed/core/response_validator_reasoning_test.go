package core

import "testing"

// ---------------------------------------------------------------------------
// LooksLikeReasoningOnlyAfterToolResults
// ---------------------------------------------------------------------------

// TestLooksLikeReasoningOnlyAfterToolResults_CoreCases covers the primary
// detection surface: content with no visible text paired with non-empty reasoning
// is the exact case we want to surface as "reasoning-only" so the runLoop
// can inject the nudge.
func TestLooksLikeReasoningOnlyAfterToolResults_CoreCases(t *testing.T) {
	rv := newTestValidator(t)

	tests := []struct {
		name             string
		content          string
		reasoningContent string
		want             bool
	}{
		{
			name:             "empty content + non-empty reasoning",
			content:          "",
			reasoningContent: "The user asked me to verify the build. I should run make build-all next.",
			want:             true,
		},
		{
			name:             "whitespace content + non-empty reasoning",
			content:          "   \n\t  ",
			reasoningContent: "Let me think about whether to continue or to call the next tool here.",
			want:             true,
		},
		{
			name:             "one-word answer + non-empty reasoning",
			content:          "Done.",
			reasoningContent: "I have decided to stop the conversation here since the user's task is complete.",
			want:             false,
		},
		{
			name:             "punctuation only + non-empty reasoning",
			content:          " … ",
			reasoningContent: "I will summarize the findings and then end the response without taking further action.",
			want:             true,
		},
		{
			name:             "real answer + reasoning",
			content:          "The WASM build errors are gone.",
			reasoningContent: "Let me note this for the user.",
			want:             false,
		},
		{
			name:             "empty reasoning + empty content",
			content:          "",
			reasoningContent: "",
			want:             false, // handled by blank guard, not us
		},
		{
			name:             "empty reasoning + non-empty content",
			content:          "All done — the build passed and tests pass.",
			reasoningContent: "",
			want:             false,
		},
		{
			name:             "tiny reasoning (below min len) + empty content",
			content:          "",
			reasoningContent: "ok",
			want:             false,
		},
		{
			name:             "reasoning exact-min + empty content",
			content:          "",
			reasoningContent: "abcde", // exactly 5 chars
			want:             true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rv.LooksLikeReasoningOnlyAfterToolResults(tt.content, tt.reasoningContent)
			if got != tt.want {
				t.Errorf("LooksLikeReasoningOnlyAfterToolResults(%q, %q) = %v, want %v",
					tt.content, tt.reasoningContent, got, tt.want)
			}
		})
	}
}

// TestLooksLikeReasoningOnlyAfterToolResults_VisibleText covers the boundary:
// content without a letter or digit fires the guard; any word, however
// short, is an answer.
func TestLooksLikeReasoningOnlyAfterToolResults_VisibleText(t *testing.T) {
	rv := newTestValidator(t)

	// Reasoning must always be ≥5 runes (the min len) but the
	// substance of this test is the content.
	const reasoning = "I should consider whether to continue or call the next tool."

	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{"empty content", "", true},
		{"only punctuation", ".", true},
		{"only symbols", "--- …", true},
		{"one word", "DONE", false},
		{"one word with period", "Done.", false},
		{"a number", "42", false},
		{"non-latin word", "完成", false},
		{"a sentence", "Done now, the file is written.", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rv.LooksLikeReasoningOnlyAfterToolResults(tt.content, reasoning)
			if got != tt.want {
				t.Errorf("LooksLikeReasoningOnlyAfterToolResults(%q, %q) = %v, want %v",
					tt.content, reasoning, got, tt.want)
			}
		})
	}
}

// TestLooksLikeReasoningOnlyAfterToolResults_RealAnswersPreserved protects
// the regression case this guard must NOT cause: a real short summary that
// follows tool results, paired with reasoning, should not be re-nudged.
func TestLooksLikeReasoningOnlyAfterToolResults_RealAnswersPreserved(t *testing.T) {
	rv := newTestValidator(t)

	realAnswers := []struct {
		content          string
		reasoningContent string
	}{
		{
			content:          "Build is green.",
			reasoningContent: "Verify was the main thing the user wanted.",
		},
		{
			content:          "Yes, that compiles.",
			reasoningContent: "I'll just confirm and we're done.",
		},
		{
			content:          "Tests pass.",
			reasoningContent: "All checks done.",
		},
		{
			content:          "DONE",
			reasoningContent: "The file is written; the user asked me to reply DONE.",
		},
		{
			content:          "WASM build errors gone.",
			reasoningContent: "Now I will narrate the next step to the user.",
		},
	}

	for i, tt := range realAnswers {
		got := rv.LooksLikeReasoningOnlyAfterToolResults(tt.content, tt.reasoningContent)
		if got {
			t.Errorf("real-answer case %d (content=%q) must not fire guard", i, tt.content)
		}
	}
}

// TestLooksLikeReasoningOnlyAfterToolResults_UnicodeReasoning covers UTF-8
// rune counting for the reasoning-min-len check. UTF-8 bytes != runes; the
// guard must compare rune counts so multi-byte languages are not undercounted.
func TestLooksLikeReasoningOnlyAfterToolResults_UnicodeReasoning(t *testing.T) {
	rv := newTestValidator(t)

	// "我们应该继续调用下一个工具。" — 14 runes, encoded in UTF-8 as ~40 bytes.
	chinese := "我们应该继续调用下一个工具。"

	if !rv.LooksLikeReasoningOnlyAfterToolResults("", chinese) {
		t.Error("Chinese reasoning (>5 runes) with empty content should fire guard")
	}

	// 4-rune Chinese reasoning — below min len — should NOT fire.
	if rv.LooksLikeReasoningOnlyAfterToolResults("", "你好世界") {
		t.Error("4-rune Chinese reasoning should not fire guard (under min len)")
	}
}

// TestLooksLikeReasoningOnlyAfterToolResults_DebugLogNil guards against
// the validator panicking if no DebugLog is configured.
func TestLooksLikeReasoningOnlyAfterToolResults_DebugLogNil(t *testing.T) {
	rv := NewResponseValidator(ResponseValidatorOptions{})
	_ = rv.LooksLikeReasoningOnlyAfterToolResults("", "some reasoning content")
}

// TestLooksLikeReasoningOnlyAfterToolResults_DebugLogCalled ensures the
// debug log fires when the guard returns true (so operators can diagnose
// in the wild).
func TestLooksLikeReasoningOnlyAfterToolResults_DebugLogCalled(t *testing.T) {
	var logCalled bool
	rv := NewResponseValidator(ResponseValidatorOptions{
		DebugLog: func(format string, args ...interface{}) {
			logCalled = true
		},
	})
	_ = rv.LooksLikeReasoningOnlyAfterToolResults("", "I will continue with the next step now.")
	if !logCalled {
		t.Error("expected debugLog to be called on reasoning-only detection")
	}

	// Reset and confirm: guard returns false → no log call.
	logCalled = false
	_ = rv.LooksLikeReasoningOnlyAfterToolResults("Done.", "")
	if logCalled {
		t.Error("debugLog should NOT be called when guard returns false")
	}
}
