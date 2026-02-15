package commands

import (
	"errors"
	"strings"
)

// Commit status constants
const (
	CommitStatusSuccess = "success"
	CommitStatusError   = "error"
	CommitStatusDryRun  = "dry-run"
)

// CommitCommand implements the /commit slash command
type CommitCommand struct {
	skipPrompt       bool
	dryRun           bool
	allowSecrets     bool
	agentError       error  // Store agent creation error for better error reporting
	review           string // Store the commit review result
	userInstructions string // User-provided instructions for the commit message
}

// CommitJSONResult is the JSON output structure for commit command
type CommitJSONResult struct {
	Status  string `json:"status"`            // success, error, dry-run
	Commit  string `json:"commit,omitempty"`  // commit hash if successful
	Message string `json:"message,omitempty"` // commit message
	Branch  string `json:"branch,omitempty"`  // branch name
	Error   string `json:"error,omitempty"`   // error message
	Review  string `json:"review,omitempty"`  // commit review (critical concerns)
}

// Validate checks that required fields are populated
func (r *CommitJSONResult) Validate() error {
	if r.Status == "" {
		return errors.New("status field is required")
	}
	switch r.Status {
	case CommitStatusSuccess:
		if r.Commit == "" {
			return errors.New("commit hash required for success status")
		}
	case CommitStatusError:
		if r.Error == "" {
			return errors.New("error message required for error status")
		}
	case CommitStatusDryRun:
		// Only status required for dry-run
	}
	return nil
}

// wrapText wraps text to a specific line length
func wrapText(text string, lineLength int) string {
	if text == "" {
		return ""
	}

	paragraphs := strings.Split(text, "\n\n")
	var wrappedParagraphs []string

	for _, paragraph := range paragraphs {
		// Skip empty paragraphs
		if strings.TrimSpace(paragraph) == "" {
			wrappedParagraphs = append(wrappedParagraphs, "")
			continue
		}

		words := strings.Fields(paragraph)
		if len(words) == 0 {
			wrappedParagraphs = append(wrappedParagraphs, "")
			continue
		}

		var lines []string
		currentLine := words[0]

		for i := 1; i < len(words); i++ {
			word := words[i]
			if len(currentLine)+1+len(word) <= lineLength {
				currentLine += " " + word
			} else {
				lines = append(lines, currentLine)
				currentLine = word
			}
		}

		if currentLine != "" {
			lines = append(lines, currentLine)
		}

		wrappedParagraphs = append(wrappedParagraphs, strings.Join(lines, "\n"))
	}

	return strings.Join(wrappedParagraphs, "\n\n")
}
