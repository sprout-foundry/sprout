//go:build !js

package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// detectShellProfilePath returns the absolute path of the shell profile
// file that should be edited for the current user. Detection order:
//   1. $SHELL basename:
//        - "bash" → ~/.bashrc (fallback to ~/.bash_profile if .bashrc missing)
//        - "zsh"  → ~/.zshrc
//        - "fish" → ~/.config/fish/config.fish
//        - others → ~/.profile (POSIX fallback)
//   2. If the detected file doesn't exist, ~/.bashrc is used as a final
//      fallback because it is the most widely-supported login shell profile.
//
// On any failure (missing $HOME, etc.) the empty string is returned.
func detectShellProfilePath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}

	shell := filepath.Base(os.Getenv("SHELL"))
	var candidate string
	switch shell {
	case "bash":
		candidate = filepath.Join(home, ".bashrc")
		if _, err := os.Stat(candidate); err != nil {
			alt := filepath.Join(home, ".bash_profile")
			if _, err := os.Stat(alt); err == nil {
				candidate = alt
			}
		}
	case "zsh":
		candidate = filepath.Join(home, ".zshrc")
	case "fish":
		candidate = filepath.Join(home, ".config", "fish", "config.fish")
	default:
		candidate = filepath.Join(home, ".profile")
	}
	return candidate
}

// validateShellValue rejects values that could break shell quoting.
// GitHub PATs are alphanumeric+underscore, but this is defensive.
func validateShellValue(value string) error {
	for _, r := range []rune{'"', '\n', '\r', '\\', '$', '`', '!'} {
		if strings.ContainsRune(value, r) {
			return fmt.Errorf("value contains forbidden character %q; cannot safely write to shell profile", r)
		}
	}
	return nil
}

// writeEnvToShellProfile writes an `export NAME="value"` line to path,
// idempotently: it updates an existing sprout-managed block in-place or
// appends a new one. The write is atomic (write .tmp then rename).
func writeEnvToShellProfile(path, envName, value string) error {
	if err := validateShellValue(value); err != nil {
		return fmt.Errorf("%s: write to %s aborted", err, path)
	}

	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to read %s: %w", path, err)
	}

	var content []byte
	if !os.IsNotExist(err) {
		content = make([]byte, len(existing))
		copy(content, existing)
	}

	existingStr := string(content)
	updated := false

	// Check for existing sprout-managed block and replace it in-place.
	startMarker := "# sprout-managed: " + envName
	endMarker := "# sprout-managed: end"

	startIdx := strings.Index(existingStr, startMarker)
	if startIdx >= 0 {
		endIdx := strings.Index(existingStr[startIdx:], endMarker)
		if endIdx >= 0 {
			endIdx += startIdx + len(endMarker)
			newBlock := startMarker + "\n" +
				"export " + envName + "=\"" + value + "\"\n" +
				endMarker
			// endIdx now points past the end marker; the trailing newline (if
			// any) is preserved by including content[endIdx:] in the slice.
			content = append(content[:startIdx], append([]byte(newBlock), content[endIdx:]...)...)
			updated = true
		}
	}

	// No existing line — append a new sprout-managed block.
	// We deliberately do NOT replace bare `export NAME=...` or `NAME=...`
	// lines: those may be intentional (e.g. command-substitution
	// `NAME="$(op read ...)"`) and silently overwriting them with a
	// hardcoded value would be a data-loss risk. New writes always go
	// through the managed-block path so future updates are safe.
	if !updated {
		appendBlock := "\n# sprout-managed: " + envName + "\n" +
			"export " + envName + "=\"" + value + "\"\n" +
			"# sprout-managed: end\n"
		if len(content) > 0 && content[len(content)-1] != '\n' {
			content = append(content, '\n')
		}
		content = append(content, []byte(appendBlock)...)
	}

	// Atomic write: .tmp then rename, so a crash never leaves partial content.
	tmpPath := path + ".tmp"
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create parent dir for %s: %w", path, err)
	}
	if err := os.WriteFile(tmpPath, content, 0644); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to write %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to rename temp file to %s: %w", path, err)
	}
	return nil
}
