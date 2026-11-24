package core

import (
	"testing"
)

func TestLooksInsufficientAfterToolCalls(t *testing.T) {
	rv := NewResponseValidator(ResponseValidatorOptions{})

	cases := []struct {
		name    string
		content string
		want    bool
	}{
		// --- Should be caught: meta-acknowledgement with no findings ---
		{"empty", "", false}, // empty handled by isBlankIteration
		{"reviewed no findings", "I've reviewed the files.", true},
		{"reviewed lowercase", "i've reviewed the files.", true},
		{"have reviewed", "I have reviewed the files you mentioned.", true},
		{"checked no findings", "I checked the code.", true},
		{"looked at no findings", "I've looked at the relevant files.", true},
		{"read the no findings", "I read the files.", true},
		{"examined no findings", "I examined the handler.", true},
		// New: identified/found/analyzed without findings (from GRPO eval failures)
		{"identified no findings", "I've identified the key issues.", true},
		{"found no findings", "I found the problems in the codebase.", true},
		{"analyzed no findings", "I've analyzed the code structure.", true},
		{"have identified", "I have identified the build issues.", true},
		{"have found", "I have found the security concerns.", true},

		// --- Should pass: simple confirmations (no meta prefix) ---
		{"done", "Done.", false},
		{"got it", "Got it.", false},
		{"yes", "Yes.", false},
		{"build passed", "Build passed.", false},
		{"tests pass", "All tests pass.", false},
		{"no issues", "No issues found.", false},

		// --- Should pass: meta prefix BUT has substance signals ---
		{"reviewed with file ref", "I've reviewed the files. handler.go:42 has a nil pointer dereference.", false},
		{"reviewed with code block", "I've reviewed the files.\n```go\nfunc main() {}\n```", false},
		{"reviewed with list", "I've reviewed the files.\n- Issue 1: missing error check\n- Issue 2: nil pointer", false},
		{"reviewed with error", "I've checked the code. error: undefined variable x on line 15", false},
		{"checked with func ref", "I checked the code. The func processRequest has a bug.", false},
		{"reviewed with test result", "I've reviewed the tests. 3 passed, 2 failed: assertion mismatch", false},

		// --- Should pass: meta prefix BUT long enough to be substantive (>=60 words) ---
		{"long meta response", "I've reviewed the files you asked me to look at and I found that the overall structure is reasonable and the code follows the project conventions for error handling, logging, and configuration management throughout the entire codebase. The main areas are well organized, the functions are properly separated, and the test coverage is adequate for the critical paths. I particularly note that the authentication layer uses a clean middleware chain and the database access is properly abstracted behind a repository interface.", false},

		// --- Should pass: tentative planning (caught by separate check) ---
		{"let me check", "Let me check that file first.", false}, // not a meta-acknowledgement
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := rv.LooksInsufficientAfterToolCalls(tc.content)
			if got != tc.want {
				t.Errorf("LooksInsufficientAfterToolCalls(%q) = %v, want %v",
					tc.content, got, tc.want)
			}
		})
	}
}

func TestLooksInsufficientAfterToolCalls_DisabledViaOption(t *testing.T) {
	// The validator method itself always works; the DisableMinimumSubstanceGuard
	// option controls whether the conversation loop calls it. Here we just
	// verify the validator returns true for a known-bad input so the loop
	// test has a reliable trigger.
	rv := NewResponseValidator(ResponseValidatorOptions{})
	if !rv.LooksInsufficientAfterToolCalls("I've reviewed the files.") {
		t.Fatal("expected true for meta-acknowledgement with no findings")
	}
}
