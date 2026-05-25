//go:build js && wasm

package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"syscall/js"

	"github.com/sprout-foundry/sprout/pkg/ast"
)

// ─── JS Function Registry ────────────────────────────────────────

// astJSFuncs returns the code-intelligence entries that main.go merges
// into the SproutWasm global. Provides synchronous parsing and symbol
// extraction powered by tree-sitter via pkg/ast.
func astJSFuncs() map[string]interface{} {
	return map[string]interface{}{
		"parseFile":          js.FuncOf(parseFileFunc),
		"extractSymbols":     js.FuncOf(extractSymbolsFunc),
		"supportedLanguages": js.FuncOf(supportedLanguagesFunc),
	}
}

// ─── parseFile ───────────────────────────────────────────────────

// parseFileFunc wraps ast.ParseFile for JavaScript.
// Synchronous — tree-sitter parsing on typical source files completes in
// milliseconds, so there is no need for a Promise wrapper.
// For files >1MB, consider chunking or using a web worker to avoid
// blocking the browser main thread.
//
// Returns JSON: {"language": "...", "filePath": "...", "symbols": [...]}
// On error: {"error": "..."}.
func parseFileFunc(_ js.Value, args []js.Value) interface{} {
	path := argString(args, 0, "")
	if path == "" {
		return marshalJS(map[string]interface{}{"error": "filePath is required"})
	}

	content, err := copyBytesFromJS(args, 1)
	if err != nil {
		return marshalJS(map[string]interface{}{"error": err.Error()})
	}

	result, err := ast.ParseFile(path, content)
	if err != nil {
		return marshalJS(map[string]interface{}{"error": err.Error()})
	}
	defer result.Release()

	syms := make([]map[string]interface{}, len(result.Symbols))
	for i, s := range result.Symbols {
		syms[i] = map[string]interface{}{
			"name":      s.Name,
			"kind":      s.Kind,
			"startLine": s.StartLine,
			"endLine":   s.EndLine,
			"startByte": s.StartByte,
			"endByte":   s.EndByte,
		}
	}

	return marshalJS(map[string]interface{}{
		"language": result.Language,
		"filePath": result.FilePath,
		"symbols":  syms,
	})
}

// ─── extractSymbols ──────────────────────────────────────────────

// extractSymbolsFunc parses source content and returns scoped symbols with
// nesting information.  Accepts the file path (string) and content
// (Uint8Array or ArrayBuffer).  Synchronous.
//
// Returns JSON array of symbols:
//   [{name, kind, scope, depth, startLine, endLine, startByte, endByte}]
//
// On error: {"error": "..."}.
func extractSymbolsFunc(_ js.Value, args []js.Value) interface{} {
	path := argString(args, 0, "")
	if path == "" {
		return marshalJS(map[string]interface{}{"error": "filePath is required"})
	}

	content, err := copyBytesFromJS(args, 1)
	if err != nil {
		return marshalJS(map[string]interface{}{"error": err.Error()})
	}

	result, err := ast.ParseFile(path, content)
	if err != nil {
		return marshalJS(map[string]interface{}{"error": err.Error()})
	}
	defer result.Release()

	scoped := ast.ExtractSymbols(result.Root, result.Bound, result.Language)

	syms := make([]map[string]interface{}, len(scoped))
	for i, s := range scoped {
		syms[i] = map[string]interface{}{
			"name":      s.Name,
			"kind":      s.Kind,
			"scope":     s.Scope,
			"depth":     s.Depth,
			"startLine": s.StartLine,
			"endLine":   s.EndLine,
			"startByte": s.StartByte,
			"endByte":   s.EndByte,
		}
	}

	return marshalJS(syms)
}

// ─── supportedLanguages ──────────────────────────────────────────

// supportedLanguagesFunc returns the sorted list of languages that the
// tree-sitter grammar cache supports.  Synchronous, no arguments needed.
//
// Returns JSON array: ["go", "javascript", "python", "tsx", "typescript"].
func supportedLanguagesFunc(_ js.Value, _ []js.Value) interface{} {
	names := make([]string, 0, len(ast.SupportedLanguages))
	for lang := range ast.SupportedLanguages {
		names = append(names, lang)
	}
	sort.Strings(names)
	return marshalJS(names)
}

// ─── Helpers ─────────────────────────────────────────────────────

// copyBytesFromJS extracts byte content from a JS argument at position idx.
// Accepts either an ArrayBuffer (wrapped in Uint8Array) or a typed array
// (Uint8Array).  Returns an empty slice for zero-length input; errors only
// for missing/null arguments or unsupported types.
func copyBytesFromJS(args []js.Value, idx int) ([]byte, error) {
	if idx >= len(args) {
		return nil, fmt.Errorf("missing byte argument at index %d", idx)
	}
	src := args[idx]
	if src.IsNull() || src.IsUndefined() {
		return nil, fmt.Errorf("byte argument at index %d is null", idx)
	}

	// Accept either an ArrayBuffer (wrap in Uint8Array) or a typed array.
	if src.InstanceOf(js.Global().Get("ArrayBuffer")) {
		src = js.Global().Get("Uint8Array").New(src)
	}

	// Validate that src is actually a Uint8Array before copying.
	if !src.InstanceOf(js.Global().Get("Uint8Array")) {
		return nil, fmt.Errorf("byte argument at index %d: expected Uint8Array or ArrayBuffer, got %s", idx, src.Type().String())
	}

	length := src.Get("length").Int()
	if length == 0 {
		return []byte{}, nil // Allow empty content — parser handles it
	}

	buf := make([]byte, length)
	copied := js.CopyBytesToGo(buf, src)
	if copied != length {
		return nil, fmt.Errorf("byte copy short: %d of %d bytes", copied, length)
	}
	return buf, nil
}

// Ensure marshalJS works for our simple map types by verifying at init.
// (No-op compile-time sanity check — we go through JSON round-trip so
// struct types aren't needed, but this guards against accidental type
// mismatches.)
var _ = marshalJS(json.RawMessage{})
