package console

import (
	"strings"
	"testing"
)

func TestStyleComposerRow_LabelAccentAndPlainText(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "1")
	for _, mode := range []ComposerMode{ComposerIdle, ComposerSteer, ComposerQueue} {
		prefix := ComposerPrefix(mode)
		rendered := steerRowTextWithCursor(prefix+"fix the tests", 60, true, -1)
		got := styleComposerRow(rendered, true, false, mode, 60)
		if !strings.HasPrefix(got, composerAccent(mode)+prefix+ColorReset) {
			t.Errorf("mode %d: label should carry the mode accent, got %q", mode, got)
		}
		if !strings.Contains(got, ColorReset+"fix the tests") {
			t.Errorf("mode %d: typed text should be in the normal color, got %q", mode, got)
		}
	}
}

func TestStyleComposerRow_PlaceholderWhenEmpty(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "1")
	prefix := ComposerPrefix(ComposerQueue)
	rendered := steerRowTextWithCursor(prefix, 60, true, -1)
	got := styleComposerRow(rendered, true, true, ComposerQueue, 60)
	if !strings.Contains(got, ColorDim+composerPlaceholder(ComposerQueue)) {
		t.Fatalf("empty box should show the dim placeholder, got %q", got)
	}
	if w := visibleLen(got); w != 60 {
		t.Fatalf("placeholder row should stay exactly the width, got %d", w)
	}
}

func TestStyleComposerRow_NoColorStillShowsPlaceholder(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	prefix := ComposerPrefix(ComposerIdle)
	rendered := steerRowTextWithCursor(prefix, 40, true, -1)
	got := styleComposerRow(rendered, true, true, ComposerIdle, 40)
	if strings.Contains(got, "\x1b[") || !strings.Contains(got, "Message sprout") {
		t.Fatalf("NO_COLOR box should show a plain placeholder, got %q", got)
	}
}

func TestComposerPrefixesAreDistinct(t *testing.T) {
	seen := map[string]bool{}
	for _, mode := range []ComposerMode{ComposerIdle, ComposerSteer, ComposerQueue} {
		p := ComposerPrefix(mode)
		if seen[p] || !strings.HasSuffix(p, "› ") {
			t.Fatalf("prefix %q should be unique and end in the shared '› ' marker", p)
		}
		seen[p] = true
	}
}
