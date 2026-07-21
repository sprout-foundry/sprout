package tools

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/filesystem"
)

// ---------------------------------------------------------------------------
// SP-082-2: patch_structured_file key order preservation tests
// ---------------------------------------------------------------------------

// assertKeyOrder checks that each key in `keys` appears in `content` in the
// given order by comparing byte offsets via strings.Index.  It is format-
// agnostic — it works for both JSON (`"key"`) and YAML (`key:`) output.
func assertKeyOrder(t *testing.T, content string, keys []string) {
	t.Helper()
	positions := make(map[string]int, len(keys))
	for _, k := range keys {
		// Try quoted form first (JSON), then bare form (YAML).
		idx := strings.Index(content, `"`+k+`"`)
		if idx < 0 {
			idx = strings.Index(content, k+":")
		}
		if idx < 0 {
			t.Fatalf("key %q not found in output:\n%s", k, content)
		}
		positions[k] = idx
	}
	for i := 1; i < len(keys); i++ {
		prev := keys[i-1]
		curr := keys[i]
		if positions[prev] >= positions[curr] {
			t.Errorf("key %q (pos %d) should appear before %q (pos %d) in output:\n%s",
				prev, positions[prev], curr, positions[curr], content)
		}
	}
}

func TestPatchStructuredFile_Definition_KeyOrderPreservation(t *testing.T) {
	h := &patchStructuredFileHandler{}
	def := h.Definition()

	descLower := strings.ToLower(def.Description)
	if !strings.Contains(descLower, "order") {
		t.Fatalf("description should mention 'order' for key order preservation, got: %s", def.Description)
	}
	if !strings.Contains(descLower, "diff") {
		t.Fatalf("description should mention 'diff' for minimal diff, got: %s", def.Description)
	}
}

func TestPatchStructuredFile_PreservesOrder_JSON(t *testing.T) {
	// Create a temp JSON file with keys in non-alphabetical order.
	tmpDir := t.TempDir()
	jsonPath := filepath.Join(tmpDir, "package.json")

	input := `{
  "name": "test-pkg",
  "version": "1.0.0",
  "description": "A test package",
  "dependencies": {
    "express": "4.18.0",
    "lodash": "4.17.21",
    "react": "18.2.0"
  },
  "scripts": {
    "build": "tsc",
    "test": "jest",
    "lint": "eslint",
    "start": "node index.js"
  },
  "author": "Test Author",
  "license": "MIT",
  "repository": {
    "type": "git",
    "url": "git+https://github.com/test/test.git"
  },
  "keywords": ["test", "example"]
}`
	if err := os.WriteFile(jsonPath, []byte(input), 0644); err != nil {
		t.Fatal(err)
	}

	h := &patchStructuredFileHandler{}
	ctx := filesystem.WithWorkspaceRoot(context.Background(), tmpDir)
	env := ToolEnv{WorkspaceRoot: tmpDir}
	result, err := h.Execute(ctx, env, map[string]any{
		"path": jsonPath,
		"patch_ops": []interface{}{
			map[string]interface{}{"op": "replace", "path": "/version", "value": "2.0.0"},
		},
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.IsError {
		t.Fatalf("Execute returned IsError: %s", result.Output)
	}

	// Read the patched file.
	output, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	outStr := string(output)

	// 1. Verify key order is preserved (non-alphabetical: name, version, description, dependencies, scripts, author, license, repository, keywords).
	assertKeyOrder(t, outStr, []string{
		"name", "version", "description", "dependencies", "scripts", "author", "license", "repository", "keywords",
	})

	// 2. Verify nested key order inside dependencies.
	assertKeyOrder(t, outStr, []string{"express", "lodash", "react"})

	// 3. Verify nested key order inside scripts.
	assertKeyOrder(t, outStr, []string{"build", "test", "lint", "start"})

	// 4. Verify the version value actually changed.
	if !strings.Contains(outStr, `"2.0.0"`) {
		t.Errorf("Output should contain updated version 2.0.0:\n%s", outStr)
	}
	if strings.Contains(outStr, `"1.0.0"`) {
		t.Errorf("Output should NOT contain old version 1.0.0:\n%s", outStr)
	}

	// 5. Verify other values are unchanged.
	if !strings.Contains(outStr, `"test-pkg"`) {
		t.Errorf("name should still be test-pkg:\n%s", outStr)
	}
	if !strings.Contains(outStr, `"Test Author"`) {
		t.Errorf("author should still be Test Author:\n%s", outStr)
	}
}

func TestPatchStructuredFile_PreservesOrder_YAML(t *testing.T) {
	tmpDir := t.TempDir()
	yamlPath := filepath.Join(tmpDir, "config.yaml")

	input := `name: test-pkg
version: 1.0.0
description: A test package
dependencies:
  express: 4.18.0
  lodash: 4.17.21
  react: 18.2.0
scripts:
  build: tsc
  test: jest
  lint: eslint
  start: node index.js
author: Test Author
license: MIT
`
	if err := os.WriteFile(yamlPath, []byte(input), 0644); err != nil {
		t.Fatal(err)
	}

	h := &patchStructuredFileHandler{}
	ctx := filesystem.WithWorkspaceRoot(context.Background(), tmpDir)
	env := ToolEnv{WorkspaceRoot: tmpDir}
	result, err := h.Execute(ctx, env, map[string]any{
		"path": yamlPath,
		"patch_ops": []interface{}{
			map[string]interface{}{"op": "replace", "path": "/version", "value": "2.0.0"},
		},
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.IsError {
		t.Fatalf("Execute returned IsError: %s", result.Output)
	}

	output, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatal(err)
	}
	outStr := string(output)

	// 1. Verify top-level key order is preserved.
	assertKeyOrder(t, outStr, []string{
		"name", "version", "description", "dependencies", "scripts", "author", "license",
	})

	// 2. Verify nested key order inside dependencies.
	assertKeyOrder(t, outStr, []string{"express", "lodash", "react"})

	// 3. Verify nested key order inside scripts.
	assertKeyOrder(t, outStr, []string{"build", "test", "lint", "start"})

	// 4. Verify the version value changed.
	if !strings.Contains(outStr, "2.0.0") {
		t.Errorf("Output should contain updated version 2.0.0:\n%s", outStr)
	}
	// The old "1.0.0" should not appear as a standalone value (watch for substring in 2.0.0).
	lines := strings.Split(outStr, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "version:") {
			if strings.Contains(trimmed, "1.0.0") {
				t.Errorf("version line should be 2.0.0, got: %s", trimmed)
			}
		}
	}

	// 5. Verify other values are unchanged.
	if !strings.Contains(outStr, "test-pkg") {
		t.Errorf("name should still be test-pkg:\n%s", outStr)
	}
	if !strings.Contains(outStr, "Test Author") {
		t.Errorf("author should still be Test Author:\n%s", outStr)
	}
}

func TestPatchStructuredFile_YAMLLiteralBlockPreserved(t *testing.T) {
	tmpDir := t.TempDir()
	yamlPath := filepath.Join(tmpDir, "config.yaml")

	input := `name: my-app
version: 1.0.0
dockerfile: |
  FROM node:18
  WORKDIR /app
  COPY . .
  RUN npm install
  CMD ["node", "index.js"]
description: Multi-line dockerfile
`
	if err := os.WriteFile(yamlPath, []byte(input), 0644); err != nil {
		t.Fatal(err)
	}

	h := &patchStructuredFileHandler{}
	ctx := filesystem.WithWorkspaceRoot(context.Background(), tmpDir)
	env := ToolEnv{WorkspaceRoot: tmpDir}
	result, err := h.Execute(ctx, env, map[string]any{
		"path": yamlPath,
		"patch_ops": []interface{}{
			map[string]interface{}{"op": "replace", "path": "/version", "value": "2.0.0"},
		},
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.IsError {
		t.Fatalf("Execute returned IsError: %s", result.Output)
	}

	output, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatal(err)
	}
	outStr := string(output)

	// 1. Verify key order preserved: name, version, dockerfile, description.
	assertKeyOrder(t, outStr, []string{"name", "version", "dockerfile", "description"})

	// 2. Verify the literal block scalar style (|) is preserved.
	if !strings.Contains(outStr, "dockerfile: |") {
		t.Errorf("dockerfile should use literal block style (|), output:\n%s", outStr)
	}

	// 3. Verify the multi-line content round-trips correctly.
	expectedLines := []string{"FROM node:18", "WORKDIR /app", "COPY . .", "RUN npm install"}
	for _, line := range expectedLines {
		if !strings.Contains(outStr, line) {
			t.Errorf("dockerfile content should contain %q:\n%s", line, outStr)
		}
	}

	// 4. Verify version changed.
	if !strings.Contains(outStr, "2.0.0") {
		t.Errorf("version should be 2.0.0:\n%s", outStr)
	}
}

func TestPatchStructuredFile_NestedArrayPatch(t *testing.T) {
	tmpDir := t.TempDir()
	jsonPath := filepath.Join(tmpDir, "package.json")

	input := `{
  "name": "my-pkg",
  "version": "1.0.0",
  "dependencies": ["express", "lodash", "react"],
  "scripts": {
    "build": "tsc",
    "test": "jest"
  },
  "author": "Test"
}`
	if err := os.WriteFile(jsonPath, []byte(input), 0644); err != nil {
		t.Fatal(err)
	}

	h := &patchStructuredFileHandler{}
	ctx := filesystem.WithWorkspaceRoot(context.Background(), tmpDir)
	env := ToolEnv{WorkspaceRoot: tmpDir}
	result, err := h.Execute(ctx, env, map[string]any{
		"path": jsonPath,
		"patch_ops": []interface{}{
			map[string]interface{}{"op": "replace", "path": "/dependencies/1", "value": "underscore"},
		},
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.IsError {
		t.Fatalf("Execute returned IsError: %s", result.Output)
	}

	output, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	outStr := string(output)

	// 1. Verify top-level key order preserved.
	assertKeyOrder(t, outStr, []string{"name", "version", "dependencies", "scripts", "author"})

	// 2. Verify the array element was replaced: lodash -> underscore.
	if !strings.Contains(outStr, "underscore") {
		t.Errorf("dependencies should contain underscore:\n%s", outStr)
	}
	if strings.Contains(outStr, "lodash") {
		t.Errorf("dependencies should NOT contain lodash:\n%s", outStr)
	}

	// 3. Verify other array elements are unchanged.
	if !strings.Contains(outStr, "express") {
		t.Errorf("dependencies should still contain express:\n%s", outStr)
	}
	if !strings.Contains(outStr, "react") {
		t.Errorf("dependencies should still contain react:\n%s", outStr)
	}

	// 4. Verify scripts and author are unchanged.
	if !strings.Contains(outStr, `"build"`) {
		t.Errorf("scripts.build should still exist:\n%s", outStr)
	}
	if !strings.Contains(outStr, `"Test"`) {
		t.Errorf("author should still be Test:\n%s", outStr)
	}
}

func TestPatchStructuredFile_AddNewKey_JSON(t *testing.T) {
	tmpDir := t.TempDir()
	jsonPath := filepath.Join(tmpDir, "config.json")

	input := `{"name":"x","version":"1.0.0"}`
	if err := os.WriteFile(jsonPath, []byte(input), 0644); err != nil {
		t.Fatal(err)
	}

	h := &patchStructuredFileHandler{}
	ctx := filesystem.WithWorkspaceRoot(context.Background(), tmpDir)
	env := ToolEnv{WorkspaceRoot: tmpDir}
	result, err := h.Execute(ctx, env, map[string]any{
		"path": jsonPath,
		"patch_ops": []interface{}{
			map[string]interface{}{"op": "add", "path": "/description", "value": "new field"},
		},
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.IsError {
		t.Fatalf("Execute returned IsError: %s", result.Output)
	}

	output, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	outStr := string(output)

	// 1. Verify key order: name, version, description (new key appended at end).
	assertKeyOrder(t, outStr, []string{"name", "version", "description"})

	// 2. Verify the new value.
	if !strings.Contains(outStr, "new field") {
		t.Errorf("Output should contain new field value:\n%s", outStr)
	}

	// 3. Verify original values unchanged.
	if !strings.Contains(outStr, `"x"`) {
		t.Errorf("name should still be x:\n%s", outStr)
	}
	if !strings.Contains(outStr, `"1.0.0"`) {
		t.Errorf("version should still be 1.0.0:\n%s", outStr)
	}
}

func TestPatchStructuredFile_RemoveKey_JSON(t *testing.T) {
	tmpDir := t.TempDir()
	jsonPath := filepath.Join(tmpDir, "config.json")

	input := `{"name":"x","version":"1.0.0","description":"y","author":"z"}`
	if err := os.WriteFile(jsonPath, []byte(input), 0644); err != nil {
		t.Fatal(err)
	}

	h := &patchStructuredFileHandler{}
	ctx := filesystem.WithWorkspaceRoot(context.Background(), tmpDir)
	env := ToolEnv{WorkspaceRoot: tmpDir}
	result, err := h.Execute(ctx, env, map[string]any{
		"path": jsonPath,
		"patch_ops": []interface{}{
			map[string]interface{}{"op": "remove", "path": "/description"},
		},
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.IsError {
		t.Fatalf("Execute returned IsError: %s", result.Output)
	}

	output, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	outStr := string(output)

	// 1. Verify remaining keys stay in original relative order: name, version, author.
	assertKeyOrder(t, outStr, []string{"name", "version", "author"})

	// 2. Verify description is gone.
	if strings.Contains(outStr, "description") {
		t.Errorf("Output should NOT contain description:\n%s", outStr)
	}

	// 3. Verify remaining values unchanged.
	if !strings.Contains(outStr, `"x"`) {
		t.Errorf("name should still be x:\n%s", outStr)
	}
	if !strings.Contains(outStr, `"1.0.0"`) {
		t.Errorf("version should still be 1.0.0:\n%s", outStr)
	}
	if !strings.Contains(outStr, `"z"`) {
		t.Errorf("author should still be z:\n%s", outStr)
	}
}

// ---------------------------------------------------------------------------
// SP-082-3: Single-field diff test
// ---------------------------------------------------------------------------

// TestPatchStructuredFile_SingleFieldDiff creates a JSON file with 10 top-level
// keys, patches ONE field, then computes a line-by-line diff to verify that
// exactly 1 line differs — proving that a 1-field patch produces a 1-line diff.
func TestPatchStructuredFile_SingleFieldDiff(t *testing.T) {
	tmpDir := t.TempDir()
	jsonPath := filepath.Join(tmpDir, "config.json")

	// Write the original file in the exact format the serializer produces
	// (2-space indent, space after colon, no trailing comma on last item)
	// so that the line-by-line diff is apples-to-apples.
	//
	// IMPORTANT: This hand-crafted JSON format MUST match the nodeToJSON
	// serializer's exact output format (pkg/agent_tools/structured_helpers.go)
	// for the 1-line diff assertion to remain valid.  If nodeToJSON's formatting
	// changes, this test's input must be updated accordingly.
	input := `{
  "a": "1",
  "b": "2",
  "c": "3",
  "d": "4",
  "e": "5",
  "f": "6",
  "g": "7",
  "h": "8",
  "i": "9",
  "j": "10"
}
`
	if err := os.WriteFile(jsonPath, []byte(input), 0644); err != nil {
		t.Fatal(err)
	}

	// Keep the original content for diff comparison.
	originalContent, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatal(err)
	}

	h := &patchStructuredFileHandler{}
	ctx := filesystem.WithWorkspaceRoot(context.Background(), tmpDir)
	env := ToolEnv{WorkspaceRoot: tmpDir}
	result, err := h.Execute(ctx, env, map[string]any{
		"path": jsonPath,
		"patch_ops": []interface{}{
			map[string]interface{}{"op": "replace", "path": "/e", "value": "5.1"},
		},
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.IsError {
		t.Fatalf("Execute returned IsError: %s", result.Output)
	}

	patchedContent, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatal(err)
	}

	// Compute a line-by-line diff: split both by \n and count differing lines.
	originalLines := strings.Split(string(originalContent), "\n")
	patchedLines := strings.Split(string(patchedContent), "\n")

	// Normalize to the same length for comparison.
	maxLen := len(originalLines)
	if len(patchedLines) > maxLen {
		maxLen = len(patchedLines)
	}

	differingLines := 0
	for i := 0; i < maxLen; i++ {
		orig := ""
		patch := ""
		if i < len(originalLines) {
			orig = originalLines[i]
		}
		if i < len(patchedLines) {
			patch = patchedLines[i]
		}
		if orig != patch {
			differingLines++
		}
	}

	// Exactly 1 line should differ (the line containing "e" with value changed from "5" to "5.1").
	if differingLines != 1 {
		t.Errorf("Expected exactly 1 differing line, got %d.\nOriginal lines:\n%s\nPatched lines:\n%s",
			differingLines, string(originalContent), string(patchedContent))
	}

	// Verify the patched value is correct.
	if !strings.Contains(string(patchedContent), `"5.1"`) {
		t.Errorf("Output should contain updated value 5.1:\n%s", string(patchedContent))
	}
}

// TestPatchStructuredFile_SingleApprovalForReadAndWrite verifies the
// TOCTOU + double-prompt fix: a single off-workspace patch consults
// the gate exactly once, not twice. Before the fix, the read and
// write phases each independently invoked withFilesystemApproval,
// so "Approve once" on the read still prompted on the write.
func TestPatchStructuredFile_SingleApprovalForReadAndWrite(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix fixtures only")
	}

	gate := &recordingGate{
		approveDecision: true,
		returnedCtx:     filesystem.WithSecurityBypass(context.Background()),
	}

	// Off-workspace target under $HOME — External tier.
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	dir, err := os.MkdirTemp(home, "sprout-patch-single-approval-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	target := filepath.Join(dir, "config.json")
	if err := os.WriteFile(target, []byte(`{"version":"1.0.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	h := &patchStructuredFileHandler{}
	// No pre-applied bypass — we want the resolve to FAIL so the gate
	// is consulted. After approval, the returned ctx carries the bypass
	// for the actual write.
	ctx := context.Background()
	env := ToolEnv{
		FilesystemGate: gate,
		WorkspaceRoot:  t.TempDir(), // off-workspace from this dir's perspective
	}

	result, err := h.Execute(ctx, env, map[string]any{
		"path": target,
		"patch_ops": []interface{}{
			map[string]interface{}{"op": "replace", "path": "/version", "value": "2.0.0"},
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("Execute returned IsError: %s", result.Output)
	}
	if gate.calls != 1 {
		t.Errorf("gate should be consulted exactly once, got %d (double-prompt regression)", gate.calls)
	}
}

// TestPatchStructuredFile_PreservesExistingFilePermissions verifies
// the permission-preservation fix: an off-workspace patch on a file
// with custom permissions (e.g. 0755) keeps those permissions after
// the write. Before the fix, the write hardcoded 0644 and silently
// stripped the execute bit.
func TestPatchStructuredFile_PreservesExistingFilePermissions(t *testing.T) {
	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "script.json")
	if err := os.WriteFile(target, []byte(`{"mode":"script"}`), 0o755); err != nil {
		t.Fatal(err)
	}

	h := &patchStructuredFileHandler{}
	ctx := filesystem.WithWorkspaceRoot(context.Background(), tmpDir)
	env := ToolEnv{WorkspaceRoot: tmpDir}
	result, err := h.Execute(ctx, env, map[string]any{
		"path": target,
		"patch_ops": []interface{}{
			map[string]interface{}{"op": "replace", "path": "/mode", "value": "updated"},
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("Execute returned IsError: %s", result.Output)
	}

	fi, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	gotMode := fi.Mode() & 0777
	if gotMode != 0o755 {
		t.Errorf("existing mode not preserved: got %o, want 0755", gotMode)
	}
}
