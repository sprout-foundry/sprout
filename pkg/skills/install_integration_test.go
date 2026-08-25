//go:build !js

package skills

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// writeSKILLMD writes a minimal valid SKILL.md to the given directory.
func writeSKILLMD(t *testing.T, dir, name, description string) {
	t.Helper()
	content := "---\nname: " + name + "\ndescription: " + description + "\n---\n# " + name + "\n\nBody content.\n"
	if err := os.WriteFile(filepath.Join(dir, SkillFileName), []byte(content), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
}

func TestIntegration_RegistryInstall_ListRemoveReinstall(t *testing.T) {
	installDir := t.TempDir()
	t.Setenv("SPROUT_SKILLS_DIR", installDir)

	// Create fake registry source with skills/<id>/SKILL.md layout.
	srcRoot := t.TempDir()
	skillSubdir := filepath.Join(srcRoot, "skills", "integration-skill")
	if err := os.MkdirAll(skillSubdir, 0o755); err != nil {
		t.Fatalf("mkdir skill subdir: %v", err)
	}
	writeSKILLMD(t, skillSubdir, "integration-skill", "Integration test skill")

	fileURL := "file://" + srcRoot
	fakeReg := &Registry{
		Version: 1,
		Skills: []RegistryEntry{
			{
				ID:          "integration-skill",
				Name:        "Integration Skill",
				Description: "Integration test skill",
				GitURL:      fileURL,
				GitRef:      "main",
				PathInRepo:  "skills/integration-skill",
			},
		},
	}
	RegistryOverrideForTest(fakeReg)
	defer RegistryOverrideForTest(nil)

	// 1. Install from registry.
	results, err := InstallFromRegistry(context.Background(), "integration-skill", InstallOptions{})
	if err != nil {
		t.Fatalf("InstallFromRegistry: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	skillDir := filepath.Join(installDir, "integration-skill")

	// 2. Assert install dir layout.
	if _, err := os.Stat(filepath.Join(skillDir, SkillFileName)); os.IsNotExist(err) {
		t.Error("SKILL.md not found in install dir")
	}
	if _, err := os.Stat(filepath.Join(skillDir, OriginMetadataFile)); os.IsNotExist(err) {
		t.Error(".sprout-origin.json not found in install dir")
	}

	// 3. Assert origin content.
	origin, err := LoadOrigin(skillDir)
	if err != nil {
		t.Fatalf("LoadOrigin: %v", err)
	}
	if origin.Type != "registry" {
		t.Errorf("origin.Type = %q, want %q", origin.Type, "registry")
	}
	if origin.RegistryID != "integration-skill" {
		t.Errorf("origin.RegistryID = %q, want %q", origin.RegistryID, "integration-skill")
	}
	if origin.URL != fileURL {
		t.Errorf("origin.URL = %q, want %q", origin.URL, fileURL)
	}

	// 4. ReadDir shows 1 entry.
	entries, err := os.ReadDir(installDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	var dirCount int
	for _, e := range entries {
		if e.IsDir() {
			dirCount++
		}
	}
	if dirCount != 1 {
		t.Errorf("expected 1 entry in skills dir, got %d", dirCount)
	}

	// 5. Uninstall removes it.
	if err := Uninstall("integration-skill"); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if _, err := os.Stat(skillDir); !os.IsNotExist(err) {
		t.Error("expected skill dir to be removed after uninstall")
	}

	// 6. Reinstall re-creates SKILL.md.
	results, err = InstallFromRegistry(context.Background(), "integration-skill", InstallOptions{})
	if err != nil {
		t.Fatalf("reinstall: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result from reinstall, got %d", len(results))
	}
	if _, err := os.Stat(filepath.Join(skillDir, SkillFileName)); os.IsNotExist(err) {
		t.Error("SKILL.md not found after reinstall")
	}
}

func TestIntegration_MalformedFrontmatter_Rejected(t *testing.T) {
	tests := []struct {
		name         string
		skillContent string
	}{
		{
			name:         "missing name",
			skillContent: "---\ndescription: foo\n---\n# Body\n",
		},
		{
			name:         "missing description",
			skillContent: "---\nname: foo\n---\n# Body\n",
		},
		{
			name:         "empty name",
			skillContent: "---\nname:   \ndescription: foo\n---\n# Body\n",
		},
		{
			name:         "empty description",
			skillContent: "---\nname: foo\ndescription:   \n---\n# Body\n",
		},
		{
			name:         "no frontmatter at all",
			skillContent: "# Just body text\nNo frontmatter here.\n",
		},
		{
			name:         "unclosed frontmatter",
			skillContent: "---\nname: foo\ndescription: bar\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srcDir := t.TempDir()
			installDir := t.TempDir()
			t.Setenv("SPROUT_SKILLS_DIR", installDir)

			if err := os.WriteFile(filepath.Join(srcDir, SkillFileName), []byte(tt.skillContent), 0o644); err != nil {
				t.Fatalf("write SKILL.md: %v", err)
			}

			_, err := InstallFromPath(srcDir, InstallOptions{})
			if err == nil {
				t.Fatal("expected error for malformed frontmatter, got nil")
			}
		})
	}
}
