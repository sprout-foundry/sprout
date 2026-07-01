package agent

import (
	"encoding/json"
	"os"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// Theme represents a color theme configuration
type Theme struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Colors      struct {
		Success   string `json:"success"`
		Warning   string `json:"warning"`
		Error     string `json:"error"`
		Info      string `json:"info"`
		Primary   string `json:"primary"`
		Secondary string `json:"secondary"`
		Accent    string `json:"accent"`
	} `json:"colors"`
}

// ThemeManager manages color themes
type ThemeManager struct {
	theme Theme
}

// NewThemeManager creates a new theme manager with default theme
func NewThemeManager() *ThemeManager {
	manager := &ThemeManager{}
	manager.LoadDefaultTheme()
	return manager
}

// LoadDefaultTheme loads the default theme
func (tm *ThemeManager) LoadDefaultTheme() {
	tm.theme = Theme{
		Name:        "default",
		Description: "Default theme",
		Colors: struct {
			Success   string `json:"success"`
			Warning   string `json:"warning"`
			Error     string `json:"error"`
			Info      string `json:"info"`
			Primary   string `json:"primary"`
			Secondary string `json:"secondary"`
			Accent    string `json:"accent"`
		}{
			Success:   "green",
			Warning:   "yellow",
			Error:     "red",
			Info:      "blue",
			Primary:   "cyan",
			Secondary: "magenta",
			Accent:    "white",
		},
	}
}

// LoadThemeFromFile loads a theme from a JSON file
func (tm *ThemeManager) LoadThemeFromFile(themePath string) error {
	// Check if file exists
	if _, err := os.Stat(themePath); os.IsNotExist(err) {
		// File doesn't exist, use default
		tm.LoadDefaultTheme()
		return nil
	}

	// Read the file
	data, err := os.ReadFile(themePath)
	if err != nil {
		return agenterrors.Wrap(err, "error reading theme file")
	}

	// Parse JSON
	var theme Theme
	if err := json.Unmarshal(data, &theme); err != nil {
		return agenterrors.Wrap(err, "error parsing theme JSON")
	}

	tm.theme = theme
	return nil
}

// GetColor returns a color by name
func (tm *ThemeManager) GetColor(name string) string {
	switch name {
	case "success":
		return tm.theme.Colors.Success
	case "warning":
		return tm.theme.Colors.Warning
	case "error":
		return tm.theme.Colors.Error
	case "info":
		return tm.theme.Colors.Info
	case "primary":
		return tm.theme.Colors.Primary
	case "secondary":
		return tm.theme.Colors.Secondary
	case "accent":
		return tm.theme.Colors.Accent
	default:
		return "white" // default fallback
	}
}

// GetTheme returns the current theme
func (tm *ThemeManager) GetTheme() Theme {
	return tm.theme
}
