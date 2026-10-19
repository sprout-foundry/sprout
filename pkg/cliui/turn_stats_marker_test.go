//go:build !js

package cliui

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
)

// captureTurnEnd redirects the TTY-gated turn-end output into a buffer
// for the duration of one test.
func captureTurnEnd(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	restore := SetTurnEndSinkForTest(&buf)
	t.Cleanup(restore)
	return &buf
}

// TestPrintTurnEndMarker_PrintsClosingMarker pins the 162b contract: the
// short closing marker prints whenever a turn ended without token
// movement, and it reads as "the turn is over".
func TestPrintTurnEndMarker_PrintsClosingMarker(t *testing.T) {
	buf := captureTurnEnd(t)
	PrintTurnEndMarker()
	out := strings.Trim(buf.String(), "\n")
	plain := stripANSI(out)
	if !strings.Contains(plain, "turn over") {
		t.Errorf("closing marker missing from output: %q", out)
	}
	if !strings.HasPrefix(plain, "⎯") && !strings.HasPrefix(plain, "-") {
		t.Errorf("marker should open with the rule glyph: %q", out)
	}
}

// stripANSI removes ANSI escape sequences so assertions read plain text.
func stripANSI(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		if inEsc {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
			continue
		}
		if r == '\033' {
			inEsc = true
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// TestPrintPerTurnSummary_ZeroDelta_PrintsMarkerInsteadOfStats covers the
// fast-path / zero-token / failed-before-any-token shape: the early-return
// that used to print nothing must now print the closing marker.
func TestPrintPerTurnSummary_ZeroDelta_PrintsMarkerInsteadOfStats(t *testing.T) {
	a := newTurnStatsTestAgent(t)
	buf := captureTurnEnd(t)

	PrintPerTurnSummary(a, time.Now().Add(-time.Second), 0, 0, 0)

	out := buf.String()
	if !strings.Contains(out, "turn over") {
		t.Errorf("zero-token turn must still print the closing marker, got: %q", out)
	}
	if strings.Contains(out, "this turn:") {
		t.Errorf("stats segments must be absent for a zero-token turn: %q", out)
	}
}

// TestPrintPerTurnSummary_TokenDelta_PrintsStatsLine pins the unchanged
// full stats line for turns that consumed tokens.
func TestPrintPerTurnSummary_TokenDelta_PrintsStatsLine(t *testing.T) {
	a := newTurnStatsTestAgent(t)
	a.TrackMetricsFromResponse(1200, 300, 1500, 0, 0, 0, 0, 0)
	buf := captureTurnEnd(t)

	PrintPerTurnSummary(a, time.Now().Add(-2*time.Second), 0, 0, 0)

	out := buf.String()
	if !strings.Contains(out, "this turn:") {
		t.Errorf("token-consuming turn must print the stats line, got: %q", out)
	}
	if !strings.Contains(out, "1.2k in") || !strings.Contains(out, "300 out") {
		t.Errorf("stats line should carry token deltas, got: %q", out)
	}
}

// TestPrintPerTurnSummary_SinkBypassesTTYGate proves the sink capture
// works even though the test process has no TTY — the sink short-circuits
// the TTY gate rather than the gate being removed.
func TestPrintPerTurnSummary_SinkBypassesTTYGate(t *testing.T) {
	if ShouldShowTurnStats() {
		t.Skip("stderr is a TTY in this environment; the gate cannot be observed")
	}
	buf := captureTurnEnd(t)

	PrintTurnEndMarker()

	if buf.Len() == 0 {
		t.Fatal("sink capture should bypass the TTY gate")
	}
}

// newTurnStatsTestAgent builds an isolated agent whose token counters
// start at zero; the test client is auto-selected under go test.
func newTurnStatsTestAgent(t *testing.T) *agent.Agent {
	t.Helper()
	t.Setenv("SPROUT_CONFIG", t.TempDir())
	a, err := agent.NewAgentWithModel("test:test")
	if err != nil {
		t.Fatalf("newTurnStatsTestAgent: %v", err)
	}
	return a
}
