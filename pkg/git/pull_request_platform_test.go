package git

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// platformEnv makes the test process look like a platform workspace whose
// platform API is srv.
func platformEnv(t *testing.T, srv *httptest.Server) {
	t.Setenv("PLATFORM_API_URL", srv.URL)
	t.Setenv("WORKSPACE_ID", "ws-1")
	t.Setenv("WORKSPACE_TOKEN", "ws-secret")
	prHTTPClient = srv.Client()
}

// stubPush makes PushBranch a no-op for the test.
func stubPush(t *testing.T) {
	orig := PushBranch
	PushBranch = func(context.Context, string, string) error { return nil }
	t.Cleanup(func() { PushBranch = orig })
}

func TestCreatePullRequest_ViaPlatform_AnyHost(t *testing.T) {
	saved := saveHooks()
	defer restoreHooks(saved)
	stubPush(t)

	dir := newTestGitRepoWithOrigin(t, "https://gitlab.com/acme/platform/web.git")
	gitRun(t, dir, "checkout", "-b", "feature/login")

	var got platformPullRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/internal/workspace/ws-1/pulls" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("X-Workspace-Token") != "ws-secret" {
			t.Errorf("workspace token = %q", r.Header.Get("X-Workspace-Token"))
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"host":"gitlab.com","pull_request":{"number":7,"html_url":"https://gitlab.com/acme/platform/web/-/merge_requests/7","state":"open"}}`))
	}))
	defer srv.Close()
	platformEnv(t, srv)

	res, err := CreatePullRequest(context.Background(), dir, PullRequestRequest{Title: "Add login", Body: "Body", Base: "main"})
	if err != nil {
		t.Fatalf("CreatePullRequest: %v", err)
	}
	if res.Number != 7 || res.URL != "https://gitlab.com/acme/platform/web/-/merge_requests/7" || res.State != "open" {
		t.Fatalf("result = %+v", res)
	}
	if got.Title != "Add login" || got.HeadBranch != "feature/login" || got.BaseBranch != "main" {
		t.Fatalf("platform got %+v", got)
	}
}

func TestCreatePullRequest_ViaPlatform_RefusalIsFinal(t *testing.T) {
	saved := saveHooks()
	defer restoreHooks(saved)
	stubPush(t)
	ghCalled := false
	RunGhCommand = func(context.Context, string, ...string) ([]byte, error) {
		ghCalled = true
		return nil, nil
	}

	dir := newTestGitRepoWithOrigin(t, "https://github.com/acme/web.git")
	gitRun(t, dir, "checkout", "-b", "feature/x")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":"A pull request from this branch already exists.","code":"change_request_exists","existing_url":"https://github.com/acme/web/pull/3"}`))
	}))
	defer srv.Close()
	platformEnv(t, srv)

	_, err := CreatePullRequest(context.Background(), dir, PullRequestRequest{Title: "X", Base: "main"})
	if err == nil || !strings.Contains(err.Error(), "already exists") || !strings.Contains(err.Error(), "/pull/3") {
		t.Fatalf("err = %v, want the platform's refusal with the existing URL", err)
	}
	if ghCalled {
		t.Fatal("a platform answer must not fall through to gh")
	}
}

func TestCreatePullRequest_OtherHostOutsidePlatform(t *testing.T) {
	saved := saveHooks()
	defer restoreHooks(saved)
	stubPush(t)
	t.Setenv("PLATFORM_API_URL", "")
	t.Setenv("WORKSPACE_ID", "")
	t.Setenv("WORKSPACE_TOKEN", "")

	RunGhCommand = func(context.Context, string, ...string) ([]byte, error) {
		return []byte("none of the git remotes configured for this repository point to a known GitHub host"), errors.New("exit status 1")
	}

	dir := newTestGitRepoWithOrigin(t, "git@bitbucket.org:acme/web.git")
	gitRun(t, dir, "checkout", "-b", "feature/x")
	_, err := CreatePullRequest(context.Background(), dir, PullRequestRequest{Title: "X", Base: "main"})
	if err == nil || !strings.Contains(err.Error(), "bitbucket.org") || strings.Contains(err.Error(), "gh pr create") {
		t.Fatalf("err = %v, want a plain error naming bitbucket.org", err)
	}
}

func TestRemoteHost(t *testing.T) {
	cases := map[string]string{
		"https://github.com/o/r.git":        "github.com",
		"https://oauth2:x@GitLab.com/g/s/r": "gitlab.com",
		"git@bitbucket.org:w/r.git":         "bitbucket.org",
		"ssh://git@codeberg.org/o/r.git":    "codeberg.org",
	}
	for in, want := range cases {
		if got, ok := remoteHost(in); !ok || got != want {
			t.Errorf("remoteHost(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	for _, in := range []string{"/tmp/repo", "file:///tmp/repo", "../sibling"} {
		if got, ok := remoteHost(in); ok {
			t.Errorf("remoteHost(%q) = %q, want no host", in, got)
		}
	}
}
