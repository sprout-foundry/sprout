package tools

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// =============================================================================
// extractGoSymbolsByRegex
// =============================================================================

func TestExtractGoSymbolsByRegex_Function(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		lineNum  int
		expected []symbolEntry
	}{
		{
			name:     "simple function",
			line:     "func MyFunction() {}",
			lineNum:  5,
			expected: []symbolEntry{{Name: "func MyFunction", Line: 5}},
		},
		{
			name:     "function with receiver",
			line:     "func (s *Server) ServeHTTP(w http.ResponseWriter) {}",
			lineNum:  10,
			expected: []symbolEntry{{Name: "func ServeHTTP", Line: 10}},
		},
		{
			name:     "function with spaces",
			line:     "  func WithLeadingSpaces() {}",
			lineNum:  3,
			expected: []symbolEntry{{Name: "func WithLeadingSpaces", Line: 3}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractGoSymbolsByRegex(tt.line, tt.lineNum)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractGoSymbolsByRegex_TypeDeclarations(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		lineNum  int
		expected []symbolEntry
	}{
		{
			name:     "type struct",
			line:     "type Config struct{}",
			lineNum:  1,
			expected: []symbolEntry{{Name: "type Config struct", Line: 1}},
		},
		{
			name:     "type interface",
			line:     "type Reader interface{}",
			lineNum:  2,
			expected: []symbolEntry{{Name: "type Reader interface", Line: 2}},
		},
		{
			name:     "type with leading spaces",
			line:     "  type MyType struct{}",
			lineNum:  15,
			expected: []symbolEntry{{Name: "type MyType struct", Line: 15}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractGoSymbolsByRegex(tt.line, tt.lineNum)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractGoSymbolsByRegex_NonMatchingLines(t *testing.T) {
	tests := []struct {
		name string
		line string
	}{
		{
			name: "comment",
			line: "// This is a comment",
		},
		{
			name: "variable declaration",
			line: "var x = 42",
		},
		{
			name: "import statement",
			line: "import \"fmt\"",
		},
		{
			name: "empty line",
			line: "",
		},
		{
			name: "function call inside body",
			line: "  fmt.Println(hello)",
		},
		{
			name: "type alias (not struct/interface)",
			line: "type MyAlias = int",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractGoSymbolsByRegex(tt.line, 1)
			assert.Nil(t, result)
		})
	}
}

func TestExtractGoSymbolsByRegex_FunctionTakesPrecedence(t *testing.T) {
	// When a line matches both func and type patterns (unlikely but test priority),
	// func should be returned first since it's checked first.
	line := "func type struct() {}"
	result := extractGoSymbolsByRegex(line, 1)
	assert.Equal(t, []symbolEntry{{Name: "func type", Line: 1}}, result)
}

// =============================================================================
// extractTSSymbols
// =============================================================================

func TestExtractTSSymbols_Function(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		lineNum  int
		expected []symbolEntry
	}{
		{
			name:     "simple function",
			line:     "function greet(name) {}",
			lineNum:  1,
			expected: []symbolEntry{{Name: "function greet", Line: 1}},
		},
		{
			name:     "exported function",
			line:     "export function parse(data) {}",
			lineNum:  5,
			expected: []symbolEntry{{Name: "function parse", Line: 5}},
		},
		{
			name:     "export default function",
			line:     "export default function handler() {}",
			lineNum:  10,
			expected: []symbolEntry{{Name: "function handler", Line: 10}},
		},
		{
			name:     "async function",
			line:     "async function fetchData() {}",
			lineNum:  15,
			expected: []symbolEntry{{Name: "function fetchData", Line: 15}},
		},
		{
			name:     "export async function",
			line:     "export async function load() {}",
			lineNum:  20,
			expected: []symbolEntry{{Name: "function load", Line: 20}},
		},
		{
			name:     "function with leading spaces",
			line:     "  function nested() {}",
			lineNum:  2,
			expected: []symbolEntry{{Name: "function nested", Line: 2}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractTSSymbols(tt.line, tt.lineNum)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractTSSymbols_Class(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		lineNum  int
		expected []symbolEntry
	}{
		{
			name:     "simple class",
			line:     "class Animal {}",
			lineNum:  1,
			expected: []symbolEntry{{Name: "class Animal", Line: 1}},
		},
		{
			name:     "export class",
			line:     "export class Dog extends Animal {}",
			lineNum:  5,
			expected: []symbolEntry{{Name: "class Dog", Line: 5}},
		},
		{
			name:     "export default class",
			line:     "export default class App {}",
			lineNum:  10,
			expected: []symbolEntry{{Name: "class App", Line: 10}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractTSSymbols(tt.line, tt.lineNum)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractTSSymbols_Interface(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		lineNum  int
		expected []symbolEntry
	}{
		{
			name:     "simple interface",
			line:     "interface User {}",
			lineNum:  1,
			expected: []symbolEntry{{Name: "interface User", Line: 1}},
		},
		{
			name:     "export interface",
			line:     "export interface Config {}",
			lineNum:  5,
			expected: []symbolEntry{{Name: "interface Config", Line: 5}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractTSSymbols(tt.line, tt.lineNum)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractTSSymbols_Type(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		lineNum  int
		expected []symbolEntry
	}{
		{
			name:     "simple type",
			line:     "type ID = number",
			lineNum:  1,
			expected: []symbolEntry{{Name: "type ID", Line: 1}},
		},
		{
			name:     "export type",
			line:     "export type Point = { x: number; y: number }",
			lineNum:  3,
			expected: []symbolEntry{{Name: "type Point", Line: 3}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractTSSymbols(tt.line, tt.lineNum)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractTSSymbols_Const(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		lineNum  int
		expected []symbolEntry
	}{
		{
			name:     "const declaration",
			line:     "const MAX_SIZE = 100",
			lineNum:  1,
			expected: []symbolEntry{{Name: "const MAX_SIZE", Line: 1}},
		},
		{
			name:     "export const",
			line:     "export const API_URL = '/api'",
			lineNum:  5,
			expected: []symbolEntry{{Name: "const API_URL", Line: 5}},
		},
		{
			name:     "let declaration",
			line:     "let counter = 0",
			lineNum:  2,
			expected: []symbolEntry{{Name: "const counter", Line: 2}},
		},
		{
			name:     "export let",
			line:     "export let config = {}",
			lineNum:  7,
			expected: []symbolEntry{{Name: "const config", Line: 7}},
		},
		{
			name:     "var declaration",
			line:     "var temp = null",
			lineNum:  3,
			expected: []symbolEntry{{Name: "const temp", Line: 3}},
		},
		{
			name:     "export var",
			line:     "export var flags = []",
			lineNum:  8,
			expected: []symbolEntry{{Name: "const flags", Line: 8}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractTSSymbols(tt.line, tt.lineNum)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractTSSymbols_CommentsSkipped(t *testing.T) {
	tests := []struct {
		name string
		line string
	}{
		{
			name: "single line comment",
			line: "// this is a comment",
		},
		{
			name: "block comment start",
			line: "/* block comment",
		},
		{
			name: "block comment continuation",
			line: "* continuation line",
		},
		{
			name: "comment with leading spaces",
			line: "  // indented comment",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractTSSymbols(tt.line, 1)
			assert.Nil(t, result)
		})
	}
}

func TestExtractTSSymbols_NonMatchingLines(t *testing.T) {
	tests := []struct {
		name string
		line string
	}{
		{
			name: "method call",
			line: "console.log('hello')",
		},
		{
			name: "return statement",
			line: "return true",
		},
		{
			name: "empty line",
			line: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractTSSymbols(tt.line, 1)
			assert.Nil(t, result)
		})
	}
}

// =============================================================================
// extractPySymbols
// =============================================================================

func TestExtractPySymbols_Function(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		lineNum  int
		expected []symbolEntry
	}{
		{
			name:     "simple function",
			line:     "def hello():",
			lineNum:  1,
			expected: []symbolEntry{{Name: "def hello", Line: 1}},
		},
		{
			name:     "function with args",
			line:     "def greet(name, age):",
			lineNum:  5,
			expected: []symbolEntry{{Name: "def greet", Line: 5}},
		},
		{
			name:     "async function",
			line:     "async def fetch(url):",
			lineNum:  10,
			expected: []symbolEntry{{Name: "def fetch", Line: 10}},
		},
		{
			name:     "function with leading spaces",
			line:     "  def inner():",
			lineNum:  3,
			expected: []symbolEntry{{Name: "def inner", Line: 3}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractPySymbols(tt.line, tt.lineNum)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractPySymbols_Class(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		lineNum  int
		expected []symbolEntry
	}{
		{
			name:     "simple class",
			line:     "class MyClass:",
			lineNum:  1,
			expected: []symbolEntry{{Name: "class MyClass", Line: 1}},
		},
		{
			name:     "class with inheritance",
			line:     "class Dog(Animal):",
			lineNum:  5,
			expected: []symbolEntry{{Name: "class Dog", Line: 5}},
		},
		{
			name:     "class with leading spaces",
			line:     "  class Inner:",
			lineNum:  10,
			expected: []symbolEntry{{Name: "class Inner", Line: 10}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractPySymbols(tt.line, tt.lineNum)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractPySymbols_CommentsSkipped(t *testing.T) {
	tests := []struct {
		name string
		line string
	}{
		{
			name: "hash comment",
			line: "# this is a comment",
		},
		{
			name: "comment with leading spaces",
			line: "  # indented comment",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractPySymbols(tt.line, 1)
			assert.Nil(t, result)
		})
	}
}

func TestExtractPySymbols_NonMatchingLines(t *testing.T) {
	tests := []struct {
		name string
		line string
	}{
		{
			name: "assignment",
			line: "x = 42",
		},
		{
			name: "import",
			line: "import os",
		},
		{
			name: "empty line",
			line: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractPySymbols(tt.line, 1)
			assert.Nil(t, result)
		})
	}
}

// =============================================================================
// extractSymbolsByRegex
// =============================================================================

func TestExtractSymbolsByRegex_DispatchesByExtension(t *testing.T) {
	// Test that .go extension dispatches to Go regex
	goContent := "func hello() {}\ntype Foo struct{}\nvar x = 1"
	goSyms := extractSymbolsByRegex(".go", goContent)
	assert.Len(t, goSyms, 2)
	assert.Equal(t, "func hello", goSyms[0].Name)
	assert.Equal(t, "type Foo struct", goSyms[1].Name)

	// Test that .ts extension dispatches to TS regex
	tsContent := "function greet() {}\nclass App {}\nconst X = 1"
	tsSyms := extractSymbolsByRegex(".ts", tsContent)
	assert.Len(t, tsSyms, 3)
	assert.Equal(t, "function greet", tsSyms[0].Name)
	assert.Equal(t, "class App", tsSyms[1].Name)
	assert.Equal(t, "const X", tsSyms[2].Name)

	// Test that .py extension dispatches to Python regex
	pyContent := "def main():\nclass Config:\nx = 1"
	pySyms := extractSymbolsByRegex(".py", pyContent)
	assert.Len(t, pySyms, 2)
	assert.Equal(t, "def main", pySyms[0].Name)
	assert.Equal(t, "class Config", pySyms[1].Name)
}

func TestExtractSymbolsByRegex_UnsupportedExtension(t *testing.T) {
	// Unsupported extensions like .rs, .java should return no symbols
	result := extractSymbolsByRegex(".rs", "fn main() {}")
	assert.Nil(t, result)
}

func TestExtractSymbolsByRegex_MultiLine(t *testing.T) {
	// Verify line numbers are correct across multiple lines
	content := "line one\nfunc two() {}\nline three\nfunc four() {}\nline five"
	syms := extractSymbolsByRegex(".go", content)
	assert.Len(t, syms, 2)
	assert.Equal(t, 2, syms[0].Line) // func two is on line 2
	assert.Equal(t, 4, syms[1].Line) // func four is on line 4
}

func TestExtractSymbolsByRegex_EmptyContent(t *testing.T) {
	result := extractSymbolsByRegex(".go", "")
	assert.Nil(t, result)
}

// =============================================================================
// appendSymbolEntries
// =============================================================================

func TestAppendSymbolEntries_NoDuplicates(t *testing.T) {
	base := []symbolEntry{
		{Name: "func hello", Line: 1},
		{Name: "type Foo", Line: 5},
	}
	newSyms := []symbolEntry{
		{Name: "func hello", Line: 10}, // duplicate name
		{Name: "func world", Line: 15}, // new
	}

	result := appendSymbolEntries(base, newSyms)
	assert.Len(t, result, 3)
	assert.Equal(t, "func hello", result[0].Name)
	assert.Equal(t, "type Foo", result[1].Name)
	assert.Equal(t, "func world", result[2].Name)
}

func TestAppendSymbolEntries_AllNew(t *testing.T) {
	base := []symbolEntry{
		{Name: "func a", Line: 1},
	}
	newSyms := []symbolEntry{
		{Name: "func b", Line: 2},
		{Name: "func c", Line: 3},
	}

	result := appendSymbolEntries(base, newSyms)
	assert.Len(t, result, 3)
	assert.Equal(t, "func a", result[0].Name)
	assert.Equal(t, "func b", result[1].Name)
	assert.Equal(t, "func c", result[2].Name)
}

func TestAppendSymbolEntries_AllDuplicates(t *testing.T) {
	base := []symbolEntry{
		{Name: "func x", Line: 1},
		{Name: "func y", Line: 2},
	}
	newSyms := []symbolEntry{
		{Name: "func x", Line: 99},
		{Name: "func y", Line: 99},
	}

	result := appendSymbolEntries(base, newSyms)
	assert.Len(t, result, 2)
	// Original entries preserved (line numbers unchanged)
	assert.Equal(t, 1, result[0].Line)
	assert.Equal(t, 2, result[1].Line)
}

func TestAppendSymbolEntries_EmptyBase(t *testing.T) {
	base := []symbolEntry{}
	newSyms := []symbolEntry{
		{Name: "func a", Line: 1},
	}

	result := appendSymbolEntries(base, newSyms)
	assert.Len(t, result, 1)
	assert.Equal(t, "func a", result[0].Name)
}

func TestAppendSymbolEntries_EmptyNewSyms(t *testing.T) {
	base := []symbolEntry{
		{Name: "func a", Line: 1},
	}
	newSyms := []symbolEntry{}

	result := appendSymbolEntries(base, newSyms)
	assert.Len(t, result, 1)
	assert.Equal(t, "func a", result[0].Name)
}

func TestAppendSymbolEntries_BothEmpty(t *testing.T) {
	result := appendSymbolEntries([]symbolEntry{}, []symbolEntry{})
	assert.Empty(t, result)
}
