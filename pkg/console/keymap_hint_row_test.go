package console

import (
	"strings"
	"testing"
)

func TestKeymapHintRow_EmptyRegistry(t *testing.T) {
	// Trigger lazy initialization before swapping the global so the
	// cleanup restores a non-nil registry. Otherwise capturing nil
	// and restoring it leaves globalKeymap nil, which breaks
	// later tests that expect Register() on the global to work.
	_ = GlobalKeymap()
	reg := newKeymapRegistry()
	old := globalKeymap
	globalKeymap = reg
	t.Cleanup(func() { globalKeymap = old })

	got := KeymapHintRow()
	if got != "" {
		t.Errorf("KeymapHintRow() = %q, want empty string for empty registry", got)
	}
}

func TestKeymapHintRow_SingleEntry(t *testing.T) {
	_ = GlobalKeymap()
	reg := newKeymapRegistry()
	reg.Register(KeymapEntry{
		Key:         "Alt+T",
		Action:      "test.single",
		Description: "Toggle the thing",
	})
	old := globalKeymap
	globalKeymap = reg
	t.Cleanup(func() { globalKeymap = old })

	got := KeymapHintRow()
	if !strings.Contains(got, "Alt+T") {
		t.Errorf("KeymapHintRow() = %q, should contain 'Alt+T'", got)
	}
	if !strings.Contains(got, "Toggle the thing") {
		t.Errorf("KeymapHintRow() = %q, should contain description", got)
	}
}

func TestKeymapHintRow_MultipleEntries(t *testing.T) {
	_ = GlobalKeymap()
	reg := newKeymapRegistry()
	reg.Register(KeymapEntry{
		Key:         "Alt+T",
		Action:      "test.toggle",
		Description: "Toggle",
	})
	reg.Register(KeymapEntry{
		Key:         "Alt+V",
		Action:      "test.verbose",
		Description: "Verbose",
	})
	reg.Register(KeymapEntry{
		Key:         "Alt+R",
		Action:      "test.reset",
		Description: "Reset",
	})
	old := globalKeymap
	globalKeymap = reg
	t.Cleanup(func() { globalKeymap = old })

	got := KeymapHintRow()
	if !strings.Contains(got, "Alt+T Toggle") {
		t.Errorf("missing first entry in %q", got)
	}
	if !strings.Contains(got, "Alt+V Verbose") {
		t.Errorf("missing second entry in %q", got)
	}
	if !strings.Contains(got, "Alt+R Reset") {
		t.Errorf("missing third entry in %q", got)
	}
	// Entries should be joined by " · ".
	if !strings.Contains(got, " · ") {
		t.Errorf("entries should be joined by ' · ', got %q", got)
	}
}

func TestKeymapHintRow_TruncatesLongDescription(t *testing.T) {
	_ = GlobalKeymap()
	reg := newKeymapRegistry()
	reg.Register(KeymapEntry{
		Key:         "Alt+L",
		Action:      "test.long",
		Description: "This is a very long description that should definitely be truncated to fit within the footer hint row width limit",
	})
	old := globalKeymap
	globalKeymap = reg
	t.Cleanup(func() { globalKeymap = old })

	got := KeymapHintRow()
	// The label portion (after "Alt+L ") should be truncated with "…".
	if !strings.Contains(got, "…") {
		t.Errorf("expected ellipsis for long description, got %q", got)
	}
	// The full output should be reasonably short (key + truncated label).
	if len(got) > 50 {
		t.Errorf("KeymapHintRow() = %q (len=%d), expected <= 50 for a single truncated entry", got, len(got))
	}
}

func TestKeymapHintRow_OrderPreserved(t *testing.T) {
	_ = GlobalKeymap()
	reg := newKeymapRegistry()
	reg.Register(KeymapEntry{Key: "Alt+A", Action: "a", Description: "First"})
	reg.Register(KeymapEntry{Key: "Alt+B", Action: "b", Description: "Second"})
	old := globalKeymap
	globalKeymap = reg
	t.Cleanup(func() { globalKeymap = old })

	got := KeymapHintRow()
	// "Alt+A" should appear before "Alt+B".
	a := strings.Index(got, "Alt+A")
	b := strings.Index(got, "Alt+B")
	if a < 0 || b < 0 || a > b {
		t.Errorf("entries should be in registration order, got %q", got)
	}
}
