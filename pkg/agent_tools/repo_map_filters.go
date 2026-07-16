package tools

// Package tools: file/directory filtering and classification helpers for repo_map (split from repo_map.go).

import (
	"path/filepath"
	"strings"
)

// isTestFile returns true if the given relative path matches common test
// file patterns: *_test.go, *.spec.*, *.test.*, and files in test directories.
func isTestFile(relPath string) bool {
	name := filepath.Base(relPath)
	// Go test files.
	if strings.HasSuffix(name, "_test.go") {
		return true
	}
	// JS/TS spec/test files: *.spec.ts, *.test.tsx, etc.
	lower := strings.ToLower(name)
	if strings.Contains(lower, ".spec.") || strings.Contains(lower, ".test.") {
		return true
	}
	// Python test files: test_*.py, *_test.py
	if strings.HasPrefix(lower, "test_") && strings.HasSuffix(lower, ".py") {
		return true
	}
	if strings.HasSuffix(lower, "_test.py") {
		return true
	}
	// In a test directory.
	return isInTestDir(relPath)
}

// isInTestDir returns true if any path component is a recognized test directory.
func isInTestDir(relPath string) bool {
	parts := strings.Split(relPath, "/")
	for _, p := range parts {
		if isTestDirName(p) {
			return true
		}
	}
	return false
}

// isTestDirName returns true if the directory name is a recognized test directory.
func isTestDirName(dirName string) bool {
	lower := strings.ToLower(dirName)
	if lower == "e2e" || lower == "__tests__" || lower == "spec" || lower == "specs" {
		return true
	}
	if strings.HasPrefix(lower, "test") || strings.HasPrefix(lower, "__test") {
		return true
	}
	return false
}

// isEntryPoint returns true if the file is a recognized entry point or config
// file at the root or top level (depth <= 1).
func isEntryPoint(relPath string) bool {
	// Only consider root-level and top-level files.
	depth := strings.Count(relPath, "/")
	if depth > 1 {
		return false
	}

	name := filepath.Base(relPath)
	lower := strings.ToLower(name)

	// Entry-point source files: main.*, index.*, App.*, app.*
	// Check stem (name without extension).
	stem := strings.TrimSuffix(lower, filepath.Ext(lower))
	switch stem {
	case "main", "index", "app":
		return true
	}
	// App.tsx, App.jsx etc. (case-insensitive stem "app" already covered above)

	// Config files.
	switch lower {
	case "package.json", "cargo.toml", "go.mod", "tsconfig.json",
		"metro.config.js", "metro.config.ts", "webpack.config.js",
		"vite.config.ts", "vite.config.js", "rollup.config.js",
		"babel.config.js", "jest.config.js", "vitest.config.ts",
		"next.config.js", "next.config.ts", "nuxt.config.ts",
		"pom.xml", "build.gradle", "build.gradle.kts", "settings.gradle",
		"dockerfile", "makefile", "cmakelists.txt":
		return true
	}

	// metro.config.* pattern.
	if strings.HasPrefix(lower, "metro.config.") {
		return true
	}

	return false
}

// getConceptForDir maps a directory name to a concept label used in the
// structure summary.
func getConceptForDir(dirName string) string {
	lower := strings.ToLower(dirName)

	// Tests.
	if isTestDirName(lower) {
		return "Tests"
	}

	// UI / Frontend.
	if lower == "components" || lower == "ui" || lower == "views" ||
		lower == "screens" || lower == "pages" || lower == "widgets" ||
		lower == "public" || lower == "assets" || lower == "styles" ||
		lower == "scss" || lower == "css" {
		return "UI"
	}

	// Services / API.
	if lower == "services" || lower == "api" || lower == "controllers" ||
		lower == "handlers" || lower == "routes" || lower == "endpoints" ||
		lower == "server" || lower == "graphql" || lower == "middleware" {
		return "Services"
	}

	// Utilities / Helpers.
	if lower == "utils" || lower == "helpers" || lower == "lib" ||
		lower == "common" || lower == "shared" || lower == "tools" {
		return "Utilities"
	}

	// Config.
	if lower == "config" || lower == "configurations" || lower == "settings" ||
		lower == "env" || lower == "scripts" || lower == "ci" || lower == ".github" {
		return "Config"
	}

	// Core / domain.
	if lower == "src" || lower == "pkg" || lower == "cmd" || lower == "app" ||
		lower == "internal" || lower == "core" || lower == "domain" ||
		lower == "models" || lower == "types" || lower == "store" ||
		lower == "db" || lower == "entities" {
		return "Core"
	}

	return "Other"
}

// dedupStrings returns a new slice with duplicates removed, preserving order.
func dedupStrings(s []string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, v := range s {
		if !seen[v] {
			seen[v] = true
			result = append(result, v)
		}
	}
	return result
}
