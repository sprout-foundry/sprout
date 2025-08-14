package ui

import (
	"os"
	"strings"
)

var enabled bool

// SetEnabled sets the global UI enabled state.
func SetEnabled(v bool) { enabled = v }

// Enabled returns the global UI enabled state.
func Enabled() bool { return enabled }

// FromEnv detects whether UI should be enabled from environment variables.
// LEDIT_UI accepts: 1, true, yes (case-insensitive)
func FromEnv() bool {
	val := strings.TrimSpace(os.Getenv("LEDIT_UI"))
	if val == "" {
		return false
	}
	val = strings.ToLower(val)
	return val == "1" || val == "true" || val == "yes"
}
