//go:build !js

package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/skills"
	"github.com/sprout-foundry/sprout/pkg/testutil"
)

// writeSkillMD writes a minimal valid SKILL.md to the given directory.
func writeSkillMD(t *testing.T, dir, name, description string) {
	t.Helper()
	content := "---\nname: " + name + "\ndescription: " + description + "\n---\n# " + name + "\n\nBody content.\n"
	if err := os.WriteFile(filepath.Join(dir, skills.SkillFileName), []byte(content), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
}

func TestSkillCommand_Integration_RegistryInstallListRemove(t *testing.T) {
	installDir := t.TempDir()
	t.Setenv("SPROUT_SKILLS_DIR", installDir)

	// Build a temp source repo with a skills/<id>/SKILL.md subdirectory.
	srcRoot := t.TempDir()
	skillSubdir := filepath.Join(srcRoot, "skills", "int-registry-skill")
	if err := os.MkdirAll(skillSubdir, 0o755); err != nil {
		t.Fatalf("mkdir skill subdir: %v", err)
	}
	writeSkillMD(t, skillSubdir, "int-registry-skill", "Integration registry skill")

	fakeReg := &skills.Registry{
		Version: 1,
		Skills: []skills.RegistryEntry{
			{
				ID:          "int-registry-skill",
				Name:        "Int Registry Skill",
				Description: "Integration registry skill",
				GitURL:      "file://" + srcRoot,
				GitRef:      "main",
				PathInRepo:  "skills/int-registry-skill",
			},
		},
	}
	skills.RegistryOverrideForTest(fakeReg)
	defer skills.RegistryOverrideForTest(nil)

	// 1. Install from registry via CLI command.
	out, _, err := runSkillExecute(t, []string{"install", "int-registry-skill"})
	if err != nil {
		t.Fatalf("install failed: %v", err)
	}
	if !strings.Contains(out, "installed int-registry-skill") {
		t.Errorf("expected success message, got: %q", out)
	}

	// 2. List — output contains the id AND the word "registry".
	out, _, err = runSkillExecute(t, []string{"list"})
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if !strings.Contains(out, "int-registry-skill") {
		t.Errorf("expected skill id in list output, got: %q", out)
	}
	if !strings.Contains(out, "registry") {
		t.Errorf("expected 'registry' in list output, got: %q", out)
	}

	// 3. Remove — no error AND output contains "removed".
	out, _, err = runSkillExecute(t, []string{"remove", "int-registry-skill"})
	if err != nil {
		t.Fatalf("remove failed: %v", err)
	}
	if !strings.Contains(out, "removed") {
		t.Errorf("expected 'removed' in output, got: %q", out)
	}

	// 4. Verify the install dir is gone.
	skillDir := filepath.Join(installDir, "int-registry-skill")
	if _, err := os.Stat(skillDir); !os.IsNotExist(err) {
		t.Error("expected skill dir to be removed after uninstall")
	}
}

func TestSkillCommand_Integration_CLI_FlagParsing(t *testing.T) {
	tests := []struct {
		name          string
		args          []string
		wantErrSubstr string
	}{
		{
			name:          "unknown action",
			args:          []string{"bogus"},
			wantErrSubstr: "unknown action",
		},
		{
			name:          "install missing source",
			args:          []string{"install"},
			wantErrSubstr: "requires a source",
		},
		{
			name:          "install bad flag",
			args:          []string{"install", "/tmp/x", "--what"},
			wantErrSubstr: "unknown flag",
		},
		{
			name:          "install ref without value",
			args:          []string{"install", "url", "--ref"},
			wantErrSubstr: "--ref requires",
		},
		{
			name:          "update missing id",
			args:          []string{"update"},
			wantErrSubstr: "requires a skill ID",
		},
		{
			name:          "remove missing id",
			args:          []string{"remove"},
			wantErrSubstr: "requires a skill ID",
		},
		{
			name:          "empty action",
			args:          []string{},
			wantErrSubstr: "missing action",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmp := t.TempDir()
			t.Setenv("SPROUT_SKILLS_DIR", tmp)

			_, _, err := runSkillExecute(t, tt.args)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErrSubstr)
			}
			if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tt.wantErrSubstr)) {
				t.Errorf("expected error containing %q, got: %v", tt.wantErrSubstr, err)
			}
		})
	}
}

func TestSkillCommand_Integration_JSONListOutput(t *testing.T) {
	installDir := t.TempDir()
	t.Setenv("SPROUT_SKILLS_DIR", installDir)

	// Install two skills from local paths via the skills package directly.
	srcAlpha := t.TempDir()
	writeSkillMD(t, srcAlpha, "alpha-skill", "Alpha skill for JSON test")
	_, err := skills.InstallFromPath(srcAlpha, skills.InstallOptions{})
	if err != nil {
		t.Fatalf("install alpha-skill: %v", err)
	}

	srcBeta := t.TempDir()
	writeSkillMD(t, srcBeta, "beta-skill", "Beta skill for JSON test")
	_, err = skills.InstallFromPath(srcBeta, skills.InstallOptions{})
	if err != nil {
		t.Fatalf("install beta-skill: %v", err)
	}

	// Call ExecuteWithJSONOutput and capture stdout.
	cmd := &SkillCommand{}
	output := testutil.CaptureStdout(t, func() {
		ctx := &CommandContext{OutputFormat: OutputJSON}
		if err := cmd.ExecuteWithJSONOutput([]string{"list"}, nil, ctx); err != nil {
			t.Errorf("ExecuteWithJSONOutput: %v", err)
		}
	})

	var result []map[string]string
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("invalid JSON %q: %v", output, err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 skills in JSON output, got %d", len(result))
	}

	// Verify both skill IDs are present.
	ids := make(map[string]bool)
	for _, entry := range result {
		ids[entry["id"]] = true
	}
	if !ids["alpha-skill"] {
		t.Error("expected alpha-skill in JSON output")
	}
	if !ids["beta-skill"] {
		t.Error("expected beta-skill in JSON output")
	}
}
