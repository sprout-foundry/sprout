package console

import (
	"strings"
	"testing"
)

func TestKeymapHintRow_FollowsComposerMode(t *testing.T) {
	cases := map[ComposerMode][]string{
		ComposerIdle:  {"Enter send", "Alt+Enter newline", "/ commands", "? shortcuts"},
		ComposerSteer: {"Enter steer", "Tab queue instead", "^C interrupt"},
		ComposerQueue: {"Enter queue", "Tab steer instead", "^C interrupt"},
	}
	for mode, wants := range cases {
		got := KeymapHintRow(mode)
		for _, want := range wants {
			if !strings.Contains(got, want) {
				t.Errorf("KeymapHintRow(%d) = %q, want contains %q", mode, got, want)
			}
		}
	}
	if strings.Contains(KeymapHintRow(ComposerIdle), "steer") {
		t.Errorf("idle hint should not offer steering: %q", KeymapHintRow(ComposerIdle))
	}
}

func TestKeymapHintRow_Shape(t *testing.T) {
	for _, mode := range []ComposerMode{ComposerIdle, ComposerSteer, ComposerQueue} {
		got := KeymapHintRow(mode)
		if strings.Contains(got, "\n") || !strings.Contains(got, " · ") {
			t.Errorf("KeymapHintRow(%d) = %q, want one ' · '-separated line", mode, got)
		}
		for _, excluded := range []string{"/settings", "Alt+T", "Alt+V"} {
			if strings.Contains(got, excluded) {
				t.Errorf("KeymapHintRow(%d) = %q, should NOT contain %q", mode, got, excluded)
			}
		}
	}
}
