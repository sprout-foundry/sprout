package webcontent

import (
	"testing"
)

// ---------------------------------------------------------------------------
// isGitHubURL
// ---------------------------------------------------------------------------

func TestIsGitHubURL(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"standard repo URL", "https://github.com/owner/repo", true},
		{"blob URL", "https://github.com/owner/repo/blob/main/file.go", true},
		{"www subdomain", "https://www.github.com/owner/repo", true},
		{"issue URL", "https://github.com/owner/repo/issues/42", true},
		{"pull URL", "https://github.com/owner/repo/pull/7", true},

		// NOT GitHub
		{"raw githubusercontent", "https://raw.githubusercontent.com/owner/repo/main/file.go", false},
		{"api github", "https://api.github.com/repos/owner/repo", false},
		{"gist", "https://gist.github.com/abc123", false},
		{"random site", "https://gitlab.com/owner/repo", false},
		{"empty string", "", false},
		{"not a URL", "not-a-url", false},
		{"localhost", "http://localhost:8080", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isGitHubURL(tt.input)
			if got != tt.expected {
				t.Errorf("isGitHubURL(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// stripLineAnchor
// ---------------------------------------------------------------------------

func TestStripLineAnchor(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"single line", "https://github.com/o/r/blob/main/f.go#L42", "https://github.com/o/r/blob/main/f.go"},
		{"line range", "https://github.com/o/r/blob/main/f.go#L10-L25", "https://github.com/o/r/blob/main/f.go"},
		{"no anchor", "https://github.com/o/r/blob/main/f.go", "https://github.com/o/r/blob/main/f.go"},
		{"non-line anchor kept", "https://github.com/o/r/blob/main/f.go#readme", "https://github.com/o/r/blob/main/f.go#readme"},
		{"L with no digits kept", "https://github.com/o/r/blob/main/f.go#L-abc", "https://github.com/o/r/blob/main/f.go#L-abc"},
		{"query preserved", "https://github.com/o/r/blob/main/f.go?foo=bar#L42", "https://github.com/o/r/blob/main/f.go?foo=bar"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripLineAnchor(tt.input)
			if got != tt.expected {
				t.Errorf("stripLineAnchor(%q)\n  got:  %q\n  want: %q", tt.input, got, tt.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// rewriteGitHubBlobToRaw
// ---------------------------------------------------------------------------

func TestRewriteGitHubBlobToRaw(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			"basic blob to raw",
			"https://github.com/owner/repo/blob/main/README.md",
			"https://raw.githubusercontent.com/owner/repo/main/README.md",
		},
		{
			"deep path",
			"https://github.com/owner/repo/blob/main/pkg/webcontent/fetcher.go",
			"https://raw.githubusercontent.com/owner/repo/main/pkg/webcontent/fetcher.go",
		},
		{
			"with line anchor",
			"https://github.com/owner/repo/blob/main/file.go#L42-L56",
			"https://raw.githubusercontent.com/owner/repo/main/file.go",
		},
		{
			"single line anchor",
			"https://github.com/owner/repo/blob/main/file.go#L10",
			"https://raw.githubusercontent.com/owner/repo/main/file.go",
		},
		{
			"branch with slashes",
			"https://github.com/owner/repo/blob/feature/foo/file.go",
			"https://raw.githubusercontent.com/owner/repo/feature/foo/file.go",
		},
		{
			"SHA ref",
			"https://github.com/owner/repo/blob/abc123def/pkg/main.go",
			"https://raw.githubusercontent.com/owner/repo/abc123def/pkg/main.go",
		},
		{
			"with query params",
			"https://github.com/owner/repo/blob/main/f.go?raw=true",
			"https://raw.githubusercontent.com/owner/repo/main/f.go?raw=true",
		},
		{
			"www subdomain",
			"https://www.github.com/owner/repo/blob/main/f.go",
			"https://raw.githubusercontent.com/owner/repo/main/f.go",
		},
		{
			"query and anchor",
			"https://github.com/owner/repo/blob/main/f.go?token=abc#L5-L20",
			"https://raw.githubusercontent.com/owner/repo/main/f.go?token=abc",
		},

		// --- should return empty ---
		{
			"tree URL returns empty",
			"https://github.com/owner/repo/tree/main/dir",
			"",
		},
		{
			"issue URL returns empty",
			"https://github.com/owner/repo/issues/42",
			"",
		},
		{
			"blob without ref returns empty",
			"https://github.com/owner/repo/blob",
			"",
		},
		{
			"blob without path returns empty",
			"https://github.com/owner/repo/blob/main",
			"",
		},
		{
			"non-github returns empty",
			"https://gitlab.com/owner/repo/blob/main/f.go",
			"",
		},
		{
			"raw URL returns empty",
			"https://raw.githubusercontent.com/owner/repo/main/f.go",
			"",
		},
		{
			"empty string returns empty",
			"",
			"",
		},
		{
			"just repo returns empty",
			"https://github.com/owner/repo",
			"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rewriteGitHubBlobToRaw(tt.input)
			if got != tt.expected {
				t.Errorf("rewriteGitHubBlobToRaw(%q)\n  got:  %q\n  want: %q", tt.input, got, tt.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ParseGitHubURL
// ---------------------------------------------------------------------------

func TestParseGitHubURL(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  GitHubURLInfo
	}{
		{
			"bare repo",
			"https://github.com/owner/repo",
			GitHubURLInfo{Type: "repo", Owner: "owner", Repo: "repo"},
		},
		{
			"file via blob",
			"https://github.com/owner/repo/blob/main/pkg/file.go",
			GitHubURLInfo{Type: "file", Owner: "owner", Repo: "repo", Ref: "main", Path: "pkg/file.go"},
		},
		{
			"file in root",
			"https://github.com/owner/repo/blob/main/README.md",
			GitHubURLInfo{Type: "file", Owner: "owner", Repo: "repo", Ref: "main", Path: "README.md"},
		},
		{
			"file with deep path",
			"https://github.com/golang/go/blob/master/src/net/http/server.go",
			GitHubURLInfo{Type: "file", Owner: "golang", Repo: "go", Ref: "master", Path: "src/net/http/server.go"},
		},
		{
			"blob with ref only (no file)",
			"https://github.com/owner/repo/blob/main",
			GitHubURLInfo{Type: "directory", Owner: "owner", Repo: "repo", Ref: "main"},
		},
		{
			"directory via tree",
			"https://github.com/owner/repo/tree/main/pkg",
			GitHubURLInfo{Type: "directory", Owner: "owner", Repo: "repo", Ref: "main", Path: "pkg"},
		},
		{
			"tree root",
			"https://github.com/owner/repo/tree/main",
			GitHubURLInfo{Type: "directory", Owner: "owner", Repo: "repo", Ref: "main"},
		},
		{
			"tree with branch containing slash",
			"https://github.com/owner/repo/tree/feature/foo/pkg",
			GitHubURLInfo{Type: "directory", Owner: "owner", Repo: "repo", Ref: "feature", Path: "foo/pkg"},
		},
		{
			"issue",
			"https://github.com/owner/repo/issues/42",
			GitHubURLInfo{Type: "issue", Owner: "owner", Repo: "repo", Number: 42},
		},
		{
			"issue (singular)",
			"https://github.com/owner/repo/issue/7",
			GitHubURLInfo{Type: "issue", Owner: "owner", Repo: "repo", Number: 7},
		},
		{
			"pull request",
			"https://github.com/owner/repo/pull/123",
			GitHubURLInfo{Type: "pull_request", Owner: "owner", Repo: "repo", Number: 123},
		},
		{
			"pulls list page",
			"https://github.com/owner/repo/pulls",
			GitHubURLInfo{Type: "repo", Owner: "owner", Repo: "repo"},
		},
		{
			"pulls list page (pulls)",
			"https://github.com/owner/repo/pulls",
			GitHubURLInfo{Type: "repo", Owner: "owner", Repo: "repo"},
		},
		{
			"gist",
			"https://gist.github.com/abc123def",
			GitHubURLInfo{Type: "gist", GistID: "abc123def"},
		},
		{
			"unknown — wiki",
			"https://github.com/owner/repo/wiki",
			GitHubURLInfo{Type: "unknown", Owner: "owner", Repo: "repo"},
		},
		{
			"unknown — actions",
			"https://github.com/owner/repo/actions",
			GitHubURLInfo{Type: "unknown", Owner: "owner", Repo: "repo"},
		},
		{
			"unknown — non-numeric issue",
			"https://github.com/owner/repo/issues/abc",
			GitHubURLInfo{Type: "unknown", Owner: "owner", Repo: "repo"},
		},
		{
			"unknown — non-numeric pull",
			"https://github.com/owner/repo/pull/new",
			GitHubURLInfo{Type: "unknown", Owner: "owner", Repo: "repo"},
		},
		{
			"unknown — just owner",
			"https://github.com/owner",
			GitHubURLInfo{Type: "unknown"},
		},
		{
			"unknown — empty path",
			"https://github.com/",
			GitHubURLInfo{Type: "unknown"},
		},
		{
			"unknown — non-github",
			"https://gitlab.com/owner/repo",
			GitHubURLInfo{Type: "unknown"},
		},
		{
			"unknown — malformed URL",
			"://bad-url",
			GitHubURLInfo{Type: "unknown"},
		},
		{
			"issue zero",
			"https://github.com/owner/repo/issues/0",
			GitHubURLInfo{Type: "unknown", Owner: "owner", Repo: "repo"},
		},
		{
			"www subdomain",
			"https://www.github.com/owner/repo",
			GitHubURLInfo{Type: "repo", Owner: "owner", Repo: "repo"},
		},
		{
			"blob with line anchor",
			"https://github.com/owner/repo/blob/main/f.go#L42",
			GitHubURLInfo{Type: "file", Owner: "owner", Repo: "repo", Ref: "main", Path: "f.go"},
		},
		{
			"commit",
			"https://github.com/owner/repo/commit/abc123def456",
			GitHubURLInfo{Type: "commit", Owner: "owner", Repo: "repo", Ref: "abc123def456"},
		},
		{
			"commits with sha",
			"https://github.com/owner/repo/commits/abc123",
			GitHubURLInfo{Type: "commit", Owner: "owner", Repo: "repo", Ref: "abc123"},
		},
		{
			"commits list page",
			"https://github.com/owner/repo/commits",
			GitHubURLInfo{Type: "repo", Owner: "owner", Repo: "repo"},
		},
		{
			"discussion",
			"https://github.com/owner/repo/discussions/42",
			GitHubURLInfo{Type: "discussion", Owner: "owner", Repo: "repo", Number: 42},
		},
		{
			"discussion list page",
			"https://github.com/owner/repo/discussions",
			GitHubURLInfo{Type: "unknown", Owner: "owner", Repo: "repo"},
		},
		{
			"release by tag",
			"https://github.com/owner/repo/releases/tag/v1.0.0",
			GitHubURLInfo{Type: "release", Owner: "owner", Repo: "repo", Ref: "v1.0.0"},
		},
		{
			"release by numeric id",
			"https://github.com/owner/repo/releases/123",
			GitHubURLInfo{Type: "release", Owner: "owner", Repo: "repo", Number: 123},
		},
		{
			"releases list page",
			"https://github.com/owner/repo/releases",
			GitHubURLInfo{Type: "repo", Owner: "owner", Repo: "repo"},
		},
		{
			"actions run",
			"https://github.com/owner/repo/actions/runs/12345",
			GitHubURLInfo{Type: "actions_run", Owner: "owner", Repo: "repo", Number: 12345},
		},
		{
			"actions list page",
			"https://github.com/owner/repo/actions",
			GitHubURLInfo{Type: "unknown", Owner: "owner", Repo: "repo"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseGitHubURL(tt.input)
			if got != tt.want {
				t.Errorf("ParseGitHubURL(%q)\n  got:  %+v\n  want: %+v", tt.input, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Integration sanity: rewrite + parse round-trip
// ---------------------------------------------------------------------------

func TestRewriteAndParse(t *testing.T) {
	blobURL := "https://github.com/golang/go/blob/master/src/net/http/server.go"
	rawURL := rewriteGitHubBlobToRaw(blobURL)
	if rawURL == "" {
		t.Fatal("rewriteGitHubBlobToRaw returned empty for a valid blob URL")
	}

	// The rewritten URL should NOT be detected as a GitHub URL.
	if isGitHubURL(rawURL) {
		t.Errorf("rewritten URL %q should not be considered a GitHub URL", rawURL)
	}

	// Parse the original blob URL — should be a file.
	info := ParseGitHubURL(blobURL)
	if info.Type != "file" || info.Owner != "golang" || info.Repo != "go" || info.Path != "src/net/http/server.go" {
		t.Errorf("ParseGitHubURL of blob returned unexpected: %+v", info)
	}
}

// ---------------------------------------------------------------------------
// parsePositiveInt
// ---------------------------------------------------------------------------

func TestParsePositiveInt(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"42", 42},
		{"0", 0},
		{"", 0},
		{"-1", 0},
		{"12abc", 0},
		{"999", 999},
	}
	for _, tt := range tests {
		got := parsePositiveInt(tt.input)
		if got != tt.want {
			t.Errorf("parsePositiveInt(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// isDecimal
// ---------------------------------------------------------------------------

func TestIsDecimal(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"123", true},
		{"0", true},
		{"", false},
		{"12a", false},
		{"-5", false},
	}
	for _, tt := range tests {
		got := isDecimal(tt.input)
		if got != tt.want {
			t.Errorf("isDecimal(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}
