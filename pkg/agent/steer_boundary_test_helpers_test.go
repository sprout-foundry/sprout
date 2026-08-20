package agent

import (
	"testing"
)

// fakeSeedInjector records accepted injections and can be flipped to reject
// (mirroring seed's capacity-1 injection channel holding an unconsumed msg).
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

func newDelivererWithFake(t *testing.T, a *Agent, fake *fakeSeedInjector) *steerBoundaryDeliverer {
	t.Helper()
	d := &steerBoundaryDeliverer{agent: a}
	d.setSeedAgent(fake)
	return d
}
