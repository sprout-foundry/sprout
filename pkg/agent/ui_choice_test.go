package agent

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/stretchr/testify/require"
)

// stubPromptChoiceAsk answers PromptChoice's ask_user fallback and counts the
// questions.
func stubPromptChoiceAsk(t *testing.T, answer string, err error) *int {
	t.Helper()
	asked := 0
	prev := promptChoiceAsk
	promptChoiceAsk = func(_ *Agent, req tools.AskUserRequest) (string, error) {
		asked++
		require.NotEmpty(t, req.Options)
		return answer, err
	}
	t.Cleanup(func() { promptChoiceAsk = prev })
	return &asked
}

var approveDeny = []ChoiceOption{{Label: "Approve", Value: "approve"}, {Label: "Deny", Value: "deny"}}

// The regression: with no UI installed (the CLI never installs one),
// PromptChoice reported "no UI" even in an interactive session, so commit
// and create_pr proceeded without asking. It now asks.
func TestPromptChoice_InteractiveSessionAsks(t *testing.T) {
	t.Setenv("SPROUT_FORCE_INTERACTIVE", "1")
	a := newTestAgent(t)
	asked := stubPromptChoiceAsk(t, "deny", nil)

	got, err := a.PromptChoice("Allow agent to commit staged changes?", approveDeny)
	require.NoError(t, err)
	require.Equal(t, "deny", got)
	require.Equal(t, 1, *asked)
}

func TestPromptChoice_TypedOrDismissedAnswerIsNoChoice(t *testing.T) {
	t.Setenv("SPROUT_FORCE_INTERACTIVE", "1")
	a := newTestAgent(t)

	stubPromptChoiceAsk(t, "sure, go ahead", nil)
	got, err := a.PromptChoice("Allow?", approveDeny)
	require.NoError(t, err)
	require.Empty(t, got, "only a listed value is a choice")

	stubPromptChoiceAsk(t, "", tools.ErrAskUserDismissed)
	got, err = a.PromptChoice("Allow?", approveDeny)
	require.NoError(t, err)
	require.Empty(t, got, "Esc is not approval")
}

// Only a run nobody can answer reports ErrUINotAvailable — the case the
// callers' autonomous fallbacks are for.
func TestPromptChoice_UnattendedRunHasNoUI(t *testing.T) {
	a := newTestAgent(t)
	a.SetWorkflowRun(true)
	asked := stubPromptChoiceAsk(t, "approve", nil)

	_, err := a.PromptChoice("Allow?", approveDeny)
	require.ErrorIs(t, err, ErrUINotAvailable)
	require.Zero(t, *asked, "an automate run is never prompted")

	t.Setenv("SPROUT_FORCE_INTERACTIVE", "1")
	b := newTestAgent(t)
	stubPromptChoiceAsk(t, "", tools.ErrAskUserNoChannel)
	_, err = b.PromptChoice("Allow?", approveDeny)
	require.ErrorIs(t, err, ErrUINotAvailable, "no terminal and no browser after all")
}

// End to end through the commit tool: a denial stops the commit before git
// is touched. Before the fix this committed without asking.
func TestCommitTool_InteractiveDenialStopsTheCommit(t *testing.T) {
	t.Setenv("SPROUT_FORCE_INTERACTIVE", "1")
	a := newTestAgent(t)
	repo := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"add", "f.txt"}} {
		if args[0] == "add" {
			require.NoError(t, os.WriteFile(filepath.Join(repo, "f.txt"), []byte("x\n"), 0o644))
		}
		cmd := exec.Command("git", args...) //nolint:gosec // G204: fixed git subcommands in a test repo
		cmd.Dir = repo
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", out)
	}
	a.SetWorkspaceRoot(repo)
	// A persona without git_write is the one that has to ask; orchestrator,
	// the default, may commit by capability.
	require.NoError(t, a.ApplyPersona("general"))
	require.False(t, a.isGitWriteAllowed())
	asked := stubPromptChoiceAsk(t, "deny", nil)

	out, err := handleCommitTool(context.Background(), a, map[string]interface{}{"message": "fix: tidy"})
	require.NoError(t, err)
	require.Equal(t, "Commit cancelled by user.", out)
	require.Equal(t, 1, *asked)
}
