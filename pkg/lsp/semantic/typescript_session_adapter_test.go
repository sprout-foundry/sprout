package semantic

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTypeScriptSessionAdapterHealthyWhenFresh(t *testing.T) {
	a := &typeScriptSessionAdapter{}
	// No process started, so Healthy should be false
	if a.Healthy() {
		t.Error("fresh adapter with no process should not be healthy (no cmd)")
	}
}

func TestTypeScriptSessionAdapterClose(t *testing.T) {
	a := &typeScriptSessionAdapter{}
	if err := a.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if !a.closed {
		t.Error("expected closed to be true")
	}
}

func TestTypeScriptSessionAdapterRunWhenClosed(t *testing.T) {
	a := &typeScriptSessionAdapter{}
	a.Close()
	_, err := a.Run(ToolInput{Method: "diagnostics"})
	if err == nil {
		t.Error("expected error when running on closed adapter")
	}
}

func TestNewTypeScriptSessionPool(t *testing.T) {
	pool := NewTypeScriptSessionPool(5 * time.Minute)
	if pool == nil {
		t.Fatal("expected non-nil pool")
	}
	pool.Close()
}

// The persistent worker script is embedded in a Go raw string. A raw string
// cannot express "\n" via an escape, so "\\n" in the Go source becomes a
// literal backslash-n in the JavaScript — the worker then writes responses
// terminated by the two characters \ and n instead of a newline byte, and the
// adapter's ReadBytes('\n') blocks until workerReadTimeout on every request.
// Commit 617c8c8c4 introduced exactly that regression and nothing caught it:
// every real worker round-trip silently degraded to a 2-minute stall. Guard
// the script text so it can't come back.
func TestTypeScriptWorkerScriptTerminatesLines(t *testing.T) {
	if strings.Contains(typeScriptNodeWorkerScript, `'\\n'`) {
		t.Fatal("worker script writes literal backslash-n: JSON.stringify(out) + '\\\\n' must be + '\\n' (one backslash) so responses end in a newline byte")
	}
	if !strings.Contains(typeScriptNodeWorkerScript, `JSON.stringify(out) + '\n'`) {
		t.Fatal("worker script must terminate stdout responses with a newline byte")
	}
}

// End-to-end round-trip through the persistent worker: a diagnostics request
// must answer well within workerReadTimeout. Before the '\\n' fix this hung
// for the full 120s timeout and returned EOF.
func TestTypeScriptSessionAdapterWorkerRoundTrip(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available")
	}

	dir := t.TempDir()
	tsPkg := filepath.Join(dir, "node_modules", "typescript")
	if err := os.MkdirAll(tsPkg, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tsPkg, "package.json"), []byte(`{"name":"typescript","version":"0.0.0-fake","main":"index.js"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// Minimal fake module: the analyzer only needs the API surface used by
	// createSession when no tsconfig.json is present (defaults path).
	// Raw string: '\n' must reach node as backslash-n, not a raw newline
	// (a literal newline inside a JS string literal is a SyntaxError).
	lib := `module.exports = {
  ScriptTarget:{ESNext:99}, ModuleKind:{ESNext:99}, ModuleResolutionKind:{NodeJs:1},
  JsxEmit:{ReactJSX:4},
  DiagnosticCategory:{Error:1,Warning:2,Suggestion:3,Message:4},
  ScriptSnapshot:{fromString:function(){return {}}},
  getDefaultLibFilePath:function(){return 'lib.d.ts'},
  createLanguageService:function(){return {getSyntacticDiagnostics:function(){return []}, getSemanticDiagnostics:function(){return []}}},
  createDocumentRegistry:function(){return {}},
  findConfigFile:function(){return undefined},
  parseConfigFileTextToJson:function(){return {}},
  parseJsonConfigFileContent:function(){return {options:{}, fileNames:[]}},
  flattenDiagnosticMessageText:function(m){return String(m)},
  sys:{fileExists:function(){return false}, readFile:function(){return undefined}, readDirectory:function(){return []}, directoryExists:function(){return false}, getDirectories:function(){return []}, useCaseSensitiveFileNames:true, newLine:'\n'}
};`
	if err := os.WriteFile(filepath.Join(tsPkg, "index.js"), []byte(lib), 0o644); err != nil {
		t.Fatal(err)
	}

	a := &typeScriptSessionAdapter{}
	defer a.Close()

	done := make(chan struct{})
	var result ToolResult
	var runErr error
	go func() {
		defer close(done)
		result, runErr = a.Run(ToolInput{
			Method:        "diagnostics",
			Content:       "const x = 1;\n",
			FilePath:      filepath.Join(dir, "a.ts"),
			WorkspaceRoot: dir,
		})
	}()

	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatalf("worker round-trip did not complete within 30s (pre-fix symptom: %v stall)", workerReadTimeout)
	}
	if runErr != nil {
		t.Fatalf("worker run failed: %v", runErr)
	}
	if !result.Capabilities.Diagnostics {
		t.Fatalf("expected diagnostics capability, got %+v error=%q (stderr: %q)", result.Capabilities, result.Error, a.stderr.String())
	}
}
