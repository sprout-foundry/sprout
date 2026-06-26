//go:build !js

package webui

import (
	"fmt"
	"strings"
	"testing"
)

// ====================================================================
// AddToHistory
// ====================================================================

func TestAddToHistory_ValidCommand(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)
	if _, err := tm.CreateSession("s1"); err != nil {
		t.Skipf("CreateSession failed: %v", err)
	}
	defer tm.CloseSession("s1")

	err := tm.AddToHistory("s1", "echo hello")
	if err != nil {
		t.Fatalf("AddToHistory failed: %v", err)
	}

	history, err := tm.GetHistory("s1")
	if err != nil {
		t.Fatalf("GetHistory failed: %v", err)
	}
	if len(history) != 1 || history[0] != "echo hello" {
		t.Errorf("history = %v, want [echo hello]", history)
	}

	session, _ := tm.GetSession("s1")
	session.mutex.RLock()
	idx := session.HistoryIndex
	session.mutex.RUnlock()
	if idx != 1 {
		t.Errorf("HistoryIndex = %d, want 1", idx)
	}
}

func TestAddToHistory_DuplicateCommand(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)
	if _, err := tm.CreateSession("s1"); err != nil {
		t.Skipf("CreateSession failed: %v", err)
	}
	defer tm.CloseSession("s1")

	tm.AddToHistory("s1", "echo hello")
	tm.AddToHistory("s1", "echo hello") // duplicate

	history, err := tm.GetHistory("s1")
	if err != nil {
		t.Fatalf("GetHistory failed: %v", err)
	}
	if len(history) != 1 {
		t.Errorf("duplicate command was added: history = %v, want 1 entry", history)
	}
}

func TestAddToHistory_EmptyCommand(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)
	if _, err := tm.CreateSession("s1"); err != nil {
		t.Skipf("CreateSession failed: %v", err)
	}
	defer tm.CloseSession("s1")

	// Empty string
	err := tm.AddToHistory("s1", "")
	if err != nil {
		t.Fatalf("AddToHistory(\"\") failed: %v", err)
	}

	// Whitespace only
	err = tm.AddToHistory("s1", "   ")
	if err != nil {
		t.Fatalf("AddToHistory(\"   \") failed: %v", err)
	}

	history, _ := tm.GetHistory("s1")
	if len(history) != 0 {
		t.Errorf("empty commands should be skipped: history = %v", history)
	}
}

func TestAddToHistory_SessionNotFound(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	err := tm.AddToHistory("nonexistent", "echo hello")
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, should mention 'not found'", err.Error())
	}
}

func TestAddToHistory_HiddenSession(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)
	if _, err := tm.CreateHiddenSession("hidden-1", "agent", "chat-1"); err != nil {
		t.Skipf("CreateHiddenSession failed: %v", err)
	}
	defer tm.CloseSession("hidden-1")

	err := tm.AddToHistory("hidden-1", "echo hello")
	if err == nil {
		t.Fatal("expected error for hidden session")
	}
	if !strings.Contains(err.Error(), "not accessible") {
		t.Errorf("error = %q, should mention 'not accessible'", err.Error())
	}
}

func TestAddToHistory_CapAt1000(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)
	if _, err := tm.CreateSession("s1"); err != nil {
		t.Skipf("CreateSession failed: %v", err)
	}
	defer tm.CloseSession("s1")

	// Add 1100 commands
	for i := 0; i < 1100; i++ {
		err := tm.AddToHistory("s1", fmt.Sprintf("cmd-%d", i))
		if err != nil {
			t.Fatalf("AddToHistory failed at %d: %v", i, err)
		}
	}

	history, err := tm.GetHistory("s1")
	if err != nil {
		t.Fatalf("GetHistory failed: %v", err)
	}
	if len(history) != 1000 {
		t.Errorf("history length = %d, want 1000", len(history))
	}
	// Oldest should be cmd-100 (the 101st command, since 0-99 were trimmed)
	if history[0] != "cmd-100" {
		t.Errorf("first entry = %q, want cmd-100", history[0])
	}
	// Latest should be cmd-1099
	if history[999] != "cmd-1099" {
		t.Errorf("last entry = %q, want cmd-1099", history[999])
	}
}

// ====================================================================
// GetHistory
// ====================================================================

func TestGetHistory_ReturnsCopy(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)
	if _, err := tm.CreateSession("s1"); err != nil {
		t.Skipf("CreateSession failed: %v", err)
	}
	defer tm.CloseSession("s1")

	tm.AddToHistory("s1", "cmd-1")

	history, err := tm.GetHistory("s1")
	if err != nil {
		t.Fatalf("GetHistory failed: %v", err)
	}

	// Mutate returned slice
	history[0] = "mutated"
	history = append(history, "extra")

	// Internal history should be unchanged
	history2, err := tm.GetHistory("s1")
	if err != nil {
		t.Fatalf("GetHistory failed: %v", err)
	}
	if len(history2) != 1 || history2[0] != "cmd-1" {
		t.Errorf("internal history was mutated: %v, want [cmd-1]", history2)
	}
}

func TestGetHistory_SessionNotFound(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	_, err := tm.GetHistory("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, should mention 'not found'", err.Error())
	}
}

func TestGetHistory_HiddenSession(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)
	if _, err := tm.CreateHiddenSession("hidden-1", "agent", "chat-1"); err != nil {
		t.Skipf("CreateHiddenSession failed: %v", err)
	}
	defer tm.CloseSession("hidden-1")

	_, err := tm.GetHistory("hidden-1")
	if err == nil {
		t.Fatal("expected error for hidden session")
	}
	if !strings.Contains(err.Error(), "not accessible") {
		t.Errorf("error = %q, should mention 'not accessible'", err.Error())
	}
}

func TestGetHistory_EmptyHistory(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)
	if _, err := tm.CreateSession("s1"); err != nil {
		t.Skipf("CreateSession failed: %v", err)
	}
	defer tm.CloseSession("s1")

	history, err := tm.GetHistory("s1")
	if err != nil {
		t.Fatalf("GetHistory failed: %v", err)
	}
	if len(history) != 0 {
		t.Errorf("empty history should return empty slice, got %v", history)
	}
}

// ====================================================================
// NavigateHistory
// ====================================================================

func TestNavigateHistory_UpAndDown(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)
	if _, err := tm.CreateSession("s1"); err != nil {
		t.Skipf("CreateSession failed: %v", err)
	}
	defer tm.CloseSession("s1")

	tm.AddToHistory("s1", "cmd-1")
	tm.AddToHistory("s1", "cmd-2")
	tm.AddToHistory("s1", "cmd-3")

	// Navigate up: should go cmd-3, cmd-2, cmd-1
	cmd, err := tm.NavigateHistory("s1", "up")
	if err != nil {
		t.Fatalf("NavigateHistory up failed: %v", err)
	}
	if cmd != "cmd-3" {
		t.Errorf("up[0] = %q, want cmd-3", cmd)
	}

	cmd, err = tm.NavigateHistory("s1", "up")
	if err != nil {
		t.Fatalf("NavigateHistory up failed: %v", err)
	}
	if cmd != "cmd-2" {
		t.Errorf("up[1] = %q, want cmd-2", cmd)
	}

	cmd, err = tm.NavigateHistory("s1", "up")
	if err != nil {
		t.Fatalf("NavigateHistory up failed: %v", err)
	}
	if cmd != "cmd-1" {
		t.Errorf("up[2] = %q, want cmd-1", cmd)
	}

	// At beginning, further up should stay at cmd-1
	cmd, err = tm.NavigateHistory("s1", "up")
	if err != nil {
		t.Fatalf("NavigateHistory up at boundary failed: %v", err)
	}
	if cmd != "cmd-1" {
		t.Errorf("up at boundary = %q, want cmd-1", cmd)
	}

	// Navigate down: cmd-2, cmd-3
	cmd, err = tm.NavigateHistory("s1", "down")
	if err != nil {
		t.Fatalf("NavigateHistory down failed: %v", err)
	}
	if cmd != "cmd-2" {
		t.Errorf("down[0] = %q, want cmd-2", cmd)
	}

	cmd, err = tm.NavigateHistory("s1", "down")
	if err != nil {
		t.Fatalf("NavigateHistory down failed: %v", err)
	}
	if cmd != "cmd-3" {
		t.Errorf("down[1] = %q, want cmd-3", cmd)
	}

	// At last command, down should return empty
	cmd, err = tm.NavigateHistory("s1", "down")
	if err != nil {
		t.Fatalf("NavigateHistory down at end failed: %v", err)
	}
	if cmd != "" {
		t.Errorf("down at end = %q, want empty", cmd)
	}
}

func TestNavigateHistory_EmptyHistory(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)
	if _, err := tm.CreateSession("s1"); err != nil {
		t.Skipf("CreateSession failed: %v", err)
	}
	defer tm.CloseSession("s1")

	cmd, err := tm.NavigateHistory("s1", "up")
	if err != nil {
		t.Fatalf("NavigateHistory on empty history failed: %v", err)
	}
	if cmd != "" {
		t.Errorf("up on empty history = %q, want empty", cmd)
	}
}

func TestNavigateHistory_InvalidDirection(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)
	if _, err := tm.CreateSession("s1"); err != nil {
		t.Skipf("CreateSession failed: %v", err)
	}
	defer tm.CloseSession("s1")

	// Need at least one history entry so we reach the switch statement
	tm.AddToHistory("s1", "echo hello")

	_, err := tm.NavigateHistory("s1", "left")
	if err == nil {
		t.Fatal("expected error for invalid direction")
	}
	if !strings.Contains(err.Error(), "invalid direction") {
		t.Errorf("error = %q, should mention 'invalid direction'", err.Error())
	}
}

func TestNavigateHistory_SessionNotFound(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	_, err := tm.NavigateHistory("nonexistent", "up")
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, should mention 'not found'", err.Error())
	}
}

func TestNavigateHistory_HiddenSession(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)
	if _, err := tm.CreateHiddenSession("hidden-1", "agent", "chat-1"); err != nil {
		t.Skipf("CreateHiddenSession failed: %v", err)
	}
	defer tm.CloseSession("hidden-1")

	_, err := tm.NavigateHistory("hidden-1", "up")
	if err == nil {
		t.Fatal("expected error for hidden session")
	}
	if !strings.Contains(err.Error(), "not accessible") {
		t.Errorf("error = %q, should mention 'not accessible'", err.Error())
	}
}

// ====================================================================
// ResetHistoryIndex
// ====================================================================

func TestResetHistoryIndex_ResetsToEnd(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)
	if _, err := tm.CreateSession("s1"); err != nil {
		t.Skipf("CreateSession failed: %v", err)
	}
	defer tm.CloseSession("s1")

	tm.AddToHistory("s1", "cmd-1")
	tm.AddToHistory("s1", "cmd-2")

	// Navigate up
	tm.NavigateHistory("s1", "up")

	session, _ := tm.GetSession("s1")
	session.mutex.RLock()
	idx := session.HistoryIndex
	session.mutex.RUnlock()
	if idx != 1 {
		t.Errorf("after up, index = %d, want 1", idx)
	}

	// Reset
	err := tm.ResetHistoryIndex("s1")
	if err != nil {
		t.Fatalf("ResetHistoryIndex failed: %v", err)
	}

	session.mutex.RLock()
	idx = session.HistoryIndex
	session.mutex.RUnlock()
	if idx != 2 {
		t.Errorf("after reset, index = %d, want 2", idx)
	}
}

func TestResetHistoryIndex_SessionNotFound(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	err := tm.ResetHistoryIndex("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, should mention 'not found'", err.Error())
	}
}

func TestResetHistoryIndex_HiddenSession(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)
	if _, err := tm.CreateHiddenSession("hidden-1", "agent", "chat-1"); err != nil {
		t.Skipf("CreateHiddenSession failed: %v", err)
	}
	defer tm.CloseSession("hidden-1")

	err := tm.ResetHistoryIndex("hidden-1")
	if err == nil {
		t.Fatal("expected error for hidden session")
	}
	if !strings.Contains(err.Error(), "not accessible") {
		t.Errorf("error = %q, should mention 'not accessible'", err.Error())
	}
}
