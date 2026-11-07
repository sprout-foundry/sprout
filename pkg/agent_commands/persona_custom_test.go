package commands

// persona_custom_test.go — the custom-persona CLI surface (SP-175).

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/personas"
)

// TestPersonaNew_WritesUserScopeFile pins `/persona new <id>`: a file the
// creator owns, in the user scope, that a reload picks up.
func TestPersonaNew_WritesUserScopeFile(t *testing.T) {
	chatAgent := createTestAgentWithTempConfig(t)

	cmd := &PersonaCommand{}
	var buf bytes.Buffer
	cmd.SetOutput(&buf)

	if err := cmd.Execute([]string{"new", "foundry-planner"}, chatAgent); err != nil {
		t.Fatalf("persona new: %v", err)
	}

	dir, err := configuration.UserPersonasDir()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "foundry_planner.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read created file: %v", err)
	}

	var catalog personas.Catalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		t.Fatalf("created file is not a persona catalog: %v\n%s", err, data)
	}
	if len(catalog.Personas) != 1 || catalog.Personas[0].ID != "foundry_planner" {
		t.Fatalf("catalog = %+v", catalog)
	}
	if !catalog.Personas[0].Enabled {
		t.Error("a created persona must start enabled")
	}
	if !strings.Contains(buf.String(), path) {
		t.Errorf("output does not name the file: %q", buf.String())
	}

	// A second create refuses to overwrite the creator's file.
	if err := cmd.Execute([]string{"new", "foundry-planner"}, chatAgent); err == nil {
		t.Fatal("creating the same persona twice overwrote it")
	}
}

// TestPersonaNew_FromCoreCopiesOnce checks `--from`: the core definition is
// copied, the id is the creator's, aliases are dropped (they belong to core),
// and a core prompt path carries over as text.
func TestPersonaNew_FromCoreCopiesOnce(t *testing.T) {
	chatAgent := createTestAgentWithTempConfig(t)

	cmd := &PersonaCommand{}
	cmd.SetOutput(&bytes.Buffer{})
	if err := cmd.Execute([]string{"new", "my_planner", "--from", "planner"}, chatAgent); err != nil {
		t.Fatalf("persona new --from: %v", err)
	}

	dir, err := configuration.UserPersonasDir()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "my_planner.json"))
	if err != nil {
		t.Fatal(err)
	}
	var catalog personas.Catalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		t.Fatal(err)
	}
	got := catalog.Personas[0]
	if got.ID != "my_planner" {
		t.Errorf("ID = %q", got.ID)
	}
	if len(got.Aliases) != 0 {
		t.Errorf("aliases copied from core (%v); they would collide", got.Aliases)
	}
	if got.SystemPrompt == "" && got.SystemPromptText == "" {
		t.Error("no prompt carried over from the core persona")
	}

	// The copy is a valid custom persona: it hydrates and applies.
	if err := chatAgent.GetConfigManager().UpdateConfigNoSave(func(cfg *configuration.Config) error {
		configuration.HydrateCustomPersonas(cfg, "")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, ok := chatAgent.GetConfigManager().GetConfig().SubagentTypes["my_planner"]; !ok {
		t.Fatalf("copied persona did not hydrate; issues: %+v",
			chatAgent.GetConfigManager().GetConfig().PersonaIssues)
	}
	if err := chatAgent.ApplyPersona("my_planner"); err != nil {
		t.Fatalf("apply copied persona: %v", err)
	}
}

// TestPersonaNew_RefusesCoreID pins decision 1: core personas are immutable,
// so a user file cannot claim one.
func TestPersonaNew_RefusesCoreID(t *testing.T) {
	chatAgent := createTestAgentWithTempConfig(t)
	cmd := &PersonaCommand{}
	cmd.SetOutput(&bytes.Buffer{})

	err := cmd.Execute([]string{"new", "designer"}, chatAgent)
	if err == nil || !strings.Contains(err.Error(), "core persona id") {
		t.Fatalf("err = %v, want a core-id refusal", err)
	}
}

// TestPersonaAccept_RecordsHash pins `/persona accept <id>`: the project
// persona becomes usable, and the record is keyed by workspace root and
// content hash.
func TestPersonaAccept_RecordsHash(t *testing.T) {
	chatAgent := createTestAgentWithTempConfig(t)
	workspaceRoot := chatAgent.GetWorkspaceRoot()
	if workspaceRoot == "" {
		t.Fatal("test agent has no workspace root")
	}
	workspaceConfigDir := filepath.Join(workspaceRoot, configuration.ConfigDirName)
	projectDir := configuration.ProjectPersonasDir(workspaceConfigDir)
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"personas":[{"id":"repo_persona","name":"Repo","description":"from the repo","system_prompt_text":"hi"}]}`
	path := filepath.Join(projectDir, "repo.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	// The pending state is what the agent sees.
	if err := chatAgent.GetConfigManager().UpdateConfigNoSave(func(cfg *configuration.Config) error {
		configuration.HydrateCustomPersonas(cfg, workspaceConfigDir)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	cfg := chatAgent.GetConfigManager().GetConfig()
	if _, ok := cfg.SubagentTypes["repo_persona"]; ok {
		t.Fatal("project persona usable before acceptance")
	}

	cmd := &PersonaCommand{}
	var buf bytes.Buffer
	cmd.SetOutput(&buf)
	if err := cmd.Execute([]string{"accept", "repo_persona"}, chatAgent); err != nil {
		t.Fatalf("persona accept: %v", err)
	}

	accepted := chatAgent.GetConfigManager().GetConfig().ProjectPersonaAcceptances(workspaceRoot)
	if accepted["repo_persona"] != personas.ContentHash([]byte(body)) {
		t.Fatalf("acceptance = %v, want the file's hash", accepted)
	}

	// Accepting something that is not pending is an error, not a silent no-op.
	if err := cmd.Execute([]string{"accept", "nope"}, chatAgent); err == nil {
		t.Fatal("accepting an unknown persona succeeded")
	}
}

// TestEffectiveToolSet_AddsSelectorsForCustomPersonas pins what `/persona show`
// reports: the tools a persona actually gets, including the ones its
// selectors add.
func TestEffectiveToolSet_AddsSelectorsForCustomPersonas(t *testing.T) {
	core := configuration.SubagentType{
		ID: "designer", AllowedTools: []string{"read_file"}, HostTools: []string{"draw"},
		Skills: []string{"design-system"}, Source: "core",
	}
	if got := effectiveToolSet(core); len(got) != 1 || got[0] != "read_file" {
		t.Errorf("core tool set = %v, want just read_file", got)
	}

	custom := core
	custom.Source = "user"
	got := effectiveToolSet(custom)
	want := []string{"activate_skill", "draw", "list_skills", "read_file"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("custom tool set = %v, want %v", got, want)
	}
}
