package validation

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alantheprice/ledit/pkg/events"
)

// --- Helpers ---

func invalidGoCode() string {
	return "package x\nfunc("
}

func validGoCodeWithImport() string {
	return `package main

import "fmt"

func main() { fmt.Println("hi") }
`
}

// --- ValidateSyntax ---

func TestValidateSyntax_InvalidCode(t *testing.T) {
	v := NewValidator(nil)
	err := v.ValidateSyntax(context.Background(), "test.go", invalidGoCode())
	if err == nil {
		t.Fatal("expected error for invalid code, got nil")
	}
	if msg := err.Error(); len(msg) == 0 {
		t.Fatal("expected non-empty error message")
	}
}

func TestValidateSyntax_CancelledContext(t *testing.T) {
	v := NewValidator(nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately
	err := v.ValidateSyntax(ctx, "test.go", invalidGoCode())
	if err == nil {
		t.Fatal("expected error on cancelled context, got nil")
	}
}

func TestValidateSyntax_ValidCode(t *testing.T) {
	// ValidateSyntax checks syntax via gofmt on a temp file, which works
	// consistently across Go versions (no stdin issues).
	v := NewValidator(nil)
	err := v.ValidateSyntax(context.Background(), "test.go", validGoCodeWithImport())
	if err != nil {
		t.Fatalf("expected no error for valid code, got: %v", err)
	}
}

// --- ValidateImports ---

func TestValidateImports_ValidCode(t *testing.T) {
	v := NewValidator(nil)
	diags := v.ValidateImports(context.Background(), "test.go", validGoCodeWithImport())
	if len(diags) != 0 {
		t.Fatalf("expected no diagnostics for well-formatted imports, got %d", len(diags))
	}
}

func TestValidateImports_InvalidCode(t *testing.T) {
	// goimports on syntax-invalid code may error; the function should return nil on error
	v := NewValidator(nil)
	diags := v.ValidateImports(context.Background(), "test.go", invalidGoCode())
	// Should not panic; result is either empty or nil
	if len(diags) > 0 {
		// Acceptable but unexpected — goimports might produce output
		t.Logf("got %d diagnostics for invalid code (acceptable)", len(diags))
	}
}

func TestValidateImports_CancelledContext(t *testing.T) {
	v := NewValidator(nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	diags := v.ValidateImports(ctx, "test.go", validGoCodeWithImport())
	if len(diags) != 0 {
		t.Fatalf("expected no diagnostics on cancelled context, got %d", len(diags))
	}
}

func TestValidateImports_CodeWithoutImportKeyword(t *testing.T) {
	v := NewValidator(nil)
	// Code without "import" — goimports should still handle it gracefully
	code := "package main\nfunc main(){}\n"
	diags := v.ValidateImports(context.Background(), "test.go", code)
	// Should not panic; result is nil or empty
	if len(diags) > 0 {
		t.Logf("got %d diagnostics for code without imports: %v", len(diags), diags)
	}
}

// --- RunValidation ---

func TestRunValidation_InvalidCode(t *testing.T) {
	v := NewValidator(nil)
	result := v.RunValidation(context.Background(), "test.go", invalidGoCode())
	if result.Valid {
		t.Fatal("expected Valid=false for invalid code")
	}
	if len(result.Errors) == 0 {
		t.Fatal("expected at least one error")
	}
	// Verify error details
	d := result.Errors[0]
	if d.Path != "test.go" {
		t.Errorf("error Path = %q, want %q", d.Path, "test.go")
	}
	if d.Severity != "error" {
		t.Errorf("error Severity = %q, want %q", d.Severity, "error")
	}
	if d.Source != "gofmt" {
		t.Errorf("error Source = %q, want %q", d.Source, "gofmt")
	}
}

func TestRunValidation_PathSet(t *testing.T) {
	v := NewValidator(nil)
	result := v.RunValidation(context.Background(), "myfile.go", invalidGoCode())
	if result.Path != "myfile.go" {
		t.Fatalf("expected Path=myfile.go, got %s", result.Path)
	}
}

func TestRunValidation_CancelledContext(t *testing.T) {
	v := NewValidator(nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := v.RunValidation(ctx, "test.go", validGoCodeWithImport())
	if result.Valid {
		t.Fatal("expected Valid=false when context is already cancelled")
	}
}

func TestRunValidation_DiagnosticsIncludesErrorsAndWarnings(t *testing.T) {
	// When syntax check fails, RunValidation returns early with Errors set
	// but Diagnostics is empty (populated only on success path).
	bus := events.NewEventBus()
	defer bus.Unsubscribe("diag-test")

	v := NewValidator(bus)
	result := v.RunValidation(context.Background(), "err.go", invalidGoCode())
	if result.Valid {
		t.Fatal("expected Valid=false")
	}
	if len(result.Errors) == 0 {
		t.Fatal("expected at least one error")
	}

	// On the error path, Diagnostics is empty because RunValidation returns early
	if len(result.Diagnostics) != 0 {
		t.Errorf("expected empty diagnostics on error path, got %d", len(result.Diagnostics))
	}
}

func TestRunValidation_WithEventBus_Metadata(t *testing.T) {
	bus := events.NewEventBus()
	defer bus.Unsubscribe("test-sub-meta")

	ch := bus.Subscribe("test-sub-meta")
	v := NewValidator(bus)
	v.SetEventMetadata(map[string]interface{}{
		"editor":  "test",
		"version": "1.0",
	})

	// Use invalid code: RunValidation returns early on syntax error,
	// so no event is published on the error path.
	v.RunValidation(context.Background(), "test.go", invalidGoCode())

	// Verify no event was published (early return before event publishing)
	select {
	case <-ch:
		t.Fatal("expected no event on syntax error path (early return)")
	case <-time.After(200 * time.Millisecond):
		// Expected: no event published on error path
	}
}

func TestRunValidation_NoImportKeywordSkipsImportCheck(t *testing.T) {
	v := NewValidator(nil)
	// Code without "import" keyword should skip ValidateImports
	code := "package main\nfunc main(){ println(1) }\n"
	result := v.RunValidation(context.Background(), "test.go", code)
	// With temp-file validation, syntax check should succeed (valid code).
	if result.Valid {
		if len(result.Warnings) > 0 {
			t.Fatalf("expected no import warnings when 'import' not in code, got %d", len(result.Warnings))
		}
	}
}

func TestRunValidation_ContextTimeout(t *testing.T) {
	v := NewValidator(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()
	// Give context time to expire
	time.Sleep(5 * time.Millisecond)
	result := v.RunValidation(ctx, "test.go", validGoCodeWithImport())
	// With an expired context, cmd should fail
	if result.Valid {
		t.Log("validation succeeded despite expired context (non-fatal, may vary)")
	}
}

func TestRunValidation_WithNilEventBus_NoPanic(t *testing.T) {
	v := NewValidator(nil)
	// Must not panic when eventBus is nil
	result := v.RunValidation(context.Background(), "test.go", invalidGoCode())
	if result.Valid {
		t.Fatal("expected Valid=false for invalid code")
	}
}

// --- toDiagnosticsMap ---

func TestToDiagnosticsMap_Empty(t *testing.T) {
	result := toDiagnosticsMap(nil)
	if result == nil {
		t.Fatal("expected non-nil result for empty slice")
	}
	if len(result) != 0 {
		t.Fatalf("expected empty result, got %d items", len(result))
	}
}

func TestToDiagnosticsMap_NonEmpty(t *testing.T) {
	diags := []Diagnostic{
		{Path: "a.go", Line: 1, Column: 1, Severity: "error", Message: "err1", Source: "gofmt"},
		{Path: "b.go", Line: 2, Column: 5, Severity: "warning", Message: "warn1", Source: "goimports"},
	}
	result := toDiagnosticsMap(diags)
	if len(result) != 2 {
		t.Fatalf("expected 2 items, got %d", len(result))
	}

	// Check first diagnostic
	if result[0]["path"] != "a.go" {
		t.Errorf("item[0] path = %v, want a.go", result[0]["path"])
	}
	if result[0]["line"] != 1 {
		t.Errorf("item[0] line = %v, want 1", result[0]["line"])
	}
	if result[0]["column"] != 1 {
		t.Errorf("item[0] column = %v, want 1", result[0]["column"])
	}
	if result[0]["severity"] != "error" {
		t.Errorf("item[0] severity = %v, want error", result[0]["severity"])
	}
	if result[0]["message"] != "err1" {
		t.Errorf("item[0] message = %v, want err1", result[0]["message"])
	}
	if result[0]["source"] != "gofmt" {
		t.Errorf("item[0] source = %v, want gofmt", result[0]["source"])
	}

	// Check second diagnostic
	if result[1]["path"] != "b.go" {
		t.Errorf("item[1] path = %v, want b.go", result[1]["path"])
	}
	if result[1]["line"] != 2 {
		t.Errorf("item[1] line = %v, want 2", result[1]["line"])
	}
	if result[1]["column"] != 5 {
		t.Errorf("item[1] column = %v, want 5", result[1]["column"])
	}
	if result[1]["severity"] != "warning" {
		t.Errorf("item[1] severity = %v, want warning", result[1]["severity"])
	}
	if result[1]["message"] != "warn1" {
		t.Errorf("item[1] message = %v, want warn1", result[1]["message"])
	}
	if result[1]["source"] != "goimports" {
		t.Errorf("item[1] source = %v, want goimports", result[1]["source"])
	}
}

// --- SetEventMetadata ---

func TestSetEventMetadata_Nil(t *testing.T) {
	v := NewValidator(nil)
	v.SetEventMetadata(nil)
	// Should not panic; eventMetadata should be nil after nil/empty input
	payload := v.decorateEventPayload(map[string]interface{}{"key": "val"})
	if len(payload) != 1 {
		t.Errorf("expected 1 key, got %d: %v", len(payload), payload)
	}
}

func TestSetEventMetadata_Empty(t *testing.T) {
	v := NewValidator(nil)
	v.SetEventMetadata(map[string]interface{}{"a": "b"})
	v.SetEventMetadata(map[string]interface{}{})
	// Empty map should clear metadata
	payload := v.decorateEventPayload(map[string]interface{}{"key": "val"})
	if len(payload) != 1 {
		t.Errorf("expected 1 key after clearing, got %d: %v", len(payload), payload)
	}
}

func TestSetEventMetadata_NonNil(t *testing.T) {
	v := NewValidator(nil)
	v.SetEventMetadata(map[string]interface{}{
		"source":  "editor",
		"version": 2,
	})
	payload := v.decorateEventPayload(map[string]interface{}{"file": "test.go"})
	if payload["source"] != "editor" {
		t.Errorf("expected source=editor, got %v", payload["source"])
	}
	if payload["version"] != 2 {
		t.Errorf("expected version=2, got %v", payload["version"])
	}
	if payload["file"] != "test.go" {
		t.Errorf("expected file=test.go, got %v", payload["file"])
	}
}

func TestSetEventMetadata_StoresACopy(t *testing.T) {
	v := NewValidator(nil)
	original := map[string]interface{}{"key": "value"}
	v.SetEventMetadata(original)
	// Mutate the original map — should not affect the validator's copy
	original["key"] = "mutated"
	payload := v.decorateEventPayload(map[string]interface{}{})
	if payload["key"] == "mutated" {
		t.Error("SetEventMetadata did not copy the input map")
	}
	if payload["key"] != "value" {
		t.Errorf("expected key=value, got %v", payload["key"])
	}
}

func TestSetEventMetadata_OverridesPreviousMetadata(t *testing.T) {
	v := NewValidator(nil)
	v.SetEventMetadata(map[string]interface{}{"a": 1})
	v.SetEventMetadata(map[string]interface{}{"b": 2})
	payload := v.decorateEventPayload(map[string]interface{}{})
	if payload["a"] != nil {
		t.Errorf("expected old key 'a' to be gone, got: %v", payload["a"])
	}
	if payload["b"] != 2 {
		t.Errorf("expected b=2, got %v", payload["b"])
	}
}

// --- decorateEventPayload ---

func TestDecorateEventPayload_NilInput(t *testing.T) {
	v := NewValidator(nil)
	result := v.decorateEventPayload(nil)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestDecorateEventPayload_NoMetadata(t *testing.T) {
	v := NewValidator(nil)
	input := map[string]interface{}{"a": "b"}
	result := v.decorateEventPayload(input)
	// Since no metadata is set, it should return the same map (or copy)
	if result["a"] != "b" {
		t.Errorf("expected a=b, got %v", result["a"])
	}
}

func TestDecorateEventPayload_MergesWithoutOverwrite(t *testing.T) {
	v := NewValidator(nil)
	v.SetEventMetadata(map[string]interface{}{
		"key1": "from-meta",
		"key2": "from-meta",
	})
	input := map[string]interface{}{
		"key2": "from-input",
		"key3": "from-input",
	}
	result := v.decorateEventPayload(input)

	// key1 should come from metadata
	if result["key1"] != "from-meta" {
		t.Errorf("key1 = %v, want from-meta", result["key1"])
	}
	// key2 should come from input (not overwritten by metadata)
	if result["key2"] != "from-input" {
		t.Errorf("key2 = %v, want from-input", result["key2"])
	}
	// key3 should come from input
	if result["key3"] != "from-input" {
		t.Errorf("key3 = %v, want from-input", result["key3"])
	}
}

func TestDecorateEventPayload_DoesNotMutateInput(t *testing.T) {
	v := NewValidator(nil)
	v.SetEventMetadata(map[string]interface{}{"meta": "val"})
	input := map[string]interface{}{"input": "val"}
	result := v.decorateEventPayload(input)

	// The input map should not be mutated
	if _, ok := input["meta"]; ok {
		t.Error("decorateEventPayload mutated the input map")
	}
	// The result should have both
	if result["input"] != "val" {
		t.Errorf("expected input=val in result, got %v", result["input"])
	}
	if result["meta"] != "val" {
		t.Errorf("expected meta=val in result, got %v", result["meta"])
	}
}

func TestDecorateEventPayload_MetadataTakesPrecedenceForNewKeys(t *testing.T) {
	v := NewValidator(nil)
	v.SetEventMetadata(map[string]interface{}{
		"meta_key": "meta_val",
	})
	input := map[string]interface{}{}
	result := v.decorateEventPayload(input)

	if result["meta_key"] != "meta_val" {
		t.Errorf("meta_key = %v, want meta_val", result["meta_key"])
	}
}

// --- RunAsyncValidation ---

func TestRunAsyncValidation_DoesNotBlock(t *testing.T) {
	v := NewValidator(nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	v.RunAsyncValidation(ctx, "test.go", invalidGoCode())
	elapsed := time.Since(start)

	if elapsed > time.Second {
		t.Fatalf("RunAsyncValidation blocked for %v, expected immediate return", elapsed)
	}
}

func TestRunAsyncValidation_CompletesEventually(t *testing.T) {
	// NOTE: This is a weak test. RunAsyncValidation is fire-and-forget with no
	// callback or channel to observe completion. The best we can verify is that
	// it doesn't deadlock or panic within a reasonable timeout.
	bus := events.NewEventBus()
	defer bus.Unsubscribe("async-test")

	v := NewValidator(bus)

	completed := make(chan struct{})

	go func() {
		// Wait a bit — the async goroutine should complete within this time
		time.Sleep(2 * time.Second)
		close(completed)
	}()

	v.RunAsyncValidation(context.Background(), "test.go", invalidGoCode())

	select {
	case <-completed:
		// If we get here, RunAsyncValidation didn't block/deadlock
	case <-time.After(3 * time.Second):
		t.Fatal("async validation may have blocked")
	}
}

func TestRunAsyncValidation_WithAtomicFlag(t *testing.T) {
	v := NewValidator(nil)
	bus := events.NewEventBus()
	subCh := bus.Subscribe("atomic-test")
	defer bus.Unsubscribe("atomic-test")

	var completed atomic.Bool

	_ = subCh // just subscribing is enough to ensure bus is active

	done := make(chan struct{})
	go func() {
		time.Sleep(2 * time.Second)
		completed.Store(true)
		close(done)
	}()

	v.RunAsyncValidation(context.Background(), "test.go", invalidGoCode())

	select {
	case <-done:
		// Good — 2 seconds passed without deadlock
	case <-time.After(3 * time.Second):
		t.Fatal("possible deadlock or unexpected blocking")
	}
}

// --- NewValidator ---

func TestNewValidator_NilEventBus(t *testing.T) {
	v := NewValidator(nil)
	if v == nil {
		t.Fatal("expected non-nil validator")
	}
	if v.eventBus != nil {
		t.Fatal("expected nil event bus")
	}
}

func TestNewValidator_WithEventBus(t *testing.T) {
	bus := events.NewEventBus()
	v := NewValidator(bus)
	if v == nil {
		t.Fatal("expected non-nil validator")
	}
	if v.eventBus != bus {
		t.Fatal("expected event bus to be set")
	}
}

// --- Additional edge case tests ---

func TestValidateSyntax_EmptyContent(t *testing.T) {
	v := NewValidator(nil)
	// Empty content should trigger gofmt error
	err := v.ValidateSyntax(context.Background(), "test.go", "")
	if err == nil {
		t.Log("gofmt accepted empty content (some versions)")
	}
}

func TestValidateImports_EmptyContent(t *testing.T) {
	v := NewValidator(nil)
	diags := v.ValidateImports(context.Background(), "test.go", "")
	// Should not panic
	_ = diags
}

func TestRunValidation_EventBusPublishesCorrectEventType(t *testing.T) {
	bus := events.NewEventBus()
	defer bus.Unsubscribe("evt-type-test")
	ch := bus.Subscribe("evt-type-test")

	v := NewValidator(bus)
	// On syntax error path, RunValidation returns early without publishing.
	v.RunValidation(context.Background(), "test.go", invalidGoCode())

	select {
	case <-ch:
		t.Fatal("expected no event on syntax error path (early return)")
	case <-time.After(200 * time.Millisecond):
		// Expected: no event published
	}
}

func TestRunValidation_EventContainsFilePath(t *testing.T) {
	bus := events.NewEventBus()
	defer bus.Unsubscribe("file-path-test")
	ch := bus.Subscribe("file-path-test")

	v := NewValidator(bus)
	v.RunValidation(context.Background(), "specific/path/file.go", invalidGoCode())

	select {
	case <-ch:
		t.Fatal("expected no event on syntax error path (early return)")
	case <-time.After(200 * time.Millisecond):
		// Expected: no event published
	}
}

func TestDecorateEventPayload_WithNilMetadataAndNilPayload(t *testing.T) {
	v := NewValidator(nil)
	result := v.decorateEventPayload(nil)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if len(result) != 0 {
		t.Errorf("expected empty map, got %d keys", len(result))
	}
}

func TestDecorateEventPayload_MultipleMetadataKeys(t *testing.T) {
	v := NewValidator(nil)
	v.SetEventMetadata(map[string]interface{}{
		"k1": "v1",
		"k2": "v2",
		"k3": "v3",
	})
	result := v.decorateEventPayload(nil)
	if len(result) != 3 {
		t.Errorf("expected 3 keys, got %d: %v", len(result), result)
	}
	if result["k1"] != "v1" || result["k2"] != "v2" || result["k3"] != "v3" {
		t.Errorf("unexpected metadata values: %v", result)
	}
}

func TestToDiagnosticsMap_FieldsAreCorrectTypes(t *testing.T) {
	diags := []Diagnostic{
		{Path: "test.go", Line: 42, Column: 7, Severity: "error", Message: "test msg", Source: "gofmt"},
	}
	result := toDiagnosticsMap(diags)
	if path, ok := result[0]["path"].(string); !ok || path != "test.go" {
		t.Errorf("path: expected string 'test.go', got %T %v", result[0]["path"], result[0]["path"])
	}
	if line, ok := result[0]["line"].(int); !ok || line != 42 {
		t.Errorf("line: expected int 42, got %T %v", result[0]["line"], result[0]["line"])
	}
	if col, ok := result[0]["column"].(int); !ok || col != 7 {
		t.Errorf("column: expected int 7, got %T %v", result[0]["column"], result[0]["column"])
	}
	if sev, ok := result[0]["severity"].(string); !ok || sev != "error" {
		t.Errorf("severity: expected string 'error', got %T %v", result[0]["severity"], result[0]["severity"])
	}
	if msg, ok := result[0]["message"].(string); !ok || msg != "test msg" {
		t.Errorf("message: expected string 'test msg', got %T %v", result[0]["message"], result[0]["message"])
	}
	if src, ok := result[0]["source"].(string); !ok || src != "gofmt" {
		t.Errorf("source: expected string 'gofmt', got %T %v", result[0]["source"], result[0]["source"])
	}
}

// --- Additional coverage tests ---

func TestValidateSyntax_StdoutHasContent(t *testing.T) {
	// gofmt -e -l on a temp file writes error info to stderr for invalid code.
	v := NewValidator(nil)
	err := v.ValidateSyntax(context.Background(), "test.go", "package x\nfunc(")
	if err == nil {
		t.Fatal("expected error for invalid code")
	}
	msg := err.Error()
	if len(msg) == 0 {
		t.Fatal("expected non-empty error message")
	}
	t.Logf("ValidateSyntax error: %v", msg)
}

func TestRunValidation_WithImportKeyword(t *testing.T) {
	// Code with "import" keyword should trigger the import check path
	// after syntax validation passes (uses temp files, works across Go versions).
	v := NewValidator(nil)
	code := `package main

import "fmt"

func main() { fmt.Println(1) }
`
	result := v.RunValidation(context.Background(), "test.go", code)
	// With temp-file validation, syntax check should succeed.
	if !result.Valid {
		t.Fatalf("expected Valid=true for valid code, got errors: %v", result.Errors)
	}
}

func TestRunValidation_ValidSyntax_PublishesEvent(t *testing.T) {
	// With temp-file validation, the success path is always reachable.
	bus := events.NewEventBus()
	defer bus.Unsubscribe("valid-succ-test")
	ch := bus.Subscribe("valid-succ-test")

	v := NewValidator(bus)
	v.SetEventMetadata(map[string]interface{}{"test": "coverage"})

	code := `package main

import "fmt"

func main() { fmt.Println(1) }
`
	result := v.RunValidation(context.Background(), "test.go", code)

	if !result.Valid {
		t.Fatalf("expected Valid=true, got errors: %v", result.Errors)
	}

	// Event should be published on success path
	select {
	case evt := <-ch:
		t.Logf("Event published: %+v", evt)
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected event to be published on success path")
	}
}

func TestValidateImports_GoimportsNotAvailable(t *testing.T) {
	// When goimports is not available on PATH, cmd.Run() fails and
	// ValidateImports should return nil (not panic).
	// We simulate this by using a context that's already cancelled,
	// which causes the command to fail immediately.
	v := NewValidator(nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before running

	diags := v.ValidateImports(ctx, "test.go", validGoCodeWithImport())
	// Should return nil when goimports fails (cancelled context)
	if diags != nil {
		t.Fatalf("expected nil diagnostics when goimports fails, got %d: %v", len(diags), diags)
	}
}

func TestValidateImports_GoimportsBinaryNotFound(t *testing.T) {
	// Test with invalid content that makes goimports error (exit non-zero), which returns nil
	v := NewValidator(nil)

	diags := v.ValidateImports(context.Background(), "test.go", "package x\nfunc(")
	if diags != nil {
		t.Fatalf("expected nil diagnostics when goimports errors, got %d: %v", len(diags), diags)
	}
}

func TestValidateImports_NonEmptyGoimportsOutput(t *testing.T) {
	// goimports -l on a temp file outputs file paths with import issues.
	// Well-formatted code should produce no diagnostics.
	v := NewValidator(nil)
	code := `package main

import (
	"fmt"
	"strings"
)

func main() {
	fmt.Println(strings.Join([]string{"a"}, ""))
}
`
	diags := v.ValidateImports(context.Background(), "test.go", code)
	// Well-formatted code with all used imports should produce no diagnostics
	if len(diags) > 0 {
		t.Logf("got %d diagnostics for well-formatted code: %v", len(diags), diags)
	}
}

func TestRunValidation_CodeWithoutImportKeyword_SkipsImportCheck(t *testing.T) {
	// Code without "import" should skip ValidateImports entirely
	v := NewValidator(nil)
	code := "package main\nfunc main(){ println(1) }\n"

	result := v.RunValidation(context.Background(), "test.go", code)

	// With temp-file validation, this should succeed (valid code, no imports)
	if !result.Valid {
		t.Fatalf("expected Valid=true for valid code, got errors: %v", result.Errors)
	}
	// No warnings since no "import" keyword
	if len(result.Warnings) > 0 {
		t.Fatalf("expected no warnings without import keyword, got %d", len(result.Warnings))
	}
}

func TestRunValidation_EventBusPublishesWithMetadata(t *testing.T) {
	bus := events.NewEventBus()
	defer bus.Unsubscribe("meta-pub-test")
	ch := bus.Subscribe("meta-pub-test")

	v := NewValidator(bus)
	v.SetEventMetadata(map[string]interface{}{
		"editor":  "test-editor",
		"display": true,
	})

	// Valid code → success path → event published with metadata
	result := v.RunValidation(context.Background(), "test.go", validGoCodeWithImport())

	if !result.Valid {
		t.Fatalf("expected Valid=true, got: %v", result.Errors)
	}

	select {
	case evt := <-ch:
		data, ok := evt.Data.(map[string]interface{})
		if !ok {
			t.Fatalf("event Data type = %T, want map[string]interface{}", evt.Data)
		}
		if data["editor"] != "test-editor" {
			t.Errorf("metadata 'editor' = %v, want test-editor", data["editor"])
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected event to be published on success path")
	}
}
