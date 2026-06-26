//go:build !js

package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func newSymbolsTestServer(t *testing.T, files map[string]string) *ReactWebServer {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return &ReactWebServer{
		workspaceRoot:   root,
		daemonRoot:      root,
		terminalManager: NewTerminalManager(root),
	}
}

type symbolsResponse struct {
	Message string             `json:"message"`
	Files   []fileSymbolsEntry `json:"files"`
	Total   int                `json:"total"`
}

type fileSymbolsEntry struct {
	File    string        `json:"file"`
	Symbols []symbolEntry `json:"symbols"`
}

type symbolEntry struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	Line int    `json:"line,omitempty"`
}

func doGetSymbols(t *testing.T, ws *ReactWebServer, query string) symbolsResponse {
	t.Helper()
	target := "/api/workspace/symbols"
	if query != "" {
		target += "?query=" + url.QueryEscape(query)
	}
	req := httptest.NewRequest(http.MethodGet, target, nil)
	rec := httptest.NewRecorder()
	ws.handleAPIWorkspaceSymbols(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp symbolsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	return resp
}

// TestAPIWorkspaceSymbolsNoQueryReturnsAllSymbols verifies that GET /api/workspace/symbols
// with no query parameter returns all symbols from workspace files.
func TestAPIWorkspaceSymbolsNoQueryReturnsAllSymbols(t *testing.T) {
	ws := newSymbolsTestServer(t, map[string]string{
		"main.go": "package main\n\nfunc Hello() {}\nfunc World() {}\n",
		"util.go": "package main\n\nfunc Helper() {}\n",
	})

	resp := doGetSymbols(t, ws, "")

	if resp.Message != "ok" {
		t.Fatalf("expected message %q, got %q", "ok", resp.Message)
	}
	if resp.Total != 3 {
		t.Fatalf("expected 3 total symbols, got %d", resp.Total)
	}
	if len(resp.Files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(resp.Files))
	}

	// Collect all symbol names
	names := map[string]bool{}
	for _, f := range resp.Files {
		for _, s := range f.Symbols {
			names[s.Name] = true
		}
	}
	for _, expected := range []string{"Hello", "World", "Helper"} {
		if !names[expected] {
			t.Errorf("expected symbol %q in results", expected)
		}
	}
}

// TestAPIWorkspaceSymbolsQueryFiltersResults verifies that GET /api/workspace/symbols?query=xxx
// returns only files containing matching symbol names.
func TestAPIWorkspaceSymbolsQueryFiltersResults(t *testing.T) {
	ws := newSymbolsTestServer(t, map[string]string{
		"main.go": "package main\n\nfunc Hello() {}\nfunc World() {}\n",
		"util.go": "package main\n\nfunc Helper() {}\nfunc CalculateSum() {}\n",
	})

	resp := doGetSymbols(t, ws, "Helper")

	if resp.Message != "ok" {
		t.Fatalf("expected message %q, got %q", "ok", resp.Message)
	}
	if len(resp.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(resp.Files))
	}
	if filepath.Base(resp.Files[0].File) != "util.go" {
		t.Fatalf("expected util.go, got %s", resp.Files[0].File)
	}
	if len(resp.Files[0].Symbols) < 1 {
		t.Fatal("expected at least 1 symbol in util.go")
	}

	found := false
	for _, s := range resp.Files[0].Symbols {
		if s.Name == "Helper" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected to find Helper symbol")
	}
}

// TestAPIWorkspaceSymbolsResponseStructure verifies the response has the correct
// JSON structure: {message, files: [{file, symbols: [{name, kind}]}], total}.
func TestAPIWorkspaceSymbolsResponseStructure(t *testing.T) {
	ws := newSymbolsTestServer(t, map[string]string{
		"app.go": "package main\n\nfunc ProcessData() {}\n",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/workspace/symbols", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIWorkspaceSymbols(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify Content-Type
	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Fatalf("expected Content-Type application/json, got %q", ct)
	}

	// Decode as raw map for structural validation
	var raw map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Check top-level keys
	for _, key := range []string{"message", "files", "total"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("response missing key %q", key)
		}
	}

	// Check files structure
	files, ok := raw["files"].([]interface{})
	if !ok {
		t.Fatal("files is not an array")
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file entry, got %d", len(files))
	}

	file := files[0].(map[string]interface{})
	for _, key := range []string{"file", "symbols"} {
		if _, ok := file[key]; !ok {
			t.Errorf("file entry missing key %q", key)
		}
	}

	symbols, ok := file["symbols"].([]interface{})
	if !ok {
		t.Fatal("symbols is not an array")
	}
	if len(symbols) != 1 {
		t.Fatalf("expected 1 symbol, got %d", len(symbols))
	}

	symbol := symbols[0].(map[string]interface{})
	for _, key := range []string{"name", "kind"} {
		if _, ok := symbol[key]; !ok {
			t.Errorf("symbol entry missing key %q", key)
		}
	}
}

// TestAPIWorkspaceSymbolsMethodNotAllowed verifies that POST /api/workspace/symbols
// returns 405 Method Not Allowed.
func TestAPIWorkspaceSymbolsMethodNotAllowed(t *testing.T) {
	root := t.TempDir()
	ws := &ReactWebServer{
		workspaceRoot:   root,
		daemonRoot:      root,
		terminalManager: NewTerminalManager(root),
	}

	req := httptest.NewRequest(http.MethodPost, "/api/workspace/symbols", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIWorkspaceSymbols(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestAPIWorkspaceSymbolsMultipleTokensNarrowResults verifies that a query with
// multiple space-separated tokens triggers OR matching across files.
// Note: SearchSymbols uses OR logic — a file matches if ANY of its symbols
// contains ANY of the query tokens (case-insensitive, tokens >= 3 chars).
func TestAPIWorkspaceSymbolsMultipleTokensNarrowResults(t *testing.T) {
	ws := newSymbolsTestServer(t, map[string]string{
		"main.go":  "package main\n\nfunc PrintMessage() {}\nfunc PrintLine() {}\n",
		"util.go":  "package main\n\nfunc CalculateSum() {}\nfunc CalculateAvg() {}\n",
		"extra.go": "package main\n\nfunc Greeter() {}\n",
	})

	// Single token "Pri" (3+ chars required) matches main.go only (PrintMessage, PrintLine)
	respSingle := doGetSymbols(t, ws, "Pri")
	if len(respSingle.Files) != 1 {
		t.Fatalf("single token: expected 1 file, got %d", len(respSingle.Files))
	}
	if filepath.Base(respSingle.Files[0].File) != "main.go" {
		t.Fatalf("single token: expected main.go, got %s", respSingle.Files[0].File)
	}

	// Multiple tokens "Pri Cal" — OR logic: matches files for either token.
	// main.go matches "Pri" (PrintMessage), util.go matches "Cal" (CalculateSum).
	respMulti := doGetSymbols(t, ws, "Pri Cal")
	if respMulti.Total == 0 {
		t.Fatal("multi-token: expected non-zero results")
	}

	fileSet := map[string]bool{}
	for _, f := range respMulti.Files {
		fileSet[filepath.Base(f.File)] = true
	}
	if !fileSet["main.go"] {
		t.Error("multi-token: expected main.go to match token 'Pri'")
	}
	if !fileSet["util.go"] {
		t.Error("multi-token: expected util.go to match token 'Cal'")
	}
	// extra.go should NOT match any token
	if fileSet["extra.go"] {
		t.Error("multi-token: did not expect extra.go to match")
	}

	// Unrecognized token should match nothing
	respNone := doGetSymbols(t, ws, "xyz")
	if respNone.Total != 0 {
		t.Fatalf("non-matching token: expected 0 results, got %d", respNone.Total)
	}
}

// TestAPIWorkspaceSymbolsEmptyWorkspace verifies behavior with no .go files.
func TestAPIWorkspaceSymbolsEmptyWorkspace(t *testing.T) {
	ws := newSymbolsTestServer(t, map[string]string{
		"readme.txt": "not a source file\n",
	})

	resp := doGetSymbols(t, ws, "")
	if resp.Message != "ok" {
		t.Fatalf("expected message %q, got %q", "ok", resp.Message)
	}
	if resp.Total != 0 {
		t.Fatalf("expected 0 symbols, got %d", resp.Total)
	}
	if len(resp.Files) != 0 {
		t.Fatalf("expected 0 files, got %d", len(resp.Files))
	}
}

// TestAPIWorkspaceSymbolsReturnsLineNumbers verifies that line numbers are returned
// in the workspace symbol API response.
func TestAPIWorkspaceSymbolsReturnsLineNumbers(t *testing.T) {
	ws := newSymbolsTestServer(t, map[string]string{
		"main.go": "package main\n\nfunc Hello() {}\nfunc World() {}\n",
	})

	resp := doGetSymbols(t, ws, "")

	// Verify line numbers in results
	for _, f := range resp.Files {
		for _, s := range f.Symbols {
			if s.Line == 0 {
				t.Errorf("symbol %q in %s: expected non-zero line number", s.Name, f.File)
			}
		}
	}

	// Find Hello and verify it's at line 3
	for _, f := range resp.Files {
		for _, s := range f.Symbols {
			if s.Name == "Hello" {
				if s.Line != 3 {
					t.Errorf("Hello: expected line 3, got %d", s.Line)
				}
			}
			if s.Name == "World" {
				if s.Line != 4 {
					t.Errorf("World: expected line 4, got %d", s.Line)
				}
			}
		}
	}
}
