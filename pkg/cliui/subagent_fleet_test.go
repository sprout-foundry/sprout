//go:build !js

package cliui

import (
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/stretchr/testify/require"
)

func fleetEvent(taskID, persona, tool string) map[string]interface{} {
	return map[string]interface{}{
		agent.EventMetaSubagentTaskID:     taskID,
		agent.EventMetaSubagentConcurrent: true,
		"active_persona":                  persona,
		"tool_name":                       tool,
	}
}

// Same-persona workers are told apart by run, in the order they appeared,
// and a run between tools reads as thinking.
func TestSubagentFleet_LabelNamesEachRun(t *testing.T) {
	var f subagentFleet
	f.toolStarted(fleetEvent("auth", "coder", "read_file"))
	f.toolStarted(fleetEvent("ui", "coder", "grep"))
	label := f.toolEnded(fleetEvent("auth", "coder", "read_file"))
	require.Equal(t, "2 subagents · coder:auth thinking · coder:ui grep", label)

	require.Equal(t, 1, f.finished("auth"))
	require.Equal(t, "1 subagent · coder:ui grep", f.label())
	require.Equal(t, 0, f.finished("ui"))
	require.Equal(t, 0, f.finished("never-seen"), "finishing an unknown run is harmless")
}

func TestIsConcurrentSubagentEvent(t *testing.T) {
	require.True(t, isConcurrentSubagentEvent(fleetEvent("auth", "coder", "grep")))
	require.False(t, isConcurrentSubagentEvent(map[string]interface{}{
		agent.EventMetaSubagentTaskID: "subagent-1", agent.EventMetaSubagentConcurrent: false,
	}), "a foreground run prints its own tool lines")
	require.False(t, isConcurrentSubagentEvent(map[string]interface{}{"tool_name": "grep"}), "the primary's own events")
}

func TestSubagentFleet_ResetClearsRuns(t *testing.T) {
	var f subagentFleet
	f.toolStarted(fleetEvent("auth", "coder", "grep"))
	f.reset()
	require.False(t, f.has("auth"))
	require.Equal(t, "0 subagents", f.label())
}
