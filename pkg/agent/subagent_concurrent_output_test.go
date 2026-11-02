package agent

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestIsConcurrentSubagentRun(t *testing.T) {
	require.False(t, isConcurrentSubagentRun("subagent-1730000000"), "foreground")
	require.True(t, isConcurrentSubagentRun("auth"), "parallel task id")
	require.True(t, isConcurrentSubagentRun("bg-subagent-3"), "background")
}

func TestConcurrentSubagentBlock_TailOfResult(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	out := strings.Join([]string{"l1", "l2", "l3", "l4", "l5", "l6", "l7", "l8"}, "\n")
	block := formatConcurrentSubagentBlock("coder", "auth", &SubagentResult{
		Output: out, ToolCalls: 4, Elapsed: 2 * time.Second,
	})
	lines := strings.Split(block, "\n")
	require.Contains(t, lines[0], "subagent [coder:auth] done", "the run is named, not just its persona")
	require.Equal(t, "    … 2 earlier lines in the result", lines[1])
	require.Equal(t, []string{"    l3", "    l4", "    l5", "    l6", "    l7", "    l8"}, lines[2:])
}

func TestConcurrentSubagentBlock_FailureWithoutOutput(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	block := formatConcurrentSubagentBlock("reviewer", "ui", &SubagentResult{Error: errors.New("model refused")})
	require.NotContains(t, block, "\n", "a failed run with nothing to show is one line")
	require.Contains(t, block, "subagent [reviewer:ui] failed: model refused")
}
