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
	if !strings.Contains(out, "steer text") {
		t.Fatalf("steer line not rendered; output=%q", out)
	}
}

// TestResize_GrowClearsStrandedOldFooterRows pins the fix for resize
// grow-stranding: after the terminal grows, the OLD footer rows (drawn
// for the old height) sit mid-screen while the new footer renders at
// the new bottom. A clear window computed only from the NEW geometry
// misses them, leaving duplicate hint/rule rows smeared up the screen.
//
// The clear must start at EXACTLY the old-geometry footer top row:
// one row lower leaves the stranded rows; one row higher erases a live
// conversation row (the pre-fix off-by-one that left blank lines in
// Termux, where the soft keyboard resizes the terminal per message).
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

	// Initial draw at 24 rows: footer occupies rows 22..24 (hint, rule, content).
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
	// Old footer top: oldRows(24) - reserved(3) + 1 = 22. Row 21 is
	// conversation content — the window must start at exactly 22, not
	// above it (row 21 must survive).
	if clearTop != 22 {
		t.Fatalf("clear window starts at row %d; want exactly 22 (old footer top, row 21 is content); output=%q", clearTop, out)
	}
}

// TestResize_ShrinkKeepsExistingClearBehavior guards the shrink path:
// the clear must start at EXACTLY the top of the reflowed old-footer
// block (new-geometry top minus wrapped overflow, plus the +1 top-row
// correction) — no slack row above it, so the last conversation row
// survives every shrink.
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
	// Shrink: newTop (24 - reserved2 - overflow2 + 1) must win the min().
	// Overflow for 120→80 cols with 2 reserved rows: each padded row
	// wraps to ceil(120/80)=2 rows → 1 extra row × 2 rows = 2.
	// The reflowed footer block spans rows 21..24; row 20 (content)
	// must survive, so the window starts at exactly 21.
	if clearTop != 21 {
		t.Fatalf("shrink clear top = %d, want 21 (reflowed footer block top, no slack row); output=%q", clearTop, out)
	}
}

// TestResize_GrowWidthShrinkClearsFromOldFooterTop pins the grow+width-
// shrink case: the old footer block starts at oldRows-reserved+1 and its
// wrap overflow extends DOWNWARD into the fresh rows, so the old-geometry
// top (no overflow term) must win the union — clearing from the
// reflowed new-geometry top would miss the stranded block's upper rows.
func TestResize_GrowWidthShrinkClearsFromOldFooterTop(t *testing.T) {
	var buf bytes.Buffer
	f := &StatusFooter{
		w:            &buf,
		isTTY:        true,
		active:       true,
		steerCursor:  -1,
		fd:           -1,
		sizeOverride: &terminalSizeOverride{cols: 80, rows: 24},
	}

	LockOutput()
	f.drawLocked()
	UnlockOutput()

	// Grow rows AND shrink cols: 24r/80c → 40r/40c. reserved=2,
	// overflow = (80→40: 2 rows/line → 1 extra) × 2 rows = 2.
	f.mu.Lock()
	f.sizeOverride = &terminalSizeOverride{cols: 40, rows: 40}
	f.mu.Unlock()

	buf.Reset()
	f.Resize()

	out := buf.String()
	re := regexp.MustCompile(`\x1b\[(\d+);1H\x1b\[J`)
	m := re.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("Resize did not emit a clear-to-end window; output=%q", out)
	}
	clearTop, _ := strconv.Atoi(m[1])
	// oldTop = 24-2+1 = 23 beats newTop = 40-2-2+1 = 37. Row 22 is
	// conversation content and must survive.
	if clearTop != 23 {
		t.Fatalf("grow+width-shrink clear top = %d, want 23 (old footer top); output=%q", clearTop, out)
	}
}

// TestResize_WidthOnlyChangeKeepsExactClear pins the equal-row-count case
// (rotation-free width changes, e.g. split view): reflow settles the
// wrapped footer block at the bottom, so the new-geometry top (with
// overflow) wins the union — no slack row above it.
func TestResize_WidthOnlyChangeKeepsExactClear(t *testing.T) {
	var buf bytes.Buffer
	f := &StatusFooter{
		w:            &buf,
		isTTY:        true,
		active:       true,
		steerCursor:  -1,
		fd:           -1,
		sizeOverride: &terminalSizeOverride{cols: 120, rows: 24},
	}

	LockOutput()
	f.drawLocked()
	UnlockOutput()

	// Width-only change: 24r/120c → 24r/80c. reserved=2,
	// overflow = (120→80: 2 rows/line → 1 extra) × 2 rows = 2.
	f.mu.Lock()
	f.sizeOverride = &terminalSizeOverride{cols: 80, rows: 24}
	f.mu.Unlock()

	buf.Reset()
	f.Resize()

	out := buf.String()
	re := regexp.MustCompile(`\x1b\[(\d+);1H\x1b\[J`)
	m := re.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("Resize did not emit a clear window on width change; output=%q", out)
	}
	clearTop, _ := strconv.Atoi(m[1])
	// newTop = 24-2-2+1 = 21 beats oldTop = 24-2+1 = 23; row 20 (content)
	// must survive.
	if clearTop != 21 {
		t.Fatalf("width-only clear top = %d, want 21 (reflowed footer block top); output=%q", clearTop, out)
	}
}

// TestStop_ClearStartsAtPinnedBlockTop pins the teardown path against the
// same off-by-one: the erase must start at the topmost pinned row
// (rows-reserved+1, minus wrap overflow), never one row into content.
func TestStop_ClearStartsAtPinnedBlockTop(t *testing.T) {
	var buf bytes.Buffer
	f := &StatusFooter{
		w:            &buf,
		isTTY:        true,
		active:       true,
		steerCursor:  -1,
		fd:           -1,
		sizeOverride: &terminalSizeOverride{cols: 120, rows: 24},
	}

	LockOutput()
	f.drawLocked()
	UnlockOutput()

	// Shrink the width before stopping so the overflow term is exercised:
	// 120→80 cols, reserved=2 → overflow=2.
	f.mu.Lock()
	f.sizeOverride = &terminalSizeOverride{cols: 80, rows: 24}
	f.mu.Unlock()

	buf.Reset()
	f.Stop()

	out := buf.String()
	re := regexp.MustCompile(`\x1b\[(\d+);1H\x1b\[J`)
	m := re.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("Stop did not emit a clear window; output=%q", out)
	}
	clearTop, _ := strconv.Atoi(m[1])
	// topRow = 24-2+1-2 = 21: the reflowed footer block top, row 20
	// (content) survives.
	if clearTop != 21 {
		t.Fatalf("Stop clear top = %d, want 21 (pinned block top incl. overflow); output=%q", clearTop, out)
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
