//go:build !js

package tools

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// searchDefaultLimit caps the files returned, bounding what reaches the
// model's context.
const searchDefaultLimit = 20

type searchHandler struct{}

func (h *searchHandler) Name() string { return "search" }

func (h *searchHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name: "search",
		Description: "Find code in the workspace with a regex search, one result per matching file. " +
			"Use a regex for exact matches ('func NewServer', 'SPROUT_[A-Z_]+'). A plain-language query of three or more words is reduced to its distinctive word stems and matched as alternatives.",
		Required: []string{"query"},
		Parameters: []ParameterDef{
			{Name: "query", Type: "string", Description: "A regex pattern, or a plain-language description whose distinctive words are matched.", Required: true},
			{Name: "directory", Type: "string", Description: "Directory to search (default: workspace root)"},
			{Name: "file_glob", Type: "string", Description: "Restrict matching to files matching this glob (e.g. '*.go')"},
			{Name: "case_sensitive", Type: "boolean", Description: "Case-sensitive matching (default: false)"},
			{Name: "max_results", Type: "integer", Description: "Maximum files to return (default: 20)"},
		},
	}
}

func (h *searchHandler) Validate(args map[string]any) error {
	return requireArgs(h.Name(), args, "query")
}

// searchCandidate is one matching file: its first match plus a count of the rest.
type searchCandidate struct {
	path         string
	line         int
	text         string
	extraMatches int
}

func (h *searchHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	query, err := extractString(args, "query")
	if err != nil {
		return ToolResult{Output: err.Error(), IsError: true}, nil
	}

	// Capture whether the user explicitly named a directory before
	// resolveSearchDirectory normalises it to the workspace root.
	rawDir, _ := extractString(args, "directory")

	directory, err := resolveSearchDirectory(rawDir, env.WorkspaceRoot)
	if err != nil {
		return ToolResult{Output: err.Error(), IsError: true}, nil
	}

	// Gate 1 precheck — only when the user explicitly named a non-default
	// directory. An empty or "." directory resolves to the workspace root,
	// which is already allowlisted; gating the default would prompt on every
	// vanilla search.
	if rawDir != "" && rawDir != "." {
		resolvedPath, decision := PrecheckFileAccess(ctx, env.FileAccessClassifier, "search", directory)
		if decision == "deny" {
			return ToolResult{Output: fmt.Sprintf("read blocked: %s is not accessible from this session", directory), IsError: true},
				fmt.Errorf("read blocked: %s is not accessible", directory)
		}
		if decision == "prompt" && env.FileAccessPrompter != nil {
			if ctx2, approved := promptForOffWorkspacePath(ctx, env, "search", directory, resolvedPath, "read"); approved {
				ctx = ctx2
			} else {
				return ToolResult{Output: fmt.Sprintf("read blocked: off-workspace access to %s was not approved", directory), IsError: true},
					fmt.Errorf("read blocked: off-workspace access to %s was not approved", directory)
			}
		}
	}

	limit, _ := extractInt(args, "max_results")
	if limit <= 0 {
		limit = searchDefaultLimit
	}

	fileGlob, _ := extractString(args, "file_glob")
	res, literalErr := runLiteralSearch(ctx, literalSearchOpts{
		Directory:     directory,
		Pattern:       literalPatternFor(query),
		FileGlob:      fileGlob,
		CaseSensitive: getBoolArg(args, "case_sensitive"),
		MaxFiles:      limit,
		MaxPerFile:    3,
	})

	results := groupSearchHits(res.Hits, env.WorkspaceRoot)
	if len(results) == 0 {
		return ToolResult{Output: formatEmptySearch(query, directory, literalErr)}, nil
	}
	return ToolResult{Output: formatSearchCandidates(query, results, res)}, nil
}

// groupSearchHits collapses line hits into one candidate per file, keeping
// walk order.
func groupSearchHits(hits []literalHit, workspaceRoot string) []searchCandidate {
	byPath := map[string]int{}
	var out []searchCandidate
	for _, h := range hits {
		path := normalizeSearchPath(h.Path, workspaceRoot)
		if i, ok := byPath[path]; ok {
			out[i].extraMatches++
			continue
		}
		byPath[path] = len(out)
		out = append(out, searchCandidate{path: path, line: h.Line, text: strings.TrimSpace(h.Text)})
	}
	return out
}

// Convert prose queries to a regex of distinctive word stems; pass through patterns as-is.
func literalPatternFor(query string) string {
	q := strings.TrimSpace(query)
	if q == "" {
		return q
	}
	if strings.ContainsAny(q, `\[](){}*+?|^$.`) {
		return q
	}
	fields := strings.Fields(q)
	if len(fields) <= 2 {
		return q
	}

	var stems []string
	seen := map[string]bool{}
	for _, w := range fields {
		w = strings.ToLower(strings.Trim(w, `"'.,:;!?`))
		if len(w) < 5 || searchStopwords[w] {
			continue
		}
		stem := w
		if len(stem) > 6 {
			stem = stem[:6]
		}
		if seen[stem] {
			continue
		}
		seen[stem] = true
		stems = append(stems, regexp.QuoteMeta(stem))
		if len(stems) == 6 {
			break
		}
	}
	if len(stems) == 0 {
		return q
	}
	return strings.Join(stems, "|")
}

// searchStopwords are words too common in prose to narrow a code search.
var searchStopwords = map[string]bool{
	"about": true, "after": true, "again": true, "against": true, "because": true,
	"before": true, "being": true, "between": true, "cannot": true, "could": true,
	"during": true, "every": true, "from": true, "given": true, "having": true,
	"into": true, "might": true, "other": true, "over": true, "should": true,
	"since": true, "some": true, "such": true, "than": true, "that": true,
	"their": true, "them": true, "then": true, "there": true, "these": true,
	"they": true, "this": true, "those": true, "through": true, "under": true,
	"until": true, "using": true, "were": true, "what": true, "when": true,
	"where": true, "which": true, "while": true, "with": true, "would": true,
	"your": true, "does": true, "each": true, "only": true, "same": true,
	"want": true, "will": true, "make": true, "made": true, "used": true,
}

func normalizeSearchPath(p, workspaceRoot string) string {
	if workspaceRoot == "" {
		return filepath.ToSlash(p)
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return filepath.ToSlash(p)
	}
	absRoot, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return filepath.ToSlash(p)
	}
	if rel, err := filepath.Rel(absRoot, abs); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(p)
}

func formatSearchCandidates(query string, results []searchCandidate, res literalResult) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Found %d file(s) for %q", len(results), query)
	if res.Truncated {
		fmt.Fprintf(&sb, " — showing %d of %d matching files; narrow the pattern or raise max_results", res.FilesShown, res.FilesMatched)
	}
	sb.WriteString(":\n\n")
	for _, r := range results {
		fmt.Fprintf(&sb, "%s:%d", r.path, r.line)
		if r.extraMatches > 0 {
			fmt.Fprintf(&sb, " +%d more in file", r.extraMatches)
		}
		sb.WriteString("\n")
		if t := strings.TrimSpace(r.text); t != "" {
			if len(t) > 200 {
				t = t[:200] + "…"
			}
			sb.WriteString("    " + t + "\n")
		}
	}
	return sb.String()
}

func formatEmptySearch(query, directory string, literalErr error) string {
	msg := fmt.Sprintf("No results for %q in %s.\n", query, directory)
	if literalErr != nil {
		msg += fmt.Sprintf("\nThe search could not complete: %v\n", literalErr)
	}
	return msg
}

func (h *searchHandler) Aliases() []string      { return nil }
func (h *searchHandler) Timeout() time.Duration { return 60 * time.Second }
func (h *searchHandler) MaxResultSize() int     { return 0 }
func (h *searchHandler) SafeForParallel() bool  { return true }
func (h *searchHandler) Interactive() bool      { return false }
