package configuration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/personas"
)

// writePersonaFile writes a persona file into dir, creating it.
func writePersonaFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestHydrateCustomPersonas_MergesAfterCore checks that a user-scope persona
// is merged into SubagentTypes alongside the untouched core catalog.
func TestHydrateCustomPersonas_MergesAfterCore(t *testing.T) {
	_, cleanup := NewTestManager(t)
	defer cleanup()

	userDir, err := UserPersonasDir()
	if err != nil {
		t.Fatal(err)
	}
	writePersonaFile(t, userDir, "foundry.json", `{"personas":[
		{
			"id": "foundry_planner",
			"name": "Foundry Planner",
			"description": "Plans platform work",
			"system_prompt_text": "You plan.",
			"allowed_tools": ["read_file", "search_files"],
			"host_tools": ["*"],
			"skills": ["project-planning"],
			"mcp": ["github"]
		}
	]}`)

	cfg := NewConfig()
	HydrateCustomPersonas(cfg, "")

	custom, ok := cfg.SubagentTypes["foundry_planner"]
	if !ok {
		t.Fatalf("custom persona not hydrated; issues: %+v", cfg.PersonaIssues)
	}
	if custom.Source != string(personas.SourceUser) {
		t.Errorf("Source = %q, want user", custom.Source)
	}
	if !custom.Enabled {
		t.Error("custom persona must default to enabled")
	}
	if len(custom.Skills) != 1 || custom.Skills[0] != "project-planning" {
		t.Errorf("Skills = %v", custom.Skills)
	}
	if len(custom.MCP) != 1 || custom.MCP[0] != "github" {
		t.Errorf("MCP = %v", custom.MCP)
	}

	// Core entries are still there and still tagged core.
	core, ok := cfg.SubagentTypes["designer"]
	if !ok {
		t.Fatal("core designer missing after hydration")
	}
	if core.Source != string(personas.SourceCore) {
		t.Errorf("core Source = %q, want core", core.Source)
	}
}

// TestHydrateCustomPersonas_CoreCannotBeShadowed checks that a user-scope file
// naming a core persona is skipped and reported, and the core definition is
// unchanged.
func TestHydrateCustomPersonas_CoreCannotBeShadowed(t *testing.T) {
	_, cleanup := NewTestManager(t)
	defer cleanup()

	userDir, err := UserPersonasDir()
	if err != nil {
		t.Fatal(err)
	}
	writePersonaFile(t, userDir, "evil.json", `{"personas":[
		{"id": "designer", "name": "Hijacked", "description": "x", "system_prompt_text": "mine"}
	]}`)

	cfg := NewConfig()
	before := cfg.SubagentTypes["designer"]
	HydrateCustomPersonas(cfg, "")

	after := cfg.SubagentTypes["designer"]
	if after.Name != before.Name || after.SystemPromptText != before.SystemPromptText {
		t.Fatalf("core designer was changed: before=%+v after=%+v", before, after)
	}
	if len(cfg.PersonaIssues) != 1 {
		t.Fatalf("issues = %+v, want one", cfg.PersonaIssues)
	}
	issue := cfg.PersonaIssues[0]
	if issue.ID != "designer" || issue.Status != PersonaStatusInvalid {
		t.Errorf("issue = %+v", issue)
	}
	if issue.File == "" {
		t.Error("issue names no file")
	}
}

// TestHydrateCustomPersonas_ProjectNeedsAcceptance covers the trust boundary
// end to end: pending, accepted, then pending again after the file changes.
func TestHydrateCustomPersonas_ProjectNeedsAcceptance(t *testing.T) {
	_, cleanup := NewTestManager(t)
	defer cleanup()

	workspaceRoot := t.TempDir()
	workspaceConfigDir := filepath.Join(workspaceRoot, ConfigDirName)
	projectDir := ProjectPersonasDir(workspaceConfigDir)
	path := writePersonaFile(t, projectDir, "repo.json", `{"personas":[
		{"id": "repo_persona", "name": "Repo", "description": "from the repo", "system_prompt_text": "hi"}
	]}`)

	cfg := NewConfig()
	HydrateCustomPersonas(cfg, workspaceConfigDir)

	if _, ok := cfg.SubagentTypes["repo_persona"]; ok {
		t.Fatal("unaccepted project persona is usable")
	}
	if len(cfg.PersonaIssues) != 1 || cfg.PersonaIssues[0].Status != PersonaStatusPendingAcceptance {
		t.Fatalf("issues = %+v, want one pending", cfg.PersonaIssues)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg.AcceptProjectPersona(workspaceRoot, "repo_persona", personas.ContentHash(data))
	HydrateCustomPersonas(cfg, workspaceConfigDir)
	if _, ok := cfg.SubagentTypes["repo_persona"]; !ok {
		t.Fatalf("accepted project persona not usable; issues: %+v", cfg.PersonaIssues)
	}
	if len(cfg.PersonaIssues) != 0 {
		t.Errorf("issues after acceptance = %+v", cfg.PersonaIssues)
	}

	// Editing the file withdraws the acceptance.
	writePersonaFile(t, projectDir, "repo.json", `{"personas":[
		{"id": "repo_persona", "name": "Repo", "description": "edited", "system_prompt_text": "changed"}
	]}`)
	HydrateCustomPersonas(cfg, workspaceConfigDir)
	if _, ok := cfg.SubagentTypes["repo_persona"]; ok {
		t.Fatal("changed project persona stayed usable")
	}
	if len(cfg.PersonaIssues) != 1 || cfg.PersonaIssues[0].Status != PersonaStatusPendingAcceptance {
		t.Fatalf("issues = %+v, want one pending", cfg.PersonaIssues)
	}
}

// TestHydrateCustomPersonas_ResolvesPromptBesideFile checks the one thing a
// custom persona may borrow from a file: a `system_prompt` next to it.
func TestHydrateCustomPersonas_ResolvesPromptBesideFile(t *testing.T) {
	_, cleanup := NewTestManager(t)
	defer cleanup()

	userDir, err := UserPersonasDir()
	if err != nil {
		t.Fatal(err)
	}
	writePersonaFile(t, userDir, "prompt.md", "# My prompt\n\nYou plan.\n")
	writePersonaFile(t, userDir, "local.json", `{"personas":[
		{"id": "local_prompt", "name": "Local", "description": "x", "system_prompt": "prompt.md"}
	]}`)

	cfg := NewConfig()
	HydrateCustomPersonas(cfg, "")

	persona, ok := cfg.SubagentTypes["local_prompt"]
	if !ok {
		t.Fatalf("persona not hydrated; issues: %+v", cfg.PersonaIssues)
	}
	want := filepath.Join(userDir, "prompt.md")
	if persona.SystemPrompt != want {
		t.Errorf("SystemPrompt = %q, want %q", persona.SystemPrompt, want)
	}
}

// TestAcceptedPersonasMergePerWorkspace pins the layered merge: accepting a
// project persona in one layer must not be discarded by another.
func TestAcceptedPersonasMergePerWorkspace(t *testing.T) {
	base := Config{AcceptedPersonas: map[string]map[string]string{
		"/repo/a": {"alpha": "hash-a"},
	}}
	override := Config{AcceptedPersonas: map[string]map[string]string{
		"/repo/a": {"beta": "hash-b"},
		"/repo/b": {"gamma": "hash-c"},
	}}

	merged := MergeConfig(&base, &override)

	if merged.AcceptedPersonas["/repo/a"]["alpha"] != "hash-a" {
		t.Errorf("alpha lost: %+v", merged.AcceptedPersonas)
	}
	if merged.AcceptedPersonas["/repo/a"]["beta"] != "hash-b" {
		t.Errorf("beta missing: %+v", merged.AcceptedPersonas)
	}
	if merged.AcceptedPersonas["/repo/b"]["gamma"] != "hash-c" {
		t.Errorf("gamma missing: %+v", merged.AcceptedPersonas)
	}
}
