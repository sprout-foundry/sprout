//go:build !js

package tools

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestRedactPassword_TokenBoundary locks in the "only whole-token match"
// redaction strategy. Short passwords like "pw" must redact only when
// they appear as a standalone word, not as a substring of "power",
// "puzzle", or any other benign word that happens to contain "pw".
//
// Why this matters: the prior strings.ReplaceAll approach was correct
// for long, unique passwords but silently corrupted output when the
// password was a short common sequence. A user with password "pw" who
// ran `echo "puzzle solved"` would see "z[REDACTED]le solved" in the
// tool response — confusing and potentially leaking adjacent text.
func TestRedactPassword_TokenBoundary(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		password string
		want     string
	}{
		{
			name:     "exact token",
			input:    "got pw\n",
			password: "pw",
			want:     "got [REDACTED]\n",
		},
		{
			name:     "with equals sign (no token boundary on either side)",
			input:    "got=pw\n",
			password: "pw",
			want:     "got=[REDACTED]\n",
		},
		{
			name:     "substring of larger word untouched",
			input:    "power on",
			password: "pw",
			want:     "power on",
		},
		{
			name:     "another substring untouched",
			input:    "puzzle master",
			password: "pw",
			want:     "puzzle master",
		},
		{
			name:     "mixed — one token, one substring",
			input:    "power pw puzzle",
			password: "pw",
			want:     "power [REDACTED] puzzle",
		},
		{
			name:     "long password",
			input:    "hunter2 hunter23",
			password: "hunter2",
			want:     "[REDACTED] hunter23", // hunter2 is a prefix of hunter23 — redaction leaves suffix
		},
		{
			name:     "password with regex metachars",
			input:    "got a.b\nnot a!b",
			password: "a.b",
			want:     "got [REDACTED]\nnot a!b",
		},
		{
			name:     "empty password no-op",
			input:    "nothing happens",
			password: "",
			want:     "nothing happens",
		},
		{
			// Regression: \b doesn't match between two non-word chars,
			// so a password starting with "!" would leak. Negative
			// class [^\w] captures the boundary and preserves it.
			name:     "password starting with non-word char",
			input:    "got !pw\n",
			password: "!pw",
			want:     "got [REDACTED]\n",
		},
		{
			name:     "password starting with non-word char at string start",
			input:    "!pw at start",
			password: "!pw",
			want:     "[REDACTED] at start",
		},
		{
			name:     "password ending with non-word char",
			input:    "got pw!\n",
			password: "pw!",
			want:     "got [REDACTED]\n",
		},
		{
			// Password "p.w" must match as a whole token. The
			// non-word boundary consumes the surrounding punctuation
			// and re-emits it in the replacement.
			name:     "password with internal non-word char",
			input:    "got p.w\n",
			password: "p.w",
			want:     "got [REDACTED]\n",
		},
		{
			name:     "password with non-word boundaries — boundary preserved",
			input:    "!pw,foo",
			password: "!pw",
			want:     "[REDACTED],foo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := redactPassword(tt.input, tt.password)
			if got != tt.want {
				t.Errorf("redactPassword(%q, %q)\n  got:  %q\n  want: %q", tt.input, tt.password, got, tt.want)
			}
		})
	}
}

// TestRunShellCommandWithPasswordSupport_LongPassword covers a longer
// realistic password like "my-secret-pw-99" — redaction must still work
// end-to-end with the token-boundary strategy.
func TestRunShellCommandWithPasswordSupport_LongPassword(t *testing.T) {
	prompter := &countingPrompter{password: "my-secret-pw-99"}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := `printf 'Password: '; read pw; echo "got=$pw"`

	out, err := runShellCommandWithPasswordSupport(ctx, cmd, prompter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prompter.calls != 1 {
		t.Errorf("expected prompter to be called once, got %d", prompter.calls)
	}
	if !strings.Contains(out, "got=[REDACTED]") {
		t.Errorf("password should be redacted; output: %s", out)
	}
	if strings.Contains(out, "my-secret-pw-99") {
		t.Errorf("password should not appear in output; output: %s", out)
	}
}
