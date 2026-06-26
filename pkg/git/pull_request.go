package git

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// ErrNoGitHubAuth is the sentinel returned when neither a GH_TOKEN nor the gh
// CLI are available for PR creation.  The wrapped error message contains the
// exact gh pr create command the user can run manually.
var ErrNoGitHubAuth = errors.New("no GitHub authentication available")

// ---------------------------------------------------------------------------
// Public types
// ---------------------------------------------------------------------------

// PullRequestRequest describes a pull request to create.
type PullRequestRequest struct {
	Title     string // PR title (required)
	Body      string // PR body; synthesised from commits when empty
	Base      string // target branch; default = repo default branch
	Head      string // source branch; default = current HEAD branch
	Draft     bool
	Reviewers []string // usernames (API-only; ignored by gh CLI fallback)
}

// PullRequestResult holds the outcome of a successful PR creation.
type PullRequestResult struct {
	URL    string `json:"html_url"`
	Number int    `json:"number"`
	State  string `json:"state"` // "open" or "closed"
}

// ---------------------------------------------------------------------------
// Package-level hooks for testability
// ---------------------------------------------------------------------------

// RunGhCommand is the function used to invoke the gh CLI.  The default
// implementation simply calls exec.CommandContext("gh", args...).  Tests may
// override this variable to avoid a real binary.
//
// NOTE: this variable is intentionally not safe for concurrent writes —
// override it in init() or TestMain, not mid-flight.
var RunGhCommand = func(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return out, err
}

// prHTTPClient is the HTTP client used for GitHub REST API calls.  Tests
// may override it to use httptest.Server.
var prHTTPClient = http.DefaultClient

// GitHubAPIBaseURL is the base URL for the GitHub REST API.  Tests may
// override it to point at an httptest server.
var GitHubAPIBaseURL = "https://api.github.com"

// GetDefaultBranch returns the default branch name for the repo rooted at
// repoDir.  By default it reads
//
//	git symbolic-ref refs/remotes/origin/HEAD
//
// and falls back to "main".  Tests may override this variable.
var GetDefaultBranch = func(ctx context.Context, repoDir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "symbolic-ref", "--short", "refs/remotes/origin/HEAD")
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		return "main", nil // graceful fallback
	}
	return strings.TrimSpace(string(out)), nil
}

// ---------------------------------------------------------------------------
// Exported helpers
// ---------------------------------------------------------------------------

// ParseGitHubRemoteURL extracts the owner and repository name from a GitHub
// remote URL.  It supports both HTTPS and SSH formats:
//
//	https://github.com/owner/repo.git     -> owner, repo
//	git@github.com:owner/repo.git         -> owner, repo
//
// Returns an error for non-GitHub remotes or unrecognised formats.
func ParseGitHubRemoteURL(remoteURL string) (owner, repo string, err error) {
	// HTTPS format
	if strings.HasPrefix(remoteURL, "https://github.com/") || strings.HasPrefix(remoteURL, "http://github.com/") {
		trimmed := strings.TrimPrefix(remoteURL, "https://github.com/")
		trimmed = strings.TrimPrefix(trimmed, "http://github.com/")
		return splitOwnerRepo(trimmed)
	}

	// SSH format: git@github.com:owner/repo.git
	if strings.HasPrefix(remoteURL, "git@github.com:") {
		trimmed := strings.TrimPrefix(remoteURL, "git@github.com:")
		return splitOwnerRepo(trimmed)
	}

	return "", "", fmt.Errorf("unsupported git remote URL format: %q", remoteURL)
}

// splitOwnerRepo extracts "owner/repo" from a raw path like "owner/repo.git".
func splitOwnerRepo(raw string) (string, string, error) {
	// Strip .git suffix.
	raw = strings.TrimSuffix(raw, ".git")

	parts := strings.SplitN(raw, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("failed to parse owner/repo from %q", raw)
	}
	return parts[0], parts[1], nil
}

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

// CreatePullRequest creates a pull request on GitHub for the given repoDir.
//
// Resolution order:
//
//  1. GitHub REST API (requires GH_TOKEN env var)
//  2. gh pr create shell-out (fallback when no token)
//  3. Structured error with the exact gh command the user can run manually
//
// If req.Head is empty, the current branch name is used.
// If req.Base is empty, the repository's default branch is inferred.
// If req.Body is empty, a body is synthesised from commit messages between
// base and head.
func CreatePullRequest(ctx context.Context, repoDir string, req PullRequestRequest) (*PullRequestResult, error) {
	// --- Resolve head / base ---
	head, err := resolveHead(ctx, repoDir, req.Head)
	if err != nil {
		return nil, fmt.Errorf("resolve head branch: %w", err)
	}
	base, err := resolveBase(ctx, repoDir, req.Base)
	if err != nil {
		return nil, fmt.Errorf("resolve base branch: %w", err)
	}

	// --- Synthesise body if empty ---
	body := req.Body
	if body == "" {
		body, err = synthesizePRBody(ctx, repoDir, base, head)
		if err != nil {
			// Non-fatal: proceed with empty body
			body = ""
		}
	}

	// --- Auto-push head if it has no upstream (best-effort) ---
	var pushErr error
	if pushErr = ensureBranchPushed(ctx, repoDir, head); pushErr != nil {
		// Best-effort: still try the API and gh CLI below.
		// If everything else fails, the final error will mention the push problem too.
	}

	// --- Try GitHub REST API first ---
	// TODO(SP-004): Check credential store before falling back to GH_TOKEN
	token := os.Getenv("GH_TOKEN")
	var apiErr error
	if token != "" {
		owner, repo, getErr := getOwnerRepo(ctx, repoDir)
		if getErr == nil {
			result, createErr := createPRViaAPI(ctx, owner, repo, head, base, req.Title, body, req.Draft, token)
			if createErr == nil {
				return result, nil
			}
			// 401 -> fall through to gh CLI
			if !isHTTPError(createErr, http.StatusUnauthorized) {
				return nil, createErr
			}
			apiErr = createErr
		} else {
			apiErr = getErr
		}
		// If we couldn't resolve owner/repo, fall through to gh
	}

	// --- Fallback: gh pr create ---
	result, ghErr := createPRViaGH(ctx, repoDir, head, base, req.Title, body, req.Draft)
	if ghErr == nil {
		return result, nil
	}

	// --- Both failed — build structured error ---
	fallbackCmd := buildFallbackGHCommand(head, base, req.Title, body, req.Draft)
	details := fmt.Sprintf("\n\t%s", fallbackCmd)
	if apiErr != nil {
		details += fmt.Sprintf("\n\nAPI error: %s", apiErr)
	}
	details += fmt.Sprintf("\ngh error: %s", ghErr)
	if pushErr != nil {
		details += fmt.Sprintf("\npush error: %s", pushErr)
	}
	return nil, fmt.Errorf("%w:%s", ErrNoGitHubAuth, details)
}

// ---------------------------------------------------------------------------
// Branch resolution
// ---------------------------------------------------------------------------

// resolveHead returns the source branch name.  If given, use it; otherwise
// read the current branch from git.
func resolveHead(ctx context.Context, repoDir string, head string) (string, error) {
	if head != "" {
		return head, nil
	}
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("get current branch: %w", err)
	}
	branch := strings.TrimSpace(string(out))
	if branch == "" || branch == "HEAD" {
		return "", errors.New("no current branch (detached HEAD)")
	}
	return branch, nil
}

// resolveBase returns the target branch name.  If given, use it; otherwise
// infer the repository's default branch.
func resolveBase(ctx context.Context, repoDir string, base string) (string, error) {
	if base != "" {
		return base, nil
	}
	return GetDefaultBranch(ctx, repoDir)
}

// getOwnerRepo parses the origin remote URL and returns owner/repo for GitHub
// remotes.  Returns an error for non-GitHub remotes.
func getOwnerRepo(ctx context.Context, repoDir string) (owner, repo string, err error) {
	cmd := exec.CommandContext(ctx, "git", "remote", "get-url", "origin")
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		return "", "", fmt.Errorf("get remote URL: %w", err)
	}
	remoteURL := strings.TrimSpace(string(out))
	return ParseGitHubRemoteURL(remoteURL)
}

// ---------------------------------------------------------------------------
// Body synthesis
// ---------------------------------------------------------------------------

// synthesizePRBody collects commit subjects between base..head and joins
// them as a bullet list.
func synthesizePRBody(ctx context.Context, repoDir, base, head string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "log", "--format=%s", fmt.Sprintf("%s..%s", base, head))
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git log for body synthesis: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var subjects []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" {
			subjects = append(subjects, l)
		}
	}
	if len(subjects) == 0 {
		return "", nil
	}

	var buf strings.Builder
	buf.WriteString("## Commits\n\n")
	for _, s := range subjects {
		buf.WriteString(fmt.Sprintf("- %s\n", s))
	}
	return buf.String(), nil
}

// ---------------------------------------------------------------------------
// Auto-push
// ---------------------------------------------------------------------------

// PushBranch executes "git push -u origin <head>".  Tests may override this
// variable to avoid network access.
//
// NOTE: intentionally exported only for testability — follow the same
// pattern as RunGhCommand / GetDefaultBranch.  Not safe for concurrent
// writes; override in init() or TestMain, not mid-flight.
var PushBranch = func(ctx context.Context, repoDir, head string) error {
	cmd := exec.CommandContext(ctx, "git", "push", "-u", "origin", head)
	cmd.Dir = repoDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("push branch %q to origin: %w (output: %s)", head, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// ensureBranchPushed pushes the head branch with upstream tracking if it
// doesn't already have an upstream.
func ensureBranchPushed(ctx context.Context, repoDir, head string) error {
	// Check if @{upstream} exists
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--abbrev-ref", fmt.Sprintf("%s@{upstream}", head))
	cmd.Dir = repoDir
	out, err := cmd.CombinedOutput()
	if err == nil && strings.TrimSpace(string(out)) != "HEAD" {
		// Upstream is set — no need to push.
		return nil
	}

	// Push with upstream tracking
	return PushBranch(ctx, repoDir, head)
}

// ---------------------------------------------------------------------------
// GitHub REST API
// ---------------------------------------------------------------------------

type createPRRequest struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Head  string `json:"head"`
	Base  string `json:"base"`
	Draft bool   `json:"draft,omitempty"`
}

type prAPIResponse struct {
	HTMLURL string `json:"html_url"`
	Number  int    `json:"number"`
	State   string `json:"state"`
	Message string `json:"message,omitempty"`
}

func createPRViaAPI(ctx context.Context, owner, repo, head, base, title, body string, draft bool, token string) (*PullRequestResult, error) {
	reqBody := createPRRequest{
		Title: title,
		Body:  body,
		Head:  head,
		Base:  base,
		Draft: draft,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal PR request: %w", err)
	}

	urlStr := fmt.Sprintf("%s/repos/%s/%s/pulls", GitHubAPIBaseURL, owner, repo)
	req, err := http.NewRequestWithContext(ctx, "POST", urlStr, bytes.NewReader(jsonData))
	if err != nil {
		return nil, fmt.Errorf("create HTTP request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "token "+token)
	req.Header.Set("User-Agent", "sprout-agent")
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := prHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request to GitHub API: %w", err)
	}
	defer resp.Body.Close()

	// Read body for error messages
	respBody, _ := io.ReadAll(resp.Body)

	switch resp.StatusCode {
	case http.StatusCreated:
		var pr prAPIResponse
		if err := json.Unmarshal(respBody, &pr); err != nil {
			return nil, fmt.Errorf("parse PR API response (HTTP %d): %w", resp.StatusCode, err)
		}
		return &PullRequestResult{
			URL:    pr.HTMLURL,
			Number: pr.Number,
			State:  pr.State,
		}, nil

	case http.StatusUnprocessableEntity:
		// PR may already exist
		var pr prAPIResponse
		_ = json.Unmarshal(respBody, &pr)
		return nil, fmt.Errorf("GitHub API 422: %s (response: %s)", pr.Message, string(respBody))

	case http.StatusUnauthorized:
		return nil, httpError(resp.StatusCode, "GitHub API 401: invalid or expired token")

	default:
		return nil, httpError(resp.StatusCode, fmt.Sprintf("GitHub API returned HTTP %d: %s", resp.StatusCode, string(respBody)))
	}
}

// httpErrorStatus wraps an HTTP error code with a message so callers
// can inspect the code via errors.As.
type httpErrorStatus struct {
	Code    int
	Message string
}

func (e *httpErrorStatus) Error() string { return e.Message }

func httpError(code int, msg string) *httpErrorStatus {
	return &httpErrorStatus{Code: code, Message: msg}
}

func isHTTPError(err error, code int) bool {
	var e *httpErrorStatus
	return errors.As(err, &e) && e.Code == code
}

// ---------------------------------------------------------------------------
// gh CLI fallback
// ---------------------------------------------------------------------------

func createPRViaGH(ctx context.Context, repoDir, head, base, title, body string, draft bool) (*PullRequestResult, error) {
	args := []string{"pr", "create"}
	args = append(args, "--title", title)
	args = append(args, "--body", body)
	args = append(args, "--base", base)
	args = append(args, "--head", head)
	if draft {
		args = append(args, "--draft")
	}

	out, err := RunGhCommand(ctx, repoDir, args...)
	if err != nil {
		return nil, fmt.Errorf("gh pr create: %w", err)
	}

	prURL, number := extractPRURLAndNumber(string(out))
	if prURL == "" {
		prURL = strings.TrimSpace(string(out))
	}

	return &PullRequestResult{
		URL:    prURL,
		Number: number,
		State:  "open",
	}, nil
}

// extractPRURLAndNumber pulls the PR URL and its number from gh CLI output.
// gh outputs a line like: https://github.com/owner/repo/pull/42
func extractPRURLAndNumber(output string) (url string, number int) {
	prURLRe := regexp.MustCompile(`https://github\.com/[^/]+/[^/]+/pull/(\d+)`)
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		matches := prURLRe.FindStringSubmatch(line)
		if matches != nil {
			urlStart := strings.Index(line, "https://")
			if urlStart >= 0 {
				url = strings.TrimSpace(line[urlStart:])
			}
			number, _ = strconv.Atoi(matches[1]) // regex guarantees digits
			return url, number
		}
	}
	return "", 0
}

// ---------------------------------------------------------------------------
// Fallback command builder
// ---------------------------------------------------------------------------

// buildFallbackGHCommand returns the exact gh pr create command for the user
// to run manually when all programmatic paths fail.
func buildFallbackGHCommand(head, base, title, body string, draft bool) string {
	titleEsc := shellQuote(title)
	bodyEsc := shellQuote(body)
	baseEsc := shellQuote(base)
	headEsc := shellQuote(head)

	cmd := fmt.Sprintf("gh pr create --title %s --body %s --base %s --head %s",
		titleEsc, bodyEsc, baseEsc, headEsc)
	if draft {
		cmd += " --draft"
	}
	return cmd
}

// shellQuote returns a single-quoted, shell-safe representation of s.
// Handles the classic '\” escaping pattern.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
