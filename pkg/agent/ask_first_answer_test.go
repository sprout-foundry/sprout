package agent

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/stretchr/testify/require"
)

// answerAfter answers after d unless withdrawn first; withdrawn records that
// it saw the cancellation and finished.
func answerAfter(d time.Duration, answer string, withdrawn *atomic.Bool) func(context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		select {
		case <-time.After(d):
			return answer, nil
		case <-ctx.Done():
			time.Sleep(20 * time.Millisecond) // teardown
			withdrawn.Store(true)
			return "", ctx.Err()
		}
	}
}

func failWith(err error) func(context.Context) (string, error) {
	return func(context.Context) (string, error) { return "", err }
}

func TestAskFirstAnswer_EitherSurfaceWinsAndTheOtherIsWithdrawn(t *testing.T) {
	var browserGone, terminalGone atomic.Bool
	got, err := askFirstAnswer(context.Background(),
		answerAfter(10*time.Millisecond, "from the terminal", &terminalGone),
		answerAfter(5*time.Second, "from the browser", &browserGone))
	require.NoError(t, err)
	require.Equal(t, "from the terminal", got)
	require.True(t, browserGone.Load(), "the browser dialog is withdrawn and finished before returning")

	got, err = askFirstAnswer(context.Background(),
		answerAfter(5*time.Second, "from the terminal", &terminalGone),
		answerAfter(10*time.Millisecond, "from the browser", &browserGone))
	require.NoError(t, err)
	require.Equal(t, "from the browser", got)
	require.True(t, terminalGone.Load(), "the terminal picker closes before output resumes")
}

func TestAskFirstAnswer_NoChannelLeavesItToTheOther(t *testing.T) {
	var gone atomic.Bool
	got, err := askFirstAnswer(context.Background(),
		failWith(tools.ErrAskUserNoChannel),
		answerAfter(10*time.Millisecond, "from the browser", &gone))
	require.NoError(t, err)
	require.Equal(t, "from the browser", got)
}

func TestAskFirstAnswer_DismissalIsAnAnswer(t *testing.T) {
	var gone atomic.Bool
	_, err := askFirstAnswer(context.Background(),
		failWith(tools.ErrAskUserDismissed),
		answerAfter(5*time.Second, "late", &gone))
	require.ErrorIs(t, err, tools.ErrAskUserDismissed)
	require.True(t, gone.Load())
}

func TestAskFirstAnswer_BothFailing(t *testing.T) {
	boom := errors.New("dialog failed")
	_, err := askFirstAnswer(context.Background(), failWith(tools.ErrAskUserNoChannel), failWith(boom))
	require.ErrorIs(t, err, boom, "the real failure, not the absent channel")
}

func TestAskBothSurfaces_CredentialsStayBrowserOnly(t *testing.T) {
	t.Setenv("SPROUT_FORCE_INTERACTIVE", "1")
	a := newTestAgent(t)
	require.True(t, a.askBothSurfaces(tools.AskUserRequest{Question: "q"}))
	require.False(t, a.askBothSurfaces(tools.AskUserRequest{Question: "q", Sensitive: true}))
	a.SetWorkflowRun(true)
	require.False(t, a.askBothSurfaces(tools.AskUserRequest{Question: "q"}), "nobody at the terminal")
}
