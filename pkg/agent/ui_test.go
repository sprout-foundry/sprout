package agent

import (
	"context"
	"testing"
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
