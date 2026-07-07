package console

import (
	"strings"
	"testing"
)

func TestKeymapHintRow_ReturnsSlashCommandPrompt(t *testing.T) {
	got := KeymapHintRow()
	if !strings.Contains(got, "/help") {
		t.Errorf("KeymapHintRow() = %q, should mention /help", got)
	}
	if got == "" {
		t.Error("KeymapHintRow() should not be empty")
	}
}
