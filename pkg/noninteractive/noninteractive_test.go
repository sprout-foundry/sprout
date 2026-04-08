package noninteractive

import (
	"errors"
	"strings"
	"testing"
)

func TestHelpHint(t *testing.T) {
	if HelpHint == "" {
		t.Error("HelpHint should not be empty")
	}

	keyPhrases := []string{
		"LEDIT_PROVIDER",
		"~/.ledit/config.json",
		"ledit agent",
		"interactively",
	}

	for _, phrase := range keyPhrases {
		if !strings.Contains(HelpHint, phrase) {
			t.Errorf("HelpHint should contain phrase %q", phrase)
		}
	}
}

func TestIsNonInteractiveHint(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error returns false",
			err:  nil,
			want: false,
		},
		{
			name: "error containing full HelpHint returns true",
			err:  errors.New("provider not configured: " + HelpHint),
			want: true,
		},
		{
			name: "error with only part of HelpHint returns false",
			err:  errors.New("error: Set LEDIT_PROVIDER / configure ~/.ledit/config.json"),
			want: false,
		},
		{
			name: "error without HelpHint returns false",
			err:  errors.New("some other error"),
			want: false,
		},
		{
			name: "exact HelpHint as error returns true",
			err:  errors.New(HelpHint),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsNonInteractiveHint(tt.err); got != tt.want {
				t.Errorf("IsNonInteractiveHint(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
