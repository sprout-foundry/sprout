package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	core "github.com/sprout-foundry/seed/core"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// writeInboxMessage drops a message into a steer inbox the way a sender does:
// written to a temporary name, then renamed into place.
func writeInboxMessage(t *testing.T, dir, id, text string) {
	t.Helper()
	tmp := filepath.Join(dir, "."+id+".tmp")
	if err := os.WriteFile(tmp, []byte(text), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.Rename(tmp, filepath.Join(dir, id+inboxMessageExt)); err != nil {
		t.Fatalf("rename: %v", err)
	}
}

func inboxStates(a *Agent) map[string]string {
	out := map[string]string{}
	for _, s := range a.SteerInboxReport() {
		out[s.ID] = s.State
	}
	return out
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// Inbox messages are delivered one per boundary, oldest first, each claimed
// (renamed .taken) only as it is delivered; each is reported landed once the
// conversation holds it.
func TestSteerInboxDeliversOnePerBoundaryInOrder(t *testing.T) {
	a := newIsolatedTestAgent(t)
	defer a.Shutdown()
	dir := t.TempDir()
	a.SetSteerInbox(dir)
	writeInboxMessage(t, dir, "0001", "use the billing helper")
	writeInboxMessage(t, dir, "0002", "skip the migration")

	fake := &fakeSeedWithState{state: core.NewState()}
	d := newDelivererWithFake(t, a, fake)
	if !d.deliverOne() {
		t.Fatal("the first inbox message should be delivered")
	}
	if len(fake.buffered) != 1 || !strings.Contains(fake.buffered[0], "use the billing helper") {
		t.Fatalf("delivered %v", fake.buffered)
	}
	if !exists(filepath.Join(dir, "0001"+inboxTakenExt)) || !exists(filepath.Join(dir, "0002"+inboxMessageExt)) {
		t.Fatal("only the delivered message is claimed; the next stays retractable")
	}

	fake.consume()
	if !d.deliverOne() {
		t.Fatal("the second message should be delivered at the next boundary")
	}
	if !strings.Contains(fake.buffered[0], "skip the migration") {
		t.Fatalf("delivered %v", fake.buffered)
	}
	fake.consume()
	d.deliverOne() // settles the second
	a.recoverUnlandedSteer(landedObserver(fake))

	got := inboxStates(a)
	if got["0001"] != inboxSteerLanded || got["0002"] != inboxSteerLanded {
		t.Fatalf("both should be landed, got %v", got)
	}
}

// A message its sender took back (deleted) before the run read it is never
// delivered.
func TestSteerInboxRetractedMessageIsNotDelivered(t *testing.T) {
	a := newIsolatedTestAgent(t)
	defer a.Shutdown()
	dir := t.TempDir()
	a.SetSteerInbox(dir)
	writeInboxMessage(t, dir, "0001", "never mind")
	if err := os.Remove(filepath.Join(dir, "0001"+inboxMessageExt)); err != nil {
		t.Fatal(err)
	}

	fake := &fakeSeedInjector{}
	if newDelivererWithFake(t, a, fake).deliverOne() {
		t.Fatalf("a retracted message must not be delivered, got %v", fake.injected)
	}
	if len(a.SteerInboxReport()) != 0 {
		t.Fatalf("nothing to report, got %v", a.SteerInboxReport())
	}
}

// A run that ends before a claimed message reached the conversation reports it
// unread, as it does the messages still waiting in the inbox.
func TestSteerInboxReportsUnreadAtTheEnd(t *testing.T) {
	a := newIsolatedTestAgent(t)
	defer a.Shutdown()
	dir := t.TempDir()
	a.SetSteerInbox(dir)
	writeInboxMessage(t, dir, "0001", "first")
	writeInboxMessage(t, dir, "0002", "second")

	fake := &fakeSeedWithState{state: core.NewState()}
	if !newDelivererWithFake(t, a, fake).deliverOne() {
		t.Fatal("expected the first message to be taken")
	}
	// The run ends without seed's pickup consuming it.
	a.recoverUnlandedSteer(landedObserver(fake))

	got := inboxStates(a)
	if got["0001"] != inboxSteerUnread || got["0002"] != inboxSteerUnread {
		t.Fatalf("both should be unread, got %v", got)
	}
}

// A typed steer waiting to be delivered goes first; the inbox is read only
// when nothing else is waiting.
func TestSteerInboxWaitsBehindATypedSteer(t *testing.T) {
	a := newIsolatedTestAgent(t)
	defer a.Shutdown()
	dir := t.TempDir()
	a.SetSteerInbox(dir)
	if err := a.StageSteerInput("typed"); err != nil {
		t.Fatal(err)
	}
	writeInboxMessage(t, dir, "0001", "from the inbox")

	fake := &fakeSeedInjector{}
	if !newDelivererWithFake(t, a, fake).deliverOne() {
		t.Fatal("expected a delivery")
	}
	if len(fake.injected) != 1 || !strings.Contains(fake.injected[0], "typed") {
		t.Fatalf("the typed steer goes first, delivered %v", fake.injected)
	}
	if !exists(filepath.Join(dir, "0001"+inboxMessageExt)) {
		t.Fatal("the inbox message is not claimed while another steer waits")
	}
}

// An oversized message is set aside unread and reported dropped, and the
// landing of an inbox message is published with its id.
func TestSteerInboxReportsDropsAndLandingsAsEvents(t *testing.T) {
	a := newIsolatedTestAgent(t)
	defer a.Shutdown()
	bus := events.NewEventBus()
	const sub = "steer-inbox-test"
	ch := bus.Subscribe(sub)
	defer bus.Unsubscribe(sub)
	a.SetEventBus(bus)
	dir := t.TempDir()
	a.SetSteerInbox(dir)
	writeInboxMessage(t, dir, "0001", strings.Repeat("x", maxInboxSteerBytes+1))
	writeInboxMessage(t, dir, "0002", "a short one")

	fake := &fakeSeedWithState{state: core.NewState()}
	d := newDelivererWithFake(t, a, fake)
	if !d.deliverOne() {
		t.Fatal("the short message should be delivered past the oversized one")
	}
	fake.consume()
	d.deliverOne()

	got := inboxStates(a)
	if got["0001"] != inboxSteerDropped || got["0002"] != inboxSteerLanded {
		t.Fatalf("got %v", got)
	}
	seen := map[string]string{}
	deadline := time.After(2 * time.Second)
	for len(seen) < 2 {
		select {
		case ev := <-ch:
			if ev.Type == events.EventTypeSteerLanded || ev.Type == events.EventTypeSteerDropped {
				data, _ := ev.Data.(map[string]interface{})
				id, _ := data["id"].(string)
				seen[id] = ev.Type
			}
		case <-deadline:
			t.Fatalf("events seen: %v", seen)
		}
	}
	if seen["0001"] != events.EventTypeSteerDropped || seen["0002"] != events.EventTypeSteerLanded {
		t.Fatalf("events seen: %v", seen)
	}
}
