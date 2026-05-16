//go:build !(js && wasm)

// Package ast (continued) — BrowserCache stub for non-WASM builds.
//
// BrowserCache persists grammar blob metadata to localStorage in WASM
// builds, enabling faster subsequent loads by detecting which grammars
// were already compiled in a previous browser session.
//
// On non-WASM builds this file provides no-op equivalents so that callers
// do not need build tags to reference the WASM-specific APIs.
package ast

// InitBrowserCache is a no-op on non-WASM builds.  The actual implementation
// lives in browser_cache.go with a //go:build js&&wasm tag and uses
// syscall/js to persist grammar metadata to localStorage.
func InitBrowserCache() {
	// no-op: localStorage is not available outside WASM builds
}

// CachedGrammarNames returns nil on non-WASM builds.  On WASM it returns
// language names with persisted metadata in localStorage.
func CachedGrammarNames() []string {
	return nil
}
