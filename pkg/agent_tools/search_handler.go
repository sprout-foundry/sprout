//go:build !js

package tools

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/embedding"
)

// rrfK is the reciprocal-rank-fusion damping constant. 60 is the value from the
// original RRF paper and the usual default; it flattens the contribution of
// top ranks enough that one strategy's confident-but-wrong #1 cannot bury the
// other strategy's correct #2.
const rrfK = 60.0

// searchDefaultLimit caps merged results. Both inputs are already capped; this
// bounds what reaches the model's context.
const searchDefaultLimit = 20

type searchHandler struct{}

func (h *searchHandler) Name() string { return "search" }

func (h *searchHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name: "search",
		Description: "Find code in the workspace. Runs a literal regex search and, when the embedding index is available, a semantic search, then merges both rankings. " +
			"Use a regex for exact matches ('func NewServer', 'SPROUT_[A-Z_]+') and plain language for conceptual questions ('where do we retry failed connections') — both work, and a conceptual query still returns literal matches if the index is not built yet.",
		Required: []string{"query"},
		Parameters: []ParameterDef{
			{Name: "query", Type: "string", Description: "A regex pattern for exact matching, or a plain-language description of the behaviour you are looking for.", Required: true},
			{Name: "directory", Type: "string", Description: "Directory to search (default: workspace root)"},
			{Name: "file_glob", Type: "string", Description: "Restrict literal matching to files matching this glob (e.g. '*.go')"},
			{Name: "case_sensitive", Type: "boolean", Description: "Case-sensitive literal matching (default: false)"},
			{Name: "max_results", Type: "integer", Description: "Maximum merged results to return (default: 20)"},
			{Name: "literal_only", Type: "boolean", Description: "Skip semantic search even when the index is available. Use when you need an exhaustive, exact answer — e.g. verifying every reference to a symbol is gone."},
		},
	}
}

func (h *searchHandler) Validate(args map[string]any) error {
	return requireArgs(h.Name(), args, "query")
}

// searchCandidate is one result from either strategy, keyed for merging.
type searchCandidate struct {
	path         string
	line         int
	text         string
	score        float64
	fromParts    []string
	similarity   float32
	extraMatches int
}

func (h *searchHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	query, err := extractString(args, "query")
	if err != nil {
		return ToolResult{Output: err.Error(), IsError: true}, nil
	}

	directory, _ := extractString(args, "directory")
	directory, err = resolveSearchDirectory(directory, env.WorkspaceRoot)
	if err != nil {
		return ToolResult{Output: err.Error(), IsError: true}, nil
	}
	limit, _ := extractInt(args, "max_results")
	if limit <= 0 {
		limit = searchDefaultLimit
	}
	literalOnly := getBoolArg(args, "literal_only")

	// --- literal pass: always runs ---
	// It is exact, fast, needs no index, and reads the working tree as it is
	// right now rather than as of the last index build. Everything else is
	// additive to it, which is what makes degradation graceful rather than a
	// mode switch.
	fileGlob, _ := extractString(args, "file_glob")
	literalRes, literalErr := runLiteralSearch(ctx, literalSearchOpts{
		Directory:     directory,
		Pattern:       literalPatternFor(query),
		FileGlob:      fileGlob,
		CaseSensitive: getBoolArg(args, "case_sensitive"),
		MaxFiles:      limit * 5, // room to backfill behind the semantic list
		MaxPerFile:    3,
	})
	literalHits := literalRes.Hits

	// --- semantic pass: only when the index can actually answer ---
	var semanticHits []embedding.QueryResult
	var semanticNote string
	switch {
	case literalOnly:
		semanticNote = "literal-only (requested)"
	case env.EmbeddingMgr == nil:
		semanticNote = "literal-only (no embedding index in this session)"
	default:
		r := env.EmbeddingMgr.Readiness()
		switch {
		case r.CanAnswerQueries():
			semanticHits, _ = env.EmbeddingMgr.QuerySimilar(ctx, query, limit*2,
				embedding.DefaultSemanticSearchThreshold)
			if r.Building {
				semanticNote = fmt.Sprintf("semantic results partial — index still building (%d records)", r.Records)
			}
		case r.Building:
			semanticNote = fmt.Sprintf("literal-only — embedding index still building (%d records)", r.Records)
		default:
			semanticNote = "literal-only — embedding index not built for this workspace"
		}
	}

	merged := fuseSearchResults(literalHits, semanticHits, env.WorkspaceRoot, limit)

	if len(merged) == 0 {
		return ToolResult{Output: formatEmptySearch(query, directory, semanticNote, literalErr)}, nil
	}
	return ToolResult{Output: formatFusedSearch(query, merged, semanticNote)}, nil
}

// fuseSearchResults merges the two result sets.
//
// NOT reciprocal rank fusion, despite that being the obvious choice. RRF
// assumes both inputs are ranked by relevance; grep output is ordered by
// directory walk, so its "rank 1" carries no information. Measured on the
// held-out set, RRF-fusing the two scored 7/14 — worse than semantic alone at
// 10/14 — because an incidental TODO.md match at walk-position 1 outscored a
// correct semantic hit at rank 3.
//
// Instead the semantic ranking leads and literal matches backfill behind it.
// Semantic order is meaningful, so it is preserved; literal results still
// contribute everything semantic missed, which is the whole point of running
// both. A file found by both is promoted to the semantic position and marked,
// since agreement is the strongest signal available.
func fuseSearchResults(literal []literalHit, semantic []embedding.QueryResult, workspaceRoot string, limit int) []searchCandidate {
	byPath := map[string]*searchCandidate{}
	var ordered []*searchCandidate

	for _, r := range semantic {
		path := normalizeSearchPath(r.Record.File, workspaceRoot)
		c, ok := byPath[path]
		if !ok {
			c = &searchCandidate{path: path, line: r.Record.StartLine, text: strings.TrimSpace(r.Record.Signature)}
			byPath[path] = c
			ordered = append(ordered, c)
		}
		if r.Similarity > c.similarity {
			c.similarity = r.Similarity
		}
		c.fromParts = appendOnce(c.fromParts, "semantic")
		if c.text == "" {
			c.text = r.Record.Name
		}
	}

	// Literal matches: annotate files semantic already found, append the rest.
	var backfill []*searchCandidate
	for _, h := range literal {
		path := normalizeSearchPath(h.Path, workspaceRoot)
		if c, ok := byPath[path]; ok {
			before := len(c.fromParts)
			c.fromParts = appendOnce(c.fromParts, "literal")
			if len(c.fromParts) == before {
				c.extraMatches++
			}
			continue
		}
		c := &searchCandidate{path: path, line: h.Line, text: strings.TrimSpace(h.Text)}
		c.fromParts = append(c.fromParts, "literal")
		byPath[path] = c
		backfill = append(backfill, c)
	}

	out := make([]searchCandidate, 0, len(ordered)+len(backfill))
	for _, c := range ordered {
		out = append(out, *c)
	}
	for _, c := range backfill {
		out = append(out, *c)
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// literalPatternFor turns the caller's query into something worth grepping.
//
// A conceptual query is a terrible regex — "write a file atomically so a crash
// cannot leave it half written" matches nothing — so the literal pass
// contributed nothing on exactly the queries where it was supposed to be the
// safety net. When the query reads as prose rather than a pattern, this ORs the
// distinctive words instead, truncated to a stem so "atomically" still matches
// "WriteFileAtomic".
//
// Queries that already look like patterns (regex metacharacters, or a single
// token) are passed through untouched: the caller meant them literally, and
// rewriting an exact search is the one thing that must never happen here.
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

func appendOnce(s []string, v string) []string {
	for _, e := range s {
		if e == v {
			return s
		}
	}
	return append(s, v)
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

func formatFusedSearch(query string, results []searchCandidate, note string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d result(s) for %q", len(results), query))
	if note != "" {
		sb.WriteString(" — " + note)
	}
	sb.WriteString(":\n\n")
	for _, r := range results {
		sb.WriteString(fmt.Sprintf("%s:%d", r.path, r.line))
		if len(r.fromParts) > 0 {
			sb.WriteString(" [" + strings.Join(r.fromParts, "+") + "]")
		}
		if r.similarity > 0 {
			sb.WriteString(fmt.Sprintf(" (%.2f)", r.similarity))
		}
		if r.extraMatches > 0 {
			sb.WriteString(fmt.Sprintf(" +%d more in file", r.extraMatches))
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

func formatEmptySearch(query, directory, note string, literalErr error) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("No results for %q in %s.\n", query, directory))
	if literalErr != nil {
		sb.WriteString(fmt.Sprintf("\nThe literal search could not complete: %v\n", literalErr))
	}
	// Say what was actually searched. "No results" from a literal-only run is
	// a much weaker statement than one backed by both strategies, and the
	// caller has to be able to tell the difference before concluding the code
	// does not exist.
	if note != "" {
		sb.WriteString("\nThis run was " + note + ", so only exact text matches were considered.\n" +
			"A conceptual query may still match code that uses different wording once the index is available.\n")
	} else {
		sb.WriteString("\nBoth literal and semantic search were applied.\n")
	}
	return sb.String()
}

func (h *searchHandler) Aliases() []string      { return nil }
func (h *searchHandler) Timeout() time.Duration { return 60 * time.Second }
func (h *searchHandler) MaxResultSize() int     { return 0 }
func (h *searchHandler) SafeForParallel() bool  { return true }
func (h *searchHandler) Interactive() bool      { return false }
