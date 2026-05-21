package configuration

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadAllowedSkills_NoFile(t *testing.T) {
	tmpDir := t.TempDir()
	allowed := ReadAllowedSkills(tmpDir)
	if allowed != nil {
		t.Errorf("expected nil when file doesn't exist, got %v", allowed)
	}
}

func TestReadAllowedSkills_WithFile(t *testing.T) {
	tmpDir := t.TempDir()
	sproutDir := filepath.Join(tmpDir, ".sprout")
	os.MkdirAll(sproutDir, 0755)

	content := "project-planning\nbrowse-debugging\n# comment line\n\nrepo-onboarding\n"
	os.WriteFile(filepath.Join(sproutDir, "allowed_skills"), []byte(content), 0644)

	allowed := ReadAllowedSkills(tmpDir)

	if len(allowed) != 3 {
		t.Fatalf("expected 3 allowed skills, got %d", len(allowed))
	}
	for _, id := range []string{"project-planning", "browse-debugging", "repo-onboarding"} {
		if !allowed[id] {
			t.Errorf("expected %q to be allowed", id)
		}
	}
}

func TestWriteAllowedSkills(t *testing.T) {
	tmpDir := t.TempDir()

	ids := []string{"zebra", "alpha", "mid"}
	if err := WriteAllowedSkills(tmpDir, ids); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(tmpDir, ".sprout", "allowed_skills"))
	if err != nil {
		t.Fatal(err)
	}

	expected := "alpha\nmid\nzebra\n"
	if string(data) != expected {
		t.Errorf("expected sorted output %q, got %q", expected, string(data))
	}
}

func TestAllowedSkillsPath(t *testing.T) {
	got := AllowedSkillsPath("/home/user/project")
	want := "/home/user/project/.sprout/allowed_skills"
	if got != want {
		t.Errorf("AllowedSkillsPath() = %q, want %q", got, want)
	}
}
