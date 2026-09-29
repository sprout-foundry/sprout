package console

import (
	"testing"
	"time"
)

// drainQueuedKeys runs queued input through the same per-key path as the
// picker's read loop.
func drainQueuedKeys(t *testing.T, s *SelectList) (done bool, val string, ok bool) {
	t.Helper()
	for len(s.pending) > 0 {
		b, _ := s.readByteBefore(time.Now().Add(20 * time.Millisecond))
		key := []byte{b}
		if b >= 0xC0 {
			key = s.collectUTF8(b)
		}
		if done, val, ok = s.processKey(b, len(key), key); done {
			return
		}
	}
	return
}

// A single read can carry several keys (a paste, or keys the console
// delivers together); the picker used to handle only the first byte of
// each read, so a burst like "hello" filtered on "h".
func TestSelectList_ProcessesEveryKeyInABurst(t *testing.T) {
	s := NewSelectList(SelectListOptions{
		Items:      []SelectItem{{Label: "hello world", Value: "hw"}, {Label: "héllo", Value: "accent"}, {Label: "help", Value: "h"}},
		Searchable: true,
	})
	s.pending = []byte("hé")
	drainQueuedKeys(t, s)
	if s.filter != "hé" {
		t.Fatalf("filter = %q, want %q (every queued key, including the 2-byte é)", s.filter, "hé")
	}

	s.pending = []byte("llo\r")
	done, val, ok := drainQueuedKeys(t, s)
	if !done || !ok || val != "accent" {
		t.Fatalf("done=%v ok=%v val=%q, want the héllo item confirmed", done, ok, val)
	}
}

// An arrow key split across reads must still be recognised: the follow-up
// bytes come from the queue rather than a fresh blocking read.
func TestSelectList_EscapeSequenceFromQueue(t *testing.T) {
	s := NewSelectList(SelectListOptions{
		Items: []SelectItem{{Label: "one", Value: "1"}, {Label: "two", Value: "2"}},
	})
	s.pending = []byte("\x1b[B\r")
	done, val, ok := drainQueuedKeys(t, s)
	if !done || !ok || val != "2" {
		t.Fatalf("done=%v ok=%v val=%q, want Down then Enter to confirm the second item", done, ok, val)
	}
}

// A picker dismissed by the first key of a typed-ahead burst forwards the
// whole burst to the prompt, not just that key — but never a queued Enter.
func TestSelectList_DismissForwardsTypedAhead(t *testing.T) {
	s := NewSelectList(SelectListOptions{
		Items:           []SelectItem{{Label: "session", Value: "s"}},
		DismissOnAnyKey: true,
	})
	s.pending = []byte("run echo\rmore")
	done, _, _ := drainQueuedKeys(t, s)
	if !done {
		t.Fatal("expected the first key to dismiss the picker")
	}
	if got := s.DismissKey(); got != "run echo" {
		t.Fatalf("DismissKey() = %q, want %q", got, "run echo")
	}
}
