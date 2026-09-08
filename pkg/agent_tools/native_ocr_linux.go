//go:build linux && !js

package tools

import (
	"context"
	"fmt"
	"os/exec"
)

// nativeOCRAvailable reports whether the platform OCR shim can run.
// Linux has no universal OS OCR API; tesseract on PATH is the shim.
func nativeOCRAvailable() bool {
	if _, err := exec.LookPath("tesseract"); err != nil {
		return false
	}
	return true
}

// nativeOCR runs tesseract and returns recognized text. Empty output is a
// valid result (an image with no text).
func nativeOCR(ctx context.Context, imagePath string) (string, error) {
	cmd := exec.CommandContext(ctx, "tesseract", imagePath, "stdout", "--psm", "3")
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			return "", fmt.Errorf("native OCR: %w: %s", err, string(ee.Stderr))
		}
		return "", fmt.Errorf("native OCR: %w", err)
	}
	return string(out), nil
}
