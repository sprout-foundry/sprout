package subagents

import (
	"errors"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent/changes"
)

func TestCompactCount(t *testing.T) {
	cases := map[int]string{0: "0", 999: "999", 1000: "1k", 12500: "12.5k", 1_000_000: "1M", 1_500_000: "1.5M"}
	for in, want := range cases {
		if got := CompactCount(in); got != want {
			t.Errorf("CompactCount(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestSubagentStatSuffix(t *testing.T) {
	// Empty result → no suffix.
	if got := subagentStatSuffix(&SubagentResult{}); got != "" {
		t.Errorf("empty result suffix = %q, want \"\"", got)
	}
	res := &SubagentResult{
		TokensUsed:  12500,
		Cost:        0.0234,
		ToolCalls:   4,
		Elapsed:     8100 * time.Millisecond,
		FileChanges: []changes.TrackedFileChange{{}, {}, {}},
	}
	got := subagentStatSuffix(res)
	want := " · 3 files · 12.5k tok · $0.02 · 4 tools · 8.1s"
	if got != want {
		t.Errorf("suffix = %q, want %q", got, want)
	}
	// Singular file/tool.
	if got := subagentStatSuffix(&SubagentResult{FileChanges: []changes.TrackedFileChange{{}}, ToolCalls: 1}); got != " · 1 file · 1 tool" {
		t.Errorf("singular suffix = %q", got)
	}
}

func TestPrintSubagentDone_NilSafe(t *testing.T) {
	PrintSubagentDone("coder", nil)                                        // must not panic
	PrintSubagentDone("coder", &SubagentResult{Error: errors.New("boom")}) // error path
}
