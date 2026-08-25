package embedding

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// valueCase is a held-out conceptual query with hand-verified ground truth.
//
// Two rules were applied when writing these, because breaking either turns the
// evaluation into a self-fulfilling one:
//
//  1. Every target lives in a package this work never modified (workflow,
//     skills, secretdetect, git, mcp, console, personas, codereview, text,
//     credentials, history, modelprobe). None overlaps the pkg/embedding
//     benchmark the thresholds were tuned on.
//  2. Queries are phrased the way someone asks who does NOT know the
//     identifier — that is the only situation where semantic search can beat
//     grep, so it is the situation worth measuring. Queries that name the
//     symbol would just be measuring grep with extra steps.
type valueCase struct {
	name string
	// query is what a developer would type into semantic search.
	query string
	// grep is a good-faith first attempt at the same question with ripgrep:
	// the distinctive content words, case-insensitive. Written before seeing
	// any results from either system.
	grep string
	// wantFile is the file that answers the question.
	wantFile string
	// wantSymbol, when set, must appear in a semantic hit for it to count.
	wantSymbol string
}

var valueCases = []valueCase{
	{"atomic file write", "write a file atomically so a crash cannot leave it half written",
		"atomic", "pkg/workflow/checkpoint.go", "WriteFileAtomic"},
	{"path traversal guard", "reject an identifier that tries to escape its directory using dot dot",
		"traversal", "pkg/skills/builtin.go", ""},
	{"secret scanning", "detect leaked API keys and credentials inside text",
		"secret|credential scan", "pkg/secretdetect/scanner.go", ""},
	{"secret redaction", "replace a detected secret with a safe placeholder token",
		"redact", "pkg/secretdetect/scanner.go", ""},
	{"staged change parsing", "parse git name-status output into a list of changed files",
		"name-status|staged.*change", "pkg/git/commit_helpers.go", ""},
	{"reconnect backoff", "exponentially increasing delay between retry attempts after a dropped connection",
		"backoff", "pkg/mcp/client_reconnect.go", "calculateBackoff"},
	{"duration formatting", "render an elapsed time compactly for terminal output",
		"formatDuration", "pkg/console/ci_output_handler.go", ""},
	{"persona catalog", "the built-in set of agent role definitions shipped with the product",
		"persona", "pkg/personas/catalog.go", ""},
	{"review defaults", "default configuration values for the automated code review pass",
		"review.*config", "pkg/codereview", ""},
	{"mcp config load", "read the model context protocol server list from a config file",
		"mcp.*config", "pkg/mcp", ""},
	{"keyword extraction", "pull the significant words out of a block of prose",
		"keyword", "pkg/text", ""},
	{"credential storage mode", "which backend is currently used to store API keys on this machine",
		"storage mode|backend mode", "pkg/credentials", ""},
	{"revision file listing", "list the files that were touched by a particular revision",
		"revision", "pkg/history", ""},
	{"probe cost budget", "decide whether a model probe is affordable before running it",
		"cost budget", "pkg/modelprobe", ""},
}

// TestSemanticSearchValueVsRipgrep answers the question the threshold work
// could not: is semantic search worth its cost compared with the tool everyone
// already has?
//
// Scoring is "how many results must a developer scan before reaching the
// answer" for both systems — rank of the correct file, or a miss.
//
// Opt-in: SPROUT_VALUE_INDEX_DIR=<dir built by TestBuildFullIndexForValueEval>
func TestSemanticSearchValueVsRipgrep(t *testing.T) {
	dir := valueEvalIndexDir(t)

	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	mgr := NewEmbeddingManager(&configuration.EmbeddingIndexConfig{IndexDir: dir}, repoRoot)
	t.Cleanup(func() { _ = mgr.Close() })
	if err := mgr.Init(ctx); err != nil {
		t.Skipf("embedding init unavailable: %v", err)
	}
	if n := mgr.IndexSize(); n < 1000 {
		t.Skipf("index holds only %d records — run TestBuildFullIndexForValueEval first", n)
	}
	t.Logf("index: %d records", mgr.IndexSize())

	idx, err := mgr.snapshotIndexMgr()
	if err != nil {
		t.Fatalf("index manager: %v", err)
	}

	const topK = 10

	var rows []valueRow

	for _, c := range valueCases {
		// --- semantic ---
		hits, err := idx.QuerySimilar(ctx, c.query, topK, DefaultSemanticSearchThreshold)
		if err != nil {
			t.Fatalf("%s: semantic query: %v", c.name, err)
		}
		r := valueRow{name: c.name}
		if len(hits) > 0 {
			r.semTopScore = hits[0].Similarity
		}
		for i, h := range hits {
			rel := relativeTo(repoRoot, h.Record.File)
			if !strings.HasPrefix(rel, c.wantFile) {
				continue
			}
			if c.wantSymbol != "" && !strings.Contains(h.Record.ID, c.wantSymbol) {
				continue
			}
			r.semRank = i + 1
			break
		}

		// --- ripgrep ---
		files := grepFiles(t, repoRoot, c.grep)
		r.grepTotal = len(files)
		for i, f := range files {
			if strings.HasPrefix(f, c.wantFile) {
				r.grepRank = i + 1
				break
			}
		}
		rows = append(rows, r)
	}

	t.Log("")
	t.Logf("%-24s %-18s %-22s", "query", "semantic", "ripgrep")
	t.Log("---------------------------------------------------------------------")
	var semHits, grepHits, semBetter, grepBetter, tie int
	for _, r := range rows {
		sem := "MISS"
		if r.semRank > 0 {
			sem = fmt.Sprintf("#%d (%.2f)", r.semRank, r.semTopScore)
			semHits++
		}
		grep := fmt.Sprintf("MISS (%d files)", r.grepTotal)
		if r.grepRank > 0 {
			grep = fmt.Sprintf("#%d of %d files", r.grepRank, r.grepTotal)
			grepHits++
		}
		t.Logf("%-24s %-18s %-22s", r.name, sem, grep)

		switch {
		case r.semRank > 0 && r.grepRank == 0:
			semBetter++
		case r.semRank == 0 && r.grepRank > 0:
			grepBetter++
		case r.semRank > 0 && r.grepRank > 0:
			switch {
			case r.semRank < r.grepRank:
				semBetter++
			case r.grepRank < r.semRank:
				grepBetter++
			default:
				tie++
			}
		}
	}

	t.Log("")
	t.Logf("found the answer:  semantic %d/%d,  ripgrep %d/%d", semHits, len(rows), grepHits, len(rows))
	t.Logf("ranked better:     semantic %d,  ripgrep %d,  tie %d", semBetter, grepBetter, tie)
	t.Logf("(both miss on %d)", len(rows)-semHits-grepHits+countBothHit(rows))

	if semHits == 0 {
		t.Error("semantic search found nothing on a held-out query set — no value over grep")
	}
}

type valueRow struct {
	name        string
	semRank     int // 0 = miss within topK
	grepRank    int // 0 = miss
	grepTotal   int
	semTopScore float32
}

func countBothHit(rows []valueRow) int {
	n := 0
	for _, r := range rows {
		if r.semRank > 0 && r.grepRank > 0 {
			n++
		}
	}
	return n
}

// grepFiles returns the distinct non-test .go files matching pattern, in the
// tool's own output order — the order a developer would scan them in.
//
// Uses grep rather than ripgrep because `rg` on this machine is a shell
// function, not a binary: exec.Command("rg") fails, and an earlier version of
// this helper swallowed that error and reported "0 files matched" for every
// query. That produced a clean-looking table in which grep lost every single
// case — a false result that would have been easy to believe.
func grepFiles(t *testing.T, root, pattern string) []string {
	t.Helper()
	cmd := exec.Command("grep", "-rEil", "--include=*.go", pattern, ".")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		// grep exits 1 when there are no matches — a legitimate result. Any
		// other exit status is a harness failure and must not be silent.
		if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 1 {
			t.Fatalf("grep %q failed: %v", pattern, err)
		}
		return nil
	}
	var files []string
	seen := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		f := strings.TrimPrefix(strings.TrimSpace(line), "./")
		if f == "" || seen[f] || strings.HasSuffix(f, "_test.go") {
			continue
		}
		seen[f] = true
		files = append(files, f)
	}
	return files
}

// relativeTo normalizes an indexed record path to repo-relative form.
//
// Records store whatever path shape the build used — this index was built with
// a relative root, so File comes back as "../../pkg/x.go". Comparing that to a
// repo-relative expectation with filepath.Rel silently never matches, which
// made every semantic case read as MISS in an earlier run of this test.
func relativeTo(root, path string) string {
	p := path
	if !filepath.IsAbs(p) {
		if abs, err := filepath.Abs(p); err == nil {
			p = abs
		}
	}
	if rel, err := filepath.Rel(root, p); err == nil {
		return rel
	}
	return path
}
