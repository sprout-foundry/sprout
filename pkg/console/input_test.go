package console

import (
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func TestNewInputReader(t *testing.T) {
	// Test basic InputReader creation
	ir := NewInputReader("test> ")

	if ir.prompt != "test> " {
		t.Errorf("Expected prompt 'test> ', got '%s'", ir.prompt)
	}

	// Test history functionality
	testHistory := []string{"command1", "command2", "command3"}
	ir.SetHistory(testHistory)

	retrievedHistory := ir.GetHistory()
	if len(retrievedHistory) != len(testHistory) {
		t.Errorf("Expected history length %d, got %d", len(testHistory), len(retrievedHistory))
	}

	for i, cmd := range testHistory {
		if retrievedHistory[i] != cmd {
			t.Errorf("Expected history[%d] = '%s', got '%s'", i, cmd, retrievedHistory[i])
		}
	}
}

func TestEscapeParserBasic(t *testing.T) {
	ep := NewEscapeParser()

	// Test parsing regular characters
	event := ep.Parse('a')
	if event == nil || event.Type != EventChar || event.Data != "a" {
		t.Errorf("Expected character event 'a', got %v", event)
	}

	// Test parsing control characters
	event = ep.Parse(127) // Backspace
	if event == nil || event.Type != EventBackspace {
		t.Errorf("Expected backspace event, got %v", event)
	}

	event = ep.Parse(13) // Enter
	if event == nil || event.Type != EventEnter {
		t.Errorf("Expected enter event, got %v", event)
	}

	// Reset parser for next test
	ep.Reset()
}

func TestInputReaderHistory(t *testing.T) {
	ir := NewInputReader("test> ")

	// Test adding to history
	ir.SetHistory([]string{"cmd1"})
	currentHistory := ir.GetHistory()
	if len(currentHistory) != 1 || currentHistory[0] != "cmd1" {
		t.Errorf("Expected history with 'cmd1', got %v", currentHistory)
	}

	// Test updating history
	ir.SetHistory([]string{"cmd1", "cmd2", "cmd3"})
	currentHistory = ir.GetHistory()
	if len(currentHistory) != 3 {
		t.Errorf("Expected history length 3, got %d", len(currentHistory))
	}
}

func TestVisualLineCount(t *testing.T) {
	tests := []struct {
		name          string
		terminalWidth int
		renderedWidth int
		want          int
	}{
		{name: "zero width terminal", terminalWidth: 0, renderedWidth: 10, want: 1},
		{name: "empty content", terminalWidth: 10, renderedWidth: 0, want: 1},
		{name: "single line partial", terminalWidth: 10, renderedWidth: 9, want: 1},
		{name: "exact boundary stays one line", terminalWidth: 10, renderedWidth: 10, want: 1},
		{name: "one over boundary", terminalWidth: 10, renderedWidth: 11, want: 2},
		{name: "two exact boundaries", terminalWidth: 10, renderedWidth: 20, want: 2},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := visualLineCount(tc.terminalWidth, tc.renderedWidth)
			if got != tc.want {
				t.Fatalf("visualLineCount(%d, %d) = %d, want %d",
					tc.terminalWidth, tc.renderedWidth, got, tc.want)
			}
		})
	}
}

func TestCursorLineIndex(t *testing.T) {
	tests := []struct {
		name          string
		terminalWidth int
		cursorPos     int
		want          int
	}{
		{name: "zero width terminal", terminalWidth: 0, cursorPos: 10, want: 0},
		{name: "zero cursor pos", terminalWidth: 10, cursorPos: 0, want: 0},
		{name: "partial first line", terminalWidth: 10, cursorPos: 9, want: 0},
		{name: "exact boundary still first line", terminalWidth: 10, cursorPos: 10, want: 0},
		{name: "one over boundary", terminalWidth: 10, cursorPos: 11, want: 1},
		{name: "second exact boundary", terminalWidth: 10, cursorPos: 20, want: 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := cursorLineIndex(tc.terminalWidth, tc.cursorPos)
			if got != tc.want {
				t.Fatalf("cursorLineIndex(%d, %d) = %d, want %d",
					tc.terminalWidth, tc.cursorPos, got, tc.want)
			}
		})
	}
}

func TestCursorColumnOffset(t *testing.T) {
	tests := []struct {
		name          string
		terminalWidth int
		cursorPos     int
		want          int
	}{
		{name: "zero width terminal", terminalWidth: 0, cursorPos: 10, want: 0},
		{name: "zero cursor pos", terminalWidth: 10, cursorPos: 0, want: 0},
		{name: "first char", terminalWidth: 10, cursorPos: 1, want: 1},
		{name: "middle of line", terminalWidth: 10, cursorPos: 7, want: 7},
		{name: "exact boundary stays at line end", terminalWidth: 10, cursorPos: 10, want: 9},
		{name: "wrapped next line offset", terminalWidth: 10, cursorPos: 11, want: 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := cursorColumnOffset(tc.terminalWidth, tc.cursorPos)
			if got != tc.want {
				t.Fatalf("cursorColumnOffset(%d, %d) = %d, want %d",
					tc.terminalWidth, tc.cursorPos, got, tc.want)
			}
		})
	}
}

func TestInsertCharFastPathTracking(t *testing.T) {
	ir := NewInputReader(">")
	ir.terminalWidth = 10
	ir.termFd = int(os.Stdout.Fd())

	// Simulate typing at end-of-line (fast path, no Refresh call).
	for _, ch := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i"} {
		ir.InsertChar(ch)
	}

	// Prompt width 1 + line width 9 => total width 10 (exact boundary).
	if ir.lastLineLength != 10 {
		t.Fatalf("expected lastLineLength=10, got %d", ir.lastLineLength)
	}

	// Cursor should still be on first visual line at exact boundary.
	if ir.currentPhysicalLine != 0 {
		t.Fatalf("expected currentPhysicalLine=0, got %d", ir.currentPhysicalLine)
	}
}

func TestInsertCharFastPathTrackingWithANSIPrompt(t *testing.T) {
	ir := NewInputReader("\033[32mledit>\033[0m ")
	ir.terminalWidth = 10
	ir.termFd = int(os.Stdout.Fd())

	// Visible prompt width is 7 ("ledit> "), so 3 chars reaches exact boundary.
	for _, ch := range []string{"a", "b", "c"} {
		ir.InsertChar(ch)
	}

	if ir.lastLineLength != 10 {
		t.Fatalf("expected lastLineLength=10, got %d", ir.lastLineLength)
	}
	if ir.currentPhysicalLine != 0 {
		t.Fatalf("expected currentPhysicalLine=0, got %d", ir.currentPhysicalLine)
	}
	if !ir.lastWrapPending {
		t.Fatalf("expected lastWrapPending=true at exact boundary")
	}
}

func TestRefreshCancelsPendingWrapBeforeRedraw(t *testing.T) {
	ir := NewInputReader(">")
	ir.terminalWidth = 10
	ir.termFd = int(os.Stdout.Fd())
	ir.line = "abcdefghi"
	ir.cursorPos = len(ir.line)
	ir.lastLineLength = 10
	ir.currentPhysicalLine = 0
	ir.lastWrapPending = true

	output := captureStdout(t, func() {
		ir.Backspace()
	})

	if !strings.HasPrefix(output, MoveCursorLeftSeq(1)+"\r") {
		t.Fatalf("expected redraw to normalize pending wrap first, got %q", output)
	}
	if ir.lastWrapPending {
		t.Fatalf("expected lastWrapPending=false after shrinking off boundary")
	}
}

func TestRefreshPlacesCursorAtLineEndForExactBoundary(t *testing.T) {
	ir := NewInputReader(">")
	ir.terminalWidth = 10
	ir.termFd = int(os.Stdout.Fd())
	ir.line = "abcdefghi"
	ir.cursorPos = len(ir.line)

	output := captureStdout(t, func() {
		ir.Refresh()
	})

	if !strings.Contains(output, "\r\033[9C") {
		t.Fatalf("expected cursor to move to last column at wrap boundary, got %q", output)
	}
}

func TestApplyTerminalWidthChangeResetsRedrawState(t *testing.T) {
	ir := NewInputReader("> ")
	ir.terminalWidth = 10
	ir.termFd = int(os.Stdout.Fd())
	ir.line = "abcdefghi"
	ir.cursorPos = len(ir.line)
	ir.lastLineLength = 10
	ir.currentPhysicalLine = 0
	ir.lastWrapPending = true

	output := captureStdout(t, func() {
		changed := ir.applyTerminalWidthChange(10, 6)
		if !changed {
			t.Fatal("expected width change to be handled")
		}
	})

	if !strings.HasPrefix(output, "\r"+ClearLineSeq()+"\n") {
		t.Fatalf("expected resize redraw to start on a fresh line, got %q", output)
	}
	if ir.terminalWidth != 6 {
		t.Fatalf("unexpected terminal width: %d", ir.terminalWidth)
	}
	if ir.lastWrapPending {
		t.Fatalf("expected wrap-pending state to be recalculated after resize")
	}
}

func TestApplyTerminalWidthChangeNoOpWhenWidthUnchanged(t *testing.T) {
	ir := NewInputReader("> ")
	ir.terminalWidth = 10

	output := captureStdout(t, func() {
		changed := ir.applyTerminalWidthChange(10, 10)
		if changed {
			t.Fatal("expected unchanged width to be ignored")
		}
	})

	if output != "" {
		t.Fatalf("expected no output for unchanged width, got %q", output)
	}
}

func TestFinalizePasteInsertsAtCursorLiterally(t *testing.T) {
	ir := NewInputReader("> ")
	ir.terminalWidth = 80
	ir.termFd = int(os.Stdout.Fd())
	ir.line = "hello world"
	ir.cursorPos = 6
	ir.inPasteMode = true
	ir.pasteActive = true
	ir.pasteBuffer.WriteString("foo\nbar\n")

	ok := ir.finalizePaste()
	if !ok {
		t.Fatal("finalizePaste() returned false")
	}

	if ir.line != "hello foo\nbarworld" {
		t.Fatalf("unexpected line after paste: %q", ir.line)
	}

	wantCursor := len("hello foo\nbar")
	if ir.cursorPos != wantCursor {
		t.Fatalf("unexpected cursorPos: got %d want %d", ir.cursorPos, wantCursor)
	}
}

func TestFinalizePasteDoesNotAutoFormatCode(t *testing.T) {
	ir := NewInputReader("> ")
	ir.terminalWidth = 80
	ir.termFd = int(os.Stdout.Fd())
	ir.inPasteMode = true
	ir.pasteActive = true
	ir.pasteBuffer.WriteString("func main() {\n\tprintln(\"hi\")\n}\n")

	ok := ir.finalizePaste()
	if !ok {
		t.Fatal("finalizePaste() returned false")
	}

	if ir.line != "func main() {\n\tprintln(\"hi\")\n}" {
		t.Fatalf("unexpected formatted paste result: %q", ir.line)
	}
}

func TestShouldStartHeuristicPaste(t *testing.T) {
	tests := []struct {
		name      string
		chunk     []byte
		delay     time.Duration
		wantStart bool
	}{
		{
			name:      "small burst should not trigger",
			chunk:     []byte("hello"),
			delay:     5 * time.Millisecond,
			wantStart: false,
		},
		{
			name:      "moderate printable burst with fast timing should trigger",
			chunk:     []byte("this is pasted"),
			delay:     5 * time.Millisecond,
			wantStart: true,
		},
		{
			name:      "moderate printable burst with slow timing should not trigger",
			chunk:     []byte("this is pasted"),
			delay:     50 * time.Millisecond,
			wantStart: false,
		},
		{
			name:      "burst containing backspace should not trigger",
			chunk:     []byte{'h', 'e', 'l', 'l', 'o', 127, 'w', 'o', 'r', 'l', 'd', '!'},
			delay:     5 * time.Millisecond,
			wantStart: false,
		},
		{
			name:      "large printable burst should trigger regardless of timing",
			chunk:     []byte("abcdefghijklmnopqrstuvwxyz"),
			delay:     80 * time.Millisecond,
			wantStart: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldStartHeuristicPaste(tc.chunk, tc.delay)
			if got != tc.wantStart {
				t.Fatalf("shouldStartHeuristicPaste(%q, %v) = %v, want %v", string(tc.chunk), tc.delay, got, tc.wantStart)
			}
		})
	}
}

func TestEscapeParserBracketedPasteMarkers(t *testing.T) {
	ep := NewEscapeParser()

	// ESC [ 200 ~ => paste start
	var event *InputEvent
	for _, b := range []byte{27, '[', '2', '0', '0', '~'} {
		event = ep.Parse(b)
	}
	if event == nil || event.Type != EventPasteStart {
		t.Fatalf("expected EventPasteStart, got %v", event)
	}

	// ESC [ 201 ~ => paste end
	for _, b := range []byte{27, '[', '2', '0', '1', '~'} {
		event = ep.Parse(b)
	}
	if event == nil || event.Type != EventPasteEnd {
		t.Fatalf("expected EventPasteEnd, got %v", event)
	}
}

func TestConsumeBracketedPasteByteCollectsMultiline(t *testing.T) {
	ir := NewInputReader("> ")
	ir.bracketedPaste = true

	input := []byte("line1\r\nline2\x1b[201~")
	ended := false
	for _, b := range input {
		if ir.consumeBracketedPasteByte(b) {
			ended = true
			break
		}
	}
	if !ended {
		t.Fatal("expected bracketed paste to end")
	}

	if got := ir.pasteBuffer.String(); got != "line1\nline2" {
		t.Fatalf("unexpected pasted content: %q", got)
	}
}

func TestConsumeBracketedPasteBytePreservesUTF8(t *testing.T) {
	ir := NewInputReader("> ")
	ir.bracketedPaste = true

	input := append([]byte("caf\xc3\xa9 日本語"), []byte("\x1b[201~")...)
	ended := false
	for _, b := range input {
		if ir.consumeBracketedPasteByte(b) {
			ended = true
			break
		}
	}
	if !ended {
		t.Fatal("expected bracketed paste to end")
	}

	if got := ir.pasteBuffer.String(); got != "café 日本語" {
		t.Fatalf("unexpected pasted UTF-8 content: %q", got)
	}
}

func TestRenderLineWithCollapsedPastes(t *testing.T) {
	ir := NewInputReader("> ")
	ir.line = "preline1\nline2post"
	ir.collapsedPastes = []pasteSpan{{start: 3, end: len("preline1\nline2")}}
	ir.cursorPos = len("preline1\nline2")

	display, cursor := ir.renderLineWithCollapsedPastes()
	if display != "pre[pasted 11 chars]post" {
		t.Fatalf("unexpected display: %q", display)
	}
	if cursor != len("pre[pasted 11 chars]") {
		t.Fatalf("unexpected display cursor: %d", cursor)
	}
}

func TestSetCursorInsideCollapsedPasteExpands(t *testing.T) {
	ir := NewInputReader("> ")
	ir.line = "abcPASTEdef"
	ir.collapsedPastes = []pasteSpan{{start: 3, end: 8}}
	ir.cursorPos = 0
	ir.SetCursor(5)

	if len(ir.collapsedPastes) != 0 {
		t.Fatalf("expected collapsed span to expand, got %d spans", len(ir.collapsedPastes))
	}
	if ir.cursorPos != 5 {
		t.Fatalf("unexpected cursor position: %d", ir.cursorPos)
	}
}

func TestBackspaceAtCollapsedPasteBoundaryDeletesWholePaste(t *testing.T) {
	ir := NewInputReader("> ")
	ir.line = "prePASTEsuf"
	ir.collapsedPastes = []pasteSpan{{start: 3, end: 8}}
	ir.cursorPos = 8

	ir.Backspace()

	if ir.line != "presuf" {
		t.Fatalf("unexpected line after backspace: %q", ir.line)
	}
	if ir.cursorPos != 3 {
		t.Fatalf("unexpected cursor after backspace: %d", ir.cursorPos)
	}
	if len(ir.collapsedPastes) != 0 {
		t.Fatalf("expected collapsed spans to be removed, got %d", len(ir.collapsedPastes))
	}
}

func TestDeleteAtCollapsedPasteBoundaryDeletesWholePaste(t *testing.T) {
	ir := NewInputReader("> ")
	ir.line = "prePASTEsuf"
	ir.collapsedPastes = []pasteSpan{{start: 3, end: 8}}
	ir.cursorPos = 3

	ir.Delete()

	if ir.line != "presuf" {
		t.Fatalf("unexpected line after delete: %q", ir.line)
	}
	if ir.cursorPos != 3 {
		t.Fatalf("unexpected cursor after delete: %d", ir.cursorPos)
	}
	if len(ir.collapsedPastes) != 0 {
		t.Fatalf("expected collapsed spans to be removed, got %d", len(ir.collapsedPastes))
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	defer func() {
		os.Stdout = oldStdout
	}()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	return string(out)
}

func TestNavigateHistoryClearsCollapsedPastes(t *testing.T) {
	ir := NewInputReader("> ")
	ir.SetHistory([]string{"cmd-one"})
	ir.line = "prePASTE"
	ir.collapsedPastes = []pasteSpan{{start: 3, end: 8}}

	ir.NavigateHistory(1)

	if ir.line != "cmd-one" {
		t.Fatalf("unexpected history line: %q", ir.line)
	}
	if len(ir.collapsedPastes) != 0 {
		t.Fatalf("expected collapsed spans to clear on history nav, got %d", len(ir.collapsedPastes))
	}
}
