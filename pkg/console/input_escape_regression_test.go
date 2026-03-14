package console

import "testing"

func TestEscapeParserDoesNotFlushIncompleteArrowSequence(t *testing.T) {
	parser := NewEscapeParser()

	if event := parser.Parse(27); event != nil {
		t.Fatalf("expected nil for initial ESC, got %#v", event)
	}
	if parser.state != 1 {
		t.Fatalf("expected parser state 1 after ESC, got %d", parser.state)
	}

	// ReadLine drains pending characters after each byte. That drain must not
	// collapse an in-progress escape sequence back to EventEscape.
	if parser.hasPending {
		t.Fatalf("expected no pending character after ESC")
	}

	if event := parser.Parse('['); event != nil {
		t.Fatalf("expected nil for CSI introducer, got %#v", event)
	}
	if parser.state != 2 {
		t.Fatalf("expected parser state 2 after ESC [, got %d", parser.state)
	}

	event := parser.Parse('A')
	if event == nil || event.Type != EventUp {
		t.Fatalf("expected EventUp after ESC [ A, got %#v", event)
	}
	if parser.state != 0 {
		t.Fatalf("expected parser to reset to state 0, got %d", parser.state)
	}
}
