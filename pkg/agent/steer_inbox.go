package agent

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// A steer inbox lets something outside the process steer a run that has no
// terminal: a host drops a message file into a directory and the run reads it
// at its next loop boundary, as if the person had typed it.
//
// Each message is one file, <id>.msg, written atomically (to a temporary name,
// then renamed). The run claims a message by renaming it to <id>.taken, so
// exactly one of "the run read it" and "the sender took it back" happens:
// taking a message back is deleting <id>.msg, which fails once the run has
// claimed it. A message is claimed only when it is about to be delivered, so
// it stays retractable until the run reads it.
//
// Ids sort in the order the messages were sent (the sender's choice, e.g. a
// time-ordered UUID); messages are read oldest first, one per boundary.

const (
	inboxMessageExt = ".msg"
	inboxTakenExt   = ".taken"
	// maxInboxSteerBytes bounds one inbox message; a larger file is set aside
	// unread and reported dropped.
	maxInboxSteerBytes = 8 << 10
)

// The states a steer read from the inbox ends a run in (SteerInboxReport).
const (
	inboxSteerLanded  = "landed"
	inboxSteerDropped = "dropped"
	inboxSteerUnread  = "unread"
)

// InboxSteer is one inbox message's outcome for the run.
type InboxSteer struct {
	ID    string `json:"id"`
	State string `json:"state"`
}

type steerInbox struct {
	dir string

	mu     sync.Mutex
	order  []string
	states map[string]string
}

// SetSteerInbox makes the run read steers from dir (see the package comment
// above). An empty dir turns the inbox off.
func (a *Agent) SetSteerInbox(dir string) {
	if a == nil {
		return
	}
	a.steerStageMu.Lock()
	defer a.steerStageMu.Unlock()
	if strings.TrimSpace(dir) == "" {
		a.steerInbox = nil
		return
	}
	a.steerInbox = &steerInbox{dir: dir, states: map[string]string{}}
}

func (a *Agent) getSteerInbox() *steerInbox {
	if a == nil {
		return nil
	}
	a.steerStageMu.Lock()
	defer a.steerStageMu.Unlock()
	return a.steerInbox
}

// claimInboxSteer stages the oldest inbox message when no steer is waiting to
// be delivered, so the next boundary delivers it. One message at a time: the
// rest stay in the inbox, retractable, until their turn.
func (a *Agent) claimInboxSteer() {
	in := a.getSteerInbox()
	if in == nil || a.PendingSteerCount() > 0 {
		return
	}
	for _, id := range inboxMessageIDs(in.dir) {
		msg := filepath.Join(in.dir, id+inboxMessageExt)
		taken := filepath.Join(in.dir, id+inboxTakenExt)
		if err := os.Rename(msg, taken); err != nil {
			// Taken back by its sender since the listing: try the next.
			continue
		}
		text, ok := readInboxMessage(taken)
		if !ok {
			a.reportInboxSteer(id, inboxSteerDropped, "unreadable or over the size limit")
			continue
		}
		if err := a.stageSteer(text, id); err != nil {
			// The stage is full: give the message back for a later boundary.
			_ = os.Rename(taken, msg)
			return
		}
		in.mu.Lock()
		if _, seen := in.states[id]; !seen {
			in.order = append(in.order, id)
		}
		in.states[id] = inboxSteerUnread
		in.mu.Unlock()
		return
	}
}

// inboxMessageIDs lists the unclaimed message ids in dir, oldest first.
func inboxMessageIDs(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var ids []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, inboxMessageExt) {
			continue
		}
		if id := strings.TrimSuffix(name, inboxMessageExt); id != "" {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

// readInboxMessage reads a claimed message: UTF-8 text, not empty, within
// maxInboxSteerBytes.
func readInboxMessage(path string) (string, bool) {
	info, err := os.Stat(path)
	if err != nil || info.Size() > maxInboxSteerBytes {
		return "", false
	}
	b, err := os.ReadFile(path)
	if err != nil || !utf8.Valid(b) {
		return "", false
	}
	text := strings.TrimSpace(string(b))
	return text, text != ""
}

// reportInboxSteer records an inbox message's outcome and publishes it, so a
// host following the run's progress learns it as it happens.
func (a *Agent) reportInboxSteer(id, state, reason string) {
	in := a.getSteerInbox()
	if in == nil || id == "" {
		return
	}
	in.mu.Lock()
	if _, seen := in.states[id]; !seen {
		in.order = append(in.order, id)
	}
	in.states[id] = state
	in.mu.Unlock()
	switch state {
	case inboxSteerLanded:
		a.publishEvent(events.EventTypeSteerLanded, map[string]interface{}{"id": id})
	case inboxSteerDropped:
		a.publishEvent(events.EventTypeSteerDropped, map[string]interface{}{"id": id, "reason": reason})
	}
}

// SteerInboxReport is every inbox message's outcome at the end of the run:
// landed, dropped, or unread (claimed but never delivered, or still waiting
// in the inbox). Nil when the run has no inbox.
func (a *Agent) SteerInboxReport() []InboxSteer {
	in := a.getSteerInbox()
	if in == nil {
		return nil
	}
	in.mu.Lock()
	out := make([]InboxSteer, 0, len(in.order))
	seen := map[string]bool{}
	for _, id := range in.order {
		out = append(out, InboxSteer{ID: id, State: in.states[id]})
		seen[id] = true
	}
	in.mu.Unlock()
	for _, id := range inboxMessageIDs(in.dir) {
		if !seen[id] {
			out = append(out, InboxSteer{ID: id, State: inboxSteerUnread})
		}
	}
	return out
}
