package agent

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/personas"
)

const plannerPromptPath = "pkg/agent/prompts/subagent_prompts/planner.md"

// newPlannerAgent returns a test agent with the planner persona active and a
// fresh temp workspace root.
func newPlannerAgent(t *testing.T) (*Agent, string) {
	t.Helper()
	agent := newTestAgent(t)
	t.Cleanup(agent.Shutdown)
	root := t.TempDir()
	agent.SetWorkspaceRoot(root)
	if err := agent.ApplyPersona(personas.IDPlanner); err != nil {
		t.Fatalf("ApplyPersona(planner): %v", err)
	}
	return agent, root
}

func TestPlannerPersona_AllowedToolsAreRegistered(t *testing.T) {
	agent, _ := newPlannerAgent(t)
	planner := agent.configManager.GetConfig().GetSubagentType(personas.IDPlanner)
	if planner == nil {
		t.Fatal("planner persona must resolve from the catalog")
	}
	if !planner.IsConfined() {
		t.Fatal("planner must be confined (write_paths set)")
	}
	if planner.HasCapability(personas.CapabilityGitWrite) {
		t.Error("planner must not carry git_write")
	}
	registry := tools.GetNewToolRegistry()
	hostTools := map[string]bool{}
	for _, name := range planner.HostTools {
		hostTools[name] = true
	}
	allowed := map[string]bool{}
	for _, name := range planner.AllowedTools {
		allowed[name] = true
	}
	for _, name := range planner.HostTools {
		if !allowed[name] {
			t.Errorf("host tool %q must also be in allowed_tools", name)
		}
	}
	if !hostTools["start_change"] {
		t.Error("planner must declare the host's start_change tool")
	}
	for _, name := range planner.AllowedTools {
		if hostTools[name] {
			continue
		}
		if _, ok := registry.Lookup(name); !ok {
			t.Errorf("planner allowed tool %q is not in the tool registry", name)
		}
	}
}

func TestPlannerPersona_PromptLoads(t *testing.T) {
	agent, _ := newPlannerAgent(t)
	prompt := agent.GetSystemPrompt()
	for _, anchor := range []string{"You are the planner", "write_plan", "draw_mockup", "ask_user", "What is offered"} {
		if !strings.Contains(prompt, anchor) {
			t.Errorf("planner system prompt missing %q", anchor)
		}
	}
}

func TestPlannerPersona_OfferedToolsAreTheAllowlist(t *testing.T) {
	agent, _ := newPlannerAgent(t)
	planner := agent.configManager.GetConfig().GetSubagentType(personas.IDPlanner)

	offered := agent.getOptimizedToolDefinitions(nil)
	got := map[string]bool{}
	for _, tool := range offered {
		got[tool.Function.Name] = true
	}
	for name := range got {
		if !containsString(planner.AllowedTools, name) {
			t.Errorf("planner is offered %q, which is not on its allowlist", name)
		}
	}
	for _, name := range planner.AllowedTools {
		// A host tool is offered only once a host registers it; none does here.
		if containsString(planner.HostTools, name) {
			continue
		}
		if !got[name] {
			t.Errorf("planner is not offered its allowed tool %q", name)
		}
	}
}

func TestPlannerPersona_ConfinementRules(t *testing.T) {
	agent, _ := newPlannerAgent(t)

	allowed := []struct {
		name string
		tool string
		args map[string]interface{}
	}{
		{"plan tool", "write_plan", map[string]interface{}{"plan": map[string]interface{}{}}},
		{"mockup tool", "draw_mockup", map[string]interface{}{}},
		{"mockup change", "draw_mockup", map[string]interface{}{"change": "bigger photos", "part": "the photos on the home screen"}},
		{"read project", "read_file", map[string]interface{}{"path": "src/app.go"}},
		{"list project", "list_directory", map[string]interface{}{"path": "."}},
		{"search project", "search", map[string]interface{}{"query": "hours"}},
		{"ask", "ask_user", map[string]interface{}{"question": "Which look?"}},
		{"read a page", "browse_url", map[string]interface{}{"url": "https://example.com", "action": "text"}},
		{"page screenshot inline", "browse_url", map[string]interface{}{"url": "https://example.com", "action": "screenshot"}},
	}
	for _, tc := range allowed {
		if err := agent.checkPersonaConfinement(tc.tool, tc.args); err != nil {
			t.Errorf("%s: %s must be allowed, got %v", tc.name, tc.tool, err)
		}
	}

	refused := []struct {
		name string
		tool string
		args map[string]interface{}
	}{
		{"mockup by hand", "write_file", map[string]interface{}{"path": "design/mockup.html", "content": "x"}},
		{"mockup edit by hand", "edit_file", map[string]interface{}{"path": "design/mockup.html", "old_str": "a", "new_str": "b"}},
		{"render", "design_render", map[string]interface{}{"source": "design/mockup.html"}},
		{"shell", "shell_command", map[string]interface{}{"command": "ls"}},
		{"git", "git", map[string]interface{}{"args": "status"}},
		{"subagent", "run_subagent", map[string]interface{}{"prompt": "build it"}},
		{"skills", "activate_skill", map[string]interface{}{"skill_id": "design-system"}},
		{"mcp", "mcp_tools", map[string]interface{}{"action": "list"}},
		{"structured writer", "write_structured_file", map[string]interface{}{"path": "design/x.json"}},
		{"browse steps", "browse_url", map[string]interface{}{"url": "https://example.com", "steps": []interface{}{map[string]interface{}{"action": "click"}}}},
		{"browse interact", "browse_url", map[string]interface{}{"url": "https://example.com", "action": "interact"}},
		{"browse file url", "browse_url", map[string]interface{}{"url": "file:///etc/hosts", "allow_file_url": true}},
		{"browse session", "browse_url", map[string]interface{}{"url": "https://example.com", "persist_session": true}},
		{"browse screenshot outside design", "browse_url", map[string]interface{}{"url": "https://example.com", "action": "screenshot", "screenshot_path": "shot.png"}},
		{"source file", "write_file", map[string]interface{}{"path": "src/app.go", "content": "x"}},
		{"raw plan file", "write_file", map[string]interface{}{"path": ".sprout/plan.json", "content": "{}"}},
	}
	for _, tc := range refused {
		err := agent.checkPersonaConfinement(tc.tool, tc.args)
		if err == nil {
			t.Errorf("%s: %s must be refused for the planner", tc.name, tc.tool)
			continue
		}
		if !agenterrors.IsPermission(err) {
			t.Errorf("%s: want a permission error, got %T: %v", tc.name, err, err)
		}
	}
}

// TestPersonaConfinement_WritePaths exercises the write-scope rule on a
// confined persona that does hold the file writers (the planner holds none,
// so its writes are refused by the allowlist before the path is looked at).
func TestPersonaConfinement_WritePaths(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "design", "screens"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "design", "escape")); err != nil {
		t.Fatal(err)
	}
	persona := &configuration.SubagentType{
		ID:           "scoped",
		AllowedTools: []string{"write_file", "edit_file", "browse_url"},
		WritePaths:   []string{"design/"},
	}
	check := func(tool string, args map[string]interface{}) error {
		return confinementViolation(persona, root, tool, args)
	}

	for name, args := range map[string]map[string]interface{}{
		"new file":          {"path": "design/screens/home.html"},
		"nested new dir":    {"path": "design/new/deeper/x.html"},
		"absolute inside":   {"path": filepath.Join(root, "design", "mockup.html")},
		"cleaned inside":    {"path": "design/screens/../mockup.html"},
		"screenshot inside": nil,
	} {
		tool := "write_file"
		if args == nil {
			tool, args = "browse_url", map[string]interface{}{"url": "https://example.com", "action": "screenshot", "screenshot_path": "design/ref.png"}
		}
		if err := check(tool, args); err != nil {
			t.Errorf("%s: must be allowed, got %v", name, err)
		}
	}
	for name, path := range map[string]string{
		"source file":      "src/app.go",
		"raw plan file":    ".sprout/plan.json",
		"dot-dot escape":   "design/../src/app.go",
		"design itself":    "design",
		"look-alike dir":   "design-old/x.html",
		"absolute outside": filepath.Join(outside, "x.html"),
		"symlink out":      "design/escape/x.html",
	} {
		for _, tool := range []string{"write_file", "edit_file"} {
			err := check(tool, map[string]interface{}{"path": path})
			if err == nil || !agenterrors.IsPermission(err) {
				t.Errorf("%s via %s: want a permission refusal, got %v", name, tool, err)
			}
		}
	}
}

// TestPlannerPersona_PreExecuteHookEnforces checks the gate sits on the path
// the agent loop actually executes tools through, not only on the helper.
func TestPlannerPersona_PreExecuteHookEnforces(t *testing.T) {
	agent, _ := newPlannerAgent(t)
	hook := newPreExecuteHook(agent)

	if err := hook("write_file", map[string]interface{}{"path": "src/app.go", "content": "x"}); err == nil {
		t.Error("pre-execute hook must refuse a planner write outside design/")
	}
	if err := hook("shell_command", map[string]interface{}{"command": "ls"}); err == nil {
		t.Error("pre-execute hook must refuse a tool outside the planner's allowlist")
	}
	if err := hook("draw_mockup", map[string]interface{}{"change": "darker colours"}); err != nil {
		t.Errorf("pre-execute hook must allow the planner's mockup tool, got %v", err)
	}
}

func TestPersonaConfinement_UnconfinedPersonaUnaffected(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()
	agent.SetWorkspaceRoot(t.TempDir())
	if err := agent.ApplyPersona(personas.IDDesigner); err != nil {
		t.Fatalf("ApplyPersona(designer): %v", err)
	}
	if err := agent.checkPersonaConfinement("write_file", map[string]interface{}{"path": "src/app.go"}); err != nil {
		t.Errorf("an unconfined persona must not be write-scoped, got %v", err)
	}
	if err := agent.checkPersonaConfinement("shell_command", map[string]interface{}{"command": "ls"}); err != nil {
		t.Errorf("an unconfined persona keeps today's offered-list-only filtering, got %v", err)
	}
}

// plannerPromptText reads the planner prompt the way ApplyPersona does when
// the repo copy is not on disk: from the embedded prompt set.
func plannerPromptText(t *testing.T) string {
	t.Helper()
	data, err := readEmbeddedPromptFile(plannerPromptPath)
	if err != nil {
		t.Fatalf("planner prompt is not embedded: %v", err)
	}
	return string(data)
}

// promptWithoutWordsSection drops the "## Words" section, the one place the
// prompt names the developer terms in order to forbid them.
func promptWithoutWordsSection(prompt string) string {
	start := strings.Index(prompt, "\n## Words\n")
	if start < 0 {
		return prompt
	}
	rest := prompt[start+1:]
	end := strings.Index(rest[len("## Words\n"):], "\n## ")
	if end < 0 {
		return prompt[:start]
	}
	return prompt[:start] + rest[len("## Words\n")+end:]
}

// TestPlannerPrompt_BuilderVocabulary mirrors the builder-vocabulary lint
// used for builder-facing copy: the developer terms appear only in the
// prompt's own avoid-list, never in its instructions or examples, and the
// builder terms are all present.
func TestPlannerPrompt_BuilderVocabulary(t *testing.T) {
	prompt := plannerPromptText(t)
	lower := strings.ToLower(prompt)

	for _, term := range []string{"project", "changes", "version", "go back", "put it online", "screens"} {
		if !strings.Contains(lower, term) {
			t.Errorf("planner prompt does not carry the builder term %q", term)
		}
	}

	words := strings.Index(prompt, "\n## Words\n")
	if words < 0 {
		t.Fatal("planner prompt must carry a \"## Words\" section naming the terms to avoid")
	}

	developerTerms := []string{
		"repository", "repo", "commits", "commit", "branch", "revert", "rollback",
		"roll back", "deploy", "deployment", "ship", "routes", "components",
		"provider", "api key", "terminal", "git",
	}
	wordsSection := strings.ToLower(prompt[words:])
	body := strings.ToLower(promptWithoutWordsSection(prompt))
	for _, term := range developerTerms {
		re := regexp.MustCompile(`\b` + regexp.QuoteMeta(term) + `\b`)
		if !re.MatchString(wordsSection) {
			t.Errorf("the Words section must name %q as a term to avoid", term)
		}
		if loc := re.FindStringIndex(body); loc != nil {
			from := max(0, loc[0]-40)
			to := min(len(body), loc[1]+40)
			t.Errorf("developer term %q appears outside the Words section: …%s…", term, body[from:to])
		}
	}
}

// TestPlannerPrompt_NoSessionTags guards the repo rule that prompts carry no
// item tags, spec numbers, or dates.
func TestPlannerPrompt_NoSessionTags(t *testing.T) {
	prompt := plannerPromptText(t)
	if strings.Contains(prompt, "SP-") {
		t.Error("planner prompt carries a spec/item tag (SP-…)")
	}
	if strings.Contains(prompt, "§") {
		t.Error("planner prompt carries a section sign")
	}
	if regexp.MustCompile(`\b\d+\.\d+\b`).MatchString(prompt) {
		t.Error("planner prompt carries a spec-number pattern (digits.digits)")
	}
	if regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}\b`).MatchString(prompt) {
		t.Error("planner prompt carries a date")
	}
}

// Under a low-context model (< 132K window, e.g. a host's managed model) the
// generic low-context tool list must not replace the planner's own: a
// confined persona's allowed_tools are its whole tool set at any size, while
// any other persona stays narrowed by the low-context list.
func TestPlannerPersona_KeepsItsToolsUnderALowContextModel(t *testing.T) {
	agent, _ := newPlannerAgent(t)
	profile, err := configuration.ResolveContextProfile(agent.configManager.GetConfig(), 128_000)
	if err != nil {
		t.Fatal(err)
	}
	if len(profile.ToolAllowlist) == 0 {
		t.Fatal("a 128K model must resolve to the low-context profile for this test to mean anything")
	}
	agent.contextProfile = profile

	planner := agent.configManager.GetConfig().GetSubagentType(personas.IDPlanner)
	offered := map[string]bool{}
	for _, tool := range agent.getOptimizedToolDefinitions(nil) {
		offered[tool.Function.Name] = true
	}
	for _, name := range []string{"write_plan", "draw_mockup", "ask_user"} {
		if !offered[name] {
			t.Errorf("planner under a low-context model is not offered %q", name)
		}
	}
	for name := range offered {
		if !containsString(planner.AllowedTools, name) {
			t.Errorf("planner under a low-context model is offered %q, which is not on its list", name)
		}
	}
	seed := seedToolNames(NewSeedToolRegistry(agent))
	if !seed["write_plan"] || !seed["draw_mockup"] || seed["shell_command"] {
		t.Errorf("seed registry under a low-context model = %v; want the planner's tools only", seed)
	}

	// A persona that is not confined is still narrowed by the low-context list.
	if err := agent.ApplyPersona(personas.IDDesigner); err != nil {
		t.Fatal(err)
	}
	agent.contextProfile = profile
	for _, tool := range agent.getOptimizedToolDefinitions(nil) {
		if !containsString(profile.ToolAllowlist, tool.Function.Name) {
			t.Errorf("designer under a low-context model is offered %q, outside the low-context list", tool.Function.Name)
		}
	}
}
