package console

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// PastedTextDirName is the workspace-relative directory under which large
// text pastes are auto-saved by SavePastedText. Mirrors PastedImageDirName.
const PastedTextDirName = ".sprout/pastes"

// SmartPasteLineThreshold and SmartPasteByteThreshold trigger the smart-
// paste flow when a bracketed paste's content exceeds either bound. The
// content is written to PastedTextDirName and a `@<relpath>` reference is
// inserted into the input buffer instead of the raw blob, mirroring the
// image-paste pattern. SP-048-4d.
const (
	SmartPasteLineThreshold = 100
	SmartPasteByteThreshold = 5 * 1024
)

// SavePastedText writes content to .sprout/pastes/ with a timestamped
// random-suffixed filename and returns the workspace-relative path
// (e.g. "./.sprout/pastes/paste_20260520_145959_abc123.txt"). The path is
// suitable for insertion as a `@path` reference the agent can read.
// SP-048-4d. Mirrors SavePastedImage.
func SavePastedText(content, baseDir string) (string, error) {
	cwd := baseDir
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("failed to get working directory: %w", err)
		}
	}

	dir := filepath.Join(cwd, PastedTextDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create paste directory: %w", err)
	}

	randBytes := make([]byte, 3)
	if _, err := rand.Read(randBytes); err != nil {
		return "", fmt.Errorf("failed to generate random suffix: %w", err)
	}
	suffix := hex.EncodeToString(randBytes)
	timestamp := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("paste_%s_%s.txt", timestamp, suffix)

	fullPath := filepath.Join(dir, filename)
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("failed to write paste file: %w", err)
	}

	return "./" + filepath.Join(PastedTextDirName, filename), nil
}

// ShouldSmartSavePaste reports whether content is large enough to merit
// auto-saving via SavePastedText. Threshold-only helper so the caller can
// short-circuit without computing both counters at every call site.
func ShouldSmartSavePaste(content string) bool {
	if len(content) > SmartPasteByteThreshold {
		return true
	}
	// Count newlines lazily — cheaper than splitting.
	lines := 1
	for i := 0; i < len(content); i++ {
		if content[i] == '\n' {
			lines++
			if lines > SmartPasteLineThreshold {
				return true
			}
		}
	}
	return false
}
