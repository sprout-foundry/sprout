package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/events"
)

func newReviewTestRunner(t *testing.T) (*Agent, *SubagentRunner) {
	t.Helper()
	parent := newIsolatedTestAgent(t)
	t.Cleanup(parent.Shutdown)
	shared := &SharedState{
		EventBus:      events.NewEventBus(),
		TodoManager:   tools.NewTodoManager(),
		ConfigManager: parent.configManager,
		WorkspaceRoot: parent.workspaceRoot,
	}
	return parent, NewSubagentRunner(parent, shared)
}

func toolNames(ts []api.Tool) map[string]bool {
	names := make(map[string]bool, len(ts))
	for _, tool := range ts {
		names[tool.Function.Name] = true
	}
	return names
}

func TestCreateSubagent_AppliesPersonaToolAllowlist(t *testing.T) {
	_, runner := newReviewTestRunner(t)

	sub, err := runner.createSubagent(SubagentOptions{Persona: "code_reviewer"}, context.Background())
	if err != nil {
		t.Fatalf("createSubagent: %v", err)
	}
	defer sub.Shutdown()

	if got := sub.GetActivePersona(); got != "reviewer" {
		t.Fatalf("active persona = %q, want alias resolved to %q", got, "reviewer")
	}
	if sub.isGitWriteAllowed() {
		t.Error("reviewer subagent has git_write capability; it must not inherit the orchestrator's")
	}

	// The test client resolves to low-context mode, whose fixed allowlist
	// supersedes persona filtering; exercise the full-mode path.
	sub.contextProfile = configuration.ContextProfile{Mode: configuration.ContextModeFull}

	advertised := toolNames(sub.getOptimizedToolDefinitions(nil))
	for _, want := range []string{"read_file", "search", "shell_command"} {
		if !advertised[want] {
			t.Errorf("reviewer subagent missing %q in advertised tools", want)
		}
	}
	for _, unwanted := range []string{"write_file", "edit_file", "web_search", "browse_url", "revert_my_changes"} {
		if advertised[unwanted] {
			t.Errorf("reviewer subagent advertises %q; persona allowlist not applied", unwanted)
		}
	}
}

func TestCreateSubagent_NoPersonaKeepsWriteTools(t *testing.T) {
	_, runner := newReviewTestRunner(t)

	sub, err := runner.createSubagent(SubagentOptions{}, context.Background())
	if err != nil {
		t.Fatalf("createSubagent: %v", err)
	}
	defer sub.Shutdown()

	sub.contextProfile = configuration.ContextProfile{Mode: configuration.ContextModeFull}
	if !toolNames(sub.getOptimizedToolDefinitions(nil))["write_file"] {
		t.Error("persona-less subagent lost write_file")
	}
}

func TestResolveSubagentProviderModel_FallsBackToEmbeddedPersonaPrompt(t *testing.T) {
	parent, _ := newReviewTestRunner(t)

	// A workspace that is not the sprout source tree: the repo-relative
	// persona prompt path does not exist on disk there.
	_, _, prompt, err := resolveSubagentProviderModel(parent, "reviewer", true, t.TempDir())
	if err != nil {
		t.Fatalf("resolveSubagentProviderModel: %v", err)
	}
	if !strings.Contains(prompt, "Reviewer Subagent") {
		t.Fatalf("expected embedded reviewer prompt, got %q", truncateString(prompt, 120))
	}
}

func initReviewRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...) //nolint:gosec // G204: git on the test's own temp dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q")
	writeFile(t, dir, "main.go", "package main\n\nfunc main() {}\n")
	git("add", "main.go")
	git("commit", "-q", "-m", "init")
	return dir
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBuildReviewerChangeContext_InlinesDiffAndUntracked(t *testing.T) {
	dir := initReviewRepo(t)
	writeFile(t, dir, "main.go", "package main\n\nfunc main() { println(\"changed\") }\n")
	writeFile(t, dir, "new.go", "package main\n\nfunc helper() int { return 42 }\n")
	got := buildReviewerChangeContext(context.Background(), dir, defaultChangeContextBudget)

	for _, want := range []string{
		"# Change Under Review",
		"```diff",
		`+func main() { println("changed") }`,
		"## Code around each change",
		"### main.go\n```go\n",
		`    3  func main() { println("changed") }`,
		"## New untracked files",
		"### new.go\n```go\n",
		"    3  func helper() int { return 42 }",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("change context missing %q\n---\n%s", want, got)
		}
	}
}

func TestBuildReviewerChangeContext_LargeDiffFallsBackToStat(t *testing.T) {
	dir := initReviewRepo(t)
	writeFile(t, dir, "main.go", "package main\n// "+strings.Repeat("x", reviewContextMaxDiffBytes+1024)+"\n")

	got := buildReviewerChangeContext(context.Background(), dir, defaultChangeContextBudget)

	if strings.Contains(got, "```diff") {
		t.Error("oversized diff was inlined")
	}
	if !strings.Contains(got, "too large to inline") || !strings.Contains(got, "main.go") {
		t.Errorf("expected stat fallback naming main.go, got:\n%s", got)
	}
}

func TestBuildReviewerChangeContext_EmptyWhenNothingToShow(t *testing.T) {
	clean := initReviewRepo(t)
	if got := buildReviewerChangeContext(context.Background(), clean, defaultChangeContextBudget); got != "" {
		t.Errorf("clean repo: got %q, want empty", got)
	}
	if got := buildReviewerChangeContext(context.Background(), t.TempDir(), defaultChangeContextBudget); got != "" {
		t.Errorf("non-git dir: got %q, want empty", got)
	}
}

func TestParseParallelTasks_ReadsPersona(t *testing.T) {
	tasks, err := parseParallelTasks(map[string]interface{}{
		"subagents": []interface{}{
			map[string]interface{}{"prompt": "review a", "persona": "reviewer"},
			map[string]interface{}{"prompt": "plain"},
			"string task",
		},
	})
	if err != nil {
		t.Fatalf("parseParallelTasks: %v", err)
	}
	if tasks[0].Persona != "reviewer" || tasks[1].Persona != "" || tasks[2].Persona != "" {
		t.Fatalf("personas = %q,%q,%q", tasks[0].Persona, tasks[1].Persona, tasks[2].Persona)
	}

	built := buildParallelSubagentTasks([]SubagentTask{{ID: "a", Prompt: "p", Persona: "reviewer", SystemPrompt: "sys"}})
	if built[0].Persona != "reviewer" || built[0].SystemPrompt != "sys" {
		t.Fatalf("buildParallelSubagentTasks dropped persona/system prompt: %+v", built[0])
	}
}

func TestResolveParallelTaskPersonas(t *testing.T) {
	parent, _ := newReviewTestRunner(t)
	dir := initReviewRepo(t)
	writeFile(t, dir, "main.go", "package main\n\nfunc main() { println(1) }\n")
	parent.workspaceRoot = dir

	tasks := []SubagentTask{
		{ID: "r1", Prompt: "review main.go", Persona: "Code-Reviewer"},
		{ID: "r2", Prompt: "review the rest", Persona: "reviewer"},
		{ID: "plain", Prompt: "do a thing"},
	}
	if err := resolveParallelTaskPersonas(context.Background(), parent, tasks); err != nil {
		t.Fatalf("resolveParallelTaskPersonas: %v", err)
	}

	for _, task := range tasks[:2] {
		if !strings.Contains(task.SystemPrompt, "Reviewer Subagent") {
			t.Errorf("%s: reviewer system prompt not resolved", task.ID)
		}
		if !strings.HasPrefix(task.Prompt, "# Change Under Review") || !strings.Contains(task.Prompt, "# Your Task\n\n") {
			t.Errorf("%s: change context not prepended:\n%s", task.ID, truncateString(task.Prompt, 200))
		}
	}
	if !strings.HasSuffix(tasks[0].Prompt, "# Your Task\n\nreview main.go") {
		t.Errorf("r1 task text lost: %q", truncateString(tasks[0].Prompt, 80))
	}
	if tasks[2].Prompt != "do a thing" {
		t.Errorf("persona-less task prompt modified: %q", tasks[2].Prompt)
	}
	if tasks[2].Persona != "general" || tasks[2].SystemPrompt == "" {
		t.Errorf("persona-less task did not get the default persona: persona=%q prompt=%q", tasks[2].Persona, truncateString(tasks[2].SystemPrompt, 60))
	}

	bad := []SubagentTask{{ID: "x", Prompt: "p", Persona: "no_such_persona"}}
	if err := resolveParallelTaskPersonas(context.Background(), parent, bad); err == nil {
		t.Error("expected error for unknown persona")
	}
}

func TestRunParallel_UsesPerTaskSystemPrompt(t *testing.T) {
	_, runner := newReviewTestRunner(t)

	clients := make(chan *ScriptedClient, 2)
	runner.testClientFactory = func(api.ClientType, string) (api.ClientInterface, error) {
		c := NewScriptedClient(NewScriptedResponseBuilder().Content("finished reviewing the assigned slice; no issues found in it.").Build())
		clients <- c
		return c, nil
	}

	results := runner.RunParallel(context.Background(), []SubagentTask{
		{ID: "a", Prompt: "review a", Persona: "reviewer", SystemPrompt: "SYSTEM-PROMPT-A"},
		{ID: "b", Prompt: "review b", Persona: "reviewer", SystemPrompt: "SYSTEM-PROMPT-B"},
	}, SubagentOptions{})
	close(clients)

	for _, r := range results {
		if r.Error != nil {
			t.Fatalf("task %s failed: %v", r.ID, r.Error)
		}
	}

	seen := map[string]bool{}
	for c := range clients {
		for _, req := range c.GetSentRequests() {
			for _, m := range req {
				if m.Role != "system" {
					continue
				}
				for _, marker := range []string{"SYSTEM-PROMPT-A", "SYSTEM-PROMPT-B"} {
					if strings.Contains(m.Content, marker) {
						seen[marker] = true
					}
				}
			}
		}
	}
	if !seen["SYSTEM-PROMPT-A"] || !seen["SYSTEM-PROMPT-B"] {
		t.Fatalf("per-task system prompts not used; saw %v", seen)
	}
}

func TestCreateSubagent_SystemPromptIncludesRepoConventions(t *testing.T) {
	parent, runner := newReviewTestRunner(t)
	dir := t.TempDir()
	writeFile(t, dir, "AGENTS.md", "Always wrap errors with context.\n")
	runner.shared.WorkspaceRoot = dir
	_ = parent

	sub, err := runner.createSubagent(SubagentOptions{Persona: "coder"}, context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Shutdown()
	if !strings.Contains(sub.systemPrompt, "## Repo Conventions (from AGENTS.md") || !strings.Contains(sub.systemPrompt, "Always wrap errors with context.") {
		t.Errorf("conventions missing from subagent system prompt:\n%s", truncateString(sub.systemPrompt, 300))
	}

	runner.shared.WorkspaceRoot = t.TempDir()
	bare, err := runner.createSubagent(SubagentOptions{Persona: "coder"}, context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer bare.Shutdown()
	if strings.Contains(bare.systemPrompt, "Repo Conventions") {
		t.Error("conventions section added for a workspace without a conventions file")
	}
}

func TestIsSubagentSecurityFailure(t *testing.T) {
	security := []string{
		"write: ErrWriteOutsideWorkingDirectory",
		"security rejected: shell_command — rm -rf. The user declined approval.",
		"security hard block: x",
		"SUBAGENT_SECURITY_ERROR: nested",
	}
	for _, msg := range security {
		if !isSubagentSecurityFailure(msg, "") {
			t.Errorf("not classified as security: %q", msg)
		}
	}
	ordinary := []string{
		"max iterations (38) reached",
		"provider returned 500",
		"failed to update security_test.go: compile error",
		"",
	}
	for _, msg := range ordinary {
		if isSubagentSecurityFailure(msg, "reviewed the security of the change") {
			t.Errorf("ordinary failure classified as security: %q", msg)
		}
	}
}

func TestHandleSubagentSecurityError_OrdinaryFailureInNestedSubagent(t *testing.T) {
	_, runner := newReviewTestRunner(t)
	nested, err := runner.createSubagent(SubagentOptions{Persona: "coder"}, context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer nested.Shutdown()

	ordinary := map[string]string{"exit_code": "1", "stderr": "provider returned 500", "stdout": ""}
	if msg := handleSubagentSecurityError(nested, ordinary); msg != "" {
		t.Errorf("ordinary failure reported as security error:\n%s", msg)
	}
	blocked := map[string]string{"exit_code": "1", "stderr": "security hard block: rm", "stdout": ""}
	if msg := handleSubagentSecurityError(nested, blocked); !strings.HasPrefix(msg, "SUBAGENT_SECURITY_ERROR") {
		t.Errorf("security block not delegated: %q", msg)
	}
}

func TestExtractSubagentSummary_RecordsWroteLines(t *testing.T) {
	summary := extractSubagentSummary("Created: a.go\nWrote b.go\nWrote\nModified: c.go\n")
	for _, want := range []string{"Created: a.go", "Created: b.go", "Modified: c.go"} {
		if !strings.Contains(summary["files"], want) {
			t.Errorf("files summary %q missing %q", summary["files"], want)
		}
	}
}

func TestBuildReviewerChangeContext_ExcerptBudget(t *testing.T) {
	dir := initReviewRepo(t)
	big := strings.Repeat("// filler line for budget testing ..............................\n", 350)
	for i := 0; i < 6; i++ {
		writeFile(t, dir, fmt.Sprintf("new%d.go", i), "package main\n"+big)
	}

	got := buildReviewerChangeContext(context.Background(), dir, defaultChangeContextBudget)

	if len(got) > reviewContextMaxExcerptBytes+reviewContextMaxStatBytes+8*1024 {
		t.Errorf("change context %d bytes exceeds the excerpt budget", len(got))
	}
	if !strings.Contains(got, "### new0.go") {
		t.Error("first new file not pre-loaded")
	}
	if !strings.Contains(got, "## Other untracked files (not pre-loaded") || !strings.Contains(got, "- new5.go") {
		t.Errorf("files over budget not listed:\n%s", got[max(0, len(got)-600):])
	}
}

func TestPrepareSubagentLaunch_ReviewerFilesBecomeFocusList(t *testing.T) {
	parent, _ := newReviewTestRunner(t)
	dir := initReviewRepo(t)
	writeFile(t, dir, "main.go", "package main\n\nfunc main() { println(\"changed\") }\n")
	parent.workspaceRoot = dir

	spec, err := prepareSubagentLaunch(context.Background(), parent, map[string]interface{}{
		"prompt":  "review it",
		"persona": "reviewer",
		"files":   "main.go",
	})
	if err != nil {
		t.Fatalf("prepareSubagentLaunch: %v", err)
	}
	if strings.Contains(spec.enhancedPrompt, "# Relevant Files") {
		t.Error("reviewer prompt inlined whole files from `files`")
	}
	if !strings.Contains(spec.enhancedPrompt, "Focus files (excerpts of their changes are in the change context above): main.go") {
		t.Errorf("focus list missing:\n%s", spec.enhancedPrompt)
	}

	coder, err := prepareSubagentLaunch(context.Background(), parent, map[string]interface{}{
		"prompt":  "edit it",
		"persona": "coder",
		"files":   "main.go",
	})
	if err != nil {
		t.Fatalf("prepareSubagentLaunch: %v", err)
	}
	if !strings.Contains(coder.enhancedPrompt, "# Relevant Files") {
		t.Error("non-reviewer personas should still get whole files inlined")
	}
}
