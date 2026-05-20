//go:build !js

package webui

import (
	"testing"
)

func TestGitReviewShouldSkipFileForContext(t *testing.T) {
	tests := []struct {
		name string
		path string
		want bool
	}{
		// Lock files
		{"go.sum", "go.sum", true},
		{"go.lock", "go.lock", true},
		{"package-lock.json", "package-lock.json", true},
		{"yarn.lock", "yarn.lock", true},
		// Minified / maps
		{"app.min.js", "app.min.js", true},
		{"source.map", "source.map", true},
		// node_modules
		{"node_modules/thing.js", "node_modules/thing.js", true},
		{"vendor/pkg.go", "vendor/pkg.go", true},
		// Generated code
		{"file.pb.go", "file.pb.go", true},
		{"_generated.go", "_generated.go", true},
		{"_generated.js", "_generated.js", true},
		// Coverage / output
		{"coverage.out", "coverage.out", true},
		{"coverage.html", "coverage.html", true},
		{"test.test", "test.test", true},
		{"build.out", "build.out", true},
		// Images
		{"logo.png", "logo.png", true},
		{"photo.jpg", "photo.jpg", true},
		{"icon.ico", "icon.ico", true},
		// .git directory
		{".git/hooks/pre-commit", ".git/hooks/pre-commit", true},
		// Normal source files — should NOT skip
		{"main.go", "main.go", false},
		{"src/util.ts", "src/util.ts", false},
		{"README.md", "README.md", false},
		{"Makefile", "Makefile", false},
		{"config.json", "config.json", false},
		{"test.go", "test.go", false},
		{"Dockerfile", "Dockerfile", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := gitReviewShouldSkipFileForContext(tt.path)
			if got != tt.want {
				t.Errorf("gitReviewShouldSkipFileForContext(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}
