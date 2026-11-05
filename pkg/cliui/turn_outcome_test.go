//go:build !js

package cliui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/plancontract"
	"github.com/sprout-foundry/sprout/pkg/verify"
)

// turnOutcomeCases drives ResolveTurnOutcome: the termination reason wins
// over the error (a stop is not a failure), then classified errors fail
// the turn, then a failed verification.
func TestResolveTurnOutcome(t *testing.T) {
	passed := &verify.Result{Checks: []verify.Check{{Kind: kindForTest("build"), Passed: true}}}
	failed := &verify.Result{Checks: []verify.Check{{Kind: kindForTest("build"), Passed: false}}}

	cases := []struct {
		name  string
		facts TurnOutcomeFacts
		want  TurnOutcome
	}{
		{
			name:  "plain success",
			facts: TurnOutcomeFacts{},
			want:  TurnSucceeded,
		},
		{
			name: "classified error fails",
			facts: TurnOutcomeFacts{
				Err: agenterrors.NewNetwork("dial failed", errors.New("connection refused")),
			},
			want: TurnFailed,
		},
		{
			name: "interrupt wins over interrupt-shaped error",
			facts: TurnOutcomeFacts{
				Err:               fmt.Errorf("query interrupted: %w", errors.New("context canceled")),
				TerminationReason: agent.RunTerminationInterrupted,
			},
			want: TurnStopped,
		},
		{
			name: "max iterations stops",
			facts: TurnOutcomeFacts{
				TerminationReason: agent.RunTerminationMaxIterations,
			},
			want: TurnStopped,
		},
		{
			name: "fleet budget stops",
			facts: TurnOutcomeFacts{
				TerminationReason: agent.RunTerminationFleetBudgetExceeded,
			},
			want: TurnStopped,
		},
		{
			name: "failed verification fails the turn",
			facts: TurnOutcomeFacts{
				Verification: failed,
			},
			want: TurnFailed,
		},
		{
			name: "passed verification succeeds",
			facts: TurnOutcomeFacts{
				Verification: passed,
			},
			want: TurnSucceeded,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveTurnOutcome(tc.facts); got != tc.want {
				t.Errorf("ResolveTurnOutcome() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestFormatTurnOutcomeLine_FailurePhrasing pins the user's ask: a
// classified error becomes a plain sentence, never a raw exception.
func TestFormatTurnOutcomeLine_FailurePhrasing(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "typed network error",
			err:  agenterrors.NewNetwork("dial tcp: connection refused", errors.New("connection refused")),
			want: "network difficulty with the model — gave up",
		},
		{
			name: "legacy transient error",
			err:  agenterrors.NewTransientError("request failed", errors.New("timeout")),
			want: "network difficulty with the model — gave up",
		},
		{
			name: "rate limited",
			err:  agenterrors.NewRateLimitError("quota", errors.New("429"), "openrouter"),
			want: "the provider rate-limited the model — gave up",
		},
		{
			name: "context overflow",
			err:  agenterrors.NewContextError("too long", errors.New("context length exceeded")),
			want: "the conversation outgrew the model's context window",
		},
		{
			name: "security block",
			err:  agenterrors.NewSecurityError("blocked", errors.New("denied")),
			want: "a security rule blocked the work",
		},
		{
			name: "provider auth",
			err:  agenterrors.NewProviderError("provider", errors.New("401 unauthorized"), "p", "m"),
			want: "the provider rejected the credentials",
		},
		{
			name: "provider overload",
			err:  agenterrors.NewProviderError("provider", errors.New("503 overloaded"), "p", "m"),
			want: "the provider is overloaded — gave up",
		},
		{
			name: "timeout code",
			err:  agenterrors.NewTimeout("model call", 30*time.Second),
			want: "the model took too long to respond — gave up",
		},
		{
			name: "approval gate",
			err:  agenterrors.NewApproval("not approved", nil),
			want: "an approval gate blocked the work",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			line := stripANSI(FormatTurnOutcomeLine(TurnOutcomeFacts{Err: tc.err}))
			if !strings.Contains(line, "Turn failed") {
				t.Errorf("line should state the failure, got: %q", line)
			}
			if !strings.Contains(line, tc.want) {
				t.Errorf("line should carry the plain-language cause %q, got: %q", tc.want, line)
			}
			if strings.Contains(line, tc.err.Error()) {
				t.Errorf("raw exception text must not leak into the outcome line: %q", line)
			}
		})
	}
}

// TestFormatTurnOutcomeLine_UnclassifiedErrorStaysQuiet pins the
// "do not invent detail" rule: an unclassified error names the failure
// without fabricating a cause (the error block above already showed it).
func TestFormatTurnOutcomeLine_UnclassifiedErrorStaysQuiet(t *testing.T) {
	err := errors.New("something unexpected happened")
	line := stripANSI(FormatTurnOutcomeLine(TurnOutcomeFacts{Err: err}))
	if !strings.Contains(line, "Turn failed") {
		t.Errorf("line should state the failure, got: %q", line)
	}
	if strings.Contains(line, "something unexpected") {
		t.Errorf("unclassified cause should not be invented into the outcome line: %q", line)
	}
}

// TestFormatTurnOutcomeLine_Success covers the three success shapes:
// verified-done uses the SP-149 summary verbatim, a plain turn states the
// deterministic facts, and a vacuous verification claims nothing.
func TestFormatTurnOutcomeLine_Success(t *testing.T) {
	t.Run("with passing verification", func(t *testing.T) {
		res := &verify.Result{
			PlanRevision: 2,
			Checks: []verify.Check{
				{Kind: kindForTest("build"), Passed: true, Command: "go build ./..."},
				{Kind: kindForTest("test"), Passed: true, Command: "go test ./..."},
			},
		}
		line := stripANSI(FormatTurnOutcomeLine(TurnOutcomeFacts{Verification: res}))
		if !strings.Contains(line, "Turn succeeded") {
			t.Errorf("line should state success, got: %q", line)
		}
		if !strings.Contains(line, res.Summary()) {
			t.Errorf("verification summary should be used verbatim, got: %q", line)
		}
	})
	t.Run("with changed files and duration", func(t *testing.T) {
		line := stripANSI(FormatTurnOutcomeLine(TurnOutcomeFacts{
			ChangedPaths: []string{"pkg/a/auth.go", "pkg/a/auth_test.go"},
			Duration:     12 * time.Second,
		}))
		if !strings.Contains(line, "Turn succeeded") {
			t.Errorf("line should state success, got: %q", line)
		}
		// Changed files are named by basename so the line stays short.
		if !strings.Contains(line, "auth.go") || !strings.Contains(line, "auth_test.go") {
			t.Errorf("line should name the changed files, got: %q", line)
		}
		if !strings.Contains(line, "12") {
			t.Errorf("line should state the turn duration, got: %q", line)
		}
	})
	t.Run("with many changed files collapses to a count", func(t *testing.T) {
		line := stripANSI(FormatTurnOutcomeLine(TurnOutcomeFacts{
			ChangedPaths: []string{"a.go", "b.go", "c.go", "d.go"},
		}))
		if !strings.Contains(line, "changed 4 files") {
			t.Errorf("count form expected, got: %q", line)
		}
	})
	t.Run("vacuous verification claims nothing", func(t *testing.T) {
		res := &verify.Result{Checks: []verify.Check{{Kind: kindForTest("build"), Skipped: true}}}
		line := stripANSI(FormatTurnOutcomeLine(TurnOutcomeFacts{Verification: res}))
		if !strings.Contains(line, "nothing was verified") {
			t.Errorf("vacuous verification must not read as verified, got: %q", line)
		}
	})
}

// TestFormatTurnOutcomeLine_FailedVerification uses the SP-149 result
// rather than restating anything: a failing verification fails the turn
// and carries the summary verbatim.
func TestFormatTurnOutcomeLine_FailedVerification(t *testing.T) {
	res := &verify.Result{
		PlanRevision: 3,
		Checks:       []verify.Check{{Kind: kindForTest("test"), Passed: false, Command: "go test ./..."}},
	}
	line := stripANSI(FormatTurnOutcomeLine(TurnOutcomeFacts{Verification: res}))
	if !strings.Contains(line, "Turn failed") {
		t.Errorf("failing verification must fail the turn, got: %q", line)
	}
	if !strings.Contains(line, res.Summary()) {
		t.Errorf("verification summary should be carried verbatim, got: %q", line)
	}
}

// TestFormatTurnOutcomeLine_Stopped covers the partway endings: each
// termination reason says the turn stopped and why.
func TestFormatTurnOutcomeLine_Stopped(t *testing.T) {
	cases := []struct {
		name  string
		facts TurnOutcomeFacts
		want  string
	}{
		{
			name: "interrupted",
			facts: TurnOutcomeFacts{
				TerminationReason: agent.RunTerminationInterrupted,
				Duration:          5 * time.Second,
			},
			want: "Turn stopped — interrupted",
		},
		{
			name: "max iterations",
			facts: TurnOutcomeFacts{
				TerminationReason: agent.RunTerminationMaxIterations,
				MaxIterations:     25,
			},
			want: "Turn stopped — reached the iteration limit (25)",
		},
		{
			name: "fleet budget",
			facts: TurnOutcomeFacts{
				TerminationReason: agent.RunTerminationFleetBudgetExceeded,
			},
			want: "Turn stopped — the fleet token budget ran out",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			line := stripANSI(FormatTurnOutcomeLine(tc.facts))
			if !strings.Contains(line, tc.want) {
				t.Errorf("expected %q in line, got: %q", tc.want, line)
			}
		})
	}
}

// TestPrintTurnOutcome_SinkCapture proves the outcome line reaches the
// turn-end sink so the REPL turn end reads outcome-then-marker.
func TestPrintTurnOutcome_SinkCapture(t *testing.T) {
	buf := captureTurnEnd(t)
	PrintTurnOutcome(TurnOutcomeFacts{
		Err:      agenterrors.NewNetwork("dial failed", errors.New("refused")),
		Duration: 3 * time.Second,
	})
	out := stripANSI(buf.String())
	if !strings.Contains(out, "Turn failed") || !strings.Contains(out, "network difficulty") {
		t.Errorf("outcome line should print the plain failure, got: %q", out)
	}
}

// kindForTest wraps plancontract.Kind so check literals stay short.
func kindForTest(s string) plancontract.Kind {
	return plancontract.Kind(s)
}
