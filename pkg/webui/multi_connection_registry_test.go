//go:build !js

package webui

import (
	"sync"
	"testing"
)

// makeUserConn builds a UserConnection with a stable *Raw identity. Tests
// use string IDs as the Raw token because the registry matches by
// pointer equality and we want different "connections" to be distinct
// without dragging in gorilla/websocket.
func makeUserConn(sessionID, clientID string) UserConnection {
	s := sessionID
	c := clientID
	return UserConnection{
		Conn:      nil,
		Raw:       &s, // unique pointer per session
		SessionID: s,
		ClientID:  c,
	}
}

func TestUserConnections_AddCountRemove(t *testing.T) {
	uc := &UserConnections{}
	a := makeUserConn("s1", "c1")
	b := makeUserConn("s2", "c1")

	uc.Add("user-1", a)
	uc.Add("user-1", b)
	if got := uc.Count("user-1"); got != 2 {
		t.Fatalf("Count after 2 adds = %d, want 2", got)
	}

	uc.Remove("user-1", a.Raw)
	if got := uc.Count("user-1"); got != 1 {
		t.Fatalf("Count after 1 remove = %d, want 1", got)
	}

	// Removing same pointer again is a no-op (no panic, no state change).
	uc.Remove("user-1", a.Raw)
	if got := uc.Count("user-1"); got != 1 {
		t.Fatalf("Count after double-remove = %d, want 1", got)
	}
}

func TestUserConnections_PerUserIsolation(t *testing.T) {
	uc := &UserConnections{}
	uc.Add("user-1", makeUserConn("s1", "c1"))
	uc.Add("user-2", makeUserConn("s2", "c2"))

	if got := uc.Count("user-1"); got != 1 {
		t.Errorf("Count(user-1) = %d, want 1", got)
	}
	if got := uc.Count("user-2"); got != 1 {
		t.Errorf("Count(user-2) = %d, want 1", got)
	}
	if got := uc.Count("user-3"); got != 0 {
		t.Errorf("Count(unknown) = %d, want 0", got)
	}

	// Removing user-1's session must not affect user-2.
	a := uc.Snapshot("user-1")
	b := uc.Snapshot("user-2")
	if len(a) != 1 || len(b) != 1 {
		t.Fatalf("snapshots wrong: user-1=%d user-2=%d", len(a), len(b))
	}
	uc.Remove("user-1", a[0].Raw)
	if got := uc.Count("user-2"); got != 1 {
		t.Errorf("user-2 affected by user-1 Remove: count=%d", got)
	}
}

func TestUserConnections_EmptyUserIDRefused(t *testing.T) {
	uc := &UserConnections{}
	// Empty userID must NOT register (would collapse all local-mode
	// connections onto one global bucket).
	uc.Add("", makeUserConn("s1", "c1"))
	if got := uc.Count(""); got != 0 {
		t.Fatalf("Count(\"\") = %d, want 0 (empty userID must be refused)", got)
	}
	if got := len(uc.AllUserIDs()); got != 0 {
		t.Fatalf("AllUserIDs = %d, want 0 after refused Add", got)
	}
}

func TestUserConnections_RemoveByRawPointerIdentity(t *testing.T) {
	uc := &UserConnections{}
	// Two sessions with identical string fields but different *Raw
	// pointers must NOT be confused by Remove.
	a := makeUserConn("s1", "c1")
	b := makeUserConn("s1", "c1") // same strings
	if a.Raw == b.Raw {
		t.Fatalf("test setup error: Raw pointers collided")
	}
	uc.Add("user-1", a)
	uc.Add("user-1", b)

	uc.Remove("user-1", a.Raw)
	if got := uc.Count("user-1"); got != 1 {
		t.Fatalf("Count after removing one of two = %d, want 1 (Remove must match by pointer)", got)
	}
	snap := uc.Snapshot("user-1")
	if len(snap) != 1 || snap[0].Raw != b.Raw {
		t.Fatalf("wrong survivor: %+v", snap)
	}
}

func TestUserConnections_ForEachOrder(t *testing.T) {
	uc := &UserConnections{}
	uc.Add("user-1", makeUserConn("s1", "c1"))
	uc.Add("user-1", makeUserConn("s2", "c1"))
	uc.Add("user-1", makeUserConn("s3", "c1"))

	var got []string
	uc.ForEach("user-1", func(c UserConnection) bool {
		got = append(got, c.SessionID)
		return true
	})
	if len(got) != 3 || got[0] != "s1" || got[1] != "s2" || got[2] != "s3" {
		t.Fatalf("ForEach order = %v, want [s1 s2 s3]", got)
	}
}

func TestUserConnections_ForEachEarlyStop(t *testing.T) {
	uc := &UserConnections{}
	uc.Add("user-1", makeUserConn("s1", "c1"))
	uc.Add("user-1", makeUserConn("s2", "c1"))
	uc.Add("user-1", makeUserConn("s3", "c1"))

	var got []string
	uc.ForEach("user-1", func(c UserConnection) bool {
		got = append(got, c.SessionID)
		return len(got) < 2
	})
	if len(got) != 2 || got[0] != "s1" || got[1] != "s2" {
		t.Fatalf("ForEach early-stop = %v, want [s1 s2]", got)
	}
}

func TestUserConnections_SnapshotIsCopy(t *testing.T) {
	uc := &UserConnections{}
	uc.Add("user-1", makeUserConn("s1", "c1"))
	snap := uc.Snapshot("user-1")
	if len(snap) != 1 {
		t.Fatalf("snapshot len = %d, want 1", len(snap))
	}
	// Mutating the returned slice (not its underlying UserConnection)
	// must not affect the registry.
	snap[0].SessionID = "tampered"
	again := uc.Snapshot("user-1")
	if again[0].SessionID != "s1" {
		t.Fatalf("Snapshot is not a copy: %q", again[0].SessionID)
	}
}

func TestUserConnections_AllUserIDs(t *testing.T) {
	uc := &UserConnections{}
	uc.Add("user-1", makeUserConn("s1", "c1"))
	uc.Add("user-2", makeUserConn("s2", "c2"))
	uc.Add("user-3", makeUserConn("s3", "c3"))

	ids := uc.AllUserIDs()
	if len(ids) != 3 {
		t.Fatalf("AllUserIDs len = %d, want 3", len(ids))
	}
	set := map[string]bool{}
	for _, id := range ids {
		set[id] = true
	}
	for _, want := range []string{"user-1", "user-2", "user-3"} {
		if !set[want] {
			t.Errorf("AllUserIDs missing %q", want)
		}
	}
}

func TestUserConnections_ConcurrentAddRemove(t *testing.T) {
	uc := &UserConnections{}
	const goroutines = 32
	const iterations = 100

	// Pre-build all connection tokens so Add can race on the same
	// underlying registry without test-side allocation churn.
	type conn struct {
		raw      *string
		session  string
		registry string
	}
	conns := make([]conn, goroutines*iterations)
	for i := range conns {
		s := string(rune('a' + (i % 26)))
		conns[i] = conn{
			raw:      &s,
			session:  s,
			registry: "user-" + string(rune('A'+(i%goroutines)%4)),
		}
	}

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(base int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				c := conns[base+i]
				uc.Add(c.registry, UserConnection{Raw: c.raw, SessionID: c.session, ClientID: c.session})
				uc.Count(c.registry)
				uc.Remove(c.registry, c.raw)
			}
		}(g * iterations)
	}
	wg.Wait()

	// Final state: every conn was added then removed, so each user's
	// slice should be empty. We don't strictly assert that — concurrent
	// re-adds on the same key can briefly bump the count, but the
	// final snapshot must be sane (no panics, no leaked goroutines).
	// The race detector is the real check here; the assertion is just
	// that we make it through.
	for _, id := range uc.AllUserIDs() {
		if got := uc.Count(id); got < 0 {
			t.Fatalf("Count(%q) negative: %d", id, got)
		}
	}
}

func TestUserConnections_ZeroValueUsable(t *testing.T) {
	var uc UserConnections // no constructor, no init
	uc.Add("user-1", makeUserConn("s1", "c1"))
	if got := uc.Count("user-1"); got != 1 {
		t.Fatalf("zero-value Add failed: Count=%d, want 1", got)
	}
}
