package webui

import (
	"bytes"
	"encoding/json"
	"sync"
	"testing"
)

// ------------------------------------------------------------------------
// sessRing tests — thread-safe circular byte buffer
// ------------------------------------------------------------------------

func TestNewSessRing_Capacity(t *testing.T) {
	r := newSessRing()
	if len(r.data) != ringCapacity {
		t.Errorf("expected ring capacity %d, got %d", ringCapacity, len(r.data))
	}
	if r.n != 0 {
		t.Errorf("expected initial size 0, got %d", r.n)
	}
	if r.head != 0 {
		t.Errorf("expected initial head 0, got %d", r.head)
	}
}

func TestSessRing_WriteAndSnapshot(t *testing.T) {
	r := newSessRing()

	data := []byte("hello world")
	r.write(data)

	snap := r.snapshot()
	if string(snap) != "hello world" {
		t.Errorf("expected 'hello world', got %q", string(snap))
	}
}

func TestSessRing_WriteEmpty(t *testing.T) {
	r := newSessRing()
	r.write([]byte{})
	r.write(nil)

	if r.n != 0 {
		t.Errorf("expected size 0 after empty writes, got %d", r.n)
	}
	snap := r.snapshot()
	if snap != nil {
		t.Errorf("expected nil snapshot, got %q", string(snap))
	}
}

func TestSessRing_MultipleWrites(t *testing.T) {
	r := newSessRing()
	r.write([]byte("hello "))
	r.write([]byte("world"))

	snap := r.snapshot()
	if string(snap) != "hello world" {
		t.Errorf("expected 'hello world', got %q", string(snap))
	}
}

func TestSessRing_SnapshotEmpty(t *testing.T) {
	r := newSessRing()
	snap := r.snapshot()
	if snap != nil {
		t.Errorf("expected nil snapshot for empty ring, got %q", string(snap))
	}
}

func TestSessRing_WrapAround(t *testing.T) {
	// Create a ring with small capacity for testing
	r := &sessRing{data: make([]byte, 16)}

	// Write more data than capacity to force wrap-around
	r.write([]byte("0123456789")) // 10 bytes
	r.write([]byte("abcdef"))     // 6 more = 16 total

	expected := "0123456789abcdef"
	snap := r.snapshot()
	if string(snap) != expected {
		t.Errorf("expected %q, got %q", expected, string(snap))
	}

	// Now overflow: write 4 more bytes, dropping oldest 4
	r.write([]byte("ABCD"))
	expected = "456789abcdefABCD"
	snap = r.snapshot()
	if string(snap) != expected {
		t.Errorf("after overflow expected %q, got %q", expected, string(snap))
	}
}

func TestSessRing_OverflowDropsOldest(t *testing.T) {
	r := &sessRing{data: make([]byte, 8)}

	r.write([]byte("AABBCCDD")) // fills exactly
	snap := r.snapshot()
	if string(snap) != "AABBCCDD" {
		t.Errorf("expected 'AABBCCDD', got %q", string(snap))
	}

	// Write 4 more bytes — drops first 4
	r.write([]byte("EEFF"))
	snap = r.snapshot()
	if string(snap) != "CCDDEEFF" {
		t.Errorf("after overflow expected 'CCDDEEFF', got %q", string(snap))
	}
}

func TestSessRing_FullOverwrite(t *testing.T) {
	r := &sessRing{data: make([]byte, 4)}

	// Write more than capacity in one call
	r.write([]byte("ABCDEFGH")) // 8 bytes, capacity 4

	snap := r.snapshot()
	if string(snap) != "EFGH" {
		t.Errorf("expected last 4 bytes 'EFGH', got %q", string(snap))
	}
}

func TestSessRing_SingleByteWrites(t *testing.T) {
	r := &sessRing{data: make([]byte, 4)}

	for i := byte('A'); i <= 'H'; i++ {
		r.write([]byte{i})
	}

	snap := r.snapshot()
	if string(snap) != "EFGH" {
		t.Errorf("expected 'EFGH', got %q", string(snap))
	}
}

func TestSessRing_LargeWrapAround(t *testing.T) {
	// Simulate real usage with the actual ring capacity
	r := newSessRing()

	// Write 128 KB of 'A'
	bigA := bytes.Repeat([]byte("A"), 128*1024)
	r.write(bigA)

	// Write 128 KB of 'B' — fills the ring
	bigB := bytes.Repeat([]byte("B"), 128*1024)
	r.write(bigB)

	snap := r.snapshot()
	if len(snap) != ringCapacity {
		t.Errorf("expected snapshot size %d, got %d", ringCapacity, len(snap))
	}

	// First half should be 'A', second half 'B'
	for i, b := range snap[:128*1024] {
		if b != 'A' {
			t.Errorf("expected 'A' at position %d, got %c", i, b)
			break
		}
	}
	for i, b := range snap[128*1024:] {
		if b != 'B' {
			t.Errorf("expected 'B' at position %d, got %c", 128*1024+i, b)
			break
		}
	}

	// Now write another 128 KB of 'C' — drops oldest 128 KB of 'A'
	bigC := bytes.Repeat([]byte("C"), 128*1024)
	r.write(bigC)

	snap = r.snapshot()
	if len(snap) != ringCapacity {
		t.Errorf("expected snapshot size %d, got %d", ringCapacity, len(snap))
	}

	// Should now be 128 KB of 'B' followed by 128 KB of 'C'
	for i, b := range snap[:128*1024] {
		if b != 'B' {
			t.Errorf("expected 'B' at position %d, got %c", i, b)
			break
		}
	}
	for i, b := range snap[128*1024:] {
		if b != 'C' {
			t.Errorf("expected 'C' at position %d, got %c", 128*1024+i, b)
			break
		}
	}
}

func TestSessRing_ConcurrentReadWrite(t *testing.T) {
	r := newSessRing()
	var wg sync.WaitGroup

	// Concurrent writers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				r.write([]byte{byte(id)})
			}
		}(i)
	}

	// Concurrent snapshots
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				snap := r.snapshot()
				// Snapshot should always be valid (not garbage)
				if len(snap) > ringCapacity {
					t.Errorf("snapshot size %d exceeds capacity %d", len(snap), ringCapacity)
				}
			}
		}()
	}

	wg.Wait()

	// After all writes, total bytes = 10 * 100 = 1000
	snap := r.snapshot()
	if len(snap) > ringCapacity {
		t.Errorf("final snapshot size %d exceeds capacity %d", len(snap), ringCapacity)
	}
}

// ------------------------------------------------------------------------
// GitStatus / GitFile JSON serialization
// ------------------------------------------------------------------------

func TestGitStatus_JSONSerialization(t *testing.T) {
	status := GitStatus{
		Branch:    "main",
		Ahead:     2,
		Behind:    1,
		Staged:    []GitFile{{Path: "a.go", Status: "M", Staged: true}},
		Modified:  []GitFile{{Path: "b.go", Status: "M"}},
		Untracked: []GitFile{{Path: "c.go", Status: "??"}},
		Deleted:   []GitFile{{Path: "d.go", Status: "D"}},
		Renamed:   []GitFile{{Path: "e.go", Status: "R"}},
		Truncated: false,
	}

	data, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("failed to marshal GitStatus: %v", err)
	}

	var decoded GitStatus
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal GitStatus: %v", err)
	}

	if decoded.Branch != "main" {
		t.Errorf("expected branch 'main', got %q", decoded.Branch)
	}
	if decoded.Ahead != 2 {
		t.Errorf("expected ahead 2, got %d", decoded.Ahead)
	}
	if decoded.Behind != 1 {
		t.Errorf("expected behind 1, got %d", decoded.Behind)
	}
	if len(decoded.Staged) != 1 || decoded.Staged[0].Path != "a.go" {
		t.Errorf("expected 1 staged file 'a.go', got %+v", decoded.Staged)
	}
	if len(decoded.Modified) != 1 || decoded.Modified[0].Path != "b.go" {
		t.Errorf("expected 1 modified file 'b.go', got %+v", decoded.Modified)
	}
}

func TestGitFile_JSONSerialization(t *testing.T) {
	tests := []struct {
		name     string
		file     GitFile
		wantJSON string
	}{
		{
			name:     "basic file",
			file:     GitFile{Path: "test.go", Status: "M"},
			wantJSON: `{"path":"test.go","status":"M"}`,
		},
		{
			name:     "staged file includes omitempty",
			file:     GitFile{Path: "test.go", Status: "A", Staged: true},
			wantJSON: `{"path":"test.go","status":"A","staged":true}`,
		},
		{
			name:     "staged false omitted",
			file:     GitFile{Path: "test.go", Status: "M", Staged: false},
			wantJSON: `{"path":"test.go","status":"M"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.file)
			if err != nil {
				t.Fatalf("failed to marshal: %v", err)
			}
			if string(data) != tt.wantJSON {
				t.Errorf("expected %s, got %s", tt.wantJSON, string(data))
			}
		})
	}
}

func TestGitStatus_EmptyLists(t *testing.T) {
	status := GitStatus{
		Branch:    "main",
		Staged:    []GitFile{},
		Modified:  nil,
		Untracked: nil,
	}

	data, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded GitStatus
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	// Both nil and empty slice should round-trip
	if decoded.Branch != "main" {
		t.Errorf("expected branch 'main', got %q", decoded.Branch)
	}
}

// ------------------------------------------------------------------------
// ShellInfo JSON serialization
// ------------------------------------------------------------------------

func TestShellInfo_JSONSerialization(t *testing.T) {
	info := ShellInfo{
		Name:    "bash",
		Path:    "/bin/bash",
		Default: true,
	}

	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("failed to marshal ShellInfo: %v", err)
	}

	var decoded ShellInfo
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal ShellInfo: %v", err)
	}

	if decoded.Name != "bash" {
		t.Errorf("expected name 'bash', got %q", decoded.Name)
	}
	if decoded.Path != "/bin/bash" {
		t.Errorf("expected path '/bin/bash', got %q", decoded.Path)
	}
	if !decoded.Default {
		t.Error("expected default true")
	}
}

// ------------------------------------------------------------------------
// TerminalSession subscribe/unsubscribe/broadcast
// ------------------------------------------------------------------------

func TestTerminalSession_SubscribeUnsubscribe(t *testing.T) {
	s := &TerminalSession{
		ring: newSessRing(),
	}

	sub := s.subscribe()
	if sub == nil || sub.ch == nil {
		t.Fatal("expected non-nil subscriber with channel")
	}

	// Should be in the list
	s.subsMu.Lock()
	count := len(s.subs)
	s.subsMu.Unlock()
	if count != 1 {
		t.Errorf("expected 1 subscriber, got %d", count)
	}

	s.unsubscribe(sub)

	s.subsMu.Lock()
	count = len(s.subs)
	s.subsMu.Unlock()
	if count != 0 {
		t.Errorf("expected 0 subscribers after unsubscribe, got %d", count)
	}

	// Unsubscribing again should be safe (no-op)
	s.unsubscribe(sub)
}

func TestTerminalSession_Broadcast(t *testing.T) {
	s := &TerminalSession{
		ring: newSessRing(),
	}

	sub := s.subscribe()

	s.broadcast([]byte("hello"))

	// Should receive the message
	select {
	case msg := <-sub.ch:
		if string(msg) != "hello" {
			t.Errorf("expected 'hello', got %q", string(msg))
		}
	default:
		t.Error("expected to receive broadcast message")
	}

	// Ring should have the data
	snap := s.ring.snapshot()
	if string(snap) != "hello" {
		t.Errorf("ring expected 'hello', got %q", string(snap))
	}
}

func TestTerminalSession_BroadcastEvictsFullChannel(t *testing.T) {
	s := &TerminalSession{
		ring: newSessRing(),
	}

	sub := s.subscribe()

	// Fill the channel buffer (capacity 10000)
	for i := 0; i < 10000; i++ {
		s.subsMu.Lock()
		sub.ch <- []byte("x")
		s.subsMu.Unlock()
	}

	// Next broadcast should evict the subscriber
	s.broadcast([]byte("overflow"))

	// Subscriber should be evicted
	s.subsMu.Lock()
	count := len(s.subs)
	s.subsMu.Unlock()
	if count != 0 {
		t.Errorf("expected 0 subscribers after eviction, got %d", count)
	}
}

func TestTerminalSession_CloseAllSubs(t *testing.T) {
	s := &TerminalSession{
		ring: newSessRing(),
	}

	sub1 := s.subscribe()
	sub2 := s.subscribe()

	s.closeAllSubs()

	// Both channels should be closed
	_, ok1 := <-sub1.ch
	_, ok2 := <-sub2.ch
	if ok1 || ok2 {
		t.Error("expected both channels to be closed")
	}

	s.subsMu.Lock()
	count := len(s.subs)
	s.subsMu.Unlock()
	if count != 0 {
		t.Errorf("expected 0 subscribers after closeAllSubs, got %d", count)
	}
}

func TestTerminalSession_BroadcastToMultiple(t *testing.T) {
	s := &TerminalSession{
		ring: newSessRing(),
	}

	sub1 := s.subscribe()
	sub2 := s.subscribe()

	s.broadcast([]byte("test"))

	// Both should receive
	msg1 := <-sub1.ch
	msg2 := <-sub2.ch
	if string(msg1) != "test" || string(msg2) != "test" {
		t.Errorf("expected both subscribers to receive 'test', got %q and %q", string(msg1), string(msg2))
	}
}
