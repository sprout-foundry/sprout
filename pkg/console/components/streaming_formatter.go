package components

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/fatih/color"
)

const contentPadding = "  " // 2 spaces for left padding

// StreamingFormatter handles better formatting for streaming responses
type StreamingFormatter struct {
	mu             sync.Mutex
	lastUpdate     time.Time
	buffer         strings.Builder
	lineBuffer     strings.Builder
	isFirstChunk   bool
	firstContent   bool // Track if this is the first actual content after streaming indicator
	lastWasNewline bool
	inCodeBlock    bool
	inListContext  bool // Track if we're in a list to avoid extra spacing
	outputMutex    *sync.Mutex
	consoleBuffer  interface{ AddContent(string) } // Interface to avoid circular import
	minUpdateDelay time.Duration
	maxBufferSize  int
	finalized      bool // Prevent double finalization

	// Custom output function (if set, replaces fmt.Print calls)
	outputFunc func(string)
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

// SetConsoleBuffer sets the console buffer for output tracking
func (sf *StreamingFormatter) SetConsoleBuffer(buffer interface{ AddContent(string) }) {
	sf.mu.Lock()
	defer sf.mu.Unlock()
	sf.consoleBuffer = buffer
}

// SetOutputFunc sets a custom output function to replace fmt.Print calls
func (sf *StreamingFormatter) SetOutputFunc(outputFunc func(string)) {
	sf.mu.Lock()
	defer sf.mu.Unlock()
	sf.outputFunc = outputFunc
}

// print outputs text using custom output function if available, otherwise fmt.Print
func (sf *StreamingFormatter) print(text string) {
	if sf.outputFunc != nil {
		sf.outputFunc(text)
	} else {
		fmt.Print(text)
	}
}

// println outputs text with newline using custom output function if available, otherwise fmt.Println
func (sf *StreamingFormatter) println(text string) {
	if sf.outputFunc != nil {
		sf.outputFunc(text + "\n")
	} else {
		fmt.Println(text)
	}
}

// printlnColored outputs colored text with newline using consistent output system
func (sf *StreamingFormatter) printlnColored(colorFunc func(...interface{}) string, text string) {
	if sf.outputFunc != nil {
		sf.outputFunc(colorFunc(text) + "\n")
	} else {
		fmt.Println(colorFunc(text))
	}
}

// Write formats and outputs streaming content
func (sf *StreamingFormatter) Write(content string) {
	sf.mu.Lock()
	defer sf.mu.Unlock()

	// Don't write after finalization
	if sf.finalized {
		return
	}

	// Handle first chunk - show we're streaming
	if sf.isFirstChunk {
		sf.isFirstChunk = false
		sf.firstContent = true // Mark that the next content should be flushed aggressively
		if sf.outputMutex != nil {
			sf.outputMutex.Lock()
			// Ensure we're on a new line before showing streaming indicator
			if !sf.lastWasNewline {
				sf.println("") // Add separation from processing message
			}
			// Use consistent output method to ensure proper cursor positioning
			if sf.outputFunc != nil {
				// Use output function for consistency
				sf.outputFunc(color.New(color.FgCyan).Sprint("✨ Streaming response...") + "\n")
			} else {
				color.New(color.FgCyan).Println("✨ Streaming response...")
			}
			sf.println("") // Add blank line for spacing
			sf.outputMutex.Unlock()
		}
		// IMPORTANT: Update state to reflect that we just output newlines
		sf.lastWasNewline = true
	}

	// Filter out XML-style tool calls and completion signals before adding to buffer
	filteredContent := sf.filterXMLToolCalls(content)

	// Add filtered content to buffer
	sf.buffer.WriteString(filteredContent)

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

	// 6. First content after streaming indicator - flush more aggressively for immediate feedback
	if sf.firstContent && sf.buffer.Len() > 10 {
		shouldFlush = true
		sf.firstContent = false // Only apply this once
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
			// Incomplete line - buffer it for proper formatting
			sf.lineBuffer.WriteString(line)
			sf.lastWasNewline = false
		}
	}

	sf.lastUpdate = time.Now()
}

// outputLine outputs a complete line with formatting
func (sf *StreamingFormatter) outputLine(line string) {
	// Combine any buffered content with the current line
	if sf.lineBuffer.Len() > 0 {
		line = sf.lineBuffer.String() + line
		sf.lineBuffer.Reset()
	}

	// Store the original line in buffer before formatting
	if sf.consoleBuffer != nil {
		sf.consoleBuffer.AddContent(line + "\n")
	}

	// Apply formatting based on content type
	trimmed := strings.TrimSpace(line)

	// Handle lines starting with bullet character (•) - some LLMs use this instead of markdown
	if strings.HasPrefix(trimmed, "•") {
		// Convert to standard markdown bullet for consistent formatting
		bulletText := strings.TrimSpace(strings.TrimPrefix(trimmed, "•"))
		sf.print(contentPadding + "  ")
		sf.print(color.New(color.FgHiBlack).Sprint("• "))
		// Apply inline formatting to the bullet text
		formattedText := sf.applyInlineFormatting(bulletText)
		sf.println(formattedText)
		sf.lastWasNewline = true
		sf.inListContext = true
	} else if strings.HasPrefix(trimmed, "#") {
		// Check if this is a markdown header
		// Add visual separation for headers
		if !sf.lastWasNewline {
			sf.println("")
		}

		// Style headers with color using consistent output system
		if strings.HasPrefix(trimmed, "# ") {
			// Main header - bold blue
			sf.printlnColored(color.New(color.FgBlue, color.Bold).Sprint, sf.addPadding(line))
		} else if strings.HasPrefix(trimmed, "## ") {
			// Sub header - bright blue with bold for better visibility
			sf.printlnColored(color.New(color.FgHiBlue, color.Bold).Sprint, sf.addPadding(line))
		} else if strings.HasPrefix(trimmed, "### ") {
			// Level 3 headers - cyan
			sf.printlnColored(color.New(color.FgCyan).Sprint, sf.addPadding(line))
		} else if strings.HasPrefix(trimmed, "#### ") {
			// Level 4 headers - cyan (same as level 3)
			sf.printlnColored(color.New(color.FgCyan).Sprint, sf.addPadding(line))
		} else {
			// Other headers - normal with emphasis
			sf.printlnColored(color.New(color.Bold).Sprint, sf.addPadding(line))
		}

		// Add spacing after headers for better readability
		sf.lastWasNewline = true
	} else if strings.HasPrefix(trimmed, "```") {
		// Code blocks - handle language identifier
		if len(trimmed) > 3 {
			// Code fence with language
			sf.printlnColored(color.New(color.FgGreen, color.Faint).Sprint, sf.addPadding(line))
			sf.inCodeBlock = !sf.inCodeBlock
		} else {
			// Plain code fence
			sf.printlnColored(color.New(color.Faint).Sprint, sf.addPadding(line))
			sf.inCodeBlock = !sf.inCodeBlock
		}
	} else if sf.inCodeBlock {
		// Inside code block - yellow/amber color
		sf.printlnColored(color.New(color.FgYellow).Sprint, sf.addPadding(line))
	} else if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") || strings.HasPrefix(trimmed, "+ ") {
		// Bullet points - light grey bullet with formatted text
		bulletText := strings.TrimSpace(trimmed[2:])
		sf.print("  ")
		sf.print(color.New(color.FgHiBlack).Sprint("• "))
		// Apply inline formatting to the bullet text
		formattedText := sf.applyInlineFormatting(bulletText)
		sf.println(formattedText)
		sf.inListContext = true
	} else if matched, _ := regexp.MatchString(`^\d+\.`, trimmed); matched {
		// Numbered lists with formatted text
		parts := strings.SplitN(trimmed, ".", 2)
		if len(parts) == 2 {
			sf.print("  ")
			sf.print(color.New(color.FgHiBlack).Sprint(parts[0] + ". "))
			// Apply inline formatting to the list item text
			formattedText := sf.applyInlineFormatting(strings.TrimSpace(parts[1]))
			sf.println(formattedText)
		} else {
			sf.println(line)
		}
		sf.inListContext = true
	} else if strings.HasPrefix(trimmed, ">") {
		// Blockquotes - dim italic
		quotedText := strings.TrimSpace(strings.TrimPrefix(trimmed, ">"))
		sf.print("  ")
		sf.printlnColored(color.New(color.Faint, color.Italic).Sprint, "│ "+quotedText)
	} else if strings.HasPrefix(trimmed, "---") || strings.HasPrefix(trimmed, "***") || strings.HasPrefix(trimmed, "___") {
		// Horizontal rules
		if len(strings.TrimSpace(trimmed)) >= 3 {
			sf.printlnColored(color.New(color.Faint).Sprint, strings.Repeat("─", 60))
		} else {
			sf.println(line)
		}
	} else if trimmed == "" && sf.lastWasNewline {
		// Preserve paragraph breaks but not in list contexts
		if !sf.inListContext {
			sf.println("")
		}
	} else {
		// Reset list context if we hit non-list content
		if !strings.HasPrefix(trimmed, "•") &&
			!strings.HasPrefix(trimmed, "- ") &&
			!strings.HasPrefix(trimmed, "* ") &&
			!strings.HasPrefix(trimmed, "+ ") &&
			!regexp.MustCompile(`^\d+\.`).MatchString(trimmed) {
			sf.inListContext = false
		}
		// Regular line - apply inline formatting
		formatted := sf.applyInlineFormatting(line)
		sf.println(formatted)
		sf.lastWasNewline = true
	}
}

// ForceFlush immediately outputs any buffered content without finalizing
func (sf *StreamingFormatter) ForceFlush() {
	sf.mu.Lock()
	defer sf.mu.Unlock()

	// Don't flush if already finalized
	if sf.finalized {
		return
	}

	// Flush any buffered content immediately
	if sf.buffer.Len() > 0 {
		sf.flush()
	}
}

// Finalize ensures all buffered content is output
func (sf *StreamingFormatter) Finalize() {
	sf.mu.Lock()
	defer sf.mu.Unlock()

	// Don't finalize twice
	if sf.finalized {
		return
	}
	sf.finalized = true

	// Flush any remaining buffer
	if sf.buffer.Len() > 0 {
		sf.flush()
	}

	// Output any remaining line buffer with proper formatting
	if sf.lineBuffer.Len() > 0 {
		if sf.outputMutex != nil {
			sf.outputMutex.Lock()
			defer sf.outputMutex.Unlock()
		}
		// Apply formatting to the final line
		sf.outputLine(sf.lineBuffer.String())
		sf.lineBuffer.Reset()
	}

	// Only add a final newline if the last output didn't end with one
	if !sf.lastWasNewline {
		if sf.outputMutex != nil {
			sf.outputMutex.Lock()
			defer sf.outputMutex.Unlock()
		}
		sf.println("")
		sf.lastWasNewline = true
	}
}

// Reset prepares the formatter for a new streaming session
func (sf *StreamingFormatter) Reset() {
	sf.mu.Lock()
	defer sf.mu.Unlock()

	sf.buffer.Reset()
	sf.lineBuffer.Reset()
	sf.isFirstChunk = true
	sf.firstContent = false
	sf.lastWasNewline = false
	sf.inCodeBlock = false
	sf.inListContext = false
	sf.lastUpdate = time.Time{}
	sf.finalized = false
}

// HasProcessedContent returns true if any content has been processed
func (sf *StreamingFormatter) HasProcessedContent() bool {
	sf.mu.Lock()
	defer sf.mu.Unlock()
	return !sf.isFirstChunk
}

// EndedWithNewline returns true if the last output ended with a newline
func (sf *StreamingFormatter) EndedWithNewline() bool {
	sf.mu.Lock()
	defer sf.mu.Unlock()
	return sf.lastWasNewline
}

// addPadding adds left padding to a line
func (sf *StreamingFormatter) addPadding(line string) string {
	return contentPadding + line
}

// applyInlineFormatting applies markdown inline formatting using ANSI codes
func (sf *StreamingFormatter) applyInlineFormatting(text string) string {
	// Handle inline code blocks `code`
	// Use cyan with background for better visibility (similar to One Dark theme)
	codePattern := regexp.MustCompile("`([^`]+)`")
	text = codePattern.ReplaceAllStringFunc(text, func(match string) string {
		code := match[1 : len(match)-1]
		// Cyan text with subtle background highlighting
		return color.New(color.FgCyan, color.BgBlack).Sprint(" " + code + " ")
	})

	// Handle bold **text** - use non-greedy matching
	// Use muted yellow color (One Dark theme) with bold
	boldPattern1 := regexp.MustCompile(`\*\*(.+?)\*\*`)
	text = boldPattern1.ReplaceAllStringFunc(text, func(match string) string {
		content := match[2 : len(match)-2]
		// Muted yellow with bold for better readability
		return color.New(color.FgYellow, color.Bold).Sprint(content)
	})

	// Handle bold __text__ - use non-greedy matching
	boldPattern2 := regexp.MustCompile(`__(.+?)__`)
	text = boldPattern2.ReplaceAllStringFunc(text, func(match string) string {
		content := match[2 : len(match)-2]
		// Same muted yellow with bold
		return color.New(color.FgYellow, color.Bold).Sprint(content)
	})

	// Handle italic *text* - use non-greedy matching but avoid matching bold
	italicPattern1 := regexp.MustCompile(`(?:^|[\s\p{P}])\*([^*]+)\*(?:[\s\p{P}]|$)`)
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
	// Only apply italics when underscores are surrounded by word characters or at word boundaries
	// This prevents underscores in filenames like "my_file.txt" from being treated as italics
	italicPattern2 := regexp.MustCompile(`(?:^|[\s\p{P}])_([^_]+)_([^\w]|$)`)
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

// filterXMLToolCalls formats XML-style tool calls for display instead of removing them
func (sf *StreamingFormatter) filterXMLToolCalls(content string) string {
	// Pattern to match XML-style function calls like:
	// <function=shell_command><parameter=command>ls</parameter></function>
	// or <function=shell_command>...<parameter=command>ls</parameter>...</tool_call>
	funcRegex := regexp.MustCompile(`<function=(\w+)>[\s\S]*?(?:</function>|</tool_call>)`)

	// Replace XML tool calls with formatted display text
	filtered := funcRegex.ReplaceAllStringFunc(content, func(match string) string {
		// Extract function name from <function=name>
		funcNameRegex := regexp.MustCompile(`<function=(\w+)>`)
		funcMatches := funcNameRegex.FindStringSubmatch(match)
		if len(funcMatches) < 2 {
			return "" // If we can't parse it, remove it
		}

		functionName := funcMatches[1]
		// Format as a simple tool execution indicator with trailing newline for readability
		return fmt.Sprintf("🔧 %s\n", functionName)
	})

	// Clean up excessive newlines that might result from the replacement
	// Replace 3+ consecutive newlines with just 2 newlines
	// This allows for proper spacing between content while preventing excessive gaps
	excessiveNewlineRegex := regexp.MustCompile(`\n{3,}`)
	filtered = excessiveNewlineRegex.ReplaceAllString(filtered, "\n\n")

	// Also filter out task completion signals that should not be displayed
	completionSignals := []string{
		"[[TASK_COMPLETE]]",
		"[[TASKCOMPLETE]]",
		"[[TASK COMPLETE]]",
		"[[task_complete]]",
		"[[taskcomplete]]",
		"[[task complete]]",
	}

	for _, signal := range completionSignals {
		if strings.Contains(filtered, signal) {
			// Debug: completion signal detected and filtered
			filtered = strings.ReplaceAll(filtered, signal, "")
		}
	}

	return filtered
}
