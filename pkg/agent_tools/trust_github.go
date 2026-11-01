package tools

// trust_github.go — deciding whether a repository is one the user owns or
// contributes to, so the trust question is not asked about their own code.
//
// The trust question exists because a clone may be someone else's work. But
// the common case in a developer's workspace is the opposite: a clone of a
// repository they own, or one they have push access to through an
// organization. Asking about those wastes a prompt and, worse, invites the
// wrong answer — framing your own code as untrusted data makes the model
// distrust its own output.
//
// So before asking, the repository's origin is resolved and checked against
// the identities the local tooling already knows: the authenticated GitHub
// account and the repositories it can push to. A repository the user owns or
// can push to is first-party without asking. Everything else — no remote, no
// credentials, a network failure, an unrecognized host — falls through to the
// question, whose default is third-party. A failure to prove ownership is
// never treated as ownership.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sprout-foundry/sprout/pkg/credentials"
)

const (
	// trustGitHubAPIBase is the REST root. A variable so tests can point it at
	// a local server.
	trustGitHubAPIBase = "https://api.github.com"

	// trustGitHubLookupTimeout bounds every outbound call made while
	// classifying a repository. This runs on the read path, so it must not
	// stall a tool call: a slow or unreachable API degrades to "ask" rather
	// than to "trust".
	trustGitHubLookupTimeout = 3 * time.Second

	// trustGitHubCacheTTL bounds how long a repository's ownership verdict is
	// reused. Long enough that a session asks the API once per repository,
	// short enough that a revoked grant is noticed within a working day.
	trustGitHubCacheTTL = 30 * time.Minute
)

// repoOwnership is the verdict for one repository: whether the user owns it or
// can push to it, and a short human-readable reason for diagnostics.
type repoOwnership struct {
	trusted bool
	reason  string
}

// trustGitHubClient performs the lookups. It is a struct rather than free
// functions so tests can inject a fake API base, token, and clock.
type trustGitHubClient struct {
	// apiBase overrides trustGitHubAPIBase in tests.
	apiBase string
	// token, when set, skips credential resolution.
	token string
	// now is the clock, for cache expiry in tests.
	now func() time.Time

	mu    sync.Mutex
	cache map[string]cachedOwnership
}

type cachedOwnership struct {
	verdict repoOwnership
	expires time.Time
}

// defaultTrustGitHub is the process-wide client. Its cache is what makes the
// per-read check cheap: a workspace full of files from one clone resolves the
// origin once.
var defaultTrustGitHub = &trustGitHubClient{
	apiBase: trustGitHubAPIBase,
	now:     time.Now,
}

// repoOwnedByUser reports whether the repository containing absPath is one the
// user owns or can push to. Best-effort: any failure to establish ownership
// returns false, which sends the caller to the question.
func repoOwnedByUser(absPath string) repoOwnership {
	return defaultTrustGitHub.ownedByUser(absPath)
}

func (c *trustGitHubClient) ownedByUser(absPath string) repoOwnership {
	origin, ok := gitOriginURL(absPath)
	if !ok {
		return repoOwnership{reason: "no origin remote"}
	}
	owner, repo, ok := parseGitHubRemote(origin)
	if !ok {
		return repoOwnership{reason: "origin is not a GitHub repository"}
	}
	key := owner + "/" + repo

	c.mu.Lock()
	if c.now == nil {
		c.now = time.Now
	}
	if entry, hit := c.cache[key]; hit && c.now().Before(entry.expires) {
		c.mu.Unlock()
		return entry.verdict
	}
	c.mu.Unlock()

	verdict := c.lookup(owner, repo)
	c.mu.Lock()
	if c.cache == nil {
		c.cache = make(map[string]cachedOwnership)
	}
	c.cache[key] = cachedOwnership{verdict: verdict, expires: c.now().Add(trustGitHubCacheTTL)}
	c.mu.Unlock()
	return verdict
}

// lookup asks GitHub about one repository. The cheap owner check runs first:
// a repository under the authenticated account's own login is certainly the
// user's. Otherwise the repository's permission block decides, which covers
// organization repositories the user can push to.
func (c *trustGitHubClient) lookup(owner, repo string) repoOwnership {
	ctx, cancel := context.WithTimeout(context.Background(), trustGitHubLookupTimeout)
	defer cancel()

	token := c.token
	if token == "" {
		token = trustGitHubToken()
	}
	if token == "" {
		return repoOwnership{reason: "no GitHub credentials"}
	}

	login, err := c.authenticatedLogin(ctx, token)
	if err != nil {
		return repoOwnership{reason: fmt.Sprintf("cannot resolve GitHub identity: %v", err)}
	}
	if strings.EqualFold(login, owner) {
		return repoOwnership{trusted: true, reason: "owned by " + login}
	}

	perm, err := c.repositoryPermission(ctx, token, owner, repo)
	if err != nil {
		return repoOwnership{reason: fmt.Sprintf("cannot read repository permissions: %v", err)}
	}
	if perm {
		return repoOwnership{trusted: true, reason: "push access to " + owner + "/" + repo}
	}
	return repoOwnership{reason: "no push access to " + owner + "/" + repo}
}

// authenticatedLogin returns the login of the account the token belongs to.
// The user endpoint needs no scopes beyond the token's own identity.
func (c *trustGitHubClient) authenticatedLogin(ctx context.Context, token string) (string, error) {
	var body struct {
		Login string `json:"login"`
	}
	if err := c.getJSON(ctx, token, "/user", &body); err != nil {
		return "", err
	}
	if body.Login == "" {
		return "", fmt.Errorf("empty login in /user response")
	}
	return body.Login, nil
}

// repositoryPermission reports whether the token can push to the repository.
// The permission block is the answer GitHub itself gives to "may this account
// write here", so it covers organization grants without enumerating them.
func (c *trustGitHubClient) repositoryPermission(ctx context.Context, token, owner, repo string) (bool, error) {
	var body struct {
		Permissions struct {
			Push   bool `json:"push"`
			Admin  bool `json:"admin"`
			Triage bool `json:"triage"`
			Pull   bool `json:"pull"`
		} `json:"permissions"`
	}
	if err := c.getJSON(ctx, token, "/repos/"+owner+"/"+repo, &body); err != nil {
		return false, err
	}
	return body.Permissions.Push || body.Permissions.Admin, nil
}

// getJSON performs one authenticated GET and decodes the response.
func (c *trustGitHubClient) getJSON(ctx context.Context, token, path string, out any) error {
	base := c.apiBase
	if base == "" {
		base = trustGitHubAPIBase
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "token "+token)

	resp, err := trustHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		// The body is not echoed: it can carry the token's scope details, and
		// the caller only needs to know the lookup did not settle the question.
		return fmt.Errorf("GET %s: %s", path, resp.Status)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out)
}

// trustHTTPClient is the client every ownership lookup uses. The timeout is
// belt-and-braces on top of the per-request context: a hung connection must
// not outlive the read that triggered it.
var trustHTTPClient = &http.Client{Timeout: trustGitHubLookupTimeout}

// githubToken resolves a token the same way the pull-request path does:
// the credential store first, then GH_TOKEN. Indirected so tests can decide
// "no credentials" without touching the developer's real store.
var trustGitHubToken = githubToken

func githubToken() string {
	if token, _, err := credentials.GetFromActiveBackend("github"); err == nil && token != "" {
		return token
	}
	return strings.TrimSpace(os.Getenv("GH_TOKEN"))
}

// gitOriginURL returns the repository's origin remote URL, resolved from the
// repository containing absPath. A worktree or a subdirectory both resolve to
// the same origin.
func gitOriginURL(absPath string) (string, bool) {
	dir := filepath.Dir(absPath)
	ctx, cancel := context.WithTimeout(context.Background(), trustGitHubLookupTimeout)
	defer cancel()
	out, err := trustRunGit(ctx, dir, "config", "--get", "remote.origin.url")
	if err != nil {
		return "", false
	}
	remote := strings.TrimSpace(string(out))
	if remote == "" {
		return "", false
	}
	return remote, true
}

// trustRunGit runs a read-only git command in dir. A variable so tests can drive
// the remote parsing without a real repository.
var trustRunGit = func(ctx context.Context, dir string, args ...string) ([]byte, error) {
	full := append([]string{"-C", dir}, args...)
	return exec.CommandContext(ctx, "git", full...).Output() //nolint:gosec // G204: args are fixed literals; dir is the resolved workspace path
}

// parseGitHubRemote extracts owner and repo from a git remote URL. It accepts
// the forms git writes: https, ssh://, and the scp-like git@host:owner/repo.
// A non-GitHub host returns false — a self-hosted forge is not something this
// check can make claims about.
func parseGitHubRemote(remote string) (owner, repo string, ok bool) {
	remote = strings.TrimSpace(remote)
	if remote == "" {
		return "", "", false
	}

	// scp-like syntax: [user@]host:path — no scheme, and the colon is not a
	// port separator because there is no "//" after it.
	if !strings.Contains(remote, "://") {
		at := strings.LastIndex(remote, "@")
		rest := remote
		if at >= 0 {
			rest = remote[at+1:]
		}
		colon := strings.Index(rest, ":")
		if colon < 0 {
			return "", "", false
		}
		host := rest[:colon]
		if !isGitHubHost(host) {
			return "", "", false
		}
		return splitOwnerRepo(rest[colon+1:])
	}

	parsed, err := url.Parse(remote)
	if err != nil {
		return "", "", false
	}
	if !isGitHubHost(parsed.Hostname()) {
		return "", "", false
	}
	return splitOwnerRepo(parsed.Path)
}

// isGitHubHost reports whether host is github.com or a subdomain of it.
func isGitHubHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	return host == "github.com" || strings.HasSuffix(host, ".github.com")
}

// splitOwnerRepo turns an owner/repo path into its two parts, trimming the
// leading slash, a trailing .git, and any trailing path segments.
func splitOwnerRepo(path string) (owner, repo string, ok bool) {
	path = strings.TrimPrefix(strings.TrimSpace(path), "/")
	path = strings.TrimSuffix(path, "/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		return "", "", false
	}
	owner = parts[0]
	repo = strings.TrimSuffix(parts[1], ".git")
	if owner == "" || repo == "" {
		return "", "", false
	}
	return owner, repo, true
}
