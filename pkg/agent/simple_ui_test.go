package agent

import (
	"context"
	"testing"
)

func TestNewSimpleUI(t *testing.T) {
	ui := NewSimpleUI()
	if ui == nil {
		t.Fatal("NewSimpleUI() returned nil")
	}
}

func TestSimpleUIIsInteractive(t *testing.T) {
	ui := NewSimpleUI()
	if ui.IsInteractive() {
		t.Error("IsInteractive() = true, want false")
	}
}

func TestSimpleUIShowDropdown(t *testing.T) {
	ui := NewSimpleUI()
	ctx := context.Background()
	result, err := ui.ShowDropdown(ctx, nil, DropdownOptions{})
	if err != ErrUINotAvailable {
		t.Errorf("ShowDropdown error = %v, want %v", err, ErrUINotAvailable)
	}
	if result != nil {
		t.Errorf("ShowDropdown result = %v, want nil", result)
	}
}

func TestSimpleUIShowQuickPrompt(t *testing.T) {
	ui := NewSimpleUI()
	ctx := context.Background()
	result, err := ui.ShowQuickPrompt(ctx, "", nil, false)
	if err != ErrUINotAvailable {
		t.Errorf("ShowQuickPrompt error = %v, want %v", err, ErrUINotAvailable)
	}
	if result != (QuickOption{}) {
		t.Errorf("ShowQuickPrompt result = %+v, want empty QuickOption", result)
	}
}
