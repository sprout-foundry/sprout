package logging

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/envutil"
	"github.com/sprout-foundry/sprout/pkg/redact"
)

// LogRequestPayload saves the exact JSON payload sent to the provider.
// It writes both the canonical lastRequest.json and a timestamped diagnostic copy.
// All payloads are redacted via pkg/redact to prevent credential leakage in log files.
func LogRequestPayload(payload []byte, provider, model string, streaming bool) {
	payload = redact.Apply(payload)

	dir := filepath.Join(os.Getenv("HOME"), ".sprout")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}

	lastPath := filepath.Join(dir, "lastRequest.json")
	if err := os.WriteFile(lastPath, payload, 0600); err != nil {
		return
	}
	WriteLocalCopyRequest("lastRequest.json", payload)

	entry := map[string]interface{}{
		"timestamp": time.Now().Format(time.RFC3339Nano),
		"provider":  provider,
		"model":     model,
		"streaming": streaming,
		"request":   json.RawMessage(payload),
	}

	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return
	}

	filename := fmt.Sprintf("api_request_%s.json", time.Now().Format("20060102_150405.000000000"))
	if err := os.WriteFile(filepath.Join(dir, filename), data, 0600); err != nil {
		return
	}
	WriteLocalCopyRequest(filename, data)
}

// LogRequestPayloadOnError saves the JSON payload only when an error occurs.
// It writes to lastRequest.json and a timestamped file with error context.
// All payloads are redacted via pkg/redact to prevent credential leakage in log files.
func LogRequestPayloadOnError(payload []byte, provider, model string, streaming bool, errorType string, err error) {
	payload = redact.Apply(payload)

	dir := getErrorLogDir()
	if dir == "" {
		return
	}

	// Always update lastRequest.json for easy debugging
	lastPath := filepath.Join(dir, "lastRequest.json")
	if err := os.WriteFile(lastPath, payload, 0600); err != nil {
		return
	}
	WriteLocalCopyRequest("lastRequest.json", payload)

	entry := map[string]interface{}{
		"timestamp":       time.Now().Format(time.RFC3339Nano),
		"provider":        provider,
		"model":           model,
		"streaming":       streaming,
		"error_type":      errorType,
		"error_message":   err.Error(),
		"error":           err.Error(),
		"error_details":   formatErrorDetails(err),
		"request":         json.RawMessage(payload),
	}

	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return
	}

	filename := fmt.Sprintf("error_request_%s_%s.json", errorType, time.Now().Format("20060102_150405.000000000"))
	if err := os.WriteFile(filepath.Join(dir, filename), data, 0600); err != nil {
		return
	}
	WriteLocalCopyRequest(filename, data)

	// Rotate old error logs to prevent unbounded growth
	cleanupOldErrorLogs(dir)
}

// getErrorLogDir returns the .sprout directory used for error logs.
// Returns empty string if it cannot be created.
func getErrorLogDir() string {
	dir := filepath.Join(os.Getenv("HOME"), ".sprout")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ""
	}
	return dir
}

// formatErrorDetails returns a string with any additional error context.
func formatErrorDetails(err error) string {
	if err == nil {
		return ""
	}
	// Check for wrapped errors
	var wrapped error
	for unwrapped := err; unwrapped != nil; unwrapped = wrapped {
		if e, ok := unwrapped.(interface{ Unwrap() error }); ok {
			wrapped = e.Unwrap()
			if wrapped != nil && wrapped.Error() != unwrapped.Error() {
				return fmt.Sprintf("%s (caused by: %s)", unwrapped.Error(), wrapped.Error())
			}
			wrapped = nil
		} else {
			break
		}
	}
	return err.Error()
}

const maxErrorLogFiles = 100

// cleanupOldErrorLogs removes old error log files, keeping only the most recent maxErrorLogFiles.
// Files are sorted by name (which contains timestamps), so lexicographic sort = chronological.
func cleanupOldErrorLogs(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	// Filter to error_request_ files only
	var errorFiles []fs.DirEntry
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "error_request_") {
			errorFiles = append(errorFiles, entry)
		}
	}

	if len(errorFiles) <= maxErrorLogFiles {
		return
	}

	// Sort by name (timestamps in names make this chronological)
	sort.Slice(errorFiles, func(i, j int) bool {
		return errorFiles[i].Name() < errorFiles[j].Name()
	})

	// Delete oldest
	toDelete := len(errorFiles) - maxErrorLogFiles
	for i := 0; i < toDelete; i++ {
		os.Remove(filepath.Join(dir, errorFiles[i].Name()))
	}
}

// WriteLocalCopy optionally mirrors log files to the current working directory.
func WriteLocalCopyRequest(filename string, data []byte) {
	if envutil.GetEnvSimple("COPY_LOGS_TO_CWD") != "1" {
		return
	}
	wd, err := os.Getwd()
	if err != nil {
		return
	}
	path := filepath.Join(wd, filename)
	if err := os.WriteFile(path, data, 0600); err != nil {
		log.Printf("[debug] failed to write request log: %v", err)
	}
}
