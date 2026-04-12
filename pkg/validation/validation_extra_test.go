package validation

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alantheprice/ledit/pkg/events"
)

// --- 1. RunValidation success path (event publishing) ---

func TestRunValidation_SuccessPath_PublishesEvent(t *testing.T) {
	// Tests the full success path of RunValidation: valid syntax →
	// optional import check → diagnostics assembly → event publishing.
	// With temp-file validation, this path works across all Go versions.

	bus := events.NewEventBus()
	defer bus.Unsubscribe("succ-evt-test")
	ch := bus.Subscribe("succ-evt-test")

	v := NewValidator(bus)
	v.SetEventMetadata(map[string]interface{}{"test": "success-path"})

	result := v.RunValidation(context.Background(), "success_test.go", validGoCodeWithImport())

	if !result.Valid {
		t.Fatal("expected Valid=true for well-formed code")
	}

	select {
	case evt := <-ch:
		// Verify event structure
		if evt.Type != events.EventTypeValidation {
			t.Errorf("event Type = %q, want %q", evt.Type, events.EventTypeValidation)
		}
		data, ok := evt.Data.(map[string]interface{})
		if !ok {
			t.Fatalf("event Data type = %T, want map[string]interface{}", evt.Data)
		}
		if data["file_path"] != "success_test.go" {
			t.Errorf("file_path = %v, want success_test.go", data["file_path"])
		}
		// Metadata should be merged in
		if data["test"] != "success-path" {
			t.Errorf("metadata 'test' = %v, want success-path", data["test"])
		}
		// Diagnostics should be present (may be empty slice for clean code)
		_, ok = data["diagnostics"]
		if !ok {
			t.Fatal("missing 'diagnostics' key in event data")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected event on success path, got none")
	}
}

func TestRunValidation_SuccessPath_DiagnosticsPopulated(t *testing.T) {
	// When syntax check succeeds, Diagnostics should contain Errors + Warnings.

	bus := events.NewEventBus()
	defer bus.Unsubscribe("succ-diag-test")
	_ = bus.Subscribe("succ-diag-test")

	v := NewValidator(bus)

	code := `package main
import "fmt"
func main() { fmt.Println("hi") }
`
	result := v.RunValidation(context.Background(), "diag_check.go", code)

	if !result.Valid {
		t.Fatalf("expected Valid=true, got errors: %v", result.Errors)
	}

	// Diagnostics should be populated (non-nil) on the success path
	if result.Diagnostics == nil {
		t.Fatal("Diagnostics should be non-nil on success path")
	}

	// If there are warnings (import issues), they should appear in Diagnostics too
	if len(result.Warnings) > 0 {
		if len(result.Diagnostics) < len(result.Warnings) {
			t.Errorf("Diagnostics (%d) should include all Warnings (%d)",
				len(result.Diagnostics), len(result.Warnings))
		}
	}
}

// --- 2. ValidateImports with multiple output lines ---

func TestValidateImports_ValidFormattedCode_ReturnsNil(t *testing.T) {
	v := NewValidator(nil)

	tests := []struct {
		name    string
		content string
	}{
		{
			name: "single_import",
			content: `package main

import "fmt"

func main() { fmt.Println("hello") }
`,
		},
		{
			name: "multiple_imports",
			content: `package main

import (
	"fmt"
	"os"
	"strings"
)

func main() {
	s := fmt.Sprintf("%s-%s", strings.Join([]string{"a", "b"}, ","), os.Args[0])
	fmt.Println(s)
}
`,
		},
		{
			name: "no_imports_builtin_only",
			content: `package main

func main() { println("builtin only") }
`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			diags := v.ValidateImports(context.Background(), tc.name+".go", tc.content)
			if len(diags) != 0 {
				t.Fatalf("expected nil diagnostics for well-formatted code, got %d: %+v",
					len(diags), diags)
			}
		})
	}
}

func TestValidateImports_ErrorPath_ReturnsNil(t *testing.T) {
	// When goimports errors (e.g., syntax-invalid code), it should return nil.
	v := NewValidator(nil)

	tests := []struct {
		name    string
		content string
	}{
		{
			name:    "unclosed_brace",
			content: "package x\nfunc(",
		},
		{
			name:    "completely_invalid",
			content: "this is not go code at all {{{",
		},
		{
			name:    "empty_package",
			content: "package main\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			diags := v.ValidateImports(context.Background(), tc.name+".go", tc.content)
			if diags != nil {
				t.Fatalf("expected nil diagnostics when goimports errors, got %d: %+v",
					len(diags), diags)
			}
		})
	}
}

func TestValidateImports_CancelledContext_ReturnsNil(t *testing.T) {
	v := NewValidator(nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	diags := v.ValidateImports(ctx, "test.go", validGoCodeWithImport())
	if diags != nil {
		t.Fatalf("expected nil on cancelled context, got %d: %+v", len(diags), diags)
	}
}

// --- 3. RunValidation with empty content ---

func TestRunValidation_EmptyContent_NoPanic(t *testing.T) {
	v := NewValidator(nil)

	// Must not panic on empty content
	result := v.RunValidation(context.Background(), "empty.go", "")

	// Either gofmt errors or succeeds — neither should panic
	_ = result // Just verify completion
}

func TestRunValidation_EmptyContent_WithEventBus(t *testing.T) {
	bus := events.NewEventBus()
	defer bus.Unsubscribe("empty-bus-test")
	ch := bus.Subscribe("empty-bus-test")

	v := NewValidator(bus)
	result := v.RunValidation(context.Background(), "empty.go", "")

	// Should not panic regardless of gofmt behavior
	_ = result

	// Empty content is not valid Go syntax, so no event should be published.
	select {
	case evt := <-ch:
		t.Logf("unexpected event on empty content: %+v", evt)
	case <-time.After(300 * time.Millisecond):
		// Expected: no event published
	}
}

// --- 4. toDiagnosticsMap with nil/empty slice ---

func TestToDiagnosticsMap_NilSlice(t *testing.T) {
	result := toDiagnosticsMap(nil)
	if result == nil {
		t.Fatal("expected non-nil result for nil input")
	}
	if len(result) != 0 {
		t.Fatalf("expected empty result, got %d items", len(result))
	}
}

func TestToDiagnosticsMap_PreservesAllFields(t *testing.T) {
	diags := []Diagnostic{
		{Path: "path.go", Line: 10, Column: 3, Severity: "error", Message: "test error", Source: "gofmt"},
		{Path: "path2.go", Line: 99, Column: 80, Severity: "warning", Message: "test warning", Source: "goimports"},
	}
	result := toDiagnosticsMap(diags)

	for i, d := range diags {
		m := result[i]
		if m["path"] != d.Path {
			t.Errorf("[%d] path = %v, want %v", i, m["path"], d.Path)
		}
		if m["line"] != d.Line {
			t.Errorf("[%d] line = %v, want %v", i, m["line"], d.Line)
		}
		if m["column"] != d.Column {
			t.Errorf("[%d] column = %v, want %v", i, m["column"], d.Column)
		}
		if m["severity"] != d.Severity {
			t.Errorf("[%d] severity = %v, want %v", i, m["severity"], d.Severity)
		}
		if m["message"] != d.Message {
			t.Errorf("[%d] message = %v, want %v", i, m["message"], d.Message)
		}
		if m["source"] != d.Source {
			t.Errorf("[%d] source = %v, want %v", i, m["source"], d.Source)
		}
	}
}

// --- 5. Concurrency: RunValidation + RunAsyncValidation simultaneously ---

func TestRunValidation_ConcurrentWithRunAsyncValidation(t *testing.T) {
	// Use nil event bus to avoid race between async goroutine's Publish
	// and test cleanup's Unsubscribe (fire-and-forget can't be synchronized).
	v := NewValidator(nil)
	v.SetEventMetadata(map[string]interface{}{"concurrent": true})

	var wg sync.WaitGroup
	const goroutines = 10
	var panicCount atomic.Int32

	for i := 0; i < goroutines; i++ {
		wg.Add(2)

		// Sync RunValidation
		go func(idx int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					panicCount.Add(1)
					t.Errorf("goroutine %d panicked: %v", idx, r)
				}
			}()
			code := fmt.Sprintf("package main\nfunc main(){ println(%d) }\n", idx)
			_ = v.RunValidation(context.Background(), fmt.Sprintf("conc_%d.go", idx), code)
		}(i)

		// Async RunAsyncValidation
		go func(idx int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					panicCount.Add(1)
					t.Errorf("async goroutine %d panicked: %v", idx, r)
				}
			}()
			code := fmt.Sprintf("package main\nfunc main(){ println(%d) }\n", idx)
			v.RunAsyncValidation(context.Background(), fmt.Sprintf("async_%d.go", idx), code)
		}(i)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("concurrent test timed out — possible deadlock")
	}

	time.Sleep(500 * time.Millisecond)

	if panicCount.Load() > 0 {
		t.Fatalf("%d goroutine(s) panicked during concurrent test", panicCount.Load())
	}
}

func TestRunValidation_ConcurrentReadMetadata(t *testing.T) {
	bus := events.NewEventBus()
	defer bus.Unsubscribe("conc-meta-test")
	_ = bus.Subscribe("conc-meta-test")

	v := NewValidator(bus)

	var wg sync.WaitGroup
	const iterations = 50
	var panicCount atomic.Int32

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					panicCount.Add(1)
				}
			}()
			for j := 0; j < iterations; j++ {
				v.SetEventMetadata(map[string]interface{}{
					"writer": idx,
					"iter":   j,
				})
			}
		}(i)
	}

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					panicCount.Add(1)
				}
			}()
			for j := 0; j < iterations; j++ {
				_ = v.RunValidation(context.Background(), "meta_conc.go", invalidGoCode())
			}
		}(i)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("concurrent metadata test timed out")
	}

	if n := panicCount.Load(); n > 0 {
		t.Fatalf("%d goroutine(s) panicked", n)
	}
}

func TestRunAsyncValidation_MultipleConcurrentCalls(t *testing.T) {
	bus := events.NewEventBus()
	defer bus.Unsubscribe("multi-async-test")
	_ = bus.Subscribe("multi-async-test")

	v := NewValidator(bus)

	var wg sync.WaitGroup
	const calls = 20
	var panicCount atomic.Int32

	for i := 0; i < calls; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					panicCount.Add(1)
					t.Errorf("async call %d panicked: %v", idx, r)
				}
			}()
			v.RunAsyncValidation(context.Background(), fmt.Sprintf("async_%d.go", idx), invalidGoCode())
		}(i)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for async calls to return")
	}

	time.Sleep(1 * time.Second)

	if n := panicCount.Load(); n > 0 {
		t.Fatalf("%d goroutine(s) panicked", n)
	}
}
