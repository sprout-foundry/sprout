package agent

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...) //nolint:gosec // G204: git on the test's own temp dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestBuildReviewerChangeContext_StagedScopeExcludesUnstaged(t *testing.T) {
	dir := initReviewRepo(t)
	writeFile(t, dir, "main.go", "package main\n\nfunc main() { println(\"staged\") }\n")
	gitIn(t, dir, "add", "main.go")
	writeFile(t, dir, "main.go", "package main\n\nfunc main() { println(\"unstaged\") }\n")
	writeFile(t, dir, "untracked.go", "package main\n")

	got := buildChangeContext(context.Background(), dir, stagedTarget, nil, defaultChangeContextBudget)

	if !strings.Contains(got, `+func main() { println("staged") }`) {
		t.Errorf("staged hunk missing:\n%s", got)
	}
	if strings.Contains(got, "unstaged") || strings.Contains(got, "untracked.go") {
		t.Errorf("staged scope leaked unstaged/untracked content:\n%s", got)
	}
	if !strings.Contains(got, "(staged content, line-numbered)") || !strings.Contains(got, `    3  func main() { println("staged") }`) {
		t.Errorf("staged excerpt not taken from the index:\n%s", got)
	}
	if !strings.Contains(got, "git diff --cached") {
		t.Errorf("staged scope not labeled:\n%s", got)
	}
}

func TestResolveSubagentProviderModel_ReviewerUsesReviewSettings(t *testing.T) {
	parent, _ := newReviewTestRunner(t)
	if err := parent.configManager.UpdateConfigNoSave(func(c *configuration.Config) error {
		c.SubagentProvider = "subagent-prov"
		c.SubagentModel = "subagent-model"
		c.ReviewProvider = "review-prov"
		c.ReviewModel = "review-model"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()

	provider, model, _, _, err := resolveSubagentProviderModel(parent, "code_reviewer", true, root)
	if err != nil {
		t.Fatal(err)
	}
	if provider != "review-prov" || model != "review-model" {
		t.Errorf("reviewer resolved to %s/%s, want review-prov/review-model", provider, model)
	}

	provider, model, _, _, err = resolveSubagentProviderModel(parent, "coder", true, root)
	if err != nil {
		t.Fatal(err)
	}
	if provider != "subagent-prov" || model != "subagent-model" {
		t.Errorf("coder resolved to %s/%s, want subagent settings", provider, model)
	}
}

func TestResolveSubagentProviderModel_ReviewerWithoutReviewSettings(t *testing.T) {
	parent, _ := newReviewTestRunner(t)
	if err := parent.configManager.UpdateConfigNoSave(func(c *configuration.Config) error {
		c.SubagentProvider = "subagent-prov"
		c.SubagentModel = "subagent-model"
		c.ReviewProvider = ""
		c.ReviewModel = ""
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	provider, model, _, _, err := resolveSubagentProviderModel(parent, "reviewer", true, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if provider != "subagent-prov" || model != "subagent-model" {
		t.Errorf("reviewer resolved to %s/%s, want subagent settings when review_* unset", provider, model)
	}
}
