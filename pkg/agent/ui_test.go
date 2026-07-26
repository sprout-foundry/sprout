package agent

import (
	"context"
	"testing"
	"time"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

type testUIMock struct {
	interactive       bool
	dropdownResult    interface{}
	dropdownErr       error
	promptResult      QuickOption
	promptErr         error
	dropdownCallCount int
	promptCallCount   int
	lastPrompt        string
	lastHorizontal    bool
}

func (m *testUIMock) ShowDropdown(ctx context.Context, items interface{}, opts DropdownOptions) (interface{}, error) {
	m.dropdownCallCount++
	return m.dropdownResult, m.dropdownErr
}

func (m *testUIMock) ShowQuickPrompt(ctx context.Context, prompt string, options []QuickOption, horizontal bool) (QuickOption, error) {
	m.promptCallCount++
	m.lastPrompt = prompt
	m.lastHorizontal = horizontal
	return m.promptResult, m.promptErr
}

func (m *testUIMock) IsInteractive() bool {
	return m.interactive
}

func TestSetUI(t *testing.T) {
	a := newTestAgent(t)
	defer a.Shutdown()

	t.Run("sets the ui field", func(t *testing.T) {
		mock := &testUIMock{interactive: true}
		a.SetUI(mock)
		if a.ui == nil {
			t.Fatal("expected non-nil ui after SetUI")
		}
		if a.ui != mock {
			t.Error("SetUI should store the exact UI instance")
		}
	})

	t.Run("with nil does not crash", func(t *testing.T) {
		a.SetUI(nil)
		if a.ui != nil {
			t.Error("SetUI(nil) should set ui to nil")
		}
	})

	t.Run("replacing existing UI", func(t *testing.T) {
		mock1 := &testUIMock{interactive: true}
		mock2 := &testUIMock{interactive: false}
		a.SetUI(mock1)
		a.SetUI(mock2)
		if a.ui != mock2 {
			t.Error("SetUI should replace existing UI")
		}
	})
}

func TestShowDropdown(t *testing.T) {
	t.Run("with nil UI returns ErrUINotAvailable", func(t *testing.T) {
		a := newTestAgent(t)
		defer a.Shutdown()
		a.ui = nil

		_, err := a.ShowDropdown(nil, DropdownOptions{})
		if err != ErrUINotAvailable {
			t.Errorf("got %v, want ErrUINotAvailable", err)
		}
	})

	t.Run("with non-interactive UI returns ErrUINotAvailable", func(t *testing.T) {
		a := newTestAgent(t)
		defer a.Shutdown()
		a.ui = &testUIMock{interactive: false}

		_, err := a.ShowDropdown(nil, DropdownOptions{})
		if err != ErrUINotAvailable {
			t.Errorf("got %v, want ErrUINotAvailable", err)
		}
	})

	t.Run("with interactive mock UI delegates to UI", func(t *testing.T) {
		a := newTestAgent(t)
		defer a.Shutdown()
		mock := &testUIMock{
			interactive:    true,
			dropdownResult: "selected_item",
		}
		a.SetUI(mock)

		result, err := a.ShowDropdown(nil, DropdownOptions{Prompt: "Pick one"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.(string) != "selected_item" {
			t.Errorf("got %v, want selected_item", result)
		}
		if mock.dropdownCallCount != 1 {
			t.Errorf("expected 1 call, got %d", mock.dropdownCallCount)
		}
	})

	t.Run("with interactive UI that returns error", func(t *testing.T) {
		a := newTestAgent(t)
		defer a.Shutdown()
		mock := &testUIMock{
			interactive: true,
			dropdownErr: context.Canceled,
		}
		a.SetUI(mock)

		_, err := a.ShowDropdown(nil, DropdownOptions{})
		if err != context.Canceled {
			t.Errorf("got %v, want context.Canceled", err)
		}
	})
}

func TestShowQuickPrompt(t *testing.T) {
	t.Run("with nil UI returns ErrUINotAvailable", func(t *testing.T) {
		a := newTestAgent(t)
		defer a.Shutdown()
		a.ui = nil

		result, err := a.ShowQuickPrompt("prompt", nil, false)
		if err != ErrUINotAvailable {
			t.Errorf("got %v, want ErrUINotAvailable", err)
		}
		if result != (QuickOption{}) {
			t.Errorf("got %v, want empty QuickOption", result)
		}
	})

	t.Run("with non-interactive UI returns ErrUINotAvailable", func(t *testing.T) {
		a := newTestAgent(t)
		defer a.Shutdown()
		a.ui = &testUIMock{interactive: false}

		result, err := a.ShowQuickPrompt("prompt", nil, false)
		if err != ErrUINotAvailable {
			t.Errorf("got %v, want ErrUINotAvailable", err)
		}
		if result != (QuickOption{}) {
			t.Errorf("got %v, want empty QuickOption", result)
		}
	})

	t.Run("with interactive mock UI delegates to UI", func(t *testing.T) {
		a := newTestAgent(t)
		defer a.Shutdown()
		mock := &testUIMock{
			interactive:  true,
			promptResult: QuickOption{Label: "Yes", Value: "y"},
		}
		a.SetUI(mock)

		result, err := a.ShowQuickPrompt("Do it?", []QuickOption{{Label: "Yes", Value: "y"}}, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Label != "Yes" || result.Value != "y" {
			t.Errorf("got %+v, want {Label: Yes, Value: y}", result)
		}
		if mock.promptCallCount != 1 {
			t.Errorf("expected 1 call, got %d", mock.promptCallCount)
		}
		if mock.lastPrompt != "Do it?" {
			t.Errorf("mock received prompt %q, want 'Do it?'", mock.lastPrompt)
		}
		if !mock.lastHorizontal {
			t.Error("mock should have received horizontal=true")
		}
	})

	t.Run("with interactive UI that returns error", func(t *testing.T) {
		a := newTestAgent(t)
		defer a.Shutdown()
		mock := &testUIMock{
			interactive: true,
			promptErr:   context.Canceled,
		}
		a.SetUI(mock)

		_, err := a.ShowQuickPrompt("prompt", nil, false)
		if err != context.Canceled {
			t.Errorf("got %v, want context.Canceled", err)
		}
	})
}

// mockUI implements the UI interface for testing
type mockUI struct {
	interactive   bool
	quickPromptFn func(ctx context.Context, prompt string, options []QuickOption, horizontal bool) (QuickOption, error)
	dropdownFn    func(ctx context.Context, items interface{}, options DropdownOptions) (interface{}, error)
}

func (m *mockUI) IsInteractive() bool {
	return m.interactive
}

func (m *mockUI) ShowQuickPrompt(ctx context.Context, prompt string, options []QuickOption, horizontal bool) (QuickOption, error) {
	if m.quickPromptFn != nil {
		return m.quickPromptFn(ctx, prompt, options, horizontal)
	}
	return QuickOption{}, ErrUINotAvailable
}

func (m *mockUI) ShowDropdown(ctx context.Context, items interface{}, options DropdownOptions) (interface{}, error) {
	if m.dropdownFn != nil {
		return m.dropdownFn(ctx, items, options)
	}
	return nil, ErrUINotAvailable
}

func TestPromptChoice_UINotAvailable(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()
	// No UI set
	_, err := a.PromptChoice("test prompt", []ChoiceOption{
		{Label: "A", Value: "a"},
	})
	if err != ErrUINotAvailable {
		t.Errorf("expected ErrUINotAvailable, got %v", err)
	}
}

func TestPromptChoice_UINotInteractive(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()
	a.ui = &mockUI{interactive: false}

	_, err := a.PromptChoice("test prompt", []ChoiceOption{
		{Label: "A", Value: "a"},
	})
	if err != ErrUINotAvailable {
		t.Errorf("expected ErrUINotAvailable, got %v", err)
	}
}

func TestPromptChoice_QuickPromptSuccess(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()
	a.ui = &mockUI{
		interactive: true,
		quickPromptFn: func(ctx context.Context, prompt string, options []QuickOption, horizontal bool) (QuickOption, error) {
			if prompt != "Select an option" {
				t.Errorf("expected prompt 'Select an option', got %q", prompt)
			}
			if len(options) != 2 {
				t.Fatalf("expected 2 options, got %d", len(options))
			}
			if options[0].Label != "Option A" || options[0].Value != "a" {
				t.Errorf("unexpected first option: %+v", options[0])
			}
			if options[1].Label != "Option B" || options[1].Value != "b" {
				t.Errorf("unexpected second option: %+v", options[1])
			}
			return QuickOption{Label: "Option B", Value: "b"}, nil
		},
	}

	result, err := a.PromptChoice("Select an option", []ChoiceOption{
		{Label: "Option A", Value: "a"},
		{Label: "Option B", Value: "b"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "b" {
		t.Errorf("expected value 'b', got %q", result)
	}
}

func TestPromptChoice_QuickPromptError_FallbackToDropdown(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()
	a.ui = &mockUI{
		interactive: true,
		quickPromptFn: func(ctx context.Context, prompt string, options []QuickOption, horizontal bool) (QuickOption, error) {
			return QuickOption{}, ErrUINotAvailable // force fallback
		},
		dropdownFn: func(ctx context.Context, items interface{}, options DropdownOptions) (interface{}, error) {
			if options.Prompt != "Select an option" {
				t.Errorf("expected prompt 'Select an option', got %q", options.Prompt)
			}
			dropdownItems := items.([]DropdownItem)
			if len(dropdownItems) != 2 {
				t.Fatalf("expected 2 dropdown items, got %d", len(dropdownItems))
			}
			if dropdownItems[0].Label != "Option A" {
				t.Errorf("expected first item 'Option A', got %q", dropdownItems[0].Label)
			}
			return DropdownItem{Label: "Option A", Value: "a"}, nil
		},
	}

	result, err := a.PromptChoice("Select an option", []ChoiceOption{
		{Label: "Option A", Value: "a"},
		{Label: "Option B", Value: "b"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "a" {
		t.Errorf("expected value 'a', got %q", result)
	}
}

func TestPromptChoice_DropdownError(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()
	a.ui = &mockUI{
		interactive: true,
		quickPromptFn: func(ctx context.Context, prompt string, options []QuickOption, horizontal bool) (QuickOption, error) {
			return QuickOption{}, ErrUINotAvailable // force fallback
		},
		dropdownFn: func(ctx context.Context, items interface{}, options DropdownOptions) (interface{}, error) {
			return nil, ErrCancelled
		},
	}

	_, err := a.PromptChoice("Select an option", []ChoiceOption{
		{Label: "A", Value: "a"},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// Should be wrapped as transient error
	if !agenterrors.IsTransient(err) {
		t.Errorf("expected transient error, got %v", err)
	}
}

func TestChoiceDropdownItem_Adapter(t *testing.T) {
	opt := ChoiceOption{Label: "My Label", Value: "my-value"}
	item := choiceDropdownItem{opt: opt}

	if item.Display() != "My Label" {
		t.Errorf("expected Display() 'My Label', got %q", item.Display())
	}
	if item.SearchText() != "My Label" {
		t.Errorf("expected SearchText() 'My Label', got %q", item.SearchText())
	}
	val := item.Value().(string)
	if val != "my-value" {
		t.Errorf("expected Value() 'my-value', got %q", val)
	}
}

func TestChoiceDropdownItem_EmptyValues(t *testing.T) {
	opt := ChoiceOption{}
	item := choiceDropdownItem{opt: opt}

	if item.Display() != "" {
		t.Errorf("expected empty Display(), got %q", item.Display())
	}
	if item.SearchText() != "" {
		t.Errorf("expected empty SearchText(), got %q", item.SearchText())
	}
	val := item.Value().(string)
	if val != "" {
		t.Errorf("expected empty Value(), got %q", val)
	}
}

func TestPromptChoice_EmptyChoices(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()
	a.ui = &mockUI{
		interactive: true,
		quickPromptFn: func(ctx context.Context, prompt string, options []QuickOption, horizontal bool) (QuickOption, error) {
			if len(options) != 0 {
				t.Errorf("expected 0 options, got %d", len(options))
			}
			return QuickOption{Value: ""}, nil
		},
	}

	result, err := a.PromptChoice("Select", []ChoiceOption{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "" {
		t.Errorf("expected empty result, got %q", result)
	}
}

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

// TestSessionItem2 tests SessionItem creation
func TestSessionItem2(t *testing.T) {
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

// TestModelItem2 tests ModelItem creation
func TestModelItem2(t *testing.T) {
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
	if item.InputCost != 0.003 {
		t.Errorf("expected input cost 0.003, got %f", item.InputCost)
	}
	if item.ContextLength != 200000 {
		t.Errorf("expected context length 200000, got %d", item.ContextLength)
	}
}

// TestModelItemZeroValues2 tests zero value ModelItem
func TestModelItemZeroValues2(t *testing.T) {
	item := ModelItem{}
	if item.Label != "" {
		t.Error("expected empty label for zero value")
	}
	if item.Provider != "" {
		t.Error("expected empty provider for zero value")
	}
	if item.Tags != nil {
		t.Error("expected nil tags for zero value")
	}
}

// TestDropdownOptions2 tests DropdownOptions creation
func TestDropdownOptions2(t *testing.T) {
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

// TestQuickOption2 tests QuickOption creation
func TestQuickOption2(t *testing.T) {
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

// TestDropdownItem2 tests DropdownItem creation
func TestDropdownItem2(t *testing.T) {
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

// TestErrUINotAvailable2 tests error value
func TestErrUINotAvailable2(t *testing.T) {
	if ErrUINotAvailable == nil {
		t.Fatal("ErrUINotAvailable should not be nil")
	}
	if ErrUINotAvailable.Error() != "UI not available" {
		t.Errorf("expected error message 'UI not available', got %q", ErrUINotAvailable.Error())
	}
}

// TestErrCancelled2 tests error value
func TestErrCancelled2(t *testing.T) {
	if ErrCancelled == nil {
		t.Fatal("ErrCancelled should not be nil")
	}
	if ErrCancelled.Error() != "user cancelled" {
		t.Errorf("expected error message 'user cancelled', got %q", ErrCancelled.Error())
	}
}

// TestPublishModel2 tests PublishModel doesn't panic
func TestPublishModel2(t *testing.T) {
	// PublishModel is a placeholder that just prints
	PublishModel("test-model")
	PublishModel("")
	PublishModel("model-with-dashes-123")
}
