package agent

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestStageAndRetractRoundtrip(t *testing.T) {
	a := newIsolatedTestAgent(t)
	defer a.Shutdown()

	if err := a.StageSteerInput("first steer"); err != nil {
		t.Fatalf("stage first: %v", err)
	}
	if err := a.StageSteerInput("second steer"); err != nil {
		t.Fatalf("stage second: %v", err)
	}
	if got := a.PendingSteerCount(); got != 2 {
		t.Fatalf("expected 2 staged, got %d", got)
	}

	content, ok := a.RetractLatestSteer()
	if !ok || content != "second steer" {
		t.Fatalf("retract should return newest ('second steer'), got %q ok=%v", content, ok)
	}
	if got := a.PendingSteerCount(); got != 1 {
		t.Fatalf("expected 1 staged after retract, got %d", got)
	}

	content, ok = a.RetractLatestSteer()
	if !ok || content != "first steer" {
		t.Fatalf("retract should return 'first steer', got %q ok=%v", content, ok)
	}
	if got := a.PendingSteerCount(); got != 0 {
		t.Fatalf("expected 0 staged after retracting all, got %d", got)
	}

	if _, ok := a.RetractLatestSteer(); ok {
		t.Fatal("retract on empty staging should return false")
	}
}

func TestRetractAfterCommitFails(t *testing.T) {
	a := newIsolatedTestAgent(t)
	defer a.Shutdown()

	if err := a.StageSteerInput("will be delivered"); err != nil {
		t.Fatalf("stage: %v", err)
	}

	d := newDelivererWithFake(t, a, &fakeSeedInjector{})
	if !d.deliverOne() {
		t.Fatal("deliverOne should deliver the staged message")
	}

	if _, ok := a.RetractLatestSteer(); ok {
		t.Fatal("retract after commit must fail — message is in seed's pipeline")
	}
	if got := a.PendingSteerCount(); got != 0 {
		t.Fatalf("expected 0 staged after delivery, got %d", got)
	}
}

func TestDeliverOneRejectedKeepsStaged(t *testing.T) {
	a := newIsolatedTestAgent(t)
	defer a.Shutdown()

	if err := a.StageSteerInput("waiting"); err != nil {
		t.Fatalf("stage: %v", err)
	}

	fake := &fakeSeedInjector{injectNext: []bool{false, true}}
	d := newDelivererWithFake(t, a, fake)

	if d.deliverOne() {
		t.Fatal("deliverOne should report false when seed rejects")
	}
	if got := a.PendingSteerCount(); got != 1 {
		t.Fatalf("rejected delivery must leave the message staged, got %d", got)
	}

	if !d.deliverOne() {
		t.Fatal("deliverOne should succeed once seed accepts")
	}
	if got := a.PendingSteerCount(); got != 0 {
		t.Fatalf("expected 0 staged after accepted delivery, got %d", got)
	}
	if len(fake.injected) != 1 || fake.injected[0] != "waiting" {
		t.Fatalf("unexpected deliveries: %v", fake.injected)
	}
}

func TestStageCapEnforced(t *testing.T) {
	a := newIsolatedTestAgent(t)
	defer a.Shutdown()

	for i := 0; i < steerStageCap; i++ {
		if err := a.StageSteerInput("filler"); err != nil {
			t.Fatalf("stage %d within cap should succeed: %v", i, err)
		}
	}
	if err := a.StageSteerInput("one too many"); err == nil {
		t.Fatal("staging beyond cap must error")
	}
}

func TestDeliverOrderIsFIFO(t *testing.T) {
	a := newIsolatedTestAgent(t)
	defer a.Shutdown()

	for _, msg := range []string{"m1", "m2", "m3"} {
		if err := a.StageSteerInput(msg); err != nil {
			t.Fatalf("stage %s: %v", msg, err)
		}
	}

	fake := &fakeSeedInjector{}
	d := newDelivererWithFake(t, a, fake)

	if !d.deliverOne() {
		t.Fatal("deliverOne should hand m1 to seed")
	}
	if !d.deliverOne() {
		t.Fatal("deliverOne should hand m2 to seed after m1 pickup")
	}
	if len(fake.injected) != 2 || fake.injected[0] != "m1" || fake.injected[1] != "m2" {
		t.Fatalf("expected FIFO delivery [m1 m2], got %v", fake.injected)
	}

	// Retract m3 before its boundary — it must never reach seed.
	if content, ok := a.RetractLatestSteer(); !ok || content != "m3" {
		t.Fatalf("expected to retract m3, got %q ok=%v", content, ok)
	}
	if d.deliverOne() {
		t.Fatal("nothing left staged — deliverOne should return false")
	}
	if len(fake.injected) != 2 {
		t.Fatalf("retracted message must not be delivered, injected=%v", fake.injected)
	}
}

func TestInjectInputContextMirrorsToSteeringChannel(t *testing.T) {
	a := newIsolatedTestAgent(t)
	defer a.Shutdown()

	if err := a.InjectInputContext("via legacy api"); err != nil {
		t.Fatalf("legacy inject: %v", err)
	}

	select {
	case msg := <-a.SteeringChannel():
		if msg != "via legacy api" {
			t.Fatalf("mirror wrong content: %q", msg)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("legacy SteeringChannel reader saw nothing")
	}
	if got := a.PendingSteerCount(); got != 1 {
		t.Fatalf("legacy inject must stage, got %d staged", got)
	}
}

func TestClearInputInjectionContextClearsStaging(t *testing.T) {
	a := newIsolatedTestAgent(t)
	defer a.Shutdown()

	_ = a.StageSteerInput("doomed")
	a.ClearInputInjectionContext()

	if got := a.PendingSteerCount(); got != 0 {
		t.Fatalf("clear must wipe staging, got %d", got)
	}
	if _, ok := a.RetractLatestSteer(); ok {
		t.Fatal("retract after clear must fail")
	}
}

func TestNilAgentSteerStagingSafety(t *testing.T) {
	var a *Agent
	if err := a.StageSteerInput("x"); err == nil {
		t.Fatal("nil agent stage must error")
	}
	if _, ok := a.RetractLatestSteer(); ok {
		t.Fatal("nil agent retract must return false")
	}
	if got := a.PendingSteerCount(); got != 0 {
		t.Fatalf("nil agent count must be 0, got %d", got)
	}
}

func TestInFlightEntryNotRetractable(t *testing.T) {
	a := newIsolatedTestAgent(t)
	defer a.Shutdown()

	if err := a.StageSteerInput("being delivered"); err != nil {
		t.Fatalf("stage: %v", err)
	}

	id, content, ok := a.peekStagedSteer()
	if !ok || content != "being delivered" {
		t.Fatalf("peek should mark in-flight and return content, got %q ok=%v", content, ok)
	}

	// While in-flight (deliverOne is between peek and InjectInput),
	// retraction must deterministically fail.
	if _, retracted := a.RetractLatestSteer(); retracted {
		t.Fatal("in-flight entry must not be retractable")
	}
	if got := a.PendingSteerCount(); got != 0 {
		t.Fatalf("in-flight entry is not part of the pending count, got %d", got)
	}

	// Rejection releases it back to retractable.
	a.releaseStagedSteer(id)
	if _, retracted := a.RetractLatestSteer(); !retracted {
		t.Fatal("released entry must be retractable again")
	}
}

func TestStageAndRetractConcurrent(t *testing.T) {
	a := newIsolatedTestAgent(t)
	defer a.Shutdown()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(2)
		go func(n int) {
			defer wg.Done()
			_ = a.StageSteerInput(fmt.Sprintf("msg-%d", n))
		}(i)
		go func() {
			defer wg.Done()
			a.RetractLatestSteer()
		}()
	}
	wg.Wait()

	// No panic and no corruption: every remaining entry is well-formed.
	if got := a.PendingSteerCount(); got > steerStageCap {
		t.Fatalf("pending count %d exceeds cap %d", got, steerStageCap)
	}
	a.ClearInputInjectionContext()
	if got := a.PendingSteerCount(); got != 0 {
		t.Fatalf("clear must wipe staging, got %d", got)
	}
}
