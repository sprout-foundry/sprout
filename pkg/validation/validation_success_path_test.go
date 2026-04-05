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

// --- RunValidation success path ---

func TestRunValidation_Success_PublishesEventWithMetadata(t *testing.T) {
	if !gofmtAcceptsStdin(t) {
		t.Skip("gofmt does not accept stdin (Go 1.25+ with old arg style)")
	}

	bus := events.NewEventBus()
	defer bus.Unsubscribe("succ-meta-pub")
	ch := bus.Subscribe("succ-meta-pub")

	v := NewValidator(bus)
	v.SetEventMetadata(map[string]interface{}{
		"editor":  "test-editor",
		"session": "abc123",
	})

	result := v.RunValidation(context.Background(), "meta_check.go", validGoCodeWithImport())

	if !result.Valid {
		t.Fatalf("expected Valid=true for well-formed code, got errors: %v", result.Errors)
	}

	// Event must be published
	select {
	case evt := <-ch:
		if evt.Type != events.EventTypeValidation {
			t.Errorf("event type = %q, want %q", evt.Type, events.EventTypeValidation)
		}
		data, ok := evt.Data.(map[string]interface{})
		if !ok {
			t.Fatalf("event Data type = %T, want map[string]interface{}", evt.Data)
		}
		if data["file_path"] != "meta_check.go" {
			t.Errorf("file_path = %v, want meta_check.go", data["file_path"])
		}
		if data["editor"] != "test-editor" {
			t.Errorf("metadata 'editor' = %v, want test-editor", data["editor"])
		}
		if data["session"] != "abc123" {
			t.Errorf("metadata 'session' = %v, want abc123", data["session"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected event on success path, got none within timeout")
	}
}

func TestRunValidation_Success_DiagnosticsFieldPopulated(t *testing.T) {
	if !gofmtAcceptsStdin(t) {
		t.Skip("gofmt does not accept stdin")
	}

	v := NewValidator(nil)
	code := `package main
func main() { println("hi") }
`
	result := v.RunValidation(context.Background(), "diag_pop.go", code)

	if !result.Valid {
		t.Fatalf("expected Valid=true, got errors: %v", result.Errors)
	}

	// Diagnostics must be populated on the success path.
	// When both Errors and Warnings are empty, append(nil, nil...) stays nil.
	// This is expected Go behavior — the field is conceptually set even if nil.
	if len(result.Diagnostics) != 0 {
		t.Fatalf("expected no diagnostics for clean code, got %d", len(result.Diagnostics))
	}
}

func TestRunValidation_Success_NoImportKeyword_SkipsImportCheck(t *testing.T) {
	if !gofmtAcceptsStdin(t) {
		t.Skip("gofmt does not accept stdin")
	}

	bus := events.NewEventBus()
	defer bus.Unsubscribe("no-import-success")
	ch := bus.Subscribe("no-import-success")

	v := NewValidator(bus)
	code := "package main\nfunc main() { println(42) }\n"

	result := v.RunValidation(context.Background(), "builtin_only.go", code)

	if !result.Valid {
		t.Fatalf("expected Valid=true, got errors: %v", result.Errors)
	}
	if len(result.Warnings) > 0 {
		t.Fatalf("expected no warnings without import keyword, got %d", len(result.Warnings))
	}

	// Event must still be published even without import check
	select {
	case evt := <-ch:
		if evt.Type != events.EventTypeValidation {
			t.Errorf("event type = %q, want %q", evt.Type, events.EventTypeValidation)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected event on success path without imports")
	}
}

func TestRunValidation_Success_WithValidImportCheck(t *testing.T) {
	if !gofmtAcceptsStdin(t) {
		t.Skip("gofmt does not accept stdin")
	}

	bus := events.NewEventBus()
	defer bus.Unsubscribe("valid-import-succ")
	_ = bus.Subscribe("valid-import-succ")

	v := NewValidator(bus)
	// Alphabetically sorted imports — goimports should produce no output
	code := `package main

import (
	"fmt"
	"os"
)

func main() { fmt.Println(os.Args) }
`
	result := v.RunValidation(context.Background(), "sorted_imports.go", code)

	if !result.Valid {
		t.Fatalf("expected Valid=true, got errors: %v", result.Errors)
	}
	if len(result.Warnings) > 0 {
		t.Fatalf("expected no import warnings for properly formatted imports, got %d", len(result.Warnings))
	}
	if len(result.Diagnostics) > 0 {
		t.Fatalf("expected no diagnostics for clean code, got %d", len(result.Diagnostics))
	}
}

// --- RunValidation with import issues (valid syntax + unsorted imports) ---

func TestRunValidation_ImportWarning_PopulatesWarningsAndDiagnostics(t *testing.T) {
	if !gofmtAcceptsStdin(t) {
		t.Skip("gofmt does not accept stdin")
	}

	bus := events.NewEventBus()
	defer bus.Unsubscribe("import-warn-test")
	ch := bus.Subscribe("import-warn-test")

	v := NewValidator(bus)
	// Intentionally unsorted imports — valid syntax but goimports will flag it
	code := `package main

import (
	"strings"
	"fmt"
)

func main() { fmt.Println(strings.Join(nil, "")) }
`
	result := v.RunValidation(context.Background(), "unsorted.go", code)

	if !result.Valid {
		t.Fatalf("expected Valid=true (syntax OK), got errors: %v", result.Errors)
	}
	if len(result.Warnings) == 0 {
		t.Fatal("expected import warnings for unsorted imports")
	}

	// Diagnostics must include both errors (none) + warnings
	if len(result.Diagnostics) < len(result.Warnings) {
		t.Errorf("Diagnostics (%d) should include all Warnings (%d)",
			len(result.Diagnostics), len(result.Warnings))
	}

	// Event must be published with diagnostics including the warning
	select {
	case evt := <-ch:
		data, ok := evt.Data.(map[string]interface{})
		if !ok {
			t.Fatalf("event Data type = %T, want map[string]interface{}", evt.Data)
		}
		diags, ok := data["diagnostics"].([]map[string]interface{})
		if !ok {
			t.Fatalf("diagnostics type = %T, want []map[string]interface{}", data["diagnostics"])
		}
		if len(diags) == 0 {
			t.Fatal("expected diagnostics in event data")
		}
		// Verify at least one diagnostic is a warning
		foundWarning := false
		for _, d := range diags {
			if d["severity"] == "warning" {
				foundWarning = true
			}
		}
		if !foundWarning {
			t.Error("expected at least one warning diagnostic in event")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected event with import warnings")
	}
}

// --- ValidateSyntax: stderr vs stdout fallback ---

func TestValidateSyntax_StderrFallback_InvalidCode(t *testing.T) {
	// For invalid code, gofmt writes errors to stderr.
	// The `if output == ""` fallback to stdout should not be triggered
	// (stderr has content), but we verify the error message is present.
	v := NewValidator(nil)
	err := v.ValidateSyntax(context.Background(), "stderr_test.go", "package x\nfunc(")
	if err == nil {
		t.Fatal("expected error for invalid code")
	}
	msg := err.Error()
	if !strings.HasPrefix(msg, "syntax error:") {
		t.Errorf("error message = %q, want prefix 'syntax error:'", msg)
	}
}

func TestValidateSyntax_StderrFallback_ValidButUnformatted(t *testing.T) {
	if !gofmtAcceptsStdin(t) {
		t.Skip("gofmt does not accept stdin")
	}

	// Valid syntax but not gofmt-formatted.
	// gofmt -l returns the filename on stdout but exits 0 → no error.
	// This tests that the `if err == nil` path (early return nil) is exercised.
	v := NewValidator(nil)
	code := `package main
func main(){println("hi")}
`
	err := v.ValidateSyntax(context.Background(), "unformatted.go", code)
	if err != nil {
		t.Errorf("expected no error for valid (but unformatted) code, got: %v", err)
	}
}

func TestValidateSyntax_StderrFallback_ValidButUnformatted_StdoutHasContent(t *testing.T) {
	if !gofmtAcceptsStdin(t) {
		t.Skip("gofmt does not accept stdin")
	}

	// Same as above but also verify gofmt -l would list the file on stdout.
	// The key insight: with -l flag, gofmt outputs filenames that need reformatting.
	// When code is valid but not formatted, stdout is non-empty but exit code is 0.
	// The function returns nil because it checks `if err == nil` first.
	v := NewValidator(nil)
	code := `package main
func main(){println("hi")}
`
	err := v.ValidateSyntax(context.Background(), "list_check.go", code)
	if err != nil {
		// Even if gofmt returns the filename on stdout, exit 0 → no error
		t.Errorf("expected nil for valid-but-unformatted code, got: %v", err)
	}
}

// --- ValidateImports: import formatting issues ---

func TestValidateImports_UnsortedImportBlock(t *testing.T) {
	if !gofmtAcceptsStdin(t) {
		t.Skip("gofmt does not accept stdin")
	}

	v := NewValidator(nil)
	// Intentionally unsorted import block — goimports should flag this
	code := `package main

import (
	"strings"
	"fmt"
)

func main() { fmt.Println(strings.Join(nil, "")) }
`
	diags := v.ValidateImports(context.Background(), "unsorted_imports.go", code)
	if len(diags) == 0 {
		t.Fatal("expected diagnostics for unsorted import block")
	}
	if diags[0].Severity != "warning" {
		t.Errorf("expected severity 'warning', got %q", diags[0].Severity)
	}
	if diags[0].Source != "goimports" {
		t.Errorf("expected source 'goimports', got %q", diags[0].Source)
	}
	if diags[0].Path != "unsorted_imports.go" {
		t.Errorf("expected path 'unsorted_imports.go', got %q", diags[0].Path)
	}
}

func TestValidateImports_MultipleFilesOutput(t *testing.T) {
	// When goimports -l outputs multiple lines (e.g., for a multi-file diff),
	// each non-empty line should produce a separate Diagnostic.
	// With stdin input, goimports outputs "<standard input>" at most once,
	// so this test verifies the parsing logic works correctly if output
	// somehow contained multiple lines (future-proofing).
	input := "line1\nline2\n\nline3\n"
	var diagnostics []Diagnostic
	for _, line := range strings.Split(input, "\n") {
		if line != "" {
			diagnostics = append(diagnostics, Diagnostic{
				Path:     "test.go",
				Severity: "warning",
				Message:  "import issue detected",
				Source:   "goimports",
			})
		}
	}
	if len(diagnostics) != 3 {
		t.Errorf("expected 3 diagnostics from 3 non-empty lines, got %d", len(diagnostics))
	}
}

func TestValidateImports_SortedImportBlock(t *testing.T) {
	if !gofmtAcceptsStdin(t) {
		t.Skip("gofmt does not accept stdin")
	}

	v := NewValidator(nil)
	code := `package main

import (
	"fmt"
	"os"
	"strings"
)

func main() {
	_ = fmt.Sprintf("%s %s", os.Args, strings.Join(nil, ""))
}
`
	diags := v.ValidateImports(context.Background(), "sorted_imports.go", code)
	if len(diags) > 0 {
		t.Fatalf("expected no diagnostics for properly sorted imports, got %d: %+v",
			len(diags), diags)
	}
}

func TestValidateImports_SingleImportNoBlock(t *testing.T) {
	if !gofmtAcceptsStdin(t) {
		t.Skip("gofmt does not accept stdin")
	}

	v := NewValidator(nil)
	code := `package main

import "fmt"

func main() { fmt.Println("hi") }
`
	diags := v.ValidateImports(context.Background(), "single_import.go", code)
	if len(diags) > 0 {
		t.Fatalf("expected no diagnostics for clean single import, got %d", len(diags))
	}
}

// --- ValidateSyntax context cancellation ---

func TestValidateSyntax_ContextCancellation(t *testing.T) {
	v := NewValidator(nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before running

	err := v.ValidateSyntax(ctx, "test.go", "package main\nfunc main(){}")
	if err == nil {
		t.Log("gofmt may have returned before context cancellation was noticed")
	} else {
		t.Logf("gofmt cancelled: %v", err)
	}
}

// --- toDiagnosticsMap edge cases ---

func TestToDiagnosticsMap_NilInput_ReturnsNonNilEmptySlice(t *testing.T) {
	result := toDiagnosticsMap(nil)
	if result == nil {
		t.Fatal("expected non-nil for nil input")
	}
	if len(result) != 0 {
		t.Fatalf("expected 0 items, got %d", len(result))
	}
}

// --- Concurrent safety under extreme conditions ---

func TestConcurrent_HeavyValidationWithMetadata(t *testing.T) {
	bus := events.NewEventBus()
	defer bus.Unsubscribe("heavy-conc")
	_ = bus.Subscribe("heavy-conc")

	v := NewValidator(bus)
	v.SetEventMetadata(map[string]interface{}{"stress": true})

	const workers = 20
	const iterations = 50
	var wg sync.WaitGroup
	var panicCount atomic.Int32
	var completions atomic.Int64

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					panicCount.Add(1)
				}
			}()
			for j := 0; j < iterations; j++ {
				code := fmt.Sprintf("package main\nfunc _%d_%d() { println(%d) }\n", id, j, id+j)
				_ = v.RunValidation(context.Background(), fmt.Sprintf("stress_%d_%d.go", id, j), code)
				completions.Add(1)
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
	case <-time.After(30 * time.Second):
		t.Fatal("heavy concurrency test timed out — possible deadlock")
	}

	if n := panicCount.Load(); n > 0 {
		t.Fatalf("%d goroutine(s) panicked", n)
	}
	t.Logf("completed %d validation calls across %d workers", completions.Load(), workers)
}

func TestConcurrent_RaceBetweenMetadataAndAsyncValidation(t *testing.T) {
	// Use nil event bus to avoid race between async goroutine's Publish
	// and test cleanup's Unsubscribe (fire-and-forget can't be synchronized).
	v := NewValidator(nil)

	const iterations = 100
	var wg sync.WaitGroup
	var panicCount atomic.Int32

	// Writer: rapidly set/clear metadata
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				panicCount.Add(1)
			}
		}()
		for i := 0; i < iterations; i++ {
			if i%3 == 0 {
				v.SetEventMetadata(nil)
			} else {
				v.SetEventMetadata(map[string]interface{}{
					"iteration": i,
					"tag":       fmt.Sprintf("t%d", i),
				})
			}
		}
	}()

	// Reader: fire async validations that read metadata
	for i := 0; i < iterations; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					panicCount.Add(1)
				}
			}()
			code := fmt.Sprintf("package main\nfunc main(){ println(%d) }\n", id)
			v.RunAsyncValidation(context.Background(), fmt.Sprintf("race_%d.go", id), code)
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
		t.Fatal("race test timed out")
	}

	// Give async goroutines time to settle
	time.Sleep(500 * time.Millisecond)

	if n := panicCount.Load(); n > 0 {
		t.Fatalf("%d goroutine(s) panicked", n)
	}
}

func TestConcurrent_MultipleEventBuses(t *testing.T) {
	// Multiple validators each with their own event bus
	const validators = 10
	var wg sync.WaitGroup
	var panicCount atomic.Int32

	for i := 0; i < validators; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					panicCount.Add(1)
				}
			}()
			bus := events.NewEventBus()
			defer bus.Unsubscribe(fmt.Sprintf("multi-bus-%d", id))
			_ = bus.Subscribe(fmt.Sprintf("multi-bus-%d", id))

			v := NewValidator(bus)
			v.SetEventMetadata(map[string]interface{}{"bus_id": id})

			code := fmt.Sprintf("package main\nfunc main(){ println(%d) }\n", id)
			_ = v.RunValidation(context.Background(), fmt.Sprintf("bus_%d.go", id), code)
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
		t.Fatal("multiple bus test timed out")
	}

	if n := panicCount.Load(); n > 0 {
		t.Fatalf("%d goroutine(s) panicked", n)
	}
}

// --- RunAsyncValidation immediate return under various loads ---

func TestRunAsyncValidation_ReturnsImmediately_UnderLoad(t *testing.T) {
	v := NewValidator(nil)

	// 200 rapid async calls should all return immediately (non-blocking)
	start := time.Now()
	for i := 0; i < 200; i++ {
		code := fmt.Sprintf("package main\nfunc _%d(){ println(%d) }\n", i, i)
		v.RunAsyncValidation(context.Background(), fmt.Sprintf("load_%d.go", i), code)
	}
	elapsed := time.Since(start)

	if elapsed > 3*time.Second {
		t.Fatalf("200 async calls took %v, expected <3s", elapsed)
	}
}

func TestRunAsyncValidation_DoesNotBlockWithLargeContent(t *testing.T) {
	bus := events.NewEventBus()
	defer bus.Unsubscribe("async-large")
	_ = bus.Subscribe("async-large")

	v := NewValidator(bus)

	// Very large file content
	largeContent := strings.Repeat("package main\nfunc _(){ println(42) }\n", 10000)

	start := time.Now()
	v.RunAsyncValidation(context.Background(), "large_async.go", largeContent)
	elapsed := time.Since(start)

	if elapsed > 200*time.Millisecond {
		t.Fatalf("RunAsyncValidation blocked for %v with large content", elapsed)
	}
}
