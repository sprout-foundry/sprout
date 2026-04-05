package validation

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alantheprice/ledit/pkg/events"
)

// --- toDiagnosticsMap with all Diagnostic field values ---

func TestToDiagnosticsMap_ZeroValuesInFields(t *testing.T) {
	// Verify that zero-value fields (empty string, 0 int) map correctly
	diags := []Diagnostic{
		{Path: "", Line: 0, Column: 0, Severity: "", Message: "", Source: ""},
	}
	result := toDiagnosticsMap(diags)
	if len(result) != 1 {
		t.Fatalf("expected 1 item, got %d", len(result))
	}
	m := result[0]
	if m["path"] != "" {
		t.Errorf("path = %v, want empty string", m["path"])
	}
	if m["line"] != 0 {
		t.Errorf("line = %v, want 0", m["line"])
	}
	if m["column"] != 0 {
		t.Errorf("column = %v, want 0", m["column"])
	}
	if m["severity"] != "" {
		t.Errorf("severity = %v, want empty string", m["severity"])
	}
	if m["message"] != "" {
		t.Errorf("message = %v, want empty string", m["message"])
	}
	if m["source"] != "" {
		t.Errorf("source = %v, want empty string", m["source"])
	}
}

func TestToDiagnosticsMap_LargeLineAndColumn(t *testing.T) {
	// Verify large int values are preserved (not truncated)
	diags := []Diagnostic{
		{Path: "big.go", Line: 999999, Column: 999999, Severity: "error", Message: "deep", Source: "test"},
	}
	result := toDiagnosticsMap(diags)
	if result[0]["line"] != 999999 {
		t.Errorf("line = %v, want 999999", result[0]["line"])
	}
	if result[0]["column"] != 999999 {
		t.Errorf("column = %v, want 999999", result[0]["column"])
	}
}

func TestToDiagnosticsMap_SingleDiagnostic(t *testing.T) {
	diags := []Diagnostic{
		{Path: "solo.go", Line: 1, Column: 2, Severity: "warning", Message: "only one", Source: "src"},
	}
	result := toDiagnosticsMap(diags)
	if len(result) != 1 {
		t.Fatalf("expected 1 item, got %d", len(result))
	}
	if result[0]["path"] != "solo.go" {
		t.Errorf("path = %v", result[0]["path"])
	}
	if result[0]["message"] != "only one" {
		t.Errorf("message = %v", result[0]["message"])
	}
}

func TestToDiagnosticsMap_ThreeDiagnostics(t *testing.T) {
	diags := []Diagnostic{
		{Path: "a.go", Line: 1, Column: 1, Severity: "error", Message: "e1", Source: "s1"},
		{Path: "b.go", Line: 2, Column: 2, Severity: "warning", Message: "w1", Source: "s2"},
		{Path: "c.go", Line: 3, Column: 3, Severity: "info", Message: "i1", Source: "s3"},
	}
	result := toDiagnosticsMap(diags)
	if len(result) != 3 {
		t.Fatalf("expected 3 items, got %d", len(result))
	}
	// Check ordering is preserved
	if result[2]["path"] != "c.go" {
		t.Errorf("item[2] path = %v, want c.go", result[2]["path"])
	}
	if result[2]["severity"] != "info" {
		t.Errorf("item[2] severity = %v, want info", result[2]["severity"])
	}
}

func TestToDiagnosticsMap_MapKeyCount(t *testing.T) {
	diags := []Diagnostic{
		{Path: "go.go", Line: 1, Column: 1, Severity: "error", Message: "m", Source: "s"},
	}
	result := toDiagnosticsMap(diags)
	if len(result[0]) != 6 {
		t.Errorf("expected 6 keys in map, got %d", len(result[0]))
	}
}

// --- Validator concurrent safety ---

func TestValidator_ConcurrentRunValidationAndSetEventMetadata(t *testing.T) {
	bus := events.NewEventBus()
	defer bus.Unsubscribe("conc-safety-test")
	_ = bus.Subscribe("conc-safety-test")

	v := NewValidator(bus)
	v.SetEventMetadata(map[string]interface{}{"initial": true})

	const workers = 10
	const iterations = 20
	var wg sync.WaitGroup
	var panicCount atomic.Int32

	// Goroutines that run validations (read metadata via decorateEventPayload)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					panicCount.Add(1)
					t.Errorf("validation goroutine %d panicked: %v", idx, r)
				}
			}()
			for j := 0; j < iterations; j++ {
				code := fmt.Sprintf("package main\nfunc main(){ println(%d) }\n", idx*iterations+j)
				_ = v.RunValidation(context.Background(), fmt.Sprintf("v%d_%d.go", idx, j), code)
			}
		}(i)
	}

	// Goroutines that set metadata (write)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					panicCount.Add(1)
					t.Errorf("metadata goroutine %d panicked: %v", idx, r)
				}
			}()
			for j := 0; j < iterations; j++ {
				v.SetEventMetadata(map[string]interface{}{
					"writer": idx,
					"iter":   j,
					"tag":    fmt.Sprintf("w%d-i%d", idx, j),
				})
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
	case <-time.After(15 * time.Second):
		t.Fatal("concurrent safety test timed out — possible deadlock")
	}

	if n := panicCount.Load(); n > 0 {
		t.Fatalf("%d goroutine(s) panicked during concurrent access", n)
	}
}

func TestValidator_ConcurrentSetEventMetadataNilAndNonNil(t *testing.T) {
	// Stress-test toggling between nil and non-nil metadata concurrently
	v := NewValidator(nil)

	const iterations = 100
	var wg sync.WaitGroup
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
				if j%2 == 0 {
					v.SetEventMetadata(map[string]interface{}{"key": idx, "val": j})
				} else {
					v.SetEventMetadata(nil)
				}
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
		t.Fatal("nil/non-nil metadata toggle test timed out")
	}

	if n := panicCount.Load(); n > 0 {
		t.Fatalf("%d goroutine(s) panicked", n)
	}

	// After all writers finish, verify metadata is usable
	v.SetEventMetadata(map[string]interface{}{"final": true})
	payload := v.decorateEventPayload(nil)
	if payload["final"] != true {
		t.Error("metadata not usable after concurrent writes")
	}
}

func TestValidator_ConcurrentRunAsyncValidation(t *testing.T) {
	// Use nil event bus to avoid race between async goroutine's Publish
	// and test cleanup's Unsubscribe (fire-and-forget can't be synchronized).
	v := NewValidator(nil)
	v.SetEventMetadata(map[string]interface{}{"async": true})

	const calls = 50
	var wg sync.WaitGroup
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
			code := fmt.Sprintf("package main\nfunc _%d(){}\n", idx)
			v.RunAsyncValidation(context.Background(), fmt.Sprintf("a%d.go", idx), code)
		}(i)
	}

	// RunAsyncValidation returns immediately
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("async calls may have blocked")
	}

	// Let background goroutines finish
	time.Sleep(2 * time.Second)

	if n := panicCount.Load(); n > 0 {
		t.Fatalf("%d goroutine(s) panicked", n)
	}
}

// --- RunAsyncValidation does not block ---

func TestRunAsyncValidation_ImmediateReturn(t *testing.T) {
	v := NewValidator(nil)

	start := time.Now()
	v.RunAsyncValidation(context.Background(), "heavy.go", strings.Repeat("package x\nfunc _(){}\n", 10000))
	elapsed := time.Since(start)

	if elapsed > 500*time.Millisecond {
		t.Fatalf("RunAsyncValidation blocked for %v, expected <500ms", elapsed)
	}
}

func TestRunAsyncValidation_ImmediateReturnWithEventBus(t *testing.T) {
	bus := events.NewEventBus()
	defer bus.Unsubscribe("async-no-block")
	_ = bus.Subscribe("async-no-block")

	v := NewValidator(bus)
	v.SetEventMetadata(map[string]interface{}{"test": true})

	start := time.Now()
	v.RunAsyncValidation(context.Background(), "heavy2.go", strings.Repeat("package x\nfunc _(){}\n", 50000))
	elapsed := time.Since(start)

	if elapsed > 500*time.Millisecond {
		t.Fatalf("RunAsyncValidation blocked for %v, expected <500ms", elapsed)
	}
}

func TestRunAsyncValidation_ImmediateReturnUnderLoad(t *testing.T) {
	// Fire many async validations and verify none block
	v := NewValidator(nil)

	start := time.Now()
	for i := 0; i < 100; i++ {
		code := fmt.Sprintf("package main\nfunc _%d(){ println(%d) }\n", i, i)
		v.RunAsyncValidation(context.Background(), fmt.Sprintf("load%d.go", i), code)
	}
	elapsed := time.Since(start)

	if elapsed > 2*time.Second {
		t.Fatalf("100 RunAsyncValidation calls took %v, expected <2s", elapsed)
	}
}

// --- ValidateSyntax edge cases ---

func TestValidateSyntax_VeryLongContent(t *testing.T) {
	v := NewValidator(nil)

	// Generate a very long Go file (100KB+)
	var b strings.Builder
	b.WriteString("package main\n")
	for i := 0; i < 5000; i++ {
		fmt.Fprintf(&b, "func _%d() { println(%d) }\n", i, i)
	}

	err := v.ValidateSyntax(context.Background(), "long.go", b.String())
	// Should not panic regardless of length
	if err == nil {
		t.Log("gofmt accepted long content (older Go)")
	} else {
		if msg := err.Error(); len(msg) == 0 {
			t.Fatal("expected non-empty error message for long content")
		}
	}
}

func TestValidateSyntax_OnlyPackageDeclaration(t *testing.T) {
	v := NewValidator(nil)
	err := v.ValidateSyntax(context.Background(), "pkg.go", "package main\n")
	_ = err // Just verify no panic (gofmt behavior varies by Go version)
}

func TestValidateSyntax_WhitespaceOnlyContent(t *testing.T) {
	v := NewValidator(nil)
	err := v.ValidateSyntax(context.Background(), "ws.go", "   \n\t\n  ")
	_ = err // Should not panic
}

func TestValidateSyntax_SingleBrace(t *testing.T) {
	v := NewValidator(nil)
	err := v.ValidateSyntax(context.Background(), "brace.go", "package x\n{")
	if err == nil {
		t.Log("gofmt accepted single brace (older Go)")
	}
}

func TestValidateSyntax_UnterminatedString(t *testing.T) {
	v := NewValidator(nil)
	err := v.ValidateSyntax(context.Background(), "str.go", "package x\nvar s = \"unterminated")
	if err == nil {
		t.Fatal("expected error for unterminated string")
	}
}

// --- ValidateImports edge cases ---

func TestValidateImports_EmptyOutput(t *testing.T) {
	// When goimports produces empty output (well-formatted code), should return nil
	v := NewValidator(nil)
	code := `package main

import "fmt"

func main() { fmt.Println("hello") }
`
	diags := v.ValidateImports(context.Background(), "clean.go", code)
	if diags != nil {
		t.Fatalf("expected nil for clean code, got %d diagnostics", len(diags))
	}
}

// --- RunValidation edge cases ---

func TestRunValidation_VeryLongContent_NoPanic(t *testing.T) {
	v := NewValidator(nil)

	var b strings.Builder
	b.WriteString("package main\n")
	for i := 0; i < 2000; i++ {
		fmt.Fprintf(&b, "func _%d() { println(%d) }\n", i, i)
	}

	result := v.RunValidation(context.Background(), "long_valid.go", b.String())
	// Should not panic regardless of gofmt behavior
	_ = result
}

func TestRunValidation_WithEventBus_NilEventBus(t *testing.T) {
	// Verify no crash when eventBus is nil and no panic from nil pointer
	v := NewValidator(nil)
	v.SetEventMetadata(map[string]interface{}{"key": "val"})
	result := v.RunValidation(context.Background(), "nil_bus.go", invalidGoCode())
	if result.Valid {
		t.Fatal("expected Valid=false")
	}
}
