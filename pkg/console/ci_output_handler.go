package console

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"
)

// CIOutputHandler manages output formatting for CI/non-interactive environments
type CIOutputHandler struct {
	writer           io.Writer
	isCI             bool
	isInteractive    bool
	progressInterval time.Duration
	lastProgressTime time.Time
	mutex            sync.Mutex

	// Token and cost tracking
	totalTokens      int
	totalCost        float64
	iteration        int
	contextTokens    int
	maxContextTokens int

	// Progress tracking
	startTime      time.Time
	operationCount int

	// Buffer for handling split content
	buffer strings.Builder
}

// NewCIOutputHandler creates a new CI output handler
func NewCIOutputHandler(writer io.Writer) *CIOutputHandler {
	if writer == nil {
		writer = os.Stdout
	}

	// Detect CI environment
	isCI := os.Getenv("CI") != "" || os.Getenv("GITHUB_ACTIONS") != ""

	// Detect if we're in an interactive terminal
	isInteractive := false
	if f, ok := writer.(*os.File); ok {
		isInteractive = term.IsTerminal(int(f.Fd()))
	}

	// In CI, use shorter progress intervals for more frequent updates
	progressInterval := 2 * time.Second
	if !isCI {
		progressInterval = 30 * time.Second
	}

	return &CIOutputHandler{
		writer:           writer,
		isCI:             isCI,
		isInteractive:    isInteractive,
		progressInterval: progressInterval,
		startTime:        time.Now(),
	}
}

// Write implements io.Writer interface
func (h *CIOutputHandler) Write(p []byte) (n int, err error) {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	// Add to buffer to handle split content
	h.buffer.Write(p)
	content := h.buffer.String()

	// Debug all writes
	if os.Getenv("LEDIT_DEBUG_OUTPUT") == "1" {
		fmt.Fprintf(os.Stderr, "[DEBUG CIOutputHandler.Write] Buffer now: %q\n", content)
	}

	// Check if we might be in the middle of a completion signal
	partialSignals := []string{"[", "[[", "[[T", "[[TA", "[[TAS", "[[TASK",
		"[[TASK_", "[[TASK_C", "[[TASK_CO", "[[TASK_COM", "[[TASK_COMP",
		"[[TASK_COMPL", "[[TASK_COMPLE", "[[TASK_COMPLET", "[[TASK_COMPLETE"}

	for _, partial := range partialSignals {
		if strings.HasSuffix(content, partial) {
			// Wait for more content
			return len(p), nil
		}
	}

	// Filter out task completion signals
	completionSignals := []string{
		"[[TASK_COMPLETE]]",
		"[[TASKCOMPLETE]]",
		"[[TASK COMPLETE]]",
		"[[task_complete]]",
		"[[taskcomplete]]",
		"[[task complete]]",
	}

	originalContent := content
	for _, signal := range completionSignals {
		content = strings.ReplaceAll(content, signal, "")
	}

	// Debug: log if we filtered anything
	if os.Getenv("LEDIT_DEBUG_OUTPUT") == "1" && originalContent != content {
		fmt.Fprintf(os.Stderr, "[DEBUG] Filtered completion signal from output\n")
	}

	// Clear buffer
	h.buffer.Reset()

	// In CI/non-interactive mode, fix line endings and strip ANSI codes
	if !h.isInteractive || h.isCI {
		// Replace carriage returns without newlines with proper newlines
		// This handles progress-style updates that use \r
		if strings.Contains(content, "\r") && !strings.Contains(content, "\n") {
			content = strings.ReplaceAll(content, "\r", "\n")
		}

		// Strip ANSI escape codes
		content = h.stripANSIEscapeCodes(content)

		// Also strip any cursor movement sequences
		content = h.stripCursorSequences(content)
	}

	// Write the filtered content (even if empty, to maintain proper io.Writer behavior)
	n, err = h.writer.Write([]byte(content))

	// If we filtered content but wrote nothing, return the original byte count
	// to maintain io.Writer contract (the caller thinks we wrote everything)
	if n == 0 && len(p) > 0 {
		return len(p), nil
	}

	return n, err
}

// WriteString writes a string with appropriate formatting
func (h *CIOutputHandler) WriteString(s string) error {
	_, err := h.Write([]byte(s))
	return err
}

// Printf writes formatted output
func (h *CIOutputHandler) Printf(format string, args ...interface{}) {
	h.WriteString(fmt.Sprintf(format, args...))
}

// PrintProgress prints a progress update in CI-friendly format
func (h *CIOutputHandler) PrintProgress() {
	h.mutex.Lock()

	now := time.Now()
	if now.Sub(h.lastProgressTime) < h.progressInterval {
		h.mutex.Unlock()
		return
	}

	h.lastProgressTime = now
	elapsed := now.Sub(h.startTime)

	// Copy values while holding mutex
	isCI := h.isCI
	isInteractive := h.isInteractive
	iteration := h.iteration
	contextTokens := h.contextTokens
	maxContextTokens := h.maxContextTokens
	totalTokens := h.totalTokens
	totalCost := h.totalCost
	h.mutex.Unlock()

	// Format progress without holding mutex
	var progress string
	if isCI {
		progress = fmt.Sprintf("\n[CI Progress] Iteration: %d | Context: %s/%s | Tokens: %s | Cost: %s | Elapsed: %s\n",
			iteration,
			h.formatTokensCompact(contextTokens),
			h.formatTokensCompact(maxContextTokens),
			h.formatTokensCompact(totalTokens),
			h.formatCostCompact(totalCost),
			h.formatDuration(elapsed))
	} else if !isInteractive {
		progress = fmt.Sprintf("... Processing (elapsed: %s, tokens: %d) ...\n",
			h.formatDuration(elapsed), totalTokens)
	}

	if progress != "" {
		h.WriteString(progress)
	}
}

// UpdateMetrics updates the tracked metrics
func (h *CIOutputHandler) UpdateMetrics(totalTokens, contextTokens, maxContextTokens, iteration int, totalCost float64) {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	h.totalTokens = totalTokens
	h.contextTokens = contextTokens
	h.maxContextTokens = maxContextTokens
	h.iteration = iteration
	h.totalCost = totalCost
	h.operationCount++
}

// PrintSummary prints a final summary
func (h *CIOutputHandler) PrintSummary() {
	h.mutex.Lock()
	elapsed := time.Since(h.startTime)
	isCI := h.isCI
	iteration := h.iteration
	totalTokens := h.totalTokens
	totalCost := h.totalCost
	operationCount := h.operationCount
	h.mutex.Unlock()

	// Format summary without holding the mutex
	var summary string
	if isCI {
		summary = fmt.Sprintf("\n[CI Summary]\n"+
			"├─ Total Iterations: %d\n"+
			"├─ Total Tokens: %s\n"+
			"├─ Total Cost: %s\n"+
			"├─ Elapsed Time: %s\n"+
			"└─ Operations: %d\n",
			iteration,
			h.formatTokensVerbose(totalTokens),
			h.formatCostVerbose(totalCost),
			h.formatDuration(elapsed),
			operationCount)
	} else {
		summary = fmt.Sprintf("\n💰 Session: %s tokens | %s | Duration: %s\n",
			h.formatTokensVerbose(totalTokens),
			h.formatCostVerbose(totalCost),
			h.formatDuration(elapsed))
	}

	// Write the complete summary
	h.WriteString(summary)
}

// ShouldShowProgress returns true if progress should be shown
func (h *CIOutputHandler) ShouldShowProgress() bool {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	// Always show progress in CI after interval
	if h.isCI && time.Since(h.lastProgressTime) >= h.progressInterval {
		return true
	}

	// In non-interactive non-CI, show less frequent progress
	if !h.isInteractive && time.Since(h.lastProgressTime) >= h.progressInterval*2 {
		return true
	}

	return false
}

// IsCI returns true if running in CI environment
func (h *CIOutputHandler) IsCI() bool {
	return h.isCI
}

// IsInteractive returns true if running in an interactive terminal
func (h *CIOutputHandler) IsInteractive() bool {
	return h.isInteractive
}

// stripANSIEscapeCodes removes ANSI escape sequences from text
func (h *CIOutputHandler) stripANSIEscapeCodes(text string) string {
	// Remove common ANSI escape sequences
	// This is a simple implementation - could be enhanced with regex
	var result strings.Builder
	inEscape := false

	for i := 0; i < len(text); i++ {
		if text[i] == '\033' && i+1 < len(text) && text[i+1] == '[' {
			inEscape = true
			i++ // Skip the '['
			continue
		}

		if inEscape {
			// Skip until we find a letter that terminates the sequence
			if (text[i] >= 'A' && text[i] <= 'Z') || (text[i] >= 'a' && text[i] <= 'z') {
				inEscape = false
			}
			continue
		}

		result.WriteByte(text[i])
	}

	return result.String()
}

// stripCursorSequences removes cursor control sequences from text
func (h *CIOutputHandler) stripCursorSequences(text string) string {
	// Remove sequences like \033[K (clear to end of line) and other cursor controls
	replacements := []string{
		"\033[K",    // Clear to end of line
		"\033[2K",   // Clear entire line
		"\033[J",    // Clear to end of screen
		"\033[2J",   // Clear entire screen
		"\033[H",    // Home cursor
		"\033[?25l", // Hide cursor
		"\033[?25h", // Show cursor
	}

	result := text
	for _, seq := range replacements {
		result = strings.ReplaceAll(result, seq, "")
	}

	// Remove any remaining cursor positioning sequences like \033[1;1H
	// This is a simple pattern match for cursor positioning
	for {
		start := strings.Index(result, "\033[")
		if start == -1 {
			break
		}
		end := start + 2
		for end < len(result) && result[end] >= '0' && result[end] <= '9' || result[end] == ';' {
			end++
		}
		if end < len(result) && (result[end] == 'H' || result[end] == 'f') {
			result = result[:start] + result[end+1:]
		} else {
			break
		}
	}

	return result
}

// Format helpers
func (h *CIOutputHandler) formatTokensCompact(tokens int) string {
	if tokens >= 1000000 {
		return fmt.Sprintf("%.1fM", float64(tokens)/1000000)
	} else if tokens >= 1000 {
		return fmt.Sprintf("%.1fK", float64(tokens)/1000)
	}
	return fmt.Sprintf("%d", tokens)
}

func (h *CIOutputHandler) formatTokensVerbose(tokens int) string {
	if tokens >= 1000000 {
		return fmt.Sprintf("%d (%s)", tokens, h.formatTokensCompact(tokens))
	} else if tokens >= 1000 {
		return fmt.Sprintf("%d (%s)", tokens, h.formatTokensCompact(tokens))
	}
	return fmt.Sprintf("%d", tokens)
}

func (h *CIOutputHandler) formatCostCompact(cost float64) string {
	if cost < 0.01 {
		return fmt.Sprintf("$%.4f", cost)
	} else if cost < 1.0 {
		return fmt.Sprintf("$%.3f", cost)
	}
	return fmt.Sprintf("$%.2f", cost)
}

func (h *CIOutputHandler) formatCostVerbose(cost float64) string {
	return h.formatCostCompact(cost)
}

func (h *CIOutputHandler) formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	} else if d < time.Hour {
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
}
