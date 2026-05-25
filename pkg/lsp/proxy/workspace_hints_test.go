//go:build !js

package proxy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSuggestServersRust(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	result := SuggestServers(dir)
	if result == nil {
		t.Fatal("expected suggestions, got nil")
	}

	found := false
	for _, s := range result {
		if s.ServerID == "rust" {
			found = true
			if s.ProjectFile != "Cargo.toml" {
				t.Errorf("expected ProjectFile 'Cargo.toml', got %q", s.ProjectFile)
			}
			if !strings.Contains(s.Language, "rust") {
				t.Errorf("expected Language to contain 'rust', got %q", s.Language)
			}
			break
		}
	}
	if !found {
		t.Error("expected rust server suggestion")
	}
}

func TestSuggestServersPython(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	result := SuggestServers(dir)
	if result == nil {
		t.Fatal("expected suggestions, got nil")
	}

	found := false
	for _, s := range result {
		if s.ServerID == "python" {
			found = true
			if s.ProjectFile != "requirements.txt" {
				t.Errorf("expected ProjectFile 'requirements.txt', got %q", s.ProjectFile)
			}
			if !strings.Contains(s.Language, "python") {
				t.Errorf("expected Language to contain 'python', got %q", s.Language)
			}
			break
		}
	}
	if !found {
		t.Error("expected python server suggestion")
	}
}

func TestSuggestServersGo(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\n"), 0644); err != nil {
		t.Fatal(err)
	}

	result := SuggestServers(dir)
	if result == nil {
		t.Fatal("expected suggestions, got nil")
	}

	found := false
	for _, s := range result {
		if s.ServerID == "go" {
			found = true
			if s.ProjectFile != "go.mod" {
				t.Errorf("expected ProjectFile 'go.mod', got %q", s.ProjectFile)
			}
			if !strings.Contains(s.Language, "go") {
				t.Errorf("expected Language to contain 'go', got %q", s.Language)
			}
			break
		}
	}
	if !found {
		t.Error("expected go server suggestion")
	}
}

func TestSuggestServersMultiple(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte(""), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	result := SuggestServers(dir)
	if result == nil {
		t.Fatal("expected suggestions, got nil")
	}

	if len(result) < 2 {
		t.Fatalf("expected at least 2 suggestions, got %d", len(result))
	}

	foundRust := false
	foundPython := false
	for _, s := range result {
		if s.ServerID == "rust" {
			foundRust = true
		}
		if s.ServerID == "python" {
			foundPython = true
		}
	}
	if !foundRust {
		t.Error("expected rust server suggestion in multiple-detection test")
	}
	if !foundPython {
		t.Error("expected python server suggestion in multiple-detection test")
	}
}

func TestSuggestServersEmptyDir(t *testing.T) {
	dir := t.TempDir()

	result := SuggestServers(dir)

	// Empty dir should return empty slice (or nil), not crash
	if len(result) != 0 {
		t.Errorf("expected no suggestions for empty dir, got %d", len(result))
	}
}

func TestSuggestServersNonExistentDir(t *testing.T) {
	result := SuggestServers("/nonexistent/path/that/does/not/exist")

	if result != nil {
		t.Errorf("expected nil for non-existent dir, got %d suggestions", len(result))
	}
}

func TestSuggestServersCSharp(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "test.csproj"), []byte("<Project />"), 0644); err != nil {
		t.Fatal(err)
	}

	result := SuggestServers(dir)
	if result == nil {
		t.Fatal("expected suggestions, got nil")
	}

	found := false
	for _, s := range result {
		if s.ServerID == "csharp" {
			found = true
			if !strings.Contains(s.ProjectFile, "csproj") {
				t.Errorf("expected ProjectFile to contain 'csproj', got %q", s.ProjectFile)
			}
			if !strings.Contains(s.Language, "csharp") {
				t.Errorf("expected Language to contain 'csharp', got %q", s.Language)
			}
			break
		}
	}
	if !found {
		t.Error("expected csharp server suggestion")
	}
}

func TestSuggestServersJava(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pom.xml"), []byte("<project />"), 0644); err != nil {
		t.Fatal(err)
	}

	result := SuggestServers(dir)
	if result == nil {
		t.Fatal("expected suggestions, got nil")
	}

	found := false
	for _, s := range result {
		if s.ServerID == "java" {
			found = true
			if s.ProjectFile != "pom.xml" {
				t.Errorf("expected ProjectFile 'pom.xml', got %q", s.ProjectFile)
			}
			if !strings.Contains(s.Language, "java") {
				t.Errorf("expected Language to contain 'java', got %q", s.Language)
			}
			break
		}
	}
	if !found {
		t.Error("expected java server suggestion")
	}
}

func TestSuggestServersFileNotDirectory(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "somefile.txt")
	if err := os.WriteFile(filePath, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	// Pass a file path instead of a directory
	result := SuggestServers(filePath)

	if result != nil {
		t.Errorf("expected nil when workspaceRoot is a file, got %d suggestions", len(result))
	}
}

func TestSuggestServersAbsoluteAndRelativePaths(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Use absolute path
	result := SuggestServers(dir)
	if result == nil {
		t.Fatal("expected suggestions for absolute path")
	}
	found := false
	for _, s := range result {
		if s.ServerID == "go" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected go server for absolute path")
	}
}
