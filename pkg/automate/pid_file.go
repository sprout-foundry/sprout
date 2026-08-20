//go:build !js

package automate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// AutomateSessionInfo is the schema for .sprout/automate/<session_id>.json PID files.
type AutomateSessionInfo struct {
	Workflow       string    `json:"workflow"`
	PID            int       `json:"pid"`
	StartedAt      time.Time `json:"started_at"`
	OutputFilePath string    `json:"output_file_path,omitempty"`
	BudgetUSD      *float64  `json:"budget_usd,omitempty"`
	Kind           string    `json:"kind"` // always "automate"
	// EndedAt, ExitCode, and Status are set when the session's process
	// exits (FinalizeSessionFile). Empty Status means the session was
	// written by a pre-finalization build and never finalized — PID
	// liveness is the fallback for those legacy records.
	EndedAt  *time.Time `json:"ended_at,omitempty"`
	ExitCode *int       `json:"exit_code,omitempty"`
	Status   string     `json:"status,omitempty"`
}

// GetAutomateSessionDir returns the .sprout/automate/ directory path.
// It resolves the sprout directory relative to the given base (typically project root).
// Creates the directory if it doesn't exist.
func GetAutomateSessionDir(baseDir string) (string, error) {
	dir := filepath.Join(baseDir, ".sprout", "automate")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create automate session directory: %w", err)
	}
	return dir, nil
}

// WriteSessionFile writes a session info JSON to .sprout/automate/<sessionID>.json.
// Creates the directory if needed. A record with empty Status is normalized
// to "running" so consumers never see a third state.
func WriteSessionFile(sproutDir string, sessionID string, info *AutomateSessionInfo) error {
	if info != nil && info.Status == "" && info.EndedAt == nil {
		info.Status = "running"
	}
	dir := filepath.Join(sproutDir, "automate")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create automate session directory: %w", err)
	}
	path := filepath.Join(dir, sessionID+".json")
	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal session info: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write session file: %w", err)
	}
	return nil
}

// RemoveSessionFile removes the PID file for a session.
func RemoveSessionFile(sproutDir string, sessionID string) error {
	path := filepath.Join(sproutDir, "automate", sessionID+".json")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove session file: %w", err)
	}
	return nil
}

// ReadSessionFile reads and parses a single session file.
func ReadSessionFile(sproutDir string, sessionID string) (*AutomateSessionInfo, error) {
	path := filepath.Join(sproutDir, "automate", sessionID+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read session file: %w", err)
	}
	var info AutomateSessionInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, fmt.Errorf("unmarshal session file: %w", err)
	}
	return &info, nil
}

// ListSessionFiles reads all session files in .sprout/automate/ and returns them.
func ListSessionFiles(sproutDir string) ([]AutomateSessionInfo, error) {
	dir := filepath.Join(sproutDir, "automate")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // directory doesn't exist yet, return nil slice
		}
		return nil, fmt.Errorf("read automate session directory: %w", err)
	}
	var results []AutomateSessionInfo
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if filepath.Ext(name) != ".json" {
			continue
		}
		if len(name) <= 5 {
			continue // skip filenames too short to have a meaningful session ID
		}
		// Extract sessionID from filename (remove .json extension)
		sessionID := name[:len(name)-5] // len(".json") == 5
		info, err := ReadSessionFile(sproutDir, sessionID)
		if err != nil {
			continue // skip unreadable files
		}
		results = append(results, *info)
	}
	return results, nil
}

// sessionRetention is how long a dead session's record is kept after the
// process exited (or, for legacy records without an end time, after it
// started). Records persist so `sprout automate status --all` can show what
// ran and how it ended — deleting on death made post-mortems impossible
// (the OOM-killed workflow of 2026-08-19 left no trace for exactly this reason).
const sessionRetention = 7 * 24 * time.Hour

// FinalizeSessionFile records the process outcome on an existing session
// record: EndedAt, ExitCode, and a Status of "success" (exit 0) or "error".
// The PID is zeroed so IsProcessAlive never matches a recycled PID later.
func FinalizeSessionFile(sproutDir string, sessionID string, exitCode int) error {
	info, err := ReadSessionFile(sproutDir, sessionID)
	if err != nil {
		return fmt.Errorf("finalize session %s: %w", sessionID, err)
	}
	now := time.Now()
	code := exitCode
	info.EndedAt = &now
	info.ExitCode = &code
	info.PID = 0
	if exitCode == 0 {
		info.Status = "success"
	} else {
		info.Status = "error"
	}
	return WriteSessionFile(sproutDir, sessionID, info)
}

// SweepStaleSessions removes session files for dead sessions whose retention
// window has passed: finalized records older than sessionRetention after
// EndedAt, legacy unfinalized records older than sessionRetention after
// StartedAt. Live or recently-ended records are kept. Returns the number of
// removed entries. Errors from listing or reading the session directory are
// returned; errors from individual file removals are silently ignored.
func SweepStaleSessions(sproutDir string) (int, error) {
	dir := filepath.Join(sproutDir, "automate")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("read automate session directory during sweep: %w", err)
	}

	removed := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if filepath.Ext(name) != ".json" {
			continue
		}
		if len(name) <= 5 {
			continue // skip filenames too short to have a meaningful session ID
		}
		sessionID := name[:len(name)-5] // len(".json") == 5
		info, err := ReadSessionFile(sproutDir, sessionID)
		if err != nil {
			continue // skip unreadable files
		}
		if info.EndedAt != nil {
			if time.Since(*info.EndedAt) < sessionRetention {
				continue
			}
		} else if info.Status == "running" && IsProcessAlive(info.PID) {
			continue
		} else if time.Since(info.StartedAt) < sessionRetention {
			continue
		}
		if err := RemoveSessionFile(sproutDir, sessionID); err != nil {
			continue // log but don't fail
		}
		removed++
	}

	return removed, nil
}
