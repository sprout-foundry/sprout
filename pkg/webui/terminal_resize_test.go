//go:build !js

package webui

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
)

// ====================================================================
// ResizeTerminal
// ====================================================================

func TestResizeTerminal_ZeroRows(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	err := tm.ResizeTerminal("s1", 0, 80)
	if err == nil {
		t.Fatal("expected error for zero rows")
	}
	if !strings.Contains(err.Error(), "zero") {
		t.Errorf("error = %q, should mention 'zero'", err.Error())
	}
}

func TestResizeTerminal_ZeroCols(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	err := tm.ResizeTerminal("s1", 24, 0)
	if err == nil {
		t.Fatal("expected error for zero cols")
	}
	if !strings.Contains(err.Error(), "zero") {
		t.Errorf("error = %q, should mention 'zero'", err.Error())
	}
}

func TestResizeTerminal_BothZero(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	err := tm.ResizeTerminal("s1", 0, 0)
	if err == nil {
		t.Fatal("expected error for zero dimensions")
	}
	if !strings.Contains(err.Error(), "zero") {
		t.Errorf("error = %q, should mention 'zero'", err.Error())
	}
}

func TestResizeTerminal_SessionNotFound(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	err := tm.ResizeTerminal("nonexistent", 24, 80)
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, should mention 'not found'", err.Error())
	}
}

func TestResizeTerminal_InactiveSession(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	session := &TerminalSession{
		ID:     "inactive-1",
		Active: false,
		Size:   &pty.Winsize{Rows: 24, Cols: 80},
		ring:   newSessRing(),
	}
	tm.mutex.Lock()
	tm.sessions["inactive-1"] = session
	tm.mutex.Unlock()

	err := tm.ResizeTerminal("inactive-1", 30, 100)
	if err == nil {
		t.Fatal("expected error for inactive session")
	}
	if !strings.Contains(err.Error(), "not active") {
		t.Errorf("error = %q, should mention 'not active'", err.Error())
	}
}

func TestResizeTerminal_HiddenSession(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	session := &TerminalSession{
		ID:     "hidden-1",
		Active: true,
		Hidden: true,
		Size:   &pty.Winsize{Rows: 24, Cols: 80},
		ring:   newSessRing(),
	}
	tm.mutex.Lock()
	tm.sessions["hidden-1"] = session
	tm.mutex.Unlock()

	err := tm.ResizeTerminal("hidden-1", 30, 100)
	if err == nil {
		t.Fatal("expected error for hidden session")
	}
	if !strings.Contains(err.Error(), "not accessible") {
		t.Errorf("error = %q, should mention 'not accessible'", err.Error())
	}
}

func TestResizeTerminal_NilPTY(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	session := &TerminalSession{
		ID:     "no-pty-1",
		Active: true,
		Pty:    nil, // no PTY
		Size:   &pty.Winsize{Rows: 24, Cols: 80},
		ring:   newSessRing(),
	}
	tm.mutex.Lock()
	tm.sessions["no-pty-1"] = session
	tm.mutex.Unlock()

	err := tm.ResizeTerminal("no-pty-1", 30, 100)
	if err == nil {
		t.Fatal("expected error for nil PTY")
	}
	if !strings.Contains(err.Error(), "no PTY") {
		t.Errorf("error = %q, should mention 'no PTY'", err.Error())
	}
}

func TestResizeTerminal_ActiveWithPTY(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PTY")
	}

	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	session, err := tm.CreateSession("resize-session")
	if err != nil {
		t.Skipf("CreateSession failed: %v", err)
	}
	defer tm.CloseSession("resize-session")

	// Wait for PTY to be assigned
	for i := 0; i < 50; i++ {
		session.mutex.RLock()
		hasPty := session.Pty != nil
		session.mutex.RUnlock()
		if hasPty {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	session.mutex.RLock()
	ptyNotNil := session.Pty != nil
	session.mutex.RUnlock()
	if !ptyNotNil {
		t.Skip("PTY not assigned in time")
	}

	err = tm.ResizeTerminal("resize-session", 30, 100)
	if err != nil {
		t.Fatalf("ResizeTerminal failed: %v", err)
	}

	session.mutex.RLock()
	rows := session.Size.Rows
	cols := session.Size.Cols
	session.mutex.RUnlock()

	if rows != 30 {
		t.Errorf("rows = %d, want 30", rows)
	}
	if cols != 100 {
		t.Errorf("cols = %d, want 100", cols)
	}
}

// ====================================================================
// GetTerminalSize
// ====================================================================

func TestGetTerminalSize_SessionNotFound(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	_, err := tm.GetTerminalSize("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, should mention 'not found'", err.Error())
	}
}

func TestGetTerminalSize_HiddenSession(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	session := &TerminalSession{
		ID:     "hidden-1",
		Active: true,
		Hidden: true,
		Size:   &pty.Winsize{Rows: 24, Cols: 80},
		ring:   newSessRing(),
	}
	tm.mutex.Lock()
	tm.sessions["hidden-1"] = session
	tm.mutex.Unlock()

	_, err := tm.GetTerminalSize("hidden-1")
	if err == nil {
		t.Fatal("expected error for hidden session")
	}
	if !strings.Contains(err.Error(), "not accessible") {
		t.Errorf("error = %q, should mention 'not accessible'", err.Error())
	}
}

func TestGetTerminalSize_NilSize(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	session := &TerminalSession{
		ID:     "no-size-1",
		Active: true,
		Size:   nil, // no size
		ring:   newSessRing(),
	}
	tm.mutex.Lock()
	tm.sessions["no-size-1"] = session
	tm.mutex.Unlock()

	_, err := tm.GetTerminalSize("no-size-1")
	if err == nil {
		t.Fatal("expected error for nil size")
	}
	if !strings.Contains(err.Error(), "not set") {
		t.Errorf("error = %q, should mention 'not set'", err.Error())
	}
}

func TestGetTerminalSize_ValidReturnsCopy(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	session := &TerminalSession{
		ID:     "s1",
		Active: true,
		Size:   &pty.Winsize{Rows: 24, Cols: 80},
		ring:   newSessRing(),
	}
	tm.mutex.Lock()
	tm.sessions["s1"] = session
	tm.mutex.Unlock()

	size, err := tm.GetTerminalSize("s1")
	if err != nil {
		t.Fatalf("GetTerminalSize failed: %v", err)
	}
	if size.Rows != 24 {
		t.Errorf("rows = %d, want 24", size.Rows)
	}
	if size.Cols != 80 {
		t.Errorf("cols = %d, want 80", size.Cols)
	}

	// Mutate returned copy
	size.Rows = 999
	size.Cols = 999

	// Internal should be unchanged
	session.mutex.RLock()
	internalRows := session.Size.Rows
	internalCols := session.Size.Cols
	session.mutex.RUnlock()

	if internalRows != 24 {
		t.Errorf("internal rows = %d, want 24 (was mutated by returned copy)", internalRows)
	}
	if internalCols != 80 {
		t.Errorf("internal cols = %d, want 80 (was mutated by returned copy)", internalCols)
	}
}

func TestGetTerminalSize_ReturnsNilError(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	size, err := tm.GetTerminalSize("nonexistent")
	if err == nil {
		t.Fatal("expected error")
	}
	if size != nil {
		t.Error("should return nil size on error")
	}
}

// Additional test: resize with a real PTY from CreateSession, verify the
// error message format for zero dimensions matches what frontend expects.
func TestResizeTerminal_ZeroDimensionErrorFormat(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	err := tm.ResizeTerminal("s1", 0, 0)
	if err == nil {
		t.Fatal("expected error")
	}
	errMsg := err.Error()
	if !strings.Contains(errMsg, "0x0") {
		t.Errorf("error should contain '0x0': %q", errMsg)
	}
	if !strings.Contains(errMsg, "corrupt") {
		t.Errorf("error should mention 'corrupt': %q", errMsg)
	}
}

func TestResizeTerminal_ZeroRowsNotCols(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	err := tm.ResizeTerminal("s1", 0, 100)
	if err == nil {
		t.Fatal("expected error")
	}
	// Format is (cols x rows) = (100x0)
	if !strings.Contains(err.Error(), "100x0") {
		t.Errorf("error should contain '100x0': %q", err.Error())
	}
}

func TestResizeTerminal_ZeroColsNotRows(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	err := tm.ResizeTerminal("s1", 24, 0)
	if err == nil {
		t.Fatal("expected error")
	}
	// Format is (cols x rows) = (0x24)
	if !strings.Contains(err.Error(), "0x24") {
		t.Errorf("error should contain '0x24': %q", err.Error())
	}
}

// Additional: Test ResizeTerminal with a file descriptor check
func TestResizeTerminal_NilPTYErrorFormat(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	session := &TerminalSession{
		ID:     "no-pty-2",
		Active: true,
		Pty:    (*os.File)(nil),
		Size:   &pty.Winsize{Rows: 24, Cols: 80},
		ring:   newSessRing(),
	}
	tm.mutex.Lock()
	tm.sessions["no-pty-2"] = session
	tm.mutex.Unlock()

	err := tm.ResizeTerminal("no-pty-2", 30, 100)
	if err == nil {
		t.Fatal("expected error for nil PTY")
	}
	if !strings.Contains(err.Error(), "no PTY available") {
		t.Errorf("error = %q, should mention 'no PTY available'", err.Error())
	}
}
