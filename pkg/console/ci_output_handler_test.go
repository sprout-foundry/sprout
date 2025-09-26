package console

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"
)

func TestCIOutputHandler_EnvironmentDetection(t *testing.T) {
	tests := []struct {
		name              string
		envVars           map[string]string
		expectCI          bool
		expectInteractive bool
	}{
		{
			name:              "CI environment",
			envVars:           map[string]string{"CI": "1"},
			expectCI:          true,
			expectInteractive: false,
		},
		{
			name:              "GitHub Actions",
			envVars:           map[string]string{"GITHUB_ACTIONS": "true"},
			expectCI:          true,
			expectInteractive: false,
		},
		{
			name:              "No CI environment",
			envVars:           map[string]string{},
			expectCI:          false,
			expectInteractive: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variables
			for k, v := range tt.envVars {
				os.Setenv(k, v)
				defer os.Unsetenv(k)
			}

			buf := &bytes.Buffer{}
			handler := NewCIOutputHandler(buf)

			if handler.IsCI() != tt.expectCI {
				t.Errorf("IsCI() = %v, want %v", handler.IsCI(), tt.expectCI)
			}

			// Buffer is not a terminal, so should always be non-interactive
			if handler.IsInteractive() != tt.expectInteractive {
				t.Errorf("IsInteractive() = %v, want %v", handler.IsInteractive(), tt.expectInteractive)
			}
		})
	}
}

func TestCIOutputHandler_ANSIStripping(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Simple text",
			input:    "Hello, World!",
			expected: "Hello, World!",
		},
		{
			name:     "Text with color codes",
			input:    "\033[31mRed text\033[0m",
			expected: "Red text",
		},
		{
			name:     "Complex ANSI sequences",
			input:    "\033[1;32mBold Green\033[0m \033[2J\033[H",
			expected: "Bold Green ",
		},
		{
			name:     "Multiple escape sequences",
			input:    "Start\033[31m Red \033[32mGreen\033[0m End",
			expected: "Start Red Green End",
		},
	}

	buf := &bytes.Buffer{}
	handler := NewCIOutputHandler(buf)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := handler.stripANSIEscapeCodes(tt.input)
			if result != tt.expected {
				t.Errorf("stripANSIEscapeCodes(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestCIOutputHandler_ProgressOutput(t *testing.T) {
	// Set CI environment
	os.Setenv("CI", "1")
	defer os.Unsetenv("CI")

	buf := &bytes.Buffer{}
	handler := NewCIOutputHandler(buf)

	// Update metrics
	handler.UpdateMetrics(1500, 1000, 4000, 2, 0.0045)

	// First progress should show immediately
	handler.PrintProgress()
	output := buf.String()

	if !strings.Contains(output, "[CI Progress]") {
		t.Errorf("Expected CI progress indicator, got: %s", output)
	}

	if !strings.Contains(output, "Iteration: 2") {
		t.Errorf("Expected iteration count, got: %s", output)
	}

	if !strings.Contains(output, "1.5K") {
		t.Errorf("Expected formatted token count, got: %s", output)
	}

	if !strings.Contains(output, "$0.0045") {
		t.Errorf("Expected cost information, got: %s", output)
	}
}

func TestCIOutputHandler_ProgressInterval(t *testing.T) {
	os.Setenv("CI", "1")
	defer os.Unsetenv("CI")

	buf := &bytes.Buffer{}
	handler := NewCIOutputHandler(buf)

	// Set a short interval for testing
	handler.progressInterval = 100 * time.Millisecond

	// First progress
	handler.PrintProgress()
	firstOutput := buf.String()

	// Immediate second call should not produce output
	handler.PrintProgress()
	if buf.String() != firstOutput {
		t.Error("Progress shown too soon")
	}

	// Wait for interval
	time.Sleep(150 * time.Millisecond)

	// Now progress should show
	handler.PrintProgress()
	if buf.String() == firstOutput {
		t.Error("Progress not shown after interval")
	}
}

func TestCIOutputHandler_TokenFormatting(t *testing.T) {
	buf := &bytes.Buffer{}
	handler := NewCIOutputHandler(buf)

	tests := []struct {
		tokens   int
		expected string
	}{
		{100, "100"},
		{1500, "1.5K"},
		{15000, "15.0K"},
		{1500000, "1.5M"},
		{15000000, "15.0M"},
	}

	for _, tt := range tests {
		result := handler.formatTokensCompact(tt.tokens)
		if result != tt.expected {
			t.Errorf("formatTokensCompact(%d) = %s, want %s", tt.tokens, result, tt.expected)
		}
	}
}

func TestCIOutputHandler_CostFormatting(t *testing.T) {
	buf := &bytes.Buffer{}
	handler := NewCIOutputHandler(buf)

	tests := []struct {
		cost     float64
		expected string
	}{
		{0.0001, "$0.0001"},
		{0.0045, "$0.0045"},
		{0.045, "$0.045"},
		{0.45, "$0.450"},
		{4.5, "$4.50"},
		{45.0, "$45.00"},
	}

	for _, tt := range tests {
		result := handler.formatCostCompact(tt.cost)
		if result != tt.expected {
			t.Errorf("formatCostCompact(%f) = %s, want %s", tt.cost, result, tt.expected)
		}
	}
}

func TestCIOutputHandler_Summary(t *testing.T) {
	tests := []struct {
		name     string
		isCI     bool
		tokens   int
		cost     float64
		checkFor []string
	}{
		{
			name:     "CI summary",
			isCI:     true,
			tokens:   5000,
			cost:     0.025,
			checkFor: []string{"[CI Summary]", "Total Tokens:", "Total Cost:", "Elapsed Time:"},
		},
		{
			name:     "Non-CI summary",
			isCI:     false,
			tokens:   5000,
			cost:     0.025,
			checkFor: []string{"Session:", "tokens", "$"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.isCI {
				os.Setenv("CI", "1")
				defer os.Unsetenv("CI")
			}

			buf := &bytes.Buffer{}
			handler := NewCIOutputHandler(buf)

			handler.UpdateMetrics(tt.tokens, 1000, 4000, 3, tt.cost)
			handler.PrintSummary()

			output := buf.String()
			for _, check := range tt.checkFor {
				if !strings.Contains(output, check) {
					t.Errorf("Summary missing %q, got: %s", check, output)
				}
			}
		})
	}
}

func TestCIOutputHandler_NonInteractiveWrite(t *testing.T) {
	buf := &bytes.Buffer{}
	handler := NewCIOutputHandler(buf)

	// Write text with ANSI codes
	input := "Processing\033[32m✓\033[0m Complete\n"
	handler.Write([]byte(input))

	// In non-interactive mode (buffer), ANSI codes should be stripped
	output := buf.String()
	if strings.Contains(output, "\033") {
		t.Errorf("ANSI codes not stripped in non-interactive mode: %q", output)
	}

	expected := "Processing✓ Complete\n"
	if output != expected {
		t.Errorf("Write() output = %q, want %q", output, expected)
	}
}
