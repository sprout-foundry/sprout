package changes

import (
	"testing"
)

// The per-turn change window (SP-149 §149a): MarkTurnStart opens it,
// TurnChangedPaths reads it without consuming it.

// trackWrite and trackEdit record changes and fail the test if the
// (enabled) tracker refuses one — the tracker returns an error only
// when disabled, which these tests never exercise.
func trackWrite(t *testing.T, ct *ChangeTracker, path, original, newContent string, existed bool) {
	t.Helper()
	if err := ct.TrackFileWriteState(path, original, newContent, existed); err != nil {
		t.Fatalf("TrackFileWriteState(%q): %v", path, err)
	}
}

func trackEdit(t *testing.T, ct *ChangeTracker, path, original, newContent string) {
	t.Helper()
	if err := ct.TrackFileEdit(path, original, newContent); err != nil {
		t.Fatalf("TrackFileEdit(%q): %v", path, err)
	}
}

func TestTurnChangedPaths_MarkThenRecordListsPaths(t *testing.T) {
	ct := NewTrackerForTesting("rev-turn")

	// An unmarked tracker's window opens at zero changes (production
	// callers mark in EnableChangeTracking, right after creation), so a
	// pre-mark change is still in the window.
	trackWrite(t, ct, "/ws/pre.go", "old", "new", true)
	if got := ct.TurnChangedPaths(); len(got) != 1 || got[0] != "/ws/pre.go" {
		t.Fatalf("TurnChangedPaths before MarkTurnStart = %v, want [/ws/pre.go] (unmarked window spans from zero)", got)
	}

	ct.MarkTurnStart()
	trackWrite(t, ct, "/ws/app.go", "old", "new", true)
	trackEdit(t, ct, "/ws/util.go", "old", "new")

	// Two changes to the same path collapse to one entry.
	trackWrite(t, ct, "/ws/app.go", "new", "newer", true)

	got := ct.TurnChangedPaths()
	want := []string{"/ws/app.go", "/ws/util.go"}
	if len(got) != len(want) {
		t.Fatalf("TurnChangedPaths = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("TurnChangedPaths[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestTurnChangedPaths_SecondMarkResetsWindow(t *testing.T) {
	ct := NewTrackerForTesting("rev-turn")
	ct.MarkTurnStart()
	trackWrite(t, ct, "/ws/app.go", "old", "new", true)

	// A second turn window opens: the previous turn's change is no longer
	// in the window.
	ct.MarkTurnStart()
	if got := ct.TurnChangedPaths(); got != nil {
		t.Fatalf("TurnChangedPaths after second MarkTurnStart = %v, want nil", got)
	}

	trackEdit(t, ct, "/ws/util.go", "old", "new")
	got := ct.TurnChangedPaths()
	if len(got) != 1 || got[0] != "/ws/util.go" {
		t.Fatalf("TurnChangedPaths = %v, want [/ws/util.go]", got)
	}
}

func TestTurnChangedPaths_NilAndDisabledTracker(t *testing.T) {
	var ct *ChangeTracker
	if got := ct.TurnChangedPaths(); got != nil {
		t.Fatalf("nil tracker TurnChangedPaths = %v, want nil", got)
	}

	bare := NewTrackerForTesting("rev-off")
	bare.Disable()
	if got := bare.TurnChangedPaths(); got != nil {
		t.Fatalf("disabled tracker TurnChangedPaths = %v, want nil", got)
	}

	// MarkTurnStart on a nil tracker is a no-op, not a panic.
	ct.MarkTurnStart()
}

func TestTurnChangedPaths_IsIndependentOfCheckpointCapture(t *testing.T) {
	ct := NewTrackerForTesting("rev-mixed")
	ct.MarkTurnStart()
	trackWrite(t, ct, "/ws/app.go", "old", "new", true)

	// The checkpoint capture consumes the window it tracks; the turn
	// window must survive it (and vice versa, the turn read does not
	// move the checkpoint counter).
	ct.CollectFileChangesForCheckpoint()
	if got := ct.TurnChangedPaths(); len(got) != 1 || got[0] != "/ws/app.go" {
		t.Fatalf("TurnChangedPaths after checkpoint capture = %v, want [/ws/app.go] (non-destructive)", got)
	}
}
