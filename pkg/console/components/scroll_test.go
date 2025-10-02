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
	var scrollUpLines, scrollDownLines int

	im.SetScrollCallbacks(
		func(lines int) {
			scrollUpCount++
			scrollUpLines = lines
		},
		func(lines int) {
			scrollDownCount++
			scrollDownLines = lines
		},
	)

	im.termWidth = 80
	im.termHeight = 24
	im.running = true

	focusMode := "input"
	im.SetFocusProvider(func() string { return focusMode })
	im.SetScrollingProvider(func() bool { return true })

	sgrWheelDown := []byte{27, '[', '<', '6', '5', ';', '1', '0', ';', '5', 'M'}
	sgrWheelUp := []byte{27, '[', '<', '6', '4', ';', '1', '0', ';', '5', 'M'}
	legacyWheelUp := []byte{27, '[', 'M', 32 + 64, 35, 40}
	legacyWheelDown := []byte{27, '[', 'M', 32 + 65, 35, 40}

	// Helper to reset counters
	resetCounters := func() {
		scrollUpCount = 0
		scrollDownCount = 0
		scrollUpLines = 0
		scrollDownLines = 0
	}

	pagelines := im.mouseScrollLines()

	t.Run("InputFocusIgnoresMouse", func(t *testing.T) {
		resetCounters()
		focusMode = "input"
		im.processKeystrokes(sgrWheelDown)
		if scrollUpCount != 0 || scrollDownCount != 0 {
			t.Fatalf("expected no scroll callbacks in input focus, got up=%d down=%d", scrollUpCount, scrollDownCount)
		}
	})

	t.Run("OutputFocusScrollsSGRWheelDown", func(t *testing.T) {
		resetCounters()
		focusMode = "output"
		im.processKeystrokes(sgrWheelDown)
		if scrollDownCount != 1 {
			t.Fatalf("expected one scroll down callback, got %d", scrollDownCount)
		}
		if scrollDownLines != pagelines {
			t.Fatalf("expected scroll down lines %d, got %d", pagelines, scrollDownLines)
		}
	})

	t.Run("OutputFocusScrollsSGRWheelUp", func(t *testing.T) {
		resetCounters()
		focusMode = "output"
		im.processKeystrokes(sgrWheelUp)
		if scrollUpCount != 1 {
			t.Fatalf("expected one scroll up callback, got %d", scrollUpCount)
		}
		if scrollUpLines != pagelines {
			t.Fatalf("expected scroll up lines %d, got %d", pagelines, scrollUpLines)
		}
	})

	t.Run("OutputFocusScrollsLegacyWheelDown", func(t *testing.T) {
		resetCounters()
		focusMode = "output"
		im.processKeystrokes(legacyWheelDown)
		if scrollDownCount != 1 {
			t.Fatalf("expected one legacy scroll down callback, got %d", scrollDownCount)
		}
		if scrollDownLines != pagelines {
			t.Fatalf("expected legacy scroll down lines %d, got %d", pagelines, scrollDownLines)
		}
	})

	t.Run("OutputFocusScrollsLegacyWheelUp", func(t *testing.T) {
		resetCounters()
		focusMode = "output"
		im.processKeystrokes(legacyWheelUp)
		if scrollUpCount != 1 {
			t.Fatalf("expected one legacy scroll up callback, got %d", scrollUpCount)
		}
		if scrollUpLines != pagelines {
			t.Fatalf("expected legacy scroll up lines %d, got %d", pagelines, scrollUpLines)
		}
	})
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
	// The streaming formatter processes content line by line and keeps the trailing newline with the final line
	// Test content has 3 lines, so we expect exactly 3 additions
	expectedAdditions := 3
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
