package console

import "testing"

// feedBytes runs every byte through the parser and collects the
// printable characters that would land in the input buffer.
func feedBytes(t *testing.T, input string) string {
	t.Helper()
	parser := NewEscapeParser()
	var out string
	for i := 0; i < len(input); i++ {
		if ev := parser.Parse(input[i]); ev != nil && ev.Type == EventChar {
			out += ev.Data
		}
	}
	return out
}

// TestEscapeParserSwallowsDA2Reply is the regression pin for the
// ">41;320;0c" ghosting in Termux input lines: the footer's DA2 probe
// reply can land in a concurrent reader's stdin stream, and the parser
// used to shred it into typed text via the unknown-sequence fallback.
// Device reports must be swallowed whole, emitting nothing.
func TestEscapeParserSwallowsDA2Reply(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"termux DA2", "\033[>41;320;0c"},
		{"xterm DA2", "\033[>41;379;0c"},
		{"DA1 reply", "\033[?1;2c"},
		{"DA3 reply", "\033[=67;84;0c"},
		{"DECRPM bracketed paste", "\033[?2004;1$y"},
		{"kitty keyboard flags", "\033[?1u"},
		{"truncated DA2 then keys", "\033[>41;320"},
	}
	for _, tc := range cases {
		if got := feedBytes(t, tc.input); got != "" {
			t.Errorf("%s: parser echoed %q into the input buffer; want swallowed silently", tc.name, got)
		}
	}
}

// TestEscapeParserRecoversAfterSwallowedReport pins that the swallow
// state terminates cleanly: after a full device report the parser is
// back in ground state and subsequent keystrokes render normally.
func TestEscapeParserRecoversAfterSwallowedReport(t *testing.T) {
	if got := feedBytes(t, "\033[>41;320;0chello"); got != "hello" {
		t.Fatalf("after DA2 reply parser produced %q; want \"hello\" (reply swallowed, keys intact)", got)
	}
	if got := feedBytes(t, "\033[?2004;1$yworld"); got != "world" {
		t.Fatalf("after DECRPM parser produced %q; want \"world\"", got)
	}
}

// TestEscapeParserControlByteAbortsSwallow pins the malformed-input
// guard: a control byte inside a device report terminates the swallow
// (and is dropped) instead of letting the buffer grow unbounded. The
// parser returns to ground state — the following printable key passes
// through normally.
func TestEscapeParserControlByteAbortsSwallow(t *testing.T) {
	if got := feedBytes(t, "\033[>41;320;\x00x"); got != "x" {
		t.Fatalf("control byte mid-report: parser produced %q; want \"x\" (report aborted, key intact)", got)
	}
}

// TestEscapeParserTruncatedReportThenArrow pins recovery mid-sequence:
// a truncated report followed by a real keystroke must deliver the
// keystroke, not leak "[A" as text. This is the exact shape of the
// keyboard-dismiss race: a reply cut off by a competing reader, then
// the user presses an arrow key.
func TestEscapeParserTruncatedReportThenArrow(t *testing.T) {
	parser := NewEscapeParser()
	for _, b := range []byte("\033[>41;320") {
		parser.Parse(b)
	}
	if ev := parser.Parse(27); ev != nil {
		t.Fatalf("expected nil for ESC re-dispatch, got %#v", ev)
	}
	if ev := parser.Parse('['); ev != nil {
		t.Fatalf("expected nil for CSI introducer, got %#v", ev)
	}
	if ev := parser.Parse('A'); ev == nil || ev.Type != EventUp {
		t.Fatalf("expected EventUp after truncated report + ESC [ A, got %#v", ev)
	}
}
