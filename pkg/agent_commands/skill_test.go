//go:build !js

package commands

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/skills"
	"github.com/sprout-foundry/sprout/pkg/testutil"
)

// runSkillExecute invokes the testable internal dispatcher and captures output.
func runSkillExecute(t *testing.T, args []string) (string, string, error) {
	t.Helper()
	var out, errBuf strings.Builder
	err := executeSkillCommand(args, &out, &errBuf)
	return out.String(), errBuf.String(), err
}

// writeTempSkill creates a temp dir containing a valid SKILL.md file.
func writeTempSkill(t *testing.T, dirName string) string {
	t.Helper()
	root := t.TempDir()
	skillDir := filepath.Join(root, dirName)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir skill: %v", err)
	}
	content := "---\nname: " + dirName + "\ndescription: A test skill for unit tests\n---\n# Test skill body\n"
	if err := os.WriteFile(filepath.Join(skillDir, skills.SkillFileName), []byte(content), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	return skillDir
}

func TestSkillCommand_List_Empty(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SPROUT_SKILLS_DIR", tmp)

	out, _, err := runSkillExecute(t, []string{"list"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "No skills installed.") {
		t.Errorf("expected 'No skills installed.' in output, got: %q", out)
	}
}

func TestSkillCommand_List_OneSkill(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SPROUT_SKILLS_DIR", tmp)

	src := writeTempSkill(t, "demo-skill")
	if _, _, err := runSkillExecute(t, []string{"install", src}); err != nil {
		t.Fatalf("install failed: %v", err)
	}

	out, _, err := runSkillExecute(t, []string{"list"})
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if !strings.Contains(out, "demo-skill") {
		t.Errorf("expected demo-skill in output, got: %q", out)
	}
}

func TestSkillCommand_Install_FromPath(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SPROUT_SKILLS_DIR", tmp)

	src := writeTempSkill(t, "path-skill")
	out, _, err := runSkillExecute(t, []string{"install", src})
	if err != nil {
		t.Fatalf("install failed: %v", err)
	}
	if !strings.Contains(out, "installed path-skill") {
		t.Errorf("expected success message, got: %q", out)
	}

	installDir, err := skills.SkillInstallDir("path-skill")
	if err != nil {
		t.Fatalf("SkillInstallDir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(installDir, skills.OriginMetadataFile)); err != nil {
		t.Errorf("expected origin metadata file at %s: %v", installDir, err)
	}
}

func TestSkillCommand_Install_ForceFlag(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SPROUT_SKILLS_DIR", tmp)

	src := writeTempSkill(t, "force-skill")
	if _, _, err := runSkillExecute(t, []string{"install", src}); err != nil {
		t.Fatalf("first install: %v", err)
	}
	if _, _, err := runSkillExecute(t, []string{"install", src, "--force"}); err != nil {
		t.Fatalf("second install with --force: %v", err)
	}
}

func TestSkillCommand_Install_NoForce_AlreadyInstalled(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SPROUT_SKILLS_DIR", tmp)

	src := writeTempSkill(t, "dup-skill")
	if _, _, err := runSkillExecute(t, []string{"install", src}); err != nil {
		t.Fatalf("first install: %v", err)
	}
	_, _, err := runSkillExecute(t, []string{"install", src})
	if err == nil {
		t.Fatalf("expected error on second install without --force")
	}
	if !errors.Is(err, skills.ErrAlreadyInstalled) {
		t.Errorf("expected ErrAlreadyInstalled, got: %v", err)
	}
}

func TestSkillCommand_Install_FromRegistry(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SPROUT_SKILLS_DIR", tmp)

	// Build a temp source repo with a skills/security-review subdirectory.
	srcRoot := t.TempDir()
	skillSubdir := filepath.Join(srcRoot, "skills", "security-review")
	if err := os.MkdirAll(skillSubdir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := "---\nname: security-review\ndescription: Test skill\n---\n# Security review skill\n"
	if err := os.WriteFile(filepath.Join(skillSubdir, skills.SkillFileName), []byte(content), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}

	fakeReg := &skills.Registry{
		Version: 1,
		Skills: []skills.RegistryEntry{{
			ID:          "security-review",
			Name:        "Security Review",
			Description: "Test registry entry",
			GitURL:      "file://" + srcRoot,
			GitRef:      "main",
			PathInRepo:  "skills/security-review",
		}},
	}
	skills.RegistryOverrideForTest(fakeReg)
	defer skills.RegistryOverrideForTest(nil)

	out, _, err := runSkillExecute(t, []string{"install", "security-review"})
	if err != nil {
		t.Fatalf("registry install: %v", err)
	}
	if !strings.Contains(out, "installed security-review") {
		t.Errorf("expected success message, got: %q", out)
	}
}

func TestSkillCommand_Update_PathOrigin_ErrNotGitOrigin(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SPROUT_SKILLS_DIR", tmp)

	src := writeTempSkill(t, "local-only")
	if _, _, err := runSkillExecute(t, []string{"install", src}); err != nil {
		t.Fatalf("install: %v", err)
	}

	_, _, err := runSkillExecute(t, []string{"update", "local-only"})
	if err == nil {
		t.Fatalf("expected error updating a path-origin skill")
	}
	if !errors.Is(err, skills.ErrNotGitOrigin) {
		t.Errorf("expected ErrNotGitOrigin, got: %v", err)
	}
}

func TestSkillCommand_Remove_CleansDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SPROUT_SKILLS_DIR", tmp)

	src := writeTempSkill(t, "removable")
	if _, _, err := runSkillExecute(t, []string{"install", src}); err != nil {
		t.Fatalf("install: %v", err)
	}

	installDir, _ := skills.SkillInstallDir("removable")
	if _, err := os.Stat(installDir); err != nil {
		t.Fatalf("install dir should exist: %v", err)
	}

	out, _, err := runSkillExecute(t, []string{"remove", "removable"})
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	if !strings.Contains(out, "removed removable") {
		t.Errorf("expected 'removed removable' in output, got: %q", out)
	}
	if _, err := os.Stat(installDir); !os.IsNotExist(err) {
		t.Errorf("expected install dir gone, got err=%v", err)
	}
}

func TestSkillCommand_Remove_NotFound(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SPROUT_SKILLS_DIR", tmp)

	_, _, err := runSkillExecute(t, []string{"remove", "ghost-skill"})
	if err == nil {
		t.Fatalf("expected error removing nonexistent skill")
	}
	if !errors.Is(err, skills.ErrNotInstalled) {
		t.Errorf("expected ErrNotInstalled, got: %v", err)
	}
}

func TestSkillCommand_Usage_EmptyAction(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SPROUT_SKILLS_DIR", tmp)

	_, _, err := runSkillExecute(t, nil)
	if err == nil {
		t.Fatalf("expected error for empty args")
	}
	if !strings.Contains(err.Error(), "missing action") {
		t.Errorf("expected 'missing action' error, got: %v", err)
	}
}

func TestSkillCommand_UnknownAction(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SPROUT_SKILLS_DIR", tmp)

	_, _, err := runSkillExecute(t, []string{"foo"})
	if err == nil {
		t.Fatalf("expected error for unknown action")
	}
	if !strings.Contains(err.Error(), "unknown action") {
		t.Errorf("expected 'unknown action' error, got: %v", err)
	}
}

func TestSkillCommand_Install_Flags_Unknown(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SPROUT_SKILLS_DIR", tmp)

	_, _, err := runSkillExecute(t, []string{"install", "/tmp/x", "--bogus"})
	if err == nil {
		t.Fatalf("expected error for unknown flag")
	}
	if !strings.Contains(err.Error(), "unknown flag") {
		t.Errorf("expected 'unknown flag' error, got: %v", err)
	}
}

func TestSkillCommand_Install_RefRequiresValue(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SPROUT_SKILLS_DIR", tmp)

	_, _, err := runSkillExecute(t, []string{"install", "https://example.com/foo.git", "--ref"})
	if err == nil {
		t.Fatalf("expected error for --ref without value")
	}
	if !strings.Contains(err.Error(), "--ref requires a value") {
		t.Errorf("expected '--ref requires a value' error, got: %v", err)
	}
}

func TestSkillCommand_JSON_ListOutput(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SPROUT_SKILLS_DIR", tmp)

	cmd := &SkillCommand{}
	output := testutil.CaptureStdout(t, func() {
		ctx := &CommandContext{OutputFormat: OutputJSON}
		if err := cmd.ExecuteWithJSONOutput([]string{"list"}, nil, ctx); err != nil {
			t.Errorf("ExecuteWithJSONOutput: %v", err)
		}
	})

	var list []map[string]string
	if err := json.Unmarshal([]byte(output), &list); err != nil {
		t.Fatalf("invalid JSON %q: %v", output, err)
	}
	// Empty list is acceptable.
}

func TestSkillCommand_RegisteredInRegistry(t *testing.T) {
	reg := NewCommandRegistry()
	cmd, ok := reg.GetCommand("skill")
	if !ok || cmd == nil {
		t.Fatalf("expected 'skill' command to be registered")
	}
	if cmd.Name() != "skill" {
		t.Errorf("expected Name()=='skill', got %q", cmd.Name())
	}
}

func TestSkillCommand_JSON_ListOutput_WithSkills(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SPROUT_SKILLS_DIR", tmp)

	src := writeTempSkill(t, "json-test-skill")
	if _, _, err := runSkillExecute(t, []string{"install", src}); err != nil {
		t.Fatalf("install: %v", err)
	}

	cmd := &SkillCommand{}
	output := testutil.CaptureStdout(t, func() {
		ctx := &CommandContext{OutputFormat: OutputJSON}
		if err := cmd.ExecuteWithJSONOutput([]string{"list"}, nil, ctx); err != nil {
			t.Errorf("ExecuteWithJSONOutput: %v", err)
		}
	})

	var list []map[string]string
	if err := json.Unmarshal([]byte(output), &list); err != nil {
		t.Fatalf("invalid JSON %q: %v", output, err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(list))
	}
	if list[0]["id"] != "json-test-skill" {
		t.Errorf("expected id=json-test-skill, got %q", list[0]["id"])
	}
	if list[0]["origin_type"] != "path" {
		t.Errorf("expected origin_type=path, got %q", list[0]["origin_type"])
	}
}

func TestSkillCommand_Update_All_PartialFailure(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SPROUT_SKILLS_DIR", tmp)

	// Install one skill from a local path (will fail to update).
	src := writeTempSkill(t, "path-origin-skill")
	if _, _, err := runSkillExecute(t, []string{"install", src}); err != nil {
		t.Fatalf("install: %v", err)
	}

	_, stderr, err := runSkillExecute(t, []string{"update", "all"})
	if err == nil {
		t.Fatalf("expected error when at least one update fails")
	}
	if !strings.Contains(err.Error(), "failed to update") {
		t.Errorf("expected 'failed to update' error, got: %v", err)
	}
	if !strings.Contains(stderr, "path-origin-skill") {
		t.Errorf("expected per-skill error in stderr, got: %q", stderr)
	}
}

// ── /skill enable / disable ─────────────────────────────────

func newSkillToggleTestAgent(t *testing.T) *agent.Agent {
	t.Helper()
	mgr, cleanup := configuration.NewTestManager(t)
	t.Cleanup(cleanup)
	// Initialize with empty skills map
	if err := mgr.UpdateConfig(func(cfg *configuration.Config) error {
		if cfg.Skills == nil {
			cfg.Skills = make(map[string]configuration.Skill)
		}
		return nil
	}); err != nil {
		t.Fatalf("init config: %v", err)
	}
	chatAgent, err := agent.NewAgentWithModel("")
	if err != nil {
		t.Fatalf("NewAgentWithModel: %v", err)
	}
	return chatAgent
}

func TestSkillCommand_Enable_Disable(t *testing.T) {
	chatAgent := newSkillToggleTestAgent(t)

	// Add a skill to the config
	mgr := chatAgent.GetConfigManager()
	if err := mgr.UpdateConfig(func(cfg *configuration.Config) error {
		cfg.Skills["test-skill"] = configuration.Skill{ID: "test-skill", Name: "Test Skill", Enabled: false}
		return nil
	}); err != nil {
		t.Fatalf("add skill: %v", err)
	}

	cmd := &SkillCommand{}

	// Enable
	if err := cmd.Execute([]string{"enable", "test-skill"}, chatAgent); err != nil {
		t.Fatalf("enable: %v", err)
	}
	cfg := chatAgent.GetConfig()
	if !cfg.Skills["test-skill"].Enabled {
		t.Error("skill should be enabled")
	}

	// Disable
	if err := cmd.Execute([]string{"disable", "test-skill"}, chatAgent); err != nil {
		t.Fatalf("disable: %v", err)
	}
	cfg = chatAgent.GetConfig()
	if cfg.Skills["test-skill"].Enabled {
		t.Error("skill should be disabled")
	}
}

func TestSkillCommand_Enable_MissingSkill(t *testing.T) {
	chatAgent := newSkillToggleTestAgent(t)
	cmd := &SkillCommand{}
	err := cmd.Execute([]string{"enable", "nonexistent"}, chatAgent)
	if err == nil {
		t.Error("expected error for nonexistent skill")
	}
}

func TestSkillCommand_Enable_MissingSkillID(t *testing.T) {
	chatAgent := newSkillToggleTestAgent(t)
	cmd := &SkillCommand{}
	err := cmd.Execute([]string{"enable"}, chatAgent)
	if err == nil {
		t.Error("expected error for missing skill ID")
	}
}

func TestSkillCommand_Disable_MissingSkillID(t *testing.T) {
	chatAgent := newSkillToggleTestAgent(t)
	cmd := &SkillCommand{}
	err := cmd.Execute([]string{"disable"}, chatAgent)
	if err == nil {
		t.Error("expected error for missing skill ID")
	}
}

func TestSkillCommand_Enable_NilAgent(t *testing.T) {
	cmd := &SkillCommand{}
	err := cmd.Execute([]string{"enable", "test-skill"}, nil)
	if err == nil {
		t.Error("expected error for nil agent")
	}
}