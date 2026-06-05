//go:build js

package automate

import "fmt"

// SweepStaleSessions is a no-op for JS/WASM builds.
func SweepStaleSessions(sproutDir string) (int, error) {
	return 0, fmt.Errorf("stale session sweep is not supported on JS/WASM platforms")
}

// AutomateSessionInfo is a stub type for JS/WASM builds.
type AutomateSessionInfo struct {
	Workflow       string `json:"workflow"`
	PID            int    `json:"pid"`
	StartedAt      interface{} `json:"started_at"`
	OutputFilePath string `json:"output_file_path,omitempty"`
	Kind           string `json:"kind"`
}

// WriteSessionFile is a no-op for JS/WASM builds.
func WriteSessionFile(sproutDir string, sessionID string, info *AutomateSessionInfo) error {
	return fmt.Errorf("session file writing is not supported on JS/WASM platforms")
}

// ReadSessionFile returns an error for JS/WASM builds.
func ReadSessionFile(sproutDir string, sessionID string) (*AutomateSessionInfo, error) {
	return nil, fmt.Errorf("session file reading is not supported on JS/WASM platforms")
}

// ListSessionFiles returns an empty list for JS/WASM builds.
func ListSessionFiles(sproutDir string) ([]AutomateSessionInfo, error) {
	return nil, fmt.Errorf("session file listing is not supported on JS/WASM platforms")
}
