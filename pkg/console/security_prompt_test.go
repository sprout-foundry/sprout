package console

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/utils"
)

// TestSecurityPromptHooksRegistered verifies that importing pkg/console
// wires the SelectList-backed pickers into utils.  Without this, the legacy
// "[y/n/a/e]" line prompt would still drive every approval — defeating the
// whole reason for the migration.
func TestSecurityPromptHooksRegistered(t *testing.T) {
	if utils.SecurityPromptHook == nil {
		t.Error("utils.SecurityPromptHook is nil — pkg/console init did not register it")
	}
	if utils.FilesystemSecurityPromptHook == nil {
		t.Error("utils.FilesystemSecurityPromptHook is nil — pkg/console init did not register it")
	}
}

// TestWriteSecurityHeader verifies the header includes the warning glyph,
// the prompt text, and the labeled target on its own indented block.
func TestWriteSecurityHeader(t *testing.T) {
	var buf bytes.Buffer
	writeSecurityHeader(&buf, "High-risk operation — approve to run", "Command", "rm -rf /tmp/foo")

	out := buf.String()
	cases := []string{
		"⚠",                                    // glyph
		"High-risk operation — approve to run", // prompt
		"Command",                              // label
		"rm -rf /tmp/foo",                      // target
	}
	for _, want := range cases {
		if !strings.Contains(out, want) {
			t.Errorf("expected header to contain %q, got:\n%s", want, out)
		}
	}
}

// TestWriteSecurityFootnote verifies that the dim footnote line renders the
// supplied caveat text.
func TestWriteSecurityFootnote(t *testing.T) {
	var buf bytes.Buffer
	writeSecurityFootnote(&buf, "Critical ops still block.")
	if !strings.Contains(buf.String(), "Critical ops still block.") {
		t.Errorf("expected footnote text in output, got: %q", buf.String())
	}
}

// TestSecurityApprovalBell verifies that a terminal bell (\a) is emitted
// when a security approval prompt is shown. SP-070-2.
func TestSecurityApprovalBell(t *testing.T) {
	var buf bytes.Buffer
	// The function writes \a + header + footnote, then fails on Run()
	// returning ApprovalChoiceDeny because the buffer is not a TTY.
	choice := askForSecurityApprovalWriter(&buf, "High-risk operation", "rm -rf /tmp/foo", nil)
	if choice != utils.ApprovalChoiceDeny {
		t.Logf("approval choice was %v (expected Deny on non-TTY)", choice)
	}
	out := buf.String()
	// The first byte should be the bell character \a (0x07)
	if len(out) == 0 {
		t.Fatal("expected output, got empty buffer")
	}
	if out[0] != '\a' {
		t.Errorf("expected first byte to be bell (\\a, 0x07), got 0x%02x (%q)", out[0], out[:min(len(out), 20)])
	}
}

// TestFilesystemSecurityApprovalBell verifies that a terminal bell (\a) is
// emitted when a filesystem security approval prompt is shown. SP-070-2.
func TestFilesystemSecurityApprovalBell(t *testing.T) {
	var buf bytes.Buffer
	choice := askForFilesystemSecurityApprovalWriter(&buf, "External path access", "/tmp/foo", "/tmp", utils.FilesystemPromptExternal)
	if choice != utils.ApprovalChoiceDeny {
		t.Logf("approval choice was %v (expected Deny on non-TTY)", choice)
	}
	out := buf.String()
	if len(out) == 0 {
		t.Fatal("expected output, got empty buffer")
	}
	if out[0] != '\a' {
		t.Errorf("expected first byte to be bell (\\a, 0x07), got 0x%02x (%q)", out[0], out[:min(len(out), 20)])
	}
}

// TestWriteSecurityAnalysisPanel verifies the panel renders the analysis
// summary, recommendation, and modifies string with the correct tone
// indicator. SP-124 Phase 3.
func TestWriteSecurityAnalysisPanel(t *testing.T) {
	cases := []struct {
		name    string
		view    *utils.SecurityAnalysisView
		want    []string // substrings that must appear in the rendered output
		noColor bool    // disable colorization to verify the fallback path
	}{
		{
			name: "approve recommendation renders check + green tone",
			view: &utils.SecurityAnalysisView{
				Summary:        "Removes only files matching /tmp/cache/*.",
				Modifies:       "files under /tmp/cache",
				RiskAssessment: "low",
				Recommendation: "approve",
			},
			want: []string{
				"Removes only files matching /tmp/cache/*.",
				"files under /tmp/cache",
				"Looks safe",
				"low",
			},
		},
		{
			name: "review recommendation renders amber tone",
			view: &utils.SecurityAnalysisView{
				Summary:        "Touches a tracked file outside CWD.",
				Modifies:       "/etc/hosts",
				RiskAssessment: "moderate",
				Recommendation: "review",
			},
			want: []string{
				"Touches a tracked file outside CWD.",
				"/etc/hosts",
				"Review needed",
				"moderate",
			},
		},
		{
			name: "reject recommendation renders red tone",
			view: &utils.SecurityAnalysisView{
				Summary:        "Drops a remote DB without a dry-run or backup.",
				Modifies:       "production Postgres on staging.example.com",
				RiskAssessment: "high",
				Recommendation: "reject",
			},
			want: []string{
				"Drops a remote DB without a dry-run or backup.",
				"production Postgres",
				"Recommend reject",
				"high",
			},
		},
		{
			name: "empty modifies is suppressed",
			view: &utils.SecurityAnalysisView{
				Summary:        "Read-only diagnostic.",
				Modifies:       "",
				RiskAssessment: "low",
				Recommendation: "approve",
			},
			want: []string{"Read-only diagnostic.", "Looks safe"},
		},
		{
			name:    "no-color path still renders all fields",
			view:    sampleAnalysis(),
			want:    []string{"Read-only diagnostic.", "Looks safe", "low"},
			noColor: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.noColor {
				t.Setenv("NO_COLOR", "1")
			}
			var buf bytes.Buffer
			writeSecurityAnalysisPanel(&buf, tc.view)
			out := buf.String()
			for _, want := range tc.want {
				if !strings.Contains(out, want) {
					t.Errorf("expected output to contain %q, got:\n%s", want, out)
				}
			}
		})
	}
}

// TestWriteSecurityAnalysisPanel_NilSafe verifies that a nil analysis does
// not panic and emits no output — callers gate the panel render on a
// non-nil analysis, but a defensive no-op is required since the picker
// runs in user-facing prompts and panic recovery would be intrusive.
func TestWriteSecurityAnalysisPanel_NilSafe(t *testing.T) {
	var buf bytes.Buffer
	writeSecurityAnalysisPanel(&buf, nil)
	if buf.Len() != 0 {
		t.Errorf("expected no output for nil analysis, got %q", buf.String())
	}
}

// TestAskForSecurityApproval_RendersAnalysisBeforePicker verifies that when
// an analysis is supplied, the rendered output places the panel BEFORE the
// picker footer. SP-124 Phase 3 — the user must see the LLM's reasoning
// at decision time, not after.
func TestAskForSecurityApproval_RendersAnalysisBeforePicker(t *testing.T) {
	var buf bytes.Buffer
	view := &utils.SecurityAnalysisView{
		Summary:        "Removes only the configured stale-build directory.",
		Modifies:       "/home/me/project/build",
		RiskAssessment: "low",
		Recommendation: "approve",
	}

	// Inject a stub picker that emits a footer marker to the writer and
	// returns Deny. We can't drive the real SelectList (TTY only), but the
	// writer is the same one the panel renders into, so positional checks
	// remain valid.
	prev := SetApprovalPickerForTest(func(w io.Writer, sl *SelectList) (string, bool) {
		fmt.Fprintln(w, "[picker footer marker]")
		return "", false
	})
	t.Cleanup(func() { SetApprovalPickerForTest(prev) })

	askForSecurityApprovalWriter(&buf, "High-risk operation", "rm -rf /home/me/project/build", view)
	out := buf.String()

	summaryIdx := strings.Index(out, "Removes only the configured stale-build directory.")
	footerIdx := strings.Index(out, "[picker footer marker]")

	if summaryIdx == -1 {
		t.Fatalf("expected summary to appear in output, got:\n%s", out)
	}
	if footerIdx == -1 {
		t.Fatalf("expected picker footer marker to appear in output, got:\n%s", out)
	}
	if summaryIdx > footerIdx {
		t.Errorf("summary must appear BEFORE picker footer (decision-time visibility):\n%s", out)
	}
}

// TestAskForSecurityApproval_OmitsPanelWhenNil verifies that omitting the
// analysis does NOT change the existing output beyond the new parameter.
// Existing CLI behavior (no panel rendered) is the contract.
func TestAskForSecurityApproval_OmitsPanelWhenNil(t *testing.T) {
	var buf bytes.Buffer
	askForSecurityApprovalWriter(&buf, "High-risk operation", "rm -rf /tmp/foo", nil)
	out := buf.String()
	if strings.Contains(out, "security analysis") {
		t.Errorf("expected no analysis panel when view is nil, got:\n%s", out)
	}
}

func sampleAnalysis() *utils.SecurityAnalysisView {
	return &utils.SecurityAnalysisView{
		Summary:        "Read-only diagnostic.",
		Modifies:       "",
		RiskAssessment: "low",
		Recommendation: "approve",
	}
}

// ────────────────────────────────────────────────────────────────────
// SP-124b Phase 2: chain stepper tests.
// ────────────────────────────────────────────────────────────────────

// TestWriteSecurityAnalysisPanel_LongChainCollapsed verifies that when
// the chain has more than 3 subcommands, the CLI stepper renders only
// the first 3 and shows a "(+N more)" affordance to cap terminal noise.
// Per spec: terminal-width-safe, truncate at 80 chars per subcommand.
func TestWriteSecurityAnalysisPanel_LongChainCollapsed(t *testing.T) {
	subs := []string{"git add -A", "git commit -m wip", "git push", "git tag v1", "git push --tags", "gh release create", "open URL", "rm -rf old"}
	tone := []string{"low", "low", "moderate", "low", "moderate", "moderate", "low", "high"}

	view := &utils.SecurityAnalysisView{
		Summary:              "Chain of 8 ops",
		Modifies:             ".git, URL",
		RiskAssessment:       "high",
		Recommendation:       "review",
		ChainLength:          len(subs),
		ChainSubcommands:     subs,
		ChainClassifications: tone,
	}

	var buf bytes.Buffer
	writeSecurityAnalysisPanel(&buf, view)
	out := buf.String()

	// First three subcommands must appear in order.
	idx1 := strings.Index(out, "git add -A")
	idx2 := strings.Index(out, "git commit -m wip")
	idx3 := strings.Index(out, "git push")
	if idx1 == -1 || idx2 == -1 || idx3 == -1 {
		t.Fatalf("expected first 3 subcommands to appear, got:\n%s", out)
	}
	if !(idx1 < idx2 && idx2 < idx3) {
		t.Errorf("first 3 subcommands should appear in order:\n%s", out)
	}

	// The 4th subcommand must NOT appear (collapsed).
	if strings.Contains(out, "git tag v1") {
		t.Errorf("4th subcommand should be collapsed, got:\n%s", out)
	}

	// The "+5 more" affordance must appear (8 total - 3 shown = 5 more).
	if !strings.Contains(out, "+5 more") {
		t.Errorf("expected '+5 more' affordance, got:\n%s", out)
	}

	// Each rendered subcommand line carries a colored risk dot prefix.
	// We don't assert exact ANSI bytes (colorblind/no-color both produce
	// glyphs), but we do require the dot glyph (●) before each visible
	// subcommand. Three visible dots → exactly three ● instances in
	// the stepper region. (No-color path uses the same unicode glyph.)
	dotsBeforeFirst := strings.Count(out[:idx3], "\u25cf")
	if dotsBeforeFirst < 3 {
		t.Errorf("expected at least 3 risk-dot glyphs before the 3rd subcommand, got %d:\n%s", dotsBeforeFirst, out)
	}
}

// TestWriteSecurityAnalysisPanel_ShortChainAllRendered verifies that a
// 3-subcommand chain (the breakpoint) renders all subcommands — no
// collapsing — so users see every step when the chain fits.
func TestWriteSecurityAnalysisPanel_ShortChainAllRendered(t *testing.T) {
	subs := []string{"echo a", "echo b", "echo c"}
	tone := []string{"low", "low", "low"}

	view := &utils.SecurityAnalysisView{
		Summary:              "Chain of 3 ops",
		RiskAssessment:       "low",
		Recommendation:       "approve",
		ChainLength:          3,
		ChainSubcommands:     subs,
		ChainClassifications: tone,
	}

	var buf bytes.Buffer
	writeSecurityAnalysisPanel(&buf, view)
	out := buf.String()

	for _, sub := range subs {
		if !strings.Contains(out, sub) {
			t.Errorf("expected subcommand %q to appear, got:\n%s", sub, out)
		}
	}
	if strings.Contains(out, "more") {
		t.Errorf("short chain should not show '+N more' affordance, got:\n%s", out)
	}
}

// TestWriteSecurityAnalysisPanel_NoChainForSingle is the regression
// guard: when ChainLength=0 (single command) or ChainSubcommands is
// nil/empty, the panel must render identically to the legacy
// no-stepper output. SP-124 Phase 3 contract.
func TestWriteSecurityAnalysisPanel_NoChainForSingle(t *testing.T) {
	cases := []struct {
		name string
		view *utils.SecurityAnalysisView
	}{
		{
			name: "chain length zero (single command path)",
			view: &utils.SecurityAnalysisView{
				Summary:        "Read-only diagnostic.",
				RiskAssessment: "low",
				Recommendation: "approve",
				ChainLength:    0,
			},
		},
		{
			name: "chain subcommands nil (legacy path)",
			view: sampleAnalysis(),
		},
		{
			name: "chain subcommands empty array (defensive)",
			view: &utils.SecurityAnalysisView{
				Summary:          "Read-only diagnostic.",
				RiskAssessment:   "low",
				Recommendation:   "approve",
				ChainLength:      0,
				ChainSubcommands: []string{},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			writeSecurityAnalysisPanel(&buf, tc.view)
			out := buf.String()

			// Stepper markers must NOT appear.
			if strings.Contains(out, "Chain (") {
				t.Errorf("single-command panel should not show stepper header, got:\n%s", out)
			}
			if strings.Contains(out, "+N more") || strings.Contains(out, "+0 more") {
				t.Errorf("single-command panel should not show collapse affordance, got:\n%s", out)
			}

			// Existing Phase 3 content must still render (regression guard).
			if !strings.Contains(out, "Read-only diagnostic.") {
				t.Errorf("expected legacy summary to render, got:\n%s", out)
			}
			if !strings.Contains(out, "Looks safe") {
				t.Errorf("expected legacy recommendation badge to render, got:\n%s", out)
			}
		})
	}
}

// TestWriteSecurityAnalysisPanel_TruncatesLongSubcommand verifies that
// subcommands longer than 80 chars are truncated with an ellipsis so
// the panel stays readable on narrow terminals. Per spec: "truncate at
// 80 chars per subcommand". Uses ChainLength>1 so the stepper actually
// renders.
func TestWriteSecurityAnalysisPanel_TruncatesLongSubcommand(t *testing.T) {
	longCmd := strings.Repeat("very-long-token-", 10) // 150 chars
	view := &utils.SecurityAnalysisView{
		Summary:              "Chain of 2",
		RiskAssessment:       "low",
		Recommendation:       "approve",
		ChainLength:          2,
		ChainSubcommands:     []string{longCmd, "echo done"},
		ChainClassifications: []string{"low", "low"},
	}

	var buf bytes.Buffer
	writeSecurityAnalysisPanel(&buf, view)
	out := buf.String()

	// Full long command must NOT appear verbatim.
	if strings.Contains(out, longCmd) {
		t.Errorf("long subcommand should be truncated, got:\n%s", out)
	}
	// Truncation marker must appear (we use U+2026 unicode horizontal
	// ellipsis "…" since dots have to share line space with the
	// risk-glyph ● and the cell padding).
	if !strings.Contains(out, "…") && !strings.Contains(out, "...") {
		t.Errorf("expected truncation marker (… or ...), got:\n%s", out)
	}
}

// TestWriteSecurityAnalysisPanel_ChainRendersBeforePicker verifies the
// positional contract: the stepper must appear ABOVE the picker (so the
// user sees chain context before deciding). Mirrors Phase 3's panel-
// before-picker test.
func TestWriteSecurityAnalysisPanel_ChainRendersBeforePicker(t *testing.T) {
	view := &utils.SecurityAnalysisView{
		Summary:              "Chain does X",
		RiskAssessment:       "moderate",
		Recommendation:       "review",
		ChainLength:          2,
		ChainSubcommands:     []string{"echo a", "echo b"},
		ChainClassifications: []string{"low", "moderate"},
	}

	var buf bytes.Buffer
	prev := SetApprovalPickerForTest(func(w io.Writer, sl *SelectList) (string, bool) {
		fmt.Fprintln(w, "[picker footer marker]")
		return "", false
	})
	t.Cleanup(func() { SetApprovalPickerForTest(prev) })

	askForSecurityApprovalWriter(&buf, "High-risk operation", "echo a && echo b", view)
	out := buf.String()

	stepperIdx := strings.Index(out, "echo a")
	footerIdx := strings.Index(out, "[picker footer marker]")
	if stepperIdx == -1 {
		t.Fatalf("expected stepper to appear in output, got:\n%s", out)
	}
	if footerIdx == -1 {
		t.Fatalf("expected picker footer marker to appear, got:\n%s", out)
	}
	if stepperIdx > footerIdx {
		t.Errorf("stepper must appear BEFORE picker footer:\n%s", out)
	}
}
