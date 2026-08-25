package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDiscoverProjects(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tmpDir := t.TempDir()

	project1 := filepath.Join(tmpDir, "project1")
	if err := os.Mkdir(project1, 0755); err != nil {
		t.Fatal(err)
	}

	project2 := filepath.Join(tmpDir, "project2")
	if err := os.Mkdir(project2, 0755); err != nil {
		t.Fatal(err)
	}

	gitDir := filepath.Join(project2, ".git")
	if err := os.Mkdir(gitDir, 0755); err != nil {
		t.Fatal(err)
	}

	goModPath := filepath.Join(project2, "go.mod")
	if err := os.WriteFile(goModPath, []byte("module test"), 0644); err != nil {
		t.Fatal(err)
	}

	agentsPath := filepath.Join(project2, "AGENTS.md")
	if err := os.WriteFile(agentsPath, []byte("# Special Project\nThis is a special project."), 0644); err != nil {
		t.Fatal(err)
	}

	projects, err := DiscoverProjects(tmpDir, 2)
	if err != nil {
		t.Fatal(err)
	}

	if len(projects) == 0 {
		t.Fatal("Expected to find at least one project")
	}

	foundProject2 := false
	for _, p := range projects {
		if p.Name == "Special Project" {
			foundProject2 = true
			if p.Description != "This is a special project." {
				t.Errorf("Expected description 'This is a special project.', got '%s'", p.Description)
			}
			if !p.HasGitRepo {
				t.Error("Expected project2 to have git repo")
			}
			if !p.HasAgentsMd {
				t.Error("Expected project2 to have AGENTS.md")
			}
			foundGo := false
			for _, lang := range p.Languages {
				if lang == "Go" {
					foundGo = true
					break
				}
			}
			if !foundGo {
				t.Errorf("Expected Go in languages for project2, got %v", p.Languages)
			}
		}
	}

	if !foundProject2 {
		t.Error("Expected to find project2 with name 'Special Project'")
	}

	if len(projects) > 50 {
		t.Errorf("Expected at most 50 projects, got %d", len(projects))
	}
}

func TestDiscoverProjectsTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping timeout test in short mode")
	}

	tmpDir := t.TempDir()

	deepPath := tmpDir
	for i := 0; i < 5; i++ {
		deepPath = filepath.Join(deepPath, "level"+string(rune('0'+i)))
		if err := os.Mkdir(deepPath, 0755); err != nil {
			t.Fatal(err)
		}
	}

	start := time.Now()
	projects, err := DiscoverProjects(tmpDir, 10)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatal(err)
	}

	if elapsed > 10*time.Second {
		t.Errorf("Discovery took too long: %v", elapsed)
	}

	t.Logf("Discovered %d projects in %v", len(projects), elapsed)
}
