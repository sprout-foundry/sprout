package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// NOTE: splitLines() now preserves trailing empty elements, so
// ApplyHunks (which joins with "\n") will correctly preserve trailing
// newlines.  Test inputs that end with "\n" must have that expected in
// the output as well.

// ---------------------------------------------------------------------------
// TestSplitIntoHunks_*
// ---------------------------------------------------------------------------

func TestSplitIntoHunks_NoChanges(t *testing.T) {
	original := "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}"

	hunks := SplitIntoHunks(original, original)
	assert.Empty(t, hunks, "identical content should produce no hunks")
}

func TestSplitIntoHunks_SingleLineAdd(t *testing.T) {
	original := "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}"
	proposed := "package main\n\nimport \"fmt\"\n\n// New comment added\nfunc main() {\n\tfmt.Println(\"hello\")\n}"

	hunks := SplitIntoHunks(original, proposed)
	require.Len(t, hunks, 1, "should produce exactly one hunk")

	h := hunks[0]
	addCount := countDiffLines(h, DiffLineAdd)
	require.Equal(t, 1, addCount, "should have exactly one added line")

	added := getDiffLineContent(h, DiffLineAdd)
	require.Contains(t, added, "// New comment added")
}

func TestSplitIntoHunks_SingleLineRemove(t *testing.T) {
	original := "package main\n\nimport \"fmt\"\n\n// Old comment\nfunc main() {\n\tfmt.Println(\"hello\")\n}"
	proposed := "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}"

	hunks := SplitIntoHunks(original, proposed)
	require.Len(t, hunks, 1, "should produce exactly one hunk")

	h := hunks[0]
	removeCount := countDiffLines(h, DiffLineRemove)
	require.Equal(t, 1, removeCount, "should have exactly one removed line")

	removed := getDiffLineContent(h, DiffLineRemove)
	require.Contains(t, removed, "// Old comment")
}

func TestSplitIntoHunks_SingleLineReplace(t *testing.T) {
	original := "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}"
	proposed := "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"world\")\n}"

	hunks := SplitIntoHunks(original, proposed)
	require.Len(t, hunks, 1, "should produce exactly one hunk")

	h := hunks[0]
	removeCount := countDiffLines(h, DiffLineRemove)
	addCount := countDiffLines(h, DiffLineAdd)
	require.Equal(t, 1, removeCount, "should have one removed line")
	require.Equal(t, 1, addCount, "should have one added line")

	// splitLines preserves tabs, so content includes the leading tab — check for substring
	removed := getDiffLineContent(h, DiffLineRemove)
	require.NotEmpty(t, removed)
	found := false
	for _, s := range removed {
		if strings.Contains(s, "hello") {
			found = true
			break
		}
	}
	require.True(t, found, "removed line should contain 'hello', got %v", removed)

	added := getDiffLineContent(h, DiffLineAdd)
	require.NotEmpty(t, added)
	found = false
	for _, s := range added {
		if strings.Contains(s, "world") {
			found = true
			break
		}
	}
	require.True(t, found, "added line should contain 'world', got %v", added)
}

func TestSplitIntoHunks_MultipleHunks(t *testing.T) {
	original := strings.Join([]string{
		"package config",
		"",
		"import \"os\"",
		"",
		"type Config struct {",
		"	Host string",
		"	Port int",
		"}",
		"",
		"func New() *Config {",
		"	return &Config{}",
		"}",
		"",
		"func (c *Config) SetHost(h string) {",
		"	c.Host = h",
		"}",
		"",
		"func (c *Config) SetPort(p int) {",
		"	c.Port = p",
		"}",
		"",
		"func (c *Config) Load() error {",
		"	// load from env",
		"	c.Host = os.Getenv(\"APP_HOST\")",
		"	return nil",
		"}",
	}, "\n")

	proposed := strings.Join([]string{
		"package config",
		"",
		"import \"os\"",
		"",
		"type Config struct {",
		"	Host string",
		"	Port int",
		"}",
		"",
		"func New() *Config {",
		"	return &Config{Host: \"localhost\", Port: 8080}",
		"}",
		"",
		"func (c *Config) SetHost(h string) {",
		"	c.Host = h",
		"}",
		"",
		"func (c *Config) SetPort(p int) {",
		"	c.Port = p",
		"}",
		"",
		"func (c *Config) Load() error {",
		"	// load from env vars with fallback",
		"	c.Host = os.Getenv(\"APP_HOST\")",
		"	if c.Host == \"\" {",
		"		c.Host = \"localhost\"",
		"	}",
		"	return nil",
		"}",
	}, "\n")

	hunks := SplitIntoHunks(original, proposed)
	require.GreaterOrEqual(t, len(hunks), 2, "should produce at least 2 hunks for distant changes")
}

func TestSplitIntoHunks_HunkIDs(t *testing.T) {
	original := strings.Join([]string{
		"line-1", "line-2", "line-3", "line-4", "line-5",
		"line-6", "line-7", "line-8", "line-9", "line-10",
		"line-11", "line-12",
	}, "\n")
	proposed := strings.Join([]string{
		"changed-1", "line-2", "line-3", "line-4", "line-5",
		"line-6", "line-7", "line-8", "line-9", "line-10",
		"changed-11", "line-12",
	}, "\n")

	hunks := SplitIntoHunks(original, proposed)
	require.GreaterOrEqual(t, len(hunks), 2, "need at least 2 hunks for ID verification")

	for i, h := range hunks {
		expectedID := "hunk-" + string(rune('0'+i))
		assert.Equal(t, expectedID, h.ID, "hunk index %d should have ID %q", i, expectedID)
	}
}

func TestSplitIntoHunks_HunkLinePositions(t *testing.T) {
	lines := []string{
		"1: first",
		"2: second",
		"3: third",
		"4: fourth",
		"5: original",
		"6: sixth",
		"7: seventh",
		"8: eighth",
		"9: ninth",
		"10: tenth",
		"11: eleventh",
		"12: twelfth",
		"13: thirteenth",
		"14: fourteenth",
		"15: fifteenth",
	}
	original := strings.Join(lines, "\n")

	lines[4] = "5: modified"
	proposed := strings.Join(lines, "\n")

	hunks := SplitIntoHunks(original, proposed)
	require.Len(t, hunks, 1, "should produce exactly one hunk")

	h := hunks[0]

	assert.GreaterOrEqual(t, h.OldStart, 1, "OldStart should be >= 1 (1-based)")
	assert.Greater(t, h.OldLines, 0, "OldLines should be > 0")
	assert.Greater(t, h.NewLines, 0, "NewLines should be > 0")

	// OldStart and NewStart should be equal for a simple replacement
	assert.Equal(t, h.OldStart, h.NewStart, "OldStart and NewStart should match for a simple replacement")
	// OldLines and NewLines should be equal for a 1-for-1 replacement
	assert.Equal(t, h.OldLines, h.NewLines, "OldLines and NewLines should match for a 1-for-1 replacement")
}

// ---------------------------------------------------------------------------
// TestApplyHunks_*
// ---------------------------------------------------------------------------

func TestApplyHunks_AcceptAll(t *testing.T) {
	original := "package service\n\nimport \"context\"\n\ntype Service struct {\n\tName string\n}\n\nfunc (s *Service) Handle(ctx context.Context) error {\n\treturn nil\n}"
	proposed := "package service\n\nimport (\n\t\"context\"\n\t\"fmt\"\n)\n\ntype Service struct {\n\tName string\n}\n\nfunc (s *Service) Handle(ctx context.Context) error {\n\tfmt.Println(\"handling\")\n\treturn nil\n}"

	hunks := SplitIntoHunks(original, proposed)
	allIDs := hunkIDs(hunks)

	result := ApplyHunks(original, hunks, allIDs)

	assert.Equal(t, proposed, result, "accepting all hunks should reproduce the proposed content")
}

func TestApplyHunks_RejectAll(t *testing.T) {
	original := "package service\n\nimport \"context\"\n\ntype Service struct {\n\tName string\n}"
	proposed := "package service\n\nimport (\n\t\"context\"\n\t\"fmt\"\n)\n\ntype Service struct {\n\tName string\n}"

	hunks := SplitIntoHunks(original, proposed)

	result := ApplyHunks(original, hunks, nil)

	assert.Equal(t, original, result, "rejecting all hunks should return original unchanged")
}

func TestApplyHunks_PartialAccept(t *testing.T) {
	// Space changes >6 lines apart so difflib produces separate hunks.
	original := strings.Join([]string{
		"package main",                        // 0
		"",                                    // 1
		"import \"fmt\"",                      // 2
		"",                                    // 3
		"const Version = \"1.0.0\"",           // 4  <-- change 1
		"",                                    // 5
		"type App struct {",                   // 6
		"	Name string",                       // 7
		"}",                                   // 8
		"",                                    // 9
		"func (a *App) Init() {",              // 10
		"	fmt.Println(\"init\")",             // 11
		"}",                                   // 12
		"",                                    // 13
		"func (a *App) Setup() {",             // 14
		"	fmt.Println(\"setup\")",            // 15
		"}",                                   // 16
		"",                                    // 17
		"func (a *App) Config() {",            // 18
		"	fmt.Println(\"config\")",           // 19
		"}",                                   // 20
		"",                                    // 21
		"func (a *App) Validate() {",          // 22
		"	fmt.Println(\"validate\")",         // 23
		"}",                                   // 24
		"",                                    // 25
		"func (a *App) New() *App {",          // 26  <-- change 2
		"	return &App{}",                     // 27
		"}",                                   // 28
		"",                                    // 29
		"func (a *App) Run() {",               // 30
		"	fmt.Println(a.Name)",               // 31
		"}",                                   // 32
		"",                                    // 33
		"func (a *App) Stop() {",              // 34
		"	fmt.Println(\"stopped\")",          // 35
		"}",                                   // 36
		"",                                    // 37
		"func (a *App) Cleanup() {",           // 38
		"	fmt.Println(\"cleanup\")",          // 39
		"}",                                   // 40
		"",                                    // 41
		"func (a *App) Shutdown() {",          // 42
		"	fmt.Println(\"shutdown\")",         // 43
		"}",                                   // 44
		"",                                    // 45
		"func main() {",                       // 46  <-- change 3
		"	app := &App{}",                     // 47
		"	app.Run()",                         // 48
		"}",                                   // 49
	}, "\n")

	proposed := strings.Join([]string{
		"package main",                        // 0
		"",                                    // 1
		"import \"fmt\"",                      // 2
		"",                                    // 3
		"const Version = \"2.0.0\"",           // 4  <-- CHANGED
		"",                                    // 5
		"type App struct {",                   // 6
		"	Name string",                       // 7
		"}",                                   // 8
		"",                                    // 9
		"func (a *App) Init() {",              // 10
		"	fmt.Println(\"init\")",             // 11
		"}",                                   // 12
		"",                                    // 13
		"func (a *App) Setup() {",             // 14
		"	fmt.Println(\"setup\")",            // 15
		"}",                                   // 16
		"",                                    // 17
		"func (a *App) Config() {",            // 18
		"	fmt.Println(\"config\")",           // 19
		"}",                                   // 20
		"",                                    // 21
		"func (a *App) Validate() {",          // 22
		"	fmt.Println(\"validate\")",         // 23
		"}",                                   // 24
		"",                                    // 25
		"func (a *App) New() *App {",          // 26  <-- CHANGED
		"	return &App{Name: \"default\"}",    // 27  <-- CHANGED
		"}",                                   // 28
		"",                                    // 29
		"func (a *App) Run() {",               // 30
		"	fmt.Println(a.Name)",               // 31
		"}",                                   // 32
		"",                                    // 33
		"func (a *App) Stop() {",              // 34
		"	fmt.Println(\"stopped\")",          // 35
		"}",                                   // 36
		"",                                    // 37
		"func (a *App) Cleanup() {",           // 38
		"	fmt.Println(\"cleanup\")",          // 39
		"}",                                   // 40
		"",                                    // 41
		"func (a *App) Shutdown() {",          // 42
		"	fmt.Println(\"shutdown\")",         // 43
		"}",                                   // 44
		"",                                    // 45
		"func main() {",                       // 46  <-- CHANGED
		"	app := &App{Name: \"main-app\"}",   // 47  <-- CHANGED
		"	app.Run()",                         // 48
		"}",                                   // 49
	}, "\n")

	hunks := SplitIntoHunks(original, proposed)
	require.GreaterOrEqual(t, len(hunks), 3, "need at least 3 hunks for partial accept, got %d", len(hunks))

	// Accept only hunk-0 (first change: Version = "2.0.0")
	result := ApplyHunks(original, hunks, []string{hunks[0].ID})

	// First change applied
	assert.Contains(t, result, "\"2.0.0\"", "first hunk's version change should be applied")

	// Later changes NOT applied
	assert.NotContains(t, result, "\"default\"", "second hunk should NOT be applied")
	assert.NotContains(t, "\"main-app\"", "third hunk should NOT be applied")

	// Original versions of rejected hunks should still be present
	assert.Contains(t, result, "&App{}", "original empty App should still be present")
}

func TestApplyHunks_EmptyOriginal(t *testing.T) {
	original := ""
	proposed := "package main\n\nfunc main() {}"

	hunks := SplitIntoHunks(original, proposed)
	require.NotEmpty(t, hunks, "should have hunks when adding to empty original")

	allIDs := hunkIDs(hunks)
	result := ApplyHunks(original, hunks, allIDs)

	assert.Equal(t, proposed, result, "applying all hunks to empty original should produce proposed content")
}

func TestApplyHunks_NoHunks(t *testing.T) {
	original := "package main\n\nfunc main() {}"
	result := ApplyHunks(original, nil, nil)

	assert.Equal(t, original, result, "no hunks should return original unchanged")
}

func TestApplyHunks_Insertion(t *testing.T) {
	original := "line1\nline2\nline3"
	proposed := "line1\nINSERTED\nline2\nline3"

	hunks := SplitIntoHunks(original, proposed)
	require.NotEmpty(t, hunks)

	result := ApplyHunks(original, hunks, hunkIDs(hunks))
	assert.Equal(t, proposed, result)
}

func TestApplyHunks_Deletion(t *testing.T) {
	original := "line1\nline2\nline3"
	proposed := "line1\nline3"

	hunks := SplitIntoHunks(original, proposed)
	require.NotEmpty(t, hunks)

	result := ApplyHunks(original, hunks, hunkIDs(hunks))
	assert.Equal(t, proposed, result)
}

func TestApplyHunks_RejectOnlyMiddle(t *testing.T) {
	original := strings.Join([]string{
		"line-01", "line-02", "line-03", "line-04", "line-05",
		"line-06", "line-07", "line-08", "line-09", "line-10",
		"line-11", "line-12", "line-13", "line-14", "line-15",
		"line-16", "line-17", "line-18", "line-19", "line-20",
	}, "\n")

	proposed := strings.Join([]string{
		"line-01", "CHANGED-A", "line-03", "line-04", "line-05",
		"line-06", "line-07", "line-08", "line-09", "line-10",
		"line-11", "line-12", "line-13", "line-14", "CHANGED-B",
		"line-16", "line-17", "line-18", "line-19", "line-20",
	}, "\n")

	hunks := SplitIntoHunks(original, proposed)
	require.Len(t, hunks, 2, "should produce exactly 2 hunks for two distant changes")

	// Accept only hunk-0 (CHANGED-A), reject hunk-1 (CHANGED-B)
	result := ApplyHunks(original, hunks, []string{hunks[0].ID})

	assert.Contains(t, result, "CHANGED-A", "first hunk should be applied")
	assert.NotContains(t, result, "CHANGED-B", "second hunk should be rejected")
	assert.Contains(t, result, "line-15", "original line-15 should be present (rejected)")
	assert.NotContains(t, result, "line-02", "original line-02 should be replaced (accepted)")
}

// ---------------------------------------------------------------------------
// TestGenerateUnifiedDiff_*
// ---------------------------------------------------------------------------

func TestGenerateUnifiedDiff_NoChanges(t *testing.T) {
	original := "package main\n\nfunc main() {}"

	diff, err := GenerateUnifiedDiff("main.go", original, original)
	require.NoError(t, err)

	assert.Empty(t, diff, "identical content should produce an empty diff string")
}

func TestGenerateUnifiedDiff_Changes(t *testing.T) {
	original := "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}"
	proposed := "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"world\")\n}"

	diff, err := GenerateUnifiedDiff("main.go", original, proposed)
	require.NoError(t, err)

	assert.NotEmpty(t, diff, "should produce non-empty diff for changed content")
	assert.Contains(t, diff, "---", "should contain '---' header")
	assert.Contains(t, diff, "+++", "should contain '+++' header")
	assert.Contains(t, diff, "hello", "should reference removed content")
	assert.Contains(t, diff, "world", "should reference added content")
	assert.Contains(t, diff, "main.go", "should reference the file path")
}

// ---------------------------------------------------------------------------
// TestRequestEditApproval_*
// ---------------------------------------------------------------------------

func TestRequestEditApproval_NoChanges(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	original := "package main\n\nfunc main() {}"

	proposal := EditProposal{
		Path:     "main.go",
		Original: original,
		Proposed: original,
	}

	applied, summary, err := agent.RequestEditApproval(context.Background(), proposal)
	require.NoError(t, err)
	assert.Equal(t, original, applied, "should return original content unchanged")
	assert.Contains(t, summary, "no changes", "summary should mention no changes")
	assert.Contains(t, summary, "main.go", "summary should include the file path")
}

func TestRequestEditApproval_ApproveAll(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	original := "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}"
	proposed := "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"world\")\n}"

	proposal := EditProposal{
		Path:     "main.go",
		Original: original,
		Proposed: proposed,
	}

	applied, summary, err := agent.RequestEditApproval(context.Background(), proposal)
	require.NoError(t, err)
	assert.Equal(t, proposed, applied, "placeholder broker approves all, should return proposed content")
	assert.Contains(t, summary, "applied 1/1 hunks to main.go", "summary should show full acceptance")
}

func TestRequestEditApproval_PrepHunks(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	original := "line1\nline2\nline3"
	proposed := "line1\nMODIFIED\nline3"
	hunks := SplitIntoHunks(original, proposed)
	require.NotEmpty(t, hunks, "should have hunks for this diff")

	proposal := EditProposal{
		Path:     "test.txt",
		Original: original,
		Proposed: proposed,
		Hunks:    hunks,
	}

	applied, _, err := agent.RequestEditApproval(context.Background(), proposal)
	require.NoError(t, err)
	assert.Equal(t, proposed, applied, "pre-computed hunks should be used directly")
}

func TestRequestEditApproval_ContextCancelled(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	proposal := EditProposal{
		Path:     "main.go",
		Original: "original",
		Proposed: "proposed",
	}

	_, _, err := agent.RequestEditApproval(ctx, proposal)
	assert.Error(t, err, "should return error on cancelled context")
}

func TestRequestEditApproval_ProposalWithoutHunks(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	original := "line-a\nline-b\nline-c"
	proposed := "line-a\nline-B-modified\nline-c"

	proposal := EditProposal{
		Path:     "data.txt",
		Original: original,
		Proposed: proposed,
		Hunks:    nil,
	}

	applied, summary, err := agent.RequestEditApproval(context.Background(), proposal)
	require.NoError(t, err)
	assert.Equal(t, proposed, applied, "should auto-compute hunks and apply all")
	assert.Contains(t, summary, "applied", "summary should mention applied hunks")
}

// ---------------------------------------------------------------------------
// TestSplitLines_*
// ---------------------------------------------------------------------------

func TestSplitLines_Normal(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "single line without newline",
			input: "hello",
			want:  []string{"hello"},
		},
		{
			name:  "multiple lines",
			input: "a\nb\nc",
			want:  []string{"a", "b", "c"},
		},
		{
			name:  "trailing newline is preserved",
			input: "a\nb\nc\n",
			want:  []string{"a", "b", "c", ""},
		},
		{
			name:  "realistic Go source code",
			input: "package main\n\nfunc main() {}",
			want:  []string{"package main", "", "func main() {}"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitLines(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSplitLines_Empty(t *testing.T) {
	result := splitLines("")
	assert.Equal(t, []string{""}, result, "empty string should return a single empty element")
}

// ---------------------------------------------------------------------------
// TestRejectedHunkList_*
// ---------------------------------------------------------------------------

func TestRejectedHunkList_NoneRejected(t *testing.T) {
	hunks := []Hunk{
		{ID: "hunk-0", OldStart: 1, OldLines: 3},
		{ID: "hunk-1", OldStart: 10, OldLines: 5},
	}
	accepted := []string{"hunk-0", "hunk-1"}

	list := rejectedHunkList(hunks, accepted)
	assert.Equal(t, "none", list, "should return 'none' when all hunks are accepted")
}

func TestRejectedHunkList_AllRejected(t *testing.T) {
	hunks := []Hunk{
		{ID: "hunk-0", OldStart: 1, OldLines: 3},
		{ID: "hunk-1", OldStart: 10, OldLines: 5},
	}

	list := rejectedHunkList(hunks, nil)
	assert.Contains(t, list, "hunk-0", "should include hunk-0 in rejected list")
	assert.Contains(t, list, "hunk-1", "should include hunk-1 in rejected list")
	assert.Contains(t, list, "1-3", "should include line range for hunk-0")
	assert.Contains(t, list, "10-14", "should include line range for hunk-1")
}

func TestRejectedHunkList_Partial(t *testing.T) {
	hunks := []Hunk{
		{ID: "hunk-0", OldStart: 1, OldLines: 3},
		{ID: "hunk-1", OldStart: 10, OldLines: 5},
		{ID: "hunk-2", OldStart: 20, OldLines: 2},
	}
	accepted := []string{"hunk-0", "hunk-2"}

	list := rejectedHunkList(hunks, accepted)
	assert.Contains(t, list, "hunk-1", "should include the rejected hunk-1")
	assert.NotContains(t, list, "hunk-0", "should NOT include accepted hunk-0")
	assert.NotContains(t, list, "hunk-2", "should NOT include accepted hunk-2")
}

// ---------------------------------------------------------------------------
// TestHunkIDs
// ---------------------------------------------------------------------------

func TestHunkIDs(t *testing.T) {
	hunks := []Hunk{
		{ID: "hunk-0"},
		{ID: "hunk-1"},
		{ID: "hunk-2"},
	}
	ids := hunkIDs(hunks)
	assert.Equal(t, []string{"hunk-0", "hunk-1", "hunk-2"}, ids)
}

func TestHunkIDs_Empty(t *testing.T) {
	ids := hunkIDs(nil)
	assert.Empty(t, ids, "nil hunks should produce empty IDs")
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// countDiffLines counts DiffLines of a given type within a hunk.
func countDiffLines(h Hunk, typ DiffLineType) int {
	count := 0
	for _, dl := range h.Lines {
		if dl.Type == typ {
			count++
		}
	}
	return count
}

// getDiffLineContent returns all content strings of a given type in a hunk.
func getDiffLineContent(h Hunk, typ DiffLineType) []string {
	var contents []string
	for _, dl := range h.Lines {
		if dl.Type == typ {
			contents = append(contents, dl.Content)
		}
	}
	return contents
}
