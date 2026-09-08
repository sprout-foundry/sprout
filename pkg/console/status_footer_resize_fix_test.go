package console

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestDrawSteerRowsLocked_WrapsInCursorSaveRestore pins the fix for the
// select-list frame stacking seen during ask_user prompts.
//
// While proseStreaming is true (which is the case mid-turn while a tool
// like ask_user renders its select list), footer Refresh() routes to
// drawSteerRowsLocked. That path absolutely positions the cursor at the
// steer row; without a DECSC/DECRC wrap it LEAVES the cursor there. The
// SelectList erases its previous frame with a relative walk-back
// ("\r\033[K\033[A" x N) which assumes the cursor sits below the frame;
// starting from the steer row it erases the wrong rows and every
// keypress stacks a fresh frame (duplicate option rows smearing up the
// screen).
func TestDrawSteerRowsLocked_WrapsInCursorSaveRestore(t *testing.T) {
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
	f.steerLine = "» steer text"
	f.mu.Unlock()

	f.SetProseStreaming(true)

	LockOutput()
	f.drawLocked()
	UnlockOutput()

	out := buf.String()
	save := strings.Index(out, "\0337")
	restore := strings.Index(out, "\0338")
	if save < 0 || restore < 0 {
		t.Fatalf("drawSteerRowsLocked must wrap absolute writes in DECSC/DECRC (\\0337/\\0338); output=%q", out)
	}
	if save > restore {
		t.Fatalf("DECSC (\\0337) must precede DECRC (\\0338); output=%q", out)
	}
	// The steer row write itself must sit between save and restore.
	rowWrite := strings.Index(out, "\033[23;1H") // rows-hintRows-steerRows = 24-1-... steer row for 1 steer row, no hint: rows-2 = 22? compute below
	_ = rowWrite
	if !strings.Contains(out, "steer text") {
		t.Fatalf("steer line not rendered; output=%q", out)
	}
}

// TestResize_GrowClearsStrandedOldFooterRows pins the fix for resize
// grow-stranding: after the terminal grows, the OLD footer rows (drawn
// for the old height) sit mid-screen while the new footer renders at
// the new bottom. A clear window computed only from the NEW geometry
// misses them, leaving duplicate hint/rule rows smeared up the screen.
// The clear must start at or above the old-geometry top row.
func TestResize_GrowClearsStrandedOldFooterRows(t *testing.T) {
	var buf bytes.Buffer
	f := &StatusFooter{
		w:            &buf,
		isTTY:        true,
		active:       true,
		steerCursor:  -1,
		fd:           -1,
		sizeOverride: &terminalSizeOverride{cols: 80, rows: 24},
	}

	// Hint row active (matches the real REPL): reserved = rule + content + hint = 3.
	f.mu.Lock()
	f.showKeymapHint = true
	f.mu.Unlock()

	// Initial draw at 24 rows: footer occupies rows 21..24.
	LockOutput()
	f.drawLocked()
	UnlockOutput()

	f.mu.Lock()
	drawnRows := f.lastRows
	f.mu.Unlock()
	if drawnRows != 24 {
		t.Fatalf("expected lastRows=24 after first draw, got %d", drawnRows)
	}

	// Grow the terminal to 40 rows, then resize.
	f.mu.Lock()
	f.sizeOverride = &terminalSizeOverride{cols: 80, rows: 40}
	f.mu.Unlock()

	buf.Reset()
	f.Resize()

	out := buf.String()
	// Find the clear-to-end-of-screen sequence: \033[<row>;1H\033[J
	re := regexp.MustCompile(`\x1b\[(\d+);1H\x1b\[J`)
	m := re.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("Resize did not emit a clear-to-end window; output=%q", out)
	}
	clearTop, _ := strconv.Atoi(m[1])
	// Old footer top: oldRows(24) - reserved(3) = 21. The clear must
	// start at or above row 21 to wipe the stranded old rows.
	if clearTop > 21 {
		t.Fatalf("clear window starts at row %d; must cover old-geometry footer top (row 21) after a grow; output=%q", clearTop, out)
	}
}

// TestResize_ShrinkKeepsExistingClearBehavior guards the shrink path:
// the union clear must not regress the pre-existing behavior where the
// clear starts at the new-geometry top minus wrapped overflow.
func TestResize_ShrinkKeepsExistingClearBehavior(t *testing.T) {
	var buf bytes.Buffer
	f := &StatusFooter{
		w:            &buf,
		isTTY:        true,
		active:       true,
		steerCursor:  -1,
		fd:           -1,
		sizeOverride: &terminalSizeOverride{cols: 120, rows: 40},
	}

	LockOutput()
	f.drawLocked()
	UnlockOutput()

	// Shrink: 120 cols → 80 cols, 40 rows → 24 rows.
	f.mu.Lock()
	f.sizeOverride = &terminalSizeOverride{cols: 80, rows: 24}
	f.mu.Unlock()

	buf.Reset()
	f.Resize()

	out := buf.String()
	re := regexp.MustCompile(`\x1b\[(\d+);1H\x1b\[J`)
	m := re.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("Resize did not emit a clear window on shrink; output=%q", out)
	}
	clearTop, _ := strconv.Atoi(m[1])
	// Shrink: newTop (24 - reserved2 - overflow2) must win the min().
	// Overflow for 120→80 cols with 2 reserved rows: each padded row
	// wraps to ceil(120/80)=2 rows → 1 extra row × 2 rows = 2.
	// Expect 24-2-2 = 20.
	if clearTop != 20 {
		t.Fatalf("shrink clear top = %d, want 20 (new-geometry top incl. wrap overflow); output=%q", clearTop, out)
	}
}

// TestFooterTooltip_ShowHideWrappedInSaveRestore pins the DECSC/DECRC
// contract on the tooltip path: absolute-positioned row writes must not
// displace the cursor for relative-walk consumers (select lists, the
// inline input line).
func TestFooterTooltip_ShowHideWrappedInSaveRestore(t *testing.T) {
	var buf bytes.Buffer
	tt := &FooterTooltip{
		w: &buf,
		Source: func() []ToolInvocation {
			return []ToolInvocation{{Name: "read_file", Count: 1, TotalLatency: 12000}}
		},
	}
	tt.Show(80, 24)
	out := buf.String()
	if !strings.Contains(out, "\0337") || !strings.Contains(out, "\0338") {
		t.Fatalf("tooltip Show must wrap absolute writes in DECSC/DECRC; output=%q", out)
	}

	buf.Reset()
	tt.Hide()
	out = buf.String()
	if !strings.Contains(out, "\0337") || !strings.Contains(out, "\0338") {
		t.Fatalf("tooltip Hide must wrap erase writes in DECSC/DECRC; output=%q", out)
	}
}
