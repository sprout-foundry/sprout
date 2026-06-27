package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	ctx := context.Background()
	env := ToolEnv{}
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
	ctx := context.Background()
	env := ToolEnv{}
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
	ctx := context.Background()
	env := ToolEnv{}
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
	ctx := context.Background()
	env := ToolEnv{}
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
	ctx := context.Background()
	env := ToolEnv{}
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
	ctx := context.Background()
	env := ToolEnv{}
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
