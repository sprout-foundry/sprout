package utils

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// GenerateRequestHash generates a SHA256 hash for a given set of instructions.
func GenerateRequestHash(instructions string) string {
	hash := sha256.Sum256([]byte(instructions))
	return hex.EncodeToString(hash[:])
}

// GenerateFileRevisionHash generates a SHA256 hash for a file based on its name and code content.
func GenerateFileRevisionHash(filename, code string) string {
	data := []byte(filename + ":" + code)
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// SaveFile saves or removes a file with the given content.
// If content is empty, the file is removed.
func SaveFile(filename, content string) error {
	if content == "" {
		if _, err := os.Stat(filename); err == nil {
			// File exists, remove it
			return os.Remove(filename)
		} else if os.IsNotExist(err) {
			// File does not exist, nothing to do
			return nil
		} else {
			// Other error checking file stat
			return fmt.Errorf("error checking file %s: %w", filename, err)
		}
	}

	// Ensure the directory exists
	dir := filepath.Dir(filename)
	if dir != "" {
		if err := os.MkdirAll(dir, os.ModePerm); err != nil {
			return fmt.Errorf("could not create directory %s: %w", dir, err)
		}
	}

	return os.WriteFile(filename, []byte(content), 0644)
}

// ReadFile reads the content of a file.
func ReadFile(filename string) (string, error) {
	content, err := os.ReadFile(filename)
	if err != nil {
		return "", fmt.Errorf("could not read file %s: %w", filename, err)
	}
	return string(content), nil
}

// GetTimestamp returns a formatted timestamp string suitable for filenames.
func GetTimestamp() string {
	return time.Now().Format("2006-01-02 15:04:05.000")
}

// LogUserPrompt logs the user's original prompt to a file in the .ledit/prompts directory.
func LogUserPrompt(prompt string) {
	logDir := ".ledit/prompts"
	if err := os.MkdirAll(logDir, 0755); err != nil {
		GetLogger(true).LogError(fmt.Errorf("failed to create prompt log directory: %w", err))
		return
	}

	timestamp := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(GetTimestamp(), " ", "_"), ":", "-"), ".", "")
	filename := filepath.Join(logDir, fmt.Sprintf("prompt_%s.txt", timestamp))

	if err := os.WriteFile(filename, []byte(prompt), 0644); err != nil {
		GetLogger(true).LogError(fmt.Errorf("failed to write user prompt to file: %w", err))
	}
}

// StringSliceEqual checks if two string slices are equal, ignoring order.
func StringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	m := make(map[string]int)
	for _, s := range a {
		m[s]++
	}
	for _, s := range b {
		m[s]--
		if m[s] < 0 {
			return false
		}
	}
	return true
}

// EstimateTokens provides a rough estimate of the number of tokens in a given text.
// This is a simple character-based estimation (e.g., 4 chars per token) and may not be accurate
// for all models or languages, but provides a general idea for prompt length management.
func EstimateTokens(text string) int {
	// A common heuristic is 4 characters per token for English text.
	// This is a rough estimate and can vary significantly by model and language.
	return len(text) / 4
}
