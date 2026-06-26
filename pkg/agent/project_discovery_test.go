package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseAgentsMd(t *testing.T) {
	tmpDir := t.TempDir()

	agentsPath := filepath.Join(tmpDir, "AGENTS.md")
	content := `# My Test Project

This is a test project for validating the parser.

Some additional content.
`
	if err := os.WriteFile(agentsPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	name, desc := ParseAgentsMd(agentsPath)

	if name != "My Test Project" {
		t.Errorf("Expected name 'My Test Project', got '%s'", name)
	}

	if desc != "This is a test project for validating the parser." {
		t.Errorf("Expected description about test project, got '%s'", desc)
	}
}

func TestParseAgentsMdNoDescription(t *testing.T) {
	tmpDir := t.TempDir()

	agentsPath := filepath.Join(tmpDir, "AGENTS.md")
	content := `# Minimal Project
`
	if err := os.WriteFile(agentsPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	name, desc := ParseAgentsMd(agentsPath)

	if name != "Minimal Project" {
		t.Errorf("Expected name 'Minimal Project', got '%s'", name)
	}

	if desc != "" {
		t.Errorf("Expected empty description, got '%s'", desc)
	}
}

func TestDetectLanguages(t *testing.T) {
	tmpDir := t.TempDir()

	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module test"), 0644); err != nil {
		t.Fatal(err)
	}

	languages := DetectLanguages(tmpDir)

	if len(languages) == 0 {
		t.Error("Expected to detect Go language")
	}

	foundGo := false
	for _, lang := range languages {
		if lang == "Go" {
			foundGo = true
			break
		}
	}

	if !foundGo {
		t.Errorf("Expected Go in detected languages, got %v", languages)
	}
}

func TestDetectLanguagesMultiple(t *testing.T) {
	tmpDir := t.TempDir()

	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module test"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "package.json"), []byte(`{"name":"test"}`), 0644); err != nil {
		t.Fatal(err)
	}

	languages := DetectLanguages(tmpDir)

	if len(languages) < 2 {
		t.Errorf("Expected at least 2 languages, got %d: %v", len(languages), languages)
	}
}

func TestHasGitRepo(t *testing.T) {
	tmpDir := t.TempDir()

	if hasGitRepo(tmpDir) {
		t.Error("Expected no .git directory initially")
	}

	gitPath := filepath.Join(tmpDir, ".git")
	if err := os.Mkdir(gitPath, 0755); err != nil {
		t.Fatal(err)
	}

	if !hasGitRepo(tmpDir) {
		t.Error("Expected to find .git directory after creation")
	}
}

func TestHasAgentsMdFile(t *testing.T) {
	tmpDir := t.TempDir()

	if hasAgentsMdFile(tmpDir) {
		t.Error("Expected no AGENTS.md file initially")
	}

	agentsPath := filepath.Join(tmpDir, "AGENTS.md")
	if err := os.WriteFile(agentsPath, []byte("# Test"), 0644); err != nil {
		t.Fatal(err)
	}

	if !hasAgentsMdFile(tmpDir) {
		t.Error("Expected to find AGENTS.md file after creation")
	}
}

func TestSortProjects(t *testing.T) {
	projects := []ProjectInfo{
		{Path: "/home/user/b", HasGitRepo: true, HasAgentsMd: false},
		{Path: "/home/user/a", HasGitRepo: false, HasAgentsMd: true},
		{Path: "/home/user/c", HasGitRepo: false, HasAgentsMd: false},
		{Path: "/home/user/d", HasGitRepo: true, HasAgentsMd: true},
	}

	sortProjects(projects)

	if !projects[0].HasAgentsMd || !projects[0].HasGitRepo {
		t.Errorf("Expected first project to have AGENTS.md and git, got HasAgentsMd=%v, HasGitRepo=%v",
			projects[0].HasAgentsMd, projects[0].HasGitRepo)
	}

	if !projects[1].HasAgentsMd || projects[1].HasGitRepo {
		t.Errorf("Expected second project to have AGENTS.md only, got HasAgentsMd=%v, HasGitRepo=%v",
			projects[1].HasAgentsMd, projects[1].HasGitRepo)
	}

	if projects[2].HasAgentsMd {
		t.Errorf("Expected third project to be git repo only, got HasAgentsMd=%v", projects[2].HasAgentsMd)
	}
}
