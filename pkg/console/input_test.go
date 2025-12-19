package console

import (
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
