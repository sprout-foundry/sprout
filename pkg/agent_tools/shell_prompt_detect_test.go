//go:build !js

package tools

import "testing"

func TestInteractivePromptDetection(t *testing.T) {
	cases := []struct {
		in     string
		prompt bool
		secret bool
	}{
		// Secrets
		{"[sudo] password for alan: ", true, true},
		{"Password:", true, true},
		{"Enter passphrase for key '/home/x/.ssh/id_rsa': ", true, true},

		// Generic confirmations
		{"Overwrite existing file? [y/N] ", true, false},
		{"(yes/no) ", true, false},
		{"Are you sure you want to delete this? ", true, false},
		{"Do you want to continue? ", true, false},
		{"Press any key to continue", true, false},
		{"Proceed? ", true, false},

		// Value prompts
		{"Enter your name: ", true, false},
		{"Choose an option: ", true, false},
		{"Enter the commit message: ", true, false},

		// NOT prompts — ordinary output
		{"Building project...", false, false},
		{"error: something failed", false, false},
		{"See https://example.com/docs:", false, false},
		{"Total: 42 files processed", false, false},
		{"", false, false},
		{"done\n", false, false},
		// A colon-terminated sentence that is too long to be a prompt label
		{"This is a very long line of log output that happens to end with a colon and keeps going well past the limit:", false, false},
		// Prompt already answered: newline follows, so the tail is empty
		{"Continue? [y/N] y\n", false, false},
		// Common output lines that end in a colon must not open a dialog
		{"user not found:", false, false},
		{"type error:", false, false},
		{"file not found:", false, false},
		{"warning: disk full:", false, false},
	}

	for _, tc := range cases {
		gotPrompt, gotSecret := isInteractivePrompt(tc.in)
		if gotPrompt != tc.prompt || gotSecret != tc.secret {
			t.Errorf("isInteractivePrompt(%q) = (prompt=%v, secret=%v), want (%v, %v)",
				tc.in, gotPrompt, gotSecret, tc.prompt, tc.secret)
		}
	}
}
