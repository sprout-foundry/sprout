//go:build unix && !js

package console

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Regression: the per-part shell approval prompt that ends by context
// timeout must leave stdin clean. The old scanner-goroutine reader stayed
// parked in Scan() after the timeout and swallowed the next line the user
// typed, so the steer reader / REPL prompt never saw it.
func TestShellApprovalPartsTimeout_StdinSurvives(t *testing.T) {
	master := withPtyStdin(t)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	// No answer typed: the prompt runs out to the ctx deadline and returns
	// a deny-all map.
	decisions, err := PromptShellApprovalParts(ctx, []ShellPartInfo{
		{ID: "part-0", Text: "rm -rf /tmp/x", Kind: "rm", RiskLabel: "CRITICAL"},
	})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.False(t, decisions["part-0"], "timeout denies for safety")

	// The terminal must still deliver input to the next reader.
	time.Sleep(100 * time.Millisecond) // let any zombie reader surface
	_, err = master.WriteString("hello\r")
	require.NoError(t, err)
	readCtx, readCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer readCancel()
	line, readErr := ReadLineCtx(readCtx)
	require.NoError(t, readErr)
	require.Equal(t, "hello", line, "the next reader must see the line typed after the timeout")
}
