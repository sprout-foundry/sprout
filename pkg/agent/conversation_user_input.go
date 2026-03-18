package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const defaultInteractiveInputMaxChars = 50000
const defaultAutomationInputMaxChars = 50000
const defaultUserInputArchiveDir = "/tmp/ledit/inputs"

func (ch *ConversationHandler) prepareUserInputForModel(input string) string {
	maxChars, inputType := ch.getInputLimit()
	if len(input) <= maxChars {
		return input
	}

	path, saveErr := saveUserInputArchive(input)

	headLen := maxChars * 70 / 100
	tailLen := maxChars - headLen
	if tailLen <= 0 {
		tailLen = maxChars / 2
		headLen = maxChars - tailLen
	}

	omitted := len(input) - (headLen + tailLen)
	if omitted < 0 {
		omitted = 0
	}

	notice := buildUserInputTruncationNotice(omitted, path, saveErr, inputType)
	truncatedInput := input[:headLen] + notice + input[len(input)-tailLen:]

	// Print warning to console when truncation occurs
	if path != "" {
		ch.agent.PrintLineAsync(fmt.Sprintf("⚠️  INPUT TRUNCATED: %d characters omitted from %s (limit: %d chars). Full input saved to %s\n",
			omitted, inputType, maxChars, path))
	} else {
		ch.agent.PrintLineAsync(fmt.Sprintf("⚠️  INPUT TRUNCATED: %d characters omitted from %s (limit: %d chars). Failed to save full input: %v\n",
			omitted, inputType, maxChars, saveErr))
	}

	return truncatedInput
}

func (ch *ConversationHandler) getInputLimit() (int, string) {
	// Default to automation mode if agent is not set (e.g., in tests)
	isInteractive := false
	if ch.agent != nil {
		isInteractive = ch.agent.IsInteractiveMode()
	}

	var maxChars int
	var inputType string

	if isInteractive {
		maxChars = defaultInteractiveInputMaxChars
		inputType = "interactive input"
		if raw := strings.TrimSpace(os.Getenv("LEDIT_INTERACTIVE_INPUT_MAX_CHARS")); raw != "" {
			if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
				maxChars = parsed
			}
		}
	} else {
		maxChars = defaultAutomationInputMaxChars
		inputType = "automation input"
		if raw := strings.TrimSpace(os.Getenv("LEDIT_AUTOMATION_INPUT_MAX_CHARS")); raw != "" {
			if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
				maxChars = parsed
			}
		}
	}

	// Legacy support for LEDIT_USER_INPUT_MAX_CHARS
	// This overrides mode-specific limits for backward compatibility
	if raw := strings.TrimSpace(os.Getenv("LEDIT_USER_INPUT_MAX_CHARS")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			maxChars = parsed
		}
	}

	return maxChars, inputType
}

func buildUserInputTruncationNotice(omitted int, archivePath string, archiveErr error, inputType string) string {
	var envVarHint string
	switch inputType {
	case "interactive input":
		envVarHint = "LEDIT_INTERACTIVE_INPUT_MAX_CHARS"
	case "automation input":
		envVarHint = "LEDIT_AUTOMATION_INPUT_MAX_CHARS"
	default:
		envVarHint = "LEDIT_USER_INPUT_MAX_CHARS"
	}

	if archivePath == "" {
		if archiveErr != nil {
			return fmt.Sprintf("\n\n[USER INPUT TRUNCATED FOR MODEL CONTEXT: omitted %d characters. Set %s to adjust. Failed to save full input: %v]\n\n", omitted, envVarHint, archiveErr)
		}
		return fmt.Sprintf("\n\n[USER INPUT TRUNCATED FOR MODEL CONTEXT: omitted %d characters. Set %s to adjust. Full input path unavailable.]\n\n", omitted, envVarHint)
	}
	return fmt.Sprintf("\n\n[USER INPUT TRUNCATED FOR MODEL CONTEXT: omitted %d characters. Set %s to adjust. Full input saved to %s. Use read_file on this path for the complete pasted content.]\n\n", omitted, envVarHint, archivePath)
}

func saveUserInputArchive(input string) (string, error) {
	dir := strings.TrimSpace(os.Getenv("LEDIT_USER_INPUT_ARCHIVE_DIR"))
	if dir == "" {
		dir = defaultUserInputArchiveDir
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	timestamp := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("user_input_%s_%d.txt", timestamp, time.Now().UnixNano()%1_000_000)
	path := filepath.Join(dir, filename)

	header := fmt.Sprintf("Captured-At: %s\n\n", time.Now().Format(time.RFC3339))
	if err := os.WriteFile(path, []byte(header+input), 0o644); err != nil {
		return "", err
	}
	return path, nil
}
