package console

import (
	"bytes"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/utils"
)

// SP-161a: the chain stepper must never show a subcommand only in clipped
// form. A clipped line is followed by continuation lines carrying the full
// text, and a collapsed chain states the omission explicitly.

// continuationText extracts the "↪ " continuation lines from rendered
// stepper output and joins their payloads, so a test can assert the full
// subcommand text survives the clip.
func continuationText(out string) string {
	var parts []string
	for _, line := range strings.Split(out, "\n") {
		plain := stripANSIEscapeCodes(line)
		if idx := strings.Index(plain, chainContinuationPrefix); idx >= 0 {
			parts = append(parts, strings.TrimSpace(plain[idx+len(chainContinuationPrefix):]))
		}
	}
	return strings.Join(parts, " ")
}

func TestChainStepper_ClippedSubcommandSpellsOutFullText(t *testing.T) {
	longCmd := "kubectl apply -f " + strings.Repeat("very-long-token-", 8) + ".yaml"
	view := &utils.SecurityAnalysisView{
		Summary:              "Chain of 2",
		RiskAssessment:       "moderate",
		Recommendation:       "review",
		ChainLength:          2,
		ChainSubcommands:     []string{longCmd, "echo done"},
		ChainClassifications: []string{"low", "low"},
	}

	var buf bytes.Buffer
	writeSecurityAnalysisChainStepper(&buf, view)
	out := buf.String()

	if !strings.Contains(out, "…") {
		t.Errorf("expected the stepper line to be clipped with an ellipsis, got:\n%s", out)
	}

	reconstructed := continuationText(out)
	if stripAllSpaces(reconstructed) != stripAllSpaces(longCmd) {
		t.Errorf("continuation lines must carry the full subcommand.\nwant: %q\ngot:  %q\noutput:\n%s", longCmd, reconstructed, out)
	}
}

// stripAllSpaces removes every whitespace run so chunks that were
// hard-split mid-token compare equal to the original text.
func stripAllSpaces(s string) string {
	return strings.Join(strings.Fields(s), "")
}

func TestChainStepper_CollapsedChainStatesOmissionExplicitly(t *testing.T) {
	subs := []string{"git add -A", "git commit -m wip", "git push", "git tag v1", "git push --tags", "gh release create"}
	view := &utils.SecurityAnalysisView{
		Summary:              "Chain of 6",
		RiskAssessment:       "moderate",
		Recommendation:       "review",
		ChainLength:          len(subs),
		ChainSubcommands:     subs,
		ChainClassifications: []string{"low", "low", "low", "low", "low", "low"},
	}

	var buf bytes.Buffer
	writeSecurityAnalysisChainStepper(&buf, view)
	out := buf.String()

	if !strings.Contains(out, "+3 more") {
		t.Errorf("expected the collapse marker to name the hidden count, got:\n%s", out)
	}
	if !strings.Contains(out, "full command") {
		t.Errorf("expected the collapse marker to point at the full command above, got:\n%s", out)
	}
	// Hidden entries must not render.
	if strings.Contains(out, "git tag v1") {
		t.Errorf("hidden subcommand should not appear in the stepper:\n%s", out)
	}
}

func TestChainStepper_NoContinuationLinesWhenEverythingFits(t *testing.T) {
	view := &utils.SecurityAnalysisView{
		Summary:              "Chain of 2",
		RiskAssessment:       "low",
		Recommendation:       "approve",
		ChainLength:          2,
		ChainSubcommands:     []string{"echo a", "echo b"},
		ChainClassifications: []string{"low", "low"},
	}

	var buf bytes.Buffer
	writeSecurityAnalysisChainStepper(&buf, view)
	out := buf.String()

	if strings.Contains(out, chainContinuationPrefix) {
		t.Errorf("unclipped stepper must not render continuation lines, got:\n%s", out)
	}
	if strings.Contains(out, "+0 more") || strings.Contains(out, "more steps") {
		t.Errorf("unclipped, uncollapsed stepper must not state an omission, got:\n%s", out)
	}
	for _, sub := range view.ChainSubcommands {
		if !strings.Contains(out, sub) {
			t.Errorf("expected %q in output, got:\n%s", sub, out)
		}
	}
}

func TestWrapChainContinuation(t *testing.T) {
	cases := []struct {
		name  string
		in    string
		width int
		want  []string
	}{
		{"fits", "echo hi", 80, []string{"echo hi"}},
		{"zero width passes through", "abc", 0, []string{"abc"}},
		{"hard split no spaces", strings.Repeat("x", 25), 10, []string{"xxxxxxxxxx", "xxxxxxxxxx", "xxxxx"}},
		{
			"word boundary preferred",
			"alpha beta gamma delta",
			11,
			[]string{"alpha beta", "gamma delta"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := wrapChainContinuation(tc.in, tc.width)
			if len(got) != len(tc.want) {
				t.Fatalf("wrapChainContinuation(%q, %d) = %v, want %v", tc.in, tc.width, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("chunk %d = %q, want %q", i, got[i], tc.want[i])
				}
			}
			// Word-wrapped chunks drop the broken space; hard-split
			// chunks concatenate directly. Compare space-stripped.
			joined := stripAllSpaces(strings.Join(got, ""))
			if joined != stripAllSpaces(tc.in) {
				t.Errorf("chunks must reconstruct the input: %q != %q", joined, stripAllSpaces(tc.in))
			}
		})
	}
}
