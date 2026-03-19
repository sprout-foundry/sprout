# GitHub URL Handling Plan

GitHub pages are JS-heavy SPAs — both Jina Reader and direct HTTP GET produce poor results.
The GitHub REST API returns structured, machine-readable data. This plan adds a GitHub API
path that sits above Jina/direct in the routing hierarchy.

## 1. URL Detection — `isGitHubURL(url) bool`

| Pattern | Action |
|---|---|
| `github.com/{owner}/{repo}` | Repo metadata via API |
| `github.com/.../blob/{ref}/{path}` | Single raw file |
| `github.com/.../tree/{ref}/{path}` | Directory listing |
| `github.com/.../issues/{n}` or `/pull/{n}` | Issue/PR via API |
| `gist.github.com/{id}` | Gist via API |
| `raw.githubusercontent.com/...` | ← already works via `shouldBypassJina` → direct fetch |
| `api.github.com/...` | ← already works via `shouldBypassJina` → direct fetch |

Also strip `#L42-L56` line anchors from URL before routing.

## 2. API Endpoints

| Content | Endpoint | Accept Header |
|---|---|---|
| Repo metadata | `GET /repos/{owner}/{repo}` | `application/json` |
| Raw file | `GET /repos/.../contents/{path}?ref={ref}` | `application/vnd.github.raw+json` |
| Directory | `GET /repos/.../contents/{path}?ref={ref}` | `application/json` → `[{name,path,type,size}]` |
| README | `GET /repos/{owner}/{repo}/readme` | `application/vnd.github.raw+json` |
| Issue | `GET /repos/{owner}/{repo}/issues/{n}` | `application/json` |
| PR | `GET /repos/{owner}/{repo}/pulls/{n}` | `application/json` |
| Gist | `GET /gists/{id}` | `application/json` |

## 3. Authentication & Rate Limits

- **Token**: `GITHUB_TOKEN` env var via `os.Getenv("GITHUB_TOKEN")` — do NOT register in the API keys store
- Unauthenticated: 60 req/hr per IP. Authenticated: 5,000 req/hr per token.
- Check `X-RateLimit-Remaining` header; if ≤1, return error suggesting user set `GITHUB_TOKEN`.
- Public repos work without a token; private repos require `repo` scope.

## 4. Integration — `fetchContent` Routing

```go
func (w *WebContentFetcher) fetchContent(url string, cfg *configuration.Manager) (string, error) {
    // NEW: GitHub API path (highest priority for github.com URLs)
    if isGitHubURL(url) {
        if content, err := w.fetchWithGitHubAPI(url); err == nil {
            return content, nil
        }
        // Log warning, fall through to existing Jina/direct paths
    }

    // Existing: bypass Jina for JSON/static assets ...
    if w.shouldBypassJina(url) { return w.fetchDirectURL(url) }
    // Existing: Jina or direct fallback ...
}
```

**New files**: `github.go` + `github_test.go` in `pkg/webcontent/`.  
**New method**: `func (w *WebContentFetcher) fetchWithGitHubAPI(url string) (string, error)`  
Uses existing `w.httpClient` and `w.rateLimiter`. No new struct.

## 5. Output Format

Convert API JSON to the same plain-text format Jina produces. No markdown rendering — return as-is (LLM can read markdown).

- **Repo**: `# owner/repo\n⭐ N  |  Language: Go\nDescription...\n\n## README\n[raw README]`
- **File**: Full content with path header
- **Directory**: Tree-formatted listing with sizes

Apply existing `maxContentSize` (1MB) truncation at end, same as all other paths.

## 6. Edge Cases

| Case | Handling |
|---|---|
| Private repos | 404 without token → log "set GITHUB_TOKEN" |
| Wikis | API doesn't serve wiki — fall through to Jina |
| Large files (>1MB) | Contents API can't stream → construct `raw.githubusercontent.com` URL, direct fetch |
| Submodules | `"type":"submodule"` in contents → skip in tree output |
| Moved/renamed repos | GitHub API follows redirects automatically |

## 7. What NOT to Do

- Don't register GitHub as an AI provider in config
- Don't add GitHub-specific caching (existing `URLCache` handles it)
- Don't use GraphQL (REST is simpler and sufficient)
- Don't paginate aggressively — truncate and warn per existing pattern
