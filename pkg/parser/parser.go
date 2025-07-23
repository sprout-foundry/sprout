package parser

import (
	"regexp"
	"strings"
)

var (
	// startOfBlockRegex matches the beginning of a code block, e.g., ``` or ```go.
	startOfBlockRegex = regexp.MustCompile("^\\s*[>|]*```") // Fixed with escaped backslashes
	endOfBlockString  = "```END"
	// filenameRegex is removed in favor of string splitting in extractFilename.
)

func isStartOfCodeBlock(line string) bool {
	// A line that is an end of block is not a start of block.
	if isEndOfCodeBlock(line) {
		return false
	}
	return startOfBlockRegex.MatchString(line)
}

func isEndOfCodeBlock(line string) bool {
	return strings.TrimSpace(line) == endOfBlockString
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
	inCodeBlock := false

	lines := strings.Split(response, "\n")

	for i := 0; i < len(lines); i++ {
		line := lines[i]

		if !inCodeBlock && isStartOfCodeBlock(line) {
			filename := extractFilename(line)
			if validateFilename(filename) {
				inCodeBlock = true
				currentFileName = filename
				currentFileContent.Reset()
				continue
			}

			// If no valid filename on the start line, check the next line
			if i+1 < len(lines) {
				filenameOnNextLine := extractFilename(lines[i+1])
				if validateFilename(filenameOnNextLine) {
					inCodeBlock = true
					currentFileName = filenameOnNextLine
					currentFileContent.Reset()
					i++ // Consume the filename line
					continue
				}
			}
			// If it's a start block without a valid filename on the same or next line, we ignore it.
		} else if isEndOfCodeBlock(line) {
			if inCodeBlock {
				inCodeBlock = false
				if currentFileName != "" {
					updatedCode[currentFileName] = strings.TrimSuffix(currentFileContent.String(), "\n")
					currentFileName = ""
				}
			}
		} else if inCodeBlock {
			currentFileContent.WriteString(line + "\n")
		}
	}

	return updatedCode, nil
}
