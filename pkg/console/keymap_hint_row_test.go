package console

import (
	"strings"
	"sync"
	"testing"
)

func TestKeymapHintRow_ContainsRegisteredBindings(t *testing.T) {
	// Snapshot and restore the global keymap so this test is hermetic.
	prevEntries := GlobalKeymap().Entries()
	prevGlobal := globalKeymap
	t.Cleanup(func() {
		keymapOnce = sync.Once{}
		globalKeymap = prevGlobal
		for _, e := range prevEntries {
			GlobalKeymap().Register(e)
		}
	})

	// Reset and populate with known bindings.
	keymapOnce = sync.Once{}
	globalKeymap = newKeymapRegistry()
	GlobalKeymap().Register(KeymapEntry{
		Key:         "Alt+T",
		Action:      "footer.breakdown.toggle",
		Description: "Show / hide per-tool invocation breakdown above the status footer",
	})
	GlobalKeymap().Register(KeymapEntry{
		Key:         "Alt+V",
		Action:      "output.verbosity.toggle",
		Description: "Cycle output verbosity: default ↔ verbose (more tool detail)",
	})

	got := KeymapHintRow()

	// Must contain both registered keys.
	if !strings.Contains(got, "Alt+T") {
		t.Errorf("KeymapHintRow() = %q, want contains Alt+T", got)
	}
	if !strings.Contains(got, "Alt+V") {
		t.Errorf("KeymapHintRow() = %q, want contains Alt+V", got)
	}

	// Must be single-line (no embedded newlines).
	if strings.Contains(got, "\n") {
		t.Errorf("KeymapHintRow() = %q, must be single-line (no newlines)", got)
	}

	// Must use " · " as separator between entries.
	if !strings.Contains(got, " · ") {
		t.Errorf("KeymapHintRow() = %q, want separator ' · ' between entries", got)
	}

	// Labels must be distinct — the hint is useless if both bindings
	// surface the same word. Catches regressions where label
	// extraction collapses to "toggle" for every .toggle Action.
	parts := strings.Split(got, " · ")
	labels := make(map[string]struct{}, len(parts))
	for _, p := range parts {
		// Each part is "Alt+X <label>" — split off the label.
		fields := strings.SplitN(p, " ", 2)
		if len(fields) < 2 {
			t.Errorf("KeymapHintRow() part %q missing label", p)
			continue
		}
		labels[fields[1]] = struct{}{}
	}
	if len(labels) != len(parts) {
		t.Errorf("KeymapHintRow() = %q, labels not distinct (%d parts, %d unique)",
			got, len(parts), len(labels))
	}
}

func TestKeymapHintRow_EmptyRegistry(t *testing.T) {
	// Snapshot and restore the global keymap so this test is hermetic.
	prevEntries := GlobalKeymap().Entries()
	prevGlobal := globalKeymap
	t.Cleanup(func() {
		keymapOnce = sync.Once{}
		globalKeymap = prevGlobal
		for _, e := range prevEntries {
			GlobalKeymap().Register(e)
		}
	})

	// Reset to an empty registry.
	keymapOnce = sync.Once{}
	globalKeymap = newKeymapRegistry()

	got := KeymapHintRow()
	if got != "" {
		t.Errorf("KeymapHintRow() with empty registry = %q, want empty string", got)
	}
}

func TestKeymapHintRow_SkipsEmptyDescription(t *testing.T) {
	// Snapshot and restore the global keymap so this test is hermetic.
	prevEntries := GlobalKeymap().Entries()
	prevGlobal := globalKeymap
	t.Cleanup(func() {
		keymapOnce = sync.Once{}
		globalKeymap = prevGlobal
		for _, e := range prevEntries {
			GlobalKeymap().Register(e)
		}
	})

	// Reset to a registry where an entry has no description.
	keymapOnce = sync.Once{}
	globalKeymap = newKeymapRegistry()
	GlobalKeymap().Register(KeymapEntry{
		Key:         "Alt+X",
		Action:      "test.skip",
		Description: "", // empty — should be skipped
	})
	GlobalKeymap().Register(KeymapEntry{
		Key:         "Alt+Y",
		Action:      "test.visible",
		Description: "A visible binding",
	})

	got := KeymapHintRow()

	// The Alt+Y entry should appear; Alt+X should not.
	if !strings.Contains(got, "Alt+Y") {
		t.Errorf("KeymapHintRow() = %q, want contains Alt+Y from visible entry", got)
	}
	if strings.Contains(got, "Alt+X") {
		t.Errorf("KeymapHintRow() = %q, should not contain Alt+X (empty description)", got)
	}
}

func TestKeymapHintRow_TruncatesLongLabels(t *testing.T) {
	// Snapshot and restore the global keymap so this test is hermetic.
	prevEntries := GlobalKeymap().Entries()
	prevGlobal := globalKeymap
	t.Cleanup(func() {
		keymapOnce = sync.Once{}
		globalKeymap = prevGlobal
		for _, e := range prevEntries {
			GlobalKeymap().Register(e)
		}
	})

	// Action whose middle segment is very long (> 30 columns). Second-to-last
	// segment extraction will surface this long middle segment for truncation.
	keymapOnce = sync.Once{}
	globalKeymap = newKeymapRegistry()
	GlobalKeymap().Register(KeymapEntry{
		Key:         "Alt+Z",
		Action:      "test.very_long_action_name_that_exceeds_thirty_columns.something",
		Description: "This is a test description that should be truncated",
	})

	got := KeymapHintRow()

	// The label extracted from the action should be truncated with "…".
	if !strings.Contains(got, "…") {
		t.Errorf("KeymapHintRow() = %q, want '…' ellipsis for truncated label", got)
	}
	// The truncated label plus ellipsis should fit within 30 display columns.
	// Extract the label part (after "Alt+Z ").
	prefix := "Alt+Z "
	idx := strings.Index(got, prefix)
	if idx < 0 {
		t.Fatalf("KeymapHintRow() = %q, missing Alt+Z prefix", got)
	}
	label := got[idx+len(prefix):]
	// Use displayWidth to account for wide characters (not needed here
	// but correct for the general case).
	if displayWidth(label) > 30 {
		t.Errorf("label %q exceeds 30 display columns (got %d)", label, displayWidth(label))
	}
}

func TestExtractShortLabel(t *testing.T) {
	cases := []struct {
		name       string
		entry      KeymapEntry
		wantSubstr string
	}{
		{
			// Second-to-last segment — the "thing being controlled".
			name: "action with dots → second-to-last segment",
			entry: KeymapEntry{
				Key:         "Alt+T",
				Action:      "footer.breakdown.toggle",
				Description: "Show / hide per-tool invocation breakdown above the status footer",
			},
			wantSubstr: "breakdown",
		},
		{
			name: "action with dots and clean labels → second-to-last segment",
			entry: KeymapEntry{
				Key:         "Alt+V",
				Action:      "output.verbosity.toggle",
				Description: "Cycle output verbosity: default ↔ verbose (more tool detail)",
			},
			wantSubstr: "verbosity",
		},
		{
			name: "action with one dot → first segment (head, no prevDot)",
			entry: KeymapEntry{
				Action: "footer.breakdown",
			},
			wantSubstr: "footer",
		},
		{
			name: "action without dots → whole action",
			entry: KeymapEntry{
				Action: "simpleaction",
			},
			wantSubstr: "simpleaction",
		},
		{
			name: "no action, description fallback → first word",
			entry: KeymapEntry{
				Description: "Toggle something useful",
			},
			wantSubstr: "Toggle",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := extractShortLabel(c.entry)
			if !strings.Contains(got, c.wantSubstr) {
				t.Errorf("extractShortLabel() = %q, want contains %q", got, c.wantSubstr)
			}
		})
	}
}
