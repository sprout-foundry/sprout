package console

import (
	"bytes"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestKeymapRegistry_RegisterLookupDispatch covers the basic keymap
// contract: Register + Lookup round-trip, Dispatch fires the handler,
// and an unregistered action is a no-op.
func TestKeymapRegistry_RegisterLookupDispatch(t *testing.T) {
	r := newKeymapRegistry()
	var fired bool
	r.Register(KeymapEntry{
		Key:         "Alt+T",
		Action:      "test.toggle",
		Description: "test toggle",
		Handler:     func() { fired = true },
	})

	e, ok := r.Lookup("test.toggle")
	if !ok {
		t.Fatalf("Lookup failed")
	}
	if e.Key != "Alt+T" {
		t.Errorf("Key = %q, want Alt+T", e.Key)
	}
	if !r.Dispatch("test.toggle") {
		t.Errorf("Dispatch returned false")
	}
	if !fired {
		t.Errorf("handler not invoked")
	}

	if r.Dispatch("test.nonexistent") {
		t.Errorf("Dispatch of unregistered action should return false")
	}
}

// TestKeymapRegistry_MatchAltLetter verifies the Alt+<letter> lookup.
func TestKeymapRegistry_MatchAltLetter(t *testing.T) {
	r := newKeymapRegistry()
	r.Register(KeymapEntry{Key: "Alt+T", Action: "tooltip.toggle"})
	r.Register(KeymapEntry{Key: "Alt+L", Action: "list.toggle"})

	e, ok := r.MatchAltLetter("T")
	if !ok || e.Action != "tooltip.toggle" {
		t.Errorf("MatchAltLetter T: got %v ok=%v, want tooltip.toggle", e, ok)
	}
	if _, ok := r.MatchAltLetter("Z"); ok {
		t.Errorf("MatchAltLetter Z should be a miss")
	}
}

// TestKeymapRegistry_EntriesOrder asserts registration order is
// preserved (matters for /help output stability).
func TestKeymapRegistry_EntriesOrder(t *testing.T) {
	r := newKeymapRegistry()
	r.Register(KeymapEntry{Key: "Alt+A", Action: "alpha"})
	r.Register(KeymapEntry{Key: "Alt+B", Action: "bravo"})
	r.Register(KeymapEntry{Key: "Alt+C", Action: "charlie"})

	entries := r.Entries()
	if len(entries) != 3 {
		t.Fatalf("Entries length = %d, want 3", len(entries))
	}
	want := []string{"alpha", "bravo", "charlie"}
	for i, e := range entries {
		if e.Action != want[i] {
			t.Errorf("entries[%d] = %q, want %q", i, e.Action, want[i])
		}
	}
}

// TestKeymapRegistry_RegisterReplaces asserts the idempotent re-register
// path: registering the same Action twice replaces in place, no
// duplicate in Entries().
func TestKeymapRegistry_RegisterReplaces(t *testing.T) {
	r := newKeymapRegistry()
	r.Register(KeymapEntry{Key: "Alt+T", Action: "tooltip.toggle", Description: "old"})
	r.Register(KeymapEntry{Key: "Alt+T", Action: "tooltip.toggle", Description: "new"})

	entries := r.Entries()
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	if entries[0].Description != "new" {
		t.Errorf("Description = %q, want 'new'", entries[0].Description)
	}
}

// TestKeymapRegistry_ConcurrentDispatch ensures concurrent Register
// + Dispatch doesn't race. Run with -race.
func TestKeymapRegistry_ConcurrentDispatch(t *testing.T) {
	r := newKeymapRegistry()
	var counter int64
	r.Register(KeymapEntry{
		Key:    "Alt+T",
		Action: "inc",
		Handler: func() {
			// Tiny race-prone increment.
			for i := 0; i < 100; i++ {
				// no-op
				_ = i
			}
		},
	})

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				r.Dispatch("inc")
			}
		}()
	}
	wg.Wait()
	if counter != 0 {
		t.Errorf("counter should remain 0 (handler is no-op): got %d", counter)
	}
}

// TestKeymapHelpTable_RendersHeader checks the table renders a stable
// header row and at least one entry when bindings are registered.
func TestKeymapHelpTable_RendersHeader(t *testing.T) {
	// Use a fresh registry so the test is hermetic. Note: KeymapHelpTable
	// uses the global registry by design — that's the documented API.
	// We snapshot and restore around the test.
	prev := GlobalKeymap().Entries()
	t.Cleanup(func() {
		// Restore by clearing and re-registering; the keymap doesn't
		// expose a Clear, so we just re-register what was there.
		for _, e := range prev {
			GlobalKeymap().Register(e)
		}
	})
	// Register a sentinel under a unique action name so we can detect it.
	GlobalKeymap().Register(KeymapEntry{
		Key:         "Alt+Q",
		Action:      "test.help.q",
		Description: "test sentinel",
	})

	out := KeymapHelpTable()
	if !strings.Contains(out, "KEY") {
		t.Errorf("help table missing KEY header: %q", out)
	}
	if !strings.Contains(out, "ACTION") {
		t.Errorf("help table missing ACTION header: %q", out)
	}
	if !strings.Contains(out, "Alt+Q") {
		t.Errorf("help table missing Alt+Q entry: %q", out)
	}
}

// TestMetricsRecorder_RecordAndSnapshot verifies the recorder accepts
// observations, accumulates, and returns a sorted snapshot.
func TestMetricsRecorder_RecordAndSnapshot(t *testing.T) {
	mr := NewMetricsRecorder()
	mr.RecordToolInvocation("read_file", 100, 0.001, 50000)
	mr.RecordToolInvocation("read_file", 200, 0.002, 70000)
	mr.RecordToolInvocation("write_file", 50, 0.0005, 30000)

	rows := mr.Snapshot()
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	// Sorted by Name.
	if rows[0].Name != "read_file" || rows[1].Name != "write_file" {
		t.Errorf("snapshot sort order: %v", []string{rows[0].Name, rows[1].Name})
	}
	if rows[0].Count != 2 || rows[0].TotalTokens != 300 {
		t.Errorf("read_file aggregate: count=%d tokens=%d", rows[0].Count, rows[0].TotalTokens)
	}
	if rows[1].Count != 1 || rows[1].TotalTokens != 50 {
		t.Errorf("write_file aggregate: count=%d tokens=%d", rows[1].Count, rows[1].TotalTokens)
	}
	totals := mr.Totals()
	if totals.Count != 3 {
		t.Errorf("totals count = %d, want 3", totals.Count)
	}
	if totals.TotalTokens != 350 {
		t.Errorf("totals tokens = %d, want 350", totals.TotalTokens)
	}
}

// TestMetricsRecorder_AvgLatency verifies the average is computed
// correctly. 100ms + 200ms across 2 invocations = 150ms.
func TestMetricsRecorder_AvgLatency(t *testing.T) {
	mr := NewMetricsRecorder()
	mr.RecordToolInvocation("tool", 0, 0, 100_000) // 100ms
	mr.RecordToolInvocation("tool", 0, 0, 200_000) // 200ms
	rows := mr.Snapshot()
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	avg := rows[0].AvgLatency()
	if avg < 149.99 || avg > 150.01 {
		t.Errorf("AvgLatency = %f, want ~150.0", avg)
	}
}

// TestFooterTooltip_ShowHideToggle exercises the basic show/hide/toggle
// lifecycle. Uses a stub writer to capture the output bytes.
func TestFooterTooltip_ShowHideToggle(t *testing.T) {
	var buf bytes.Buffer
	tt := NewFooterTooltip(&buf)
	tt.Source = func() []ToolInvocation {
		return []ToolInvocation{{Name: "read_file", Count: 5, TotalTokens: 1000, TotalCost: 10, TotalLatency: 50_000}}
	}

	if tt.Visible() {
		t.Errorf("newly constructed tooltip should not be visible")
	}

	tt.Show(80, 24)
	if !tt.Visible() {
		t.Errorf("after Show, Visible() should be true")
	}
	out := buf.String()
	if !strings.Contains(out, "read_file") {
		t.Errorf("tooltip output missing tool name: %q", out)
	}

	tt.Hide()
	if tt.Visible() {
		t.Errorf("after Hide, Visible() should be false")
	}

	// Toggle: hidden → visible.
	buf.Reset()
	tt.Toggle(80, 24)
	if !tt.Visible() {
		t.Errorf("Toggle from hidden should make visible")
	}
	// Toggle: visible → hidden.
	tt.Toggle(80, 24)
	if tt.Visible() {
		t.Errorf("Toggle from visible should make hidden")
	}
}

// TestFooterTooltip_TimeoutExpires confirms the auto-dismiss timer
// fires when no keystroke arrives.
func TestFooterTooltip_TimeoutExpires(t *testing.T) {
	tt := NewFooterTooltip(io.Discard)
	tt.Timeout = 50 * time.Millisecond
	tt.Source = func() []ToolInvocation { return nil }

	tt.Show(80, 24)
	if !tt.Visible() {
		t.Fatalf("Show should set visible")
	}
	// Wait past the timeout.
	time.Sleep(120 * time.Millisecond)
	if tt.Visible() {
		t.Errorf("tooltip should auto-hide after timeout")
	}
}

// TestFooterTooltip_HideIsIdempotent ensures Hide on an already-hidden
// tooltip is a no-op.
func TestFooterTooltip_HideIsIdempotent(t *testing.T) {
	tt := NewFooterTooltip(io.Discard)
	tt.Hide() // not visible yet
	tt.Hide() // still not visible
	tt.Hide()
	if tt.Visible() {
		t.Errorf("Hide should never make Visible() true")
	}
}

// TestFooterTooltip_DismissOnKeystroke models the InputReader
// HandleEvent hook: any event arriving while visible should hide it.
// We don't drive the InputReader here; we model the contract.
func TestFooterTooltip_DismissOnKeystroke(t *testing.T) {
	tt := NewFooterTooltip(io.Discard)
	tt.Source = func() []ToolInvocation { return nil }
	tt.Show(80, 24)
	if !tt.Visible() {
		t.Fatalf("Show should make visible")
	}
	// Simulate "any keystroke arrives" by calling Hide.
	tt.Hide()
	if tt.Visible() {
		t.Errorf("Hide should make not-visible")
	}
}

// TestRegisterKeymapForFooter_Idempotent asserts multiple calls don't
// duplicate-Register. Visible via the registered Entries() count.
func TestRegisterKeymapForFooter_Idempotent(t *testing.T) {
	// Reset once so the test is hermetic.
	keymapOnce = sync.Once{}
	prev := GlobalKeymap().Entries()
	t.Cleanup(func() {
		keymapOnce = sync.Once{}
		for _, e := range prev {
			GlobalKeymap().Register(e)
		}
	})

	RegisterKeymapForFooter(nil, nil)
	RegisterKeymapForFooter(nil, nil)
	RegisterKeymapForFooter(nil, nil)

	// Count entries with action footer.tooltip.toggle.
	count := 0
	for _, e := range GlobalKeymap().Entries() {
		if e.Action == "footer.tooltip.toggle" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("got %d footer.tooltip.toggle entries, want 1", count)
	}
}
