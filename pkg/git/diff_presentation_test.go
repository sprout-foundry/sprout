package git

import (
	"context"
	"os"
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// --- splitDiffSections ---

func TestSplitDiffSections(t *testing.T) {
	raw := strings.Join([]string{
		"diff --git a/main.go b/main.go",
		"index 111..222 100644",
		"--- a/main.go",
		"+++ b/main.go",
		"@@ -1,2 +1,2 @@",
		"-old",
		"+new",
		"diff --git a/deleted.txt b/deleted.txt",
		"deleted file mode 100644",
		"--- a/deleted.txt",
		"+++ /dev/null",
		"@@ -1 +0,0 @@",
		"-gone",
	}, "\n")

	sections := splitDiffSections(raw)
	if len(sections) != 2 {
		t.Fatalf("expected 2 sections, got %d: %v", len(sections), keysOf(sections))
	}
	if !strings.Contains(sections["main.go"], "+new") {
		t.Errorf("main.go section missing content: %q", sections["main.go"])
	}
	// Deletions key on the --- a/ path since +++ is /dev/null.
	if !strings.Contains(sections["deleted.txt"], "-gone") {
		t.Errorf("deleted.txt section missing content: %q", sections["deleted.txt"])
	}
}

func TestSplitDiffSections_PathWithSpaces(t *testing.T) {
	raw := strings.Join([]string{
		`diff --git a/docs/my file.md b/docs/my file.md`,
		`--- a/docs/my file.md`,
		`+++ b/docs/my file.md`,
		`@@ -1 +1 @@`,
		`-a`,
		`+b`,
	}, "\n")

	sections := splitDiffSections(raw)
	if _, ok := sections["docs/my file.md"]; !ok {
		t.Fatalf("expected space-containing path, got %v", keysOf(sections))
	}
}

func TestSplitDiffSections_RenameUsesNewPath(t *testing.T) {
	raw := strings.Join([]string{
		"diff --git a/old_name.go b/new_name.go",
		"similarity index 95%",
		"--- a/old_name.go",
		"+++ b/new_name.go",
		"@@ -1 +1 @@",
		"-x",
		"+y",
	}, "\n")

	sections := splitDiffSections(raw)
	if _, ok := sections["new_name.go"]; !ok {
		t.Fatalf("expected post-rename path, got %v", keysOf(sections))
	}
}

// --- countDiffStats ---

func TestCountDiffStats(t *testing.T) {
	section := strings.Join([]string{
		"--- a/f.go",
		"+++ b/f.go",
		"@@ -1,3 +1,3 @@",
		"+added",
		"-removed",
		" context",
	}, "\n")

	// Note: a content line that begins with "+++ " is indistinguishable
	// from the metadata header in this parse and is deliberately skipped.
	additions, deletions := countDiffStats(section)
	if additions != 1 {
		t.Errorf("additions = %d, want 1", additions)
	}
	if deletions != 1 {
		t.Errorf("deletions = %d, want 1", deletions)
	}
}

// --- truncateDiffSection ---

func TestTruncateDiffSection_ShortInputUnchanged(t *testing.T) {
	in := "short diff"
	if got := truncateDiffSection(in, 1000); got != in {
		t.Errorf("short section should pass through, got %q", got)
	}
}

func TestTruncateDiffSection_LineBoundaryAndMarker(t *testing.T) {
	in := strings.Repeat("line of content here\n", 100) // 2200 bytes
	got := truncateDiffSection(in, 1000)

	if !strings.Contains(got, "[... truncated: showing first") {
		t.Errorf("missing truncation marker in %q", got)
	}
	if !strings.Contains(got, fmt_sprintf(int64(len(in)))) {
		t.Errorf("marker should state total size %d", len(in))
	}
	if !strings.HasSuffix(strings.SplitN(got, "\n\n[... ", 2)[0], "here") {
		t.Errorf("truncation should land on a line boundary, got %q", got)
	}
	if len(got) > 1000 {
		t.Errorf("truncated section exceeds budget: %d bytes", len(got))
	}
}

// fmt_sprintf keeps the total-size assertion readable above.
func fmt_sprintf(n int64) string {
	return strings.TrimSpace(toDecimal(n))
}

func toDecimal(n int64) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// --- prepareBudgetedDiff ---

func TestPrepareBudgetedDiff_NonSemanticFilesBecomeSummaries(t *testing.T) {
	raw := strings.Join([]string{
		"diff --git a/main.go b/main.go",
		"--- a/main.go",
		"+++ b/main.go",
		"@@ -1 +1 @@",
		"-old",
		"+new",
		"diff --git a/package-lock.json b/package-lock.json",
		"--- a/package-lock.json",
		"+++ b/package-lock.json",
		"@@ -1,2 +1,2 @@",
		"-\"a\": 1,",
		"+\"a\": 2,",
	}, "\n")

	out, warnings := prepareBudgetedDiff(
		[]CommitFileChange{{Status: "M", Path: "main.go"}, {Status: "M", Path: "package-lock.json"}},
		raw, 0)

	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
	if !strings.Contains(out, "→ main.go (modified, source): +1 -1") {
		t.Errorf("semantic file should keep its diff with a context header, got:\n%s", out)
	}
	if !strings.Contains(out, "+new") {
		t.Errorf("semantic diff content should be present, got:\n%s", out)
	}
	if !strings.Contains(out, "[Lockfile: package-lock.json — 1 additions, 1 deletions]") {
		t.Errorf("lockfile should collapse to a summary line, got:\n%s", out)
	}
	// Lockfile diff content must not leak through.
	if strings.Contains(out, "\"a\": 2,") {
		t.Errorf("lockfile diff content should be omitted, got:\n%s", out)
	}
}

func TestPrepareBudgetedDiff_SemanticPriorityAndTruncation(t *testing.T) {
	sem1 := strings.Repeat("a\n", 300) // 600 bytes
	sem2 := strings.Repeat("b\n", 500) // 1000 bytes
	raw := "diff --git a/first.go b/first.go\n--- a/first.go\n+++ b/first.go\n" +
		"+_sem1_placeholder\n" +
		"diff --git a/second.go b/second.go\n--- a/second.go\n+++ b/second.go\n"
	// Build realistic sections with known sizes.
	sections := []string{
		"diff --git a/first.go b/first.go\n--- a/first.go\n+++ b/first.go\n+" + strings.TrimSuffix(sem1, "\n"),
		"diff --git a/second.go b/second.go\n--- a/second.go\n+++ b/second.go\n+" + strings.TrimSuffix(sem2, "\n"),
	}
	raw = strings.Join(sections, "\n")

	// Budget 1200 → semantic budget 960. first.go (605B) fits; second.go
	// (1005B) no longer fits → 355B remaining ≥ 200 → truncated with marker.
	out, _ := prepareBudgetedDiff(
		[]CommitFileChange{{Status: "M", Path: "first.go"}, {Status: "M", Path: "second.go"}},
		raw, 1200)

	if !strings.Contains(out, "→ first.go (modified, source): +1 -0") {
		t.Errorf("first semantic diff should be intact with its header, got:\n%s", out)
	}
	if n := strings.Count(out, "[... truncated: showing first"); n != 1 {
		t.Errorf("only second.go should be truncated, found %d markers:\n%s", n, out)
	}
	if !strings.Contains(out, "[Truncated: second.go — total") {
		t.Errorf("truncated file should also get a summary line, got:\n%s", out)
	}
	// The truncated tail of second.go must not appear.
	if strings.Contains(out, strings.TrimSuffix(sem2[len(sem2)-400:], "\n")) {
		t.Errorf("truncated content should be cut, got:\n%s", out)
	}
}

func TestPrepareBudgetedDiff_TinyBudgetCollapsesToSummaries(t *testing.T) {
	sem1 := strings.Repeat("a\n", 300) // 600 bytes
	raw := "diff --git a/first.go b/first.go\n--- a/first.go\n+++ b/first.go\n+" + strings.TrimSuffix(sem1, "\n")

	// Budget 400 → semantic budget 320; remaining 320 ≥ 200 → truncated.
	out, _ := prepareBudgetedDiff([]CommitFileChange{{Status: "M", Path: "first.go"}}, raw, 400)
	if !strings.Contains(out, "[... truncated") {
		t.Errorf("expected truncation, got:\n%s", out)
	}

	// Budget 240 → semantic budget 192; remaining < 200 → summary only.
	out, _ = prepareBudgetedDiff([]CommitFileChange{{Status: "M", Path: "first.go"}}, raw, 240)
	if strings.Contains(out, "+aaa") {
		t.Errorf("no diff content should remain under a tiny budget, got:\n%s", out)
	}
	if !strings.Contains(out, "[Truncated: first.go — total 663 bytes]") {
		t.Errorf("expected collapsed summary line, got:\n%s", out)
	}
}

func TestPrepareBudgetedDiff_BinaryWarning(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("logo.bin", bytes_repeat(0x00, 512), 0o644); err != nil {
		t.Fatal(err)
	}
	raw := "diff --git a/logo.bin b/logo.bin\n--- a/logo.bin\n+++ b/logo.bin\nBinary files differ\n"

	out, warnings := prepareBudgetedDiff([]CommitFileChange{{Status: "M", Path: "logo.bin"}}, raw, 0)

	if len(warnings) != 1 || !strings.Contains(warnings[0], "Binary file staged: logo.bin") {
		t.Errorf("expected binary warning, got %v", warnings)
	}
	if !strings.Contains(out, "[Binary file: logo.bin") {
		t.Errorf("binary should collapse to a summary, got:\n%s", out)
	}
}

func TestPrepareBudgetedDiff_UnlistedSectionsIncluded(t *testing.T) {
	raw := "diff --git a/main.go b/main.go\n--- a/main.go\n+++ b/main.go\n@@ -1 +1 @@\n-old\n+new\n"
	out, _ := prepareBudgetedDiff(nil, raw, 0)
	if !strings.Contains(out, "+new") {
		t.Errorf("sections without FileChanges entries should still be presented, got:\n%s", out)
	}
}

// --- prompt integration ---

func newMockClientSilent() *mockAPIClient {
	return &mockAPIClient{}
}

// recordingClient captures the prompts sent to the model while reusing the
// package's existing mockAPIClient response behavior.
type recordingClient struct {
	*mockAPIClient
	titlePrompts []string
	descPrompts  []string
}

func (c *recordingClient) SendChatRequest(ctx context.Context, messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
	for _, m := range messages {
		if m.Role != "user" {
			continue // only user prompts carry the diff and the guide
		}
		if strings.Contains(m.Content, "commit title") {
			c.titlePrompts = append(c.titlePrompts, m.Content)
		} else {
			c.descPrompts = append(c.descPrompts, m.Content)
		}
	}
	return c.mockAPIClient.SendChatRequest(ctx, messages, tools, reasoning, disableThinking)
}

func TestGenerateCommitMessageFromStagedDiff_PromptContainsGuideAndRules(t *testing.T) {
	raw := strings.Join([]string{
		"diff --git a/main.go b/main.go",
		"--- a/main.go",
		"+++ b/main.go",
		"@@ -1 +1 @@",
		"-old",
		"+new",
		"diff --git a/package-lock.json b/package-lock.json",
		"--- a/package-lock.json",
		"+++ b/package-lock.json",
		"@@ -5,3 +5,3 @@",
		"-\"x\": 1,",
		"+\"x\": 2,",
	}, "\n")

	client := &recordingClient{mockAPIClient: newMockClientSilent()}
	client.titleResponse = &api.ChatResponse{
		Choices: []api.Choice{{Message: api.Message{Content: "feat(diff): budget staged diffs"}}},
		Usage:   api.ChatUsage{TotalTokens: 10},
	}
	client.descResponse = &api.ChatResponse{
		Choices: []api.Choice{{Message: api.Message{Content: "Applies a byte budget to diff presentation."}}},
		Usage:   api.ChatUsage{TotalTokens: 10},
	}

	result, err := GenerateCommitMessageFromStagedDiff(client, CommitMessageOptions{
		Diff:        raw,
		Branch:      "main",
		FileChanges: []CommitFileChange{{Status: "M", Path: "main.go"}, {Status: "M", Path: "package-lock.json"}},
	})
	if err != nil {
		t.Fatalf("GenerateCommitMessageFromStagedDiff: %v", err)
	}

	if len(client.titlePrompts) == 0 || len(client.descPrompts) == 0 {
		t.Fatalf("expected both prompts to be sent, got %d title / %d desc", len(client.titlePrompts), len(client.descPrompts))
	}

	for _, prompt := range client.titlePrompts {
		assertPromptHasGuide(t, prompt, "title")
	}
	for _, prompt := range client.descPrompts {
		assertPromptHasGuide(t, prompt, "desc")
	}

	// The presented diff uses the budgeted form: header for the semantic
	// file, one-line lockfile summary.
	titlePrompt := client.titlePrompts[0]
	if !strings.Contains(titlePrompt, "→ main.go (modified, source): +1 -1") {
		t.Errorf("title prompt missing context header, got:\n%s", titlePrompt)
	}
	if !strings.Contains(titlePrompt, "[Lockfile: package-lock.json") {
		t.Errorf("title prompt missing lockfile summary, got:\n%s", titlePrompt)
	}

	if !strings.HasPrefix(result.Message, "feat(diff): budget staged diffs") {
		t.Errorf("unexpected message: %q", result.Message)
	}
}

func assertPromptHasGuide(t *testing.T, prompt, label string) {
	t.Helper()
	if !strings.Contains(prompt, "Diff notation guide:") {
		t.Errorf("%s prompt missing diff notation guide", label)
	}
	if !strings.Contains(prompt, "synthesize the overall intent") {
		t.Errorf("%s prompt missing multi-file synthesis rules", label)
	}
	if !strings.Contains(prompt, "→ src/App.tsx (modified, source)") {
		t.Errorf("%s prompt missing worked example", label)
	}
}

// --- helpers ---

func keysOf(m map[string]string) []string {
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func bytes_repeat(b byte, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = b
	}
	return out
}
