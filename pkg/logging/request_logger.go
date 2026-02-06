package logging

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// LogRequestPayload saves the exact JSON payload sent to the provider.
// It writes both the canonical lastRequest.json and a timestamped diagnostic copy.
func LogRequestPayload(payload []byte, provider, model string, streaming bool) {
	dir := filepath.Join(os.Getenv("HOME"), ".ledit")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}

	lastPath := filepath.Join(dir, "lastRequest.json")
	if err := os.WriteFile(lastPath, payload, 0o644); err != nil {
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
	if err := os.WriteFile(filepath.Join(dir, filename), data, 0o644); err != nil {
		return
	}
	WriteLocalCopyRequest(filename, data)
}

// LogRequestPayloadOnError saves the JSON payload only when an error occurs.
// It writes to lastRequest.json and a timestamped file with error context.
func LogRequestPayloadOnError(payload []byte, provider, model string, streaming bool, errorType string, err error) {
	dir := filepath.Join(os.Getenv("HOME"), ".ledit")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}

	// Always update lastRequest.json for easy debugging
	lastPath := filepath.Join(dir, "lastRequest.json")
	if err := os.WriteFile(lastPath, payload, 0o644); err != nil {
		return
	}
	WriteLocalCopyRequest("lastRequest.json", payload)

	entry := map[string]interface{}{
		"timestamp":  time.Now().Format(time.RFC3339Nano),
		"provider":   provider,
		"model":      model,
		"streaming":  streaming,
		"error_type": errorType,
		"error":      err.Error(),
		"request":    json.RawMessage(payload),
	}

	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return
	}

	filename := fmt.Sprintf("error_request_%s_%s.json", errorType, time.Now().Format("20060102_150405.000000000"))
	if err := os.WriteFile(filepath.Join(dir, filename), data, 0o644); err != nil {
		return
	}
	WriteLocalCopyRequest(filename, data)
}

// WriteLocalCopy optionally mirrors log files to the current working directory.
func WriteLocalCopyRequest(filename string, data []byte) {
	if os.Getenv("LEDIT_COPY_LOGS_TO_CWD") != "1" {
		return
	}
	wd, err := os.Getwd()
	if err != nil {
		return
	}
	path := filepath.Join(wd, filename)
	_ = os.WriteFile(path, data, 0o644)
}
