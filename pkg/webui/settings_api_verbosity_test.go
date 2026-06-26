package webui

import (
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// TestSanitizedConfig_IncludesOutputVerbosity verifies that GET /api/settings
// includes the output_verbosity field so the frontend can read the value.
func TestSanitizedConfig_IncludesOutputVerbosity(t *testing.T) {
	cfg := &configuration.Config{
		OutputVerbosity: "compact",
	}
	out := sanitizedConfig(cfg)
	got, ok := out["output_verbosity"]
	if !ok {
		t.Fatal("sanitizedConfig missing output_verbosity key")
	}
	if got != "compact" {
		t.Errorf("sanitizedConfig output_verbosity = %v, want \"compact\"", got)
	}
}

// TestApplyPartialSettings_OutputVerbosity verifies that PUT /api/settings
// persists the output_verbosity setting.
func TestApplyPartialSettings_OutputVerbosity(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{"compact", "compact", "compact", false},
		{"default", "default", "default", false},
		{"verbose", "verbose", "verbose", false},
		{"empty clears", "", "", false},
		{"uppercase normalized", "COMPACT", "compact", false},
		{"invalid rejected", "loud", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &configuration.Config{}
			patch := map[string]interface{}{"output_verbosity": tc.input}
			unknown, err := applyPartialSettings(cfg, patch)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(err.Error(), "output_verbosity") {
					t.Errorf("error should mention output_verbosity: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cfg.OutputVerbosity != tc.want {
				t.Errorf("OutputVerbosity = %q, want %q", cfg.OutputVerbosity, tc.want)
			}
			// Verify the key is recognized (not in unknown list)
			for _, u := range unknown {
				if u == "output_verbosity" {
					t.Error("output_verbosity should not be in unknown keys list")
				}
			}
		})
	}
}

// TestApplyPartialSettings_OutputVerbosity_RoundTripViaSanitizedConfig
// verifies the full PUT → save → GET cycle preserves the value.
func TestApplyPartialSettings_OutputVerbosity_RoundTripViaSanitizedConfig(t *testing.T) {
	cfg := &configuration.Config{}
	patch := map[string]interface{}{"output_verbosity": "verbose"}
	if _, err := applyPartialSettings(cfg, patch); err != nil {
		t.Fatalf("applyPartialSettings failed: %v", err)
	}
	// Simulate the save → reload → GET cycle by running the config
	// through sanitizedConfig (which is what GET /api/settings returns).
	out := sanitizedConfig(cfg)
	if out["output_verbosity"] != "verbose" {
		t.Errorf("round-trip lost output_verbosity: got %v, want \"verbose\"", out["output_verbosity"])
	}
}
