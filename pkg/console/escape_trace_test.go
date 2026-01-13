package console

import (
	"fmt"
	"testing"
)

// Trace through what happens when ESC is followed by 'a'
func TestTraceEscThenLetter(t *testing.T) {
	ep := NewEscapeParser()

	// ESC (27)
	fmt.Printf("After ESC: state=%d\n", ep.state)
	event := ep.Parse(27)
	fmt.Printf("  event=%v, state=%d\n", event, ep.state)

	// 'a' (97)
	fmt.Printf("After 'a': state=%d\n", ep.state)
	event = ep.Parse(97)
	fmt.Printf("  event=%v, state=%d\n", event, ep.state)
}

// Trace through partial escape sequence then regular character
func TestTracePartialEscSeqThenLetter(t *testing.T) {
	ep := NewEscapeParser()

	// ESC [ (27, 91)
	event := ep.Parse(27)
	fmt.Printf("After ESC: event=%v, state=%d\n", event, ep.state)

	event = ep.Parse('[')
	fmt.Printf("After '[': event=%v, state=%d\n", event, ep.state)

	// Now 'x' which is not a terminator for state 2
	event = ep.Parse('x')
	fmt.Printf("After 'x': event=%v, state=%d\n", event, ep.state)

	// The 'x' would be lost because in state 2, it doesn't match any terminator
	// and falls through to return nil
}

// Test === current behavior of state 2 (got '[')
func TestEscapeParserState2Behavior(t *testing.T) {
	tests := []struct {
		name     string
		input    byte
		expected InputEventType
	}{
		{"Up arrow 'A'", 'A', EventUp},
		{"Down arrow 'B'", 'B', EventDown},
		{"Right 'C'", 'C', EventRight},
		{"Left 'D'", 'D', EventLeft},
		{"Home 'H'", 'H', EventHome},
		{"End 'F'", 'F', EventEnd},
		{"Delete start '3'", '3', -1}, // -1 means go to state 3, no event yet
		{"Home prefix '1'", '1', -1},  // go to state 5
		{"End prefix '4'", '4', -1},   // go to state 6
		{"Digit '0'", '0', -1},        // stay in state 2
		{"Letter 'x'", 'x', -1},       // stay in state 2 (will return nil!)
		{"Semicolon ';'", ';', -1},    // stay in state 2
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ep := NewEscapeParser()
			ep.Parse(27)  // ESC
			ep.Parse('[') // [
			// Now in state 2
			event := ep.Parse(tc.input)

			if tc.expected == -1 {
				// Expected no event (either moving to new state or staying in state 2)
				if event != nil {
					t.Logf("Expected nil but got event %v for input %d", event, tc.input)
				}
			} else {
				if event == nil || event.Type != tc.expected {
					t.Errorf("Expected event type %v, got %v for input %d", tc.expected, event, tc.input)
				}
			}
		})
	}
}

// FIXED: Test what happens when in middle of an escape sequence and input arrives
// Previously had infinite loop bug - for { loop with proper termination
func TestEscapeParserIncompleteSequences(t *testing.T) {
	cases := []struct {
		name        string
		bytes       []byte
		description string
	}{
		{
			"ESC then regular char",
			[]byte{27, 'x'},
			"Should get EventEscape for ESC, then 'x' should be a char event",
		},
		{
			"ESC [ then regular char",
			[]byte{27, '[', 'z'},
			"'z' is lost - bug!",
		},
		{
			"ESC [ 1 then regular char",
			[]byte{27, '[', '1', 'x'},
			"'x' is lost - bug!",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ep := NewEscapeParser()
			events := []*InputEvent{}

			// Process byte by byte - draining pending after each byte
			for _, b := range tc.bytes {
				// Process this byte
				event := ep.Parse(b)
				if event != nil {
					events = append(events, event)
				}
				// Drain pending character (if any) caused by Parse(b)
				// Continue draining until no more pending characters
				for {
					event = ep.Parse(0)
					if event != nil {
						events = append(events, event)
					} else {
						// No more pending characters, exit drain loop
						break
					}
				}
			}

			t.Logf("Input: %v, Events: %v", tc.bytes, events)
			for i, e := range events {
				t.Logf("  Event[%d]: Type=%d, Data=%q", i, e.Type, e.Data)
			}
			t.Logf("Description: %s", tc.description)
			t.Logf("Final state: %d", ep.state)

			// The bug is that in incomplete sequences followed by a character,
			// that character gets lost
			if len(events) == 0 {
				t.Errorf("BUG: No events generated for input %v - character(s) lost!", tc.bytes)
			}
		})
	}
}
