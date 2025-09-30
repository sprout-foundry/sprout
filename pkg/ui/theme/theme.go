package theme

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/fatih/color"
)

// Theme represents a color theme configuration
type Theme struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Colors      struct {
		Primary    string `json:"primary"`
		Secondary  string `json:"secondary"`
		Success    string `json:"success"`
		Warning    string `json:"warning"`
		Error      string `json:"error"`
		Info       string `json:"info"`
		Comment    string `json:"comment"`
		Keyword    string `json:"keyword"`
		String     string `json:"string"`
		Number     string `json:"number"`
		Function   string `json:"function"`
		Background string `json:"background"`
		Foreground string `json:"foreground"`
	} `json:"colors"`
}

// ThemeManager manages color themes for the application
type ThemeManager struct {
	CurrentTheme *Theme
	ThemesDir    string
}

// NewThemeManager creates a new theme manager
func NewThemeManager(themesDir string) *ThemeManager {
	return &ThemeManager{
		ThemesDir: themesDir,
	}
}

// LoadDefaultTheme loads the default theme
func (tm *ThemeManager) LoadDefaultTheme() *Theme {
	return &Theme{
		Name:        "default",
		Description: "Default theme",
		Colors: struct {
			Primary    string `json:"primary"`
			Secondary  string `json:"secondary"`
			Success    string `json:"success"`
			Warning    string `json:"warning"`
			Error      string `json:"error"`
			Info       string `json:"info"`
			Comment    string `json:"comment"`
			Keyword    string `json:"keyword"`
			String     string `json:"string"`
			Number     string `json:"number"`
			Function   string `json:"function"`
			Background string `json:"background"`
			Foreground string `json:"foreground"`
		}{
			Primary:    "cyan",
			Secondary:  "magenta",
			Success:    "green",
			Warning:    "yellow",
			Error:      "red",
			Info:       "blue",
			Comment:    "hiBlack",
			Keyword:    "hiBlue",
			String:     "hiGreen",
			Number:     "hiYellow",
			Function:   "hiCyan",
			Background: "black",
			Foreground: "white",
		},
	}
}

// LoadThemeFromFile loads a theme from a JSON file
func (tm *ThemeManager) LoadThemeFromFile(themePath string) (*Theme, error) {
	data, err := os.ReadFile(themePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read theme file: %w", err)
	}

	var theme Theme
	if err := json.Unmarshal(data, &theme); err != nil {
		return nil, fmt.Errorf("failed to parse theme file: %w", err)
	}

	return &theme, nil
}

// LoadTheme loads a theme by name
func (tm *ThemeManager) LoadTheme(themeName string) (*Theme, error) {
	// Try to load from themes directory first
	if tm.ThemesDir != "" {
		themePath := filepath.Join(tm.ThemesDir, themeName+".json")
		if _, err := os.Stat(themePath); err == nil {
			theme, err := tm.LoadThemeFromFile(themePath)
			if err == nil {
				return theme, nil
			}
		}
	}

	// Fall back to default theme
	return tm.LoadDefaultTheme(), nil
}

// ApplyTheme applies the current theme to color variables
func (tm *ThemeManager) ApplyTheme() {
	if tm.CurrentTheme == nil {
		tm.CurrentTheme = tm.LoadDefaultTheme()
	}

	// Apply colors to global color variables
	// This creates a more consistent color system
}

// GetColor returns a color based on the theme
func (tm *ThemeManager) GetColor(colorName string) *color.Color {
	if tm.CurrentTheme == nil {
		tm.CurrentTheme = tm.LoadDefaultTheme()
	}

	var colorValue string
	switch colorName {
	case "primary":
		colorValue = tm.CurrentTheme.Colors.Primary
	case "secondary":
		colorValue = tm.CurrentTheme.Colors.Secondary
	case "success":
		colorValue = tm.CurrentTheme.Colors.Success
	case "warning":
		colorValue = tm.CurrentTheme.Colors.Warning
	case "error":
		colorValue = tm.CurrentTheme.Colors.Error
	case "info":
		colorValue = tm.CurrentTheme.Colors.Info
	case "comment":
		colorValue = tm.CurrentTheme.Colors.Comment
	case "keyword":
		colorValue = tm.CurrentTheme.Colors.Keyword
	case "string":
		colorValue = tm.CurrentTheme.Colors.String
	case "number":
		colorValue = tm.CurrentTheme.Colors.Number
	case "function":
		colorValue = tm.CurrentTheme.Colors.Function
	case "background":
		colorValue = tm.CurrentTheme.Colors.Background
	case "foreground":
		colorValue = tm.CurrentTheme.Colors.Foreground
	default:
		colorValue = "white"
	}

	return getColorFromName(colorValue)
}

// getColorFromName converts a color name to a color.Color instance
func getColorFromName(colorName string) *color.Color {
	switch colorName {
	case "black":
		return color.New(color.FgBlack)
	case "red":
		return color.New(color.FgRed)
	case "green":
		return color.New(color.FgGreen)
	case "yellow":
		return color.New(color.FgYellow)
	case "blue":
		return color.New(color.FgBlue)
	case "magenta":
		return color.New(color.FgMagenta)
	case "cyan":
		return color.New(color.FgCyan)
	case "white":
		return color.New(color.FgWhite)
	case "hiBlack":
		return color.New(color.FgHiBlack)
	case "hiRed":
		return color.New(color.FgHiRed)
	case "hiGreen":
		return color.New(color.FgHiGreen)
	case "hiYellow":
		return color.New(color.FgHiYellow)
	case "hiBlue":
		return color.New(color.FgHiBlue)
	case "hiMagenta":
		return color.New(color.FgHiMagenta)
	case "hiCyan":
		return color.New(color.FgHiCyan)
	case "hiWhite":
		return color.New(color.FgHiWhite)
	default:
		return color.New(color.FgWhite)
	}
}
