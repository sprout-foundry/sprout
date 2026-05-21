package tools

import (
	"context"
	"fmt"
	"sync"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// mockHandler implements ToolHandler for testing.
type mockHandler struct {
	name string
}

func (m *mockHandler) Name() string { return m.name }

func (m *mockHandler) Definition() api.Tool { return api.Tool{} }

func (m *mockHandler) Validate(args map[string]any) error { return nil }

func (m *mockHandler) Execute(_ context.Context, _ *ToolEnv, _ map[string]any) (*ToolResult, error) {
	return nil, nil
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestRegisterAndLookup(t *testing.T) {
	r := NewToolRegistry()

	h := &mockHandler{name: "test-tool"}
	r.Register(h)

	got := r.Lookup("test-tool")
	if got != h {
		t.Errorf("Lookup(\"test-tool\") = %v; want %v", got, h)
	}

	if r.Lookup("nonexistent") != nil {
		t.Error("Lookup(\"nonexistent\") should return nil")
	}
}

func TestRegisterDuplicatePanics(t *testing.T) {
	r := NewToolRegistry()

	r.Register(&mockHandler{name: "dup"})

	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal("expected panic when registering duplicate name")
		}
	}()

	r.Register(&mockHandler{name: "dup"})
}

func TestForPersona(t *testing.T) {
	r := NewToolRegistry()

	for _, name := range []string{"alpha", "bravo", "charlie"} {
		r.Register(&mockHandler{name: name})
	}

	result := r.ForPersona([]string{"bravo", "alpha"})

	if len(result) != 2 {
		t.Fatalf("ForPersona returned %d tools; want 2", len(result))
	}

	// Should be sorted by name
	if result[0].Name() != "alpha" || result[1].Name() != "bravo" {
		t.Errorf("unexpected order: %v, %v", result[0].Name(), result[1].Name())
	}
}

func TestForPersonaEmptyAllowedReturnsAll(t *testing.T) {
	r := NewToolRegistry()

	for _, name := range []string{"x", "y", "z"} {
		r.Register(&mockHandler{name: name})
	}

	result := r.ForPersona([]string{})

	if len(result) != 3 {
		t.Fatalf("ForPersona([]) returned %d tools; want 3", len(result))
	}

	// Should also work with nil
	resultNil := r.ForPersona(nil)
	if len(resultNil) != 3 {
		t.Fatalf("ForPersona(nil) returned %d tools; want 3", len(resultNil))
	}
}

func TestAll(t *testing.T) {
	r := NewToolRegistry()

	for _, name := range []string{"c", "a", "b"} {
		r.Register(&mockHandler{name: name})
	}

	result := r.All()

	if len(result) != 3 {
		t.Fatalf("All() returned %d tools; want 3", len(result))
	}

	// Must be sorted by name
	expected := []string{"a", "b", "c"}
	for i, got := range result {
		if got.Name() != expected[i] {
			t.Errorf("All()[%d].Name() = %q; want %q", i, got.Name(), expected[i])
		}
	}
}

func TestNames(t *testing.T) {
	r := NewToolRegistry()

	for _, name := range []string{"three", "one", "two"} {
		r.Register(&mockHandler{name: name})
	}

	names := r.Names()

	if len(names) != 3 {
		t.Fatalf("Names() returned %d names; want 3", len(names))
	}

	expected := []string{"one", "three", "two"}
	for i, got := range names {
		if got != expected[i] {
			t.Errorf("Names()[%d] = %q; want %q", i, got, expected[i])
		}
	}
}

func TestConcurrentAccess(t *testing.T) {
	r := NewToolRegistry()

	var wg sync.WaitGroup

	// Concurrent registrations — each goroutine gets a unique name to avoid
	// the expected-duplicate panic and actually exercise concurrent map writes.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r.Register(&mockHandler{name: fmt.Sprintf("tool-%04d", i)})
		}(i)
	}

	// Concurrent lookups
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = r.Lookup(fmt.Sprintf("tool-%04d", i))
		}(i)
	}

	// Concurrent All/Names/ForPersona calls
	for i := 0; i < 20; i++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			_ = r.All()
		}()
		go func() {
			defer wg.Done()
			_ = r.Names()
		}()
		go func() {
			defer wg.Done()
			_ = r.ForPersona([]string{"tool-0001"})
		}()
	}

	wg.Wait()

	// Verify all 50 registrations succeeded.
	if got := len(r.All()); got != 50 {
		t.Errorf("expected 50 tools after concurrent registration, got %d", got)
	}
}
