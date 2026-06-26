package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestShouldSkipFileForContext(t *testing.T) {
	tests := []struct {
		name     string
		filePath string
		wantSkip bool
	}{
		// .sum files
		{
			name:     "go.sum",
			filePath: "go.sum",
			wantSkip: true,
		},
		// .lock files
		{
			name:     "package-lock.json",
			filePath: "package-lock.json",
			wantSkip: true,
		},
		{
			name:     "yarn.lock",
			filePath: "yarn.lock",
			wantSkip: true,
		},
		{
			name:     "Cargo.lock",
			filePath: "Cargo.lock",
			wantSkip: true,
		},
		// .min files
		{
			name:     "app.min.js",
			filePath: "app.min.js",
			wantSkip: true,
		},
		{
			name:     "style.min.css",
			filePath: "style.min.css",
			wantSkip: true,
		},
		// .map files
		{
			name:     "app.js.map",
			filePath: "app.js.map",
			wantSkip: true,
		},
		// node_modules/
		{
			name:     "node_modules/file.js",
			filePath: "node_modules/file.js",
			wantSkip: true,
		},
		{
			name:     "package/node_modules/lib/index.js",
			filePath: "package/node_modules/lib/index.js",
			wantSkip: true,
		},
		// .pb.go files
		{
			name:     "proto.pb.go",
			filePath: "proto.pb.go",
			wantSkip: true,
		},
		// _generated.go files
		{
			name:     "types_generated.go",
			filePath: "types_generated.go",
			wantSkip: true,
		},
		{
			name:     "api_generated.json",
			filePath: "api_generated.json",
			wantSkip: true,
		},
		// vendor/
		{
			name:     "vendor/github.com/pkg/errors/errors.go",
			filePath: "vendor/github.com/pkg/errors/errors.go",
			wantSkip: true,
		},
		// .git/
		{
			name:     ".git/config",
			filePath: ".git/config",
			wantSkip: true,
		},
		// image files
		{
			name:     "icon.svg",
			filePath: "icon.svg",
			wantSkip: true,
		},
		{
			name:     "logo.png",
			filePath: "logo.png",
			wantSkip: true,
		},
		{
			name:     "banner.jpg",
			filePath: "banner.jpg",
			wantSkip: true,
		},
		{
			name:     "favicon.ico",
			filePath: "favicon.ico",
			wantSkip: true,
		},
		// coverage files
		{
			name:     "coverage.out",
			filePath: "coverage.out",
			wantSkip: true,
		},
		{
			name:     "coverage.html",
			filePath: "coverage.html",
			wantSkip: true,
		},
		{
			name:     ".test",
			filePath: "main.test",
			wantSkip: true,
		},
		{
			name:     ".out",
			filePath: "program.out",
			wantSkip: true,
		},
		// regular Go files - should NOT be skipped
		{
			name:     "main.go",
			filePath: "main.go",
			wantSkip: false,
		},
		{
			name:     "pkg/utils/file.go",
			filePath: "pkg/utils/file.go",
			wantSkip: false,
		},
		{
			name:     "internal/config/config.go",
			filePath: "internal/config/config.go",
			wantSkip: false,
		},
		// regular JSON files - should NOT be skipped
		{
			name:     "config.json",
			filePath: "config.json",
			wantSkip: false,
		},
		// regular YAML files - should NOT be skipped
		{
			name:     "values.yaml",
			filePath: "values.yaml",
			wantSkip: false,
		},
		// regular JS/CSS files - should NOT be skipped
		{
			name:     "app.js",
			filePath: "app.js",
			wantSkip: false,
		},
		{
			name:     "style.css",
			filePath: "style.css",
			wantSkip: false,
		},
		// markdown files - should NOT be skipped
		{
			name:     "README.md",
			filePath: "README.md",
			wantSkip: false,
		},
		// test files - should NOT be skipped
		{
			name:     "main_test.go",
			filePath: "main_test.go",
			wantSkip: false,
		},
		{
			name:     "utils_test.go",
			filePath: "utils_test.go",
			wantSkip: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldSkipFileForContext(tt.filePath)
			assert.Equal(t, tt.wantSkip, got)
		})
	}
}

func TestIsImportantComment(t *testing.T) {
	tests := []struct {
		name    string
		comment string
		wantImp bool
	}{
		// Keywords that trigger importance
		{
			name:    "CRITICAL keyword",
			comment: "// CRITICAL: This is critical",
			wantImp: true,
		},
		{
			name:    "IMPORTANT keyword",
			comment: "// IMPORTANT: Don't change this",
			wantImp: true,
		},
		{
			name:    "NOTE: keyword",
			comment: "// NOTE: Keep this for backwards compatibility",
			wantImp: true,
		},
		{
			name:    "WARNING keyword",
			comment: "// WARNING: This may cause issues",
			wantImp: true,
		},
		{
			name:    "TODO: keyword",
			comment: "// TODO: Implement this later",
			wantImp: true,
		},
		{
			name:    "FIXME keyword",
			comment: "// FIXME: Fix this bug",
			wantImp: true,
		},
		{
			name:    "HACK keyword",
			comment: "// HACK: This is a temporary workaround",
			wantImp: true,
		},
		{
			name:    "BUG keyword",
			comment: "// BUG: Known issue",
			wantImp: true,
		},
		{
			name:    "SECURITY keyword",
			comment: "// SECURITY: Sensitive operation",
			wantImp: true,
		},
		{
			name:    "FIX keyword",
			comment: "// FIX: Patch for issue #123",
			wantImp: true,
		},
		{
			name:    "WORKAROUND keyword",
			comment: "// WORKAROUND: Temporary solution",
			wantImp: true,
		},
		{
			name:    "BECAUSE keyword",
			comment: "// BECAUSE: We need to maintain compatibility",
			wantImp: true,
		},
		{
			name:    "REASON: keyword",
			comment: "// REASON: Explaining the design choice",
			wantImp: true,
		},
		{
			name:    "WHY: keyword",
			comment: "// WHY: This approach was chosen",
			wantImp: true,
		},
		{
			name:    "INTENT: keyword",
			comment: "// INTENT: Clear purpose of code",
			wantImp: true,
		},
		{
			name:    "PURPOSE: keyword",
			comment: "// PURPOSE: This function's goal",
			wantImp: true,
		},
		// Case insensitive matching
		{
			name:    "lowercase critical",
			comment: "// critical: important",
			wantImp: true,
		},
		{
			name:    "Mixed Case Todo",
			comment: "// ToDo: Do this later",
			wantImp: true,
		},
		{
			name:    "uppercase HACK",
			comment: "// HACK: Temporary fix",
			wantImp: true,
		},
		// Long comment (>50 chars starting with //)
		{
			name:    "long comment 1",
			comment: "// This is a very long comment that explains something important and goes over 50 characters",
			wantImp: true,
		},
		{
			name:    "long comment 2",
			comment: "// The following code implements a complex algorithm that requires careful consideration of edge cases and performance implications",
			wantImp: true,
		},
		// Regular short comments - NOT important
		{
			name:    "short comment",
			comment: "// do something",
			wantImp: false,
		},
		{
			name:    "short comment 2",
			comment: "// initialize variable",
			wantImp: false,
		},
		{
			name:    "comment 49 chars",
			comment: "// this is exactly forty nine characters long!!!",
			wantImp: false,
		},
		// Empty comments - NOT important
		{
			name:    "empty comment",
			comment: "",
			wantImp: false,
		},
		{
			name:    "only slashes",
			comment: "//",
			wantImp: false,
		},
		// Hash-style comments (used in shell, python, etc.)
		{
			name:    "# CRITICAL comment",
			comment: "# CRITICAL: This is critical",
			wantImp: true,
		},
		{
			name:    "# TODO comment",
			comment: "# TODO: Implement this",
			wantImp: true,
		},
		{
			name:    "# short comment",
			comment: "# do something",
			wantImp: false,
		},
		{
			name:    "# long comment",
			comment: "# This is a very long comment that explains something important and goes over 50 characters",
			wantImp: true,
		},
		// Comments with keywords in the middle
		{
			name:    "keyword in middle",
			comment: "// There is a TODO: in this comment",
			wantImp: true,
		},
		{
			name:    "security in middle",
			comment: "// This is a SECURITY vulnerability",
			wantImp: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isImportantComment(tt.comment)
			assert.Equal(t, tt.wantImp, got)
		})
	}
}
