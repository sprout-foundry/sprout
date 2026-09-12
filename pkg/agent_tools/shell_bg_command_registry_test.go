package tools

import (
	"strings"
	"testing"
)

// Pins the background-session command registry: the completion-time
// mutation diff must see the ORIGINAL command so the destructive-command
// classifier (shellIsDestructive in pkg/agent) can classify background
// `git reset --hard` runs. Before the registry, observers only had the
// synthetic "background session <id>" label.
func TestBackgroundCommandRegistry(t *testing.T) {
	rememberBackgroundCommand("bg-reg-1", "git reset --hard HEAD~1")
	rememberBackgroundCommand("bg-reg-2", "make build")

	if got := backgroundCommandFor("bg-reg-1"); got != "git reset --hard HEAD~1" {
		t.Errorf("registry lookup: want original command, got %q", got)
	}
	// Consumed once: a second observer (check_background racing the
	// wakeup watcher) must not resurrect the entry.
	if got := backgroundCommandFor("bg-reg-1"); got != "" {
		t.Errorf("registry entry should be consumed once, got %q", got)
	}
	if got := backgroundCommandFor("bg-reg-2"); got != "make build" {
		t.Errorf("second entry clobbered: got %q", got)
	}
	// Unknown sessions fall back to empty (caller synthesizes the label).
	if got := backgroundCommandFor("bg-never-started"); got != "" {
		t.Errorf("unknown session: want empty, got %q", got)
	}
	// Empty inputs are ignored, not stored as junk entries.
	rememberBackgroundCommand("", "cmd")
	rememberBackgroundCommand("bg-reg-3", "")
	if got := backgroundCommandFor("bg-reg-3"); got != "" {
		t.Errorf("empty command must not be stored, got %q", got)
	}
}

func TestBackgroundCommandRegistryBounded(t *testing.T) {
	// Fill past the cap with distinct entries; the registry must not
	// grow beyond bgCommandRegistryMax and lookups must keep working.
	for i := 0; i < bgCommandRegistryMax+50; i++ {
		rememberBackgroundCommand("bg-bulk-"+strings.Repeat("a", i%8)+"-"+string(rune('A'+i%26))+string(rune('a'+(i/26)%26)), "echo bulk")
	}
	bgCommandRegistry.mu.RLock()
	size := len(bgCommandRegistry.commands)
	bgCommandRegistry.mu.RUnlock()
	if size > bgCommandRegistryMax {
		t.Errorf("registry exceeded cap: %d > %d", size, bgCommandRegistryMax)
	}
}

func TestTrackBackgroundMutationResolvesOriginalCommand(t *testing.T) {
	var trackedCommand string
	env := ToolEnv{
		ToolFuncs: &ToolFuncSet{
			TrackShellCommand: func(command string) error {
				trackedCommand = command
				return nil
			},
		},
	}

	// Known session: the tracker receives the original command.
	rememberBackgroundCommand("bg-track-1", "git checkout .")
	trackBackgroundMutation(env, "bg-track-1", "completed")
	if trackedCommand != "git checkout ." {
		t.Errorf("tracker should receive original command, got %q", trackedCommand)
	}

	// Unknown session: synthetic label fallback preserved.
	trackBackgroundMutation(env, "bg-track-unknown", "stopped")
	if !strings.Contains(trackedCommand, "bg-track-unknown") || !strings.Contains(trackedCommand, "stopped") {
		t.Errorf("fallback label malformed: %q", trackedCommand)
	}
}
