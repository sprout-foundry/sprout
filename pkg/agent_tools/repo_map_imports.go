package tools

import (
	"path/filepath"
	"regexp"
	"strings"

	astp "github.com/sprout-foundry/sprout/pkg/ast"
	codegraph "github.com/sprout-foundry/sprout/pkg/codegraph"
)

// TS/JS import patterns.
// ES module imports:
//
//	import { foo } from './utils'
//	import { foo as bar } from './utils'
//	import * as utils from './utils'
//	import Thing from './Thing'
//	import { a, b } from './mod'
//
// CommonJS:
//
//	const { foo } = require('./utils')
//	const utils = require('./utils')

// import { foo, bar } from './mod'
var reESImport = regexp.MustCompile(`(?m)^import\s+(?:(\w+)|(\* as (\w+))|{([^}]+)})\s+from\s+['"]([^'"]+)['"]`)

// const { foo, bar } = require('./mod')
var reCommonJSNamed = regexp.MustCompile(`(?m)^\s*const\s+{([^}]+)}\s*=\s*require\(\s*['"]([^'"]+)['"]\s*\)`)

// const mod = require('./mod')
var reCommonJSDefault = regexp.MustCompile(`(?m)^\s*const\s+(\w+)\s*=\s*require\(\s*['"]([^'"]+)['"]\s*\)`)

// Python import patterns.
//
//	from .utils import foo
//	from ..utils import foo, bar
//	from pkg.mod import foo
//	import pkg.mod
//	import pkg.mod as alias

// from <module> import <names>
var rePythonFromImport = regexp.MustCompile(`(?m)^from\s+([\.\w]+)\s+import\s+(.+)$`)

// import <module> [as <alias>]
var rePythonImport = regexp.MustCompile(`(?m)^import\s+([\w.]+)(?:\s+as\s+(\w+))?$`)

// buildTSImportMap parses import statements from TS/JS/Python source content
// and returns a map from local alias/name → resolved relative package path
// (without file extension, matching the qualified-name format used for nodes).
//
// For TS/JS it handles ES module imports and CommonJS require().
// For Python it handles "from X import Y" and "import X [as alias]".
//
// The resolved paths are relative to the repo root, with leading "./" stripped
// and file extensions removed. Node_modules / external imports are skipped.
func buildTSImportMap(filePath string, content []byte) map[string]string {
	ext := strings.ToLower(filepath.Ext(filePath))
	impMap := make(map[string]string)

	switch ext {
	case ".ts", ".tsx", ".js", ".jsx":
		buildJSImportMap(filePath, content, impMap)
	case ".py":
		buildPythonImportMap(filePath, content, impMap)
	}

	return impMap
}

// buildJSImportMap handles ES module and CommonJS imports for TS/JS files.
func buildJSImportMap(filePath string, content []byte, impMap map[string]string) {
	fileDir := filepath.Dir(filePath)

	// ES module imports.
	for _, m := range reESImport.FindAllStringSubmatch(string(content), -1) {
		if len(m) < 6 {
			continue
		}
		defaultImport := m[1]   // "Thing" from "import Thing from '...'"
		namespaceImport := m[3] // "utils" from "import * as utils from '...'"
		namedBlock := m[4]      // "foo, bar" from "import { foo, bar } from '...'"
		modulePath := m[5]      // './utils'

		// Skip node_modules and bare specifiers (external packages).
		if !strings.HasPrefix(modulePath, ".") {
			continue
		}

		resolved := resolveJSImportPath(fileDir, modulePath)

		// Default import: "import Thing from './Thing'"
		if defaultImport != "" {
			impMap[defaultImport] = resolved
			continue
		}

		// Namespace import: "import * as utils from './utils'"
		if namespaceImport != "" {
			impMap[namespaceImport] = resolved
			continue
		}

		// Named imports: "import { foo, bar as baz } from './mod'"
		if namedBlock != "" {
			for _, name := range strings.Split(namedBlock, ",") {
				name = strings.TrimSpace(name)
				if name == "" {
					continue
				}
				// Handle "foo as bar" → local name is "bar", maps to resolved path
				parts := strings.Fields(name)
				if len(parts) == 3 && parts[1] == "as" {
					impMap[parts[2]] = resolved
				} else if len(parts) == 1 {
					impMap[parts[0]] = resolved
				}
			}
		}
	}

	// CommonJS: const { foo, bar } = require('./mod')
	for _, m := range reCommonJSNamed.FindAllStringSubmatch(string(content), -1) {
		if len(m) < 3 {
			continue
		}
		modulePath := m[2]
		if !strings.HasPrefix(modulePath, ".") {
			continue
		}
		resolved := resolveJSImportPath(fileDir, modulePath)
		for _, name := range strings.Split(m[1], ",") {
			name = strings.TrimSpace(name)
			if name != "" {
				impMap[name] = resolved
			}
		}
	}

	// CommonJS: const mod = require('./mod')
	for _, m := range reCommonJSDefault.FindAllStringSubmatch(string(content), -1) {
		if len(m) < 3 {
			continue
		}
		modulePath := m[2]
		if !strings.HasPrefix(modulePath, ".") {
			continue
		}
		// Skip if this was already matched by the named pattern (const { ... } = require)
		line := m[0]
		if strings.Contains(line, "{") {
			continue
		}
		resolved := resolveJSImportPath(fileDir, modulePath)
		impMap[m[1]] = resolved
	}
}

// resolveJSImportPath resolves a relative import path against the importing
// file's directory, strips file extensions, and normalises to forward slashes.
// e.g. file at "src/app/index.ts", import "./utils" → "src/utils"
// e.g. file at "src/app/index.ts", import "./utils/index" → "src/utils"
func resolveJSImportPath(fileDir string, modulePath string) string {
	// Resolve relative path.
	resolved := filepath.Join(fileDir, modulePath)
	resolved = filepath.Clean(resolved)

	// Convert to forward slashes.
	resolved = filepath.ToSlash(resolved)

	// Strip file extension if present.
	ext := strings.ToLower(filepath.Ext(resolved))
	if sourceExtensions[ext] {
		resolved = resolved[:len(resolved)-len(ext)]
	}

	// Strip /index suffix (index.ts, index.js, etc.)
	if strings.HasSuffix(resolved, "/index") {
		resolved = resolved[:len(resolved)-len("/index")]
	}

	return resolved
}

// buildPythonImportMap handles "from X import Y" and "import X [as alias]"
// for Python files.
func buildPythonImportMap(filePath string, content []byte, impMap map[string]string) {
	fileDir := filepath.Dir(filePath)

	// from <module> import <names>
	for _, m := range rePythonFromImport.FindAllStringSubmatch(string(content), -1) {
		if len(m) < 3 {
			continue
		}
		module := m[1]
		names := m[2]

		// Skip stdlib / absolute imports without dots (e.g., "from os import path")
		// These won't have nodes in the graph anyway.
		if !strings.HasPrefix(module, ".") && !strings.Contains(module, ".") {
			continue
		}

		// Resolve the module path.
		resolved := resolvePythonModulePath(fileDir, module)
		if resolved == "" {
			continue
		}

		// Parse imported names: "foo, bar as baz"
		for _, name := range strings.Split(names, ",") {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			parts := strings.Fields(name)
			if len(parts) == 3 && parts[1] == "as" {
				impMap[parts[2]] = resolved
			} else if len(parts) == 1 {
				impMap[parts[0]] = resolved
			}
		}
	}

	// import <module> [as <alias>]
	for _, m := range rePythonImport.FindAllStringSubmatch(string(content), -1) {
		if len(m) < 2 {
			continue
		}
		module := m[1]
		alias := m[2]

		// Skip single-word lowercase stdlib imports (e.g., "import os", "import sys").
		if !strings.Contains(module, ".") {
			continue
		}

		resolved := resolvePythonModulePath(fileDir, module)
		if resolved == "" {
			continue
		}

		if alias != "" {
			impMap[alias] = resolved
		} else {
			// First segment is the local alias.
			// "import pkg.mod" → alias is "pkg", resolved path is "pkg/mod"
			// Store all intermediate prefixes so callees like "pkg.mod.func"
			// resolve to "pkg/mod.func" (matching on "pkg.mod") rather than
			// "pkg/mod.mod.func" (matching only on "pkg").
			parts := strings.Split(module, ".")
			for i := 1; i <= len(parts); i++ {
				prefix := strings.Join(parts[:i], ".")
				impMap[prefix] = resolved
			}
		}
	}
}

// resolvePythonModulePath resolves a Python module path to a relative file path.
// Relative imports (.utils, ..utils) are resolved against the file's package directory.
// Absolute imports (pkg.mod) are returned as-is with dots replaced by slashes.
func resolvePythonModulePath(fileDir string, module string) string {
	if strings.HasPrefix(module, ".") {
		// Relative import: count leading dots to determine depth.
		trimmed := module
		for strings.HasPrefix(trimmed, ".") {
			trimmed = trimmed[1:]
		}
		depth := len(module) - len(trimmed)

		// Start from the file's directory and go up (depth - 1) levels.
		// "from .utils" (depth=1) → same directory
		// "from ..utils" (depth=2) → parent directory
		base := fileDir
		for i := 1; i < depth; i++ {
			base = filepath.Dir(base)
		}

		if trimmed == "" {
			// "from . import foo" — the name is in the same package.
			return filepath.ToSlash(base)
		}

		resolved := filepath.Join(base, strings.ReplaceAll(trimmed, ".", string(filepath.Separator)))
		resolved = filepath.Clean(resolved)
		return filepath.ToSlash(resolved)
	}

	// Absolute import: convert dots to path separators.
	return strings.ReplaceAll(module, ".", "/")
}

// resolveCallEdgeWithImportMap attempts to resolve a call edge's callee name
// using the import map. Returns the resolved target name and edge type.
//
// For dotted callees (e.g., "utils.formatDate"):
//   - Extract the prefix before the first dot
//   - If the prefix is in the import map, replace it with the resolved path
//   - If not, try progressively longer prefixes (e.g., "pkg.utils" from "pkg.utils.helper")
//
// For bare callees (e.g., "formatDate"):
//   - Check if the name is directly in the import map (named import)
//   - If so, resolve to "resolved_path.calleeName"
//
// If unresolved, returns the original callee name with edge type "calls".
func resolveCallEdgeWithImportMap(calleeName string, impMap map[string]string) (string, string) {
	if len(impMap) == 0 {
		return calleeName, "calls"
	}

	// Dotted callee: "utils.formatDate", "pkg.utils.helper"
	if dotIdx := strings.IndexByte(calleeName, '.'); dotIdx > 0 {
		// Try progressively longer prefixes (longest first) to handle
		// multi-segment imports like "import pkg.utils" where the map has
		// both "pkg" → "pkg/utils" and "pkg.utils" → "pkg/utils".
		// Matching "pkg.utils" yields "pkg/utils.helper" (correct)
		// while matching only "pkg" yields "pkg/utils.utils.helper" (wrong).
		for end := len(calleeName); end > dotIdx; end-- {
			if calleeName[end-1] != '.' {
				continue
			}
			prefix := calleeName[:end-1]
			if resolvedPath, ok := impMap[prefix]; ok {
				return resolvedPath + calleeName[end-1:], "resolved_calls"
			}
		}
		return calleeName, "calls"
	}

	// Bare callee: "formatDate" — check if it's a named import.
	if resolvedPath, ok := impMap[calleeName]; ok {
		return resolvedPath + "." + calleeName, "resolved_calls"
	}

	return calleeName, "calls"
}

// resolveEdgesForTS resolves call edges from tree-sitter using the import map
// built from the file's source content.
func resolveEdgesForTS(calls []astp.CallEdge, impMap map[string]string) []codegraph.Edge {
	if len(calls) == 0 {
		return nil
	}

	edges := make([]codegraph.Edge, 0, len(calls))
	for _, ce := range calls {
		target, edgeType := resolveCallEdgeWithImportMap(ce.CalleeName, impMap)
		edges = append(edges, codegraph.Edge{
			SourceQualifiedName: ce.CallerName,
			TargetQualifiedName: target,
			EdgeType:            edgeType,
			Line:                ce.Line,
		})
	}
	return edges
}
