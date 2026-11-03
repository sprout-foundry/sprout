package console

// input_yield.go — the idle prompt stands aside while a question is open.
//
// Between turns the idle prompt (InputReader) owns stdin. A question raised
// then from another goroutine — a background subagent's trust question or
// approval, an ask_user — opens its picker while the prompt's read loop is
// still polling: keystrokes split between the two, and the picker draws over
// the prompt line. PauseSteer only covers the mid-turn steer reader. So the
// one-question slot also asks the idle prompt to yield: the read loop clears
// its prompt and stops reading until the question is answered, then redraws.
//
// The read loop does the clearing itself (its state is single-goroutine) and
// acknowledges; the question waits a bounded time for the acknowledgement.
// Yielding needs the loop's readability poll, which the REPL arms at
// startup (ArmWakeup).

import (
	"os"
	"time"
)

// inputYieldAckWait bounds how long a question waits for the idle prompt to
// stand aside: a few of the loop's poll intervals.
const inputYieldAckWait = 300 * time.Millisecond

// inputYieldPollInterval is how often a yielded loop checks for the release.
const inputYieldPollInterval = 20 * time.Millisecond

// yieldIdlePrompt asks the active idle prompt, if any, to stand aside and
// returns the function that hands stdin back.
func yieldIdlePrompt() (resume func()) {
	LockOutput()
	ir := activeInputReader
	UnlockOutput()
	if ir == nil || !ir.wakeupArmed.Load() {
		return func() {}
	}
	ir.yieldRequested.Store(true)
	select {
	case <-ir.yieldAck:
	case <-time.After(inputYieldAckWait):
	}
	return func() { ir.yieldRequested.Store(false) }
}

// handleYield is the read loop's side, called once per iteration
// before it reads. It reports whether the loop must skip reading this
// iteration (a question holds the terminal).
func (ir *InputReader) handleYield() bool {
	if ir.yieldRequested.Load() {
		if !ir.yielded {
			ir.yielded = true
			LockOutput()
			ir.clearInlineInputForResizeLocked(ir.terminalWidth)
			UnlockOutput()
			select {
			case ir.yieldAck <- struct{}{}:
			default:
			}
		}
		time.Sleep(inputYieldPollInterval)
		return true
	}
	if ir.yielded {
		ir.yielded = false
		// The picker may have reset the modes this prompt enabled (it
		// disables mouse tracking on close); restore them, then redraw
		// the prompt below whatever the question printed.
		LockOutput()
		_, _ = os.Stdout.WriteString(bracketedPasteEnable + mouseInputModes())
		UnlockOutput()
		ir.Refresh()
	}
	return false
}
