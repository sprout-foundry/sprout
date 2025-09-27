package components

import (
	"strings"
	"testing"
)

func TestInputManager_ScrollKeybindings(t *testing.T) {
	// Create a mock input manager for testing
	im := NewInputManager("test> ")

	// Mock scroll callbacks using the public interface
	var scrollUpCount, scrollDownCount int
	var scrollUpLines, scrollDownLines int

	im.SetScrollCallbacks(
		func(lines int) {
			t.Logf("Scroll up called with %d lines", lines)
			scrollUpCount++
			scrollUpLines = lines
		},
		func(lines int) {
			t.Logf("Scroll down called with %d lines", lines)
			scrollDownCount++
			scrollDownLines = lines
		},
	)

	// Set terminal dimensions
	im.termWidth = 80
	im.termHeight = 24

	// Debug: Check if callbacks are set
	t.Logf("Scroll up callback set: %v", im.onScrollUp != nil)
	t.Logf("Scroll down callback set: %v", im.onScrollDown != nil)

	// Temporarily set running flag for testing (bypassing the need for raw mode)
	im.running = true

	tests := []struct {
		name              string
		key               byte
		expectedUp        int
		expectedDown      int
		expectedUpLines   int
		expectedDownLines int
	}{
		{
			name:              "Ctrl+B (full page up)",
			key:               2,
			expectedUp:        1,
			expectedDown:      0,
			expectedUpLines:   23, // termHeight - 1
			expectedDownLines: 0,
		},
		{
			name:              "Ctrl+F (full page down)",
			key:               6,
			expectedUp:        0,
			expectedDown:      1,
			expectedUpLines:   0,
			expectedDownLines: 23, // termHeight - 1
		},
		{
			name:              "Ctrl+J (half page up)",
			key:               10,
			expectedUp:        1,
			expectedDown:      0,
			expectedUpLines:   12, // termHeight / 2
			expectedDownLines: 0,
		},
		{
			name:              "Ctrl+K (half page down)",
			key:               11,
			expectedUp:        0,
			expectedDown:      1,
			expectedUpLines:   0,
			expectedDownLines: 12, // termHeight / 2
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset counters
			scrollUpCount = 0
			scrollDownCount = 0
			scrollUpLines = 0
			scrollDownLines = 0

			// Process the key
			im.processKeystrokes([]byte{tt.key})

			// Verify scroll callbacks were called correctly
			if scrollUpCount != tt.expectedUp {
				t.Errorf("%s: expected %d scroll up calls, got %d", tt.name, tt.expectedUp, scrollUpCount)
			}
			if scrollDownCount != tt.expectedDown {
				t.Errorf("%s: expected %d scroll down calls, got %d", tt.name, tt.expectedDown, scrollDownCount)
			}
			if scrollUpLines != tt.expectedUpLines {
				t.Errorf("%s: expected %d scroll up lines, got %d", tt.name, tt.expectedUpLines, scrollUpLines)
			}
			if scrollDownLines != tt.expectedDownLines {
				t.Errorf("%s: expected %d scroll down lines, got %d", tt.name, tt.expectedDownLines, scrollDownLines)
			}
		})
	}
}

func TestInputManager_MouseEventHandling(t *testing.T) {
	// Create a mock input manager for testing
	im := NewInputManager("test> ")

	// Mock scroll callbacks
	var scrollUpCount, scrollDownCount int

	im.SetScrollCallbacks(
		func(lines int) {
			scrollUpCount++
		},
		func(lines int) {
			scrollDownCount++
		},
	)

	// Test mouse events are properly ignored
	tests := []struct {
		name          string
		mouseSequence []byte
		expectedUp    int
		expectedDown  int
	}{
		{
			name:          "X11 mouse event (ESC [ M)",
			mouseSequence: []byte{27, '[', 'M', 32, 10, 20}, // ESC [ M button x y
			expectedUp:    0,
			expectedDown:  0,
		},
		{
			name:          "SGR mouse event (ESC [ <)",
			mouseSequence: []byte{27, '[', '<', 0, ';', 10, ';', 20, 'M'}, // ESC [ < button;x;y M
			expectedUp:    0,
			expectedDown:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset counters
			scrollUpCount = 0
			scrollDownCount = 0

			// Process the mouse sequence
			im.processKeystrokes(tt.mouseSequence)

			// Verify no scroll callbacks were triggered
			if scrollUpCount != tt.expectedUp {
				t.Errorf("%s: expected %d scroll up calls, got %d", tt.name, tt.expectedUp, scrollUpCount)
			}
			if scrollDownCount != tt.expectedDown {
				t.Errorf("%s: expected %d scroll down calls, got %d", tt.name, tt.expectedDown, scrollDownCount)
			}
		})
	}
}

func TestStreamingFormatter_NoLineDuplication(t *testing.T) {
	// Create a mock console buffer to track content additions
	var contentAdditions []string
	var addContentCount int

	mockBuffer := &mockConsoleBuffer{
		addContentFunc: func(content string) {
			addContentCount++
			contentAdditions = append(contentAdditions, content)
		},
	}

	// Create streaming formatter with output function that simulates safePrint behavior
	sf := NewStreamingFormatter(nil)
	sf.SetConsoleBuffer(mockBuffer)

	// Set output function to simulate safePrint behavior
	sf.SetOutputFunc(func(text string) {
		// Simulate safePrint - add to buffer and print
		if mockBuffer.addContentFunc != nil {
			mockBuffer.addContentFunc(text)
		}
		// In real usage, this would print to terminal, but for testing we just track it
	})

	// Test content that would previously cause duplication
	testContent := `This is line 1
This is line 2
This is line 3
`

	// Write content
	sf.Write(testContent)

	// Force flush to ensure all content is processed
	sf.ForceFlush()

	// Finalize to complete processing
	sf.Finalize()

	// Verify content was added line by line (no duplication)
	// The streaming formatter processes content line by line for better streaming experience
	// Test content has 3 lines + 1 final newline = 4 additions
	expectedAdditions := 4
	if addContentCount != expectedAdditions {
		t.Errorf("Expected %d content additions (line by line), got %d", expectedAdditions, addContentCount)
	}

	// Verify the content is correct - all additions should match expected lines
	if len(contentAdditions) != expectedAdditions {
		t.Errorf("Expected %d content additions, got %d", expectedAdditions, len(contentAdditions))
	} else {
		expectedLines := []string{
			"This is line 1\n",
			"This is line 2\n",
			"This is line 3\n",
			"\n", // Final newline
		}

		for i, expectedLine := range expectedLines {
			if i >= len(contentAdditions) {
				t.Errorf("Missing content addition %d", i)
				continue
			}

			actualLine := contentAdditions[i]

			// Normalize line endings for comparison
			expectedLine = strings.ReplaceAll(expectedLine, "\r\n", "\n")
			actualLine = strings.ReplaceAll(actualLine, "\r\n", "\n")

			if actualLine != expectedLine {
				t.Errorf("Content mismatch at line %d:\nExpected: %q\nActual: %q", i, expectedLine, actualLine)
			}
		}
	}
}

// mockConsoleBuffer implements the console buffer interface for testing
type mockConsoleBuffer struct {
	addContentFunc func(string)
}

func (m *mockConsoleBuffer) AddContent(content string) {
	if m.addContentFunc != nil {
		m.addContentFunc(content)
	}
}
