package console

import (
	"bytes"
	"strings"
	"testing"
)

// The wheel belongs to the terminal. Enabling VT200 (?1000h) makes the
// application receive wheel events instead of the terminal scrolling its
// own scrollback, which left the user unable to scroll up to read a long
// security prompt or a wall of tool output. Only click reporting (SGR
// coordinates) is needed for the context menu, so the wheel stays free.
func TestMouseTrackingModes_DoNotCaptureTheWheel(t *testing.T) {
	if strings.Contains(MouseTrackingSGR, "1000h") {
		t.Errorf("SGR enable must not also enable VT200 motion tracking: %q", MouseTrackingSGR)
	}
	if !strings.Contains(MouseTrackingVT200, "1000h") {
		t.Errorf("the VT200 constant should be the motion-tracking mode: %q", MouseTrackingVT200)
	}
}

// The input reader's setup must emit only the click-reporting enable, so a
// running REPL keeps the terminal's scrollback.
func TestInputReaderMouseEnable_IsClickOnly(t *testing.T) {
	// setupInputTerm writes to stdout, so assert on the constant the reader
	// uses rather than capturing a global write.
	if strings.Contains(MouseTrackingSGR, "1000h") {
		t.Fatal("the reader's mouse enable must not include VT200")
	}
}

// The picker is fully keyboard-navigable; its hint must name the keys so
// the path is discoverable now that the wheel does not move the selection.
func TestSelectListHint_NamesKeyboardNavigation(t *testing.T) {
	s := NewSelectList(SelectListOptions{
		Items: []SelectItem{{Label: "a", Value: "a"}, {Label: "b", Value: "b"}},
	})
	lines := s.frameLines(80)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "↑↓") || !strings.Contains(joined, "enter confirm") {
		t.Errorf("picker hint must name the navigation keys, got:\n%s", joined)
	}
	if !strings.Contains(joined, "esc cancel") {
		t.Errorf("picker hint must name the cancel key, got:\n%s", joined)
	}
}

// A picker must still render its rows and confirm by keyboard with no mouse
// input at all — the wheel decision must not have removed the keyboard path.
func TestSelectList_KeyboardNavigationUnaffectedByWheelDecision(t *testing.T) {
	s := NewSelectList(SelectListOptions{
		Items: []SelectItem{
			{Label: "alpha", Value: "A"},
			{Label: "bravo", Value: "B"},
			{Label: "charlie", Value: "C"},
		},
	})
	s.isTTY = true
	var buf bytes.Buffer
	s.testOut = &buf

	s.mu.Lock()
	s.moveCursor(1)
	s.mu.Unlock()

	s.mu.Lock()
	got := s.filtered[s.cursor]
	s.mu.Unlock()
	if s.opts.Items[got].Value != "B" {
		t.Fatalf("down-arrow moved to %q, want B", s.opts.Items[got].Value)
	}
}
