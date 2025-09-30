package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/fatih/color"
)

// Common ANSI/SGR sequences used across the UI
const (
	SGRReset       = "\033[0m"
	SGRDefaultFgBg = "\033[39;49m"
	ClearLine      = "\033[2K"
)

// Theme represents the color theme configuration
type Theme struct {
	// Colors for different UI elements
	SuccessColor   *color.Color `json:"success_color"`
	ErrorColor     *color.Color `json:"error_color"`
	WarningColor   *color.Color `json:"warning_color"`
	InfoColor      *color.Color `json:"info_color"`
	HighlightColor *color.Color `json:"highlight_color"`
	CommentColor   *color.Color `json:"comment_color"`
	LinkColor      *color.Color `json:"link_color"`
	// Background colors
	SuccessBg   *color.Color `json:"success_bg"`
	ErrorBg     *color.Color `json:"error_bg"`
	WarningBg   *color.Color `json:"warning_bg"`
	InfoBg      *color.Color `json:"info_bg"`
	HighlightBg *color.Color `json:"highlight_bg"`
}

// DefaultTheme returns the default color theme
func DefaultTheme() *Theme {
	return &Theme{
		SuccessColor:   color.New(color.FgGreen),
		ErrorColor:     color.New(color.FgRed),
		WarningColor:   color.New(color.FgYellow),
		InfoColor:      color.New(color.FgBlue),
		HighlightColor: color.New(color.FgCyan),
		CommentColor:   color.New(color.FgHiBlack),
		LinkColor:      color.New(color.FgHiBlue),
		SuccessBg:      color.New(color.BgGreen),
		ErrorBg:        color.New(color.BgRed),
		WarningBg:      color.New(color.BgYellow),
		InfoBg:         color.New(color.BgBlue),
		HighlightBg:    color.New(color.BgCyan),
	}
}

// LoadTheme loads a theme from JSON file, falling back to default if file doesn't exist
func LoadTheme(themePath string) (*Theme, error) {
	// Try to load from file
	theme := DefaultTheme()
	if themePath != "" {
		data, err := os.ReadFile(themePath)
		if err == nil {
			// File exists, try to parse it
			err := json.Unmarshal(data, theme)
			if err != nil {
				return DefaultTheme(), fmt.Errorf("failed to parse theme file: %w", err)
			}
			return theme, nil
		}
	}

	return theme, nil
}

// Bg wraps text with a background SGR sequence and resets at the end.
func Bg(text, bg string) string {
	return bg + text + SGRReset
}

// WithDim wraps text in a dim+white SGR and resets at the end.
func WithDim(text string) string {
	return "\033[2m\033[37m" + text + SGRReset
}

// BgPad creates a block of spaces with the given background and resets.
func BgPad(width int, bg string) string {
	if width <= 0 {
		return ""
	}
	return bg + strings.Repeat(" ", width) + SGRReset
}
