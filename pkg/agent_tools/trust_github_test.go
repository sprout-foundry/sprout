package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestParseGitHubRemote covers the URL forms git actually writes. The scp-like
// form is the one that breaks naive parsers: it has a colon but no scheme, so
// treating the colon as a port separator yields "owner/repo" as a host.
func TestParseGitHubRemote(t *testing.T) {
	cases := []struct {
		remote    string
		wantOwner string
		wantRepo  string
		wantOK    bool
	}{
		{"https://github.com/sprout-foundry/sprout.git", "sprout-foundry", "sprout", true},
		{"https://github.com/sprout-foundry/sprout", "sprout-foundry", "sprout", true},
		{"git@github.com:sprout-foundry/sprout.git", "sprout-foundry", "sprout", true},
		{"git@github.com:sprout-foundry/sprout", "sprout-foundry", "sprout", true},
		{"ssh://git@github.com/sprout-foundry/sprout.git", "sprout-foundry", "sprout", true},
		{"https://github.com/sprout-foundry/sprout/", "sprout-foundry", "sprout", true},
		// Not GitHub: a self-hosted forge is not something this check can
		// make claims about, so it must not be treated as one.
		{"https://gitlab.com/sprout-foundry/sprout.git", "", "", false},
		{"git@gitlab.com:sprout-foundry/sprout.git", "", "", false},
		{"https://git.example.com/sprout-foundry/sprout.git", "", "", false},
		// Malformed / incomplete.
		{"", "", "", false},
		{"https://github.com/onlyowner", "", "", false},
		{"git@github.com:onlyowner", "", "", false},
		{"not a url at all", "", "", false},
	}
	for _, tc := range cases {
		owner, repo, ok := parseGitHubRemote(tc.remote)
		if ok != tc.wantOK || owner != tc.wantOwner || repo != tc.wantRepo {
			t.Errorf("parseGitHubRemote(%q) = (%q, %q, %v); want (%q, %q, %v)",
				tc.remote, owner, repo, ok, tc.wantOwner, tc.wantRepo, tc.wantOK)
		}
	}
}

// fakeGitHub serves the two endpoints the ownership lookup uses.
func fakeGitHub(t *testing.T, login string, push bool, calls *int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(calls, 1)
		if got := r.Header.Get("Authorization"); got != "token test-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case r.URL.Path == "/user":
			_ = json.NewEncoder(w).Encode(map[string]string{"login": login})
		case strings.HasPrefix(r.URL.Path, "/repos/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"permissions": map[string]bool{"push": push, "admin": false, "pull": true},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// stubOrigin makes the origin lookup return remote for every path, so the
// ownership logic can be exercised without a real repository on disk.
func stubOrigin(t *testing.T, remote string) {
	t.Helper()
	prev := trustRunGit
	trustRunGit = func(ctx context.Context, dir string, args ...string) ([]byte, error) {
		return []byte(remote + "\n"), nil
	}
	t.Cleanup(func() { trustRunGit = prev })
}

func newTestClient(apiBase string) *trustGitHubClient {
	return &trustGitHubClient{
		apiBase: apiBase,
		token:   "test-token",
		now:     time.Now,
	}
}

// TestOwnedByUser_OwnAccountRepository is the case that removes most prompts:
// a clone of a repository under the user's own login.
func TestOwnedByUser_OwnAccountRepository(t *testing.T) {
	var calls int32
	srv := fakeGitHub(t, "alantheprice", false, &calls)
	stubOrigin(t, "git@github.com:alantheprice/sprout.git")

	got := newTestClient(srv.URL).ownedByUser("/tmp/ws/sprout/file.go")
	if !got.trusted {
		t.Fatalf("own-account repository not trusted: %s", got.reason)
	}
	if calls != 1 {
		t.Errorf("API calls = %d, want 1 (the owner check settles it without a permissions read)", calls)
	}
}

// TestOwnedByUser_OrgRepositoryWithPushAccess covers the "an org they have
// access to" half: a repository owned by an organization the user can push to.
func TestOwnedByUser_OrgRepositoryWithPushAccess(t *testing.T) {
	var calls int32
	srv := fakeGitHub(t, "alantheprice", true, &calls)
	stubOrigin(t, "https://github.com/sprout-foundry/sprout.git")

	got := newTestClient(srv.URL).ownedByUser("/tmp/ws/sprout/file.go")
	if !got.trusted {
		t.Fatalf("org repository with push access not trusted: %s", got.reason)
	}
}

// TestOwnedByUser_ReadOnlyCloneIsNotTrusted is the case the question exists
// for: someone else's repository, which the user can only read. It must fall
// through to the question rather than being silently trusted.
func TestOwnedByUser_ReadOnlyCloneIsNotTrusted(t *testing.T) {
	var calls int32
	srv := fakeGitHub(t, "alantheprice", false, &calls)
	stubOrigin(t, "https://github.com/someone-else/their-lib.git")

	got := newTestClient(srv.URL).ownedByUser("/tmp/ws/their-lib/file.go")
	if got.trusted {
		t.Fatalf("read-only clone of a foreign repository was trusted: %s", got.reason)
	}
}

// TestOwnedByUser_FailuresAreNotTrust is the safety property: every way the
// lookup can fail resolves to "not trusted", never to "trusted".
func TestOwnedByUser_FailuresAreNotTrust(t *testing.T) {
	t.Run("no origin", func(t *testing.T) {
		prev := trustRunGit
		trustRunGit = func(context.Context, string, ...string) ([]byte, error) {
			return nil, context.DeadlineExceeded
		}
		t.Cleanup(func() { trustRunGit = prev })

		if got := newTestClient(trustGitHubAPIBase).ownedByUser("/tmp/x/file.go"); got.trusted {
			t.Fatalf("trusted with no origin: %s", got.reason)
		}
	})

	t.Run("non-github origin", func(t *testing.T) {
		stubOrigin(t, "https://gitlab.com/me/mine.git")
		if got := newTestClient(trustGitHubAPIBase).ownedByUser("/tmp/x/file.go"); got.trusted {
			t.Fatalf("trusted a non-GitHub origin: %s", got.reason)
		}
	})

	t.Run("api rejects the token", func(t *testing.T) {
		var calls int32
		srv := fakeGitHub(t, "alantheprice", true, &calls)
		stubOrigin(t, "https://github.com/alantheprice/mine.git")
		c := newTestClient(srv.URL)
		c.token = "stale-token"

		if got := c.ownedByUser("/tmp/x/file.go"); got.trusted {
			t.Fatalf("trusted despite a rejected token: %s", got.reason)
		}
	})

	t.Run("no credentials", func(t *testing.T) {
		stubOrigin(t, "https://github.com/alantheprice/mine.git")
		c := newTestClient(trustGitHubAPIBase)
		c.token = ""
		prevToken := trustGitHubToken
		trustGitHubToken = func() string { return "" }
		t.Cleanup(func() { trustGitHubToken = prevToken })

		if got := c.ownedByUser("/tmp/x/file.go"); got.trusted {
			t.Fatalf("trusted with no credentials: %s", got.reason)
		}
	})
}

// TestOwnedByUser_CachesTheVerdict pins that a workspace full of files from
// one clone resolves the origin once, not once per read.
func TestOwnedByUser_CachesTheVerdict(t *testing.T) {
	var calls int32
	srv := fakeGitHub(t, "alantheprice", true, &calls)
	stubOrigin(t, "https://github.com/sprout-foundry/sprout.git")
	c := newTestClient(srv.URL)

	for i := 0; i < 5; i++ {
		if got := c.ownedByUser("/tmp/ws/sprout/file.go"); !got.trusted {
			t.Fatalf("read %d not trusted: %s", i, got.reason)
		}
	}
	// /user + /repos, once.
	if calls != 2 {
		t.Errorf("API calls = %d for 5 reads of one repository; want 2 (cached)", calls)
	}
}

// TestOwnedByUser_CacheExpires ensures a revoked grant is re-checked rather
// than cached forever.
func TestOwnedByUser_CacheExpires(t *testing.T) {
	var calls int32
	srv := fakeGitHub(t, "alantheprice", true, &calls)
	stubOrigin(t, "https://github.com/sprout-foundry/sprout.git")

	now := time.Now()
	c := newTestClient(srv.URL)
	c.now = func() time.Time { return now }

	if got := c.ownedByUser("/tmp/ws/sprout/file.go"); !got.trusted {
		t.Fatalf("first read not trusted: %s", got.reason)
	}
	before := atomic.LoadInt32(&calls)

	now = now.Add(trustGitHubCacheTTL + time.Minute)
	if got := c.ownedByUser("/tmp/ws/sprout/file.go"); !got.trusted {
		t.Fatalf("second read not trusted: %s", got.reason)
	}
	if atomic.LoadInt32(&calls) == before {
		t.Error("expired verdict was not re-checked")
	}
}
