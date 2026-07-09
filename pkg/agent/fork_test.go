package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// TestBreakpoints_ReturnsUserMessages verifies that Breakpoints returns only
// user messages with correct 1-based indices.
func TestBreakpoints_ReturnsUserMessages(t *testing.T) {
	a := newTestAgent(t)
	defer a.Shutdown()

	messages := []api.Message{
		{Role: "user", Content: "first question"},
		{Role: "assistant", Content: "first answer"},
		{Role: "user", Content: "second question"},
		{Role: "assistant", Content: "second answer"},
		{Role: "user", Content: "third question"},
	}
	for _, msg := range messages {
		a.state.AddMessage(msg)
	}

	bps := a.Breakpoints()
	if len(bps) != 3 {
		t.Fatalf("expected 3 breakpoints, got %d", len(bps))
	}
	if bps[0].Index != 1 || bps[0].Content != "first question" {
		t.Errorf("breakpoint 1: Index=%d Content=%q", bps[0].Index, bps[0].Content)
	}
	if bps[1].Index != 2 || bps[1].Content != "second question" {
		t.Errorf("breakpoint 2: Index=%d Content=%q", bps[1].Index, bps[1].Content)
	}
	if bps[2].Index != 3 || bps[2].Content != "third question" {
		t.Errorf("breakpoint 3: Index=%d Content=%q", bps[2].Index, bps[2].Content)
	}
}

// TestBreakpoints_EmptyConversation verifies that Breakpoints returns an
// empty slice when there are no messages.
func TestBreakpoints_EmptyConversation(t *testing.T) {
	a := newTestAgent(t)
	defer a.Shutdown()

	bps := a.Breakpoints()
	if len(bps) != 0 {
		t.Errorf("expected 0 breakpoints for empty conversation, got %d", len(bps))
	}
}

// TestBreakpoints_ContentTruncated verifies that messages longer than 80
// characters are truncated with "..." appended.
func TestBreakpoints_ContentTruncated(t *testing.T) {
	a := newTestAgent(t)
	defer a.Shutdown()

	longContent := strings.Repeat("a", 120)
	a.state.AddMessage(api.Message{Role: "user", Content: longContent})

	bps := a.Breakpoints()
	if len(bps) != 1 {
		t.Fatalf("expected 1 breakpoint, got %d", len(bps))
	}
	if len(bps[0].Content) > 83 { // 80 + "..."
		t.Errorf("content not truncated: len=%d, content=%q", len(bps[0].Content), bps[0].Content)
	}
	if !strings.HasSuffix(bps[0].Content, "...") {
		t.Error("truncated content should end with '...'")
	}
}

// TestForkAtBreakpoint_TruncatesCorrectly verifies that forking at the 2nd
// of 3 user messages preserves the correct messages.
func TestForkAtBreakpoint_TruncatesCorrectly(t *testing.T) {
	isolateStateAndIndexForTest(t)

	a := newTestAgent(t)
	defer a.Shutdown()

	a.SetSessionID("session_fork_truncate_test")
	messages := []api.Message{
		{Role: "user", Content: "Q1"},
		{Role: "assistant", Content: "A1"},
		{Role: "user", Content: "Q2"},
		{Role: "assistant", Content: "A2"},
		{Role: "user", Content: "Q3"},
	}
	for _, msg := range messages {
		a.state.AddMessage(msg)
	}

	newID, err := a.ForkAtBreakpoint(2) // fork at Q2
	if err != nil {
		t.Fatalf("ForkAtBreakpoint returned error: %v", err)
	}
	if newID == "" {
		t.Fatal("ForkAtBreakpoint returned empty new ID")
	}

	msgs := a.GetMessages()
	// Should have: Q1, A1, Q2 (user1, assistant1, user2)
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages after fork, got %d: %+v", len(msgs), msgs)
	}
	if msgs[0].Content != "Q1" {
		t.Errorf("msg[0] = %q, want %q", msgs[0].Content, "Q1")
	}
	if msgs[1].Content != "A1" {
		t.Errorf("msg[1] = %q, want %q", msgs[1].Content, "A1")
	}
	if msgs[2].Content != "Q2" {
		t.Errorf("msg[2] = %q, want %q", msgs[2].Content, "Q2")
	}
}

// TestForkAtBreakpoint_SavesOriginalSession verifies that the original
// session file is preserved on disk after forking.
func TestForkAtBreakpoint_SavesOriginalSession(t *testing.T) {
	sessionsDir := isolateStateAndIndexForTest(t)

	a := newTestAgent(t)
	defer a.Shutdown()

	priorID := "session_fork_preserve_test"
	a.SetSessionID(priorID)
	a.state.AddMessage(api.Message{Role: "user", Content: "original message"})

	_, err := a.ForkAtBreakpoint(1)
	if err != nil {
		t.Fatalf("ForkAtBreakpoint returned error: %v", err)
	}

	// The original session must be loadable.
	loaded, err := a.LoadStateScoped(priorID, sessionsDir)
	if err != nil {
		t.Fatalf("LoadStateScoped(prior ID) failed: %v", err)
	}

	found := false
	for _, m := range loaded.Messages {
		if m.Role == "user" && m.Content == "original message" {
			found = true
			break
		}
	}
	if !found {
		t.Error("original session file did not contain the pre-fork message")
	}
}

// TestForkAtBreakpoint_InvalidIndex verifies that out-of-range and zero
// breakpoint indices return errors.
func TestForkAtBreakpoint_InvalidIndex(t *testing.T) {
	isolateStateAndIndexForTest(t)

	a := newTestAgent(t)
	defer a.Shutdown()

	// Empty conversation: index 1 should fail.
	_, err := a.ForkAtBreakpoint(1)
	if err == nil {
		t.Error("ForkAtBreakpoint(1) on empty conversation should return error")
	}
	if !strings.Contains(err.Error(), "no user messages") {
		t.Errorf("error should mention no user messages, got: %v", err)
	}

	// Add one user message and try index 2.
	a.state.AddMessage(api.Message{Role: "user", Content: "only message"})
	_, err = a.ForkAtBreakpoint(2)
	if err == nil {
		t.Error("ForkAtBreakpoint(2) with one user message should return error")
	}
	if !strings.Contains(err.Error(), "out of range") {
		t.Errorf("error should mention out of range, got: %v", err)
	}
}

// TestForkAtBreakpoint_AssignsNewSessionID verifies that the new session ID
// differs from the old one and follows the expected format.
func TestForkAtBreakpoint_AssignsNewSessionID(t *testing.T) {
	isolateStateAndIndexForTest(t)

	a := newTestAgent(t)
	defer a.Shutdown()

	priorID := "session_fork_new_id"
	a.SetSessionID(priorID)
	a.state.AddMessage(api.Message{Role: "user", Content: "question"})

	newID, err := a.ForkAtBreakpoint(1)
	if err != nil {
		t.Fatalf("ForkAtBreakpoint returned error: %v", err)
	}

	if newID == "" {
		t.Fatal("ForkAtBreakpoint returned empty new ID")
	}
	if newID == priorID {
		t.Errorf("new ID %q should differ from prior ID %q", newID, priorID)
	}
	if !strings.HasPrefix(newID, "session_") {
		t.Errorf("new ID %q should start with 'session_'", newID)
	}
	if got := a.GetSessionID(); got != newID {
		t.Errorf("GetSessionID() = %q, want %q", got, newID)
	}
}

// TestForkAtBreakpoint_SavesOriginalFile verifies that the prior session
// file physically exists on disk under the scoped state directory.
func TestForkAtBreakpoint_SavesOriginalFile(t *testing.T) {
	sessionsDir := isolateStateAndIndexForTest(t)

	a := newTestAgent(t)
	defer a.Shutdown()

	priorID := "session_fork_file_check"
	a.SetSessionID(priorID)
	a.state.AddMessage(api.Message{Role: "user", Content: "keep this"})

	_, err := a.ForkAtBreakpoint(1)
	if err != nil {
		t.Fatalf("ForkAtBreakpoint returned error: %v", err)
	}

	// Walk the sessions dir to confirm a file for our prior ID exists.
	found := false
	_ = filepath.WalkDir(sessionsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() && strings.Contains(d.Name(), priorID) {
			found = true
		}
		return nil
	})
	if !found {
		t.Errorf("no session file found under %s for prior ID %q", sessionsDir, priorID)
	}
}
