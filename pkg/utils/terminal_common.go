package utils

import (
	"os"
	"strconv"
)

// getTerminalSizeEnv tries to get terminal size from environment variables
// This function is shared between Unix and Windows platforms
func getTerminalSizeEnv() (*TerminalSize, error) {
	width := 0
	height := 0

	// Try COLUMNS and LINES
	if val := os.Getenv("COLUMNS"); val != "" {
		if w, err := strconv.Atoi(val); err == nil && w > 0 {
			width = w
		}
	}

	if val := os.Getenv("LINES"); val != "" {
		if h, err := strconv.Atoi(val); err == nil && h > 0 {
			height = h
		}
	}

	// Also try TERM_WIDTH and TERM_HEIGHT
	if width == 0 {
		if val := os.Getenv("TERM_WIDTH"); val != "" {
			if w, err := strconv.Atoi(val); err == nil && w > 0 {
				width = w
			}
		}
	}

	if height == 0 {
		if val := os.Getenv("TERM_HEIGHT"); val != "" {
			if h, err := strconv.Atoi(val); err == nil && h > 0 {
				height = h
			}
		}
	}

	// LEDIT specific overrides
	if height == 0 {
		if val := os.Getenv("LEDIT_TERM_HEIGHT"); val != "" {
			if h, err := strconv.Atoi(val); err == nil && h > 0 {
				height = h
			}
		}
	}

	if width > 0 && height > 0 {
		return &TerminalSize{
			Width:  width,
			Height: height,
		}, nil
	}

	return nil, nil
}
