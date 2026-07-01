//go:build !js

package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/sprout-foundry/sprout/pkg/automate"
	"github.com/sprout-foundry/sprout/pkg/console"
)

var (
	automateStatusAll  bool
	automateStatusJSON bool
)

func runAutomateStatus() error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}
	sproutDir := filepath.Join(cwd, ".sprout")

	sessions, err := readAllSessions(sproutDir)
	if err != nil {
		return err
	}

	if len(sessions) == 0 {
		if automateStatusJSON {
			fmt.Println("[]")
		} else {
			console.GlyphInfo.Printf("No automate sessions found.")
		}
		return nil
	}

	// Filter to running only unless --all
	if !automateStatusAll {
		filtered := make([]sessionEntry, 0, len(sessions))
		for _, s := range sessions {
			if automate.IsProcessAlive(s.PID) {
				filtered = append(filtered, s)
			}
		}
		sessions = filtered
	}

	if len(sessions) == 0 {
		if automateStatusJSON {
			fmt.Println("[]")
		} else {
			console.GlyphInfo.Printf("No running automate sessions.")
		}
		return nil
	}

	if automateStatusJSON {
		return printStatusJSON(sessions)
	}

	printStatusTable(sessions)
	return nil
}

type sessionEntry struct {
	SessionID string
	automate.AutomateSessionInfo
}

func readAllSessions(sproutDir string) ([]sessionEntry, error) {
	dir := filepath.Join(sproutDir, "automate")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read automate session directory: %w", err)
	}

	var sessions []sessionEntry
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
		sessionID := name[:len(name)-5] // strip ".json"
		info, err := automate.ReadSessionFile(sproutDir, sessionID)
		if err != nil {
			continue
		}
		sessions = append(sessions, sessionEntry{
			SessionID:           sessionID,
			AutomateSessionInfo: *info,
		})
	}
	return sessions, nil
}

func printStatusTable(sessions []sessionEntry) {
	fmt.Println()
	fmt.Printf("  %-30s %-25s %-10s %-8s %-10s %s\n",
		"SESSION", "WORKFLOW", "STATUS", "PID", "STARTED", "ELAPSED")
	fmt.Println()
	for _, s := range sessions {
		status := "exited"
		if automate.IsProcessAlive(s.PID) {
			status = "running"
		}
		fmt.Printf("  %-30s %-25s %-10s %-8d %-10s %s\n",
			s.SessionID,
			s.Workflow,
			status,
			s.PID,
			s.StartedAt.Format("15:04:05"),
			time.Since(s.StartedAt).Round(time.Second),
		)
	}
	fmt.Println()
}

func printStatusJSON(sessions []sessionEntry) error {
	type statusEntry struct {
		SessionID      string `json:"session_id"`
		Workflow       string `json:"workflow"`
		Status         string `json:"status"`
		PID            int    `json:"pid"`
		StartedAt      string `json:"started_at"`
		ElapsedSeconds int64  `json:"elapsed_seconds"`
	}

	entries := make([]statusEntry, 0, len(sessions))
	for _, s := range sessions {
		status := "exited"
		if automate.IsProcessAlive(s.PID) {
			status = "running"
		}
		entries = append(entries, statusEntry{
			SessionID:      s.SessionID,
			Workflow:       s.Workflow,
			Status:         status,
			PID:            s.PID,
			StartedAt:      s.StartedAt.Format(time.RFC3339),
			ElapsedSeconds: int64(time.Since(s.StartedAt).Seconds()),
		})
	}

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal status JSON: %w", err)
	}
	fmt.Println(string(data))
	return nil
}
