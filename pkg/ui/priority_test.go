package ui

import (
	"testing"
)

func TestClassifyMessage(t *testing.T) {
	tests := []struct {
		message  string
		expected MessagePriority
	}{
		// High priority messages
		{"✅ changes applied to main.go", PriorityHigh},
		{"❌ error: compilation failed", PriorityHigh},
		{"📝 created new file utils.go", PriorityHigh},
		{"🔧 edited main.go successfully", PriorityHigh},
		{"💡 answer: The function does X", PriorityHigh},
		{"✅ completed successfully", PriorityHigh},

		// Medium priority messages
		{"🎯 executing: go build", PriorityMedium},
		{"🚀 starting analysis", PriorityMedium},
		{"⚙️ agent analyzing code", PriorityMedium},
		{"▶️ running tests", PriorityMedium},

		// Low priority messages (should be hidden from UI)
		{"loading file main.go", PriorityLow},
		{"selected files for context", PriorityLow},
		{"workspace context built", PriorityLow},
		{"building context", PriorityLow},
		{"📄 loaded configuration", PriorityLow},
		{"processing file utils.go", PriorityLow},
		{"reading file config.json", PriorityLow},
		{"context built successfully", PriorityLow},

		// Verbose priority messages (debug mode only)
		{"debug: token count 1024", PriorityVerbose},
		{"token usage: 500 tokens", PriorityVerbose},
		{"api call to OpenAI", PriorityVerbose},
		{"estimated tokens: 750", PriorityVerbose},
		{"internal: processing request", PriorityVerbose},

		// Default to medium for unclassified
		{"some random message", PriorityMedium},
		{"updating database", PriorityMedium},
	}

	for _, tt := range tests {
		t.Run(tt.message, func(t *testing.T) {
			result := ClassifyMessage(tt.message)
			if result != tt.expected {
				t.Errorf("ClassifyMessage(%q) = %v, want %v", tt.message, result, tt.expected)
			}
		})
	}
}

func TestMessageFilter(t *testing.T) {
	tests := []struct {
		name     string
		filter   MessageFilter
		priority MessagePriority
		expected bool
	}{
		// Default UI filter tests
		{"UI High priority", DefaultUIFilter(), PriorityHigh, true},
		{"UI Medium priority", DefaultUIFilter(), PriorityMedium, true},
		{"UI Low priority", DefaultUIFilter(), PriorityLow, false},
		{"UI Verbose priority", DefaultUIFilter(), PriorityVerbose, false},

		// Console filter tests
		{"Console High priority", ConsoleFilter(), PriorityHigh, true},
		{"Console Medium priority", ConsoleFilter(), PriorityMedium, true},
		{"Console Low priority", ConsoleFilter(), PriorityLow, true},
		{"Console Verbose priority", ConsoleFilter(), PriorityVerbose, false},

		// Custom filter tests
		{"Verbose mode enabled", MessageFilter{VerboseMode: true}, PriorityVerbose, true},
		{"Verbose mode disabled", MessageFilter{VerboseMode: false}, PriorityVerbose, false},
		{"Show low disabled", MessageFilter{ShowLow: false}, PriorityLow, false},
		{"Show low enabled", MessageFilter{ShowLow: true}, PriorityLow, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.filter.ShouldShow(tt.priority)
			if result != tt.expected {
				t.Errorf("%s.ShouldShow(%v) = %v, want %v", tt.name, tt.priority, result, tt.expected)
			}
		})
	}
}

func TestSmartLogFiltering(t *testing.T) {
	// Test that verbose messages are filtered out in default UI mode
	originalFilter := GetMessageFilter()
	defer SetMessageFilter(originalFilter) // Restore original filter

	// Set to default UI filter (should hide low priority messages)
	SetMessageFilter(DefaultUIFilter())

	verboseMessages := []string{
		"loading file main.go",
		"selected files for context",
		"building context",
		"processing file utils.go",
		"reading file config.json",
		"debug: token count 1024",
		"token usage: 500 tokens",
	}

	for _, msg := range verboseMessages {
		priority := ClassifyMessage(msg)
		filter := GetMessageFilter()
		shouldShow := filter.ShouldShow(priority)

		// These verbose messages should be hidden in UI mode
		if shouldShow && (priority == PriorityLow || priority == PriorityVerbose) {
			t.Errorf("Message '%s' with priority %v should be hidden in UI mode but would be shown", msg, priority)
		}
	}

	// Test that important messages are always shown
	importantMessages := []string{
		"✅ changes applied to main.go",
		"❌ error: compilation failed",
		"📝 created new file utils.go",
		"💡 answer: The function does X",
		"🎯 executing: go build",
		"🚀 starting analysis",
	}

	for _, msg := range importantMessages {
		priority := ClassifyMessage(msg)
		filter := GetMessageFilter()
		shouldShow := filter.ShouldShow(priority)

		// These important messages should always be shown
		if !shouldShow {
			t.Errorf("Important message '%s' with priority %v should be shown but would be hidden", msg, priority)
		}
	}
}
