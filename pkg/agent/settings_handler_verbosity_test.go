package agent

import (
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// TestOutputVerbositySetting_RoundTrip verifies the output_verbosity setting
// flows through getConfigValue and setConfigValue without losing the value.
func TestOutputVerbositySetting_RoundTrip(t *testing.T) {
	cfg := &configuration.Config{}

	// Default state: empty string (no verbosity preference)
	if got := cfg.OutputVerbosity; got != "" {
		t.Errorf("default OutputVerbosity = %q, want empty string", got)
	}

	// setConfigValue should accept all three valid values
	for _, verbosity := range []string{"compact", "default", "verbose"} {
		if err := setConfigValue(cfg, "output_verbosity", verbosity); err != nil {
			t.Errorf("setConfigValue(%q) returned error: %v", verbosity, err)
		}
		if cfg.OutputVerbosity != verbosity {
			t.Errorf("after setConfigValue(%q), OutputVerbosity = %q, want %q",
				verbosity, cfg.OutputVerbosity, verbosity)
		}

		// getConfigValue should return the same value
		got, err := getConfigValue(cfg, "output_verbosity")
		if err != nil {
			t.Errorf("getConfigValue after setConfigValue(%q) returned error: %v", verbosity, err)
		}
		if got != verbosity {
			t.Errorf("getConfigValue = %q, want %q", got, verbosity)
		}
	}

	// Empty string is also valid (clears the setting)
	if err := setConfigValue(cfg, "output_verbosity", ""); err != nil {
		t.Errorf("setConfigValue(\"\") returned error: %v", err)
	}
	if cfg.OutputVerbosity != "" {
		t.Errorf("after setConfigValue(\"\"), OutputVerbosity = %q, want empty", cfg.OutputVerbosity)
	}

	// Case-insensitive: "COMPACT" should normalize to "compact"
	if err := setConfigValue(cfg, "output_verbosity", "COMPACT"); err != nil {
		t.Errorf("setConfigValue(\"COMPACT\") returned error: %v", err)
	}
	if cfg.OutputVerbosity != "compact" {
		t.Errorf("after setConfigValue(\"COMPACT\"), OutputVerbosity = %q, want \"compact\"", cfg.OutputVerbosity)
	}

	// Invalid value should be rejected
	if err := setConfigValue(cfg, "output_verbosity", "loud"); err == nil {
		t.Error("setConfigValue(\"loud\") should have returned an error")
	}
}

// TestOutputVerbositySetting_ExposedInAllSettings verifies the setting appears
// in allSettings() so the frontend can discover it.
func TestOutputVerbositySetting_ExposedInAllSettings(t *testing.T) {
	settings := allSettings()
	var found bool
	for _, s := range settings {
		if s.key == "output_verbosity" {
			found = true
			if s.description == "" {
				t.Error("output_verbosity has empty description")
			}
			if s.validValues == "" {
				t.Error("output_verbosity has empty validValues")
			}
			break
		}
	}
	if !found {
		t.Error("output_verbosity not found in allSettings()")
	}
}