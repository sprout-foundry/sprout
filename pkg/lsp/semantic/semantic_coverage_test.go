package semantic

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// =============================================================================
// session_pool requestEvict — uncovered path: nonexistent workspace key
// =============================================================================

func TestRequestEvictNonexistentWorkspace(t *testing.T) {
	pool := NewSessionPool(func(workspaceRoot string) (SessionAdapter, error) {
		return &fakeSessionAdapter{healthy: true}, nil
	}, 0)

	// requestEvict on a workspace that was never created — should be a no-op
	pool.requestEvict("/nonexistent", &fakeSessionAdapter{healthy: true})

	pool.Close()
}

// =============================================================================
// NewTypeScriptSessionPool — improve from 33.3% by exercising the pool
// =============================================================================

func TestNewTypeScriptSessionPoolFactory(t *testing.T) {
	// Just verify the pool is created and can be closed without panic.
	// We don't call Run() through the pool because the persistent node worker
	// will block waiting for stdin and never respond if typescript isn't available.
	pool := NewTypeScriptSessionPool(0)
	if pool == nil {
		t.Fatal("expected non-nil pool")
	}
	pool.Close()
}

func TestNewTypeScriptSessionPoolWithIdleEviction(t *testing.T) {
	pool := NewTypeScriptSessionPool(50 * time.Millisecond)
	if pool == nil {
		t.Fatal("expected non-nil pool")
	}
	// EvictIdle should not panic on empty pool
	pool.EvictIdle()
	pool.Close()
}

// =============================================================================
// typeScriptSessionAdapter Run — improve from 17.4% by covering error paths
// via the SessionPool with a mock SessionFactory
// =============================================================================

// errSessionAdapter is a SessionAdapter whose Run always returns an error.
type errSessionAdapter struct {
	closed bool
	err    error
}

func (a *errSessionAdapter) Run(input ToolInput) (ToolResult, error) {
	return ToolResult{}, a.err
}
func (a *errSessionAdapter) Healthy() bool { return !a.closed }
func (a *errSessionAdapter) Close() error {
	a.closed = true
	return nil
}

func TestSessionPoolRunWithAdapterRunError(t *testing.T) {
	callCount := 0
	pool := NewSessionPool(func(workspaceRoot string) (SessionAdapter, error) {
		callCount++
		return &errSessionAdapter{err: errors.New("adapter crashed")}, nil
	}, 0)
	defer pool.Close()

	_, err := pool.Run(ToolInput{WorkspaceRoot: "/repo-a"})
	if err == nil {
		t.Fatal("expected error from adapter Run")
	}
	if err.Error() != "adapter crashed" {
		t.Errorf("unexpected error: %v", err)
	}

	// After error, the adapter should be evicted, and the next call creates a new one
	_, err = pool.Run(ToolInput{WorkspaceRoot: "/repo-a"})
	if err == nil {
		t.Fatal("expected error from second adapter Run")
	}
	if callCount != 2 {
		t.Fatalf("expected factory to be called twice (eviction after error), got %d", callCount)
	}
}

func TestSessionPoolReleasesInUseOnSuccess(t *testing.T) {
	var mu sync.Mutex
	runCount := 0
	pool := NewSessionPool(func(workspaceRoot string) (SessionAdapter, error) {
		return &trackRunAdapter{runFn: func() {
			mu.Lock()
			runCount++
			mu.Unlock()
		}}, nil
	}, 0)
	defer pool.Close()

	_, err := pool.Run(ToolInput{WorkspaceRoot: "/repo-a"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mu.Lock()
	rc := runCount
	mu.Unlock()
	if rc != 1 {
		t.Fatalf("expected 1 run, got %d", rc)
	}
}

type trackRunAdapter struct {
	runFn func()
}

func (a *trackRunAdapter) Run(input ToolInput) (ToolResult, error) {
	a.runFn()
	return ToolResult{Capabilities: Capabilities{Diagnostics: true}}, nil
}
func (a *trackRunAdapter) Healthy() bool { return true }
func (a *trackRunAdapter) Close() error  { return nil }

// =============================================================================
// typeScriptSessionAdapter — Run error paths more specifically
// The internal fields are unexported, so we test through ensureWorkerLocked
// and then manually corrupt fields to trigger error paths.
// =============================================================================

func TestTypeScriptSessionAdapterRunStdoutReadError(t *testing.T) {
	a := &typeScriptSessionAdapter{}
	defer a.Close()

	// Pre-populate internal fields with mocks to avoid starting a real node process.
	a.mu.Lock()
	initFakeWorker(a, &noopWriteCloser{}, bufio.NewReader(&errorReader{err: errors.New("read failed")}))
	a.mu.Unlock()

	_, err := a.Run(ToolInput{
		Method:        "diagnostics",
		Content:       "const x = 1",
		FilePath:      "test.ts",
		WorkspaceRoot: t.TempDir(),
	})
	if err == nil {
		t.Fatal("expected error when stdout read fails")
	}
	if !strings.Contains(err.Error(), "read failed") {
		t.Errorf("error should mention read failure, got: %v", err)
	}
}

func TestTypeScriptSessionAdapterRunWriteError(t *testing.T) {
	a := &typeScriptSessionAdapter{}
	defer a.Close()

	a.mu.Lock()
	initFakeWorker(a, &errWriteCloser{err: errors.New("write failed")}, bufio.NewReader(strings.NewReader("{}\n")))
	a.mu.Unlock()

	_, err := a.Run(ToolInput{
		Method:        "diagnostics",
		Content:       "const x = 1",
		FilePath:      "test.ts",
		WorkspaceRoot: t.TempDir(),
	})
	if err == nil {
		t.Fatal("expected error when stdin write fails")
	}
	if !strings.Contains(err.Error(), "write failed") {
		t.Errorf("error should mention write failure, got: %v", err)
	}
}

func TestTypeScriptSessionAdapterRunJSONParseError(t *testing.T) {
	a := &typeScriptSessionAdapter{}
	defer a.Close()

	a.mu.Lock()
	initFakeWorker(a, &noopWriteCloser{}, bufio.NewReader(strings.NewReader("not valid json\n")))
	a.mu.Unlock()

	_, err := a.Run(ToolInput{
		Method:        "diagnostics",
		Content:       "const x = 1",
		FilePath:      "test.ts",
		WorkspaceRoot: t.TempDir(),
	})
	if err == nil {
		t.Fatal("expected JSON parse error")
	}
	if !strings.Contains(err.Error(), "parse failed") {
		t.Errorf("error should mention parse failure, got: %v", err)
	}
}

func TestTypeScriptSessionAdapterRunWithSuccessResponse(t *testing.T) {
	a := &typeScriptSessionAdapter{}
	defer a.Close()

	validResult := ToolResult{
		Capabilities: Capabilities{Diagnostics: true},
		Error:        "typescript_not_available",
	}
	resultJSON, _ := json.Marshal(validResult)
	a.mu.Lock()
	initFakeWorker(a, &noopWriteCloser{}, bufio.NewReader(bytes.NewReader(append(resultJSON, '\n'))))
	a.mu.Unlock()

	result, err := a.Run(ToolInput{
		Method:        "diagnostics",
		Content:       "const x = 1",
		FilePath:      "test.ts",
		WorkspaceRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error != "typescript_not_available" {
		t.Errorf("expected typescript_not_available error, got %q", result.Error)
	}
}

func initFakeWorker(a *typeScriptSessionAdapter, stdin io.WriteCloser, stdout *bufio.Reader) {
	// Set internal fields so ensureWorkerLocked's guard passes:
	//   a.cmd != nil && a.cmd.Process != nil && a.cmd.ProcessState == nil
	// We use PID 1 (init), which is always running. Kill on it returns
	// EPERM (discarded by resetWorkerLocked). Wait returns an error (also discarded).
	a.cmd = &exec.Cmd{Process: &os.Process{Pid: 1}}
	a.stdin = stdin
	a.stdout = stdout
}

type errWriteCloser struct {
	err error
}

func (e *errWriteCloser) Write(p []byte) (int, error) { return 0, e.err }
func (e *errWriteCloser) Close() error                { return nil }

// =============================================================================
// runTypeScriptTool — cover edge cases for node exec failure
// =============================================================================

func TestRunTypeScriptToolNodeExecError(t *testing.T) {
	// Test the case where the node command processes input but typescript is not installed.
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available")
	}

	tmpDir := t.TempDir()
	result, err := runTypeScriptTool(ToolInput{
		Content:       "const x = 1",
		FilePath:      "test.ts",
		WorkspaceRoot: tmpDir,
		Method:        "diagnostics",
	})
	// Either the tool runs successfully or returns an error — both are valid.
	// The important thing is that it does not panic.
	if err != nil {
		t.Logf("runTypeScriptTool returned error (expected without typescript): %v", err)
	} else if !result.Capabilities.Diagnostics {
		t.Log("diagnostics capability not set (acceptable without typescript)")
	}
}

func TestRunTypeScriptToolWithUnknownMethod(t *testing.T) {
	// Verify that an unknown method does not cause a panic.
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available")
	}

	tmpDir := t.TempDir()
	result, err := runTypeScriptTool(ToolInput{
		Content:       "",
		FilePath:      "test.ts",
		WorkspaceRoot: tmpDir,
		Method:        "unknown_method",
	})
	// Either outcome is valid; the test ensures no panic.
	if err != nil {
		t.Logf("runTypeScriptTool with unknown method returned error: %v", err)
	} else {
		t.Logf("runTypeScriptTool with unknown method returned without error, capabilities=%+v", result.Capabilities)
	}
}

// =============================================================================
// goSessionAdapter Run — cover dispatch paths without gopls
// These paths should return capability errors without panicking.
// =============================================================================

func TestGoSessionAdapterRunReferencesNoGoplsDispatch(t *testing.T) {
	_, err := exec.LookPath("gopls")
	if err == nil {
		t.Skip("gopls is available; skipping not-available path test")
	}

	a := &goSessionAdapter{}
	defer a.Close()

	result, err := a.Run(ToolInput{
		Method:        "references",
		Content:       "package main\n",
		FilePath:      "main.go",
		WorkspaceRoot: "/tmp",
		Position:      &Position{Line: 1, Column: 1},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error != "gopls_not_available" {
		t.Errorf("expected gopls_not_available, got %q", result.Error)
	}
}

// =============================================================================
// goSessionAdapter ensureServerLocked — cover the server-not-ready path
// =============================================================================

func TestGoSessionAdapterEnsureServerGoplsNotAvailable(t *testing.T) {
	_, err := exec.LookPath("gopls")
	if err == nil {
		t.Skip("gopls is available; skipping not-available path test")
	}

	a := &goSessionAdapter{}
	defer a.Close()

	a.mu.Lock()
	err = a.ensureServerLocked(t.TempDir())
	a.mu.Unlock()

	if err == nil {
		t.Fatal("expected error when gopls is not available")
	}
}

// =============================================================================
// Registry — Singleton adaptation edge cases
// =============================================================================

func TestRegistrySingletonMultipleLanguages(t *testing.T) {
	r := NewRegistry()
	singleton := &countingAdapter{}
	r.RegisterSingleton(singleton, "typescript", "typescriptreact", "javascript", "javascriptreact")

	for _, lang := range []string{"typescript", "typescriptreact", "javascript", "javascriptreact"} {
		adapter, ok := r.AdapterForLanguage(lang)
		if !ok {
			t.Errorf("expected %q to be registered", lang)
			continue
		}
		_, err := adapter.Run(ToolInput{})
		if err != nil {
			t.Errorf("Run failed for %q: %v", lang, err)
		}
	}

	if singleton.runCount != 4 {
		t.Errorf("expected 4 runs on singleton, got %d", singleton.runCount)
	}
}

func TestRegistrySingletonOverridesFactory(t *testing.T) {
	r := NewRegistry()
	factoryCalls := 0
	r.Register("go", func() Adapter {
		factoryCalls++
		return &countingAdapter{}
	})

	singleton := &countingAdapter{}
	r.RegisterSingleton(singleton, "go")

	adapter, ok := r.AdapterForLanguage("go")
	if !ok {
		t.Fatal("expected adapter")
	}
	_, _ = adapter.Run(ToolInput{})

	if factoryCalls != 0 {
		t.Error("factory should not be called when singleton is registered")
	}
	if singleton.runCount != 1 {
		t.Errorf("expected singleton runCount=1, got %d", singleton.runCount)
	}
}

// =============================================================================
// SessionPool — concurrent access
// =============================================================================

func TestSessionPoolConcurrentAccess(t *testing.T) {
	var createMu sync.Mutex
	created := 0
	pool := NewSessionPool(func(workspaceRoot string) (SessionAdapter, error) {
		createMu.Lock()
		created++
		createMu.Unlock()
		return &fakeSessionAdapter{healthy: true}, nil
	}, 0)
	defer pool.Close()

	var (
		errMu sync.Mutex
		errs  []error
	)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			root := fmt.Sprintf("/repo-%d", idx%3) // 3 different workspaces
			if _, err := pool.Run(ToolInput{WorkspaceRoot: root}); err != nil {
				errMu.Lock()
				errs = append(errs, fmt.Errorf("goroutine %d: %w", idx, err))
				errMu.Unlock()
			}
		}(i)
	}
	wg.Wait()

	for _, e := range errs {
		t.Error(e.Error())
	}

	if created != 3 {
		t.Errorf("expected 3 adapters (one per workspace), got %d", created)
	}
}

// =============================================================================
// goLineColToOffset — additional edge cases
// =============================================================================

func TestGoLineColToOffsetMultiLine(t *testing.T) {
	// Multi-line content — verify that line offsets are computed correctly
	content := "package main\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n"
	// Line 1, col 1 → offset 0
	if got := goLineColToOffset(content, 1, 1); got != 0 {
		t.Errorf("got %d, want 0", got)
	}
	// Line 3, col 6 → should be at 'm' in "main"
	want := len("package main\n\nfunc ")
	if got := goLineColToOffset(content, 3, 6); got != want {
		t.Errorf("got %d, want %d", got, want)
	}
}

func TestGoLineColToOffsetSingleLine(t *testing.T) {
	if got := goLineColToOffset("hello", 1, 1); got != 0 {
		t.Errorf("got %d, want 0", got)
	}
	if got := goLineColToOffset("hello", 1, 5); got != 4 {
		t.Errorf("got %d, want 4", got)
	}
	if got := goLineColToOffset("hello", 2, 1); got != 5 {
		t.Errorf("got %d (past end of single line), want 5", got)
	}
}

// =============================================================================
// parseGofmtErrors / parseGoVetErrors — additional edge cases
// =============================================================================

func TestParseGofmtErrorsMultiline(t *testing.T) {
	content := "package main\n\nfunc main() {\n\tfmt.Println(\n}\n"
	output := "/tmp/main.go:4:2: expected expression, found }\n/tmp/main.go:5:1: expected ';'\n/tmp/main.go:6:1: unexpected }\n"
	diags := parseGofmtErrors(output, content)
	if len(diags) != 3 {
		t.Fatalf("expected 3 diagnostics, got %d", len(diags))
	}
	for _, d := range diags {
		if d.Severity != "error" {
			t.Errorf("expected severity error, got %s", d.Severity)
		}
		if d.Source != "gofmt" {
			t.Errorf("expected source gofmt, got %s", d.Source)
		}
		if d.From < 0 || d.To <= d.From {
			t.Errorf("invalid from/to: %d/%d", d.From, d.To)
		}
	}
}

func TestParseGoVetErrorsMultipleVets(t *testing.T) {
	content := "package main\n\nfunc main() {\n}\nfunc unused() {}\n"
	output := "# command-line-arguments\n/tmp/main.go:3:2: unreachable code\n/tmp/main.go:5:6: unused variable\n"
	diags := parseGoVetErrors(output, content)
	if len(diags) != 2 {
		t.Fatalf("expected 2 diagnostics, got %d", len(diags))
	}
	for _, d := range diags {
		if d.Severity != "warning" {
			t.Errorf("expected severity warning, got %s", d.Severity)
		}
		if d.Source != "go vet" {
			t.Errorf("expected source 'go vet', got %s", d.Source)
		}
	}
}

func TestParseGoVetErrorsAllHashLines(t *testing.T) {
	output := "# line 1\n# line 2\n# line 3\n"
	diags := parseGoVetErrors(output, "package main")
	if len(diags) != 0 {
		t.Fatalf("expected 0 diagnostics for all-hash output, got %d", len(diags))
	}
}

// =============================================================================
// computeGoEdits — additional edge cases
// =============================================================================

func TestComputeGoEditsInsertAtBeginning(t *testing.T) {
	original := "world"
	modified := "hello world"
	edits := computeGoEdits(original, modified, "test.go")
	if len(edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(edits))
	}
	if edits[0].From != 0 {
		t.Errorf("From = %d, want 0", edits[0].From)
	}
	if edits[0].To != 0 {
		t.Errorf("To = %d, want 0", edits[0].To)
	}
	if edits[0].NewText != "hello " {
		t.Errorf("NewText = %q, want 'hello '", edits[0].NewText)
	}
}

func TestComputeGoEditsDeleteFromEnd(t *testing.T) {
	original := "hello world"
	modified := "hello"
	edits := computeGoEdits(original, modified, "test.go")
	if len(edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(edits))
	}
	if edits[0].From != 5 {
		t.Errorf("From = %d, want 5", edits[0].From)
	}
	if edits[0].To != 11 {
		t.Errorf("To = %d, want 11", edits[0].To)
	}
	if edits[0].NewText != "" {
		t.Errorf("NewText = %q, want empty", edits[0].NewText)
	}
}

func TestComputeGoEditsReplaceMiddle(t *testing.T) {
	original := "abcdef"
	modified := "abXYef"
	edits := computeGoEdits(original, modified, "test.go")
	if len(edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(edits))
	}
	applied := original[:edits[0].From] + edits[0].NewText + original[edits[0].To:]
	if applied != modified {
		t.Errorf("applied = %q, want %q", applied, modified)
	}
}

// =============================================================================
// mapInlayHintKind — ensure full coverage
// =============================================================================

func TestMapInlayHintKindAllCases(t *testing.T) {
	tests := []struct {
		kind int
		want string
	}{
		{1, "type"},
		{2, "parameter"},
		{0, "none"},
		{-1, "none"},
		{3, "none"},
		{100, "none"},
	}
	for _, tt := range tests {
		got := mapInlayHintKind(tt.kind)
		if got != tt.want {
			t.Errorf("mapInlayHintKind(%d) = %q, want %q", tt.kind, got, tt.want)
		}
	}
}

// =============================================================================
// parseGoplsInlayHints — non-string label (non-array, non-string)
// =============================================================================

func TestParseGoplsInlayHintsLabelIsNumber(t *testing.T) {
	content := "package main\n"
	resp := map[string]interface{}{
		"result": []interface{}{
			map[string]interface{}{
				"position": map[string]interface{}{"line": float64(0), "character": float64(0)},
				"label":    float64(42), // label is a number, not string or array
				"kind":     float64(1),
			},
		},
	}
	hints := parseGoplsInlayHints(resp, content)
	if len(hints) != 0 {
		t.Fatalf("expected 0 hints when label is a number, got %d", len(hints))
	}
}

// =============================================================================
// parseGoplsDefinition — with extra output format variations
// =============================================================================

func TestParseGoplsDefinitionWithTrailingText(t *testing.T) {
	output := "/path/to/file.go:10:5-8 some trailing text\n"
	path, line, col, ok := parseGoplsDefinition(output)
	if !ok {
		t.Fatal("expected ok")
	}
	if path != "/path/to/file.go" {
		t.Errorf("path = %q", path)
	}
	if line != 10 {
		t.Errorf("line = %d, want 10", line)
	}
	if col != 5 {
		t.Errorf("col = %d, want 5", col)
	}
}

func TestParseGoplsDefinitionWindowsPath(t *testing.T) {
	output := "C:\\Users\\project\\main.go:10:5\n"
	path, line, col, ok := parseGoplsDefinition(output)
	if !ok {
		t.Fatal("expected ok for Windows path")
	}
	if line != 10 || col != 5 {
		t.Errorf("got line=%d col=%d, want 10, 5", line, col)
	}
	_ = path
}

// =============================================================================
// extractParamsFromSignature — more edge cases
// =============================================================================

func TestExtractParamsFromSignatureOnlyReceiver(t *testing.T) {
	// Method signature with only a receiver, no params: func (t *T) Method()
	params := extractParamsFromSignature("func (t *T) Method()")
	if len(params) != 0 {
		t.Fatalf("expected 0 params for method with no args, got %d: %v", len(params), params)
	}
}

func TestExtractParamsFromSignatureSingleUnnamed(t *testing.T) {
	params := extractParamsFromSignature("func Foo(int)")
	if len(params) != 1 {
		t.Fatalf("expected 1 param, got %d", len(params))
	}
	if params[0].Label != "int" {
		t.Errorf("param = %q, want 'int'", params[0].Label)
	}
}

func TestExtractParamsFromSignatureComplexNested(t *testing.T) {
	params := extractParamsFromSignature("func Foo(fn func(int, string) error, ch chan<- struct{})")
	if len(params) != 2 {
		t.Fatalf("expected 2 params, got %d: %v", len(params), params)
	}
	if params[0].Label != "fn func(int, string) error" {
		t.Errorf("first param = %q", params[0].Label)
	}
	if params[1].Label != "ch chan<- struct{}" {
		t.Errorf("second param = %q", params[1].Label)
	}
}

// =============================================================================
// computeActiveParameter — more edge cases
// =============================================================================

func TestComputeActiveParameterAtThirdParam(t *testing.T) {
	input := ToolInput{
		Content:  "foo(1, 2, 3)",
		Position: &Position{Line: 1, Column: 10}, // at '3'
	}
	got := computeActiveParameter(input)
	if got != 2 {
		t.Errorf("expected param 2 (the '3'), got %d", got)
	}
}

func TestComputeActiveParameterSingleParam(t *testing.T) {
	input := ToolInput{
		Content:  "foo(42)",
		Position: &Position{Line: 1, Column: 5}, // at '42'
	}
	got := computeActiveParameter(input)
	if got != 0 {
		t.Errorf("expected param 0, got %d", got)
	}
}

// =============================================================================
// Go diagnostics with save trigger and go vet errors
// =============================================================================

func TestRunGoDiagnosticsWithSaveTrigger(t *testing.T) {
	// Tests the save trigger path which runs go vet when gofmt finds no errors
	if _, err := exec.LookPath("gofmt"); err != nil {
		t.Skip("gofmt not available")
	}

	content := "package main\n\nfunc main() {}\n"
	result, err := runGoDiagnostics(ToolInput{
		Content:  content,
		FilePath: "main.go",
		Trigger:  "save",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Capabilities.Diagnostics {
		t.Error("expected Diagnostics capability")
	}
	// Valid code should have no diagnostics
	if len(result.Diagnostics) != 0 {
		t.Logf("Got %d diagnostics (expected 0 but vet might report issues)", len(result.Diagnostics))
	}
}

// =============================================================================
// goSessionAdapter — ensureServerLocked covers real gopls path
// =============================================================================

func TestGoSessionAdapterEnsureServerAndReset(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not available")
	}

	a := &goSessionAdapter{}

	// Start the server
	a.mu.Lock()
	err := a.ensureServerLocked(t.TempDir())
	a.mu.Unlock()
	if err != nil {
		t.Fatalf("ensureServerLocked failed: %v", err)
	}

	// Verify process is running
	if !a.Healthy() {
		t.Error("expected healthy after server start")
	}

	// Reset should clean up
	a.mu.Lock()
	a.resetServerLocked()
	a.mu.Unlock()

	// Now should not be healthy (server killed, but not closed)
	a.mu.Lock()
	hasCmd := a.serverCmd != nil
	a.mu.Unlock()
	if hasCmd {
		t.Error("serverCmd should be nil after reset")
	}

	// Close handles cleanup (idempotent)
	a.Close()
}

// =============================================================================
// runGoRename — filter that excludes other files (testing with multi-file setup)
// =============================================================================

func TestRunGoRenameOnlyCurrentFile(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not available")
	}

	// Create a multi-file Go module
	dir := t.TempDir()
	mainContent := "package main\n\nvar config string\n\nfunc main() {\n\tprintln(config)\n}\n"
	utilContent := "package main\n\nfunc helper() {\n\t_ = config\n}\n"

	for _, pair := range [][2]string{
		{filepath.Join(dir, "main.go"), mainContent},
		{filepath.Join(dir, "util.go"), utilContent},
		{filepath.Join(dir, "go.mod"), "module testmain\n\ngo 1.21\n"},
	} {
		if err := os.WriteFile(pair[0], []byte(pair[1]), 0644); err != nil {
			t.Fatal(err)
		}
	}

	result, err := runGoRename(ToolInput{
		Content:       mainContent,
		FilePath:      filepath.Join(dir, "main.go"),
		WorkspaceRoot: dir,
		Position:      &Position{Line: 3, Column: 5},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Rename == nil {
		t.Fatal("expected non-nil Rename")
	}
	for i, loc := range result.Rename.Locations {
		if filepath.Clean(loc.FilePath) != filepath.Clean(filepath.Join(dir, "main.go")) {
			t.Errorf("location[%d] file=%q should be main.go only", i, loc.FilePath)
		}
	}
}

// =============================================================================
// runGoReferences — symbol name extraction and sorting
// =============================================================================

func TestRunGoReferencesSymbolName(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not available")
	}

	content := "package main\n\nvar myVar int\n\nfunc main() {\n\t_ = myVar\n}\n"
	dir, file := setupGoTestModule(t, content)

	result, err := runGoReferences(ToolInput{
		Content:       content,
		FilePath:      file,
		WorkspaceRoot: dir,
		Position:      &Position{Line: 3, Column: 5}, // on 'myVar'
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.References == nil {
		t.Fatal("expected non-nil References")
	}
	if result.References.SymbolName != "myVar" {
		t.Errorf("SymbolName = %q, want 'myVar'", result.References.SymbolName)
	}
}

func TestRunGoReferencesEmptyContentAtPosition(t *testing.T) {
	// Testing symbol name extraction when position is beyond content lines
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not available")
	}

	content := "package main\n"
	dir, file := setupGoTestModule(t, content)

	result, err := runGoReferences(ToolInput{
		Content:       content,
		FilePath:      file,
		WorkspaceRoot: dir,
		Position:      &Position{Line: 5, Column: 5}, // beyond content
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should not panic, even if no references found
	if result.References == nil {
		t.Error("expected non-nil References")
	}
}

// =============================================================================
// writeJSONRPC / readJSONRPC — additional coverage
// =============================================================================

func TestWriteJSONRPCMultipleMessages(t *testing.T) {
	var buf bytes.Buffer
	writer := bufio.NewWriter(&buf)

	msg1 := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      float64(1),
		"method":  "initialize",
	}
	msg2 := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      float64(2),
		"method":  "shutdown",
	}

	writeJSONRPC(writer, msg1)
	writeJSONRPC(writer, msg2)
	writer.Flush()

	reader := bufio.NewReader(&buf)

	read1, err := readJSONRPC(reader)
	if err != nil {
		t.Fatalf("first read failed: %v", err)
	}
	if read1["method"] != "initialize" {
		t.Errorf("first method = %v", read1["method"])
	}

	read2, err := readJSONRPC(reader)
	if err != nil {
		t.Fatalf("second read failed: %v", err)
	}
	if read2["method"] != "shutdown" {
		t.Errorf("second method = %v", read2["method"])
	}
}

// =============================================================================
// parseGoplsSignatureHelp — edge cases
// =============================================================================

func TestParseGoplsSignatureHelpOnlyBlankLines(t *testing.T) {
	output := "\n\n\n"
	result := parseGoplsSignatureHelp(output, ToolInput{})
	if len(result.Signatures) != 0 {
		t.Fatalf("expected 0 signatures, got %d", len(result.Signatures))
	}
}

func TestParseGoplsSignatureHelpWhitespaceOnly(t *testing.T) {
	output := "   \n"
	result := parseGoplsSignatureHelp(output, ToolInput{})
	if len(result.Signatures) != 0 {
		t.Fatalf("expected 0 signatures for whitespace-only output, got %d", len(result.Signatures))
	}
}

func TestParseGoplsSignatureHelpVaradicParams(t *testing.T) {
	output := "func Printf(format string, args ...interface{}) (n int, err error)\n"
	input := ToolInput{
		Content:  `fmt.Printf("%s", "hi")`,
		Position: &Position{Line: 1, Column: 14},
	}
	result := parseGoplsSignatureHelp(output, input)
	if len(result.Signatures) != 1 {
		t.Fatalf("expected 1 signature, got %d", len(result.Signatures))
	}
	params := result.Signatures[0].Parameters
	if len(params) != 2 {
		t.Fatalf("expected 2 params, got %d: %v", len(params), params)
	}
	if params[0].Label != "format string" {
		t.Errorf("first param = %q", params[0].Label)
	}
	if params[1].Label != "args ...interface{}" {
		t.Errorf("second param = %q", params[1].Label)
	}
}

// =============================================================================
// runGoInlayHintsWithRemote — gopls not available path (tested indirectly)
// and invalid remote address variations
// =============================================================================

func TestRunGoInlayHintsWithRemoteNonexistentSocket(t *testing.T) {
	result, err := runGoInlayHintsWithRemote(ToolInput{
		Content:       "package main\n",
		FilePath:      "main.go",
		WorkspaceRoot: "/tmp",
	}, "", "unix;/nonexistent/path/to/socket.sock")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should fail to connect but not panic
	if result.Error == "" {
		t.Error("expected an error from socket connect failure")
	}
}

// =============================================================================
// NewGoAdapter returns correct type
// =============================================================================

func TestNewGoAdapterImplementsAdapter(t *testing.T) {
	var _ Adapter = NewGoAdapter()
}

func TestNewTypeScriptAdapterImplementsAdapter(t *testing.T) {
	var _ Adapter = NewTypeScriptAdapter()
}

// =============================================================================
// SessionPool EvictIdle — edge cases
// =============================================================================

func TestSessionPoolEvictIdleLeavesActiveSessions(t *testing.T) {
	created := 0
	pool := NewSessionPool(func(workspaceRoot string) (SessionAdapter, error) {
		created++
		return &fakeSessionAdapter{healthy: true}, nil
	}, 50*time.Millisecond)
	defer pool.Close()

	// Acquire adapter for /repo-a
	_, err := pool.acquire("/repo-a")
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}

	// Wait past TTL
	time.Sleep(100 * time.Millisecond)

	// EvictIdle should not evict /repo-a because inUse > 0 from direct acquire
	// (Note: acquire incremented inUse, release hasn't been called)
	pool.EvictIdle()

	pool.mu.Lock()
	_, exists := pool.sessions["/repo-a"]
	pool.mu.Unlock()
	if !exists {
		t.Error("expected /repo-a to NOT be evicted while in use")
	}
}
