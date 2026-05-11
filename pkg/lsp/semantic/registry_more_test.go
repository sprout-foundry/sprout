package semantic

import (
	"testing"
	"time"
)

// Tests for Register, RegisterAliases, nil-receiver safety, factory mode, etc.

func TestRegistryRegisterFactory(t *testing.T) {
	r := NewRegistry()
	r.Register("go", NewGoAdapter)

	adapter, ok := r.AdapterForLanguage("go")
	if !ok {
		t.Fatal("expected adapter to be registered")
	}
	if adapter == nil {
		t.Fatal("expected non-nil adapter")
	}
}

func TestRegistryRegisterEmptyID(t *testing.T) {
	r := NewRegistry()
	r.Register("", func() Adapter { return goAdapter{} })
	r.Register("  ", func() Adapter { return goAdapter{} })

	_, ok := r.AdapterForLanguage("")
	if ok {
		t.Error("empty ID should not be registered")
	}
}

func TestRegistryRegisterNilFactory(t *testing.T) {
	r := NewRegistry()
	r.Register("go", nil)

	_, ok := r.AdapterForLanguage("go")
	if ok {
		t.Error("nil factory should not register")
	}
}

func TestRegistryNilReceiver(t *testing.T) {
	var r *Registry

	r.Register("go", func() Adapter { return goAdapter{} })
	r.RegisterSingleton(goAdapter{}, "go")
	_, ok := r.AdapterForLanguage("go")
	if ok {
		t.Error("nil registry should not find adapters")
	}
}

func TestRegistryRegisterAliases(t *testing.T) {
	r := NewRegistry()
	r.RegisterAliases(func() Adapter { return goAdapter{} }, "go", "golang")

	for _, lang := range []string{"go", "golang"} {
		_, ok := r.AdapterForLanguage(lang)
		if !ok {
			t.Errorf("expected %q to be registered", lang)
		}
	}
}

func TestRegistryCaseInsensitive(t *testing.T) {
	r := NewRegistry()
	r.Register("Go", func() Adapter { return goAdapter{} })

	_, ok := r.AdapterForLanguage("go")
	if !ok {
		t.Error("expected case-insensitive lookup")
	}

	_, ok = r.AdapterForLanguage("GO")
	if !ok {
		t.Error("expected case-insensitive lookup")
	}
}

func TestRegistryWhitespaceTrimming(t *testing.T) {
	r := NewRegistry()
	r.Register("  go  ", func() Adapter { return goAdapter{} })

	_, ok := r.AdapterForLanguage("go")
	if !ok {
		t.Error("expected whitespace trimming on lookup")
	}

	_, ok = r.AdapterForLanguage("  go  ")
	if !ok {
		t.Error("expected whitespace trimming on register")
	}
}

func TestRegistryUnregisteredLanguage(t *testing.T) {
	r := NewRegistry()
	_, ok := r.AdapterForLanguage("python")
	if ok {
		t.Error("expected false for unregistered language")
	}
}

func TestRegistryFactoryCreatesNewInstance(t *testing.T) {
	r := NewRegistry()
	callCount := 0
	r.Register("test", func() Adapter {
		callCount++
		return &countingAdapter{}
	})

	_, _ = r.AdapterForLanguage("test")
	_, _ = r.AdapterForLanguage("test")

	if callCount != 2 {
		t.Errorf("expected factory to be called twice, got %d", callCount)
	}
}

func TestRegistrySingletonPrecedenceOverFactory(t *testing.T) {
	r := NewRegistry()
	r.Register("go", func() Adapter { return goAdapter{} })

	singleton := &countingAdapter{}
	r.RegisterSingleton(singleton, "go")

	adapter, ok := r.AdapterForLanguage("go")
	if !ok {
		t.Fatal("expected adapter")
	}
	_, _ = adapter.Run(ToolInput{})
	_, _ = adapter.Run(ToolInput{})

	if singleton.runCount != 2 {
		t.Errorf("expected singleton to be used (runCount=2), got %d", singleton.runCount)
	}
}

func TestRegistryRegisterSingletonNilAdapter(t *testing.T) {
	r := NewRegistry()
	r.RegisterSingleton(nil, "go")

	_, ok := r.AdapterForLanguage("go")
	if ok {
		t.Error("nil adapter should not register")
	}
}

func TestRegistryRegisterSingletonEmptyIDs(t *testing.T) {
	r := NewRegistry()
	r.RegisterSingleton(goAdapter{}, "", "  ")

	_, ok := r.AdapterForLanguage("")
	if ok {
		t.Error("empty ID should not register")
	}
}

func TestRegistryTimedAdapterStamps(t *testing.T) {
	r := NewRegistry()
	r.Register("test", func() Adapter { return &countingAdapter{sleepFor: time.Millisecond} })

	adapter, ok := r.AdapterForLanguage("test")
	if !ok {
		t.Fatal("expected adapter")
	}

	result, err := adapter.Run(ToolInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.DurationMs <= 0 {
		t.Errorf("expected DurationMs > 0, got %d", result.DurationMs)
	}
}
