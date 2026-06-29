//go:build !js

package skills

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeValidSkillMD writes a minimal valid SKILL.md to the given directory.
func writeValidSkillMD(t *testing.T, dir string) {
	t.Helper()
	content := `---
name: my-test-skill
description: A test skill
---
# Body
`
	if err := os.WriteFile(filepath.Join(dir, SkillFileName), []byte(content), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
}

func TestInstallFromPath_ValidSkill(t *testing.T) {
	t.Setenv("SPROUT_SKILLS_DIR", t.TempDir())

	srcDir := t.TempDir()
	writeValidSkillMD(t, srcDir)

	results, err := InstallFromPath(srcDir, InstallOptions{})
	if err != nil {
		t.Fatalf("InstallFromPath: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	res := results[0]
	if res.SkillID != "my-test-skill" {
		t.Errorf("SkillID = %q, want %q", res.SkillID, "my-test-skill")
	}

	// Check SKILL.md exists in install dir
	skillMD := filepath.Join(res.InstallDir, SkillFileName)
	if _, err := os.Stat(skillMD); os.IsNotExist(err) {
		t.Error("SKILL.md not found in install dir")
	}

	// Check origin file exists
	originFile := filepath.Join(res.InstallDir, OriginMetadataFile)
	if _, err := os.Stat(originFile); os.IsNotExist(err) {
		t.Error(".sprout-origin.json not found in install dir")
	}

	// Verify origin type
	origin, err := LoadOrigin(res.InstallDir)
	if err != nil {
		t.Fatalf("LoadOrigin: %v", err)
	}
	if origin.Type != "path" {
		t.Errorf("origin.Type = %q, want %q", origin.Type, "path")
	}
}

func TestInstallFromPath_InvalidFrontmatter(t *testing.T) {
	t.Setenv("SPROUT_SKILLS_DIR", t.TempDir())

	srcDir := t.TempDir()
	// SKILL.md missing description
	content := `---
name: incomplete-skill
---
# Body
`
	if err := os.WriteFile(filepath.Join(srcDir, SkillFileName), []byte(content), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}

	_, err := InstallFromPath(srcDir, InstallOptions{})
	if !errors.Is(err, ErrInvalidFrontmatter) {
		t.Errorf("expected ErrInvalidFrontmatter, got: %v", err)
	}
}

func TestInstallFromPath_AlreadyInstalled_NoForce(t *testing.T) {
	t.Setenv("SPROUT_SKILLS_DIR", t.TempDir())

	srcDir := t.TempDir()
	writeValidSkillMD(t, srcDir)

	// First install
	_, err := InstallFromPath(srcDir, InstallOptions{})
	if err != nil {
		t.Fatalf("first InstallFromPath: %v", err)
	}

	// Second install without force
	_, err = InstallFromPath(srcDir, InstallOptions{})
	if !errors.Is(err, ErrAlreadyInstalled) {
		t.Errorf("expected ErrAlreadyInstalled, got: %v", err)
	}
}

func TestInstallFromPath_AlreadyInstalled_Force(t *testing.T) {
	t.Setenv("SPROUT_SKILLS_DIR", t.TempDir())

	srcDir := t.TempDir()
	writeValidSkillMD(t, srcDir)

	// First install
	_, err := InstallFromPath(srcDir, InstallOptions{})
	if err != nil {
		t.Fatalf("first InstallFromPath: %v", err)
	}

	// Second install with force
	results, err := InstallFromPath(srcDir, InstallOptions{Force: true})
	if err != nil {
		t.Fatalf("InstallFromPath with force: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	// Verify the skill is still there
	skillMD := filepath.Join(results[0].InstallDir, SkillFileName)
	if _, err := os.Stat(skillMD); os.IsNotExist(err) {
		t.Error("SKILL.md not found after force reinstall")
	}
}

func TestLoadOrigin_RoundTrip(t *testing.T) {
	tmpDir := t.TempDir()

	origin := Origin{
		Type:        "git",
		URL:         "https://github.com/example/skill.git",
		Ref:         "main",
		InstalledAt: time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC),
	}

	data, err := json.MarshalIndent(origin, "", "  ")
	if err != nil {
		t.Fatalf("marshal origin: %v", err)
	}

	if err := os.WriteFile(filepath.Join(tmpDir, OriginMetadataFile), data, 0o644); err != nil {
		t.Fatalf("write origin file: %v", err)
	}

	loaded, err := LoadOrigin(tmpDir)
	if err != nil {
		t.Fatalf("LoadOrigin: %v", err)
	}

	if loaded.Type != origin.Type {
		t.Errorf("Type = %q, want %q", loaded.Type, origin.Type)
	}
	if loaded.URL != origin.URL {
		t.Errorf("URL = %q, want %q", loaded.URL, origin.URL)
	}
	if loaded.Ref != origin.Ref {
		t.Errorf("Ref = %q, want %q", loaded.Ref, origin.Ref)
	}
	if !loaded.InstalledAt.Equal(origin.InstalledAt) {
		t.Errorf("InstalledAt = %v, want %v", loaded.InstalledAt, origin.InstalledAt)
	}
}

func TestUninstall_RemovesDir(t *testing.T) {
	t.Setenv("SPROUT_SKILLS_DIR", t.TempDir())

	srcDir := t.TempDir()
	writeValidSkillMD(t, srcDir)

	results, err := InstallFromPath(srcDir, InstallOptions{})
	if err != nil {
		t.Fatalf("InstallFromPath: %v", err)
	}
	installDir := results[0].InstallDir

	// Verify it exists
	if _, err := os.Stat(installDir); os.IsNotExist(err) {
		t.Fatal("install dir should exist before uninstall")
	}

	// Uninstall
	if err := Uninstall(results[0].SkillID); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}

	// Verify it's gone
	if _, err := os.Stat(installDir); !os.IsNotExist(err) {
		t.Error("install dir should be removed after uninstall")
	}
}

func TestUpdate_PathOrigin_ReturnsErrNotGitOrigin(t *testing.T) {
	t.Setenv("SPROUT_SKILLS_DIR", t.TempDir())

	srcDir := t.TempDir()
	writeValidSkillMD(t, srcDir)

	results, err := InstallFromPath(srcDir, InstallOptions{})
	if err != nil {
		t.Fatalf("InstallFromPath: %v", err)
	}

	_, err = Update(context.Background(), results[0].SkillID, InstallOptions{})
	if !errors.Is(err, ErrNotGitOrigin) {
		t.Errorf("expected ErrNotGitOrigin, got: %v", err)
	}
}

func TestDefaultSkillsDir_EnvOverride(t *testing.T) {
	want := t.TempDir()
	t.Setenv("SPROUT_SKILLS_DIR", want)

	got, err := DefaultSkillsDir()
	if err != nil {
		t.Fatalf("DefaultSkillsDir: %v", err)
	}
	if got != want {
		t.Errorf("DefaultSkillsDir() = %q, want %q", got, want)
	}
}

func TestValidateFrontmatter(t *testing.T) {
	tests := []struct {
		name        string
		fm          SkillFrontmatter
		wantErr     bool
		description string
	}{
		{
			name:        "empty name",
			fm:          SkillFrontmatter{Name: "", Description: "A skill"},
			wantErr:     true,
			description: "empty name should be invalid",
		},
		{
			name:        "empty description",
			fm:          SkillFrontmatter{Name: "my-skill", Description: ""},
			wantErr:     true,
			description: "empty description should be invalid",
		},
		{
			name:        "both empty",
			fm:          SkillFrontmatter{Name: "", Description: ""},
			wantErr:     true,
			description: "both empty should be invalid",
		},
		{
			name:        "whitespace-only name",
			fm:          SkillFrontmatter{Name: "   ", Description: "A skill"},
			wantErr:     true,
			description: "whitespace-only name should be invalid",
		},
		{
			name:        "valid name and description",
			fm:          SkillFrontmatter{Name: "my-skill", Description: "A useful skill"},
			wantErr:     false,
			description: "valid frontmatter should pass",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateFrontmatter(tt.fm)
			if tt.wantErr {
				if err == nil {
					t.Errorf("%s: expected error, got nil", tt.description)
				} else if !errors.Is(err, ErrInvalidFrontmatter) {
					t.Errorf("%s: expected ErrInvalidFrontmatter, got: %v", tt.description, err)
				}
			} else {
				if err != nil {
					t.Errorf("%s: expected nil, got: %v", tt.description, err)
				}
			}
		})
	}
}

func TestInstallFromRegistry_OverrideForTest(t *testing.T) {
	// Set up a temp skills install dir.
	t.Setenv("SPROUT_SKILLS_DIR", t.TempDir())

	// Create a fake skill source on disk.
	srcDir := t.TempDir()
	skillSubdir := filepath.Join(srcDir, "skills", "test-skill")
	if err := os.MkdirAll(skillSubdir, 0o755); err != nil {
		t.Fatalf("create skill subdir: %v", err)
	}
	// Write a SKILL.md whose frontmatter name matches the registry ID.
	skillContent := `---
name: test-skill
description: A test skill for InstallFromRegistry
---
# Test Skill Body
`
	if err := os.WriteFile(filepath.Join(skillSubdir, SkillFileName), []byte(skillContent), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}

	// Build a fake registry entry pointing to the local source.
	fakeReg := &Registry{
		Version: 1,
		Skills: []RegistryEntry{
			{
				ID:          "test-skill",
				Name:        "Test Skill",
				Description: "A test skill for InstallFromRegistry",
				GitURL:      "file://" + srcDir,
				GitRef:      "main",
				PathInRepo:  "skills/test-skill",
			},
		},
	}

	// Override the registry for this test.
	RegistryOverrideForTest(fakeReg)
	defer RegistryOverrideForTest(nil)

	// Run the install.
	results, err := InstallFromRegistry(context.Background(), "test-skill", InstallOptions{})
	if err != nil {
		t.Fatalf("InstallFromRegistry: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	res := results[0]
	if res.SkillID != "test-skill" {
		t.Errorf("SkillID = %q, want %q", res.SkillID, "test-skill")
	}

	// Check SKILL.md exists in install dir.
	skillMD := filepath.Join(res.InstallDir, SkillFileName)
	if _, err := os.Stat(skillMD); os.IsNotExist(err) {
		t.Error("SKILL.md not found in install dir")
	}

	// Verify origin metadata.
	if res.Origin.Type != "registry" {
		t.Errorf("origin.Type = %q, want %q", res.Origin.Type, "registry")
	}
	if res.Origin.RegistryID != "test-skill" {
		t.Errorf("origin.RegistryID = %q, want %q", res.Origin.RegistryID, "test-skill")
	}
}

func TestInstallFromRegistry_NotFound(t *testing.T) {
	// Explicitly clear any test override so this test always exercises the
	// embedded registry path, even if other tests run in parallel.
	RegistryOverrideForTest(nil)
	defer RegistryOverrideForTest(nil)

	t.Setenv("SPROUT_SKILLS_DIR", t.TempDir())

	_, err := InstallFromRegistry(context.Background(), "does-not-exist", InstallOptions{})
	if err == nil {
		t.Fatal("expected error for unknown registry ID")
	}
	if !errors.Is(err, ErrRegistryNotFound) {
		t.Errorf("expected ErrRegistryNotFound, got: %v", err)
	}
}
