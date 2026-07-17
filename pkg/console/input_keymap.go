package console

import (
	"sort"
	"strings"
	"sync"
)

// KeymapRegistry is a process-wide table that maps an "Alt+<letter>"
// keypress to a named action plus a callback. The InputReader consults
// the registry on every EventAltLetter; the callback runs synchronously
// in the read-loop goroutine.
//
// This is the canonical place to wire stateless, non-prefix keybindings.
// For bindings that conflict with text input (e.g. anything that
// shouldn't fire while the user is typing inside the steer panel)
// register an Action with a Guard that returns false.
//
// CLI-D-3: this is the keymap table the TODO references. Built as a
// thread-safe registry because both the REPL goroutine and any
// configuration / setup code may register handlers.
type KeymapRegistry struct {
	mu      sync.RWMutex
	entries map[string]KeymapEntry
	order   []string // preserves registration order for /help output
}

// KeymapEntry is one row in the keymap table.
type KeymapEntry struct {
	// Key is the user-facing combo, e.g. "Alt+T". Used for /help
	// documentation; the dispatch path uses Action instead.
	Key string
	// Action is the internal name, e.g. "footer.breakdown.toggle".
	Action string
	// Description is the /help blurb.
	Description string
	// Handler runs on each match. Called synchronously in the REPL
	// goroutine; long-running work should be dispatched elsewhere.
	Handler func()
}

// globalKeymap is the process-wide registry. Accessed via
// RegisterKeymap / LookupKeymap / GlobalKeymap. Tests in this package
// swap globalKeymap / keymapOnce directly (no public setter).
var (
	globalKeymap     *KeymapRegistry
	globalKeymapOnce sync.Once
)

// GlobalKeymap returns the process-wide registry, creating it on first
// use. Returns the same pointer for the rest of the process lifetime.
func GlobalKeymap() *KeymapRegistry {
	globalKeymapOnce.Do(func() {
		globalKeymap = newKeymapRegistry()
	})
	return globalKeymap
}

// newKeymapRegistry constructs an empty registry. Exported via
// GlobalKeymap; tests can build their own with this for isolation.
func newKeymapRegistry() *KeymapRegistry {
	return &KeymapRegistry{
		entries: make(map[string]KeymapEntry),
	}
}

// Register adds (or replaces) an entry keyed by Action. Action is the
// idempotent identifier so multiple Register calls with the same Action
// don't pile up; the most recent wins, and its Handler replaces the
// previous one. Key + Description are overwritten similarly.
func (r *KeymapRegistry) Register(entry KeymapEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.entries[entry.Action]; !exists {
		r.order = append(r.order, entry.Action)
	}
	r.entries[entry.Action] = entry
}

// Lookup returns the entry for action, or false if not registered.
func (r *KeymapRegistry) Lookup(action string) (KeymapEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.entries[action]
	return e, ok
}

// Entries returns a snapshot of all entries in registration order.
// Used by /help to render the binding table.
func (r *KeymapRegistry) Entries() []KeymapEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]KeymapEntry, 0, len(r.order))
	for _, action := range r.order {
		if e, ok := r.entries[action]; ok {
			out = append(out, e)
		}
	}
	return out
}

// Dispatch invokes the handler for the given action if registered.
// Returns true if a handler fired. Safe to call from any goroutine —
// it takes only a read lock during lookup and releases before invoking
// the handler.
func (r *KeymapRegistry) Dispatch(action string) bool {
	e, ok := r.Lookup(action)
	if !ok || e.Handler == nil {
		return false
	}
	e.Handler()
	return true
}

// MatchAltLetter looks up an action by Alt+<letter> binding. The
// keymap is small so we just scan Entries; the alternative (a second
// map) would double the registration bookkeeping for no measurable win.
func (r *KeymapRegistry) MatchAltLetter(letter string) (KeymapEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	want := "Alt+" + letter
	for _, action := range r.order {
		e := r.entries[action]
		if e.Key == want {
			return e, true
		}
	}
	return KeymapEntry{}, false
}

// KeymapHelpTable renders the registered entries as a fixed-column
// table suitable for embedding in /help output. Column widths are
// computed from the data so the table stays aligned regardless of
// how many entries are registered.
func KeymapHelpTable() string {
	entries := GlobalKeymap().Entries()
	if len(entries) == 0 {
		return "(no keybindings registered)"
	}

	// Compute column widths.
	keyW := len("KEY")
	actionW := len("ACTION")
	descW := len("DESCRIPTION")
	for _, e := range entries {
		if len(e.Key) > keyW {
			keyW = len(e.Key)
		}
		if len(e.Action) > actionW {
			actionW = len(e.Action)
		}
		if len(e.Description) > descW {
			descW = len(e.Description)
		}
	}
	// Sort by Action for stable /help output.
	sorted := make([]KeymapEntry, len(entries))
	copy(sorted, entries)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Action < sorted[j].Action
	})

	header := padRight("KEY", keyW) + "  " + padRight("ACTION", actionW) + "  " + padRight("DESCRIPTION", descW)
	rule := strings.Repeat("─", len(header))
	var b strings.Builder
	b.WriteString(header)
	b.WriteByte('\n')
	b.WriteString(rule)
	b.WriteByte('\n')
	for _, e := range sorted {
		b.WriteString(padRight(e.Key, keyW))
		b.WriteString("  ")
		b.WriteString(padRight(e.Action, actionW))
		b.WriteString("  ")
		b.WriteString(e.Description)
		b.WriteByte('\n')
	}
	return b.String()
}

// KeymapHintRow renders a single-line hint of registered keybindings
// suitable for embedding in a footer or status bar.
// Format: "Alt+T label1 · Alt+V label2 · ..."
// The label is derived from the Action name (the second-to-last
// dot-separated segment, e.g. "footer.breakdown.toggle" → "breakdown"),
// falling back to the Description's first word when Action is empty.
// Labels longer than 30 display columns are truncated with "…".
// Returns empty string when no bindings have descriptions.
func KeymapHintRow() string {
	entries := GlobalKeymap().Entries()
	if len(entries) == 0 {
		return ""
	}

	const maxLabelWidth = 30
	var parts []string
	for _, e := range entries {
		if e.Description == "" {
			continue
		}
		label := extractShortLabel(e)
		if len(label) > maxLabelWidth {
			label = truncateToWidth(label, maxLabelWidth, "…")
		}
		parts = append(parts, e.Key+" "+label)
	}

	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " · ")
}

// extractShortLabel derives a compact label from a keymap entry for use in
// footer hints. It takes the second-to-last dot-separated segment of the
// Action name (e.g., "footer.breakdown.toggle" → "breakdown",
// "output.verbosity.toggle" → "verbosity"). The middle segment is the
// "thing being controlled" — the most semantically useful for a hint.
//
// Edge cases:
//   - Action has only one dot ("footer.tooltip"): returns "footer" (the
//     first segment), since there's no second-to-last.
//   - Action has no dots ("simpleaction"): returns the whole Action.
//   - Action is empty: falls back to the first word of Description.
func extractShortLabel(e KeymapEntry) string {
	if e.Action != "" {
		if lastDot := strings.LastIndex(e.Action, "."); lastDot > 0 {
			// Walk back from lastDot to find the previous dot.
			head := e.Action[:lastDot]
			if prevDot := strings.LastIndex(head, "."); prevDot >= 0 {
				return head[prevDot+1:]
			}
			// Only one dot in Action: return the head segment.
			return head
		}
		// No dot: use the whole Action name.
		return e.Action
	}
	// Fallback: first word of Description.
	if idx := strings.Index(e.Description, " "); idx > 0 {
		return e.Description[:idx]
	}
	return e.Description
}

func padRight(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}
