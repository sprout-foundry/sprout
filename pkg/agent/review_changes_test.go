package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

const findingsMustFix = "Checked main.go.\n```json\n" +
	`{"verdict":"CHANGES_REQUIRED","findings":[{"severity":"MUST_FIX","file":"main.go","line":"3","issue":"prints debug output","evidence":"println","fix":"remove it"}]}` +
	"\n```\nVERDICT: CHANGES_REQUIRED"

const findingsClean = "Checked the slice.\n```json\n{\"verdict\":\"APPROVE\",\"findings\":[]}\n```\nVERDICT: APPROVE"

// scriptReviewers makes every reviewer subagent answer with reply and
// records the task prompt each one received.
func scriptReviewers(t *testing.T, parent *Agent, reply func(prompt string) string) *[]string {
	t.Helper()
	var mu sync.Mutex
	var prompts []string
	parent.GetSubagentRunner().testClientFactory = func(api.ClientType, string) (api.ClientInterface, error) {
		c := &promptEchoClient{ScriptedClient: NewScriptedClient(), reply: reply, record: func(p string) {
			mu.Lock()
			prompts = append(prompts, p)
			mu.Unlock()
		}}
		return c, nil
	}
	return &prompts
}

// promptEchoClient answers from the user prompt it receives, so parallel
// reviewers can each get a reply matching their slice.
type promptEchoClient struct {
	*ScriptedClient
	reply  func(prompt string) string
	record func(prompt string)
}

func (c *promptEchoClient) respond(messages []api.Message) {
	var user string
	for _, m := range messages {
		if m.Role == "user" {
			user = m.Content
			break
		}
	}
	c.record(user)
	c.SetResponses([]*ScriptedResponse{NewScriptedResponseBuilder().Content(c.reply(user)).Build()})
}

func (c *promptEchoClient) SendChatRequest(ctx context.Context, messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
	c.respond(messages)
	return c.ScriptedClient.SendChatRequest(ctx, messages, tools, reasoning, disableThinking)
}

func (c *promptEchoClient) SendChatRequestStream(ctx context.Context, messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool, callback api.StreamCallback) (*api.ChatResponse, error) {
	c.respond(messages)
	return c.ScriptedClient.SendChatRequestStream(ctx, messages, tools, reasoning, disableThinking, callback)
}

func TestReviewChanges_SingleReviewerParsesFindings(t *testing.T) {
	parent, _ := newReviewTestRunner(t)
	dir := initReviewRepo(t)
	writeFile(t, dir, "main.go", "package main\n\nfunc main() { println(\"debug\") }\n")
	parent.workspaceRoot = dir
	prompts := scriptReviewers(t, parent, func(string) string { return findingsMustFix })

	review, err := parent.ReviewChanges(context.Background(), ReviewChangesOptions{Focus: "debug output"})
	if err != nil {
		t.Fatalf("ReviewChanges: %v", err)
	}
	if review.Verdict != "CHANGES_REQUIRED" || len(review.Findings) != 1 || len(review.Reviewers) != 1 {
		t.Fatalf("unexpected review: %+v", review)
	}
	f := review.Findings[0]
	if f.Severity != "MUST_FIX" || f.File != "main.go" || f.Line != 3 {
		t.Errorf("finding not parsed: %+v", f)
	}
	md := review.Markdown()
	if !strings.Contains(md, "Review verdict: CHANGES_REQUIRED — 1 MUST_FIX") || !strings.Contains(md, "- main.go:3 — prints debug output") {
		t.Errorf("unexpected markdown:\n%s", md)
	}
	if len(*prompts) == 0 || !strings.Contains((*prompts)[0], "# Change Under Review") || !strings.Contains((*prompts)[0], "Focus: debug output") {
		t.Errorf("reviewer task missing context or focus")
	}
}

func TestReviewChanges_SplitsLargeChangeAcrossReviewers(t *testing.T) {
	parent, _ := newReviewTestRunner(t)
	dir := initReviewRepo(t)
	body := strings.Repeat("// line\n", 700)
	for _, name := range []string{"a.go", "b.go", "c.go"} {
		writeFile(t, dir, name, "package main\n"+body)
	}
	parent.workspaceRoot = dir
	prompts := scriptReviewers(t, parent, func(prompt string) string {
		if strings.Contains(prompt, "own only these files: a.go") {
			return strings.ReplaceAll(findingsMustFix, "main.go", "a.go")
		}
		return findingsClean
	})

	review, err := parent.ReviewChanges(context.Background(), ReviewChangesOptions{})
	if err != nil {
		t.Fatalf("ReviewChanges: %v", err)
	}
	if len(review.Reviewers) < 2 {
		t.Fatalf("expected the ~2100-line change to split, got %d reviewer(s)", len(review.Reviewers))
	}
	if review.Verdict != "CHANGES_REQUIRED" || len(review.Findings) != 1 || review.Findings[0].File != "a.go" {
		t.Errorf("findings not merged across reviewers: %+v", review)
	}
	for _, p := range *prompts {
		owned := p[strings.Index(p, "own only these files: ")+len("own only these files: "):]
		owned = owned[:strings.Index(owned, ".\n")+1]
		for _, name := range []string{"a.go", "b.go", "c.go"} {
			if !strings.Contains(owned, name) && strings.Contains(p, "### "+name+"\n") {
				t.Errorf("reviewer owning %q was given %s's content", owned, name)
			}
		}
	}
}

func TestReviewChanges_RangeScopeExcerptsCommitContent(t *testing.T) {
	parent, _ := newReviewTestRunner(t)
	dir := initReviewRepo(t)
	writeFile(t, dir, "main.go", "package main\n\nfunc main() { println(\"committed\") }\n")
	gitIn(t, dir, "commit", "-qam", "change")
	writeFile(t, dir, "main.go", "package main\n\nfunc main() { println(\"later edit\") }\n")
	parent.workspaceRoot = dir
	prompts := scriptReviewers(t, parent, func(string) string { return findingsClean })

	review, err := parent.ReviewChanges(context.Background(), ReviewChangesOptions{Scope: "range", Range: "HEAD^!"})
	if err != nil {
		t.Fatalf("ReviewChanges: %v", err)
	}
	if review.Verdict != "APPROVE" {
		t.Errorf("verdict = %s", review.Verdict)
	}
	p := (*prompts)[0]
	if !strings.Contains(p, "Change in range `HEAD^!`") || !strings.Contains(p, "(content at HEAD, line-numbered)") ||
		!strings.Contains(p, `println("committed")`) || strings.Contains(p, "later edit") {
		t.Errorf("range review did not use the commit's content:\n%s", p)
	}
}

func TestReviewChanges_Errors(t *testing.T) {
	parent, _ := newReviewTestRunner(t)
	parent.workspaceRoot = initReviewRepo(t)

	if _, err := parent.ReviewChanges(context.Background(), ReviewChangesOptions{}); err == nil {
		t.Error("expected an error when there is nothing to review")
	}
	for _, bad := range []string{"--output=/tmp/x", "HEAD -- x", "HEAD:main.go", ""} {
		if _, err := parent.ReviewChanges(context.Background(), ReviewChangesOptions{Scope: "range", Range: bad}); err == nil {
			t.Errorf("range %q accepted", bad)
		}
	}
	if _, err := parent.ReviewChanges(context.Background(), ReviewChangesOptions{Scope: "everything"}); err == nil {
		t.Error("unknown scope accepted")
	}

	parent.subagentDepth = parent.MaxSubagentDepth()
	if _, err := parent.ReviewChanges(context.Background(), ReviewChangesOptions{}); err == nil || !strings.Contains(err.Error(), "depth") {
		t.Errorf("expected depth-limit error, got %v", err)
	}
}

func TestPlanReviewGroups(t *testing.T) {
	if g := planReviewGroups(map[string]int{"a.go": 10, "b.go": 20}, 1200); len(g) != 1 || len(g[0]) != 2 {
		t.Errorf("small change split: %v", g)
	}
	if g := planReviewGroups(map[string]int{"huge.go": 9000}, 1200); len(g) != 1 {
		t.Errorf("single file split: %v", g)
	}

	counts := map[string]int{}
	for i := 0; i < 20; i++ {
		counts[fmt.Sprintf("pkg%d/f.go", i)] = 300
	}
	groups := planReviewGroups(counts, 1200)
	if len(groups) != reviewMaxReviewers {
		t.Fatalf("6000 lines: %d groups, want %d", len(groups), reviewMaxReviewers)
	}
	seen := map[string]bool{}
	prev := ""
	for _, g := range groups {
		if len(g) == 0 {
			t.Fatal("empty group")
		}
		for _, f := range g {
			if seen[f] || f < prev {
				t.Fatalf("groups are not a sorted partition: %v", groups)
			}
			seen[f], prev = true, f
		}
	}
	if len(seen) != len(counts) {
		t.Errorf("files lost: %d of %d", len(seen), len(counts))
	}
}

func TestNumstatPath(t *testing.T) {
	for in, want := range map[string]string{
		"pkg/a.go":               "pkg/a.go",
		"old.go => new.go":       "new.go",
		"pkg/{old => new}/f.go":  "pkg/new/f.go",
		"pkg/{a => }/f.go":       "pkg/f.go",
		"{pkg/a.go => cmd/b.go}": "cmd/b.go",
	} {
		if got := numstatPath(in); got != want {
			t.Errorf("numstatPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseReviewerReport(t *testing.T) {
	if r, ok := parseReviewerReport(findingsMustFix); !ok || r.Verdict != "CHANGES_REQUIRED" || len(r.Findings) != 1 || r.Findings[0].Line != 3 {
		t.Errorf("fenced report: %+v %v", r, ok)
	}
	bare := `done. {"verdict":"APPROVE","findings":[{"severity":"high","file":"x.go","line":10,"issue":"race"}]} VERDICT: APPROVE`
	if r, ok := parseReviewerReport(bare); !ok || r.Findings[0].Severity != "MUST_FIX" || r.Findings[0].Line != 10 {
		t.Errorf("unfenced report: %+v %v", r, ok)
	}
	if _, ok := parseReviewerReport("MUST_FIX: something\nVERDICT: CHANGES_REQUIRED"); ok {
		t.Error("markdown-only report parsed as structured")
	}
}

func TestMergeReviewResults(t *testing.T) {
	tasks := []SubagentTask{{ID: "review-1"}, {ID: "review-2"}}
	groups := [][]string{{"a.go"}, {"b.go"}}
	dup := "```json\n{\"verdict\":\"APPROVE\",\"findings\":[{\"severity\":\"VERIFY\",\"file\":\"a.go\",\"line\":1,\"issue\":\"Possible nil\"}]}\n```"
	dupMust := "```json\n{\"verdict\":\"CHANGES_REQUIRED\",\"findings\":[{\"severity\":\"MUST_FIX\",\"file\":\"a.go\",\"line\":1,\"issue\":\"possible  NIL\"}]}\n```"

	r := mergeReviewResults(tasks, groups, []*SubagentResult{{Output: dup}, {Output: dupMust}})
	if len(r.Findings) != 1 || r.Findings[0].Severity != "MUST_FIX" || r.Verdict != "CHANGES_REQUIRED" {
		t.Errorf("duplicate not merged to highest severity: %+v", r)
	}

	r = mergeReviewResults(tasks, groups, []*SubagentResult{{Output: findingsClean}, {Output: "looks fine to me", Error: errors.New("max iterations")}})
	if r.Verdict != "INCONCLUSIVE" || r.Reviewers[1].Unstructured != "looks fine to me" || r.Reviewers[1].Error == "" {
		t.Errorf("failed reviewer not surfaced: %+v", r)
	}
	if md := r.Markdown(); !strings.Contains(md, "## Reviewer review-2 (b.go)") || !strings.Contains(md, "Unstructured report:\nlooks fine to me") {
		t.Errorf("markdown hides the failed reviewer:\n%s", md)
	}

	r = mergeReviewResults(tasks[:1], groups[:1], []*SubagentResult{{Output: findingsClean}})
	if r.Verdict != "APPROVE" || !strings.Contains(r.Markdown(), "No issues found.") {
		t.Errorf("clean review: %+v", r)
	}
}

func TestBuildChangeContext_PathSubset(t *testing.T) {
	dir := initReviewRepo(t)
	writeFile(t, dir, "main.go", "package main\n\nfunc main() { println(1) }\n")
	writeFile(t, dir, "other.go", "package main\n\nfunc other() {}\n")

	got := buildChangeContext(context.Background(), dir, workingTreeTarget, []string{"other.go"}, defaultChangeContextBudget)
	if !strings.Contains(got, "### other.go") || strings.Contains(got, "println(1)") {
		t.Errorf("path subset not applied:\n%s", got)
	}
	if !strings.Contains(got, "This review covers only the files below") {
		t.Error("subset reviewer not told its scope")
	}
}

func TestReviewChangesResult_GuidanceJSON(t *testing.T) {
	r := &ReviewChangesResult{Findings: []ReviewFinding{
		{Severity: "MUST_FIX", File: "a.go", Line: 3, Issue: "nil deref", Evidence: "x.y", Fix: "check x"},
		{Severity: "VERIFY", Issue: "intended?"},
	}}
	got := r.GuidanceJSON()
	for _, want := range []string{`"MUST_FIX":[{`, `"file":"a.go:3"`, `"suggestion":"check x"`, `"VERIFY":[{"issue":"intended?"}]`} {
		if !strings.Contains(got, want) {
			t.Errorf("GuidanceJSON missing %s: %s", want, got)
		}
	}
	if (&ReviewChangesResult{}).GuidanceJSON() != "" {
		t.Error("empty findings should produce no guidance")
	}
}

func TestChangeContextBudgetFor(t *testing.T) {
	if b := changeContextBudgetFor(0); b != defaultChangeContextBudget {
		t.Errorf("unknown window should use the default budget: %+v", b)
	}
	if b := changeContextBudgetFor(1_000_000); b != defaultChangeContextBudget {
		t.Errorf("large window should be capped at the default budget: %+v", b)
	}
	b := changeContextBudgetFor(128_000)
	if b.total >= defaultChangeContextBudget.total || b.total > 128_000*approxBytesPerToken*40/100 {
		t.Errorf("128k budget not scaled to the window: %+v", b)
	}
	if b.diff+b.stat+b.excerpts > b.total {
		t.Errorf("parts exceed total: %+v", b)
	}
	if got := changeContextBudgetFor(32_000).linesPerReviewer(); got != 252 {
		t.Errorf("32k linesPerReviewer = %d, want 252", got)
	}
	if got := defaultChangeContextBudget.linesPerReviewer(); got != 1200 {
		t.Errorf("default linesPerReviewer = %d, want 1200", got)
	}
}

func TestBuildChangeContext_RespectsBudget(t *testing.T) {
	dir := initReviewRepo(t)
	for _, name := range []string{"a.go", "b.go", "c.go"} {
		writeFile(t, dir, name, "package main\n"+strings.Repeat("// a fairly long line of new code for budget testing\n", 300))
	}
	small := changeContextBudgetFor(32_000)
	got := buildChangeContext(context.Background(), dir, workingTreeTarget, nil, small)
	if len(got) > small.total+8*1024 {
		t.Errorf("change context %d bytes exceeds the %d-byte budget", len(got), small.total)
	}
	if !strings.Contains(got, "not pre-loaded") {
		t.Error("files over the budget should be listed, not silently dropped")
	}
}
