package webcontent

import (
	"fmt"
	"net/url"
	"strings"
)

// GitHubURLInfo holds structured information about a GitHub URL.
type GitHubURLInfo struct {
	Type   string // "repo", "file", "directory", "issue", "pull_request", "gist", "commit", "discussion", "actions_run", "release", "unknown"
	Owner  string
	Repo   string
	Ref    string
	Path   string
	Number int    // for issues/pulls/discussions/actions runs
	GistID string // for gists
}

// githubHosts are hosts that belong to the GitHub ecosystem but are NOT the
// main github.com UI (those are already handled for direct fetch elsewhere).
var githubHosts = []string{
	"github.com",
	"www.github.com",
}

// isGitHubURL returns true for URLs pointing to github.com (but NOT
// raw.githubusercontent.com or api.github.com, which are already
// handled by the existing direct-fetch logic).
func isGitHubURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}

	host := strings.ToLower(u.Hostname())
	for _, gh := range githubHosts {
		if host == gh {
			return true
		}
	}
	return false
}

// stripLineAnchor removes a GitHub line-number fragment such as "#L42"
// or "#L10-L25" from a URL. If there is no such anchor the original URL
// is returned unchanged. Query parameters are preserved.
func stripLineAnchor(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}

	if strings.HasPrefix(u.Fragment, "L") {
		// Only strip fragments that look like line anchors.
		// GitHub uses formats: #L42, #L10-L25
		rest := u.Fragment[1:] // e.g. "42", "10-L25"
		// Handle L10-L25 format: check after stripping optional L from second part
		parts := strings.SplitN(rest, "-", 2)
		if isDecimal(parts[0]) {
			if len(parts) == 2 {
				// Second part might be "25" or "L25"
				second := parts[1]
				if strings.HasPrefix(second, "L") {
					second = second[1:]
				}
				if isDecimal(second) {
					u.Fragment = ""
				}
			} else {
				u.Fragment = ""
			}
		}
	}

	return u.String()
}

// isDecimal returns true if s consists solely of ASCII digits.
func isDecimal(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return s != ""
}

// rewriteGitHubBlobToRaw converts a github.com/{owner}/{repo}/blob/{ref}/{path}
// URL into the equivalent raw.githubusercontent.com URL. Line anchors (e.g.
// #L42-L56) are stripped. Returns an empty string if the URL is not a
// recognised blob URL.
func rewriteGitHubBlobToRaw(rawURL string) string {
	// Strip line anchors first so the path segments are clean.
	cleaned := stripLineAnchor(rawURL)

	u, err := url.Parse(cleaned)
	if err != nil {
		return ""
	}

	host := strings.ToLower(u.Hostname())
	if host != "github.com" && host != "www.github.com" {
		return ""
	}

	// Path must start with "/owner/repo/blob/ref/..." (at least 5 segments).
	segments := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if len(segments) < 5 {
		return ""
	}

	owner := segments[0]
	repo := segments[1]
	kind := strings.ToLower(segments[2])
	if kind != "blob" {
		return ""
	}
	ref := segments[3]
	path := strings.Join(segments[4:], "/")

	// Reconstruct query params (raw.githubusercontent.com preserves them).
	query := u.RawQuery

	raw := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/%s", owner, repo, ref, path)
	if query != "" {
		raw += "?" + query
	}

	return raw
}

// ParseGitHubURL parses a GitHub URL into structured information about the
// resource it points to. For unrecognised patterns, Type is "unknown" and
// the remaining fields may be zero-valued.
func ParseGitHubURL(rawURL string) GitHubURLInfo {
	u, err := url.Parse(rawURL)
	if err != nil {
		return GitHubURLInfo{Type: "unknown"}
	}

	host := strings.ToLower(u.Hostname())
	path := strings.TrimPrefix(u.Path, "/")
	segments := strings.Split(path, "/")

	// --- Gist ---
	if host == "gist.github.com" && len(segments) >= 1 && segments[0] != "" {
		return GitHubURLInfo{
			Type:   "gist",
			GistID: segments[0],
		}
	}

	// --- github.com / www.github.com ---
	if host != "github.com" && host != "www.github.com" {
		return GitHubURLInfo{Type: "unknown"}
	}

	// Need at least owner/repo
	if len(segments) < 2 || segments[0] == "" || segments[1] == "" {
		return GitHubURLInfo{Type: "unknown"}
	}

	owner := segments[0]
	repo := segments[1]
	info := GitHubURLInfo{Owner: owner, Repo: repo}

	// Bare repo URL (no /tree, /blob, /issues, etc.)
	if len(segments) == 2 {
		info.Type = "repo"
		return info
	}

	// Third segment determines the resource type.
	kind := strings.ToLower(segments[2])

	switch kind {
	case "blob":
		// /owner/repo/blob/ref/path/to/file
		if len(segments) >= 5 {
			info.Type = "file"
			info.Ref = segments[3]
			info.Path = strings.Join(segments[4:], "/")
		} else if len(segments) == 4 {
			// /owner/repo/blob/ref — no file path, treat as directory at root
			info.Type = "directory"
			info.Ref = segments[3]
		} else {
			info.Type = "unknown"
		}

	case "tree":
		// /owner/repo/tree/ref/path
		if len(segments) >= 4 {
			info.Type = "directory"
			info.Ref = segments[3]
			if len(segments) > 4 {
				info.Path = strings.Join(segments[4:], "/")
			}
		} else {
			info.Type = "unknown"
		}

	case "issues", "issue":
		if len(segments) >= 4 {
			n := parsePositiveInt(segments[3])
			if n > 0 {
				info.Type = "issue"
				info.Number = n
			} else {
				info.Type = "unknown"
			}
		} else {
			info.Type = "unknown"
		}

	case "pull", "pulls":
		if len(segments) >= 4 {
			n := parsePositiveInt(segments[3])
			if n > 0 {
				info.Type = "pull_request"
				info.Number = n
			} else {
				info.Type = "unknown"
			}
		} else {
			// /owner/repo/pulls — list page, treat as repo
			info.Type = "repo"
		}

	case "commit":
		// /owner/repo/commit/sha
		if len(segments) >= 4 && segments[3] != "" {
			info.Type = "commit"
			info.Ref = segments[3]
		} else {
			info.Type = "unknown"
		}

	case "commits":
		// /owner/repo/commits — list page, treat as repo
		// /owner/repo/commits/sha — same as /commit/sha
		if len(segments) >= 4 && segments[3] != "" {
			info.Type = "commit"
			info.Ref = segments[3]
		} else {
			info.Type = "repo"
		}

	case "discussions", "discussion":
		if len(segments) >= 4 {
			n := parsePositiveInt(segments[3])
			if n > 0 {
				info.Type = "discussion"
				info.Number = n
			} else {
				info.Type = "unknown"
			}
		} else {
			info.Type = "unknown"
		}

	case "releases":
		// /owner/repo/releases/tag/v1.0 → release with tag
		// /owner/repo/releases/123 → release by numeric ID
		// /owner/repo/releases → list page, treat as repo
		if len(segments) >= 4 {
			if segments[3] == "tag" && len(segments) >= 5 && segments[4] != "" {
				info.Type = "release"
				info.Ref = segments[4]
			} else {
				n := parsePositiveInt(segments[3])
				if n > 0 {
					info.Type = "release"
					info.Number = n
				} else {
					info.Type = "unknown"
				}
			}
		} else {
			info.Type = "repo"
		}

	case "actions":
		// /owner/repo/actions/runs/12345
		if len(segments) >= 4 && segments[3] == "runs" && len(segments) >= 5 {
			n := parsePositiveInt(segments[4])
			if n > 0 {
				info.Type = "actions_run"
				info.Number = n
			} else {
				info.Type = "unknown"
			}
		} else {
			info.Type = "unknown"
		}

	default:
		// Unknown sub-path — could be a wiki, etc.
		info.Type = "unknown"
	}

	return info
}

// parsePositiveInt converts a string to a positive int, returning 0 on failure.
func parsePositiveInt(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0
		}
		n = n*10 + int(s[i]-'0')
	}
	if n == 0 {
		return 0
	}
	return n
}
