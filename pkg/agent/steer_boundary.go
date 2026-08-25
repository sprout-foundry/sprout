package agent

import (
	"context"
	"sync"

	core "github.com/sprout-foundry/seed/core"
)

// seedInjector is the slice of seed's Agent surface the boundary deliverer
// needs. *core.Agent satisfies it.
type seedInjector interface {
	InjectInput(input string) bool
}

// steerBoundaryDeliverer hands one staged steer message to seed per call.
// It is invoked at conversation-loop boundaries by sprout-owned code that
// runs inside seed's loop goroutine (provider return, tool-batch return),
// immediately before seed's own injection pickup checks — so delivery
// timing is identical to the old eager-channel pipeline.
//
// A single non-blocking InjectInput attempt per boundary is intentional:
// seed consumes at most one injected message per boundary and its channel
// holds one; a rejected attempt means the previous message is still being
// processed, and the staged message simply waits for the next boundary.
type steerBoundaryDeliverer struct {
	mu        sync.Mutex
	agent     *Agent
	seedAgent seedInjector
}

func (d *steerBoundaryDeliverer) setSeedAgent(sa seedInjector) {
	d.mu.Lock()
	d.seedAgent = sa
	d.mu.Unlock()
}

// deliverOne offers the oldest staged message to seed. Returns true when
// a message was accepted (and committed); false when nothing was staged
// or seed's channel was busy.
func (d *steerBoundaryDeliverer) deliverOne() bool {
	if d == nil {
		return false
	}
	d.mu.Lock()
	agent, sa := d.agent, d.seedAgent
	d.mu.Unlock()
	if agent == nil || sa == nil {
		return false
	}

	id, content, ok := agent.peekStagedSteer()
	if !ok {
		return false
	}
	if !sa.InjectInput(content) {
		agent.releaseStagedSteer(id)
		return false
	}
	agent.commitStagedSteer(id)
	return true
}

// steerFlushExecutor wraps a core.ToolExecutor so the oldest staged steer
// is offered to seed after every tool batch — the moment seed's loop is
// about to run its post-tool injection check.
type steerFlushExecutor struct {
	inner     core.ToolExecutor
	deliverer *steerBoundaryDeliverer
}

func (s *steerFlushExecutor) GetTools() []core.Tool {
	return s.inner.GetTools()
}

func (s *steerFlushExecutor) Execute(ctx context.Context, calls []core.ToolCall) []core.Message {
	results := s.inner.Execute(ctx, calls)
	s.deliverer.deliverOne()
	return results
}

// setProviderSteerHook installs a post-provider-call boundary hook on the
// sprout provider. Called at the END of Chat/ChatStream, covering seed's
// no-tool-call injection check (conversation.go:625 pickup point).
func setProviderSteerHook(p core.Provider, hook func()) {
	if sp, ok := p.(*sproutProvider); ok {
		sp.setSteerFlushHook(hook)
	}
}
