package embedding

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
)

// CodeUnit represents a single unit of code (e.g., a function) extracted from a source file.
type CodeUnit struct {
	// ID is a unique identifier produced by makeUnitID:
	// "<file>:<name>#L<startLine>" for code-unit extractors (Go/Python/TS).
	// File-level extractors (extractor_file.go) use the bare file path
	// instead since one record represents the whole file.
	ID string `json:"id"`

	// File is the file path the code unit comes from.
	File string `json:"file"`

	// Name is the symbol name (e.g., "funcName" or "(*Receiver).Method").
	Name string `json:"name"`

	// Signature is the full function signature text.
	Signature string `json:"signature"`

	// Body is the source text of the function body.
	Body string `json:"body"`

	// StartLine is the 1-based starting line number.
	StartLine int `json:"startLine"`

	// EndLine is the 1-based ending line number.
	EndLine int `json:"endLine"`

	// Language is the programming language identifier (e.g., "go").
	Language string `json:"language"`

	// Hash is a SHA-256 hex digest of Signature+Body for deduplication.
	Hash string `json:"hash"`
}

// ComputeHash calculates a SHA-256 hex digest of the signature and body.
func (c *CodeUnit) ComputeHash() {
	h := sha256.New()
	h.Write([]byte(c.Signature))
	h.Write([]byte(c.Body))
	c.Hash = fmt.Sprintf("%x", h.Sum(nil))
}

// makeUnitID returns a canonical ID for a code-unit CodeUnit.
//
// Format: "<path>:<name>#L<startLine>". The line suffix guarantees
// uniqueness within a file even when the name alone collides — which
// happens in practice for:
//
//   - TypeScript overloaded function declarations (multiple
//     `function f(...)` signatures with the same name).
//   - Python @typing.overload-decorated stubs (`@overload def f(...)`).
//   - Nested Python helpers where the parser doesn't scope by enclosing
//     function (`def outer(): def helper(): ...` ×2 → two `path:helper`).
//   - Go `func _()` and similar test sinks that legally repeat the
//     underscore identifier inside one file.
//
// Without line disambiguation, two such CodeUnits become two records
// with the same ID, which crashes the hnsw store's ReplaceAll path
// (its Add() invariant panics on duplicate-key insert). The
// `extractor_go.go` anonymous-func path already used this format; this
// helper extends it to every named-symbol path so the format is
// uniform and the panic is impossible at the source.
//
// File-level units (one record per file) keep their pure-path ID via
// `extractor_file.go` — they don't have a meaningful "name" or
// "startLine" distinct from the file itself.
func makeUnitID(path, name string, startLine int) string {
	return fmt.Sprintf("%s:%s#L%d", path, name, startLine)
}

// ExtractFromFile extracts code units from the given file path using the
// language-specific extractor determined by file extension.
// Returns an empty slice (no error) for unsupported file types.
func ExtractFromFile(path string, opts ...ExtractOption) ([]CodeUnit, error) {
	ext := filepath.Ext(path)
	switch ext {
	case ".go":
		return ExtractGoFile(path, opts...)
	case ".py":
		return ExtractPyFile(path, opts...)
	case ".ts", ".tsx", ".js", ".jsx", ".mjs":
		return ExtractTSFile(path, opts...)
	default:
		// Unsupported language — return empty with no error.
		return nil, nil
	}
}

// ExtractOption configures behavior for code extraction.
type ExtractOption func(*ExtractConfig)

// ExtractConfig holds options for code extraction.
type ExtractConfig struct {
	// IncludeTests controls whether test functions (Test*, Benchmark*, Fuzz*)
	// are included in the extraction. Default: false.
	IncludeTests bool
}

// ApplyOptions applies a list of ExtractOption functions to an ExtractConfig.
func (c *ExtractConfig) ApplyOptions(opts ...ExtractOption) {
	for _, opt := range opts {
		opt(c)
	}
}

// WithIncludeTests returns an ExtractOption that sets whether test functions
// are included in extraction.
func WithIncludeTests(include bool) ExtractOption {
	return func(c *ExtractConfig) {
		c.IncludeTests = include
	}
}
