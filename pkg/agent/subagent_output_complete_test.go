package agent

import (
	"strings"
	"testing"
)

// TestSubagentResult_OutputComplete verifies the OutputComplete field is set
// correctly based on output quality — this is the Phase 3 signal the
// orchestrator uses to decide whether a subagent actually produced useful
// findings vs. completing with empty/insufficient output.
func TestSubagentResult_OutputComplete(t *testing.T) {
	// generateOutput produces a string of exactly n non-whitespace chars.
	makeOutput := func(n int) string {
		return strings.Repeat("x", n)
	}

	cases := []struct {
		name           string
		output         string
		err            error
		cancelled      bool
		budgetExceeded bool
		truncated      bool
		wantComplete   bool
	}{{
		name:         "substantive output",
		output:       makeOutput(100),
		wantComplete: true,
	},
		{
			name:         "exactly 50 chars",
			output:       makeOutput(50),
			wantComplete: true,
		},
		{
			name:         "brief output under 50 chars",
			output:       "I've reviewed the files.",
			wantComplete: false,
		},
		{
			name:         "empty output",
			output:       "",
			wantComplete: false,
		},
		{
			name:         "whitespace only",
			output:       "   \n\t  ",
			wantComplete: false,
		},
		{
			name:         "error despite long output",
			output:       makeOutput(100),
			err:          &testErr{msg: "something failed"},
			wantComplete: false,
		},
		{
			name:         "cancelled despite long output",
			output:       makeOutput(100),
			cancelled:    true,
			wantComplete: false,
		},
		{
			name:           "budget exceeded despite long output",
			output:         makeOutput(100),
			budgetExceeded: true,
			wantComplete:   false,
		},
		{
			name:         "truncated despite long output",
			output:       makeOutput(100),
			truncated:    true,
			wantComplete: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &SubagentResult{
				Output:         tc.output,
				Error:          tc.err,
				Cancelled:      tc.cancelled,
				BudgetExceeded: tc.budgetExceeded,
				Truncated:      tc.truncated,
			}
			got := isOutputComplete(r)
			if got != tc.wantComplete {
				t.Errorf("isOutputComplete = %v, want %v (output len=%d, err=%v, cancelled=%v, budget=%v, truncated=%v)",
					got, tc.wantComplete, len(strings.TrimSpace(tc.output)),
					tc.err != nil, tc.cancelled, tc.budgetExceeded, tc.truncated)
			}
		})
	}
}

type testErr struct{ msg string }

func (e *testErr) Error() string { return e.msg }

// TestSubagentResult_OutputComplete_DefaultFalse verifies the zero value of
// OutputComplete is false — so if the field is never explicitly set (e.g.,
// in the panic-recovery or cancellation-timeout paths), the orchestrator
// correctly sees "incomplete" rather than accidentally seeing "complete".
func TestSubagentResult_OutputComplete_DefaultFalse(t *testing.T) {
	var r SubagentResult
	if r.OutputComplete {
		t.Error("zero-value OutputComplete should be false")
	}
}
