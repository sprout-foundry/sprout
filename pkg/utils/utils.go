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

	"github.com/alantheprice/ledit/pkg/filesystem"
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

// sanitizeTimestamp converts a timestamp string into a filename-safe format.
func sanitizeTimestamp(timestamp string) string {
	return strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(timestamp, " ", "_"), ":", "-"), ".", "")
}

// CreateBackup creates a timestamped backup of a file.
// It reads the content of the file at filePath, and saves it to a backup directory
// (.ledit/backups) with a timestamped filename.
func CreateBackup(filePath string) error {
	// Read the original file content
	content, err := filesystem.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			GetLogger(true).Log(fmt.Sprintf("File '%s' does not exist, no backup created.", filePath))
			return nil // No error, as there's nothing to back up
		}
		return fmt.Errorf("failed to read file '%s' for backup: %w", filePath, err)
	}

	backupDir := ".ledit/backups"
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return fmt.Errorf("failed to create backup directory '%s': %w", backupDir, err)
	}

	// Get base filename and sanitize timestamp
	baseFilename := filepath.Base(filePath)
	timestamp := sanitizeTimestamp(GetTimestamp())

	// Construct backup filename
	backupFilename := fmt.Sprintf("%s_%s.bak", baseFilename, timestamp)
	backupPath := filepath.Join(backupDir, backupFilename)

	// Save the content to the backup file
	if err := filesystem.SaveFile(backupPath, content); err != nil {
		return fmt.Errorf("failed to save backup file '%s': %w", backupPath, err)
	}

	GetLogger(true).Log(fmt.Sprintf("Created backup of '%s' at '%s'", filePath, backupPath))
	return nil
}

// LogUserPrompt logs the user's original prompt to a file in the .ledit/prompts directory.
func LogUserPrompt(prompt string) {
	logDir := ".ledit/prompts"
	if err := os.MkdirAll(logDir, 0755); err != nil {
		GetLogger(true).LogError(fmt.Errorf("failed to create prompt log directory: %w", err))
		return
	}

	timestamp := sanitizeTimestamp(GetTimestamp())
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

	timestamp := sanitizeTimestamp(GetTimestamp())
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

// FormatFileSize converts a file size in bytes to a human-readable string (e.g., "1.2 MB", "345 KB").
func FormatFileSize(size int64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
		TB = 1024 * GB
	)

	switch {
	case size < KB:
		return fmt.Sprintf("%d B", size)
	case size < MB:
		return fmt.Sprintf("%.1f KB", float64(size)/KB)
	case size < GB:
		return fmt.Sprintf("%.1f MB", float64(size)/MB)
	case size < TB:
		return fmt.Sprintf("%.1f GB", float64(size)/GB)
	default:
		return fmt.Sprintf("%.1f TB", float64(size)/TB)
	}
}

// TruncateString truncates a string to a specified maximum length,
// appending "..." if truncation occurs.
func TruncateString(s string, maxLength int) string {
	if maxLength < 0 {
		return ""
	}
	if len(s) <= maxLength {
		return s
	}
	if maxLength <= 3 {
		return s[:maxLength]
	}
	return s[:maxLength-3] + "..."
}

// ExtractJSONFromLLMResponse extracts JSON from an LLM response that may contain markdown formatting
// This is a centralized utility to handle the common issue of LLMs wrapping JSON in code blocks
func ExtractJSONFromLLMResponse(response string) (string, error) {
	// First try to extract from markdown code blocks
	if strings.Contains(response, "```json") {
		parts := strings.Split(response, "```json")
		if len(parts) > 1 {
			jsonPart := parts[1]
			end := strings.Index(jsonPart, "```")
			if end > 0 {
				jsonStr := strings.TrimSpace(jsonPart[:end])
				if jsonStr != "" {
					return jsonStr, nil
				}
			}
		}
	}

	// Try to find JSON object boundaries if no markdown blocks
	response = strings.TrimSpace(response)

	// Look for first opening brace or bracket
	startBrace := strings.Index(response, "{")
	startBracket := strings.Index(response, "[")

	var start int = -1
	var isArray bool = false

	if startBrace >= 0 && (startBracket < 0 || startBrace < startBracket) {
		start = startBrace
		isArray = false
	} else if startBracket >= 0 {
		start = startBracket
		isArray = true
	}

	if start == -1 {
		return "", fmt.Errorf("no JSON object or array found (no opening brace or bracket)")
	}

	// Look for matching closing brace/bracket from the end
	var end int = -1
	if isArray {
		end = strings.LastIndex(response, "]")
	} else {
		end = strings.LastIndex(response, "}")
	}

	if end == -1 || end <= start {
		return "", fmt.Errorf("no matching closing brace/bracket found")
	}

	// Extract the JSON substring
	jsonStr := strings.TrimSpace(response[start : end+1])

	// Validate it's not empty
	if jsonStr == "" {
		return "", fmt.Errorf("extracted JSON is empty")
	}

	return jsonStr, nil
}
