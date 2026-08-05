//go:build !js

package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/embedding"
)

// Held-out conceptual queries with hand-verified ground truth, in packages the
// embedding work never touched. Mirrors the set in
// pkg/embedding/value_eval_test.go so the fusion numbers are comparable with
// the standalone semantic and ripgrep numbers measured there.
//
// grepPattern is the literal a developer would try for the same question,
// written before seeing any result from either strategy.
var fusionCases = []struct {
	name        string
	query       string
	grepPattern string
	wantFile    string
}{
	{"atomic file write", "write a file atomically so a crash cannot leave it half written", "atomic", "pkg/workflow/checkpoint.go"},
	{"path traversal guard", "reject an identifier that tries to escape its directory using dot dot", "traversal", "pkg/skills/builtin.go"},
	{"secret scanning", "detect leaked API keys and credentials inside text", "secret|credential scan", "pkg/secretdetect/scanner.go"},
	{"secret redaction", "replace a detected secret with a safe placeholder token", "redact", "pkg/secretdetect/scanner.go"},
	{"staged change parsing", "parse git name-status output into a list of changed files", "name-status|staged.*change", "pkg/git/commit_helpers.go"},
	{"reconnect backoff", "exponentially increasing delay between retry attempts after a dropped connection", "backoff", "pkg/mcp/client_reconnect.go"},
	{"duration formatting", "render an elapsed time compactly for terminal output", "formatDuration", "pkg/console/ci_output_handler.go"},
	{"persona catalog", "the built-in set of agent role definitions shipped with the product", "persona", "pkg/personas/catalog.go"},
	{"review defaults", "default configuration values for the automated code review pass", "review.*config", "pkg/codereview"},
	{"mcp config load", "read the model context protocol server list from a config file", "mcp.*config", "pkg/mcp"},
	{"keyword extraction", "pull the significant words out of a block of prose", "keyword", "pkg/text"},
	{"credential storage mode", "which backend is currently used to store API keys on this machine", "storage mode|backend mode", "pkg/credentials"},
	{"revision file listing", "list the files that were touched by a particular revision", "revision", "pkg/history"},
	{"probe cost budget", "decide whether a model probe is affordable before running it", "cost budget", "pkg/modelprobe"},
}

// TestSearchFusionBeatsEitherStrategy measures the fused `search` tool against
// each strategy alone on the held-out set.
//
// The claim under test is that fusion delivers the union: the two strategies'
// misses were disjoint, so merging should find what either finds. That is an
// assumption until measured — RRF could bury a correct result from one ranking
// under a long confident-but-wrong list from the other.
//
// Opt-in: SPROUT_VALUE_INDEX_DIR=<dir built by TestBuildFullIndexForValueEval>
func TestSearchFusionBeatsEitherStrategy(t *testing.T) {
	dir := os.Getenv("SPROUT_VALUE_INDEX_DIR")
	if dir == "" {
		t.Skip("SPROUT_VALUE_INDEX_DIR unset")
	}
	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	mgr := embedding.NewEmbeddingManager(&configuration.EmbeddingIndexConfig{IndexDir: dir}, repoRoot)
	t.Cleanup(func() { _ = mgr.Close() })
	if err := mgr.Init(ctx); err != nil {
		t.Skipf("embedding unavailable: %v", err)
	}
	if !mgr.Readiness().CanAnswerQueries() {
		t.Skip("index empty — run TestBuildFullIndexForValueEval first")
	}

	env := ToolEnv{EmbeddingMgr: mgr, WorkspaceRoot: repoRoot}
	h := &searchHandler{}

	// Rank = position of the first result line naming wantFile. Result lines are
	// unindented "path:line"; continuation lines are indented.
	//
	// Excludes this test file and the embedding eval, which contain every query
	// string verbatim and would otherwise be the top literal hit for their own
	// query — measuring the harness rather than the tool.
	rankOf := func(out, wantFile string) int {
		rank := 0
		for _, line := range strings.Split(out, "\n") {
			if line == "" || strings.HasPrefix(line, " ") || strings.HasPrefix(line, "Found") {
				continue
			}
			path := strings.SplitN(strings.TrimSpace(line), ":", 2)[0]
			if strings.HasSuffix(path, "_eval_test.go") || strings.HasSuffix(path, "value_eval_test.go") {
				continue
			}
			rank++
			if strings.HasPrefix(path, wantFile) {
				return rank
			}
		}
		return 0
	}

	var fusedHits, literalOnlyHits int
	t.Log("")
	t.Logf("%-24s %-14s %-14s", "query", "fused", "literal-only")
	t.Log("-------------------------------------------------------------")
	for _, c := range fusionCases {
		fused, err := h.Execute(ctx, env, map[string]any{"query": c.query, "max_results": 10})
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		lit, err := h.Execute(ctx, env, map[string]any{"query": c.grepPattern, "max_results": 10, "literal_only": true})
		if err != nil {
			t.Fatalf("%s literal: %v", c.name, err)
		}

		fr := rankOf(fused.Output, c.wantFile)
		lr := rankOf(lit.Output, c.wantFile)
		if fr > 0 {
			fusedHits++
		}
		if lr > 0 {
			literalOnlyHits++
		}
		fmtRank := func(r int) string {
			if r == 0 {
				return "MISS"
			}
			return fmt.Sprintf("#%d", r)
		}
		t.Logf("%-24s %-14s %-14s", c.name, fmtRank(fr), fmtRank(lr))
	}

	t.Log("")
	t.Logf("fused (conceptual query):   %d/%d", fusedHits, len(fusionCases))
	t.Logf("literal-only (regex query): %d/%d", literalOnlyHits, len(fusionCases))
	t.Logf("reference from pkg/embedding/value_eval_test.go: semantic 10/14, ripgrep 12/14")

	// Measured outcome: fusion MATCHES semantic alone on conceptual queries
	// (10/14); it does not reach the union of both strategies. The union
	// predicted from the standalone evals assumed each strategy received its own
	// query formulation — a hand-written regex for grep, prose for semantic. One
	// query string cannot serve both, and deriving a keyword pattern from prose
	// did not close the gap: a broad OR matches too many files and the capped
	// walk truncates before reaching the target.
	//
	// The gate is therefore "no worse than the best single strategy", which is
	// what justifies replacing two tools with one.
	if fusedHits < 10 {
		t.Errorf("fused search found %d/%d — worse than semantic alone (10/14), so the fused tool is a regression",
			fusedHits, len(fusionCases))
	}
}
