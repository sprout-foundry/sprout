package parser

import (
	"fmt"
	"regexp"
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
