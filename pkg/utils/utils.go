package utils

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
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

// SaveFile function removed. It's moved to pkg/filesystem/io.go

// ReadFile function removed. It's moved to pkg/filesystem/io.go

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

// LogLLMResponse logs the LLM's response to a file in the .ledit/llm_responses directory.
func LogLLMResponse(filename, response string) {
	logDir := ".ledit/llm_responses"
	if err := os.MkdirAll(logDir, 0755); err != nil {
		GetLogger(true).LogError(fmt.Errorf("failed to create LLM response log directory: %w", err))
		return
	}

	// Sanitize filename for use in path
	sanitizedFilename := strings.ReplaceAll(filename, string(filepath.Separator), "_")
	if sanitizedFilename == "" {
		sanitizedFilename = "no_filename"
	}

	timestamp := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(GetTimestamp(), " ", "_"), ":", "-"), ".", "")
	logFilename := filepath.Join(logDir, fmt.Sprintf("response_%s_%s.txt", timestamp, sanitizedFilename))

	if err := os.WriteFile(logFilename, []byte(response), 0644); err != nil {
		GetLogger(true).LogError(fmt.Errorf("failed to write LLM response to file: %w", err))
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

// IsValidFileExtension checks if the given filename has one of the allowed extensions.
// Extensions should be provided with a leading dot, e.g., ".go", ".txt".
func IsValidFileExtension(filename string, allowedExtensions []string) bool {
	ext := filepath.Ext(filename)
	for _, allowedExt := range allowedExtensions {
		if strings.EqualFold(ext, allowedExt) {
			return true
		}
	}
	return false
}

// CapitalizeWords capitalizes the first letter of each word in a string.
func CapitalizeWords(s string) string {
	// Using golang.org/x/text/cases for robust capitalization, as strings.Title is deprecated.
	return cases.Title(language.Und, cases.NoLower).String(s)
}

// IsEmptyString checks if a string is empty.
func IsEmptyString(s string) bool {
	return s == ""
}