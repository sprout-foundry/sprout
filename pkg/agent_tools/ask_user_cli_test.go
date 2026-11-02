package tools

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func stubReadAnswer(t *testing.T, fn func(ctx context.Context) (string, error)) {
	t.Helper()
	prev := readAnswer
	readAnswer = fn
	t.Cleanup(func() { readAnswer = prev })
}

func TestRenderCLIPrompt_FreeformSubmitsOnEnter(t *testing.T) {
	var b strings.Builder
	renderCLIPrompt(&b, AskUserRequest{Question: "Which port?"})
	out := b.String()
	require.NotContains(t, out, "blank line",
		"one Enter submits; the prompt must not ask for a second")
	require.True(t, strings.HasSuffix(out, "> "), "the answer is typed on the prompt row: %q", out)
}

func TestRenderCLIPrompt_DefaultShownInline(t *testing.T) {
	var b strings.Builder
	renderCLIPrompt(&b, AskUserRequest{Question: "Which port?", Default: "8080"})
	require.True(t, strings.HasSuffix(b.String(), "> [default: 8080] "))
}

func TestReadCustomAnswer_KeepsKeysTypedInPicker(t *testing.T) {
	stubReadAnswer(t, func(context.Context) (string, error) { return "ust use sqlite", nil })
	got, err := readCustomAnswer(context.Background(), AskUserRequest{}, "j")
	require.NoError(t, err)
	require.Equal(t, "just use sqlite", got,
		"the key that left the picker is the answer's first character")
}

func TestReadCustomAnswer_BlankFallsBackToDefault(t *testing.T) {
	stubReadAnswer(t, func(context.Context) (string, error) { return "", nil })
	got, err := readCustomAnswer(context.Background(), AskUserRequest{Default: "postgres"}, "")
	require.NoError(t, err)
	require.Equal(t, "postgres", got)
}

func TestReadCustomAnswer_ClosedStdinIsNoChannel(t *testing.T) {
	stubReadAnswer(t, func(context.Context) (string, error) { return "", io.EOF })
	_, err := readCustomAnswer(context.Background(), AskUserRequest{}, "")
	require.ErrorIs(t, err, ErrAskUserNoChannel)
}

func TestReadCustomAnswer_DeadlineYieldsDefault(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	<-ctx.Done()
	stubReadAnswer(t, func(ctx context.Context) (string, error) { return "", ctx.Err() })
	got, err := readCustomAnswer(ctx, AskUserRequest{Default: "later"}, "")
	require.NoError(t, err)
	require.Equal(t, "later", got, "a caller-set deadline is a bounded wait, not a failure")
}

func TestReadCustomAnswer_InterruptPropagates(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	stubReadAnswer(t, func(ctx context.Context) (string, error) { return "", ctx.Err() })
	_, err := readCustomAnswer(ctx, AskUserRequest{Default: "later"}, "")
	require.ErrorIs(t, err, context.Canceled)
}
