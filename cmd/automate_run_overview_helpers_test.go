//go:build !js

package cmd

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/automate"
)

// ---------------------------------------------------------------------------
// SP-128 Phase 2a: CLI overview helpers
//
// These tests pin the format of the new "External paths this workflow will
// access" section rendered by printAllowedPaths, plus the surfacing of
// summary.Warnings in the "Heads up" block via printWorkflowOverviewFromSummary.
// The acceptance criteria are:
//
//   - A workflow with `allowed_paths` shows the new section in the CLI output.
//   - A workflow with no `allowed_paths` produces identical CLI output to today.
// ---------------------------------------------------------------------------

// captureIO redirects os.Stdout AND os.Stderr for the duration of fn,
// returning the bytes that were written to either. Used to assert the
// rendered overview text, which mixes fmt.Println (stdout) with
// console.GlyphWarning.Printf (stderr).
func captureIO(t *testing.T, fn func()) (string, string) {
	t.Helper()

	oldOut := os.Stdout
	oldErr := os.Stderr
	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	rErr, wErr, err := os.Pipe()
	if err != nil {
		t.Fatalf("stderr pipe: %v", err)
	}
	os.Stdout = wOut
	os.Stderr = wErr

	doneOut := make(chan string, 1)
	doneErr := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, rOut)
		doneOut <- buf.String()
	}()
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, rErr)
		doneErr <- buf.String()
	}()

	fn()

	_ = wOut.Close()
	_ = wErr.Close()
	os.Stdout = oldOut
	os.Stderr = oldErr
	return <-doneOut, <-doneErr
}

func TestPrintAllowedPaths_RendersHeaderAndBullets(t *testing.T) {
	// Headline Phase 2a acceptance: the new section appears with bullets,
	// mode labels in brackets, and reasons on the next line.
	summary := &automate.Summary{
		AllowedPaths: []automate.AllowedPathSummary{
			{Path: "/srv/datasets", Mode: "read_write", Reason: "Read training data"},
			{Path: "/var/log/sprout", Mode: "read_only", Reason: "Tail logs for the run"},
		},
	}

	stdout, _ := captureIO(t, func() {
		printAllowedPaths(summary)
	})

	for _, want := range []string{
		"External paths this workflow will access:",
		"• /srv/datasets",
		"[read_write]",
		"\"Read training data\"",
		"• /var/log/sprout",
		"[read_only]",
		"\"Tail logs for the run\"",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("expected %q in rendered output, got:\n%s", want, stdout)
		}
	}
}

func TestPrintAllowedPaths_OmitsReasonWhenEmpty(t *testing.T) {
	// The reason is optional; an entry without one must not print an
	// empty quoted line.
	summary := &automate.Summary{
		AllowedPaths: []automate.AllowedPathSummary{
			{Path: "/data/in", Mode: "read_write"},
		},
	}

	stdout, _ := captureIO(t, func() {
		printAllowedPaths(summary)
	})

	if !strings.Contains(stdout, "• /data/in  [read_write]") {
		t.Errorf("expected bullet line with mode, got:\n%s", stdout)
	}
	// The bullet line should be the last non-empty line — no stray
	// quoted reason on a subsequent line.
	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	last := lines[len(lines)-1]
	if strings.Contains(last, "\"") {
		t.Errorf("expected last line to be the bullet (no quoted reason), got: %q", last)
	}
}

func TestPrintAllowedPaths_NoSectionWhenEmpty(t *testing.T) {
	// Acceptance: a workflow with no allowed_paths produces no new section.
	summary := &automate.Summary{} // AllowedPaths is nil

	stdout, _ := captureIO(t, func() {
		printAllowedPaths(summary)
	})

	if strings.Contains(stdout, "External paths") {
		t.Errorf("expected no section when no paths, got:\n%s", stdout)
	}
}

func TestPrintAllowedPaths_NoSectionWhenSummaryNil(t *testing.T) {
	stdout, _ := captureIO(t, func() {
		printAllowedPaths(nil)
	})
	if strings.Contains(stdout, "External paths") {
		t.Errorf("expected no section when summary is nil, got:\n%s", stdout)
	}
}

func TestPrintAllowedPaths_PadsShorterPathsToLongest(t *testing.T) {
	// The width-padding line up modes across entries. With a long path
	// and a short one, both should appear on their own line; the mode
	// labels should both be present (line-up is verified by substring
	// search since the exact padding depends on the longest path).
	summary := &automate.Summary{
		AllowedPaths: []automate.AllowedPathSummary{
			{Path: "/srv/datasets", Mode: "read_write"},
			{Path: "/var/log/sprout", Mode: "read_only"},
			{Path: "/x", Mode: "read_only"},
		},
	}

	stdout, _ := captureIO(t, func() {
		printAllowedPaths(summary)
	})

	// All three paths and all three mode labels must appear; that's the
	// stable contract for the line-up formatter.
	for _, want := range []string{"/srv/datasets", "/var/log/sprout", "/x", "read_write", "read_only"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("expected %q in output, got:\n%s", want, stdout)
		}
	}
}

func TestPrintWorkflowOverviewFromSummary_SurfacesWarnings(t *testing.T) {
	// Phase 2a: summary.Warnings is rendered inside the "Heads up" block,
	// one warning per line. The existing "Heads up:" line is still printed.
	summary := &automate.Summary{
		Description: "Test",
		Warnings: []string{
			"first warning: /etc is a system path",
			"second warning: /var is a system path",
		},
	}

	stdout, stderr := captureIO(t, func() {
		_ = printWorkflowOverviewFromSummary(summary, "test.json")
	})

	// GlyphWarning.Printf writes to stderr, not stdout — so the
	// warnings and the "Heads up" header are on stderr.
	combined := stdout + stderr
	if !strings.Contains(combined, "Heads up:") {
		t.Errorf("expected the existing Heads up line, got stdout=%q stderr=%q", stdout, stderr)
	}
	if !strings.Contains(combined, "first warning: /etc is a system path") {
		t.Errorf("expected first warning in output, got stdout=%q stderr=%q", stdout, stderr)
	}
	if !strings.Contains(combined, "second warning: /var is a system path") {
		t.Errorf("expected second warning in output, got stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestPrintWorkflowOverviewFromSummary_NoAllowedPathsHeaderWhenEmpty(t *testing.T) {
	// Acceptance: workflows with no allowed_paths produce output that does
	// not include the new section header.
	summary := &automate.Summary{
		Description: "Plain workflow",
	}

	stdout, _ := captureIO(t, func() {
		_ = printWorkflowOverviewFromSummary(summary, "plain.json")
	})

	if strings.Contains(stdout, "External paths this workflow will access:") {
		t.Errorf("expected no External paths section for plain workflow, got:\n%s", stdout)
	}
}
