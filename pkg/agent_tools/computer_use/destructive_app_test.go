package computer_use

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// ============================================================================
// PreActionHook — auditingBackend hook integration
// ============================================================================

// setupAuditingBackend creates an auditingBackend wrapping a MockBackend
// in a temp dir. Returns the audit backend, inner mock, and the JSONL path.
func setupAuditingBackend(t *testing.T) (*auditingBackend, *MockBackend, string) {
	t.Helper()
	dir := t.TempDir()
	mock := &MockBackend{}
	ab, err := NewAuditingBackend(mock, dir, "test-session")
	if err != nil {
		t.Fatalf("NewAuditingBackend: %v", err)
	}
	t.Cleanup(func() { ab.Close() })
	jsonlPath := filepath.Join(dir, "test-session.jsonl")
	return ab, mock, jsonlPath
}

// readAuditLines reads the JSONL file and returns the parsed AuditRecords.
func readAuditLines(t *testing.T, path string) []AuditRecord {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	var records []AuditRecord
	dec := json.NewDecoder(strings.NewReader(string(data)))
	for dec.More() {
		var rec AuditRecord
		if err := dec.Decode(&rec); err != nil {
			t.Fatalf("decode audit line: %v", err)
		}
		records = append(records, rec)
	}
	return records
}

func TestPreActionHook_FiresForMouseClick(t *testing.T) {
	ab, mock, _ := setupAuditingBackend(t)
	hookCalled := false
	ab.SetPreActionHook(func(action string, args map[string]any) error {
		hookCalled = true
		if action != "mouse_click" {
			t.Errorf("hook action: got %q, want %q", action, "mouse_click")
		}
		return nil
	})

	err := ab.MouseClick(10, 20, MouseLeft, false)
	if err != nil {
		t.Fatalf("MouseClick: %v", err)
	}
	if !hookCalled {
		t.Fatal("PreActionHook was not called for MouseClick")
	}
	// Inner mock should have been called because hook returned nil.
	if len(mock.Records) != 1 || mock.Records[0].Action != "MouseClick" {
		t.Errorf("inner mock records: %+v", mock.Records)
	}
}

func TestPreActionHook_FiresForMouseDrag(t *testing.T) {
	ab, mock, _ := setupAuditingBackend(t)
	hookCalled := false
	ab.SetPreActionHook(func(action string, args map[string]any) error {
		hookCalled = true
		if action != "mouse_drag" {
			t.Errorf("hook action: got %q, want %q", action, "mouse_drag")
		}
		return nil
	})

	err := ab.MouseDrag(Point{X: 0, Y: 0}, Point{X: 100, Y: 100}, MouseLeft)
	if err != nil {
		t.Fatalf("MouseDrag: %v", err)
	}
	if !hookCalled {
		t.Fatal("PreActionHook was not called for MouseDrag")
	}
	if len(mock.Records) != 1 || mock.Records[0].Action != "MouseDrag" {
		t.Errorf("inner mock records: %+v", mock.Records)
	}
}

func TestPreActionHook_FiresForKeyboardPress(t *testing.T) {
	ab, mock, _ := setupAuditingBackend(t)
	hookCalled := false
	ab.SetPreActionHook(func(action string, args map[string]any) error {
		hookCalled = true
		if action != "keyboard_press" {
			t.Errorf("hook action: got %q, want %q", action, "keyboard_press")
		}
		return nil
	})

	err := ab.KeyboardPress("enter")
	if err != nil {
		t.Fatalf("KeyboardPress: %v", err)
	}
	if !hookCalled {
		t.Fatal("PreActionHook was not called for KeyboardPress")
	}
	if len(mock.Records) != 1 || mock.Records[0].Action != "KeyboardPress" {
		t.Errorf("inner mock records: %+v", mock.Records)
	}
}

func TestPreActionHook_FiresForScroll(t *testing.T) {
	ab, mock, _ := setupAuditingBackend(t)
	hookCalled := false
	ab.SetPreActionHook(func(action string, args map[string]any) error {
		hookCalled = true
		if action != "scroll" {
			t.Errorf("hook action: got %q, want %q", action, "scroll")
		}
		return nil
	})

	err := ab.Scroll(ScrollDown, 5, nil)
	if err != nil {
		t.Fatalf("Scroll: %v", err)
	}
	if !hookCalled {
		t.Fatal("PreActionHook was not called for Scroll")
	}
	if len(mock.Records) != 1 || mock.Records[0].Action != "Scroll" {
		t.Errorf("inner mock records: %+v", mock.Records)
	}
}

func TestPreActionHook_NotFiredForScreenshot(t *testing.T) {
	ab, _, _ := setupAuditingBackend(t)
	hookCalled := false
	ab.SetPreActionHook(func(action string, args map[string]any) error {
		hookCalled = true
		return nil
	})

	_, _, err := ab.Screenshot(nil)
	if err != nil {
		t.Fatalf("Screenshot: %v", err)
	}
	if hookCalled {
		t.Error("PreActionHook should NOT be called for Screenshot")
	}
}

func TestPreActionHook_NotFiredForKeyboardType(t *testing.T) {
	ab, _, _ := setupAuditingBackend(t)
	hookCalled := false
	ab.SetPreActionHook(func(action string, args map[string]any) error {
		hookCalled = true
		return nil
	})

	err := ab.KeyboardType("hello")
	if err != nil {
		t.Fatalf("KeyboardType: %v", err)
	}
	if hookCalled {
		t.Error("PreActionHook should NOT be called for KeyboardType")
	}
}

func TestPreActionHook_NotFiredForMoveTo(t *testing.T) {
	ab, _, _ := setupAuditingBackend(t)
	hookCalled := false
	ab.SetPreActionHook(func(action string, args map[string]any) error {
		hookCalled = true
		return nil
	})

	err := ab.MoveTo(100, 200)
	if err != nil {
		t.Fatalf("MoveTo: %v", err)
	}
	if hookCalled {
		t.Error("PreActionHook should NOT be called for MoveTo")
	}
}

func TestPreActionHook_HookErrorBlocksAction(t *testing.T) {
	ab, mock, _ := setupAuditingBackend(t)
	blockErr := errors.New("blocked by gate")
	ab.SetPreActionHook(func(action string, args map[string]any) error {
		return blockErr
	})

	err := ab.MouseClick(10, 20, MouseLeft, false)
	if err != blockErr {
		t.Errorf("expected hook error, got: %v", err)
	}
	// Inner mock should NOT have been called.
	if len(mock.Records) != 0 {
		t.Errorf("inner mock should not have been called, got %d records", len(mock.Records))
	}
}

func TestPreActionHook_NilIsNoop(t *testing.T) {
	ab, mock, _ := setupAuditingBackend(t)
	// No hook set — default nil.
	err := ab.MouseClick(10, 20, MouseLeft, false)
	if err != nil {
		t.Fatalf("MouseClick: %v", err)
	}
	if len(mock.Records) != 1 {
		t.Errorf("expected 1 record with nil hook, got %d", len(mock.Records))
	}
}

func TestPreActionHook_HookErrorRecordedInAudit(t *testing.T) {
	ab, _, jsonlPath := setupAuditingBackend(t)
	blockErr := errors.New("gate-blocked")
	ab.SetPreActionHook(func(action string, args map[string]any) error {
		return blockErr
	})

	err := ab.MouseClick(10, 20, MouseLeft, false)
	if err != blockErr {
		t.Errorf("expected hook error, got: %v", err)
	}

	// Give the file a moment to flush.
	time.Sleep(50 * time.Millisecond)
	records := readAuditLines(t, jsonlPath)
	if len(records) != 1 {
		t.Fatalf("expected 1 audit record, got %d", len(records))
	}
	if records[0].Action != "mouse_click" {
		t.Errorf("audit action: got %q, want %q", records[0].Action, "mouse_click")
	}
	if records[0].Err != "gate-blocked" {
		t.Errorf("audit error: got %q, want %q", records[0].Err, "gate-blocked")
	}
}

// ============================================================================
// SetPreActionHook — race safety
// ============================================================================

func TestSetPreActionHook_RaceSafety(t *testing.T) {
	ab, _, _ := setupAuditingBackend(t)
	const goroutines = 100
	var wg sync.WaitGroup
	wg.Add(goroutines * 2)

	// Writers: set hooks concurrently.
	for i := 0; i < goroutines; i++ {
		go func(n int) {
			defer wg.Done()
			ab.SetPreActionHook(func(action string, args map[string]any) error {
				return nil
			})
		}(i)
	}

	// Readers: invoke MouseClick concurrently (reads the hook).
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			_ = ab.MouseClick(1, 2, MouseLeft, false)
		}()
	}

	wg.Wait()
}

// ============================================================================
// DestructiveAppPrompter — setter/getter + default
// ============================================================================

func TestDestructiveAppPrompter_DefaultReturnsDeny(t *testing.T) {
	// Reset to nil (default).
	SetDestructiveAppPrompter(nil)

	prompter := GetDestructiveAppPrompter()
	decision := prompter.PromptDestructiveApp(context.Background(), "mouse_click", nil, Classification{})
	if decision != DestructiveAppDeny {
		t.Errorf("default prompter: got %v, want DestructiveAppDeny", decision)
	}
}

func TestDestructiveAppPrompter_SetterGetterRoundtrip(t *testing.T) {
	SetDestructiveAppPrompter(nil) // reset

	mp := &mockPrompterInline{decision: DestructiveAppAllowOnce}
	SetDestructiveAppPrompter(mp)

	got := GetDestructiveAppPrompter()
	if got == nil {
		t.Fatal("GetDestructiveAppPrompter returned nil after Set")
	}
	decision := got.PromptDestructiveApp(context.Background(), "test", nil, Classification{})
	if decision != DestructiveAppAllowOnce {
		t.Errorf("prompter decision: got %v, want DestructiveAppAllowOnce", decision)
	}
}

// mockPrompterInline implements DestructiveAppPrompter as an inline struct.
type mockPrompterInline struct {
	decision DestructiveAppDecision
}

func (m *mockPrompterInline) PromptDestructiveApp(_ context.Context, _ string, _ map[string]any, _ Classification) DestructiveAppDecision {
	return m.decision
}

func TestDestructiveAppPrompter_SetterGetterRaceSafety(t *testing.T) {
	SetDestructiveAppPrompter(nil)
	const goroutines = 100
	var wg sync.WaitGroup
	wg.Add(goroutines * 2)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			SetDestructiveAppPrompter(&mockPrompterInline{decision: DestructiveAppAllowOnce})
		}()
	}
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			_ = GetDestructiveAppPrompter()
		}()
	}
	wg.Wait()
	SetDestructiveAppPrompter(nil) // reset
}

// ============================================================================
// classifyAndPrompt helper
// ============================================================================

func TestClassifyAndPrompt_NoForegroundInfo(t *testing.T) {
	// Empty ForegroundInfo → skip gate entirely.
	err := classifyAndPrompt(context.Background(), "mouse_click", nil, ForegroundInfo{})
	if err != nil {
		t.Errorf("expected nil for empty ForegroundInfo, got: %v", err)
	}
}

func TestClassifyAndPrompt_NotBlockedApp(t *testing.T) {
	// App not on denylist → fast-path nil.
	err := classifyAndPrompt(context.Background(), "mouse_click", nil, ForegroundInfo{
		AppName:     "TextEditor",
		BundleID:    "com.example.texteditor",
		WindowClass: "TextEditor",
	})
	if err != nil {
		t.Errorf("expected nil for non-blocked app, got: %v", err)
	}
}

func TestClassifyAndPrompt_BlockedNoPrompter(t *testing.T) {
	SetDestructiveAppPrompter(nil) // default deny

	err := classifyAndPrompt(context.Background(), "mouse_click", nil, ForegroundInfo{
		BundleID: "com.apple.mail",
		AppName:  "Mail",
	})
	if err != ErrDestructiveAppBlocked {
		t.Errorf("expected ErrDestructiveAppBlocked, got: %v", err)
	}
}

func TestClassifyAndPrompt_BlockedAllowOnce(t *testing.T) {
	SetDestructiveAppPrompter(&mockPrompterInline{decision: DestructiveAppAllowOnce})
	t.Cleanup(func() { SetDestructiveAppPrompter(nil) })

	err := classifyAndPrompt(context.Background(), "mouse_click", nil, ForegroundInfo{
		BundleID: "com.apple.mail",
		AppName:  "Mail",
	})
	if err != nil {
		t.Errorf("expected nil when prompter allows once, got: %v", err)
	}
}

func TestClassifyAndPrompt_BlockedAllowAlways(t *testing.T) {
	SetDestructiveAppPrompter(&mockPrompterInline{decision: DestructiveAppAllowAlways})
	t.Cleanup(func() { SetDestructiveAppPrompter(nil) })

	err := classifyAndPrompt(context.Background(), "mouse_click", nil, ForegroundInfo{
		BundleID: "com.apple.mail",
		AppName:  "Mail",
	})
	if err != nil {
		t.Errorf("expected nil when prompter allows always, got: %v", err)
	}
}

// ============================================================================
// RecordSafetyEvent — audit event emission
// ============================================================================

func TestRecordSafetyEvent_ThreeEvents(t *testing.T) {
	dir := t.TempDir()
	mock := &MockBackend{}
	ab, err := NewAuditingBackend(mock, dir, "safety-test")
	if err != nil {
		t.Fatalf("NewAuditingBackend: %v", err)
	}
	defer ab.Close()

	// Install the auditing backend as the global backend so RecordSafetyEvent picks it up.
	prevBackend := GetBackend()
	SetBackend(ab)
	t.Cleanup(func() { SetBackend(prevBackend) })

	// Emit the three safety events.
	RecordSafetyEvent("destructive_app_classified", map[string]any{
		"app":      "Mail",
		"category": "destructive",
	})
	RecordSafetyEvent("destructive_app_prompt", map[string]any{
		"app":      "Mail",
		"decision": "allow_once",
	})
	RecordSafetyEvent("destructive_app_allowed_always", map[string]any{
		"app":      "Mail",
		"category": "destructive",
	})

	// Read the JSONL file.
	time.Sleep(50 * time.Millisecond)
	records := readAuditLines(t, filepath.Join(dir, "safety-test.jsonl"))
	if len(records) != 3 {
		t.Fatalf("expected 3 safety records, got %d", len(records))
	}

	wantActions := []string{"destructive_app_classified", "destructive_app_prompt", "destructive_app_allowed_always"}
	for i, want := range wantActions {
		if records[i].Action != want {
			t.Errorf("record[%d].action: got %q, want %q", i, records[i].Action, want)
		}
	}
}

func TestRecordSafetyEvent_NonAuditBackend(t *testing.T) {
	// When the backend is a MockBackend (not auditingBackend), RecordSafetyEvent is a no-op.
	prevBackend := GetBackend()
	SetBackend(&MockBackend{})
	t.Cleanup(func() { SetBackend(prevBackend) })

	// Should not panic.
	RecordSafetyEvent("test_event", map[string]any{"key": "value"})
}

// ============================================================================
// DestructiveAppDecision.String()
// ============================================================================

func TestDestructiveAppDecision_String(t *testing.T) {
	tests := []struct {
		d    DestructiveAppDecision
		want string
	}{
		{DestructiveAppDeny, "deny"},
		{DestructiveAppAllowOnce, "allow_once"},
		{DestructiveAppAllowAlways, "allow_always"},
		{DestructiveAppDecision(99), "deny"}, // unknown → safe fallback
	}
	for _, tt := range tests {
		if got := tt.d.String(); got != tt.want {
			t.Errorf("(%v).String() = %q, want %q", tt.d, got, tt.want)
		}
	}
}
