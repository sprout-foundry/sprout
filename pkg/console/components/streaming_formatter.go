package components

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/fatih/color"
)

// StreamingFormatter handles better formatting for streaming responses
type StreamingFormatter struct {
	mu             sync.Mutex
	lastUpdate     time.Time
	buffer         strings.Builder
	lineBuffer     strings.Builder
	isFirstChunk   bool
	lastWasNewline bool
	inCodeBlock    bool
	outputMutex    *sync.Mutex
	minUpdateDelay time.Duration
	maxBufferSize  int
}

// NewStreamingFormatter creates a new streaming formatter
func NewStreamingFormatter(outputMutex *sync.Mutex) *StreamingFormatter {
	return &StreamingFormatter{
		isFirstChunk:   true,
		outputMutex:    outputMutex,
		minUpdateDelay: 50 * time.Millisecond, // Minimum delay between updates
		maxBufferSize:  100,                   // Max chars to buffer before forcing output
	}
}

// Write formats and outputs streaming content
func (sf *StreamingFormatter) Write(content string) {
	sf.mu.Lock()
	defer sf.mu.Unlock()

	// Handle first chunk - show we're streaming
	if sf.isFirstChunk {
		sf.isFirstChunk = false
		if sf.outputMutex != nil {
			sf.outputMutex.Lock()
			// Clear the "Processing..." line
			fmt.Print("\r\033[K")
			// Show streaming indicator with color
			color.Cyan("✨ Streaming response...\n\n")
			sf.outputMutex.Unlock()
		}
	}

	// Add content to buffer
	sf.buffer.WriteString(content)

	// Check if we should flush
	shouldFlush := false
	bufferContent := sf.buffer.String()

	// More aggressive flushing for better streaming experience
	// Flush on any of these conditions:

	// 1. We have a complete line
	if strings.Contains(bufferContent, "\n") {
		shouldFlush = true
	}

	// 2. We have a word boundary (space) and some content
	if len(bufferContent) > 20 && strings.HasSuffix(bufferContent, " ") {
		shouldFlush = true
	}

	// 3. Buffer is getting large
	if len(bufferContent) >= sf.maxBufferSize {
		shouldFlush = true
	}

	// 4. Natural sentence breaks
	if len(bufferContent) > 10 && (strings.HasSuffix(bufferContent, ". ") ||
		strings.HasSuffix(bufferContent, "! ") ||
		strings.HasSuffix(bufferContent, "? ") ||
		strings.HasSuffix(bufferContent, ": ")) {
		shouldFlush = true
	}

	// 5. It's been long enough since last update (but reduce the delay)
	if time.Since(sf.lastUpdate) >= sf.minUpdateDelay && sf.buffer.Len() > 0 {
		shouldFlush = true
	}

	if shouldFlush {
		sf.flush()
	}
}

// flush outputs the buffered content
func (sf *StreamingFormatter) flush() {
	if sf.buffer.Len() == 0 {
		return
	}

	content := sf.buffer.String()
	sf.buffer.Reset()

	// Process the content line by line for better formatting
	lines := strings.Split(content, "\n")

	if sf.outputMutex != nil {
		sf.outputMutex.Lock()
		defer sf.outputMutex.Unlock()
	}

	for i, line := range lines {
		// Skip empty lines at the start
		if i == 0 && sf.lastWasNewline && strings.TrimSpace(line) == "" {
			continue
		}

		// Handle line buffering for better word wrapping
		if i < len(lines)-1 {
			// This is a complete line
			sf.outputLine(line)
			sf.lastWasNewline = true
		} else if strings.HasSuffix(content, "\n") {
			// Last piece ends with newline
			sf.outputLine(line)
			sf.lastWasNewline = true
		} else {
			// Incomplete line - for streaming, output it immediately
			// This ensures real-time display of content
			if line != "" {
				fmt.Print(line)
			}
			sf.lastWasNewline = false
		}
	}

	sf.lastUpdate = time.Now()
}

// outputLine outputs a complete line with formatting
func (sf *StreamingFormatter) outputLine(line string) {
	if sf.lineBuffer.Len() > 0 {
		// Output any buffered content first
		fmt.Print(sf.lineBuffer.String())
		sf.lineBuffer.Reset()
	}

	// Apply formatting based on content type
	trimmed := strings.TrimSpace(line)

	// Check if this is a markdown header
	if strings.HasPrefix(trimmed, "#") {
		// Add visual separation for headers
		if !sf.lastWasNewline {
			fmt.Println()
		}

		// Style headers with color
		if strings.HasPrefix(trimmed, "# ") {
			// Main header - bold blue
			color.New(color.FgBlue, color.Bold).Println(line)
		} else if strings.HasPrefix(trimmed, "## ") {
			// Sub header - blue
			color.New(color.FgBlue).Println(line)
		} else if strings.HasPrefix(trimmed, "### ") {
			// Level 3 headers - cyan
			color.New(color.FgCyan).Println(line)
		} else {
			// Other headers - normal with emphasis
			color.New(color.Bold).Println(line)
		}
	} else if strings.HasPrefix(trimmed, "```") {
		// Code blocks - handle language identifier
		if len(trimmed) > 3 {
			// Code fence with language
			color.New(color.FgGreen, color.Faint).Println(line)
			sf.inCodeBlock = !sf.inCodeBlock
		} else {
			// Plain code fence
			color.New(color.Faint).Println(line)
			sf.inCodeBlock = !sf.inCodeBlock
		}
	} else if sf.inCodeBlock {
		// Inside code block - yellow/amber color
		color.New(color.FgYellow).Println(line)
	} else if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") || strings.HasPrefix(trimmed, "+ ") {
		// Bullet points - green bullet with normal text
		bulletText := strings.TrimSpace(trimmed[2:])
		fmt.Print("  ")
		color.New(color.FgGreen).Print("• ")
		fmt.Println(bulletText)
	} else if matched, _ := regexp.MatchString(`^\d+\.`, trimmed); matched {
		// Numbered lists
		fmt.Print("  ")
		color.New(color.FgGreen).Print(strings.SplitN(trimmed, ".", 2)[0] + ". ")
		fmt.Println(strings.TrimSpace(strings.SplitN(trimmed, ".", 2)[1]))
	} else if strings.HasPrefix(trimmed, ">") {
		// Blockquotes - dim italic
		quotedText := strings.TrimSpace(strings.TrimPrefix(trimmed, ">"))
		fmt.Print("  ")
		color.New(color.Faint, color.Italic).Println("│ " + quotedText)
	} else if strings.HasPrefix(trimmed, "---") || strings.HasPrefix(trimmed, "***") || strings.HasPrefix(trimmed, "___") {
		// Horizontal rules
		if len(strings.TrimSpace(trimmed)) >= 3 {
			color.New(color.Faint).Println(strings.Repeat("─", 60))
		} else {
			fmt.Println(line)
		}
	} else if trimmed == "" && sf.lastWasNewline {
		// Preserve paragraph breaks
		fmt.Println()
	} else {
		// Regular line - apply inline formatting
		formatted := sf.applyInlineFormatting(line)
		fmt.Println(formatted)
	}
}

// Finalize ensures all buffered content is output
func (sf *StreamingFormatter) Finalize() {
	sf.mu.Lock()
	defer sf.mu.Unlock()

	// Flush any remaining buffer
	if sf.buffer.Len() > 0 {
		sf.flush()
	}

	// Output any remaining line buffer
	if sf.lineBuffer.Len() > 0 {
		if sf.outputMutex != nil {
			sf.outputMutex.Lock()
			defer sf.outputMutex.Unlock()
		}
		fmt.Println(sf.lineBuffer.String())
		sf.lineBuffer.Reset()
	}

	// Add a final newline for clean separation
	if !sf.lastWasNewline {
		fmt.Println()
	}
}

// Reset prepares the formatter for a new streaming session
func (sf *StreamingFormatter) Reset() {
	sf.mu.Lock()
	defer sf.mu.Unlock()

	sf.buffer.Reset()
	sf.lineBuffer.Reset()
	sf.isFirstChunk = true
	sf.lastWasNewline = false
	sf.inCodeBlock = false
	sf.lastUpdate = time.Time{}
}

// HasProcessedContent returns true if any content has been processed
func (sf *StreamingFormatter) HasProcessedContent() bool {
	sf.mu.Lock()
	defer sf.mu.Unlock()
	return !sf.isFirstChunk
}

// applyInlineFormatting applies markdown inline formatting using ANSI codes
func (sf *StreamingFormatter) applyInlineFormatting(text string) string {
	// Handle inline code blocks `code`
	codePattern := regexp.MustCompile("`([^`]+)`")
	text = codePattern.ReplaceAllStringFunc(text, func(match string) string {
		code := match[1 : len(match)-1]
		return color.New(color.FgMagenta).Sprint(code)
	})

	// Handle bold **text** - use non-greedy matching
	boldPattern1 := regexp.MustCompile(`\*\*(.+?)\*\*`)
	text = boldPattern1.ReplaceAllStringFunc(text, func(match string) string {
		content := match[2 : len(match)-2]
		return color.New(color.Bold).Sprint(content)
	})

	// Handle bold __text__ - use non-greedy matching
	boldPattern2 := regexp.MustCompile(`__(.+?)__`)
	text = boldPattern2.ReplaceAllStringFunc(text, func(match string) string {
		content := match[2 : len(match)-2]
		return color.New(color.Bold).Sprint(content)
	})

	// Handle italic *text* - use non-greedy matching but avoid matching bold
	italicPattern1 := regexp.MustCompile(`(?:^|[^*])\*([^*]+)\*(?:[^*]|$)`)
	text = italicPattern1.ReplaceAllStringFunc(text, func(match string) string {
		// Extract just the content between single asterisks
		start := strings.Index(match, "*")
		end := strings.LastIndex(match, "*")
		if start == -1 || end == -1 || start == end {
			return match
		}
		prefix := match[:start]
		suffix := match[end+1:]
		content := match[start+1 : end]
		return prefix + color.New(color.Italic).Sprint(content) + suffix
	})

	// Handle italic _text_ - use non-greedy matching but avoid matching bold
	italicPattern2 := regexp.MustCompile(`(?:^|[^_])_([^_]+)_(?:[^_]|$)`)
	text = italicPattern2.ReplaceAllStringFunc(text, func(match string) string {
		// Extract just the content between single underscores
		start := strings.Index(match, "_")
		end := strings.LastIndex(match, "_")
		if start == -1 || end == -1 || start == end {
			return match
		}
		prefix := match[:start]
		suffix := match[end+1:]
		content := match[start+1 : end]
		return prefix + color.New(color.Italic).Sprint(content) + suffix
	})

	return text
}
