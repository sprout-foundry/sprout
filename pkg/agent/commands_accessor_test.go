package agent

import (
	"testing"
)

// TestCommandsAccessor_SetAndGet verifies that SetSlashCommands and SlashCommands
// work correctly as a getter/setter pair.
func TestCommandsAccessor_SetAndGet(t *testing.T) {
	// Set a test API key to avoid provider issues
	t.Setenv("OPENROUTER_API_KEY", "test-key")

	a, err := NewAgent()
	if err != nil {
		t.Skipf("Skipping test due to agent creation error: %v", err)
	}

	// Before setting, SlashCommands should return nil
	before := a.SlashCommands()
	if before != nil {
		t.Errorf("expected nil SlashCommands before SetSlashCommands, got %v", before)
	}

	// Create a fake registry (any non-nil value) and set it
	// We use a string as a stand-in to avoid import cycle
	// (maps can't be compared directly in Go)
	fakeRegistry := "test-registry"
	a.SetSlashCommands(fakeRegistry)

	// After setting, SlashCommands should return the same value
	after := a.SlashCommands()
	if after == nil {
		t.Fatal("expected non-nil SlashCommands after SetSlashCommands")
	}
	if after != fakeRegistry {
		t.Errorf("expected SlashCommands to return %q, got %v", fakeRegistry, after)
	}
}

// TestCommandsAccessor_NilHandling verifies that calling SlashCommands()
// before SetSlashCommands returns nil.
func TestCommandsAccessor_NilHandling(t *testing.T) {
	// Set a test API key to avoid provider issues
	t.Setenv("OPENROUTER_API_KEY", "test-key")

	a, err := NewAgent()
	if err != nil {
		t.Skipf("Skipping test due to agent creation error: %v", err)
	}

	// SlashCommands should return nil before SetSlashCommands is called
	result := a.SlashCommands()
	if result != nil {
		t.Errorf("expected nil SlashCommands before SetSlashCommands, got %v", result)
	}
}

// TestCommandsAccessor_CanSetMultipleTimes verifies that SetSlashCommands
// can be called multiple times to update the registry.
func TestCommandsAccessor_CanSetMultipleTimes(t *testing.T) {
	// Set a test API key to avoid provider issues
	t.Setenv("OPENROUTER_API_KEY", "test-key")

	a, err := NewAgent()
	if err != nil {
		t.Skipf("Skipping test due to agent creation error: %v", err)
	}

	// Set first registry
	registry1 := "registry-1"
	a.SetSlashCommands(registry1)

	first := a.SlashCommands()
	if first != registry1 {
		t.Errorf("expected first registry %q, got %v", registry1, first)
	}

	// Set second registry
	registry2 := "registry-2"
	a.SetSlashCommands(registry2)

	second := a.SlashCommands()
	if second != registry2 {
		t.Errorf("expected second registry %q, got %v", registry2, second)
	}

	// Verify they are different values
	if registry1 == registry2 {
		t.Error("expected different registry values")
	}
}

// TestCommandsAccessor_Overwrite verifies that calling SetSlashCommands
// overwrites the previous value.
func TestCommandsAccessor_Overwrite(t *testing.T) {
	// Set a test API key to avoid provider issues
	t.Setenv("OPENROUTER_API_KEY", "test-key")

	a, err := NewAgent()
	if err != nil {
		t.Skipf("Skipping test due to agent creation error: %v", err)
	}

	// Set initial value
	a.SetSlashCommands("initial")
	if got := a.SlashCommands(); got != "initial" {
		t.Errorf("expected 'initial', got %v", got)
	}

	// Overwrite with new value
	a.SetSlashCommands("overwritten")
	if got := a.SlashCommands(); got != "overwritten" {
		t.Errorf("expected 'overwritten', got %v", got)
	}
}
