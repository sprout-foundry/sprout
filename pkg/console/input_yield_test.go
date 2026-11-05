//go:build unix && !js

package console

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A question raised between turns (a background subagent's trust question)
// takes the terminal from the idle prompt: keys typed while it is open stay
// on stdin for the question, and the prompt reads again once it is answered.
func TestIdlePrompt_YieldsStdinToAQuestion(t *testing.T) {
	master := withPtyStdin(t)
	ir := NewInputReader("> ")
	ir.ArmWakeup()

	type result struct {
		line string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		line, err := ir.ReadLine()
		ch <- result{line, err}
	}()
	// Wait until ReadLine has registered itself as the active prompt.
	deadline := time.Now().Add(3 * time.Second)
	for {
		LockOutput()
		active := activeInputReader == ir
		UnlockOutput()
		if active || time.Now().After(deadline) {
			require.True(t, active, "ReadLine never became the active prompt")
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	release, err := AcquireTerminalQuestion(context.Background())
	require.NoError(t, err)
	require.True(t, ir.yieldRequested.Load())

	// The question's keystroke: the prompt must leave it on stdin.
	_, err = master.WriteString("2")
	require.NoError(t, err)
	got := make([]byte, 8)
	fd := int(os.Stdin.Fd())
	require.True(t, waitForStdinReadable(fd, 2*time.Second), "the key never reached stdin")
	n, err := os.Stdin.Read(got)
	require.NoError(t, err)
	require.Equal(t, "2", string(got[:n]), "the question reads the key, not the idle prompt")

	release()
	_, err = master.WriteString("hello")
	require.NoError(t, err)
	time.Sleep(100 * time.Millisecond)
	_, err = master.WriteString("\r")
	require.NoError(t, err)
	select {
	case res := <-ch:
		require.NoError(t, res.err)
		require.Equal(t, "hello", res.line, "the prompt reads again after the question")
	case <-time.After(5 * time.Second):
		t.Fatal("the prompt did not resume after the question")
	}
}
