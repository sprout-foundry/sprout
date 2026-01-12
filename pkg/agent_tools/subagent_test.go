package tools

import (
	"os"
	"testing"
	"time"
)

func TestGetSubagentTimeout(t *testing.T) {
	// Save original environment
	originalEnv := os.Getenv("LEDIT_SUBAGENT_TIMEOUT")
	defer func() {
		if originalEnv == "" {
			os.Unsetenv("LEDIT_SUBAGENT_TIMEOUT")
		} else {
			os.Setenv("LEDIT_SUBAGENT_TIMEOUT", originalEnv)
		}
	}()

	tests := []struct {
		name      string
		envValue  string
		want      time.Duration
		setEnv    bool
	}{
		{
			name:     "default when no env var set",
			want:     0, // No timeout by default
			setEnv:   false,
		},
		{
			name:     "valid duration string with minutes",
			envValue: "45m",
			want:     45 * time.Minute,
			setEnv:   true,
		},
		{
			name:     "valid duration string with hours",
			envValue: "2h",
			want:     2 * time.Hour,
			setEnv:   true,
		},
		{
			name:     "valid duration string with seconds",
			envValue: "3600s",
			want:     3600 * time.Second,
			setEnv:   true,
		},
		{
			name:     "valid minutes number only",
			envValue: "60",
			want:     60 * time.Minute,
			setEnv:   true,
		},
		{
			name:     "zero value means no timeout",
			envValue: "0",
			want:     0, // No timeout
			setEnv:   true,
		},
		{
			name:     "invalid duration string defaults to no timeout",
			envValue: "invalid",
			want:     0, // No timeout
			setEnv:   true,
		},
		{
			name:     "empty string defaults to no timeout",
			envValue: "",
			want:     0, // No timeout
			setEnv:   true,
		},
		{
			name:     "complex duration string",
			envValue: "1h30m",
			want:     90 * time.Minute,
			setEnv:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clean up before test
			os.Unsetenv("LEDIT_SUBAGENT_TIMEOUT")

			if tt.setEnv {
				os.Setenv("LEDIT_SUBAGENT_TIMEOUT", tt.envValue)
			}

			got := GetSubagentTimeout()
			if got != tt.want {
				t.Errorf("GetSubagentTimeout() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDefaultSubagentTimeout(t *testing.T) {
	// Tests for default timeout (should be 0 - no timeout)
	want := 0 * time.Minute
	if DefaultSubagentTimeout != want {
		t.Errorf("DefaultSubagentTimeout = %v, want %v", DefaultSubagentTimeout, want)
	}
}
