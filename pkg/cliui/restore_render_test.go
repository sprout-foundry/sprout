package cliui

import (
	"fmt"
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/stretchr/testify/require"
)

func TestCollectTailLines_ChronologicalOrder(t *testing.T) {
	msgs := []api.Message{
		{Role: "user", Content: "first question"},
		{Role: "assistant", Content: "first answer"},
		{Role: "user", Content: "second question"},
	}

	lines, _ := collectTailLines(msgs, 200)
	joined := strings.Join(lines, "\n")

	first := strings.Index(joined, "first question")
	second := strings.Index(joined, "second question")
	if first < 0 || second < 0 {
		t.Fatalf("both messages should render, got:\n%s", joined)
	}
	if first > second {
		t.Fatalf("messages rendered out of order:\n%s", joined)
	}
}

func TestCollectTailLines_KeepsNewestWhenOverLimit(t *testing.T) {
	msgs := []api.Message{
		{Role: "user", Content: "oldest"},
		{Role: "user", Content: "middle"},
		{Role: "user", Content: "newest"},
	}

	lines, _ := collectTailLines(msgs, 4) // header+body per message
	joined := strings.Join(lines, "\n")

	if !strings.Contains(joined, "newest") {
		t.Fatalf("newest message dropped:\n%s", joined)
	}
	if strings.Contains(joined, "oldest") {
		t.Fatalf("oldest message should be trimmed at the limit:\n%s", joined)
	}
}

func TestCollectTailLines_SkipsToolMessages(t *testing.T) {
	msgs := []api.Message{
		{Role: "user", Content: "run the tests"},
		{Role: "tool", Content: `{"raw":"payload"}`},
		{Role: "assistant", Content: "all green"},
	}

	lines, skipped := collectTailLines(msgs, 200)
	joined := strings.Join(lines, "\n")

	if skipped != 1 {
		t.Fatalf("skipped %d tool messages, want 1", skipped)
	}
	if strings.Contains(joined, "payload") {
		t.Fatalf("tool payload leaked into the tail:\n%s", joined)
	}
	if !strings.Contains(joined, "all green") {
		t.Fatalf("assistant message missing:\n%s", joined)
	}
}

func TestCollectTailLines_StripsTimestampEnvelope(t *testing.T) {
	msgs := []api.Message{
		{Role: "user", Content: "<current-time>2026-10-31T07:00:00-05:00 (Local: x, Local)</current-time>\n\nreal question"},
	}

	lines, _ := collectTailLines(msgs, 200)
	joined := strings.Join(lines, "\n")

	if strings.Contains(joined, "current-time") {
		t.Fatalf("timestamp envelope leaked into the tail:\n%s", joined)
	}
	if !strings.Contains(joined, "real question") {
		t.Fatalf("message body missing:\n%s", joined)
	}
}

func TestCollectTailLines_EmptyConversation(t *testing.T) {
	lines, _ := collectTailLines(nil, 200)
	if len(lines) != 0 {
		t.Fatalf("expected no lines for an empty conversation, got %d", len(lines))
	}
}

func TestRenderSessionTail_EmptyIsSilent(t *testing.T) {
	var buf strings.Builder
	restore := console.SetTranscriptWriterForTest(&buf)
	defer restore()

	RenderSessionTail(nil)
	if buf.Len() != 0 {
		t.Fatalf("expected no output for an empty conversation, got %q", buf.String())
	}
}

func TestRenderSessionTail_AnchorsToRegionBottom(t *testing.T) {
	var buf strings.Builder
	restore := console.SetTranscriptWriterForTest(&buf)
	defer restore()

	footer := console.NewStatusFooter(nil, nil)
	console.SetGeometryForTest(footer, 80, 24)
	footer.SetActiveForTest()
	console.RegisterGlobalStatusFooter(footer)
	defer console.RegisterGlobalStatusFooter(nil)

	// 60 short messages render ~120 lines: taller than the 22-row region.
	var msgs []api.Message
	for i := range 60 {
		msgs = append(msgs, api.Message{Role: "user", Content: fmt.Sprintf("question %d", i)})
	}
	RenderSessionTail(msgs)

	out := buf.String()
	// The region's last row is 22 (24 rows − 2 reserved). Anchoring the
	// block there is what keeps its head on screen.
	if !strings.Contains(out, "\033[1;22r\033[22;1H") {
		t.Fatalf("tail was not anchored to the region's last row:\n%q", out)
	}
	if !strings.Contains(out, "resumed conversation") {
		t.Fatalf("header missing from the tail:\n%q", out)
	}
}

// Each message keeps its own line order: label first, then its body top to
// bottom. Reversing the collected lines (rather than the blocks) printed
// every multi-line message upside down with its label underneath.
func TestCollectTailLines_MultiLineMessagesReadTopDown(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	msgs := []api.Message{
		{Role: "user", Content: "question line one\nquestion line two"},
		{Role: "assistant", Content: "answer one\n\nanswer two"},
	}
	lines, _ := collectTailLines(msgs, 200)
	require.Equal(t, []string{
		"▌ you",
		"  question line one",
		"  question line two",
		"",
		"▌ assistant",
		"  answer one",
		"  ",
		"  answer two",
	}, lines)
}

// A cut through the oldest message shown keeps its label and marks the cut,
// and the result never exceeds the limit.
func TestCollectTailLines_CutBlockKeepsLabel(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	msgs := []api.Message{
		{Role: "user", Content: "a\nb\nc\nd\ne"},
		{Role: "assistant", Content: "done"},
	}
	lines, _ := collectTailLines(msgs, 7)
	require.Len(t, lines, 7)
	require.Equal(t, []string{"▌ you", "  …", "  d", "  e", "", "▌ assistant", "  done"}, lines)
}

func TestCollectTailLines_NewestBlockTallerThanLimit(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	msgs := []api.Message{{Role: "assistant", Content: "1\n2\n3\n4\n5\n6"}}
	lines, _ := collectTailLines(msgs, 4)
	require.Equal(t, []string{"▌ assistant", "  …", "  5", "  6"}, lines)
}
