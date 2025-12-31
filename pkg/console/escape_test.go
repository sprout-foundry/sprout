package console

import (
	"testing"
)

// Test ESC key followed by letter - should emit EventEscape for the ESC
// and then EventChar for the letter
func TestEscapeParserWithEscapeThenChar(t *testing.T) {
	ep := NewEscapeParser()

	// Press ESC
	event := ep.Parse(27)
	if event != nil {
		t.Errorf("Expected nil after ESC (waiting for sequence), got %v", event)
	}

	// Followed by a regular character (not part of an escape sequence)
	event = ep.Parse('a')
	if event == nil {
		// After ESC followed by non-sequence char, we should get EventEscape for the ESC
		// and then the next Parse should handle the 'a'
		// But wait - 'a' goes through the switch, not escapeParser!
		t.Errorf("Expected character event 'a', got nil")
	}
}

// Test ESC key followed by arrow key - should recognize the arrow sequence
func TestEscapeParserWithArrowKeys(t *testing.T) {
	ep := NewEscapeParser()

	// Press ESC [ A (Up arrow)
	event := ep.Parse(27)
	if event != nil {
		t.Errorf("Expected nil after ESC, got %v", event)
	}

	event = ep.Parse('[')
	if event != nil {
		t.Errorf("Expected nil after ESC '[', got %v", event)
	}

	event = ep.Parse('A')
	if event == nil || event.Type != EventUp {
		t.Errorf("Expected Up event, got %v", event)
	}
}

// Test ESC key alone should eventually emit EventEscape
func TestEscapeParserStandalone(t *testing.T) {
	ep := NewEscapeParser()

	// Press ESC
	event := ep.Parse(27)
	if event != nil {
		t.Errorf("Expected nil after ESC, got %v", event)
	}

	// In the actual input reading, if nothing follows ESC immediately,
	// the next byte would go through the switch statement.
	// But if a byte that ISN'T handled by switch comes through...
}

// Test multiple ESC characters in sequence
func TestEscapeParserMultipleSequences(t *testing.T) {
	ep := NewEscapeParser()

	// First arrow key: ESC [ A (Up)
	event := ep.Parse(27)
	event = ep.Parse('[')
	event = ep.Parse('A')
	if event.Type != EventUp {
		t.Errorf("Expected Up event, got %v", event)
	}

	// Second arrow key: ESC [ B (Down)
	event = ep.Parse(27)
	event = ep.Parse('[')
	event = ep.Parse('B')
	if event.Type != EventDown {
		t.Errorf("Expected Down event, got %v", event)
	}
}

// Test ESC followed by invalid sequence and then valid sequence
func TestEscapeParserMixedSequences(t *testing.T) {
	ep := NewEscapeParser()

	// ESC followed by 'x' (not a valid sequence starter)
	event := ep.Parse(27)
	if event != nil {
		t.Errorf("Expected nil after ESC, got %v", event)
	}

	event = ep.Parse('x')
	// Should return EventEscape and reset
	if event == nil || event.Type != EventEscape {
		t.Errorf("Expected EventEscape, got %v", event)
	}

	// Now a valid arrow sequence
	event = ep.Parse(27)
	event = ep.Parse('[')
	event = ep.Parse('A')
	if event.Type != EventUp {
		t.Errorf("Expected Up event, got %v", event)
	}
}

// Test state persistence after reset
func TestEscapeParserReset(t *testing.T) {
	ep := NewEscapeParser()

	// Start a sequence but reset it
	ep.Parse(27)  // ESC
	ep.Parse('[') // [
	ep.Reset()

	// Should be back in initial state
	ep.Parse('a')
	// After reset, 'a' should be parsed correctly
}

// Test buffer overflow protection
func TestEscapeParserLongSequence(t *testing.T) {
	ep := NewEscapeParser()

	// ESC [ then lots of digits (should be handled gracefully)
	ep.Parse(27)
	ep.Parse('[')
	for i := 0; i < 100; i++ {
		ep.Parse('0')
	}
	// Should still be in state looking for more or have reset
	// The important thing is it doesn't crash
}

// Test Delete sequence (ESC [ 3 ~)
func TestEscapeParserDelete(t *testing.T) {
	ep := NewEscapeParser()

	// ESC [ 3 ~ (Delete)
	event := ep.Parse(27)
	if event != nil {
		t.Errorf("Expected nil after ESC, got %v", event)
	}

	event = ep.Parse('[')
	if event != nil {
		t.Errorf("Expected nil after '[', got %v", event)
	}

	event = ep.Parse('3')
	if event != nil {
		t.Errorf("Expected nil after '3', got %v", event)
	}

	event = ep.Parse('~')
	if event == nil || event.Type != EventDelete {
		t.Errorf("Expected Delete event, got %v", event)
	}
}
