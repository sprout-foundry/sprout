package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const defaultUserInputMaxChars = 2000
const defaultUserInputArchiveDir = "/tmp/ledit/inputs"

func (ch *ConversationHandler) prepareUserInputForModel(input string) string {
	maxChars := defaultUserInputMaxChars
	if raw := strings.TrimSpace(os.Getenv("LEDIT_USER_INPUT_MAX_CHARS")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			maxChars = parsed
		}
	}

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

	notice := buildUserInputTruncationNotice(omitted, path, saveErr)
	return input[:headLen] + notice + input[len(input)-tailLen:]
}

func buildUserInputTruncationNotice(omitted int, archivePath string, archiveErr error) string {
	if archivePath == "" {
		if archiveErr != nil {
			return fmt.Sprintf("\n\n[USER INPUT TRUNCATED FOR MODEL CONTEXT: omitted %d characters. Set LEDIT_USER_INPUT_MAX_CHARS to adjust. Failed to save full input: %v]\n\n", omitted, archiveErr)
		}
		return fmt.Sprintf("\n\n[USER INPUT TRUNCATED FOR MODEL CONTEXT: omitted %d characters. Set LEDIT_USER_INPUT_MAX_CHARS to adjust. Full input path unavailable.]\n\n", omitted)
	}
	return fmt.Sprintf("\n\n[USER INPUT TRUNCATED FOR MODEL CONTEXT: omitted %d characters. Set LEDIT_USER_INPUT_MAX_CHARS to adjust. Full input saved to %s. Use read_file on this path for the complete pasted content.]\n\n", omitted, archivePath)
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
