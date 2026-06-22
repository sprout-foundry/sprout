package agent

import (
	"os"
	"path/filepath"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func addMessages(a *Agent, msgs ...string) {
	for _, c := range msgs {
		a.AddMessage(api.Message{Role: "user", Content: c})
	}
}

func addAssistantMessages(a *Agent, msgs ...string) {
	for _, c := range msgs {
		a.AddMessage(api.Message{Role: "assistant", Content: c})
	}
}

// makeFileRewindAgent creates a minimal agent with a pre-configured tracker
// ready for rewind file-revert tests. The tracker already has a tracked change
// for `path` with the given original/new content.
func makeFileRewindAgent(t *testing.T, dir, path, original, newContent, modifiedOnDisk string) *Agent {
	t.Helper()

	tracker := &ChangeTracker{
		enabled: true,
		changes: []TrackedFileChange{
			{
				FilePath:     path,
				OriginalCode: original,
				NewCode:      newContent,
				Operation:    "write",
				ToolCall:     "WriteFile",
			},
		},
	}
	a := &Agent{changeTracker: tracker, state: NewAgentStateManager(false)}
	tracker.agent = a
	a.SetWorkspaceRoot(dir)
	return a
}

// ---------------------------------------------------------------------------
// TestRewind_InvalidTurnIndex
// ---------------------------------------------------------------------------

func TestRewind_InvalidTurnIndex(t *testing.T) {
	a := newTestAgent(t)
	defer a.Shutdown()

	// Add messages and a checkpoint so there's something to rewind.
	addMessages(a, "msg1")
	a.RecordTurnCheckpoint(0, 0)

	t.Run("NegativeIndex", func(t *testing.T) {
		_, err := a.Rewind(RewindOptions{ToTurnIndex: -1})
		if err == nil {
			t.Fatal("expected error for negative turn index")
		}
	})

	t.Run("IndexBeyondCheckpointCount", func(t *testing.T) {
		// One checkpoint → valid range is [0, 0]. Index 1 is out of range.
		_, err := a.Rewind(RewindOptions{ToTurnIndex: 1})
		if err == nil {
			t.Fatal("expected error for turn index beyond checkpoint count")
		}
	})
}

// ---------------------------------------------------------------------------
// TestRewind_NoCheckpoints
// ---------------------------------------------------------------------------

func TestRewind_NoCheckpoints(t *testing.T) {
	a := newTestAgent(t)
	defer a.Shutdown()

	// No checkpoints recorded — any index is invalid.
	_, err := a.Rewind(RewindOptions{ToTurnIndex: 0})
	if err == nil {
		t.Fatal("expected error when rewinding with no checkpoints")
	}
}

// ---------------------------------------------------------------------------
// TestRewind_TruncateMessages
// ---------------------------------------------------------------------------

func TestRewind_TruncateMessages(t *testing.T) {
	a := newTestAgent(t)
	defer a.Shutdown()

	// Clear and rebuild with 3 turns
	a.SetMessages(nil)
	a.clearTurnCheckpoints()

	// Turn 0: messages 0-3
	addMessages(a, "turn0-user", "turn0-assistant", "turn0-extra")
	addAssistantMessages(a, "turn0-tool-result")
	a.RecordTurnCheckpoint(0, 3)

	// Turn 1: messages 4-7
	addMessages(a, "turn1-user", "turn1-extra")
	addAssistantMessages(a, "turn1-assistant", "turn1-tool")
	a.RecordTurnCheckpoint(4, 7)

	// Turn 2: messages 8-10
	addMessages(a, "turn2-user", "turn2-extra")
	addAssistantMessages(a, "turn2-assistant")
	a.RecordTurnCheckpoint(8, 10)

	if len(a.GetMessages()) != 11 {
		t.Fatalf("expected 11 messages, got %d", len(a.GetMessages()))
	}

	checkpoints := a.copyTurnCheckpoints()
	if len(checkpoints) != 3 {
		t.Fatalf("expected 3 checkpoints, got %d", len(checkpoints))
	}

	// Rewind to turn 1 (index 1) — truncates at checkpoints[1].StartIndex = 4
	res, err := a.Rewind(RewindOptions{ToTurnIndex: 1, RevertFiles: false})
	if err != nil {
		t.Fatalf("rewind error: %v", err)
	}

	msgs := a.GetMessages()
	if len(msgs) != 4 {
		t.Errorf("after rewind to turn 1: expected 4 messages, got %d", len(msgs))
	}

	// Verify the remaining messages are the original first 4.
	if len(msgs) > 0 && msgs[0].Content != "turn0-user" {
		t.Errorf("msg[0] = %q, want %q", msgs[0].Content, "turn0-user")
	}
	if len(msgs) > 1 && msgs[1].Content != "turn0-assistant" {
		t.Errorf("msg[1] = %q, want %q", msgs[1].Content, "turn0-assistant")
	}

	// 11 total - 4 remaining = 7 removed
	if res.MessagesRemoved != 7 {
		t.Errorf("MessagesRemoved = %d, want 7", res.MessagesRemoved)
	}
}

// ---------------------------------------------------------------------------
// TestRewind_DropOrphanedCheckpoints
// ---------------------------------------------------------------------------

func TestRewind_DropOrphanedCheckpoints(t *testing.T) {
	a := newTestAgent(t)
	defer a.Shutdown()

	a.SetMessages(nil)
	a.clearTurnCheckpoints()

	// Build 4 turns (checkpoints at 0, 2, 4, 6)
	for i := 0; i < 4; i++ {
		addMessages(a, "t"+string(rune('0'+i))+"-a", "t"+string(rune('0'+i))+"-b")
		a.RecordTurnCheckpoint(i*2, i*2+1)
	}

	if len(a.copyTurnCheckpoints()) != 4 {
		t.Fatalf("expected 4 checkpoints, got %d", len(a.copyTurnCheckpoints()))
	}

	// Rewind to turn index 1 (StartIndex=2).
	res, err := a.Rewind(RewindOptions{ToTurnIndex: 1, RevertFiles: false})
	if err != nil {
		t.Fatalf("rewind error: %v", err)
	}

	// The filter is StartIndex < startIndex (=2).
	// Turn 0: StartIndex=0 (< 2) → kept
	// Turn 1: StartIndex=2 (not < 2) → dropped
	// Turns 2 and 3: StartIndex 4,6 → dropped
	remaining := a.copyTurnCheckpoints()
	if len(remaining) != 1 {
		t.Errorf("remaining checkpoints = %d, want 1", len(remaining))
	}
	if len(remaining) > 0 && remaining[0].StartIndex != 0 {
		t.Errorf("remaining checkpoint StartIndex = %d, want 0", remaining[0].StartIndex)
	}

	if res.CheckpointsDropped != 3 {
		t.Errorf("CheckpointsDropped = %d, want 3", res.CheckpointsDropped)
	}
}

// ---------------------------------------------------------------------------
// TestRewind_Snapshot
// ---------------------------------------------------------------------------

func TestRewind_Snapshot(t *testing.T) {
	a := newTestAgent(t)
	defer a.Shutdown()

	a.SetMessages(nil)
	a.clearTurnCheckpoints()

	// Add messages and checkpoints
	addMessages(a, "before-1", "before-2")
	a.RecordTurnCheckpoint(0, 1)
	addMessages(a, "after-1")
	a.RecordTurnCheckpoint(2, 2)

	// Pre-rewind state
	beforeMsgs := a.GetMessages()
	beforeCPs := a.copyTurnCheckpoints()

	// Rewind to turn 0
	_, err := a.Rewind(RewindOptions{ToTurnIndex: 0, RevertFiles: false})
	if err != nil {
		t.Fatalf("rewind error: %v", err)
	}

	// Verify snapshot was captured
	if len(lastRewindSnapshot.messages) != len(beforeMsgs) {
		t.Errorf("snapshot message count = %d, want %d",
			len(lastRewindSnapshot.messages), len(beforeMsgs))
	}
	if len(lastRewindSnapshot.checkpoints) != len(beforeCPs) {
		t.Errorf("snapshot checkpoint count = %d, want %d",
			len(lastRewindSnapshot.checkpoints), len(beforeCPs))
	}

	// Verify snapshot content matches pre-rewind state
	if len(lastRewindSnapshot.messages) > 0 {
		if lastRewindSnapshot.messages[0].Content != "before-1" {
			t.Errorf("snapshot msg[0] = %q, want %q",
				lastRewindSnapshot.messages[0].Content, "before-1")
		}
	}

	// Clear the snapshot so other tests aren't affected
	lastRewindSnapshot.messages = nil
	lastRewindSnapshot.checkpoints = nil
}

// ---------------------------------------------------------------------------
// TestRewind_WithoutFileRevert
// ---------------------------------------------------------------------------

func TestRewind_WithoutFileRevert(t *testing.T) {
	a := newTestAgent(t)
	defer a.Shutdown()

	dir := t.TempDir()
	a.SetWorkspaceRoot(dir)

	// Create and track a file
	path := filepath.Join(dir, "tracked.txt")
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}

	a.EnableChangeTracking("rewind no-revert test")
	_ = a.TrackFileWrite(path, "modified")
	if err := os.WriteFile(path, []byte("modified"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Set up checkpoints with file changes
	a.SetMessages(nil)
	a.clearTurnCheckpoints()

	addMessages(a, "turn0")
	a.ReplaceTurnCheckpoints([]TurnCheckpoint{
		{StartIndex: 0, EndIndex: 0, Summary: "turn0", FileChanges: []CheckpointFileChange{
			{Path: path, Op: "M"},
		}},
	})
	addMessages(a, "turn1")
	a.RecordTurnCheckpoint(1, 1)

	// Rewind to turn 0 with RevertFiles: false
	res, err := a.Rewind(RewindOptions{ToTurnIndex: 0, RevertFiles: false})
	if err != nil {
		t.Fatalf("rewind error: %v", err)
	}

	// Messages and checkpoints should still be truncated
	if len(a.GetMessages()) != 0 {
		t.Errorf("expected 0 messages after rewind to turn 0, got %d", len(a.GetMessages()))
	}

	// But the file should NOT be reverted
	content, _ := os.ReadFile(path)
	if string(content) != "modified" {
		t.Errorf("file should NOT be reverted when RevertFiles=false; got %q", content)
	}

	// FilesReverted and FilesSkipped should both be empty
	if len(res.FilesReverted) != 0 {
		t.Errorf("FilesReverted = %v, want nil/empty", res.FilesReverted)
	}
	if len(res.FilesSkipped) != 0 {
		t.Errorf("FilesSkipped = %v, want nil/empty", res.FilesSkipped)
	}
}

// ---------------------------------------------------------------------------
// TestRewind_FileRevertDefault
// ---------------------------------------------------------------------------

func TestRewind_FileRevertDefault(t *testing.T) {
	dir := t.TempDir()
	original := "ORIGINAL_CONTENT"
	modified := "MODIFIED_CONTENT"
	path := filepath.Join(dir, "revert.txt")

	// Create file with original content
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create agent with pre-configured tracker
	a := makeFileRewindAgent(t, dir, path, original, modified, modified)

	// Write the modified content to disk
	if err := os.WriteFile(path, []byte(modified), 0o644); err != nil {
		t.Fatal(err)
	}

	// Set up checkpoints — turn 0 has no file changes, turn 1 has the change
	a.SetMessages(nil)
	a.clearTurnCheckpoints()

	addMessages(a, "turn0")
	a.RecordTurnCheckpoint(0, 0)

	addMessages(a, "turn1")
	a.ReplaceTurnCheckpoints([]TurnCheckpoint{
		{StartIndex: 0, EndIndex: 0, Summary: "turn0"},
		{StartIndex: 1, EndIndex: 1, Summary: "turn1", FileChanges: []CheckpointFileChange{
			{Path: path, Op: "M"},
		}},
	})

	// Rewind to turn 0 with RevertFiles: true
	res, err := a.Rewind(RewindOptions{ToTurnIndex: 0, RevertFiles: true})
	if err != nil {
		t.Fatalf("rewind error: %v", err)
	}

	// The file should be reverted to original content
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(content) != original {
		t.Errorf("file was not reverted; got %q, want %q", content, original)
	}

	if len(res.FilesReverted) != 1 {
		t.Errorf("FilesReverted = %v, want 1 entry", res.FilesReverted)
	}
	if len(res.FilesSkipped) != 0 {
		t.Errorf("FilesSkipped = %v, want empty", res.FilesSkipped)
	}
}

// ---------------------------------------------------------------------------
// TestRewind_FilesSkippedWhenModified
// ---------------------------------------------------------------------------

func TestRewind_FilesSkippedWhenModified(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "externally_modified.txt")
	original := "ORIGINAL"
	agentModified := "AGENT_WROTE_THIS"
	externalModified := "EXTERNAL_MODIFIED_THIS"

	// Create file with original content
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create agent with pre-configured tracker
	a := makeFileRewindAgent(t, dir, path, original, agentModified, agentModified)

	// Write the agent's modified content, then simulate external modification
	if err := os.WriteFile(path, []byte(agentModified), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(externalModified), 0o644); err != nil {
		t.Fatal(err)
	}

	// Set up checkpoints — turn 0 has no file changes, turn 1 has the change
	a.SetMessages(nil)
	a.clearTurnCheckpoints()

	addMessages(a, "turn0")
	a.RecordTurnCheckpoint(0, 0)

	addMessages(a, "turn1")
	a.ReplaceTurnCheckpoints([]TurnCheckpoint{
		{StartIndex: 0, EndIndex: 0, Summary: "turn0"},
		{StartIndex: 1, EndIndex: 1, Summary: "turn1", FileChanges: []CheckpointFileChange{
			{Path: path, Op: "M"},
		}},
	})

	// Rewind to turn 0 — the file was modified externally, so it should be skipped
	res, err := a.Rewind(RewindOptions{ToTurnIndex: 0, RevertFiles: true})
	if err != nil {
		t.Fatalf("rewind error: %v", err)
	}

	// The file should remain with external content (not reverted)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(content) != externalModified {
		t.Errorf("file should NOT have been reverted; got %q, want %q", content, externalModified)
	}

	if len(res.FilesSkipped) != 1 {
		t.Errorf("FilesSkipped = %v, want 1 entry for externally modified file", res.FilesSkipped)
	}
	if len(res.FilesReverted) != 0 {
		t.Errorf("FilesReverted = %v, want empty", res.FilesReverted)
	}
}

// ---------------------------------------------------------------------------
// TestRewind_TurnsDiscardedCount
// ---------------------------------------------------------------------------

func TestRewind_TurnsDiscardedCount(t *testing.T) {
	a := newTestAgent(t)
	defer a.Shutdown()

	a.SetMessages(nil)
	a.clearTurnCheckpoints()

	// Create 5 turns
	for i := 0; i < 5; i++ {
		addMessages(a, "t"+string(rune('0'+i)))
		a.RecordTurnCheckpoint(i, i)
	}

	// Rewind to turn 2 (index 2) — discards turns 3 and 4
	res, err := a.Rewind(RewindOptions{ToTurnIndex: 2, RevertFiles: false})
	if err != nil {
		t.Fatalf("rewind error: %v", err)
	}

	// discardedCheckpoints = checkpoints[3:] = [turn3, turn4] → 2 discarded
	if res.TurnsDiscarded != 2 {
		t.Errorf("TurnsDiscarded = %d, want 2", res.TurnsDiscarded)
	}

	// After first rewind: checkpoints with StartIndex < 2 remain
	// Turn 0: StartIndex=0 → kept
	// Turn 1: StartIndex=1 → kept
	// Turns 2-4: StartIndex 2,3,4 → dropped
	remaining := a.copyTurnCheckpoints()
	if len(remaining) != 2 {
		t.Errorf("after first rewind: checkpoints = %d, want 2", len(remaining))
	}

	// Rewind to turn 0 — discards turn 1
	res2, err := a.Rewind(RewindOptions{ToTurnIndex: 0, RevertFiles: false})
	if err != nil {
		t.Fatalf("second rewind error: %v", err)
	}
	if res2.TurnsDiscarded != 1 {
		t.Errorf("second rewind TurnsDiscarded = %d, want 1", res2.TurnsDiscarded)
	}
}

// ---------------------------------------------------------------------------
// TestRewind_MessagesRemovedCount
// ---------------------------------------------------------------------------

func TestRewind_MessagesRemovedCount(t *testing.T) {
	a := newTestAgent(t)
	defer a.Shutdown()

	a.SetMessages(nil)
	a.clearTurnCheckpoints()

	// 3 turns: turn0(0-2), turn1(3-5), turn2(6-8) → 9 messages
	for i := 0; i < 3; i++ {
		base := i * 3
		addMessages(a, "m"+string(rune('0'+base)), "m"+string(rune('0'+base+1)), "m"+string(rune('0'+base+2)))
		a.RecordTurnCheckpoint(base, base+2)
	}

	if len(a.GetMessages()) != 9 {
		t.Fatalf("expected 9 messages, got %d", len(a.GetMessages()))
	}

	// Rewind to turn 1 (StartIndex=3). Messages [3:9] = 6 removed.
	res, err := a.Rewind(RewindOptions{ToTurnIndex: 1, RevertFiles: false})
	if err != nil {
		t.Fatalf("rewind error: %v", err)
	}

	if res.MessagesRemoved != 6 {
		t.Errorf("MessagesRemoved = %d, want 6", res.MessagesRemoved)
	}

	msgs := a.GetMessages()
	if len(msgs) != 3 {
		t.Errorf("remaining messages = %d, want 3", len(msgs))
	}
}

// ---------------------------------------------------------------------------
// TestRewind_RewindToLastTurn
// ---------------------------------------------------------------------------

func TestRewind_RewindToLastTurn(t *testing.T) {
	a := newTestAgent(t)
	defer a.Shutdown()

	a.SetMessages(nil)
	a.clearTurnCheckpoints()

	// 2 turns
	addMessages(a, "t0a", "t0b")
	a.RecordTurnCheckpoint(0, 1)
	addMessages(a, "t1a", "t1b")
	a.RecordTurnCheckpoint(2, 3)

	// Rewind to last turn (index 1) — discards nothing
	res, err := a.Rewind(RewindOptions{ToTurnIndex: 1, RevertFiles: false})
	if err != nil {
		t.Fatalf("rewind error: %v", err)
	}

	if res.TurnsDiscarded != 0 {
		t.Errorf("TurnsDiscarded = %d, want 0 when rewinding to last turn", res.TurnsDiscarded)
	}

	// Messages truncated at checkpoints[1].StartIndex = 2
	if res.MessagesRemoved != 2 {
		t.Errorf("MessagesRemoved = %d, want 2", res.MessagesRemoved)
	}
}

// ---------------------------------------------------------------------------
// TestRewind_WithNilChangeTracker
// ---------------------------------------------------------------------------

func TestRewind_WithNilChangeTracker(t *testing.T) {
	a := newTestAgent(t)
	defer a.Shutdown()

	// Agent without change tracking enabled (no tracker)
	a.SetMessages(nil)
	a.clearTurnCheckpoints()

	// Create a temp file so it exists on disk
	dir := t.TempDir()
	path := filepath.Join(dir, "some_file.txt")
	if err := os.WriteFile(path, []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}

	addMessages(a, "turn0")
	a.RecordTurnCheckpoint(0, 0)
	addMessages(a, "turn1")
	// Turn 1 has file changes — these will be the discarded turn
	a.ReplaceTurnCheckpoints([]TurnCheckpoint{
		{StartIndex: 0, EndIndex: 0, Summary: "turn0"},
		{StartIndex: 1, EndIndex: 1, Summary: "turn1", FileChanges: []CheckpointFileChange{
			{Path: path, Op: "M"},
		}},
	})

	// Rewind with RevertFiles=true but no tracker — should still work,
	// just skip file reverts gracefully
	res, err := a.Rewind(RewindOptions{ToTurnIndex: 0, RevertFiles: true})
	if err != nil {
		t.Fatalf("rewind should not fail when tracker is nil: %v", err)
	}

	// Messages should still be truncated
	if len(a.GetMessages()) != 0 {
		t.Errorf("expected 0 messages, got %d", len(a.GetMessages()))
	}

	// Files should be skipped (no tracker to recover from)
	if len(res.FilesSkipped) != 1 {
		t.Errorf("FilesSkipped = %v, want 1 entry when tracker is nil", res.FilesSkipped)
	}
}

// ---------------------------------------------------------------------------
// TestRewind_CheckpointFileChangesDeduplication
// ---------------------------------------------------------------------------

func TestRewind_CheckpointFileChangesDeduplication(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dedup.txt")
	original := "v0"
	v1 := "v1"

	// Create file with original content
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create agent with pre-configured tracker
	a := makeFileRewindAgent(t, dir, path, original, v1, v1)

	// Write v1 to disk
	if err := os.WriteFile(path, []byte(v1), 0o644); err != nil {
		t.Fatal(err)
	}

	// Two checkpoints that both reference the same file in discarded turns
	a.SetMessages(nil)
	a.clearTurnCheckpoints()

	addMessages(a, "turn0")
	a.RecordTurnCheckpoint(0, 0)

	addMessages(a, "turn1")
	a.ReplaceTurnCheckpoints([]TurnCheckpoint{
		{StartIndex: 0, EndIndex: 0, Summary: "turn0"},
		{StartIndex: 1, EndIndex: 1, Summary: "turn1", FileChanges: []CheckpointFileChange{
			{Path: path, Op: "M"},
		}},
		{StartIndex: 2, EndIndex: 2, Summary: "turn2", FileChanges: []CheckpointFileChange{
			{Path: path, Op: "M"},
		}},
	})

	// Rewind to turn 0 — both turn1 and turn2 reference the same file.
	// Deduplication should ensure it's only attempted once.
	res, err := a.Rewind(RewindOptions{ToTurnIndex: 0, RevertFiles: true})
	if err != nil {
		t.Fatalf("rewind error: %v", err)
	}

	// File should be reverted to original
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(content) != original {
		t.Errorf("file content = %q, want %q", content, original)
	}

	// Should be in FilesReverted exactly once (deduped)
	if len(res.FilesReverted) != 1 {
		t.Errorf("FilesReverted count = %d, want 1 (deduped)", len(res.FilesReverted))
	}
}
