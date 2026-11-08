package agent

import (
	"testing"

	core "github.com/sprout-foundry/seed/core"
)

// fakeSeedInjector records accepted injections and can be flipped to reject
// (mirroring seed's capacity-1 injection channel holding an unconsumed msg).
// It deliberately does NOT expose State(): acceptance alone must never look
// like proof the message reached the conversation.
type fakeSeedInjector struct {
	injectNext []bool // pop-front: whether InjectInput accepts
	injected   []string
}

func (f *fakeSeedInjector) InjectInput(input string) bool {
	accept := true
	if len(f.injectNext) > 0 {
		accept = f.injectNext[0]
		f.injectNext = f.injectNext[1:]
	}
	if accept {
		f.injected = append(f.injected, input)
	}
	return accept
}

func newDelivererWithFake(t *testing.T, a *Agent, fake seedInjector) *steerBoundaryDeliverer {
	t.Helper()
	d := &steerBoundaryDeliverer{agent: a}
	d.setSeedAgent(fake)
	return d
}

// fakeSeedWithState is a seed agent stand-in that also reports conversation
// state, so landing can be observed the way the real *core.Agent allows it.
// consume() plays seed's pickup point: the message moves from the injection
// buffer into the conversation.
type fakeSeedWithState struct {
	state    *core.State
	buffered []string
}

func (f *fakeSeedWithState) InjectInput(input string) bool {
	if len(f.buffered) > 0 {
		return false // seed's buffer holds exactly one message
	}
	f.buffered = append(f.buffered, input)
	return true
}

func (f *fakeSeedWithState) State() *core.State { return f.state }

// consume drains one buffered message into the conversation, as seed's
// loop-boundary pickup does.
func (f *fakeSeedWithState) consume() {
	if len(f.buffered) == 0 {
		return
	}
	f.state.AddMessage(core.Message{Role: "user", Content: f.buffered[0]})
	f.buffered = f.buffered[1:]
}
