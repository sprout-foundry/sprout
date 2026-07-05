package tools

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// buildTSImportMap — TypeScript / JavaScript
// =============================================================================

func TestBuildTSImportMap_NamedImport(t *testing.T) {
	path := filepath.Join("src", "app", "index.ts")
	content := []byte(`import { formatDate } from './utils'

function main() {
    const d = formatDate(new Date());
}
`)
	impMap := buildTSImportMap(path, content)
	assert.Equal(t, "src/app/utils", impMap["formatDate"],
		"named import should map local name to resolved module path")
}

func TestBuildTSImportMap_NamedImportAliased(t *testing.T) {
	path := filepath.Join("src", "app", "index.ts")
	content := []byte(`import { formatDate as fmt } from './utils'

function main() {
    const d = fmt(new Date());
}
`)
	impMap := buildTSImportMap(path, content)
	assert.Equal(t, "src/app/utils", impMap["fmt"],
		"aliased named import should use the alias as the key")
}

func TestBuildTSImportMap_NamespaceImport(t *testing.T) {
	path := filepath.Join("src", "app", "index.ts")
	content := []byte(`import * as utils from './utils'

function main() {
    const d = utils.formatDate(new Date());
}
`)
	impMap := buildTSImportMap(path, content)
	assert.Equal(t, "src/app/utils", impMap["utils"],
		"namespace import should map alias to resolved module path")
}

func TestBuildTSImportMap_DefaultImport(t *testing.T) {
	path := filepath.Join("src", "app", "index.ts")
	content := []byte(`import Thing from './Thing'

function main() {
    const t = new Thing();
}
`)
	impMap := buildTSImportMap(path, content)
	assert.Equal(t, "src/app/Thing", impMap["Thing"],
		"default import should map name to resolved module path")
}

func TestBuildTSImportMap_MultipleNamedImports(t *testing.T) {
	path := filepath.Join("src", "app", "index.ts")
	content := []byte(`import { formatDate, parseDate, isValid } from './utils'

function main() {
    formatDate(new Date());
    parseDate("2024-01-01");
    isValid("2024-01-01");
}
`)
	impMap := buildTSImportMap(path, content)
	assert.Equal(t, "src/app/utils", impMap["formatDate"])
	assert.Equal(t, "src/app/utils", impMap["parseDate"])
	assert.Equal(t, "src/app/utils", impMap["isValid"])
}

func TestBuildTSImportMap_SkipsNodeModules(t *testing.T) {
	path := filepath.Join("src", "app", "index.ts")
	content := []byte(`import { debounce } from 'lodash'
import { formatDate } from './utils'

function main() {
    debounce(formatDate);
}
`)
	impMap := buildTSImportMap(path, content)
	// "lodash" import should be skipped
	_, exists := impMap["debounce"]
	assert.False(t, exists, "node_modules imports should not appear in the import map")
	// Local import should still be present
	assert.Equal(t, "src/app/utils", impMap["formatDate"])
}

func TestBuildTSImportMap_RelativePathParentDir(t *testing.T) {
	path := filepath.Join("src", "app", "views", "index.ts")
	content := []byte(`import { helper } from '../utils'

function main() {
    helper();
}
`)
	impMap := buildTSImportMap(path, content)
	assert.Equal(t, "src/app/utils", impMap["helper"],
		"relative import with .. should resolve to parent directory")
}

func TestBuildTSImportMap_IndexFileStripping(t *testing.T) {
	path := filepath.Join("src", "app", "index.ts")
	content := []byte(`import { run } from './utils/index'

function main() {
    run();
}
`)
	impMap := buildTSImportMap(path, content)
	assert.Equal(t, "src/app/utils", impMap["run"],
		"import path ending with /index should strip the /index suffix")
}

func TestBuildTSImportMap_CommonJSNamed(t *testing.T) {
	path := filepath.Join("src", "app", "index.js")
	content := []byte(`const { formatDate, parseDate } = require('./utils')

function main() {
    formatDate(new Date());
}
`)
	impMap := buildTSImportMap(path, content)
	assert.Equal(t, "src/app/utils", impMap["formatDate"])
	assert.Equal(t, "src/app/utils", impMap["parseDate"])
}

func TestBuildTSImportMap_CommonJSDefault(t *testing.T) {
	path := filepath.Join("src", "app", "index.js")
	content := []byte(`const utils = require('./utils')

function main() {
    utils.formatDate();
}
`)
	impMap := buildTSImportMap(path, content)
	assert.Equal(t, "src/app/utils", impMap["utils"])
}

func TestBuildTSImportMap_CommonJSSkipsNodeModules(t *testing.T) {
	path := filepath.Join("src", "app", "index.js")
	content := []byte(`const lodash = require('lodash')
const utils = require('./utils')
`)
	impMap := buildTSImportMap(path, content)
	_, exists := impMap["lodash"]
	assert.False(t, exists, "CommonJS require of node_modules should be skipped")
	assert.Equal(t, "src/app/utils", impMap["utils"])
}

func TestBuildTSImportMap_TSXExtension(t *testing.T) {
	path := filepath.Join("src", "components", "App.tsx")
	content := []byte(`import { useState } from './hooks'

function App() {
    const [state] = useState();
}
`)
	impMap := buildTSImportMap(path, content)
	assert.Equal(t, "src/components/hooks", impMap["useState"])
}

// =============================================================================
// buildTSImportMap — Python
// =============================================================================

func TestBuildTSImportMap_PythonRelativeImport(t *testing.T) {
	path := filepath.Join("pkg", "app", "handler.py")
	content := []byte(`from .utils import helper

def main():
    helper()
`)
	impMap := buildTSImportMap(path, content)
	assert.Equal(t, "pkg/app/utils", impMap["helper"],
		"Python relative import should resolve against file's directory")
}

func TestBuildTSImportMap_PythonParentRelativeImport(t *testing.T) {
	path := filepath.Join("pkg", "app", "handler.py")
	content := []byte(`from ..utils import helper

def main():
    helper()
`)
	impMap := buildTSImportMap(path, content)
	assert.Equal(t, "pkg/utils", impMap["helper"],
		"Python parent relative import (..) should resolve one level up")
}

func TestBuildTSImportMap_PythonAbsoluteImport(t *testing.T) {
	path := filepath.Join("pkg", "app", "handler.py")
	content := []byte(`from pkg.utils import helper

def main():
    helper()
`)
	impMap := buildTSImportMap(path, content)
	assert.Equal(t, "pkg/utils", impMap["helper"],
		"Python absolute from-import should convert dots to slashes")
}

func TestBuildTSImportMap_PythonModuleImport(t *testing.T) {
	path := filepath.Join("pkg", "app", "handler.py")
	content := []byte(`import pkg.utils

def main():
    pkg.utils.helper()
`)
	impMap := buildTSImportMap(path, content)
	assert.Equal(t, "pkg/utils", impMap["pkg"],
		"Python module import should map first segment to resolved path")
}

func TestBuildTSImportMap_PythonModuleImportAs(t *testing.T) {
	path := filepath.Join("pkg", "app", "handler.py")
	content := []byte(`import pkg.utils as pu

def main():
    pu.helper()
`)
	impMap := buildTSImportMap(path, content)
	assert.Equal(t, "pkg/utils", impMap["pu"],
		"Python module import with alias should use the alias as the key")
}

func TestBuildTSImportMap_PythonSkipsStdlib(t *testing.T) {
	path := filepath.Join("pkg", "app", "handler.py")
	content := []byte(`import os
import sys
from os import path
from pkg.utils import helper

def main():
    helper()
`)
	impMap := buildTSImportMap(path, content)
	// stdlib imports should be skipped
	_, osExists := impMap["os"]
	_, sysExists := impMap["sys"]
	_, pathExists := impMap["path"]
	assert.False(t, osExists, "stdlib 'import os' should be skipped")
	assert.False(t, sysExists, "stdlib 'import sys' should be skipped")
	assert.False(t, pathExists, "stdlib 'from os import path' should be skipped")
	// Local import should still be present
	assert.Equal(t, "pkg/utils", impMap["helper"])
}

func TestBuildTSImportMap_PythonMultipleNames(t *testing.T) {
	path := filepath.Join("pkg", "app", "handler.py")
	content := []byte(`from .utils import helper, formatter, validator

def main():
    helper()
    formatter()
    validator()
`)
	impMap := buildTSImportMap(path, content)
	assert.Equal(t, "pkg/app/utils", impMap["helper"])
	assert.Equal(t, "pkg/app/utils", impMap["formatter"])
	assert.Equal(t, "pkg/app/utils", impMap["validator"])
}

func TestBuildTSImportMap_PythonAliasedImport(t *testing.T) {
	path := filepath.Join("pkg", "app", "handler.py")
	content := []byte(`from .utils import helper as h

def main():
    h()
`)
	impMap := buildTSImportMap(path, content)
	assert.Equal(t, "pkg/app/utils", impMap["h"],
		"Python aliased from-import should use the alias as the key")
}

// =============================================================================
// resolveCallEdgeWithImportMap
// =============================================================================

func TestResolveCallEdgeWithImportMap_DottedCallee(t *testing.T) {
	impMap := map[string]string{
		"utils": "src/utils",
	}
	target, edgeType := resolveCallEdgeWithImportMap("utils.formatDate", impMap)
	assert.Equal(t, "src/utils.formatDate", target)
	assert.Equal(t, "resolved_calls", edgeType)
}

func TestResolveCallEdgeWithImportMap_BareCalleeNamedImport(t *testing.T) {
	impMap := map[string]string{
		"formatDate": "src/utils",
	}
	target, edgeType := resolveCallEdgeWithImportMap("formatDate", impMap)
	assert.Equal(t, "src/utils.formatDate", target)
	assert.Equal(t, "resolved_calls", edgeType)
}

func TestResolveCallEdgeWithImportMap_Unresolved(t *testing.T) {
	impMap := map[string]string{
		"utils": "src/utils",
	}
	target, edgeType := resolveCallEdgeWithImportMap("localFunc", impMap)
	assert.Equal(t, "localFunc", target)
	assert.Equal(t, "calls", edgeType)
}

func TestResolveCallEdgeWithImportMap_UnresolvedDotted(t *testing.T) {
	impMap := map[string]string{
		"utils": "src/utils",
	}
	target, edgeType := resolveCallEdgeWithImportMap("this.formatDate", impMap)
	assert.Equal(t, "this.formatDate", target)
	assert.Equal(t, "calls", edgeType)
}

func TestResolveCallEdgeWithImportMap_EmptyMap(t *testing.T) {
	target, edgeType := resolveCallEdgeWithImportMap("utils.formatDate", nil)
	assert.Equal(t, "utils.formatDate", target)
	assert.Equal(t, "calls", edgeType)
}

// =============================================================================
// ExtractCallsAndSymbols — TS import resolution (integration)
// =============================================================================

func TestExtractCallsAndSymbols_TS_NamedImportResolution(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "src", "app", "index.ts")
	content := []byte(`import { formatDate } from './utils'

function main() {
    const d = formatDate(new Date());
}
`)
	result, err := ExtractCallsAndSymbols(path, content)
	require.NoError(t, err)
	require.NotNil(t, result)

	// The call to formatDate() should be resolved with resolved_calls edge type.
	// The target should end with "/src/app/utils.formatDate" (named import resolves
	// to the module path + "." + callee name).
	found := false
	for _, e := range result.Edges {
		if e.SourceQualifiedName == "main" &&
			strings.HasSuffix(e.TargetQualifiedName, "src/app/utils.formatDate") &&
			e.EdgeType == "resolved_calls" {
			found = true
			break
		}
	}
	assert.True(t, found, "expected main -> .../src/app/utils.formatDate resolved_calls edge, got: %v", result.Edges)
}

func TestExtractCallsAndSymbols_TS_NamespaceImportResolution(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "src", "app", "index.ts")
	content := []byte(`import * as utils from './utils'

function main() {
    const d = utils.formatDate(new Date());
}
`)
	result, err := ExtractCallsAndSymbols(path, content)
	require.NoError(t, err)
	require.NotNil(t, result)

	found := false
	for _, e := range result.Edges {
		if e.SourceQualifiedName == "main" &&
			strings.HasSuffix(e.TargetQualifiedName, "src/app/utils.formatDate") &&
			e.EdgeType == "resolved_calls" {
			found = true
			break
		}
	}
	assert.True(t, found, "expected main -> .../src/app/utils.formatDate resolved_calls edge, got: %v", result.Edges)
}

func TestExtractCallsAndSymbols_TS_AliasedImportResolution(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "src", "app", "index.ts")
	content := []byte(`import { formatDate as fmt } from './utils'

function main() {
    const d = fmt(new Date());
}
`)
	result, err := ExtractCallsAndSymbols(path, content)
	require.NoError(t, err)
	require.NotNil(t, result)

	found := false
	for _, e := range result.Edges {
		if e.SourceQualifiedName == "main" &&
			strings.HasSuffix(e.TargetQualifiedName, "src/app/utils.fmt") &&
			e.EdgeType == "resolved_calls" {
			found = true
			break
		}
	}
	assert.True(t, found, "expected main -> .../src/app/utils.fmt resolved_calls edge, got: %v", result.Edges)
}

func TestExtractCallsAndSymbols_TS_SkipsNodeModules(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "src", "app", "index.ts")
	content := []byte(`import { debounce } from 'lodash'

function main() {
    debounce();
}
`)
	result, err := ExtractCallsAndSymbols(path, content)
	require.NoError(t, err)
	require.NotNil(t, result)

	// The call to debounce() should NOT be resolved (node_modules import skipped)
	for _, e := range result.Edges {
		if e.SourceQualifiedName == "main" && e.TargetQualifiedName == "debounce" {
			assert.Equal(t, "calls", e.EdgeType,
				"node_modules imports should stay as 'calls' (unresolved)")
		}
	}
}

func TestExtractCallsAndSymbols_TS_UnresolvedLocalCall(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "src", "app", "index.ts")
	content := []byte(`function localHelper() {
    return 42;
}

function main() {
    const d = localHelper();
}
`)
	result, err := ExtractCallsAndSymbols(path, content)
	require.NoError(t, err)
	require.NotNil(t, result)

	// localHelper is a local function, not an import — should stay as "calls"
	found := false
	for _, e := range result.Edges {
		if e.SourceQualifiedName == "main" && e.TargetQualifiedName == "localHelper" {
			assert.Equal(t, "calls", e.EdgeType,
				"local function calls should stay as 'calls'")
			found = true
		}
	}
	assert.True(t, found, "expected main -> localHelper edge, got: %v", result.Edges)
}

func TestExtractCallsAndSymbols_TS_ThisMethodCall(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "src", "app", "handler.ts")
	content := []byte(`class Handler {
    process() {
        this.validate();
    }
    validate() {
    }
}
`)
	result, err := ExtractCallsAndSymbols(path, content)
	require.NoError(t, err)
	require.NotNil(t, result)

	// this.validate() — "this" is not in the import map, so it stays unresolved.
	for _, e := range result.Edges {
		if e.TargetQualifiedName == "this.validate" {
			assert.Equal(t, "calls", e.EdgeType,
				"this.method() calls should stay as 'calls' (not an import)")
		}
	}
}

// =============================================================================
// ExtractCallsAndSymbols — Python import resolution (integration)
// =============================================================================

func TestExtractCallsAndSymbols_Python_FromImportResolution(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pkg", "app", "handler.py")
	content := []byte(`from .utils import helper

def main():
    helper()
`)
	result, err := ExtractCallsAndSymbols(path, content)
	require.NoError(t, err)
	require.NotNil(t, result)

	found := false
	for _, e := range result.Edges {
		if e.SourceQualifiedName == "main" &&
			strings.HasSuffix(e.TargetQualifiedName, "pkg/app/utils.helper") &&
			e.EdgeType == "resolved_calls" {
			found = true
			break
		}
	}
	assert.True(t, found, "expected main -> .../pkg/app/utils.helper resolved_calls edge, got: %v", result.Edges)
}

func TestExtractCallsAndSymbols_Python_ModuleImportResolution(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pkg", "app", "handler.py")
	content := []byte(`import pkg.utils

def main():
    pkg.utils.helper()
`)
	result, err := ExtractCallsAndSymbols(path, content)
	require.NoError(t, err)
	require.NotNil(t, result)

	found := false
	for _, e := range result.Edges {
		if e.SourceQualifiedName == "main" &&
			strings.HasSuffix(e.TargetQualifiedName, "pkg/utils.helper") &&
			e.EdgeType == "resolved_calls" {
			found = true
			break
		}
	}
	assert.True(t, found, "expected main -> .../pkg/utils.helper resolved_calls edge, got: %v", result.Edges)
}

func TestExtractCallsAndSymbols_Python_LocalCallUnresolved(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pkg", "app", "handler.py")
	content := []byte(`def local_helper():
    return 42

def main():
    local_helper()
`)
	result, err := ExtractCallsAndSymbols(path, content)
	require.NoError(t, err)
	require.NotNil(t, result)

	found := false
	for _, e := range result.Edges {
		if e.SourceQualifiedName == "main" && e.TargetQualifiedName == "local_helper" {
			assert.Equal(t, "calls", e.EdgeType,
				"local function calls should stay as 'calls'")
			found = true
		}
	}
	assert.True(t, found, "expected main -> local_helper edge, got: %v", result.Edges)
}

// =============================================================================
// resolveJSImportPath edge cases
// =============================================================================

func TestResolveJSImportPath_BasicRelative(t *testing.T) {
	resolved := resolveJSImportPath(filepath.Join("src", "app"), "./utils")
	assert.Equal(t, "src/app/utils", resolved)
}

func TestResolveJSImportPath_ParentDirectory(t *testing.T) {
	resolved := resolveJSImportPath(filepath.Join("src", "app", "views"), "../utils")
	assert.Equal(t, "src/app/utils", resolved)
}

func TestResolveJSImportPath_WithExtension(t *testing.T) {
	resolved := resolveJSImportPath(filepath.Join("src", "app"), "./utils.ts")
	assert.Equal(t, "src/app/utils", resolved)
}

func TestResolveJSImportPath_IndexFile(t *testing.T) {
	resolved := resolveJSImportPath(filepath.Join("src", "app"), "./utils/index")
	assert.Equal(t, "src/app/utils", resolved)
}

func TestResolveJSImportPath_IndexWithExtension(t *testing.T) {
	resolved := resolveJSImportPath(filepath.Join("src", "app"), "./utils/index.ts")
	assert.Equal(t, "src/app/utils", resolved)
}

// =============================================================================
// resolvePythonModulePath edge cases
// =============================================================================

func TestResolvePythonModulePath_RelativeSameDir(t *testing.T) {
	resolved := resolvePythonModulePath(filepath.Join("pkg", "app"), ".utils")
	assert.Equal(t, "pkg/app/utils", resolved)
}

func TestResolvePythonModulePath_RelativeParent(t *testing.T) {
	resolved := resolvePythonModulePath(filepath.Join("pkg", "app"), "..utils")
	assert.Equal(t, "pkg/utils", resolved)
}

func TestResolvePythonModulePath_Absolute(t *testing.T) {
	resolved := resolvePythonModulePath(filepath.Join("pkg", "app"), "pkg.utils")
	assert.Equal(t, "pkg/utils", resolved)
}

func TestResolvePythonModulePath_DottedRelative(t *testing.T) {
	resolved := resolvePythonModulePath(filepath.Join("pkg", "app"), ".sub.utils")
	assert.Equal(t, "pkg/app/sub/utils", resolved)
}
