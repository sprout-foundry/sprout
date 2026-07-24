package console

import (
	"strings"
	"testing"
)

func TestKeymapHintRow_ContainsBuiltInEssentials(t *testing.T) {
	got := KeymapHintRow()

	for _, want := range []string{
		"^C interrupt",
		"Enter steer",
		"/ commands",
		"Tab autocomplete",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("KeymapHintRow() = %q, want contains %q", got, want)
		}
	}
}

func TestKeymapHintRow_ExcludesNonEssentials(t *testing.T) {
	got := KeymapHintRow()

	for _, excluded := range []string{
		"/settings",
		"Alt+T",
		"Alt+V",
	} {
		if strings.Contains(got, excluded) {
			t.Errorf("KeymapHintRow() = %q, should NOT contain %q", got, excluded)
		}
	}
}

func TestKeymapHintRow_IsSingleLine(t *testing.T) {
	got := KeymapHintRow()

	if strings.Contains(got, "\n") {
		t.Errorf("KeymapHintRow() = %q, must be single-line (no newlines)", got)
	}
}

func TestKeymapHintRow_UsesSeparator(t *testing.T) {
	got := KeymapHintRow()

	if !strings.Contains(got, " · ") {
		t.Errorf("KeymapHintRow() = %q, want ' · ' separator between entries", got)
	}
}
