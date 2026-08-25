package console

import (
	"bytes"
	"strings"
	"testing"
)

// TestDrawLocked_RendersSteerRowsDuringProseStreaming pins the regression
// fix for the "steer echo disappears while the model streams" symptom.
//
// While proseStreaming is true, drawLocked must still render the pinned
// steer rows (the user's in-progress input) — it only suppresses the
// rule/content/hint chrome. The pre-fix code returned before drawing
// anything, so keystrokes accumulated invisibly and "caught up" only at
// segment boundaries.
func TestDrawLocked_RendersSteerRowsDuringProseStreaming(t *testing.T) {
	var buf bytes.Buffer
	f := &StatusFooter{
		w:            &buf,
		isTTY:        true,
		active:       true,
		steerCursor:  -1,
		fd:           -1,
		sizeOverride: &terminalSizeOverride{cols: 80, rows: 24},
	}

	f.mu.Lock()
	f.steerActive = true
	f.steerLine = "» fix the bug"
	f.mu.Unlock()

	f.SetProseStreaming(true)

	LockOutput()
	f.drawLocked()
	UnlockOutput()

	out := buf.String()
	if !strings.Contains(out, "fix the bug") {
		t.Fatalf("drawLocked under proseStreaming did not render the steer line; output=%q", out)
	}
	// Chrome suppression: no rule row or content line mid-stream.
	if strings.Contains(out, "──") {
		t.Fatalf("drawLocked under proseStreaming rendered the rule row (chrome must stay suppressed); output=%q", out)
	}
}

// TestSetSteerLineWrapped_DefersRegionChangeDuringStreaming verifies that
// a steer row-count change while prose is streaming pends the DECSTBM
// re-apply instead of mutating the scroll region mid-stream, and that
// SetProseStreaming(false) consumes the pending flag.
func TestSetSteerLineWrapped_DefersRegionChangeDuringStreaming(t *testing.T) {
	f := NewStatusFooter(&nonTTYWriter{}, &stubSource{model: "test"})
	f.isTTY = true
	f.active = true

	f.SetProseStreaming(true)

	// Row-count change: 0 rows → 1 row while streaming.
	f.SetSteerLineWrapped("» one row", 0, 9)

	f.mu.Lock()
	pended := f.pendingSteerRegion
	f.mu.Unlock()
	if !pended {
		t.Fatal("SetSteerLineWrapped during streaming should pend the scroll-region change, not apply it")
	}

	// Segment end consumes the flag.
	f.SetProseStreaming(false)
	f.mu.Lock()
	pended = f.pendingSteerRegion
	f.mu.Unlock()
	if pended {
		t.Fatal("SetProseStreaming(false) did not consume pendingSteerRegion")
	}
}

// TestSetSteerLineWrapped_AppliesRegionWhenNotStreaming verifies the
// non-streaming path is unchanged: a row-count change still applies the
// scroll region eagerly (no pending flag left behind).
func TestSetSteerLineWrapped_AppliesRegionWhenNotStreaming(t *testing.T) {
	f := NewStatusFooter(&nonTTYWriter{}, &stubSource{model: "test"})
	f.isTTY = true
	f.active = true

	f.SetSteerLineWrapped("» one row", 0, 9)

	f.mu.Lock()
	pended := f.pendingSteerRegion
	f.mu.Unlock()
	if pended {
		t.Fatal("SetSteerLineWrapped outside streaming must not pend a region change")
	}
}
