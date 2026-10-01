package agent

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...) //nolint:gosec // G204: git on the test's own temp dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
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

	got := buildReviewerChangeContextForScope(context.Background(), dir, reviewScopeStaged)

	if !strings.Contains(got, `+func main() { println("staged") }`) {
		t.Errorf("staged hunk missing:\n%s", got)
	}
	if strings.Contains(got, "unstaged") || strings.Contains(got, "untracked.go") {
		t.Errorf("staged scope leaked unstaged/untracked content:\n%s", got)
	}
	if !strings.Contains(got, "git diff --cached") {
		t.Errorf("staged scope not labeled:\n%s", got)
	}
}

func TestParseReviewVerdict(t *testing.T) {
	cases := map[string]string{
		"all good\nVERDICT: APPROVE":                       "approved",
		"MUST_FIX: x\n**VERDICT: CHANGES_REQUIRED**":       "needs_revision",
		"verdict: approve":                                 "approved",
		"VERDICT: APPROVE or VERDICT: CHANGES_REQUIRED...": "needs_revision",
		"ran out of budget before finishing":               "inconclusive",
	}
	for report, want := range cases {
		if got := parseReviewVerdict(report); got != want {
			t.Errorf("parseReviewVerdict(%q) = %q, want %q", report, got, want)
		}
	}
}

func TestRunStagedReview_RunsReviewerSubagentOnStagedDiff(t *testing.T) {
	parent, _ := newReviewTestRunner(t)
	dir := initReviewRepo(t)
	writeFile(t, dir, "main.go", "package main\n\nfunc main() { println(\"staged\") }\n")
	gitIn(t, dir, "add", "main.go")
	parent.workspaceRoot = dir

	var client *ScriptedClient
	parent.GetSubagentRunner().testClientFactory = func(api.ClientType, string) (api.ClientInterface, error) {
		client = NewScriptedClient(NewScriptedResponseBuilder().
			Content("MUST_FIX main.go:3 prints debug output.\nVERDICT: CHANGES_REQUIRED").Build())
		return client, nil
	}

	res, err := parent.RunStagedReview(context.Background(), "debug output", "", "")
	if err != nil {
		t.Fatalf("RunStagedReview: %v", err)
	}
	if res.Verdict != "needs_revision" || !strings.Contains(res.Report, "MUST_FIX") {
		t.Fatalf("result = %+v", res)
	}

	var system, user string
	for _, m := range client.GetSentRequest(0) {
		switch m.Role {
		case "system":
			system += m.Content
		case "user":
			user += m.Content
		}
	}
	if !strings.Contains(system, "Reviewer Subagent") {
		t.Error("reviewer persona prompt not used")
	}
	for _, want := range []string{"# Change Under Review", `println("staged")`, "Focus: debug output"} {
		if !strings.Contains(user, want) {
			t.Errorf("task missing %q", want)
		}
	}
}

func TestRunStagedReview_ErrorsWithoutStagedChanges(t *testing.T) {
	parent, _ := newReviewTestRunner(t)
	parent.workspaceRoot = initReviewRepo(t)

	if _, err := parent.RunStagedReview(context.Background(), "", "", ""); err == nil {
		t.Fatal("expected error when nothing is staged")
	}
}
