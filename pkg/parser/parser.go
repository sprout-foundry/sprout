package parser

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	// startOfBlockRegex matches the beginning of a code block, e.g., ``` or ```go.
	// It now captures the language identifier (if present) in the first submatch.
	startOfBlockRegex    = regexp.MustCompile("^\\s*[>|]*```(\\S*)")
	hardEndOfBlockString = "```END" // Explicit end marker
)

// isHardEndOfCodeBlock checks if a line is the explicit "```END" marker.
func isHardEndOfCodeBlock(line string) bool {
	return strings.TrimSpace(line) == hardEndOfBlockString
}

// isStartOfCodeBlock checks if a line marks the beginning of a code block.
// It also returns the detected language (e.g., "go", "python", or empty string if none specified).
func isStartOfCodeBlock(line string) (bool, string) {
	// A line that is a hard end of block is never a start of block.
	if isHardEndOfCodeBlock(line) {
		return false, ""
	}
	matches := startOfBlockRegex.FindStringSubmatch(line)
	if len(matches) > 0 {
		// matches[0] is the full match, matches[1] is the captured language
		return true, strings.ToLower(matches[1]) // Return the captured language
	}
	return false, ""
}

// isEndOfCodeBlock checks if a line marks the end of a code block.
// It considers both the explicit "```END" marker and the "```" fallback,
// with the "```" fallback not applying to markdown blocks.
func isEndOfCodeBlock(line string, currentLanguage string) bool {
	if isHardEndOfCodeBlock(line) {
		return true
	}
	// Fallback for 3 backticks, but not for markdown blocks
	if strings.TrimSpace(line) == "```" {
		return currentLanguage != "markdown" && currentLanguage != "md"
	}
	return false
}

// IsPartialContentMarker checks if a line is a partial content marker.
// Common patterns that indicate partial content:
// - "...unchanged..." or "// ...unchanged..."
// - "... rest of file ..." or "// rest of file"
// - "... existing code ..." or "// existing code"
// - "... (content unchanged) ..."
// - "// ... other methods unchanged ..."
// This is case-insensitive for better detection.
func IsPartialContentMarker(line string) bool {
	lowerLine := strings.ToLower(strings.TrimSpace(line))

	// Check for ellipsis patterns with common partial content indicators
	partialIndicators := []string{
		"unchanged", "rest of file", "existing code", "content unchanged",
		"other methods", "other functions", "remaining code", "previous code",
		"same as before", "no changes", "keep existing", "rest unchanged",
		"other imports", "existing imports", "previous imports",
	}

	// Look for ellipsis (...) followed by any of the partial indicators
	if strings.Contains(lowerLine, "...") {
		for _, indicator := range partialIndicators {
			if strings.Contains(lowerLine, indicator) {
				return true
			}
		}
	}

	// Also check for comment patterns like "// rest of file" without ellipsis
	if strings.HasPrefix(lowerLine, "//") || strings.HasPrefix(lowerLine, "#") {
		for _, indicator := range partialIndicators {
			if strings.Contains(lowerLine, indicator) {
				return true
			}
		}
	}

	return false
}

// IsPartialResponse checks if the code contains partial response markers
func IsPartialResponse(code string) bool {
	lines := strings.Split(code, "\n")
	for _, line := range lines {
		if IsPartialContentMarker(line) {
			return true
		}
	}
	return false
}

// MergePartialEdit merges partial file content with the original file
// Returns the full file content with the partial edit applied
func MergePartialEdit(originalContent, partialContent string, startLine, endLine int) (string, error) {
	if startLine <= 0 || endLine <= 0 {
		return partialContent, nil // If no range specified, return as-is
	}

	originalLines := strings.Split(originalContent, "\n")
	partialLines := strings.Split(partialContent, "\n")
	totalOriginalLines := len(originalLines)

	// Validate range
	if startLine > totalOriginalLines+1 {
		return "", fmt.Errorf("start line %d exceeds original file length %d", startLine, totalOriginalLines)
	}
	if endLine > totalOriginalLines+1 {
		endLine = totalOriginalLines + 1 // Allow appending to end
	}

	var result []string

	// Add lines before the edit range
	if startLine > 1 {
		result = append(result, originalLines[:startLine-1]...)
	}

	// Add the partial content
	result = append(result, partialLines...)

	// Add lines after the edit range
	if endLine <= totalOriginalLines {
		result = append(result, originalLines[endLine:]...)
	}

	return strings.Join(result, "\n"), nil
}

// ExtractPartialEditInfo extracts line range information from file content comments
// Looks for markers like "// Editing lines 10-20" or "# Lines 5-15 only"
func ExtractPartialEditInfo(content string) (startLine, endLine int, hasRange bool) {
	lines := strings.Split(content, "\n")

	// Check first few lines for range indicators
	for i := 0; i < len(lines) && i < 5; i++ {
		line := strings.TrimSpace(lines[i])

		// Look for patterns like "Lines 10-20", "Editing lines 5-15", etc.
		patterns := []string{
			`(?i)lines?\s+(\d+)[-–](\d+)`,
			`(?i)editing\s+lines?\s+(\d+)[-–](\d+)`,
			`(?i)range\s+(\d+)[-–](\d+)`,
		}

		for _, pattern := range patterns {
			re := regexp.MustCompile(pattern)
			matches := re.FindStringSubmatch(line)
			if len(matches) >= 3 {
				if start, err := strconv.Atoi(matches[1]); err == nil {
					if end, err := strconv.Atoi(matches[2]); err == nil {
						return start, end, true
					}
				}
			}
		}
	}

	return 0, 0, false
}

func extractFilename(line string) string {
	parts := strings.Split(line, "#")
	if len(parts) < 2 {
		return ""
	}
	// The filename is the first word after the last '#'
	potentialFilename := strings.TrimSpace(parts[len(parts)-1])
	if potentialFilename == "" {
		return ""
	}
	// Take the first component, in case there are comments after filename
	return strings.Fields(potentialFilename)[0]
}

func validateFilename(filename string) bool {
	if filename == "" {
		return false
	}
	parts := strings.Split(strings.Trim(filename, "."), ".")
	return len(parts) > 1 && parts[0] != ""
}

func GetUpdatedCodeFromResponse(response string) (map[string]string, error) {
	fmt.Printf("=== Parser Debug ===\n")
	fmt.Printf("Response length: %d characters\n", len(response))

	updatedCode := make(map[string]string)
	var currentFileContent strings.Builder
	var currentFileName string
	var currentLanguage string // New variable to store the language of the current block
	inCodeBlock := false

	lines := strings.Split(response, "\n")
	fmt.Printf("Split into %d lines\n", len(lines))

	for i := 0; i < len(lines); i++ {
		line := lines[i]

		isStart, lang := isStartOfCodeBlock(line)
		if !inCodeBlock && isStart {
			filename := extractFilename(line)
			if validateFilename(filename) {
				inCodeBlock = true
				currentFileName = filename
				currentLanguage = lang // Store the detected language
				currentFileContent.Reset()
				continue
			}

			// If no valid filename on the start line, check the next line
			// This handles cases like:
			// ```go
			// # myfile.go
			// ...
			if i+1 < len(lines) {
				filenameOnNextLine := extractFilename(lines[i+1])
				if validateFilename(filenameOnNextLine) {
					inCodeBlock = true
					currentFileName = filenameOnNextLine
					currentLanguage = lang // Store the detected language from the first line
					currentFileContent.Reset()
					i++ // Consume the filename line
					continue
				}
			}
			// If it's a start block without a valid filename on the same or next line, we ignore it.
		} else if inCodeBlock && isEndOfCodeBlock(line, currentLanguage) { // Pass currentLanguage to the check
			inCodeBlock = false
			if currentFileName != "" {
				fileContent := strings.TrimSuffix(currentFileContent.String(), "\n")

				// Check for partial content markers in the file
				if IsPartialResponse(fileContent) {
					fmt.Printf("⚠️  WARNING: Detected partial content in file %s\n", currentFileName)
					fmt.Printf("File contains partial content markers that indicate incomplete code.\n")
					fmt.Printf("This may cause issues when applying changes.\n")
				}

				updatedCode[currentFileName] = fileContent
				currentFileName = ""
				currentLanguage = "" // Reset language after block ends
			}
		} else if inCodeBlock {
			currentFileContent.WriteString(line + "\n")
		}
	}

	fmt.Printf("Found %d code blocks:\n", len(updatedCode))
	for filename, content := range updatedCode {
		fmt.Printf("  - %s (%d chars)\n", filename, len(content))
		// Check if any file seems suspiciously short (possible truncation)
		if len(content) < 50 {
			fmt.Printf("⚠️  WARNING: File %s is very short (%d chars) - possible truncation\n", filename, len(content))
		}
	}
	fmt.Printf("=== End Parser Debug ===\n")

	return updatedCode, nil
}

// ExtractCodeFromResponse extracts a single code block from an LLM response for a specific language
// This is used for partial editing where we expect just one updated section
func ExtractCodeFromResponse(response, expectedLanguage string) (string, error) {
	lines := strings.Split(response, "\n")
	var codeContent strings.Builder
	inCodeBlock := false
	var currentLanguage string

	for _, line := range lines {
		isStart, lang := isStartOfCodeBlock(line)

		if !inCodeBlock && isStart {
			// Accept the block if it matches the expected language or if no language is specified
			if expectedLanguage == "" || lang == expectedLanguage || lang == "" {
				inCodeBlock = true
				currentLanguage = lang
				continue
			}
		} else if inCodeBlock && isEndOfCodeBlock(line, currentLanguage) {
			// Found the end of the code block
			break
		} else if inCodeBlock {
			// Add this line to the code content
			if codeContent.Len() > 0 {
				codeContent.WriteString("\n")
			}
			codeContent.WriteString(line)
		}
	}

	code := codeContent.String()
	if code == "" {
		return "", fmt.Errorf("no code block found for language %s", expectedLanguage)
	}

	return code, nil
}
