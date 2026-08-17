package tools

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setPackageRunSubagent points the package-level RunSubagentFunc at a
// marker-producing closure and registers a cleanup that restores the prior
// value under ToolFuncMu, so tests never leak package state.
func setPackageRunSubagent(t *testing.T, marker string) {
	t.Helper()
	ToolFuncMu.Lock()
	old := RunSubagentFunc
	RunSubagentFunc = func(context.Context, map[string]any) (string, error) {
		return marker, nil
	}
	ToolFuncMu.Unlock()
	t.Cleanup(func() {
		ToolFuncMu.Lock()
		RunSubagentFunc = old
		ToolFuncMu.Unlock()
	})
}

// TestToolEnvResolveToolFuncs_PrefersEnvSet verifies that a ToolEnv carrying
// a per-agent ToolFuncSet dispatches through that set even when the
// package-level var points at a different closure. This is the per-agent
// dispatch fix: in a daemon serving multiple agents, tool calls must route
// to the agent whose env they run under, not the most recently constructed
// one.
func TestToolEnvResolveToolFuncs_PrefersEnvSet(t *testing.T) {
	setPackageRunSubagent(t, "package-var-marker")

	envSet := &ToolFuncSet{
		RunSubagent: func(context.Context, map[string]any) (string, error) {
			return "env-set-marker", nil
		},
	}
	env := ToolEnv{ToolFuncs: envSet}

	h, ok := GetNewToolRegistry().Lookup("run_subagent")
	require.True(t, ok, "run_subagent handler must be registered")

	res, err := h.Execute(context.Background(), env, map[string]any{})
	require.NoError(t, err)
	require.False(t, res.IsError)
	assert.Equal(t, "env-set-marker", res.Output, "the env-carried set must win over the package var")
}

// TestToolEnvResolveToolFuncs_FallsBackToPackageVars verifies the legacy
// single-agent path: a ToolEnv with no ToolFuncs resolves a snapshot of the
// package-level vars, so callers that build ToolEnv directly (e.g.
// commit_handler's internal ToolEnv{}) keep working unchanged.
func TestToolEnvResolveToolFuncs_FallsBackToPackageVars(t *testing.T) {
	setPackageRunSubagent(t, "package-var-marker")

	h, ok := GetNewToolRegistry().Lookup("run_subagent")
	require.True(t, ok, "run_subagent handler must be registered")

	res, err := h.Execute(context.Background(), ToolEnv{}, map[string]any{})
	require.NoError(t, err)
	require.False(t, res.IsError)
	assert.Equal(t, "package-var-marker", res.Output, "nil env ToolFuncs must fall back to the package var")
}
