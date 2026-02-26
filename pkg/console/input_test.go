package console

import (
	"os"
	"testing"
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
