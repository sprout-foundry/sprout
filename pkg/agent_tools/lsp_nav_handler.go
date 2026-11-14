//go:build !js

package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	lspsemantic "github.com/sprout-foundry/sprout/pkg/lsp/semantic"
)

// The language-server navigation tools: where a symbol is defined and every
// place it is used, from the language's own server (gopls for Go, the
// project's TypeScript for TS/JS, and the other adapters
// lspsemantic.DefaultRegistry holds). Unlike get_callers they answer across
// dependencies and need no index, at the cost of starting the server on
// first use.

// maxLSPReferences bounds a references answer; the rest are counted.
const maxLSPReferences = 100

const lspNavParams = "path: the file the symbol appears in; line: its 1-based line; symbol: the name as written on that line (its first whole-word occurrence is used, or give column)."

func lspNavDefinition(name, description string) ToolDefinition {
	return ToolDefinition{
		Name:        name,
		Description: description + " " + lspNavParams,
		Required:    []string{"path", "line", "symbol"},
		Parameters: []ParameterDef{
			{Name: "path", Type: "string", Description: "The file, relative to the workspace or absolute.", Required: true},
			{Name: "line", Type: "integer", Description: "The 1-based line the symbol is on.", Required: true},
			{Name: "symbol", Type: "string", Description: "The symbol's name as it appears on that line.", Required: true},
			{Name: "column", Type: "integer", Description: "Optional 1-based column of the symbol, when it appears more than once on the line."},
		},
	}
}

// lspNavQuery is one resolved request: the file, its language, its content
// and the symbol's position.
type lspNavQuery struct {
	root, abs, lang, content, symbol string
	pos                              lspsemantic.Position
}

func resolveLSPNavQuery(env ToolEnv, args map[string]any) (lspNavQuery, string) {
	var q lspNavQuery
	path, _ := args["path"].(string)
	q.symbol, _ = args["symbol"].(string)
	line := intArgValue(args["line"])
	if strings.TrimSpace(path) == "" || strings.TrimSpace(q.symbol) == "" || line <= 0 {
		return q, "give path, a 1-based line and the symbol"
	}
	q.root = env.WorkspaceRoot
	if q.root == "" {
		q.root, _ = os.Getwd()
	}
	q.abs = path
	if !filepath.IsAbs(q.abs) {
		q.abs = filepath.Join(q.root, path)
	}
	q.abs = filepath.Clean(q.abs)
	q.lang = lspsemantic.LanguageForPath(q.abs)
	if q.lang == "" {
		return q, fmt.Sprintf("no language server for %s files; use search or get_callers", filepath.Ext(q.abs))
	}
	b, err := os.ReadFile(q.abs)
	if err != nil {
		return q, fmt.Sprintf("cannot read %s: %v", path, err)
	}
	q.content = string(b)
	lines := strings.Split(q.content, "\n")
	if line > len(lines) {
		return q, fmt.Sprintf("%s has %d lines, not %d", path, len(lines), line)
	}
	col := intArgValue(args["column"])
	if col <= 0 {
		col = wordColumn(lines[line-1], q.symbol)
	}
	if col <= 0 {
		return q, fmt.Sprintf("%q is not on line %d of %s: %s", q.symbol, line, path, strings.TrimSpace(lines[line-1]))
	}
	q.pos = lspsemantic.Position{Line: line, Column: col}
	return q, ""
}

// wordColumn is the 1-based column of word's first whole-word occurrence in
// text, or 0.
func wordColumn(text, word string) int {
	isIdent := func(r rune) bool { return r == '_' || r == '$' || unicode.IsLetter(r) || unicode.IsDigit(r) }
	for from := 0; ; {
		i := strings.Index(text[from:], word)
		if i < 0 {
			return 0
		}
		i += from
		before := i == 0 || !isIdent(rune(text[i-1]))
		after := i+len(word) >= len(text) || !isIdent(rune(text[i+len(word)]))
		if before && after {
			return len([]rune(text[:i])) + 1
		}
		from = i + len(word)
	}
}

func intArgValue(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	case string:
		var i int
		fmt.Sscanf(n, "%d", &i)
		return i
	}
	return 0
}

func runLSPNav(q lspNavQuery, method string) (lspsemantic.ToolResult, string) {
	adapter, ok := lspsemantic.DefaultRegistry().AdapterForLanguage(q.lang)
	if !ok {
		return lspsemantic.ToolResult{}, "no language server for " + q.lang
	}
	res, err := adapter.Run(lspsemantic.ToolInput{WorkspaceRoot: q.root, FilePath: q.abs, Content: q.content, Method: method, Position: &q.pos})
	if err != nil {
		return res, fmt.Sprintf("the %s language server could not answer: %v", q.lang, err)
	}
	return res, ""
}

// relPath shows a path relative to the workspace when it is inside it.
func relPath(root, p string) string {
	p = strings.TrimPrefix(p, "file://")
	if rel, err := filepath.Rel(root, p); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return p
}

// lineAt is a file's 1-based line, trimmed, or "".
func lineAt(path string, line int) string {
	b, err := os.ReadFile(path)
	if err != nil || line <= 0 {
		return ""
	}
	lines := strings.Split(string(b), "\n")
	if line > len(lines) {
		return ""
	}
	return strings.TrimSpace(lines[line-1])
}

// --- find_definition ---

type findDefinitionHandler struct{}

func (h *findDefinitionHandler) Name() string { return "find_definition" }

func (h *findDefinitionHandler) Definition() ToolDefinition {
	return lspNavDefinition(h.Name(), "Find where a symbol is defined, using the language's own server: works across packages and into dependencies (the module cache, node_modules).")
}

func (h *findDefinitionHandler) Validate(args map[string]any) error {
	return requireArgs(h.Name(), args, "path", "line", "symbol")
}

func (h *findDefinitionHandler) Execute(_ context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	q, problem := resolveLSPNavQuery(env, args)
	if problem != "" {
		return ToolResult{Output: problem, IsError: true}, nil
	}
	res, problem := runLSPNav(q, "definition")
	if problem != "" {
		return ToolResult{Output: problem, IsError: true}, nil
	}
	if res.Definition == nil || res.Definition.Path == "" {
		return ToolResult{Output: fmt.Sprintf("No definition found for %s at %s:%d.", q.symbol, relPath(q.root, q.abs), q.pos.Line)}, nil
	}
	d := res.Definition
	out := fmt.Sprintf("%s is defined at %s:%d:%d", q.symbol, relPath(q.root, d.Path), d.Line, d.Column)
	if text := lineAt(strings.TrimPrefix(d.Path, "file://"), d.Line); text != "" {
		out += "\n  " + text
	}
	return ToolResult{Output: out}, nil
}

func (h *findDefinitionHandler) Aliases() []string      { return nil }
func (h *findDefinitionHandler) Timeout() time.Duration { return 90 * time.Second }
func (h *findDefinitionHandler) MaxResultSize() int     { return 0 }
func (h *findDefinitionHandler) SafeForParallel() bool  { return true }
func (h *findDefinitionHandler) Interactive() bool      { return false }

// --- find_references ---

type findReferencesHandler struct{}

func (h *findReferencesHandler) Name() string { return "find_references" }

func (h *findReferencesHandler) Definition() ToolDefinition {
	return lspNavDefinition(h.Name(), "Find every place a symbol is used, using the language's own server: exact, unlike a text search, so a common name does not match unrelated code.")
}

func (h *findReferencesHandler) Validate(args map[string]any) error {
	return requireArgs(h.Name(), args, "path", "line", "symbol")
}

func (h *findReferencesHandler) Execute(_ context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	q, problem := resolveLSPNavQuery(env, args)
	if problem != "" {
		return ToolResult{Output: problem, IsError: true}, nil
	}
	res, problem := runLSPNav(q, "references")
	if problem != "" {
		return ToolResult{Output: problem, IsError: true}, nil
	}
	if res.References == nil || len(res.References.Locations) == 0 {
		return ToolResult{Output: fmt.Sprintf("No references found for %s at %s:%d.", q.symbol, relPath(q.root, q.abs), q.pos.Line)}, nil
	}
	locs := res.References.Locations
	var b strings.Builder
	fmt.Fprintf(&b, "%d references to %s:\n", len(locs), q.symbol)
	for i, l := range locs {
		if i == maxLSPReferences {
			fmt.Fprintf(&b, "  … and %d more\n", len(locs)-maxLSPReferences)
			break
		}
		fmt.Fprintf(&b, "  %s:%d: %s\n", relPath(q.root, l.FilePath), l.Line, strings.TrimSpace(l.LineText))
	}
	return ToolResult{Output: strings.TrimRight(b.String(), "\n")}, nil
}

func (h *findReferencesHandler) Aliases() []string      { return nil }
func (h *findReferencesHandler) Timeout() time.Duration { return 90 * time.Second }
func (h *findReferencesHandler) MaxResultSize() int     { return 0 }
func (h *findReferencesHandler) SafeForParallel() bool  { return true }
func (h *findReferencesHandler) Interactive() bool      { return false }
