//go:build (!darwin && !linux && !windows) || js

package tools

import (
	"context"
	"fmt"
)

// nativeOCRAvailable reports whether the platform OCR shim can run.
// Unsupported platforms (WASM, BSDs) report unavailable.
func nativeOCRAvailable() bool { return false }

// nativeOCR always errors on unsupported platforms.
func nativeOCR(_ context.Context, _ string) (string, error) {
	return "", fmt.Errorf("native OCR unavailable on this platform")
}
