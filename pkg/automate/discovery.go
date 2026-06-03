// Package automate provides shared workflow discovery and validation for
// the automate/ feature used by both the CLI (cmd/automate.go) and the
// agent tool layer (pkg/agent/tool_handlers_automate.go).
package automate

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Entry represents a discovered workflow file with its metadata.
type Entry struct {
	Filename    string `json:"name"`
	FilePath    string
	Description string `json:"description,omitempty"`
}

// isValidFilenamePattern matches safe workflow filenames: alphanumeric,
// dots, underscores, and hyphens followed by .json.
var isValidFilenamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+\.json$`)

// Dir returns the default automate directory path (cwd + "/automate").
func Dir() string {
	cwd, _ := os.Getwd()
	return filepath.Join(cwd, "automate")
}

// IsValidFilename checks if a filename is safe for use as a workflow filename.
// Only allows alphanumeric characters, dots, underscores, and hyphens,
// followed by .json. Prevents shell injection via filenames.
func IsValidFilename(name string) bool {
	return isValidFilenamePattern.MatchString(name)
}

// Discover scans the given directory for valid workflow JSON files.
func Discover(dir string) ([]Entry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var workflows []Entry
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !IsValidFilename(name) {
			continue // Skip files with unsafe names
		}

		fullPath := filepath.Join(dir, name)
		desc, err := ExtractDescription(fullPath)
		if err != nil {
			continue // Not a valid workflow JSON
		}

		workflows = append(workflows, Entry{
			Filename:    name,
			FilePath:    fullPath,
			Description: desc,
		})
	}

	return workflows, nil
}

// ExtractDescription reads a workflow JSON file and returns its description field.
func ExtractDescription(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return "", err
	}

	// Must have "initial" or "steps" to be a workflow
	if _, ok := raw["initial"]; !ok {
		if _, ok := raw["steps"]; !ok {
			return "", fmt.Errorf("not a workflow config")
		}
	}

	var desc string
	if descRaw, ok := raw["description"]; ok {
		_ = json.Unmarshal(descRaw, &desc)
	}

	return desc, nil
}

// ResolvePath finds a workflow file by name, with or without .json extension,
// and verifies the resolved path stays under the given directory to prevent
// path traversal attacks.
func ResolvePath(dir string, name string) (string, error) {
	// Try exact filename match first. Normalize .json extension to
	// lowercase so case-insensitive filesystems (macOS, Windows) can't
	// bypass IsValidFilename by using .JSON variants.
	target := name
	if strings.HasSuffix(strings.ToLower(name), ".json") {
		// Normalize: strip whatever-case .json and re-add lowercase
		target = name[:len(name)-len(".json")] + ".json"
	} else {
		target = name + ".json"
	}

	candidate := filepath.Join(dir, target)

	// Verify the resolved path stays UNDER dir (path traversal protection).
	// The HasPrefix check with filepath.Separator ensures the candidate is
	// strictly inside the directory, not the directory itself.
	absCandidate, err := filepath.Abs(candidate)
	if err != nil {
		return "", fmt.Errorf("failed to resolve workflow path: %w", err)
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("failed to resolve automate directory: %w", err)
	}
	if !strings.HasPrefix(absCandidate, absDir+string(filepath.Separator)) {
		return "", fmt.Errorf("workflow path escapes automate directory")
	}

	// Filename validation also runs on the exact-match branch — Discover
	// already filters its results, but the exact-match path returns
	// whatever exists at `candidate`. Without this check, a planted file
	// like `legit;echo PWNED.json` would round-trip through ResolvePath
	// and end up embedded in the shell command line that BPM.Start hands
	// to `sh -c`, where the semicolon would execute injected commands.
	if !IsValidFilename(filepath.Base(candidate)) {
		return "", fmt.Errorf("unsafe workflow filename: %q", filepath.Base(candidate))
	}

	if _, err := os.Stat(candidate); err == nil {
		return candidate, nil
	}

	// Try substring match
	workflows, err := Discover(dir)
	if err != nil {
		return "", fmt.Errorf("no automate/ directory found")
	}

	var matches []Entry
	for _, wf := range workflows {
		if strings.Contains(strings.ToLower(wf.Filename), strings.ToLower(name)) {
			matches = append(matches, wf)
		}
	}

	if len(matches) == 1 {
		return matches[0].FilePath, nil
	}

	if len(matches) > 1 {
		names := make([]string, len(matches))
		for i, m := range matches {
			names[i] = m.Filename
		}
		return "", fmt.Errorf("multiple workflows match %q: %v — please specify the full filename", name, names)
	}

	return "", fmt.Errorf("no workflow matching %q found in %s/", name, dir)
}

// IsNotExists returns true if the error indicates a missing file or directory.
func IsNotExists(err error) bool {
	return errors.Is(err, fs.ErrNotExist)
}
