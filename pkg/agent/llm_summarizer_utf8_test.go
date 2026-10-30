package agent

import (
	"strings"
	"testing"
	"unicode/utf8"

	core "github.com/sprout-foundry/seed/core"
)

// The excerpt cap is a byte count, so a cut that lands inside a multi-byte
// character would hand the summarizer an invalid UTF-8 fragment.
func TestBuildSummarizerTranscript_ReasoningCutOnRuneBoundary(t *testing.T) {
	// Two-byte runes: an odd byte cap necessarily lands mid-character.
	reasoning := strings.Repeat("é", summarizerReasoningExcerptChars)

	got := buildSummarizerTranscript([]core.Message{
		{Role: "assistant", ReasoningContent: reasoning, Content: "reply"},
	}, 0)

	if !utf8.ValidString(got) {
		t.Fatalf("transcript is not valid UTF-8: %q", got)
	}
	if !strings.Contains(got, summarizerReasoningTag) {
		t.Errorf("expected the reasoning tag, got %q", got)
	}
	excerpt := strings.TrimPrefix(strings.SplitN(got, "\n", 2)[0], summarizerReasoningTag)
	if len(excerpt) > summarizerReasoningExcerptChars {
		t.Errorf("excerpt = %d bytes, want <= %d", len(excerpt), summarizerReasoningExcerptChars)
	}
}
