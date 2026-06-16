package git

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Prevent git push from hanging on fake remotes by disabling credential
// prompts.  This ensures ensureBranchPushed fails fast in tests rather
// than blocking indefinitely.
func init() {
	os.Setenv("GIT_TERMINAL_PROMPT", "0")
}

// =============================================================================
// Test helpers — additional setup functions (reusing existing gitRun)
// =============================================================================

// gitRunOutput executes a git command in dir and returns combined output.
// Fatal on error.  Complements the existing void-returning gitRun().
func gitRunOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// newTestGitRepoWithOrigin creates a repo with an "origin" remote pointing
// at the given GitHub URL and an initial commit on main.
//
// The init() function sets GIT_TERMINAL_PROMPT=0 so that ensureBranchPushed
// fails fast on the fake remote rather than blocking on a credential prompt.
func newTestGitRepoWithOrigin(t *testing.T, remoteURL string) string {
	dir := newTestGitRepo(t)
	gitRun(t, dir, "remote", "add", "origin", remoteURL)
	return dir
}

// newTestGitRepoWithBranch builds a repo with an initial commit on main,
// then creates a feature branch with additional commits and checks it out.
func newTestGitRepoWithBranch(t *testing.T, branch string, commitMessages []string) string {
	dir := newTestGitRepo(t)
	gitRun(t, dir, "checkout", "-b", branch)
	for i, msg := range commitMessages {
		f := filepath.Join(dir, fmt.Sprintf("feature%d.txt", i))
		if err := os.WriteFile(f, []byte(msg), 0644); err != nil {
			t.Fatal(err)
		}
		gitRun(t, dir, "add", f)
		gitRun(t, dir, "commit", "-m", msg)
	}
	return dir
}

// saveHooks stores the current overridable hooks so tests can restore them.
type savedHooks struct {
	runGhCommand     func(ctx context.Context, dir string, args ...string) ([]byte, error)
	prHTTPClient     *http.Client
	getDefaultBranch func(ctx context.Context, repoDir string) (string, error)
	githubAPIBaseURL string
}

func saveHooks() savedHooks {
	return savedHooks{
		runGhCommand:     runGhCommand,
		prHTTPClient:     prHTTPClient,
		getDefaultBranch: getDefaultBranch,
		githubAPIBaseURL: githubAPIBaseURL,
	}
}

func restoreHooks(s savedHooks) {
	runGhCommand = s.runGhCommand
	prHTTPClient = s.prHTTPClient
	getDefaultBranch = s.getDefaultBranch
	githubAPIBaseURL = s.githubAPIBaseURL
}// =============================================================================
// TestParseGitHubRemoteURL
// =============================================================================

func TestParseGitHubRemoteURL(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		wantOwner string
		wantRepo  string
		wantErr   bool
	}{
		{
			name:      "https with .git suffix",
			url:       "https://github.com/sprout-foundry/sprout.git",
			wantOwner: "sprout-foundry",
			wantRepo:  "sprout",
		},
		{
			name:      "https without .git suffix",
			url:       "https://github.com/sprout-foundry/sprout",
			wantOwner: "sprout-foundry",
			wantRepo:  "sprout",
		},
		{
			name:      "ssh format with .git",
			url:       "git@github.com:sprout-foundry/sprout.git",
			wantOwner: "sprout-foundry",
			wantRepo:  "sprout",
		},
		{
			name:      "ssh format without .git",
			url:       "git@github.com:sprout-foundry/sprout",
			wantOwner: "sprout-foundry",
			wantRepo:  "sprout",
		},
		{
			name:    "non-github https",
			url:     "https://gitlab.com/owner/repo.git",
			wantErr: true,
		},
		{
			name:    "non-github ssh",
			url:     "git@gitlab.com:owner/repo.git",
			wantErr: true,
		},
		{
			name:    "malformed empty",
			url:     "",
			wantErr: true,
		},
		{
			name:    "malformed no slash",
			url:     "https://github.com/ownernorepo.git",
			wantErr: true,
		},
		{
			name:    "malformed ssh no slash",
			url:     "git@github.com:ownernorepo.git",
			wantErr: true,
		},
		{
			name:      "http without https",
			url:       "http://github.com/owner/repo.git",
			wantOwner: "owner",
			wantRepo:  "repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, err := ParseGitHubRemoteURL(tt.url)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got owner=%q repo=%q", owner, repo)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if owner != tt.wantOwner {
				t.Errorf("owner = %q, want %q", owner, tt.wantOwner)
			}
			if repo != tt.wantRepo {
				t.Errorf("repo = %q, want %q", repo, tt.wantRepo)
			}
		})
	}
}

// =============================================================================
// TestCreatePullRequest_ViaAPI
// =============================================================================

func TestCreatePullRequest_ViaAPI_Success(t *testing.T) {
	saved := saveHooks()
	defer restoreHooks(saved)

	dir := newTestGitRepoWithOrigin(t, "https://github.com/testorg/testrepo.git")
	gitRun(t, dir, "checkout", "-b", "feature-test")

	getDefaultBranch = func(_ context.Context, _ string) (string, error) {
		return "main", nil
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/repos/testorg/testrepo/pulls") {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if auth := r.Header.Get("Authorization"); auth != "token testtoken123" {
			t.Errorf("bad auth header: %s", auth)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		var body createPRRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		if body.Title != "Fix bug" {
			t.Errorf("title = %q, want 'Fix bug'", body.Title)
		}
		if body.Head != "feature-test" {
			t.Errorf("head = %q, want 'feature-test'", body.Head)
		}
		if body.Base != "main" {
			t.Errorf("base = %q, want 'main'", body.Base)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(prAPIResponse{
			HTMLURL: "https://github.com/testorg/testrepo/pull/42",
			Number:  42,
			State:   "open",
		})
	}))
	defer server.Close()

	prHTTPClient = server.Client()
	githubAPIBaseURL = server.URL
	t.Setenv("GH_TOKEN", "testtoken123")

	ctx := context.Background()
	result, err := CreatePullRequest(ctx, dir, PullRequestRequest{
		Title: "Fix bug",
		Head:  "feature-test",
		Base:  "main",
	})
	if err != nil {
		t.Fatalf("CreatePullRequest failed: %v", err)
	}
	if result.URL != "https://github.com/testorg/testrepo/pull/42" {
		t.Errorf("URL = %q, want 'https://github.com/testorg/testrepo/pull/42'", result.URL)
	}
	if result.Number != 42 {
		t.Errorf("Number = %d, want 42", result.Number)
	}
	if result.State != "open" {
		t.Errorf("State = %q, want 'open'", result.State)
	}
}

func TestCreatePullRequest_ViaAPI_AlreadyExists(t *testing.T) {
	saved := saveHooks()
	defer restoreHooks(saved)

	dir := newTestGitRepoWithOrigin(t, "https://github.com/testorg/testrepo.git")
	gitRun(t, dir, "checkout", "-b", "feature-already")

	getDefaultBranch = func(_ context.Context, _ string) (string, error) {
		return "main", nil
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		json.NewEncoder(w).Encode(map[string]string{
			"message": "A pull request already exists for feature-already.",
		})
	}))
	defer server.Close()

	prHTTPClient = server.Client()
	githubAPIBaseURL = server.URL
	t.Setenv("GH_TOKEN", "testtoken123")

	ctx := context.Background()
	_, err := CreatePullRequest(ctx, dir, PullRequestRequest{
		Title: "Already exists",
		Head:  "feature-already",
		Base:  "main",
	})
	if err == nil {
		t.Fatal("expected error for 422, got nil")
	}
	if !strings.Contains(err.Error(), "422") {
		t.Errorf("error should mention 422, got: %v", err)
	}
	// Should NOT fall through to gh — 422 is a hard error.
	if errors.Is(err, ErrNoGitHubAuth) {
		t.Errorf("422 should not wrap ErrNoGitHubAuth, got: %v", err)
	}
}

func TestCreatePullRequest_ViaAPI_401FallsThroughToGH(t *testing.T) {
	saved := saveHooks()
	defer restoreHooks(saved)

	dir := newTestGitRepoWithOrigin(t, "https://github.com/testorg/testrepo.git")
	gitRun(t, dir, "checkout", "-b", "feature-fallback")

	getDefaultBranch = func(_ context.Context, _ string) (string, error) {
		return "main", nil
	}

	apiHit := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiHit = true
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{
			"message": "Bad credentials",
		})
	}))
	defer server.Close()

	prHTTPClient = server.Client()
	githubAPIBaseURL = server.URL
	t.Setenv("GH_TOKEN", "badtoken")

	runGhCommand = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		return []byte("https://github.com/testorg/testrepo/pull/99\n"), nil
	}

	ctx := context.Background()
	result, err := CreatePullRequest(ctx, dir, PullRequestRequest{
		Title: "Fallback PR",
		Head:  "feature-fallback",
		Base:  "main",
	})
	if err != nil {
		t.Fatalf("expected success via gh fallback, got: %v", err)
	}
	if !apiHit {
		t.Error("expected API to be called before falling through to gh")
	}
	if result.URL != "https://github.com/testorg/testrepo/pull/99" {
		t.Errorf("URL = %q, want gh-fallback URL", result.URL)
	}
	if result.Number != 99 {
		t.Errorf("Number = %d, want 99", result.Number)
	}
}

func TestCreatePullRequest_NoTokenSkipsAPI(t *testing.T) {
	saved := saveHooks()
	defer restoreHooks(saved)

	dir := newTestGitRepoWithOrigin(t, "https://github.com/testorg/testrepo.git")
	gitRun(t, dir, "checkout", "-b", "feature-notoken")

	getDefaultBranch = func(_ context.Context, _ string) (string, error) {
		return "main", nil
	}

	t.Setenv("GH_TOKEN", "")

	apiCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiCalled = true
	}))
	defer server.Close()
	prHTTPClient = server.Client()
	githubAPIBaseURL = server.URL

	runGhCommand = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		return []byte("https://github.com/testorg/testrepo/pull/10\n"), nil
	}

	ctx := context.Background()
	result, err := CreatePullRequest(ctx, dir, PullRequestRequest{
		Title: "No token PR",
		Head:  "feature-notoken",
		Base:  "main",
	})
	if err != nil {
		t.Fatalf("expected success via gh fallback, got: %v", err)
	}
	if apiCalled {
		t.Error("API should NOT have been called when GH_TOKEN is empty")
	}
	if result.URL != "https://github.com/testorg/testrepo/pull/10" {
		t.Errorf("URL = %q, want gh-fallback URL", result.URL)
	}
}

// =============================================================================
// TestCreatePullRequest_ViaGH
// =============================================================================

func TestCreatePullRequest_ViaGH_Success(t *testing.T) {
	saved := saveHooks()
	defer restoreHooks(saved)

	dir := newTestGitRepo(t)
	gitRun(t, dir, "checkout", "-b", "feature-gh")

	getDefaultBranch = func(_ context.Context, _ string) (string, error) {
		return "main", nil
	}

	t.Setenv("GH_TOKEN", "")

	runGhCommand = func(_ context.Context, dir string, args ...string) ([]byte, error) {
		got := strings.Join(args, " ")
		if !strings.Contains(got, "--title") || !strings.Contains(got, "Test PR") {
			t.Errorf("expected --title 'Test PR' in args, got: %s", got)
		}
		if !strings.Contains(got, "--head") || !strings.Contains(got, "feature-gh") {
			t.Errorf("expected --head 'feature-gh' in args, got: %s", got)
		}
		return []byte("Creating pull request for feature-gh into main\nhttps://github.com/owner/repo/pull/55\n"), nil
	}

	ctx := context.Background()
	result, err := CreatePullRequest(ctx, dir, PullRequestRequest{
		Title: "Test PR",
		Head:  "feature-gh",
		Base:  "main",
	})
	if err != nil {
		t.Fatalf("CreatePullRequest failed: %v", err)
	}
	if result.URL != "https://github.com/owner/repo/pull/55" {
		t.Errorf("URL = %q, want 'https://github.com/owner/repo/pull/55'", result.URL)
	}
	if result.Number != 55 {
		t.Errorf("Number = %d, want 55", result.Number)
	}
	if result.State != "open" {
		t.Errorf("State = %q, want 'open'", result.State)
	}
}

func TestCreatePullRequest_ViaGH_Draft(t *testing.T) {
	saved := saveHooks()
	defer restoreHooks(saved)

	dir := newTestGitRepo(t)
	gitRun(t, dir, "checkout", "-b", "feature-draft")

	getDefaultBranch = func(_ context.Context, _ string) (string, error) {
		return "main", nil
	}

	t.Setenv("GH_TOKEN", "")

	runGhCommand = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		argStr := strings.Join(args, " ")
		if !strings.Contains(argStr, "--draft") {
			t.Errorf("expected --draft in args, got: %s", argStr)
		}
		return []byte("https://github.com/owner/repo/pull/1\n"), nil
	}

	ctx := context.Background()
	result, err := CreatePullRequest(ctx, dir, PullRequestRequest{
		Title: "Draft PR",
		Head:  "feature-draft",
		Base:  "main",
		Draft: true,
	})
	if err != nil {
		t.Fatalf("CreatePullRequest failed: %v", err)
	}
	if result.URL != "https://github.com/owner/repo/pull/1" {
		t.Errorf("URL = %q, want gh-fallback URL", result.URL)
	}
}

func TestCreatePullRequest_ViaGH_NotAvailable(t *testing.T) {
	saved := saveHooks()
	defer restoreHooks(saved)

	dir := newTestGitRepo(t)
	gitRun(t, dir, "checkout", "-b", "feature-no-gh")

	getDefaultBranch = func(_ context.Context, _ string) (string, error) {
		return "main", nil
	}

	t.Setenv("GH_TOKEN", "")

	runGhCommand = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		return nil, errors.New("executable file not found in $PATH")
	}

	ctx := context.Background()
	_, err := CreatePullRequest(ctx, dir, PullRequestRequest{
		Title: "No gh",
		Head:  "feature-no-gh",
		Base:  "main",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrNoGitHubAuth) {
		t.Errorf("expected ErrNoGitHubAuth, got: %v", err)
	}
	if !strings.Contains(err.Error(), "gh pr create") {
		t.Errorf("error should contain fallback gh command, got: %v", err)
	}
	if !strings.Contains(err.Error(), "--title") {
		t.Errorf("error should contain --title, got: %v", err)
	}
}

// =============================================================================
// TestCreatePullRequest_NoAuth (both API and gh fail)
// =============================================================================

func TestCreatePullRequest_NoAuth(t *testing.T) {
	saved := saveHooks()
	defer restoreHooks(saved)

	dir := newTestGitRepoWithOrigin(t, "https://github.com/testorg/testrepo.git")
	gitRun(t, dir, "checkout", "-b", "feature-noauth")

	getDefaultBranch = func(_ context.Context, _ string) (string, error) {
		return "main", nil
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"message": "Bad token"})
	}))
	defer server.Close()

	prHTTPClient = server.Client()
	githubAPIBaseURL = server.URL
	t.Setenv("GH_TOKEN", "badtoken")

	runGhCommand = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		return nil, errors.New("gh: command not found")
	}

	ctx := context.Background()
	_, err := CreatePullRequest(ctx, dir, PullRequestRequest{
		Title: "Failing PR",
		Body:  "Some description with 'quotes'",
		Head:  "feature-noauth",
		Base:  "main",
	})
	if err == nil {
		t.Fatal("expected error when both API and gh fail")
	}
	if !errors.Is(err, ErrNoGitHubAuth) {
		t.Errorf("expected ErrNoGitHubAuth, got: %v", err)
	}
	errStr := err.Error()
	if !strings.Contains(errStr, "gh pr create") {
		t.Errorf("error should contain fallback command, got: %v", err)
	}
	if !strings.Contains(errStr, "--title") {
		t.Errorf("error should contain --title in command, got: %v", err)
	}
	if !strings.Contains(errStr, "--body") {
		t.Errorf("error should contain --body in command, got: %v", err)
	}
	if !strings.Contains(errStr, "--base 'main'") {
		t.Errorf("error should contain --base 'main', got: %v", err)
	}
	if !strings.Contains(errStr, "--head 'feature-noauth'") {
		t.Errorf("error should contain --head 'feature-noauth', got: %v", err)
	}
	// The body has single quotes — they should be escaped in the command.
	if !strings.Contains(errStr, "'\\''") {
		t.Errorf("error should contain escaped single quotes in body, got: %v", err)
	}
}

// =============================================================================
// TestSynthesizePRBody
// =============================================================================

func TestSynthesizePRBody(t *testing.T) {
	dir := newTestGitRepoWithBranch(t, "feature-synthesize", []string{
		"Add user authentication",
		"Fix login bug",
		"Update README",
	})

	ctx := context.Background()
	body, err := synthesizePRBody(ctx, dir, "main", "feature-synthesize")
	if err != nil {
		t.Fatalf("synthesizePRBody failed: %v", err)
	}

	if !strings.HasPrefix(body, "## Commits") {
		t.Errorf("body should start with '## Commits', got: %q", body)
	}

	for _, msg := range []string{
		"Add user authentication",
		"Fix login bug",
		"Update README",
	} {
		if !strings.Contains(body, "- "+msg) {
			t.Errorf("body should contain '- %s', got:\n%s", msg, body)
		}
	}
}

func TestSynthesizePRBody_NoCommits(t *testing.T) {
	dir := newTestGitRepo(t)
	gitRun(t, dir, "checkout", "-b", "empty-feature")

	ctx := context.Background()
	body, err := synthesizePRBody(ctx, dir, "main", "empty-feature")
	if err != nil {
		t.Fatalf("synthesizePRBody failed: %v", err)
	}
	if body != "" {
		t.Errorf("expected empty body when no commits, got: %q", body)
	}
}

// =============================================================================
// TestExtractPRURLAndNumber
// =============================================================================

func TestExtractPRURLAndNumber(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantURL string
		wantNum int
	}{
		{
			name:    "standard output",
			input:   "Creating pull request for feature into main\nhttps://github.com/owner/repo/pull/42\n",
			wantURL: "https://github.com/owner/repo/pull/42",
			wantNum: 42,
		},
		{
			name:    "URL only",
			input:   "https://github.com/org/project/pull/123",
			wantURL: "https://github.com/org/project/pull/123",
			wantNum: 123,
		},
		{
			name:    "multi-digit number",
			input:   "https://github.com/owner/repo/pull/12345",
			wantURL: "https://github.com/owner/repo/pull/12345",
			wantNum: 12345,
		},
		{
			name:    "URL on second line after text",
			input:   "Some preamble text\nMore text here\nhttps://github.com/a/b/pull/7\n",
			wantURL: "https://github.com/a/b/pull/7",
			wantNum: 7,
		},
		{
			name:    "no URL present",
			input:   "Just some random text\nNo URL here\n",
			wantURL: "",
			wantNum: 0,
		},
		{
			name:    "empty input",
			input:   "",
			wantURL: "",
			wantNum: 0,
		},
		{
			name:    "single digit",
			input:   "https://github.com/x/y/pull/1\n",
			wantURL: "https://github.com/x/y/pull/1",
			wantNum: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url, num := extractPRURLAndNumber(tt.input)
			if url != tt.wantURL {
				t.Errorf("URL = %q, want %q", url, tt.wantURL)
			}
			if num != tt.wantNum {
				t.Errorf("Number = %d, want %d", num, tt.wantNum)
			}
		})
	}
}

// =============================================================================
// TestExtractPRURLAndNumber_EdgeCases
// =============================================================================

func TestExtractPRURLAndNumber_EdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantURL string
		wantNum int
	}{
		{
			name:    "github.com entangled in text",
			input:   "See https://github.com/x/y/pull/999 for details.",
			wantURL: "https://github.com/x/y/pull/999 for details.",
			wantNum: 999,
		},
		{
			name:    "URL with trailing whitespace",
			input:   "https://github.com/a/b/pull/10\n   \n",
			wantURL: "https://github.com/a/b/pull/10",
			wantNum: 10,
		},
		{
			name:    "non-pull github URL ignored",
			input:   "https://github.com/owner/repo/commit/abc123\n",
			wantURL: "",
			wantNum: 0,
		},
		{
			name:    "multiple lines first match wins",
			input:   "https://github.com/owner/repo/pull/1\nhttps://github.com/owner/repo/pull/2\n",
			wantURL: "https://github.com/owner/repo/pull/1",
			wantNum: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url, num := extractPRURLAndNumber(tt.input)
			if url != tt.wantURL {
				t.Errorf("URL = %q, want %q", url, tt.wantURL)
			}
			if num != tt.wantNum {
				t.Errorf("Number = %d, want %d", num, tt.wantNum)
			}
		})
	}
}

// =============================================================================
// TestBuildFallbackGHCommand
// =============================================================================

func TestBuildFallbackGHCommand(t *testing.T) {
	tests := []struct {
		name  string
		head  string
		base  string
		title string
		body  string
		draft bool
		want  string
	}{
		{
			name:  "simple case",
			head:  "feature",
			base:  "main",
			title: "Fix bug",
			body:  "This fixes the bug",
			draft: false,
			want:  "gh pr create --title 'Fix bug' --body 'This fixes the bug' --base 'main' --head 'feature'",
		},
		{
			name:  "with draft",
			head:  "feature",
			base:  "main",
			title: "Draft PR",
			body:  "",
			draft: true,
			want:  "gh pr create --title 'Draft PR' --body '' --base 'main' --head 'feature' --draft",
		},
		{
			name:  "title with single quotes",
			head:  "feature",
			base:  "main",
			title: "It's fixed",
			body:  "",
			draft: false,
			want:  "gh pr create --title 'It'\\''s fixed' --body '' --base 'main' --head 'feature'",
		},
		{
			name:  "body with single quotes",
			head:  "feature",
			base:  "main",
			title: "Fix",
			body:  "Don't do that",
			draft: false,
			want:  "gh pr create --title 'Fix' --body 'Don'\\''t do that' --base 'main' --head 'feature'",
		},
		{
			name:  "empty body",
			head:  "my-branch",
			base:  "develop",
			title: "Update docs",
			body:  "",
			draft: false,
			want:  "gh pr create --title 'Update docs' --body '' --base 'develop' --head 'my-branch'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildFallbackGHCommand(tt.head, tt.base, tt.title, tt.body, tt.draft)
			if got != tt.want {
				t.Errorf("buildFallbackGHCommand() = %q\nwant:  %q", got, tt.want)
			}
		})
	}
}

func TestBuildFallbackGHCommand_MultilineBody(t *testing.T) {
	body := "First line\nSecond line"
	cmd := buildFallbackGHCommand("feature", "main", "Title", body, false)
	if !strings.Contains(cmd, "First line") {
		t.Errorf("command should contain 'First line', got: %s", cmd)
	}
	if !strings.Contains(cmd, "Second line") {
		t.Errorf("command should contain 'Second line', got: %s", cmd)
	}
	if !strings.Contains(cmd, "--body 'First line\nSecond line'") {
		t.Errorf("body should preserve newlines, got: %s", cmd)
	}
}

// =============================================================================
// TestShellQuote
// =============================================================================

func TestShellQuote(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "simple string",
			input: "hello",
			want:  "'hello'",
		},
		{
			name:  "string with single quotes",
			input: "it's here",
			want:  "'it'\\''s here'",
		},
		{
			name:  "empty string",
			input: "",
			want:  "''",
		},
		{
			name:  "only single quotes",
			input: "'",
			want:  "''\\'''",
		},
		{
			name:  "multiple single quotes",
			input: "a'b'c",
			want:  "'a'\\''b'\\''c'",
		},
		{
			name:  "string with double quotes",
			input: `say "hi"`,
			want:  "'say \"hi\"'",
		},
		{
			name:  "string with spaces",
			input: "hello world",
			want:  "'hello world'",
		},
		{
			name:  "string with dollar sign",
			input: "$HOME",
			want:  "'$HOME'",
		},
		{
			name:  "string with backticks",
			input: "`whoami`",
			want:  "'`whoami`'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shellQuote(tt.input)
			if got != tt.want {
				t.Errorf("shellQuote(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// =============================================================================
// TestCreatePullRequest_BodySynthesis (integration: body synthesis through CreatePullRequest)
// =============================================================================

func TestCreatePullRequest_BodySynthesis(t *testing.T) {
	saved := saveHooks()
	defer restoreHooks(saved)

	dir := newTestGitRepoWithOrigin(t, "https://github.com/testorg/testrepo.git")
	gitRun(t, dir, "checkout", "-b", "feature-synth")

	f := filepath.Join(dir, "new.txt")
	if err := os.WriteFile(f, []byte("new file"), 0644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "new.txt")
	gitRun(t, dir, "commit", "-m", "Add new feature file")

	getDefaultBranch = func(_ context.Context, _ string) (string, error) {
		return "main", nil
	}

	var capturedBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body createPRRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		capturedBody = body.Body

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(prAPIResponse{
			HTMLURL: "https://github.com/testorg/testrepo/pull/1",
			Number:  1,
			State:   "open",
		})
	}))
	defer server.Close()

	prHTTPClient = server.Client()
	githubAPIBaseURL = server.URL
	t.Setenv("GH_TOKEN", "testtoken")

	ctx := context.Background()
	result, err := CreatePullRequest(ctx, dir, PullRequestRequest{
		Title: "Feature PR",
		Head:  "feature-synth",
		Base:  "main",
	})
	if err != nil {
		t.Fatalf("CreatePullRequest failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if !strings.Contains(capturedBody, "Add new feature file") {
		t.Errorf("synthesized body should contain commit subject, got: %q", capturedBody)
	}
	if !strings.Contains(capturedBody, "## Commits") {
		t.Errorf("synthesized body should contain '## Commits' header, got: %q", capturedBody)
	}
}

// =============================================================================
// TestCreatePullRequest_ExplicitBodyNotOverridden
// =============================================================================

func TestCreatePullRequest_ExplicitBodyNotOverridden(t *testing.T) {
	saved := saveHooks()
	defer restoreHooks(saved)

	dir := newTestGitRepoWithOrigin(t, "https://github.com/testorg/testrepo.git")
	gitRun(t, dir, "checkout", "-b", "feature-explicit-body")

	f := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(f, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "f.txt")
	gitRun(t, dir, "commit", "-m", "Some commit")

	getDefaultBranch = func(_ context.Context, _ string) (string, error) {
		return "main", nil
	}

	var capturedBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body createPRRequest
		json.NewDecoder(r.Body).Decode(&body)
		capturedBody = body.Body

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(prAPIResponse{
			HTMLURL: "https://github.com/testorg/testrepo/pull/2",
			Number:  2,
			State:   "open",
		})
	}))
	defer server.Close()

	prHTTPClient = server.Client()
	githubAPIBaseURL = server.URL
	t.Setenv("GH_TOKEN", "testtoken")

	ctx := context.Background()
	result, err := CreatePullRequest(ctx, dir, PullRequestRequest{
		Title: "Explicit body PR",
		Body:  "This is my custom body",
		Head:  "feature-explicit-body",
		Base:  "main",
	})
	if err != nil {
		t.Fatalf("CreatePullRequest failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if capturedBody != "This is my custom body" {
		t.Errorf("body = %q, want 'This is my custom body'", capturedBody)
	}
}

// =============================================================================
// TestCreatePullRequest_ResolveHeadFromCurrentBranch
// =============================================================================

func TestCreatePullRequest_ResolveHeadFromCurrentBranch(t *testing.T) {
	saved := saveHooks()
	defer restoreHooks(saved)

	dir := newTestGitRepoWithOrigin(t, "https://github.com/testorg/testrepo.git")
	gitRun(t, dir, "checkout", "-b", "auto-head-branch")

	getDefaultBranch = func(_ context.Context, _ string) (string, error) {
		return "main", nil
	}

	var capturedHead string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body createPRRequest
		json.NewDecoder(r.Body).Decode(&body)
		capturedHead = body.Head

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(prAPIResponse{
			HTMLURL: "https://github.com/testorg/testrepo/pull/3",
			Number:  3,
			State:   "open",
		})
	}))
	defer server.Close()

	prHTTPClient = server.Client()
	githubAPIBaseURL = server.URL
	t.Setenv("GH_TOKEN", "testtoken")

	ctx := context.Background()
	result, err := CreatePullRequest(ctx, dir, PullRequestRequest{
		Title: "Auto head PR",
		Base:  "main",
	})
	if err != nil {
		t.Fatalf("CreatePullRequest failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if capturedHead != "auto-head-branch" {
		t.Errorf("head sent to API = %q, want 'auto-head-branch'", capturedHead)
	}
}

// =============================================================================
// TestCreatePullRequest_DraftViaAPI
// =============================================================================

func TestCreatePullRequest_DraftViaAPI(t *testing.T) {
	saved := saveHooks()
	defer restoreHooks(saved)

	dir := newTestGitRepoWithOrigin(t, "https://github.com/testorg/testrepo.git")
	gitRun(t, dir, "checkout", "-b", "feature-draft-api")

	getDefaultBranch = func(_ context.Context, _ string) (string, error) {
		return "main", nil
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body createPRRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		if !body.Draft {
			t.Errorf("expected draft=true, got draft=false")
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(prAPIResponse{
			HTMLURL: "https://github.com/testorg/testrepo/pull/5",
			Number:  5,
			State:   "open",
		})
	}))
	defer server.Close()

	prHTTPClient = server.Client()
	githubAPIBaseURL = server.URL
	t.Setenv("GH_TOKEN", "testtoken")

	ctx := context.Background()
	result, err := CreatePullRequest(ctx, dir, PullRequestRequest{
		Title: "Draft PR via API",
		Head:  "feature-draft-api",
		Base:  "main",
		Draft: true,
	})
	if err != nil {
		t.Fatalf("CreatePullRequest failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Number != 5 {
		t.Errorf("Number = %d, want 5", result.Number)
	}
}

// =============================================================================
// TestCreatePullRequest_ResolveBaseFromDefaultBranch
// =============================================================================

func TestCreatePullRequest_ResolveBaseFromDefaultBranch(t *testing.T) {
	saved := saveHooks()
	defer restoreHooks(saved)

	dir := newTestGitRepoWithOrigin(t, "https://github.com/testorg/testrepo.git")
	gitRun(t, dir, "checkout", "-b", "feature-base-resolve")

	getDefaultBranch = func(_ context.Context, _ string) (string, error) {
		return "develop", nil
	}

	var capturedBase string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body createPRRequest
		json.NewDecoder(r.Body).Decode(&body)
		capturedBase = body.Base

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(prAPIResponse{
			HTMLURL: "https://github.com/testorg/testrepo/pull/6",
			Number:  6,
			State:   "open",
		})
	}))
	defer server.Close()

	prHTTPClient = server.Client()
	githubAPIBaseURL = server.URL
	t.Setenv("GH_TOKEN", "testtoken")

	ctx := context.Background()
	result, err := CreatePullRequest(ctx, dir, PullRequestRequest{
		Title: "Base resolve PR",
		Head:  "feature-base-resolve",
	})
	if err != nil {
		t.Fatalf("CreatePullRequest failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if capturedBase != "develop" {
		t.Errorf("base sent to API = %q, want 'develop'", capturedBase)
	}
}

// =============================================================================
// TestCreatePullRequest_APIUnknownError (non-401, non-422)
// =============================================================================

func TestCreatePullRequest_APIUnknownError(t *testing.T) {
	saved := saveHooks()
	defer restoreHooks(saved)

	dir := newTestGitRepoWithOrigin(t, "https://github.com/testorg/testrepo.git")
	gitRun(t, dir, "checkout", "-b", "feature-api-error")

	getDefaultBranch = func(_ context.Context, _ string) (string, error) {
		return "main", nil
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"message": "Internal server error",
		})
	}))
	defer server.Close()

	prHTTPClient = server.Client()
	githubAPIBaseURL = server.URL
	t.Setenv("GH_TOKEN", "testtoken")

	ctx := context.Background()
	_, err := CreatePullRequest(ctx, dir, PullRequestRequest{
		Title: "API error PR",
		Head:  "feature-api-error",
		Base:  "main",
	})
	if err == nil {
		t.Fatal("expected error for 500, got nil")
	}
	if errors.Is(err, ErrNoGitHubAuth) {
		t.Errorf("500 should not wrap ErrNoGitHubAuth, got: %v", err)
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should mention 500, got: %v", err)
	}
}

// =============================================================================
// TestIsHTTPError
// =============================================================================

func TestIsHTTPError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		code int
		want bool
	}{
		{
			name: "matching 401",
			err:  httpError(http.StatusUnauthorized, "bad token"),
			code: http.StatusUnauthorized,
			want: true,
		},
		{
			name: "matching 422",
			err:  httpError(http.StatusUnprocessableEntity, "already exists"),
			code: http.StatusUnprocessableEntity,
			want: true,
		},
		{
			name: "non-matching code",
			err:  httpError(http.StatusUnauthorized, "bad token"),
			code: http.StatusUnprocessableEntity,
			want: false,
		},
		{
			name: "non-httpError",
			err:  errors.New("some other error"),
			code: http.StatusUnauthorized,
			want: false,
		},
		{
			name: "wrapped httpError",
			err:  fmt.Errorf("wrapped: %w", httpError(http.StatusUnauthorized, "bad token")),
			code: http.StatusUnauthorized,
			want: true,
		},
		{
			name: "wrapped non-matching",
			err:  fmt.Errorf("wrapped: %w", httpError(http.StatusNotFound, "not found")),
			code: http.StatusUnauthorized,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isHTTPError(tt.err, tt.code)
			if got != tt.want {
				t.Errorf("isHTTPError(%v, %d) = %v, want %v", tt.err, tt.code, got, tt.want)
			}
		})
	}
}

// =============================================================================
// TestHTTPErrorStatus
// =============================================================================

func TestHTTPErrorStatus(t *testing.T) {
	err := httpError(403, "forbidden")
	if err.Error() != "forbidden" {
		t.Errorf("httpError(403, 'forbidden').Error() = %q, want 'forbidden'", err.Error())
	}
	if err.Code != 403 {
		t.Errorf("httpError(403, 'forbidden').Code = %d, want 403", err.Code)
	}
}

// =============================================================================
// TestCreatePullRequest_ResolveHeadDetachedHead
// =============================================================================

func TestCreatePullRequest_ResolveHeadDetachedHead(t *testing.T) {
	saved := saveHooks()
	defer restoreHooks(saved)

	dir := newTestGitRepo(t)

	// Detach HEAD
	hash := gitRunOutput(t, dir, "rev-parse", "HEAD")
	hash = strings.TrimSpace(hash)
	gitRun(t, dir, "checkout", hash)

	getDefaultBranch = func(_ context.Context, _ string) (string, error) {
		return "main", nil
	}

	ctx := context.Background()
	_, err := CreatePullRequest(ctx, dir, PullRequestRequest{
		Title: "Detached HEAD PR",
	})
	if err == nil {
		t.Fatal("expected error for detached HEAD, got nil")
	}
	if !strings.Contains(err.Error(), "resolve head branch") {
		t.Errorf("error should mention 'resolve head branch', got: %v", err)
	}
}

// =============================================================================
// TestCreatePullRequest_PushFailureDoesntBlockAPI
// =============================================================================

func TestCreatePullRequest_PushFailureDoesntBlockAPI(t *testing.T) {
	saved := saveHooks()
	defer restoreHooks(saved)

	dir := newTestGitRepoWithOrigin(t, "https://github.com/testorg/testrepo.git")
	gitRun(t, dir, "checkout", "-b", "feature-noupstream")

	getDefaultBranch = func(_ context.Context, _ string) (string, error) {
		return "main", nil
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(prAPIResponse{
			HTMLURL: "https://github.com/testorg/testrepo/pull/100",
			Number:  100,
			State:   "open",
		})
	}))
	defer server.Close()

	prHTTPClient = server.Client()
	githubAPIBaseURL = server.URL
	t.Setenv("GH_TOKEN", "testtoken")

	ctx := context.Background()
	result, err := CreatePullRequest(ctx, dir, PullRequestRequest{
		Title: "Push fail but API works",
		Head:  "feature-noupstream",
		Base:  "main",
	})
	// The push will fail (no real remote), but the API should still succeed.
	if err != nil {
		t.Fatalf("expected success via API despite push failure, got: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Number != 100 {
		t.Errorf("Number = %d, want 100", result.Number)
	}
}

// =============================================================================
// TestCreatePullRequest_ReviewersIgnoredByGH
// =============================================================================

func TestCreatePullRequest_ReviewersIgnoredByGH(t *testing.T) {
	saved := saveHooks()
	defer restoreHooks(saved)

	dir := newTestGitRepo(t)
	gitRun(t, dir, "checkout", "-b", "feature-reviewers")

	getDefaultBranch = func(_ context.Context, _ string) (string, error) {
		return "main", nil
	}

	t.Setenv("GH_TOKEN", "")

	var capturedArgs []string
	runGhCommand = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		capturedArgs = args
		return []byte("https://github.com/owner/repo/pull/1\n"), nil
	}

	ctx := context.Background()
	result, err := CreatePullRequest(ctx, dir, PullRequestRequest{
		Title:     "Reviewer PR",
		Head:      "feature-reviewers",
		Base:      "main",
		Reviewers: []string{"alice", "bob"},
	})
	if err != nil {
		t.Fatalf("CreatePullRequest failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	// gh CLI doesn't support --reviewers in the args (API-only feature).
	argStr := strings.Join(capturedArgs, " ")
	if strings.Contains(argStr, "alice") || strings.Contains(argStr, "bob") {
		t.Errorf("gh args should not contain reviewer names (API-only feature), got: %s", argStr)
	}
}

// =============================================================================
// TestCreatePullRequest_APIDeserializesCorrectPayload
// =============================================================================

func TestCreatePullRequest_APIDeserializesCorrectPayload(t *testing.T) {
	saved := saveHooks()
	defer restoreHooks(saved)

	dir := newTestGitRepoWithOrigin(t, "https://github.com/myorg/myrepo.git")
	gitRun(t, dir, "checkout", "-b", "feature-payload")

	getDefaultBranch = func(_ context.Context, _ string) (string, error) {
		return "main", nil
	}

	var captured createPRRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(prAPIResponse{
			HTMLURL: "https://github.com/myorg/myrepo/pull/7",
			Number:  7,
			State:   "open",
		})
	}))
	defer server.Close()

	prHTTPClient = server.Client()
	githubAPIBaseURL = server.URL
	t.Setenv("GH_TOKEN", "token123")

	ctx := context.Background()
	result, err := CreatePullRequest(ctx, dir, PullRequestRequest{
		Title: "Complex PR",
		Body:  "This has a body",
		Head:  "feature-payload",
		Base:  "main",
		Draft: true,
	})
	if err != nil {
		t.Fatalf("CreatePullRequest failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	if captured.Title != "Complex PR" {
		t.Errorf("captured.Title = %q, want 'Complex PR'", captured.Title)
	}
	if captured.Body != "This has a body" {
		t.Errorf("captured.Body = %q, want 'This has a body'", captured.Body)
	}
	if captured.Head != "feature-payload" {
		t.Errorf("captured.Head = %q, want 'feature-payload'", captured.Head)
	}
	if captured.Base != "main" {
		t.Errorf("captured.Base = %q, want 'main'", captured.Base)
	}
	if !captured.Draft {
		t.Error("captured.Draft should be true")
	}
}

// =============================================================================
// TestCreatePullRequest_GetOwnerRepoFailure (no remote at all)
// =============================================================================

func TestCreatePullRequest_GetOwnerRepoFailure(t *testing.T) {
	saved := saveHooks()
	defer restoreHooks(saved)

	dir := newTestGitRepo(t)
	gitRun(t, dir, "checkout", "-b", "feature-noremote")

	getDefaultBranch = func(_ context.Context, _ string) (string, error) {
		return "main", nil
	}

	t.Setenv("GH_TOKEN", "testtoken")

	runGhCommand = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		return []byte("https://github.com/owner/repo/pull/11\n"), nil
	}

	ctx := context.Background()
	result, err := CreatePullRequest(ctx, dir, PullRequestRequest{
		Title: "No remote PR",
		Head:  "feature-noremote",
		Base:  "main",
	})
	if err != nil {
		t.Fatalf("expected success via gh fallback when owner/repo can't be resolved, got: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.URL != "https://github.com/owner/repo/pull/11" {
		t.Errorf("URL = %q, want gh-fallback URL", result.URL)
	}
}

// =============================================================================
// TestCreatePullRequest_GetOwnerRepoNonGithub (gitlab origin, falls to gh)
// =============================================================================

func TestCreatePullRequest_GetOwnerRepoNonGithub(t *testing.T) {
	saved := saveHooks()
	defer restoreHooks(saved)

	dir := newTestGitRepoWithOrigin(t, "https://gitlab.com/owner/repo.git")
	gitRun(t, dir, "checkout", "-b", "feature-gitlab")

	getDefaultBranch = func(_ context.Context, _ string) (string, error) {
		return "main", nil
	}

	t.Setenv("GH_TOKEN", "testtoken")

	runGhCommand = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		return []byte("https://github.com/owner/repo/pull/12\n"), nil
	}

	ctx := context.Background()
	result, err := CreatePullRequest(ctx, dir, PullRequestRequest{
		Title: "GitLab origin PR",
		Head:  "feature-gitlab",
		Base:  "main",
	})
	if err != nil {
		t.Fatalf("expected success via gh fallback for non-github origin, got: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.URL != "https://github.com/owner/repo/pull/12" {
		t.Errorf("URL = %q, want gh-fallback URL", result.URL)
	}
}

// =============================================================================
// Compile-time verification that target functions exist
// =============================================================================

var (
	_ func(string) (string, string, error)                        = ParseGitHubRemoteURL
	_ func(context.Context, string, PullRequestRequest) (*PullRequestResult, error) = CreatePullRequest
	_ func(context.Context, string, string, string) (string, error) = synthesizePRBody
	_ func(string) (string, int)                                  = extractPRURLAndNumber
	_ func(string, string, string, string, bool) string           = buildFallbackGHCommand
	_ func(string) string                                         = shellQuote
)
