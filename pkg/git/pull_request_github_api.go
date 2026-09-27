package git

// pull_request_github_api.go — the GitHub REST/gh-CLI backends, split out of
// pull_request.go. createPRViaAPI hits the GitHub REST API (REST-first path),
// createPRViaGH shells out to gh pr create (fallback), and buildFallbackGHCommand
// renders the exact manual command for the all-else-failed case.
import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

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
