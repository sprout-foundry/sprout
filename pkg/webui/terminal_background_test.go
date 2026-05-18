package webui

import (
	"context"
	"strings"
	"testing"
	"time"
)

// --- ExecuteCommandInBackground tests ---

func TestExecuteCommandInBackground_Success(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PTY")
	}

	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	sessionID, err := tm.ExecuteCommandInBackground(context.Background(), "chat-1", "echo hello")
	if err != nil {
		t.Fatalf("ExecuteCommandInBackground failed: %v", err)
	}

	// Verify the session exists and is marked as background.
	session, exists := tm.GetSession(sessionID)
	if !exists {
		t.Fatal("background session should exist after ExecuteCommandInBackground")
	}

	session.mutex.RLock()
	isBackground := session.IsBackground
	hidden := session.Hidden
	owner := session.Owner
	chatID := session.ChatID
	active := session.Active
	session.mutex.RUnlock()

	if !isBackground {
		t.Error("session should have IsBackground=true")
	}
	if !hidden {
		t.Error("background session should be hidden")
	}
	if owner != "agent" {
		t.Errorf("expected owner 'agent', got %q", owner)
	}
	if chatID != "chat-1" {
		t.Errorf("expected chatID 'chat-1', got %q", chatID)
	}
	if !active {
		t.Error("background session should be active")
	}

	// Verify session ID starts with "bg-"
	if !strings.HasPrefix(sessionID, "bg-") {
		t.Errorf("expected session ID to start with 'bg-', got %q", sessionID)
	}

	tm.CloseSession(sessionID)
}

func TestExecuteCommandInBackground_EmptyCommand(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	_, err := tm.ExecuteCommandInBackground(context.Background(), "chat-1", "")
	if err == nil {
		t.Fatal("expected error for empty command")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("expected error message to mention 'empty', got %q", err.Error())
	}
}

func TestExecuteCommandInBackground_EmptyChatID(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	_, err := tm.ExecuteCommandInBackground(context.Background(), "", "echo hello")
	if err == nil {
		t.Fatal("expected error for empty chatID")
	}
	if !strings.Contains(err.Error(), "chatID") {
		t.Errorf("expected error message to mention 'chatID', got %q", err.Error())
	}
}

func TestExecuteCommandInBackground_CommandTooLong(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	longCommand := strings.Repeat("x", maxCommandLength+1)
	_, err := tm.ExecuteCommandInBackground(context.Background(), "chat-1", longCommand)
	if err == nil {
		t.Fatal("expected error for command exceeding max length")
	}
	if !strings.Contains(err.Error(), "too long") {
		t.Errorf("expected error message to mention 'too long', got %q", err.Error())
	}
}

// --- GetBackgroundOutput tests ---

func TestGetBackgroundOutput_NotFound(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	_, err := tm.GetBackgroundOutput("nonexistent-session")
	if err == nil {
		t.Fatal("expected error for non-existent session")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected error message to mention 'not found', got %q", err.Error())
	}
}

func TestGetBackgroundOutput_NotBackgroundSession(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	// Create a regular hidden session (not a background session).
	session, err := tm.CreateHiddenSession("hidden-regular", "agent", "chat-1")
	if err != nil {
		t.Fatalf("CreateHiddenSession failed: %v", err)
	}
	defer tm.CloseSession(session.ID)

	_, err = tm.GetBackgroundOutput(session.ID)
	if err == nil {
		t.Fatal("expected error when calling GetBackgroundOutput on a non-background session")
	}
	if !strings.Contains(err.Error(), "not a background session") {
		t.Errorf("expected error message to mention 'not a background session', got %q", err.Error())
	}
}

func TestGetBackgroundOutput_StripANSI(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PTY")
	}

	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	sessionID, err := tm.ExecuteCommandInBackground(context.Background(), "chat-1", "echo hello")
	if err != nil {
		t.Fatalf("ExecuteCommandInBackground failed: %v", err)
	}
	defer tm.CloseSession(sessionID)

	// Give PTY time to capture output.
	time.Sleep(500 * time.Millisecond)

	output, err := tm.GetBackgroundOutput(sessionID)
	if err != nil {
		t.Fatalf("GetBackgroundOutput failed: %v", err)
	}

	// Output should not contain raw ANSI escape sequences (stripANSI is applied).
	// The ring buffer captures PTY output which includes shell prompts with ANSI codes.
	// After stripping, we should not see ESC characters (0x1b).
	if strings.ContainsRune(output, '\x1b') {
		t.Errorf("output should have ANSI escapes stripped: %q", output)
	}
}

// --- extractCommandPrefix tests ---

func TestExtractCommandPrefix(t *testing.T) {
	cases := []struct {
		name    string
		command string
		want    string
	}{
		{"simple command", "echo hello", "echo"},
		{"command with flags", "npm install --save", "npm"},
		{"command with path", "/usr/bin/python3 script.py", "/usr/bin/python3"},
		{"pipe delimiter", "ls | grep foo", "ls"},
		{"ampersand delimiter", "sleep 10 &", "sleep"},
		{"semicolon delimiter", "echo a; echo b", "echo"},
		{"redirect output", "echo hello > /tmp/out", "echo"},
		{"redirect input", "sort < file.txt", "sort"},
		{"parentheses", "echo $(date)", "echo"},
		{"backtick", "echo `date`", "echo"},
		{"double quote", "echo \"hello\"", "echo"},
		{"single quote", "echo 'hello'", "echo"},
		{"backslash", "echo hello\\", "echo"},
		{"tab separator", "echo\thello", "echo"},
		{"newline separator", "echo\nhello", "echo"},
		{"leading whitespace", "   echo hello", "echo"},
		{"trailing whitespace", "echo hello   ", "echo"},
		{"empty string", "", ""},
		{"only whitespace", "   ", ""},
		{"single word no args", "python3", "python3"},
		{"dollar sign in command", "$HOME/bin/tool arg", "$HOME/bin/tool"},
		{"equals in command", "env KEY=value", "env"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractCommandPrefix(tc.command)
			if got != tc.want {
				t.Errorf("extractCommandPrefix(%q) = %q, want %q", tc.command, got, tc.want)
			}
		})
	}
}

// --- sanitizeSessionIDPart tests ---

func TestSanitizeSessionIDPart(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"alphanumeric", "echo123", "echo123"},
		{"mixed case", "NodeJS", "NodeJS"},
		{"with hyphens", "my-tool", "my-tool"},
		{"with underscores", "my_tool", "my_tool"},
		{"with dots", "my.tool", "my.tool"},
		{"spaces replaced", "my tool", "my-tool"},
		{"special chars replaced", "my@tool#here", "my-tool-here"},
		{"slashes replaced", "/usr/bin/tool", "-usr-bin-tool"},
		{"empty string", "", "unknown"},
		{"only special chars", "!!!", "---"},
		{"long string truncated to 32", strings.Repeat("a", 50), strings.Repeat("a", 32)},
		{"exactly 32 chars", strings.Repeat("x", 32), strings.Repeat("x", 32)},
		{"spaces only", "   ", "---"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeSessionIDPart(tc.input)
			if got != tc.want {
				t.Errorf("sanitizeSessionIDPart(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// --- Cleanup timeout tests ---

func TestCleanupInactiveSessions_BackgroundTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PTY timing-sensitive test in short mode")
	}

	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	// Create a regular hidden session.
	regularSession, err := tm.CreateHiddenSession("regular-hidden", "agent", "chat-1")
	if err != nil {
		t.Fatalf("CreateHiddenSession failed: %v", err)
	}

	// Create a background session via ExecuteCommandInBackground.
	bgSessionID, err := tm.ExecuteCommandInBackground(context.Background(), "chat-2", "echo bg")
	if err != nil {
		// If PTY creation fails (e.g., in some CI environments), skip this test.
		if strings.Contains(err.Error(), "PTY") || strings.Contains(err.Error(), "failed to create") {
			t.Skipf("skipping: PTY creation failed: %v", err)
		}
		t.Fatalf("ExecuteCommandInBackground failed: %v", err)
	}

	// Wait for the background command to finish and the PTY readLoop to settle.
	// The readLoop goroutine resets LastUsed every time it reads PTY output
	// (including shell prompts after command completion). We need to wait until
	// the shell is quiet before setting LastUsed to a past time, otherwise the
	// readLoop will overwrite our timestamp.
	time.Sleep(1 * time.Second)

	// Use large time deltas for reliability on slow platforms.
	// Set regular session to be 5s in the past (> 1s regular timeout).
	regularSession.mutex.Lock()
	regularSession.LastUsed = time.Now().Add(-5 * time.Second)
	regularSession.mutex.Unlock()

	// Set background session to be 5s in the past (> 1s regular timeout, < 30s bg timeout).
	bgSession, _ := tm.GetSession(bgSessionID)
	bgSession.mutex.Lock()
	bgSession.LastUsed = time.Now().Add(-5 * time.Second)
	bgSession.mutex.Unlock()

	// Cleanup with 1s regular timeout and 30s background timeout.
	tm.CleanupInactiveSessions(1*time.Second, 30*time.Second)

	// Regular hidden session should be cleaned up (5s > 1s).
	_, exists := tm.GetSession("regular-hidden")
	if exists {
		t.Error("regular hidden session should be cleaned up after 1s timeout (was inactive for 5s)")
	}

	// Background session should NOT be cleaned up yet (5s < 30s background timeout).
	_, exists = tm.GetSession(bgSessionID)
	if !exists {
		t.Error("background session should NOT be cleaned up before 30s timeout")
	}

	// Set the background session far enough in the past to exceed the 30s timeout.
	bgSession2, _ := tm.GetSession(bgSessionID)
	bgSession2.mutex.Lock()
	bgSession2.LastUsed = time.Now().Add(-31 * time.Second)
	bgSession2.mutex.Unlock()

	// Run cleanup again.
	tm.CleanupInactiveSessions(1*time.Second, 30*time.Second)

	// Now the background session should be cleaned up.
	_, exists = tm.GetSession(bgSessionID)
	if exists {
		t.Error("background session should be cleaned up after 30s timeout")
	}
}

// --- StopBackgroundSession tests ---

func TestStopBackgroundSession_Success(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PTY")
	}

	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	// Start a background session with a long-running command.
	sessionID, err := tm.ExecuteCommandInBackground(context.Background(), "chat-1", "sleep 300")
	if err != nil {
		t.Fatalf("ExecuteCommandInBackground failed: %v", err)
	}

	// Verify it exists.
	_, exists := tm.GetSession(sessionID)
	if !exists {
		t.Fatal("background session should exist before stopping")
	}

	// Stop it.
	err = tm.StopBackgroundSession(sessionID)
	if err != nil {
		t.Fatalf("StopBackgroundSession failed: %v", err)
	}

	// Verify it no longer exists.
	_, exists = tm.GetSession(sessionID)
	if exists {
		t.Error("background session should be removed after StopBackgroundSession")
	}
}

func TestStopBackgroundSession_NotFound(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	err := tm.StopBackgroundSession("nonexistent-session")
	if err == nil {
		t.Error("expected error for nonexistent session")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}
}

func TestStopBackgroundSession_NotBackgroundSession(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PTY")
	}

	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	// Create a regular (non-background) hidden session.
	_, err := tm.CreateHiddenSession("regular-hidden", "agent", "chat-1")
	if err != nil {
		t.Fatalf("CreateHiddenSession failed: %v", err)
	}

	// Trying to stop it as a background session should fail.
	err = tm.StopBackgroundSession("regular-hidden")
	if err == nil {
		t.Error("expected error when stopping non-background session")
	}
	if !strings.Contains(err.Error(), "not a background session") {
		t.Errorf("expected 'not a background session' error, got: %v", err)
	}

	// Clean up.
	_ = tm.CloseSession("regular-hidden")
}
