package console

import (
	"bytes"
	"strings"
	"testing"
)

// TestDrawFullLockedReassertsScrollRegion pins the self-heal contract:
// every full footer draw re-emits the DECSTBM sequence, so terminal
// state damaged by a child process (editors, pagers, anything writing
// \033[r) is repaired at the next Refresh instead of letting subsequent
// output scroll over the pinned footer rows.
func TestDrawFullLockedReassertsScrollRegion(t *testing.T) {
	var buf bytes.Buffer
	f := &StatusFooter{
		w:            &buf,
		isTTY:        true,
		source:       &stubSource{model: "m"},
		sizeOverride: &terminalSizeOverride{cols: 80, rows: 24},
		steerCursor:  -1,
	}
	f.mu.Lock()
	f.active = true
	f.mu.Unlock()

	LockOutput()
	f.drawFullLocked()
	UnlockOutput()

	first := buf.String()
	if !strings.Contains(first, "\033[1;22r") {
		t.Fatalf("first draw did not set scroll region; got %q", first)
	}

	// Simulate a child process dropping the region: full-screen reset.
	buf.Reset()

	LockOutput()
	f.drawFullLocked()
	UnlockOutput()

	if !strings.Contains(buf.String(), "\033[1;22r") {
		t.Fatal("second draw did not re-assert the scroll region — a dropped region would not self-heal")
	}
}
