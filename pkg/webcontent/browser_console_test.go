package webcontent

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestConsoleErrorMessages pins the extraction of error-level console
// messages from the rod instrumentation's "[<level>] " prefixed capture:
// only "[error] " entries are kept, with the tag stripped; every other
// level and untagged entry is dropped.
func TestConsoleErrorMessages(t *testing.T) {
	cases := []struct {
		name     string
		messages []string
		want     []string
	}{
		{"none", nil, nil},
		{"empty", []string{}, nil},
		{
			"error only",
			[]string{"[error] boom", "[error] oops"},
			[]string{"boom", "oops"},
		},
		{
			"mixed levels keep only error",
			[]string{"[log] hello", "[info] fine", "[warn] careful", "[error] bad"},
			[]string{"bad"},
		},
		{
			"untagged entries are dropped",
			[]string{"plain", "[error] tagged", "  "},
			[]string{"tagged"},
		},
		{
			"error with empty payload",
			[]string{"[error] ", "[error] real"},
			[]string{"", "real"},
		},
		{
			"order preserved",
			[]string{"[error] first", "[log] x", "[error] second"},
			[]string{"first", "second"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ConsoleErrorMessages(tc.messages)
			assert.Equal(t, tc.want, got)
		})
	}

	// The sentinel is distinct from the nop renderer's error so a page check
	// can errors.Is on it.
	assert.True(t, errors.Is(ErrBrowserUnavailable, ErrBrowserUnavailable))
	assert.Equal(t, "browser rendering not available", ErrBrowserUnavailable.Error())
}
