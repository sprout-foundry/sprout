package agent

import (
	"testing"
	"time"
)

func TestSessionItem_Fields(t *testing.T) {
	now := time.Now()
	item := SessionItem{
		Label:       "Test Session",
		Value:       "session-123",
		SessionID:   "session-123",
		Model:       "claude-sonnet-4",
		LastUpdated: now,
		Name:        "My Test Session",
	}

	if item.Label != "Test Session" {
		t.Errorf("expected label 'Test Session', got %q", item.Label)
	}
	if item.Value != "session-123" {
		t.Errorf("expected value 'session-123', got %q", item.Value)
	}
	if item.SessionID != "session-123" {
		t.Errorf("expected session ID 'session-123', got %q", item.SessionID)
	}
	if item.Model != "claude-sonnet-4" {
		t.Errorf("expected model 'claude-sonnet-4', got %q", item.Model)
	}
	if !item.LastUpdated.Equal(now) {
		t.Errorf("expected LastUpdated %v, got %v", now, item.LastUpdated)
	}
	if item.Name != "My Test Session" {
		t.Errorf("expected name 'My Test Session', got %q", item.Name)
	}
}

func TestModelItem_Fields(t *testing.T) {
	item := ModelItem{
		Label:         "Claude Sonnet 4",
		Value:         "claude-sonnet-4",
		Provider:      "anthropic",
		Model:         "claude-sonnet-4-20250514",
		InputCost:     0.003,
		OutputCost:    0.015,
		LegacyCost:    0.003,
		ContextLength: 200000,
		Tags:          []string{"reasoning", "fast"},
	}

	if item.Label != "Claude Sonnet 4" {
		t.Errorf("expected label 'Claude Sonnet 4', got %q", item.Label)
	}
	if item.Value != "claude-sonnet-4" {
		t.Errorf("expected value 'claude-sonnet-4', got %q", item.Value)
	}
	if item.Provider != "anthropic" {
		t.Errorf("expected provider 'anthropic', got %q", item.Provider)
	}
	if item.Model != "claude-sonnet-4-20250514" {
		t.Errorf("expected model 'claude-sonnet-4-20250514', got %q", item.Model)
	}
	if item.InputCost != 0.003 {
		t.Errorf("expected input cost 0.003, got %f", item.InputCost)
	}
	if item.OutputCost != 0.015 {
		t.Errorf("expected output cost 0.015, got %f", item.OutputCost)
	}
	if item.ContextLength != 200000 {
		t.Errorf("expected context length 200000, got %d", item.ContextLength)
	}
	if len(item.Tags) != 2 {
		t.Errorf("expected 2 tags, got %d", len(item.Tags))
	}
}

func TestModelItem_ZeroValues(t *testing.T) {
	item := ModelItem{}
	if item.Label != "" {
		t.Error("expected empty label for zero value")
	}
	if item.Provider != "" {
		t.Error("expected empty provider for zero value")
	}
	if item.InputCost != 0 {
		t.Error("expected zero input cost for zero value")
	}
	if item.Tags != nil {
		t.Error("expected nil tags for zero value")
	}
}

func TestDropdownOptions_Fields(t *testing.T) {
	opts := DropdownOptions{
		Prompt:       "Select a session",
		SearchPrompt: "Search sessions...",
		ShowCounts:   true,
	}

	if opts.Prompt != "Select a session" {
		t.Errorf("expected prompt 'Select a session', got %q", opts.Prompt)
	}
	if opts.SearchPrompt != "Search sessions..." {
		t.Errorf("expected search prompt 'Search sessions...', got %q", opts.SearchPrompt)
	}
	if !opts.ShowCounts {
		t.Error("expected ShowCounts to be true")
	}
}

func TestQuickOption_Fields(t *testing.T) {
	opt := QuickOption{
		Label: "Option A",
		Value: "a",
	}

	if opt.Label != "Option A" {
		t.Errorf("expected label 'Option A', got %q", opt.Label)
	}
	if opt.Value != "a" {
		t.Errorf("expected value 'a', got %q", opt.Value)
	}
}

func TestDropdownItem_Fields(t *testing.T) {
	item := DropdownItem{
		Label: "Dropdown Option",
		Value: "dropdown-val",
	}

	if item.Label != "Dropdown Option" {
		t.Errorf("expected label 'Dropdown Option', got %q", item.Label)
	}
	if item.Value != "dropdown-val" {
		t.Errorf("expected value 'dropdown-val', got %q", item.Value)
	}
}

func TestErrUINotAvailable(t *testing.T) {
	if ErrUINotAvailable == nil {
		t.Fatal("ErrUINotAvailable should not be nil")
	}
	if ErrUINotAvailable.Error() != "UI not available" {
		t.Errorf("expected error message 'UI not available', got %q", ErrUINotAvailable.Error())
	}
}

func TestErrCancelled(t *testing.T) {
	if ErrCancelled == nil {
		t.Fatal("ErrCancelled should not be nil")
	}
	if ErrCancelled.Error() != "user cancelled" {
		t.Errorf("expected error message 'user cancelled', got %q", ErrCancelled.Error())
	}
}

func TestPublishModel(t *testing.T) {
	// PublishModel is a placeholder that just prints; verify it doesn't panic
	PublishModel("test-model")
	PublishModel("")
	PublishModel("model-with-dashes-123")
}
