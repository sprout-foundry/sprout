package personas

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

func coreCatalog(t *testing.T) map[string]Definition {
	t.Helper()
	core, err := DefaultDefinitions()
	if err != nil {
		t.Fatalf("load core catalog: %v", err)
	}
	return core
}

// TestCustomPersona_CannotShadowCoreIDOrAlias is the immutability test: a
// custom file that reuses a core persona's id or alias is an error naming the
// file, and the core definition is untouched.
func TestCustomPersona_CannotShadowCoreIDOrAlias(t *testing.T) {
	core := coreCatalog(t)
	dir := t.TempDir()

	// A core id, a core alias, and a custom alias that names a core id.
	writePersonaFile(t, dir, "shadow_id.json", `{"personas":[
		{"id": "designer", "name": "Fake Designer", "description": "x", "system_prompt_text": "mine"}
	]}`)
	writePersonaFile(t, dir, "shadow_alias.json", `{"personas":[
		{"id": "not_a_core_id", "name": "Alias Thief", "description": "x", "aliases": ["planner"]}
	]}`)
	writePersonaFile(t, dir, "shadow_via_alias.json", `{"personas":[
		{"id": "another_custom", "name": "Another", "description": "x", "aliases": ["designer"]}
	]}`)

	result := LoadCustomPersonas(core, LoadOptions{UserDir: dir})

	if len(result.Personas) != 0 {
		t.Fatalf("loaded %d personas, want 0: %+v", len(result.Personas), result.Personas)
	}
	if len(result.Problems) != 3 {
		t.Fatalf("got %d problems, want 3: %+v", len(result.Problems), result.Problems)
	}
	for _, problem := range result.Problems {
		if problem.Kind != ProblemError {
			t.Errorf("%s: kind = %q, want error", problem.File, problem.Kind)
		}
		if problem.File == "" {
			t.Errorf("problem %q names no file", problem.Message)
		}
	}

	// The core definition is unchanged.
	designer, ok := core["designer"]
	if !ok {
		t.Fatal("core designer disappeared")
	}
	if designer.Name == "Fake Designer" {
		t.Fatal("core persona was mutated by a custom definition")
	}
}

// TestCustomPersona_UserScopeLoadsWithDeclaredFields checks that a user-scope
// persona loads and that every field a creator sets survives.
func TestCustomPersona_UserScopeLoadsWithDeclaredFields(t *testing.T) {
	core := coreCatalog(t)
	dir := t.TempDir()
	path := writePersonaFile(t, dir, "foundry.json", `{
		"id": "foundry-planner",
		"name": "Foundry Planner",
		"description": "Plans platform work",
		"system_prompt_text": "You plan.",
		"allowed_tools": ["read_file", "search_files"],
		"host_tools": ["*"],
		"skills": ["project-planning"],
		"mcp": ["github"],
		"write_paths": ["docs/"],
		"capabilities": ["git_write"],
		"delegatable": true
	}`)

	result := LoadCustomPersonas(core, LoadOptions{UserDir: dir})
	if len(result.Problems) != 0 {
		t.Fatalf("problems: %+v", result.Problems)
	}
	if len(result.Personas) != 1 {
		t.Fatalf("loaded %d personas, want 1", len(result.Personas))
	}

	got := result.Personas[0]
	if got.ID != "foundry_planner" {
		t.Errorf("ID = %q, want foundry_planner (normalized)", got.ID)
	}
	if got.Source != SourceUser {
		t.Errorf("Source = %q, want user", got.Source)
	}
	if got.File != path {
		t.Errorf("File = %q, want %q", got.File, path)
	}
	if got.Hash == "" {
		t.Error("Hash is empty")
	}
	if !got.Enabled {
		t.Error("a custom persona defaults to enabled")
	}
	if got.SystemPromptText != "You plan." {
		t.Errorf("SystemPromptText = %q", got.SystemPromptText)
	}
	if strings.Join(got.HostTools, ",") != "*" {
		t.Errorf("HostTools = %v", got.HostTools)
	}
	if strings.Join(got.Skills, ",") != "project-planning" {
		t.Errorf("Skills = %v", got.Skills)
	}
	if strings.Join(got.MCP, ",") != "github" {
		t.Errorf("MCP = %v", got.MCP)
	}
	if !got.Delegatable {
		t.Error("Delegatable = false")
	}
}

// TestCustomPersona_OneMalformedFileIsSkippedAndNamed pins the resilience
// rule: a bad file never stops the others.
func TestCustomPersona_OneMalformedFileIsSkippedAndNamed(t *testing.T) {
	core := coreCatalog(t)
	dir := t.TempDir()
	bad := writePersonaFile(t, dir, "broken.json", `{"personas": [{"id": "oops",`)
	writePersonaFile(t, dir, "good.json", `{"personas":[
		{"id": "good_one", "name": "Good", "description": "fine", "system_prompt_text": "hi"}
	]}`)
	writePersonaFile(t, dir, "missing_fields.json", `{"personas":[
		{"id": "half_made", "name": "Half"}
	]}`)

	result := LoadCustomPersonas(core, LoadOptions{UserDir: dir})

	if len(result.Personas) != 1 || result.Personas[0].ID != "good_one" {
		t.Fatalf("personas = %+v, want just good_one", result.Personas)
	}
	var named []string
	for _, problem := range result.Problems {
		named = append(named, problem.File)
	}
	if !containsString(named, bad) {
		t.Errorf("malformed file not named: %v", named)
	}
	if len(result.Problems) != 2 {
		t.Errorf("got %d problems, want 2 (malformed + missing fields): %+v", len(result.Problems), result.Problems)
	}
}

// TestCustomPersona_ProjectIsPendingUntilAccepted checks the trust boundary:
// a project persona is listed but unusable until its content hash is
// accepted, and is unusable again when the file changes.
func TestCustomPersona_ProjectIsPendingUntilAccepted(t *testing.T) {
	core := coreCatalog(t)
	dir := t.TempDir()
	path := writePersonaFile(t, dir, "repo.json", `{"personas":[
		{"id": "repo_persona", "name": "Repo", "description": "from the repo", "system_prompt_text": "hi"}
	]}`)

	// Unaccepted: listed as pending, not usable.
	result := LoadCustomPersonas(core, LoadOptions{ProjectDir: dir})
	if len(result.Personas) != 0 {
		t.Fatalf("unaccepted project persona loaded: %+v", result.Personas)
	}
	if len(result.PendingProject) != 1 || result.PendingProject[0].ID != "repo_persona" {
		t.Fatalf("PendingProject = %+v", result.PendingProject)
	}

	// Accepted at the file's current hash: usable.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	accepted := map[string]string{"repo_persona": ContentHash(data)}
	result = LoadCustomPersonas(core, LoadOptions{
		ProjectDir:            dir,
		AcceptedProjectHashes: accepted,
	})
	if len(result.Personas) != 1 || result.Personas[0].ID != "repo_persona" {
		t.Fatalf("accepted persona not loaded: %+v / %+v", result.Personas, result.Problems)
	}

	// Changed file: pending again.
	writePersonaFile(t, dir, "repo.json", `{"personas":[
		{"id": "repo_persona", "name": "Repo", "description": "changed", "system_prompt_text": "new"}
	]}`)
	result = LoadCustomPersonas(core, LoadOptions{
		ProjectDir:            dir,
		AcceptedProjectHashes: accepted,
	})
	if len(result.Personas) != 0 {
		t.Fatalf("changed project persona stayed usable: %+v", result.Personas)
	}
	if len(result.PendingProject) != 1 {
		t.Fatalf("PendingProject = %+v", result.PendingProject)
	}
}

// TestCustomPersona_UserShadowsProject checks precedence: a user persona wins
// over a project one with the same id, and the shadowed file is reported.
func TestCustomPersona_UserShadowsProject(t *testing.T) {
	core := coreCatalog(t)
	userDir := t.TempDir()
	projectDir := t.TempDir()
	writePersonaFile(t, userDir, "mine.json", `{"personas":[
		{"id": "shared_id", "name": "Mine", "description": "user copy", "system_prompt_text": "user"}
	]}`)
	projectPath := writePersonaFile(t, projectDir, "theirs.json", `{"personas":[
		{"id": "shared_id", "name": "Theirs", "description": "project copy", "system_prompt_text": "project"}
	]}`)

	data, err := os.ReadFile(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	result := LoadCustomPersonas(core, LoadOptions{
		UserDir:               userDir,
		ProjectDir:            projectDir,
		AcceptedProjectHashes: map[string]string{"shared_id": ContentHash(data)},
	})

	if len(result.Personas) != 1 {
		t.Fatalf("personas = %+v, want just the user copy", result.Personas)
	}
	if result.Personas[0].Source != SourceUser || result.Personas[0].SystemPromptText != "user" {
		t.Errorf("wrong persona won: %+v", result.Personas[0])
	}
	found := false
	for _, problem := range result.Problems {
		if problem.Kind == ProblemWarning && strings.Contains(problem.Message, "shadowed") {
			found = true
		}
	}
	if !found {
		t.Errorf("shadowed project persona not reported: %+v", result.Problems)
	}
}

// TestValidateDefinition_RejectsCoreCollision covers the host scope: a
// definition minted in memory is held to the same immutability rule.
func TestValidateDefinition_RejectsCoreCollision(t *testing.T) {
	core := coreCatalog(t)

	errs, _ := ValidateDefinition(core, Definition{ID: "planner", Name: "X", Description: "y"})
	if len(errs) == 0 {
		t.Fatal("a definition reusing a core id was accepted")
	}
	if !strings.Contains(strings.Join(errs, " "), "immutable") {
		t.Errorf("errors do not explain immutability: %v", errs)
	}

	errs, _ = ValidateDefinition(core, Definition{ID: "free_id", Name: "", Description: ""})
	if len(errs) == 0 {
		t.Fatal("a definition missing required fields was accepted")
	}
	if !strings.Contains(strings.Join(errs, " "), "name") {
		t.Errorf("errors do not name the missing fields: %v", errs)
	}

	errs, _ = ValidateDefinition(core, Definition{ID: "free_id", Name: "Free", Description: "ok"})
	if len(errs) != 0 {
		t.Errorf("a valid definition was rejected: %v", errs)
	}
}

// TestValidateDefinition_RejectsUnsafeID pins that an id that could escape
// the personas directory (path separators, dot segments) is refused.
func TestValidateDefinition_RejectsUnsafeID(t *testing.T) {
	core := coreCatalog(t)
	for _, id := range []string{"../escape", "a/b", "a\\b", "..", "a..b"} {
		errs, _ := ValidateDefinition(core, Definition{ID: id, Name: "X", Description: "y"})
		if len(errs) == 0 {
			t.Errorf("unsafe id %q was accepted", id)
		}
	}
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

// TestCustomPersona_DuplicatePendingProjectIDIsReported pins that two
// unaccepted project files declaring the same id are a duplicate, not two
// things to accept.
func TestCustomPersona_DuplicatePendingProjectIDIsReported(t *testing.T) {
	core := coreCatalog(t)
	dir := t.TempDir()
	writePersonaFile(t, dir, "a.json", `{"personas":[
		{"id": "same_id", "name": "A", "description": "x", "system_prompt_text": "a"}
	]}`)
	writePersonaFile(t, dir, "b.json", `{"personas":[
		{"id": "same_id", "name": "B", "description": "x", "system_prompt_text": "b"}
	]}`)

	result := LoadCustomPersonas(core, LoadOptions{ProjectDir: dir})

	if len(result.PendingProject) != 1 {
		t.Fatalf("PendingProject = %+v, want one", result.PendingProject)
	}
	found := false
	for _, problem := range result.Problems {
		if problem.Kind == ProblemError && strings.Contains(problem.Message, "duplicate") {
			found = true
		}
	}
	if !found {
		t.Errorf("duplicate id among pending project personas not reported: %+v", result.Problems)
	}
}

// TestCustomPersona_AliasCannotShadowAnotherCustomPersona pins that a custom
// persona's alias may not claim another custom persona's id or alias.
func TestCustomPersona_AliasCannotShadowAnotherCustomPersona(t *testing.T) {
	core := coreCatalog(t)
	dir := t.TempDir()
	writePersonaFile(t, dir, "a.json", `{"personas":[
		{"id": "first_custom", "name": "First", "description": "x", "aliases": ["shared_alias"]}
	]}`)
	writePersonaFile(t, dir, "b.json", `{"personas":[
		{"id": "second_custom", "name": "Second", "description": "x", "aliases": ["first_custom"]}
	]}`)
	writePersonaFile(t, dir, "c.json", `{"personas":[
		{"id": "third_custom", "name": "Third", "description": "x", "aliases": ["shared_alias"]}
	]}`)

	result := LoadCustomPersonas(core, LoadOptions{UserDir: dir})

	ids := make([]string, 0, len(result.Personas))
	for _, persona := range result.Personas {
		ids = append(ids, persona.ID)
	}
	if len(ids) != 1 || ids[0] != "first_custom" {
		t.Fatalf("personas = %v, want just first_custom", ids)
	}
	if len(result.Problems) != 2 {
		t.Fatalf("problems = %+v, want two alias collisions", result.Problems)
	}
}

// TestCustomPersona_SelectorsWithoutToolSetWarn pins the warning for a
// persona that names selectors but leaves allowed_tools empty.
func TestCustomPersona_SelectorsWithoutToolSetWarn(t *testing.T) {
	core := coreCatalog(t)
	dir := t.TempDir()
	writePersonaFile(t, dir, "s.json", `{"personas":[
		{"id": "selectors_only", "name": "S", "description": "x", "host_tools": ["*"]}
	]}`)

	result := LoadCustomPersonas(core, LoadOptions{UserDir: dir})
	if len(result.Personas) != 1 {
		t.Fatalf("persona did not load: %+v", result.Problems)
	}
	var warnings int
	for _, problem := range result.Problems {
		if problem.Kind == ProblemWarning && strings.Contains(problem.Message, "allowed_tools is empty") {
			warnings++
		}
	}
	if warnings != 1 {
		t.Errorf("warnings = %+v, want one about allowed_tools", result.Problems)
	}
}
