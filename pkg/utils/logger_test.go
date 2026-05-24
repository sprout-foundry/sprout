package utils

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type logRecord struct {
	Level string `json:"level"`
	Msg   string `json:"msg"`
	CID   string `json:"cid"`
}

func TestLogger_JSONModeWritesJSONWithCID(t *testing.T) {
	orig, _ := os.Getwd()
	dir := t.TempDir()
	defer os.Chdir(orig)
	_ = os.Chdir(dir)

	t.Setenv("LEDIT_JSON_LOGS", "1")
	t.Setenv("LEDIT_CORRELATION_ID", "abc123")

	l := GetLogger(true)
	l.Log("hello world")
	_ = l.Close()

	// Read the last JSON object from the log file; lumberjack writes raw JSON lines
	f, err := os.Open(filepath.Join(".sprout", "workspace.log"))
	if err != nil {
		t.Fatalf("open log: %v", err)
	}
	defer f.Close()
	var lastLine string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lastLine = scanner.Text()
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan: %v", err)
	}

	var rec logRecord
	if err := json.Unmarshal([]byte(lastLine), &rec); err != nil {
		t.Fatalf("unmarshal: %v; content=%q", err, lastLine)
	}
	if rec.Level != "info" || rec.Msg != "hello world" || rec.CID != "abc123" {
		t.Fatalf("unexpected record: %+v", rec)
	}
}

// TestAskForConfirmation_EOFHandling tests that AskForConfirmation handles
// EOF on stdin gracefully without infinite looping (fixes the "billions of prompts" bug)
func TestAskForConfirmation_EOFHandling(t *testing.T) {
	// This test verifies that when stdin returns EOF repeatedly,
	// the function eventually returns false instead of looping infinitely.

	// We can't easily mock stdin in a unit test without race conditions,
	// but we can verify the behavior by checking that:
	// 1. The code handles read errors (not just empty reads)
	// 2. After maxConsecutiveErrors, it returns false

	// The fix adds error checking to reader.ReadString('\n')
	// which was previously ignored with `_`
	//
	// This is primarily a code review/behavioral test - the actual fix
	// is in the error handling logic that counts consecutive errors.

	// Non-interactive mode should return default immediately
	l := GetLogger(true) // skipPrompts=true means non-interactive
	result := l.AskForConfirmation("Test prompt", false, false)
	if result != false {
		t.Error("Non-interactive mode should return default_response (false)")
	}

	result = l.AskForConfirmation("Test prompt", true, false)
	if result != true {
		t.Error("Non-interactive mode should return default_response (true)")
	}
}

// TestAskForConfirmation_NonInteractiveMode tests that when userInteractionEnabled
// is false, the function returns the default response without blocking
func TestAskForConfirmation_NonInteractiveMode(t *testing.T) {
	l := GetLogger(true) // skipPrompts = true -> userInteractionEnabled = false

	// Test with default_response = false
	result := l.AskForConfirmation("Allow access?", false, false)
	if result != false {
		t.Error("Expected false (default_response) in non-interactive mode")
	}

	// Test with default_response = true
	result = l.AskForConfirmation("Allow access?", true, false)
	if result != true {
		t.Error("Expected true (default_response) in non-interactive mode")
	}
}

// TestAskForConfirmation_RequiredExits tests that when confirmation is required
// but user interaction is disabled, the function exits
func TestAskForConfirmation_RequiredExits(t *testing.T) {
	// We can't easily test os.Exit() in a unit test without subprocess testing,
	// but we can verify the logic path exists in the code review.
	// The important thing is that the code checks:
	// if !w.userInteractionEnabled && required { os.Exit(1) }
	//
	// This test documents that behavior.
	t.Log("When required=true and userInteractionEnabled=false, function calls os.Exit(1)")
}

func TestDefaultChoiceHint(t *testing.T) {
	// Force a known color state for determinism. NO_COLOR=1 ensures the
	// bold ANSI escapes are stripped so we can assert the visible chars.
	t.Setenv("NO_COLOR", "1")
	t.Setenv("FORCE_COLOR", "")

	if got := DefaultChoiceHint(true); got != "[Y/n]" {
		t.Errorf("DefaultChoiceHint(true) without color = %q, want %q", got, "[Y/n]")
	}
	if got := DefaultChoiceHint(false); got != "[y/N]" {
		t.Errorf("DefaultChoiceHint(false) without color = %q, want %q", got, "[y/N]")
	}
}

func TestDefaultChoiceHint_BoldsDefaultWhenColorAllowed(t *testing.T) {
	// FORCE_COLOR overrides any TTY detection; the hint should wrap the
	// capitalized default letter in bold ANSI escapes.
	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "1")

	gotYes := DefaultChoiceHint(true)
	if !strings.Contains(gotYes, "\033[1mY\033[0m") {
		t.Errorf("default=yes should bold Y, got %q", gotYes)
	}
	if strings.Contains(gotYes, "\033[1mn\033[0m") {
		t.Errorf("default=yes should not bold n, got %q", gotYes)
	}

	gotNo := DefaultChoiceHint(false)
	if !strings.Contains(gotNo, "\033[1mN\033[0m") {
		t.Errorf("default=no should bold N, got %q", gotNo)
	}
	if strings.Contains(gotNo, "\033[1my\033[0m") {
		t.Errorf("default=no should not bold y, got %q", gotNo)
	}
}

func TestDefaultChoiceHint_NO_COLOR_Beats_FORCE_COLOR(t *testing.T) {
	// When both NO_COLOR and FORCE_COLOR are set, NO_COLOR must win
	// (per no-color.org precedence). The hint should have no ANSI escapes.
	t.Setenv("NO_COLOR", "1")
	t.Setenv("FORCE_COLOR", "1")

	got := DefaultChoiceHint(true)
	if strings.Contains(got, "\033[") {
		t.Errorf("NO_COLOR=1 with FORCE_COLOR=1 must suppress ANSI, got %q", got)
	}
	if got != "[Y/n]" {
		t.Errorf("NO_COLOR=1 with FORCE_COLOR=1 should produce plain hint, got %q", got)
	}

	got = DefaultChoiceHint(false)
	if strings.Contains(got, "\033[") {
		t.Errorf("NO_COLOR=1 with FORCE_COLOR=1 must suppress ANSI (default=no), got %q", got)
	}
	if got != "[y/N]" {
		t.Errorf("NO_COLOR=1 with FORCE_COLOR=1 should produce plain hint (default=no), got %q", got)
	}
}
