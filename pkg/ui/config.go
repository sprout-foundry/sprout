package ui

// Config holds the configuration for the UI package.
type Config struct {
	// Enabled determines if the UI package is active.
	Enabled bool
	// Add other UI-related configuration fields here as needed.
}

// NewConfig creates a new Config instance with default values.
func NewConfig() *Config {
	return &Config{
		Enabled: true, // Set to true to enable the UI package by default.
	}
}
