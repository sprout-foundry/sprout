package console

// terminal_question.go — one question on the terminal at a time.
//
// Questions come from many places that run concurrently: parallel tool calls
// each reading a file the trust question covers, parallel subagents, a
// security approval raised while an ask_user is open. Each pauses the steer
// reader and draws its own picker; two at once put two pickers on one
// terminal and split the user's keystrokes between them. Every terminal
// question takes this slot first, so the second waits for the first to be
// answered and then asks on a clean screen.

import (
	"context"
	"sync/atomic"
)

var (
	questionSlot   = make(chan struct{}, 1)
	questionHolder atomic.Uint64
	// questionDepth is touched only by the holding goroutine; the slot's
	// channel handoff orders it between holders.
	questionDepth int
)

// AcquireTerminalQuestion waits until no other question is on the terminal
// and returns the release to defer. It is re-entrant for the goroutine that
// holds it (a prompt raised from inside another cannot deadlock on itself).
// A cancelled ctx abandons the wait and returns its error.
func AcquireTerminalQuestion(ctx context.Context) (release func(), err error) {
	id := currentGoroutineID()
	if questionHolder.Load() == id {
		questionDepth++
		return func() { questionDepth-- }, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case questionSlot <- struct{}{}:
	case <-ctx.Done():
		return func() {}, ctx.Err()
	}
	questionHolder.Store(id)
	questionDepth = 1
	// Between turns the idle prompt owns stdin; it stands aside for the
	// question (see input_yield.go). Mid-turn this is a no-op — the steer
	// reader is paused by the question's PauseSteer instead.
	resumePrompt := yieldIdlePrompt()
	return func() {
		questionDepth--
		if questionDepth == 0 {
			questionHolder.Store(0)
			resumePrompt()
			<-questionSlot
		}
	}, nil
}
