package console

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newTimelineForTest(t *testing.T) (*events.EventBus, *ToolTimeline, *bytes.Buffer) {
	t.Helper()
	bus := events.NewEventBus()
	var buf bytes.Buffer
	tl := NewToolTimeline(bus, &buf)
	t.Cleanup(func() { tl.Stop() })
	return bus, tl, &buf
}

// waitFlush arms Flush, publishes the event, then blocks until the event
// loop has fully processed it. This replaces time.Sleep-based waits and
// eliminates data races on bytes.Buffer.
func waitFlush(t *testing.T, tl *ToolTimeline, bus *events.EventBus, eventType string, data map[string]interface{}) {
	t.Helper()
	ch := tl.Flush()
	bus.Publish(eventType, data)
	<-ch
}

// ---------------------------------------------------------------------------
// 1. ToolStart → "→ displayName · Started" with GlyphAction prefix
// ---------------------------------------------------------------------------

func TestToolTimeline_ToolStartRenders(t *testing.T) {
	bus, tl, buf := newTimelineForTest(t)

	waitFlush(t, tl, bus, events.EventTypeToolStart, events.ToolStartEvent(
		"read_file", "tc-1", "/foo/bar.go", "read_file /foo/bar.go", "", false, "", 0,
	))

	out := buf.String()
	if !strings.Contains(out, "read_file /foo/bar.go") {
		t.Fatalf("expected display name in output, got: %q", out)
	}
	if !strings.Contains(out, "Started") {
		t.Fatalf("expected 'Started' in output, got: %q", out)
	}
	// GlyphAction rune is "→".
	if !strings.Contains(out, "→") {
		t.Fatalf("expected GlyphAction rune (→) in output, got: %q", out)
	}
}

// ---------------------------------------------------------------------------
// 2. ToolEnd (completed) → "✓ displayName · X.XXs" with GlyphSuccess prefix
// ---------------------------------------------------------------------------

func TestToolTimeline_ToolEndCompleted(t *testing.T) {
	bus, tl, buf := newTimelineForTest(t)

	waitFlush(t, tl, bus, events.EventTypeToolStart, events.ToolStartEvent(
		"read_file", "tc-1", "/foo/bar.go", "read_file /foo/bar.go", "", false, "", 0,
	))
	waitFlush(t, tl, bus, events.EventTypeToolEnd, events.ToolEndEvent(
		"tc-1", "read_file", "completed", "file content", "", 320*time.Millisecond,
	))

	out := buf.String()
	if !strings.Contains(out, "0.32s") {
		t.Fatalf("expected duration '0.32s' in output, got: %q", out)
	}
	// GlyphSuccess rune is "✓".
	if !strings.Contains(out, "✓") {
		t.Fatalf("expected GlyphSuccess rune (✓) in output, got: %q", out)
	}
}

// ---------------------------------------------------------------------------
// 3. ToolEnd (failed) → "✗ displayName · X.XXs: errorMessage" with GlyphError
// ---------------------------------------------------------------------------

func TestToolTimeline_ToolEndFailed(t *testing.T) {
	bus, tl, buf := newTimelineForTest(t)

	waitFlush(t, tl, bus, events.EventTypeToolStart, events.ToolStartEvent(
		"shell_command", "tc-2", "rm -rf /", `shell "rm -rf /"`, "", false, "", 1,
	))
	waitFlush(t, tl, bus, events.EventTypeToolEnd, events.ToolEndEvent(
		"tc-2", "shell_command", "failed", "", "Permission denied", 1200*time.Millisecond,
	))

	out := buf.String()
	if !strings.Contains(out, "1.20s") {
		t.Fatalf("expected duration '1.20s' in output, got: %q", out)
	}
	if !strings.Contains(out, "Permission denied") {
		t.Fatalf("expected error message in output, got: %q", out)
	}
	// GlyphError rune is "✗".
	if !strings.Contains(out, "✗") {
		t.Fatalf("expected GlyphError rune (✗) in output, got: %q", out)
	}
}

// ---------------------------------------------------------------------------
// 4. ToolEnd removes the tool from active map — second ToolEnd falls back
//    to tool_name (proving the entry was deleted).
// ---------------------------------------------------------------------------

func TestToolTimeline_ToolEndRemovesFromActiveMap(t *testing.T) {
	bus, tl, buf := newTimelineForTest(t)

	// Use a display_name that differs from tool_name so we can tell them apart.
	waitFlush(t, tl, bus, events.EventTypeToolStart, events.ToolStartEvent(
		"shell", "tc-1", "ls", "shell_display", "", false, "", 0,
	))

	// First ToolEnd — should use display_name from active map.
	waitFlush(t, tl, bus, events.EventTypeToolEnd, events.ToolEndEvent(
		"tc-1", "shell", "completed", "", "", 100*time.Millisecond,
	))

	firstOut := buf.String()
	if !strings.Contains(firstOut, "shell_display") {
		t.Fatalf("first end should use display_name; got: %q", firstOut)
	}

	// Second ToolEnd for the same ID — active map entry was deleted, so it
	// must fall back to tool_name ("shell").
	buf.Reset()
	waitFlush(t, tl, bus, events.EventTypeToolEnd, events.ToolEndEvent(
		"tc-1", "shell", "completed", "", "", 100*time.Millisecond,
	))

	secondOut := buf.String()
	if !strings.Contains(secondOut, "shell") {
		t.Fatalf("second end should fall back to tool_name 'shell'; got: %q", secondOut)
	}
	// The second end should NOT contain the original display_name, proving
	// the active-map entry was removed.
	if strings.Contains(secondOut, "shell_display") {
		t.Fatalf("second end should NOT contain display_name; active map entry was not removed: %q", secondOut)
	}
}

// ---------------------------------------------------------------------------
// 5. Multiple tools in parallel — start two, end in reverse order.
// ---------------------------------------------------------------------------

func TestToolTimeline_ParallelTools(t *testing.T) {
	bus, tl, buf := newTimelineForTest(t)

	// Start two tools with different IDs.
	waitFlush(t, tl, bus, events.EventTypeToolStart, events.ToolStartEvent(
		"read_file", "tc-A", "/a.go", "read /a.go", "", false, "", 0,
	))
	waitFlush(t, tl, bus, events.EventTypeToolStart, events.ToolStartEvent(
		"write_file", "tc-B", "/b.go", "write /b.go", "", false, "", 1,
	))

	// End them in reverse order: B first, then A.
	waitFlush(t, tl, bus, events.EventTypeToolEnd, events.ToolEndEvent(
		"tc-B", "write_file", "completed", "", "", 200*time.Millisecond,
	))
	waitFlush(t, tl, bus, events.EventTypeToolEnd, events.ToolEndEvent(
		"tc-A", "read_file", "completed", "", "", 150*time.Millisecond,
	))

	out := buf.String()

	// Both end lines must be present.
	if !strings.Contains(out, "write /b.go") {
		t.Errorf("expected 'write /b.go' end line in output, got: %q", out)
	}
	if !strings.Contains(out, "0.20s") {
		t.Errorf("expected '0.20s' for tc-B in output, got: %q", out)
	}
	if !strings.Contains(out, "read /a.go") {
		t.Errorf("expected 'read /a.go' end line in output, got: %q", out)
	}
	if !strings.Contains(out, "0.15s") {
		t.Errorf("expected '0.15s' for tc-A in output, got: %q", out)
	}
}

// ---------------------------------------------------------------------------
// 6. Fallback to tool_name when display_name is missing.
// ---------------------------------------------------------------------------

func TestToolTimeline_FallbackToToolName(t *testing.T) {
	bus, tl, buf := newTimelineForTest(t)

	// Pass empty display_name — the implementation should fall back to tool_name.
	waitFlush(t, tl, bus, events.EventTypeToolStart, events.ToolStartEvent(
		"shell", "tc-1", "ls -la", "", "", false, "", 0,
	))

	out := buf.String()
	// The rendered line should use "shell" (tool_name) as the display name.
	if !strings.Contains(out, "shell") {
		t.Fatalf("expected tool_name 'shell' as fallback in output, got: %q", out)
	}
	if !strings.Contains(out, "Started") {
		t.Fatalf("expected 'Started' in output, got: %q", out)
	}
}

// ---------------------------------------------------------------------------
// 7. Error message truncation — 200-char error truncated to 77 runes + "…"
// ---------------------------------------------------------------------------

func TestToolTimeline_ErrorTruncation(t *testing.T) {
	bus, tl, buf := newTimelineForTest(t)

	longErr := strings.Repeat("a", 200)

	waitFlush(t, tl, bus, events.EventTypeToolStart, events.ToolStartEvent(
		"shell", "tc-1", "cmd", "shell_cmd", "", false, "", 0,
	))
	waitFlush(t, tl, bus, events.EventTypeToolEnd, events.ToolEndEvent(
		"tc-1", "shell", "failed", "", longErr, 50*time.Millisecond,
	))

	out := buf.String()

	// The output should contain the ellipsis character.
	if !strings.Contains(out, "…") {
		t.Fatalf("expected truncated error with '…' in output, got: %q", out)
	}

	// Find the error portion: everything after "0.05s: ".
	sep := "0.05s: "
	idx := strings.Index(out, sep)
	if idx < 0 {
		t.Fatalf("could not find duration separator %q in output: %q", sep, out)
	}
	// Take the line after the separator (strip trailing newline).
	errorLine := strings.TrimSuffix(out[idx+len(sep):], "\n")

	// The truncated error should be 77 runes + "…" = 78 runes.
	// In bytes: 77 ASCII bytes + 3 bytes for "…" = 80 bytes.
	if len(errorLine) > 80 {
		t.Fatalf("truncated error too long: %d bytes (max 80), got: %q", len(errorLine), errorLine)
	}

	// Verify the exact truncation: first 77 runes of original + "…".
	expectedTruncated := longErr[:77] + "…"
	if errorLine != expectedTruncated {
		t.Fatalf("truncated error mismatch:\n  got:  %q\n  want: %q", errorLine, expectedTruncated)
	}
}

// ---------------------------------------------------------------------------
// 8. Stop() unsubscribes — no further lines after Stop.
// ---------------------------------------------------------------------------

func TestToolTimeline_StopUnsubscribes(t *testing.T) {
	bus := events.NewEventBus()
	var buf bytes.Buffer
	tl := NewToolTimeline(bus, &buf)

	// Publish one event and wait for it to be processed.
	waitFlush(t, tl, bus, events.EventTypeToolStart, events.ToolStartEvent(
		"read_file", "tc-1", "/a.go", "read /a.go", "", false, "", 0,
	))

	firstLen := buf.Len()
	if firstLen == 0 {
		t.Fatal("expected at least one line before Stop")
	}

	// Stop the timeline — should unsubscribe.
	tl.Stop()

	// Publish another event — it should NOT be processed.
	bus.Publish(events.EventTypeToolStart, events.ToolStartEvent(
		"write_file", "tc-2", "/b.go", "write /b.go", "", false, "", 1,
	))
	// No Flush() here — timeline is stopped, so the event won't be processed.
	// Give a tiny grace period just in case (but no race since we're not
	// reading the buffer concurrently with the timeline goroutine).
	tl.Stop() // idempotent; ensures any lingering goroutine is done.

	// Buffer length should be unchanged.
	if buf.Len() != firstLen {
		t.Fatalf("expected no output after Stop; buffer grew from %d to %d: %q",
			firstLen, buf.Len(), buf.String())
	}
}

// ---------------------------------------------------------------------------
// 9. Stop() is idempotent — calling twice must not panic.
// ---------------------------------------------------------------------------

func TestToolTimeline_StopIdempotent(t *testing.T) {
	bus, tl, _ := newTimelineForTest(t)
	_ = bus

	tl.Stop()
	tl.Stop() // Must not panic or block.
}
