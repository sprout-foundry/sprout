package agent

import (
	"context"
	"testing"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

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
