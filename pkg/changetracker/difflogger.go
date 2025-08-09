package changetracker

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"

	"github.com/sergi/go-diff/diffmatchpatch"
)

// Color constants for better readability
const (
	RedColor             = "\x1b[31m"
	GreenColor           = "\x1b[32m"
	YellowColor          = "\x1b[33m"
	BoldStyle            = "\x1b[1m"
	ResetColor           = "\x1b[0m"
	NumberOfContextLines = 3 // Number of context lines to show around changes
)

var (
	pythonExecutor  string
	pythonAvailable bool
	checkPythonOnce sync.Once
)

const pythonDiffScript = `
import sys
import difflib
import io

# Color constants
RedColor = "\x1b[31m"
GreenColor = "\x1b[32m"
ResetColor = "\x1b[0m"

def get_diff_pretty_text(text1, text2):
    diff = difflib.unified_diff(text1.splitlines(), text2.splitlines(), lineterm='')
    colored_diff = ""
    for line in diff:
        if line.startswith('-'):
            colored_diff += RedColor + line + ResetColor + "\n"
        elif line.startswith('+'):
            colored_diff += GreenColor + line + ResetColor + "\n"
        else:
            colored_diff += line + "\n"
    return colored_diff

if __name__ == "__main__":
    if len(sys.argv) != 3:
        sys.exit("Usage: python_script original_file new_file")

    original_file_path = sys.argv[1]
    new_file_path = sys.argv[2]

    try:
        with io.open(original_file_path, 'r', encoding='utf-8') as f:
            original_code = f.read()
        with io.open(new_file_path, 'r', encoding='utf-8') as f:
            new_code = f.read()
    except Exception as e:
        sys.stderr.write("Error reading files: {}\\n".format(e))
        sys.exit(1)

    diff_text = get_diff_pretty_text(original_code, new_code)
    if sys.version_info < (3, 0):
        # Python 2: encode to utf-8
        sys.stdout.write(diff_text.encode('utf-8'))
    else:
        # Python 3: stdout is likely utf-8 by default, but writing directly is fine
        sys.stdout.write(diff_text)
`

func checkPython() {
	checkPythonOnce.Do(func() {
		// Try python3 first
		cmd := exec.Command("python3", "-c", "import difflib")
		if err := cmd.Run(); err == nil {
			pythonExecutor = "python3"
			pythonAvailable = true
			return
		}
		// Fallback to python
		cmd = exec.Command("python", "-c", "import difflib")
		if err := cmd.Run(); err == nil {
			pythonExecutor = "python"
			pythonAvailable = true
		}
	})
}

func getDiffWithPython(originalCode, newCode string) (string, error) {
	// Create temp file for python script
	scriptFile, err := os.CreateTemp("", "diff_script_*.py")
	if err != nil {
		return "", fmt.Errorf("failed to create temp script file: %w", err)
	}
	defer os.Remove(scriptFile.Name())
	if _, err := scriptFile.WriteString(pythonDiffScript); err != nil {
		scriptFile.Close()
		return "", fmt.Errorf("failed to write to temp script file: %w", err)
	}
	if err := scriptFile.Close(); err != nil {
		return "", fmt.Errorf("failed to close temp script file: %w", err)
	}

	// Create temp files for original and new code
	originalFile, err := os.CreateTemp("", "original_*.txt")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file for original code: %w", err)
	}
	defer os.Remove(originalFile.Name())
	if _, err := originalFile.WriteString(originalCode); err != nil {
		originalFile.Close()
		return "", fmt.Errorf("failed to write original code to temp file: %w", err)
	}
	if err := originalFile.Close(); err != nil {
		return "", fmt.Errorf("failed to close temp original file: %w", err)
	}

	newFile, err := os.CreateTemp("", "new_*.txt")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file for new code: %w", err)
	}
	defer os.Remove(newFile.Name())
	if _, err := newFile.WriteString(newCode); err != nil {
		newFile.Close()
		return "", fmt.Errorf("failed to write new code to temp file: %w", err)
	}
	if err := newFile.Close(); err != nil {
		return "", fmt.Errorf("failed to close temp new file: %w", err)
	}

	cmd := exec.Command(pythonExecutor, scriptFile.Name(), originalFile.Name(), newFile.Name())
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("python script execution failed: %w\nStderr: %s", err, stderr.String())
	}

	return out.String(), nil
}

// normalizeDiffText ensures that every colored line has its own color start and end codes.
// The input `dmp.DiffPrettyText` can have color blocks spanning multiple lines,
// e.g., `\x1b[31mline1\nline2\x1b[0m`. This function transforms it into
// `\x1b[31mline1\x1b[0m\n\x1b[31mline2\x1b[0m` so that each line can be processed independently.
// This is necessary because not every colored line from the diff output has a color code.
// Until there is a reset for a specific color code, that color code is active.
// This function tracks the start and end of color blocks across lines.
func normalizeDiffText(text string) string {
	lines := strings.Split(text, "\n")
	var newLines []string
	currentColor := ""

	for _, line := range lines {
		var processedLine strings.Builder
		restOfLine := line

		// If a color is active from a previous line, apply it to the start of this line.
		if currentColor != "" {
			processedLine.WriteString(currentColor)
		}

		for len(restOfLine) > 0 {
			redIndex := strings.Index(restOfLine, RedColor)
			greenIndex := strings.Index(restOfLine, GreenColor)
			resetIndex := strings.Index(restOfLine, ResetColor)

			// Find the earliest color code.
			firstIndex := -1
			var firstColor string

			if redIndex != -1 {
				firstIndex = redIndex
				firstColor = RedColor
			}
			if greenIndex != -1 && (firstIndex == -1 || greenIndex < firstIndex) {
				firstIndex = greenIndex
				firstColor = GreenColor
			}
			if resetIndex != -1 && (firstIndex == -1 || resetIndex < firstIndex) {
				firstIndex = resetIndex
				firstColor = ResetColor
			}

			if firstIndex == -1 {
				// No more color codes on this line.
				processedLine.WriteString(restOfLine)
				break
			}

			// Append text before the color code.
			processedLine.WriteString(restOfLine[:firstIndex])
			// Append the color code itself.
			processedLine.WriteString(firstColor)

			// Update current color state.
			if firstColor == ResetColor {
				currentColor = ""
			} else {
				currentColor = firstColor
			}

			// Continue processing the rest of the line.
			restOfLine = restOfLine[firstIndex+len(firstColor):]
		}

		// If a color is still active at the end of the line,
		// reset it for this line and we'll re-apply it on the next.
		if currentColor != "" {
			processedLine.WriteString(ResetColor)
		}
		newLines = append(newLines, processedLine.String())
	}

	return strings.Join(newLines, "\n")
}

func GetDiff(filename, originalCode, newCode string) string {
	checkPython()

	var result strings.Builder
	var fullPrettyText string
	var diffs []diffmatchpatch.Diff

	dmp := diffmatchpatch.New()

	if pythonAvailable {
		pyDiff, err := getDiffWithPython(originalCode, newCode)
		if err == nil {
			fullPrettyText = pyDiff
			diffs = dmp.DiffMain(originalCode, newCode, true)
			result.WriteString(getStatsFromDiff(diffs, filename))
			result.WriteString(fullPrettyText)
			return result.String()
		} else {
			// Fallback to Go implementation if python fails
			diffs = dmp.DiffMain(originalCode, newCode, true)
			diffs = dmp.DiffCleanupSemantic(diffs)
			fullPrettyText = dmp.DiffPrettyText(diffs)
		}
	} else {
		// Original Go implementation
		diffs = dmp.DiffMain(originalCode, newCode, true)
		diffs = dmp.DiffCleanupSemantic(diffs)
		fullPrettyText = dmp.DiffPrettyText(diffs)
	}

	// Normalize the diff text to ensure each colored line has color codes.
	normalized := normalizeDiffText(fullPrettyText)
	lines := strings.Split(normalized, "\n")

	// Calculate additions and deletions
	result.WriteString(getStatsFromDiff(diffs, filename))

	inChangeBlock := false
	for i, line := range lines {
		if !containsColorChange(line) {
			if inChangeBlock {
				// Print one line of context after a change block.
				result.WriteString(fmt.Sprintf("  %s\n", line))
			}
			inChangeBlock = false
			continue
		}

		// This is a line with changes.
		// Print context line before the change block.
		if !inChangeBlock && i > 0 {
			result.WriteString(fmt.Sprintf("  %s\n", lines[i-1]))
		}

		// The "before" state is the line with additions removed.
		beforeLine := removeColoredPart(line, GreenColor, ResetColor)
		beforeLine = stripAllColor(beforeLine)

		// The "after" state is the line with deletions removed.
		afterLine := removeColoredPart(line, RedColor, ResetColor)
		afterLine = stripAllColor(afterLine)

		// Only print lines if they represent a change.
		if beforeLine != afterLine {
			if containsDeletionColor(line) {
				result.WriteString(fmt.Sprintf("%s- %s%s\n", RedColor, beforeLine, ResetColor))
			}
			if containsAdditionColor(line) {
				result.WriteString(fmt.Sprintf("%s+ %s%s\n", GreenColor, afterLine, ResetColor))
			}
		} else {
			// This can happen if a line only contains color codes but no text change.
			// Print it as context.
			result.WriteString(fmt.Sprintf("  %s\n", stripAllColor(line)))
		}
		inChangeBlock = true
	}

	return result.String()
}

func PrintDiff(filename, originalCode, newCode string) {
	diff := GetDiff(filename, originalCode, newCode)
	if diff == "" {
		fmt.Print("No changes detected.")
	}
	fmt.Print(diff)
}

func getStatsFromDiff(diffs []diffmatchpatch.Diff, filename string) string {
	var result strings.Builder
	// Calculate additions and deletion
	additions, deletions := calculateChanges(diffs)
	result.WriteString(fmt.Sprintf("%s%s%s%s ", BoldStyle, YellowColor, filename, ResetColor))
	// Add Change note characters note at the
	if additions > 0 {
		result.WriteString(fmt.Sprintf("%s%s+++%d%s ", BoldStyle, GreenColor, additions, ResetColor))
	}
	if deletions > 0 {
		result.WriteString(fmt.Sprintf("%s%s---%d%s", BoldStyle, RedColor, deletions, ResetColor))
	}
	result.WriteString("\n")
	return result.String()
}

// removeColoredPart removes the text between the specified start and end color codes.
func removeColoredPart(line, startColor, endColor string) string {
	re := regexp.MustCompile(regexp.QuoteMeta(startColor) + `.*?` + regexp.QuoteMeta(endColor))
	return re.ReplaceAllString(line, "")
}

// stripAllColor is a helper function to remove all ANSI color codes.
var stripColorRegex = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripAllColor(s string) string {
	return stripColorRegex.ReplaceAllString(s, "")
}

func containsDeletionColor(line string) bool {
	// RedColor for red (deletion)
	return strings.Contains(line, RedColor)
}

func containsAdditionColor(line string) bool {
	// GreenColor for green (addition)
	return strings.Contains(line, GreenColor)
}

// containsColorChange checks if the line contains any color change escape sequences
func containsColorChange(line string) bool {
	return containsAdditionColor(line) || containsDeletionColor(line)
}

// calculateChanges calculates the number of additions and deletions in the diff
func calculateChanges(diffs []diffmatchpatch.Diff) (additions, deletions int) {
	for _, diff := range diffs {
		switch diff.Type {
		case diffmatchpatch.DiffInsert:
			additions += len(diff.Text)
		case diffmatchpatch.DiffDelete:
			deletions += len(diff.Text)
		}
	}
	return
}
