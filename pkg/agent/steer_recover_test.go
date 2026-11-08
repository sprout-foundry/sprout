package agent

import (
	"testing"
	"time"

	core "github.com/sprout-foundry/seed/core"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// A steer the run took but never consumed must come back. seed's injection
// buffer holds one message per run and dies with that run, so without recovery
// the user's steer vanishes between turns.
func TestRecoverUnlandedSteerReleasesTakenMessage(t *testing.T) {
	a := newIsolatedTestAgent(t)
	defer a.Shutdown()

	if err := a.StageSteerInput("keep going with the tests"); err != nil {
		t.Fatalf("stage: %v", err)
	}
	fake := &fakeSeedInjector{}
	if !newDelivererWithFake(t, a, fake).deliverOne() {
		t.Fatal("expected the steer to be accepted")
	}

	a.recoverUnlandedSteer(nil)

	if got := a.PendingSteerCount(); got != 1 {
		t.Fatalf("steer must be back on the queue after the run ends, got %d", got)
	}
	content, ok := a.RetractLatestSteer()
	if !ok || content != "keep going with the tests" {
		t.Fatalf("released steer must be the user's own text, got %q ok=%v", content, ok)
	}
}

// The released message is delivered by a later turn's boundary, so the model
// actually receives it.
func TestRecoveredSteerDeliversOnNextRun(t *testing.T) {
	a := newIsolatedTestAgent(t)
	defer a.Shutdown()

	if err := a.StageSteerInput("deliver me next turn"); err != nil {
		t.Fatalf("stage: %v", err)
	}
	newDelivererWithFake(t, a, &fakeSeedInjector{}).deliverOne()
	a.recoverUnlandedSteer(nil)

	// New turn, new seed agent (the old run's buffer is gone).
	next := &fakeSeedInjector{}
	if !newDelivererWithFake(t, a, next).deliverOne() {
		t.Fatal("the recovered steer must be offered to the next run")
	}
	if len(next.injected) != 1 || next.injected[0] != "deliver me next turn" {
		t.Fatalf("next run received %v", next.injected)
	}
}

// A message that demonstrably reached the conversation is not redelivered —
// that would duplicate the user's steer.
func TestRecoveredSteerNotRedeliveredWhenLanded(t *testing.T) {
	a := newIsolatedTestAgent(t)
	defer a.Shutdown()

	if err := a.StageSteerInput("already in the conversation"); err != nil {
		t.Fatalf("stage: %v", err)
	}
	fake := &fakeSeedWithState{state: core.NewState()}
	if !newDelivererWithFake(t, a, fake).deliverOne() {
		t.Fatal("expected the steer to be accepted")
	}
	fake.consume() // seed's pickup moved it into the conversation

	a.recoverUnlandedSteer(landedObserver(fake))

	if got := a.PendingSteerCount(); got != 0 {
		t.Fatalf("a landed steer must not be re-queued, got %d", got)
	}
	next := &fakeSeedInjector{}
	if newDelivererWithFake(t, a, next).deliverOne() {
		t.Fatalf("nothing should be left to deliver, delivered %v", next.injected)
	}
}

// seed's force-finalize exits (last allowed continuation, a length-truncated
// response) return no error without running an injection pickup, so a clean end
// is not proof the buffer drained. A steer still absent from the conversation
// then has to be released like any other stranding.
func TestRecoveredSteerReleasedOnCleanEndThatSkippedPickup(t *testing.T) {
	a := newIsolatedTestAgent(t)
	defer a.Shutdown()

	if err := a.StageSteerInput("stranded by force-finalize"); err != nil {
		t.Fatalf("stage: %v", err)
	}
	fake := &fakeSeedWithState{state: core.NewState()}
	if !newDelivererWithFake(t, a, fake).deliverOne() {
		t.Fatal("expected the steer to be accepted")
	}
	// No consume(): the run finalized straight after the provider returned.

	a.recoverUnlandedSteer(landedObserver(fake))

	if got := a.PendingSteerCount(); got != 1 {
		t.Fatalf("a clean end that skipped the pickup must still release the steer, got %d", got)
	}
}

// Recovery is FIFO-preserving: an older released steer is offered before a
// message staged after it.
func TestRecoveredSteerKeepsFIFOOrder(t *testing.T) {
	a := newIsolatedTestAgent(t)
	defer a.Shutdown()

	if err := a.StageSteerInput("first"); err != nil {
		t.Fatalf("stage first: %v", err)
	}
	newDelivererWithFake(t, a, &fakeSeedInjector{}).deliverOne() // accepted, then stranded
	a.recoverUnlandedSteer(nil)

	if err := a.StageSteerInput("second"); err != nil {
		t.Fatalf("stage second: %v", err)
	}

	next := &fakeSeedInjector{}
	d := newDelivererWithFake(t, a, next)
	if !d.deliverOne() {
		t.Fatal("expected the recovered steer to be offered")
	}
	if !d.deliverOne() {
		t.Fatal("expected the second steer to be offered")
	}
	if len(next.injected) != 2 || next.injected[0] != "first" || next.injected[1] != "second" {
		t.Fatalf("recovered steer must keep its FIFO position, got %v", next.injected)
	}
}

// A message the conversation can never vouch for (its text was rewritten, so
// the landing proof never matches) must not be injected on every subsequent
// turn. Past the cap it is dropped, and the drop is reported rather than
// silent — losing a user's steer quietly is the failure being fixed.
func TestStrandedSteerIsDroppedAndReportedPastCap(t *testing.T) {
	a := newIsolatedTestAgent(t)
	defer a.Shutdown()

	bus := events.NewEventBus()
	const sub = "steer-drop-test"
	eventCh := bus.Subscribe(sub)
	defer bus.Unsubscribe(sub)
	a.SetEventBus(bus)

	if err := a.StageSteerInput("unvouchable"); err != nil {
		t.Fatalf("stage: %v", err)
	}
	d := newDelivererWithFake(t, a, &fakeSeedInjector{})

	for i := 0; i <= steerStrandingCap; i++ {
		if !d.deliverOne() {
			t.Fatalf("run %d: expected the steer to be offered", i)
		}
		a.recoverUnlandedSteer(nil)
	}
	if got := a.PendingSteerCount(); got != 0 {
		t.Fatalf("an endlessly stranded steer must stop recurring, got %d", got)
	}

	reported := false
	deadline := time.After(2 * time.Second)
collect:
	for {
		select {
		case ev := <-eventCh:
			if ev.Type == events.EventTypeError {
				reported = true
				break collect
			}
		case <-deadline:
			break collect
		}
	}
	if !reported {
		t.Fatal("a dropped steer must be reported to the user, not only logged")
	}
}

// Two steers with identical text are stamped identically; one copy landing must
// not vouch for the other.
func TestIdenticalSteersAreVouchedByRank(t *testing.T) {
	a := newIsolatedTestAgent(t)
	defer a.Shutdown()

	for i := 0; i < 2; i++ {
		if err := a.StageSteerInput("same text"); err != nil {
			t.Fatalf("stage %d: %v", i, err)
		}
	}
	fake := &fakeSeedWithState{state: core.NewState()}
	d := newDelivererWithFake(t, a, fake)
	if !d.deliverOne() {
		t.Fatal("expected first steer accepted")
	}
	fake.consume() // one copy in the conversation

	a.recoverUnlandedSteer(landedObserver(fake))

	if got := a.PendingSteerCount(); got != 1 {
		t.Fatalf("only the landed copy may be dropped, got %d pending", got)
	}
}

func TestRecoverUnlandedSteerNilAgentSafe(t *testing.T) {
	var a *Agent
	a.recoverUnlandedSteer(nil)
}
