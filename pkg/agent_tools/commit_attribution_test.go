package tools

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingAttribution returns an AttributeCommitMessage hook that appends a
// fixed trailer and records whether the message was generated.
func recordingAttribution(generated *[]bool) func(string, bool) string {
	return func(message string, gen bool) string {
		*generated = append(*generated, gen)
		return message + "\n\nAssisted-by: sprout/dev (test/model)"
	}
}

func TestCommitHandler_AppliesAttributionToExplicitMessage(t *testing.T) {
	dir := newTestGitRepo(t, true)
	var generated []bool
	res, err := (&commitHandler{}).Execute(context.Background(), ToolEnv{
		WorkspaceRoot: dir,
		ToolFuncs:     &ToolFuncSet{AttributeCommitMessage: recordingAttribution(&generated)},
	}, map[string]any{"message": "fix: explicit"})
	require.NoError(t, err)
	require.False(t, res.IsError, "tool output: %s", res.Output)
	assert.Equal(t, []bool{false}, generated)
	assert.Equal(t, "fix: explicit\n\nAssisted-by: sprout/dev (test/model)", gitInRepo(t, dir, "log", "-1", "--format=%B"))
}

func TestCommitHandler_AppliesAttributionToGeneratedMessage(t *testing.T) {
	dir := newTestGitRepo(t, true)
	var generated []bool
	gen := func([]byte, string) (string, error) { return "feat: generated", nil }
	res, err := (&commitHandler{}).Execute(context.Background(), ToolEnv{
		WorkspaceRoot: dir,
		ToolFuncs: &ToolFuncSet{
			GenerateCommitMessage:  gen,
			AttributeCommitMessage: recordingAttribution(&generated),
		},
	}, map[string]any{"notes": "n"})
	require.NoError(t, err)
	require.False(t, res.IsError, "tool output: %s", res.Output)
	assert.Equal(t, []bool{true}, generated)
	assert.Equal(t, "feat: generated\n\nAssisted-by: sprout/dev (test/model)", gitInRepo(t, dir, "log", "-1", "--format=%B"))
}

func TestCommitHandler_NoAttributionHookCommitsUnchanged(t *testing.T) {
	dir := newTestGitRepo(t, true)
	res, err := (&commitHandler{}).Execute(context.Background(), ToolEnv{
		WorkspaceRoot: dir,
		ToolFuncs:     &ToolFuncSet{},
	}, map[string]any{"message": "fix: plain"})
	require.NoError(t, err)
	require.False(t, res.IsError, "tool output: %s", res.Output)
	assert.Equal(t, "fix: plain", gitInRepo(t, dir, "log", "-1", "--format=%B"))
}

func TestGitHandler_CommitAppliesAttribution(t *testing.T) {
	dir := newTestGitRepo(t, true)
	var generated []bool
	res, err := (&gitHandler{}).Execute(context.Background(), ToolEnv{
		WorkspaceRoot: dir,
		ToolFuncs:     &ToolFuncSet{AttributeCommitMessage: recordingAttribution(&generated)},
	}, map[string]any{"operation": "commit", "args": `-m "fix: via git tool"`})
	require.NoError(t, err)
	require.False(t, res.IsError, "tool output: %s", res.Output)
	assert.Equal(t, []bool{false}, generated)
	assert.Equal(t, "fix: via git tool\n\nAssisted-by: sprout/dev (test/model)", gitInRepo(t, dir, "log", "-1", "--format=%B"))
}

// recordingTranscriptAttribution returns an
// AttributeCommitMessageFromTranscript hook that appends the recorded pair,
// and records the pair and generated flag it was called with.
func recordingTranscriptAttribution(pairs *[][2]string, generated *[]bool) func(string, string, string, bool) string {
	return func(provider, model, message string, gen bool) string {
		*pairs = append(*pairs, [2]string{provider, model})
		*generated = append(*generated, gen)
		return message + "\n\nAssisted-by: sprout/dev (" + provider + "/" + model + ")"
	}
}

// TestCommitHandler_TranscriptPairNamesTrailer verifies a commit tool call
// that carries the transcript's provider/model attributes the commit to that
// pair — the model recorded for the session that did the work — rather than
// to whatever agent happens to run the tool.
func TestCommitHandler_TranscriptPairNamesTrailer(t *testing.T) {
	dir := newTestGitRepo(t, true)
	var pairs [][2]string
	var generated []bool
	res, err := (&commitHandler{}).Execute(context.Background(), ToolEnv{
		WorkspaceRoot: dir,
		ToolFuncs:     &ToolFuncSet{AttributeCommitMessageFromTranscript: recordingTranscriptAttribution(&pairs, &generated)},
	}, map[string]any{
		"message":             "fix: transcript",
		"transcript_provider": "deepinfra",
		"transcript_model":    "deepseek-ai/DeepSeek-V4.1-Flash",
	})
	require.NoError(t, err)
	require.False(t, res.IsError, "tool output: %s", res.Output)
	assert.Equal(t, [][2]string{{"deepinfra", "deepseek-ai/DeepSeek-V4.1-Flash"}}, pairs)
	assert.Equal(t, []bool{false}, generated)
	assert.Equal(t,
		"fix: transcript\n\nAssisted-by: sprout/dev (deepinfra/deepseek-ai/DeepSeek-V4.1-Flash)",
		gitInRepo(t, dir, "log", "-1", "--format=%B"))
}

// TestCommitHandler_TranscriptHookFallsBackToAgent verifies a hook set that
// lacks the transcript-aware form still attributes through the plain hook,
// so older wiring keeps working.
func TestCommitHandler_TranscriptHookFallsBackToAgent(t *testing.T) {
	dir := newTestGitRepo(t, true)
	var generated []bool
	res, err := (&commitHandler{}).Execute(context.Background(), ToolEnv{
		WorkspaceRoot: dir,
		ToolFuncs:     &ToolFuncSet{AttributeCommitMessage: recordingAttribution(&generated)},
	}, map[string]any{
		"message":             "fix: fallback",
		"transcript_provider": "deepinfra",
		"transcript_model":    "deepseek-ai/DeepSeek-V4.1-Flash",
	})
	require.NoError(t, err)
	require.False(t, res.IsError, "tool output: %s", res.Output)
	assert.Equal(t, []bool{false}, generated)
	assert.Equal(t, "fix: fallback\n\nAssisted-by: sprout/dev (test/model)", gitInRepo(t, dir, "log", "-1", "--format=%B"))
}
