//go:build unix && !js

package console

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Regression: an approval picker that times out must leave the terminal
// usable. Mid-turn sequence — steer reader active, question takes the
// terminal, picker times out unanswered, steer reader resumes — and the
// reader must still receive keystrokes afterwards.
func TestApprovalPickerTimeout_SteerInputSurvives(t *testing.T) {
	master := withPtyStdin(t)
	// SteerInputReader gates on Interactive(os.Stderr); point stderr at
	// the pty too so the reader starts, as it does in a real terminal.
	oldStderr := os.Stderr
	os.Stderr = os.Stdin
	t.Cleanup(func() { os.Stderr = oldStderr })

	submitted := make(chan string, 4)
	footer := NewStatusFooter(os.Stderr, &stubSource{model: "test"})
	footer.Start()
	defer func() { footer.SetTurnActive(false); footer.ClearSteerLine() }()
	r := NewSteerInputReader(footer, func(s string) { submitted <- s }, nil, func(string) {})
	r.Start()
	defer r.Stop()
	time.Sleep(100 * time.Millisecond)

	// The approval question: pause the reader, run a picker that times
	// out, then resume — the same sequence askForSecurityApprovalWriter
	// drives, with the 30-minute bound shortened.
	release, err := AcquireTerminalQuestion(context.Background())
	require.NoError(t, err)

	r.Stop() // PauseSteer's effect

	sl := NewSelectList(SelectListOptions{
		Items: []SelectItem{
			{Label: "Approve once", Value: "approve_once", Key: 'y'},
			{Label: "Deny", Value: "deny", Key: 'n'},
		},
		ArmDelay: 50 * time.Millisecond,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_, _, runErr := sl.Run(ctx)
	require.ErrorIs(t, runErr, context.DeadlineExceeded, "the picker should time out")

	r.Start() // ResumeSteer's effect
	release()

	// The terminal must still accept input: a steer submit arrives.
	_, err = master.WriteString("hi\r")
	require.NoError(t, err)
	select {
	case got := <-submitted:
		require.Equal(t, "hi", got)
	case <-time.After(3 * time.Second):
		t.Fatal("steer input dead after the picker timed out")
	}
}

// Same regression between turns: the idle prompt yields to a question whose
// picker times out; the prompt must read again afterwards.
func TestApprovalPickerTimeout_IdlePromptSurvives(t *testing.T) {
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

	sl := NewSelectList(SelectListOptions{
		Items: []SelectItem{
			{Label: "Approve once", Value: "approve_once", Key: 'y'},
			{Label: "Deny", Value: "deny", Key: 'n'},
		},
		ArmDelay: 50 * time.Millisecond,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_, _, runErr := sl.Run(ctx)
	require.ErrorIs(t, runErr, context.DeadlineExceeded, "the picker should time out")

	release()

	_, err = master.WriteString("hello\r")
	require.NoError(t, err)
	select {
	case res := <-ch:
		require.NoError(t, res.err)
		require.Equal(t, "hello", res.line, "the prompt reads again after the question times out")
	case <-time.After(5 * time.Second):
		t.Fatal("the prompt did not resume after the question timed out")
	}
}
