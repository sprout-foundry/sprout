package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// WriteDiagnosticTranscript serializes a DiagnosticCapture to a timestamped
// JSON file under dir and returns the path. It is the default convenience
// implementation for an OnDiagnosticCapture handler: wire it once at agent
// construction and every threading violation — reactive or proactive — lands
// on disk for offline analysis.
//
//	dir, _ := os.MkdirTemp("", "seed-diag-*")
//	opts.OnDiagnosticCapture = func(c core.DiagnosticCapture) {
//	    path, _ := core.WriteDiagnosticTranscript(c, dir)
//	    log.Printf("saved threading diagnostic: %s", path)
//	}
//
// The filename encodes the trigger and a UTC timestamp so captures sort
// chronologically. The parent directory is created if missing. Truncated
// tool-call/result IDs in the filename avoid leaking the full ids while still
// disambiguating concurrent captures.
func WriteDiagnosticTranscript(c DiagnosticCapture, dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create diagnostic dir: %w", err)
	}

	ts := time.Now().UTC().Format("20060102-150405.000")
	name := fmt.Sprintf("threading-%s-iter%d-%s.json", c.Trigger, c.Iteration, ts)
	path := filepath.Join(dir, name)

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal diagnostic capture: %w", err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("write diagnostic transcript: %w", err)
	}
	return path, nil
}
