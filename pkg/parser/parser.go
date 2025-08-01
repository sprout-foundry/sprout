package parser

import (
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

func isPartialContentMarker(line string) bool {
	// A line that contains `...` on on the same line as the text below. Splitting it into two lines here to makes sure that this comment doesn't trigger this detection.
	// then `unchanged` is a partial content marker.
	// This is case-insensitive for "unchanged".
	lowerLine := strings.ToLower(strings.TrimSpace(line))
	if idx := strings.Index(lowerLine, "..."); idx != -1 {
		// check for "unchanged" in the rest of the string
		return strings.Contains(lowerLine[idx:], "unchanged")
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
	updatedCode := make(map[string]string)
	var currentFileContent strings.Builder
	var currentFileName string
	var currentLanguage string // New variable to store the language of the current block
	inCodeBlock := false

	lines := strings.Split(response, "\n")

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
				updatedCode[currentFileName] = strings.TrimSuffix(currentFileContent.String(), "\n")
				currentFileName = ""
				currentLanguage = "" // Reset language after block ends
			}
		} else if inCodeBlock {
			currentFileContent.WriteString(line + "\n")
		}
	}

	return updatedCode, nil
}
