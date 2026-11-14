package semantic

import (
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// The process's language adapters, shared by every caller (the web editor's
// /api/semantic and the agent's code navigation tools), so one gopls or
// TypeScript session per workspace serves both.
var (
	defaultOnce     sync.Once
	defaultRegistry *Registry
	defaultPools    []*SessionPool
)

// defaultSessionIdle is how long an idle language server session lives.
const defaultSessionIdle = 10 * time.Minute

func buildDefault() {
	tsPool := NewTypeScriptSessionPool(defaultSessionIdle)
	goPool := NewGoSessionPool(defaultSessionIdle)
	defaultPools = []*SessionPool{tsPool, goPool}
	r := NewRegistry()
	r.RegisterSingleton(tsPool, "typescript", "typescript-jsx", "javascript", "javascript-jsx")
	r.RegisterSingleton(goPool, "go")
	r.Register("python", NewPythonAdapter)
	r.Register("rust", NewRustAdapter)
	r.RegisterAliases(NewCppAdapter, "c", "cpp")
	defaultRegistry = r
}

// DefaultRegistry is the process's shared adapter registry.
func DefaultRegistry() *Registry {
	defaultOnce.Do(buildDefault)
	return defaultRegistry
}

// DefaultSessionPools are the shared registry's session pools, for evicting
// idle sessions.
func DefaultSessionPools() []*SessionPool {
	defaultOnce.Do(buildDefault)
	return defaultPools
}

// LanguageForPath is the language id a file's extension names, or "".
func LanguageForPath(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return "go"
	case ".ts", ".mts", ".cts":
		return "typescript"
	case ".tsx":
		return "typescript-jsx"
	case ".js", ".mjs", ".cjs":
		return "javascript"
	case ".jsx":
		return "javascript-jsx"
	case ".py":
		return "python"
	case ".rs":
		return "rust"
	case ".c", ".h":
		return "c"
	case ".cc", ".cpp", ".cxx", ".hpp", ".hh":
		return "cpp"
	}
	return ""
}
