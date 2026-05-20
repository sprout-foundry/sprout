//go:build !js

package webui

import (
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Pure helpers: parseNameStatusLine, normalizeGitPath, makeGitRelativePath,
// pathExistsInGitStatus, containsPath, truncateDiffOutput
// (Additional tests beyond git_helpers_new_test.go)
// ---------------------------------------------------------------------------

func TestParseNameStatusLine_RenameAndCopy(t *testing.T) {
	tests := []struct {
		line       string
		wantStatus string
		wantPath   string
		wantOK     bool
	}{
		{"R100\told.go\tnew.go", "R", "new.go", true},
		{"C100\told.go\tcopy.go", "C", "copy.go", true},
		{"R085\tA\tB\tC", "R", "C", true},
	}
	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			s, p, ok := parseNameStatusLine(tt.line)
			if ok != tt.wantOK || s != tt.wantStatus || p != tt.wantPath {
				t.Errorf("parseNameStatusLine(%q) = %q, %q, %v; want %q, %q, %v",
					tt.line, s, p, ok, tt.wantStatus, tt.wantPath, tt.wantOK)
			}
		})
	}
}

func TestParseNameStatusLine_EdgeCases(t *testing.T) {
	tests := []struct {
		name       string
		line       string
		wantStatus string
		wantPath   string
		wantOK     bool
	}{
		{"whitespace only", "  ", "", "", false},
		{"no tab", "M", "", "", false},
		{"empty status", "\tfoo.go", "", "", false},
		{"empty path after trim", "M\t  ", "", "", false},
		{"whitespace around", "  M  \t  src/main.go  ", "M", "src/main.go", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, p, ok := parseNameStatusLine(tt.line)
			if ok != tt.wantOK || s != tt.wantStatus || p != tt.wantPath {
				t.Errorf("got (%q, %q, %v), want (%q, %q, %v)",
					s, p, ok, tt.wantStatus, tt.wantPath, tt.wantOK)
			}
		})
	}
}

func TestNormalizeGitPath_TrailingDotSlash(t *testing.T) {
	if got := normalizeGitPath("foo/."); got != "foo" {
		t.Errorf("got %q, want %q", got, "foo")
	}
	if got := normalizeGitPath("./"); got != "" {
		t.Errorf("got %q, want %q", got, "")
	}
}

func TestNormalizeGitPath_EdgeCases(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{"trailing slash", "foo/", "foo"},
		{"nested parent", "a/b/../c", "a/c"},
		{"root parent", "foo/..", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeGitPath(tt.path)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMakeGitRelativePath_SameAsRoot(t *testing.T) {
	got := makeGitRelativePath("/workspace", "/workspace")
	if got != "." {
		t.Errorf("got %q, want %q", got, ".")
	}
}

func TestMakeGitRelativePath_EdgeCases(t *testing.T) {
	tests := []struct {
		name          string
		path          string
		workspaceRoot string
		want          string
	}{
		{"empty ws", "/ws/foo.go", "", "/ws/foo.go"},
		{"nested in ws", "/ws/a/b/c.go", "/ws", "a/b/c.go"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := makeGitRelativePath(tt.path, tt.workspaceRoot)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTruncateDiffOutput_ZeroMaxBytes(t *testing.T) {
	// When maxBytes=0, len("hello")=5 > 0, so it truncates: diff[:0] + suffix
	got := truncateDiffOutput("hello", 0)
	if got != "\n\n... [diff truncated]" {
		t.Errorf("got %q, want %q", got, "\n\n... [diff truncated]")
	}
}

func TestTruncateDiffOutput_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		diff     string
		maxBytes int
		want     string
	}{
		{"one byte", "hello", 1, "h\n\n... [diff truncated]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateDiffOutput(tt.diff, tt.maxBytes)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPathExistsInGitStatus_AllSections(t *testing.T) {
	status := &GitStatus{
		Staged:    []GitFile{{Path: "staged.go", Status: "M"}},
		Modified:  []GitFile{{Path: "modified.go", Status: "M"}},
		Untracked: []GitFile{{Path: "untracked.go"}},
		Deleted:   []GitFile{{Path: "deleted.go", Status: "D"}},
		Renamed:   []GitFile{{Path: "renamed.go", Status: "R"}},
	}
	assertTrue := func(path string) {
		if !pathExistsInGitStatus(path, status) {
			t.Errorf("pathExistsInGitStatus(%q) should be true", path)
		}
	}
	assertFalse := func(path string) {
		if pathExistsInGitStatus(path, status) {
			t.Errorf("pathExistsInGitStatus(%q) should be false", path)
		}
	}
	assertTrue("staged.go")
	assertTrue("modified.go")
	assertTrue("untracked.go")
	assertTrue("deleted.go")
	assertTrue("renamed.go")
	assertFalse("missing.go")
	assertFalse("")
}

func TestContainsPath_EmptyAndNil(t *testing.T) {
	if containsPath([]GitFile{}, "x.go") {
		t.Error("empty slice should be false")
	}
	if containsPath(nil, "x.go") {
		t.Error("nil should be false")
	}
}

func TestContainsPath_Normalized(t *testing.T) {
	files := []GitFile{{Path: "./src/../src/main.go", Status: "M"}}
	if !containsPath(files, "src/main.go") {
		t.Error("should match normalized path")
	}
}

// ---------------------------------------------------------------------------
// gitFixReviewJob state methods
// ---------------------------------------------------------------------------

func newGitFixReviewJob() *gitFixReviewJob {
	return &gitFixReviewJob{
		ID:        "test-id",
		SessionID: "test-sess",
		Status:    "running",
		Logs:      []string{},
		UpdatedAt: time.Now().Add(-time.Hour),
	}
}

func TestGitFixReviewJob_appendLog(t *testing.T) {
	job := newGitFixReviewJob()
	before := job.UpdatedAt
	job.appendLog("first")
	job.appendLog("second")

	if len(job.Logs) != 2 {
		t.Fatalf("expected 2 logs, got %d", len(job.Logs))
	}
	if job.Logs[0] != "first" || job.Logs[1] != "second" {
		t.Errorf("logs = %v", job.Logs)
	}
	if !job.UpdatedAt.After(before) {
		t.Error("UpdatedAt should have advanced")
	}
}

func TestGitFixReviewJob_appendLog_CapsAt2000(t *testing.T) {
	job := newGitFixReviewJob()
	for i := 0; i < 2500; i++ {
		job.appendLog("x")
	}
	if len(job.Logs) != 2000 {
		t.Errorf("expected 2000 logs after cap, got %d", len(job.Logs))
	}
}

func TestGitFixReviewJob_setCompleted(t *testing.T) {
	job := newGitFixReviewJob()
	before := job.UpdatedAt
	job.setCompleted("result")

	if job.Status != "completed" {
		t.Errorf("status = %q, want %q", job.Status, "completed")
	}
	if job.Result != "result" {
		t.Errorf("result = %q, want %q", job.Result, "result")
	}
	if !job.UpdatedAt.After(before) {
		t.Error("UpdatedAt should have advanced")
	}
}

func TestGitFixReviewJob_setError(t *testing.T) {
	job := newGitFixReviewJob()
	before := job.UpdatedAt
	job.setError("boom")

	if job.Status != "error" {
		t.Errorf("status = %q, want %q", job.Status, "error")
	}
	if job.Error != "boom" {
		t.Errorf("error = %q, want %q", job.Error, "boom")
	}
	if !job.UpdatedAt.After(before) {
		t.Error("UpdatedAt should have advanced")
	}
}

func TestGitFixReviewJob_setError_WhitespaceOnly(t *testing.T) {
	job := newGitFixReviewJob()
	job.setError("   ")
	if job.Error != "Unknown error" {
		t.Errorf("error = %q, want %q", job.Error, "Unknown error")
	}
}

func TestGitFixReviewJob_setError_Empty(t *testing.T) {
	job := newGitFixReviewJob()
	job.setError("")
	if job.Error != "Unknown error" {
		t.Errorf("error = %q, want %q", job.Error, "Unknown error")
	}
}

func TestGitFixReviewJob_snapshot(t *testing.T) {
	job := &gitFixReviewJob{
		Status:    "completed",
		Logs:      []string{"a", "b", "c"},
		Result:    "done",
		Error:     "",
		UpdatedAt: time.Now(),
	}

	tests := []struct {
		name     string
		since    int
		wantLogs int
		wantNext int
	}{
		{"all", 0, 3, 3},
		{"skip 1", 1, 2, 3},
		{"skip 2", 2, 1, 3},
		{"skip all", 3, 0, 3},
		{"past end", 10, 0, 3},
		{"negative", -5, 3, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st, logs, next, res, err := job.snapshot(tt.since)
			if st != "completed" {
				t.Errorf("status = %q", st)
			}
			if len(logs) != tt.wantLogs {
				t.Errorf("logs len = %d, want %d", len(logs), tt.wantLogs)
			}
			if next != tt.wantNext {
				t.Errorf("next = %d, want %d", next, tt.wantNext)
			}
			if res != "done" {
				t.Errorf("result = %q", res)
			}
			if err != "" {
				t.Errorf("err = %q", err)
			}
		})
	}
}

func TestGitFixReviewJob_snapshot_LogsContent(t *testing.T) {
	job := &gitFixReviewJob{
		Status: "running",
		Logs:   []string{"1", "2", "3"},
	}
	_, logs, _, _, _ := job.snapshot(1)
	if len(logs) != 2 || logs[0] != "2" || logs[1] != "3" {
		t.Errorf("snapshot(1) = %v, want [2, 3]", logs)
	}
}

func TestGitFixReviewJob_appendStreamText(t *testing.T) {
	job := newGitFixReviewJob()

	job.appendStreamText("hello\n")
	if len(job.Logs) != 1 || job.Logs[0] != "hello" {
		t.Errorf("logs = %v", job.Logs)
	}

	job.appendStreamText("a\nb\nc")
	if len(job.Logs) != 3 {
		t.Errorf("logs = %v (len %d), want 3", job.Logs, len(job.Logs))
	}
	if job.Logs[1] != "a" || job.Logs[2] != "b" {
		t.Errorf("logs = %v", job.Logs)
	}
}

func TestGitFixReviewJob_appendStreamText_TrimsLines(t *testing.T) {
	job := newGitFixReviewJob()
	job.appendStreamText("  hello  \n")
	if len(job.Logs) != 1 || job.Logs[0] != "hello" {
		t.Errorf("logs = %v, want [hello]", job.Logs)
	}
}

func TestGitFixReviewJob_appendStreamText_EmptyLinesSkipped(t *testing.T) {
	job := newGitFixReviewJob()
	job.appendStreamText("\n\n\n")
	if len(job.Logs) != 0 {
		t.Errorf("empty lines should be skipped, got %v", job.Logs)
	}
}

func TestGitFixReviewJob_appendStreamText_CapsAt2000(t *testing.T) {
	job := newGitFixReviewJob()
	for i := 0; i < 2100; i++ {
		job.appendStreamText("x\n")
	}
	if len(job.Logs) > 2000 {
		t.Errorf("logs should be capped at 2000, got %d", len(job.Logs))
	}
}

func TestGitFixReviewJob_flushStreamBuffer(t *testing.T) {
	job := newGitFixReviewJob()
	job.streamBuf.WriteString("pending")
	job.flushStreamBuffer()
	if len(job.Logs) != 1 || job.Logs[0] != "pending" {
		t.Errorf("logs = %v, want [pending]", job.Logs)
	}
}

func TestGitFixReviewJob_flushStreamBuffer_Empty(t *testing.T) {
	job := newGitFixReviewJob()
	job.flushStreamBuffer()
	if len(job.Logs) != 0 {
		t.Errorf("empty flush should not add logs, got %v", job.Logs)
	}
}

func TestGitFixReviewJob_flushStreamBuffer_WhitespaceOnly(t *testing.T) {
	job := newGitFixReviewJob()
	job.streamBuf.WriteString("  ")
	job.flushStreamBuffer()
	if len(job.Logs) != 0 {
		t.Errorf("whitespace flush should not add logs, got %v", job.Logs)
	}
}
