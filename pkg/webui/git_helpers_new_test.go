//go:build !js

package webui

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseNameStatusLine(t *testing.T) {
	tests := []struct {
		name       string
		line       string
		wantStatus string
		wantPath   string
		wantOK     bool
	}{
		{
			name: "modified file", line: "M\tfile.go",
			wantStatus: "M", wantPath: "file.go", wantOK: true,
		},
		{
			name: "added file with path", line: "A\tsrc/main.go",
			wantStatus: "A", wantPath: "src/main.go", wantOK: true,
		},
		{
			name: "deleted file", line: "D\tdeleted.go",
			wantStatus: "D", wantPath: "deleted.go", wantOK: true,
		},
		{
			name: "rename uses last path", line: "R100\told.go\tnew.go",
			wantStatus: "R", wantPath: "new.go", wantOK: true,
		},
		{
			name: "empty line", line: "",
			wantStatus: "", wantPath: "", wantOK: false,
		},
		{
			name: "no tab separator", line: "no-tab",
			wantStatus: "", wantPath: "", wantOK: false,
		},
		{
			name: "tab only no file", line: "\t",
			wantStatus: "", wantPath: "", wantOK: false,
		},
		{
			name: "no status code", line: "\tnopath",
			wantStatus: "", wantPath: "", wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, path, ok := parseNameStatusLine(tt.line)
			if ok != tt.wantOK {
				t.Errorf("ok = %v, want %v", ok, tt.wantOK)
			}
			if status != tt.wantStatus {
				t.Errorf("status = %q, want %q", status, tt.wantStatus)
			}
			if path != tt.wantPath {
				t.Errorf("path = %q, want %q", path, tt.wantPath)
			}
		})
	}
}

func TestNormalizeGitPath(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{"simple path", "foo/bar/baz.go", "foo/bar/baz.go"},
		{"cleaned path", "./foo/../bar", "bar"},
		{"dot only", ".", ""},
		{"empty", "", ""},
		{"backslash to forward (Linux: unchanged)", "foo\\bar", "foo\\bar"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeGitPath(tt.path)
			if got != tt.want {
				t.Errorf("normalizeGitPath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestMakeGitRelativePath(t *testing.T) {
	tests := []struct {
		name          string
		path          string
		workspaceRoot string
		want          string
	}{
		{
			name: "relative stays unchanged",
			path: "foo.go", workspaceRoot: "/workspace",
			want: "foo.go",
		},
		{
			name: "absolute in workspace",
			path: "/workspace/src/foo.go", workspaceRoot: "/workspace",
			want: "src/foo.go",
		},
		{
			name: "absolute outside workspace",
			path: "/other/foo.go", workspaceRoot: "/workspace",
			want: "/other/foo.go",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := makeGitRelativePath(tt.path, tt.workspaceRoot)
			if got != tt.want {
				t.Errorf("makeGitRelativePath(%q, %q) = %q, want %q", tt.path, tt.workspaceRoot, got, tt.want)
			}
		})
	}
}

func TestTruncateDiffOutput(t *testing.T) {
	tests := []struct {
		name     string
		diff     string
		maxBytes int
		want     string
	}{
		{
			name: "short string no truncation",
			diff: "hello", maxBytes: 100,
			want: "hello",
		},
		{
			name: "exactly max bytes no truncation",
			diff: "12345", maxBytes: 5,
			want: "12345",
		},
		{
			name: "over max bytes truncated",
			diff: "12345678901234567890", maxBytes: 10,
			want: "1234567890\n\n... [diff truncated]",
		},
		{
			name: "empty string",
			diff: "", maxBytes: 10,
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateDiffOutput(tt.diff, tt.maxBytes)
			if got != tt.want {
				t.Errorf("truncateDiffOutput = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestContainsPath(t *testing.T) {
	files := []GitFile{
		{Path: "src/main.go", Status: "M"},
		{Path: "./src/../src/helper.go", Status: "A"}, // messy path
	}

	tests := []struct {
		name  string
		files []GitFile
		path  string
		want  bool
	}{
		{"matching path", files, "src/main.go", true},
		{"no matching path", files, "src/missing.go", false},
		{"normalized file path matches", files, "src/helper.go", true}, // file has messy path, query is clean
		{"empty list", nil, "anything.go", false},
		{"empty slice", []GitFile{}, "anything.go", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := containsPath(tt.files, tt.path)
			if got != tt.want {
				t.Errorf("containsPath(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestPathExistsInGitStatus(t *testing.T) {
	status := &GitStatus{
		Modified:  []GitFile{{Path: "src/main.go", Status: "M"}},
		Untracked: []GitFile{{Path: "src/new.go", Status: "?"}},
	}

	tests := []struct {
		name string
		st   *GitStatus
		path string
		want bool
	}{
		{"nil status", nil, "anything.go", false},
		{"matching file", status, "src/main.go", true},
		{"no matching file", status, "src/missing.go", false},
		{"untracked file", status, "src/new.go", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pathExistsInGitStatus(tt.path, tt.st)
			if got != tt.want {
				t.Errorf("pathExistsInGitStatus(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestGenerateCryptoID(t *testing.T) {
	prefix := "test"

	// Check correct prefix
	id := generateCryptoID(prefix)
	if !strings.HasPrefix(id, prefix+"-") {
		t.Errorf("generateCryptoID(%q) = %q, want prefix %q", prefix, id, prefix)
	}

	// Generates unique IDs
	id2 := generateCryptoID(prefix)
	if id == id2 {
		t.Errorf("generateCryptoID produced duplicate IDs: %q", id)
	}

	// Correct length: prefix + "-" + 24 hex chars (from 12 bytes)
	wantLen := len(prefix) + 1 + 24
	if len(id) != wantLen {
		t.Errorf("generateCryptoID length = %d, want %d (got %q)", len(id), wantLen, id)
	}
}

func TestGitStatus_JSONSerializationRoundTrip(t *testing.T) {
	status := &GitStatus{
		Branch:    "main",
		Ahead:     1,
		Behind:    0,
		Staged:    []GitFile{{Path: "a.go", Status: "M", Staged: true}},
		Modified:  []GitFile{{Path: "b.go", Status: "M"}},
		Untracked: []GitFile{{Path: "c.go", Status: "?"}},
		Deleted:   []GitFile{{Path: "d.go", Status: "D"}},
		Renamed:   []GitFile{{Path: "new.go", Status: "R"}},
		Truncated: false,
	}

	data, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var got GitStatus
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if got.Branch != "main" {
		t.Errorf("Branch = %q, want %q", got.Branch, "main")
	}
	if got.Ahead != 1 {
		t.Errorf("Ahead = %d, want 1", got.Ahead)
	}
	if len(got.Staged) != 1 || got.Staged[0].Path != "a.go" {
		t.Errorf("Staged = %+v, want [{Path: a.go, Staged: true}]", got.Staged)
	}
	if len(got.Modified) != 1 || got.Modified[0].Path != "b.go" {
		t.Errorf("Modified = %+v", got.Modified)
	}
}
