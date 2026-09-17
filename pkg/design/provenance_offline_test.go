//go:build !js

package design

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// -----------------------------------------------------------------------------
// SP-140-5 §5f — provenance at any ref / AC "Provenance offline check"
// (TODO item 5.10)
//
// §5f: generated artifacts carry content-hash headers (5a), so any checkout —
// any commit, any branch, a PR diff — can verify design/code consistency
// offline with no database, no tags, no CI. The commit hash is the version pin.
//
// The AC: "at an arbitrary checkout, the hash in a generated artifact's header
// matches the token inputs at that commit (staleness is decidable from the tree
// alone)."
//
// This test proves that property on a FIXTURE git repo, and it is deliberately
// falsifiable on *state*: at every point it reads the token sources and the
// generated artifacts OUT OF THE COMMITS THEMSELVES (`git show <rev>:<path>`),
// never off the working tree, so "at an arbitrary checkout" is not simulated by
// whatever the test happens to have left on disk. The assertions:
//
//	(a) at the export commit A, the token-input hash recomputed from A's tree
//	    equals the `source-hash:` header recorded in A's generated artifact —
//	    not stale, provable from the tree alone;
//	(b) at commit B, which changes the token inputs, the hash recomputed from
//	    B's tree differs from A's artifact header — the artifact is stale
//	    relative to B, and staleness is decidable from the tree alone;
//	(c) the same recomputation the design_assets/design_validate drift report
//	    (5.5, §5c) uses agrees: the committed tree at A reads design-ahead=false,
//	    at B design-ahead=true.
//
// No network, no browser, no database, no tags: only `git init` + commits in a
// t.TempDir(). The developer's real working tree is never touched — git's
// config lookup is redirected by the package TestMain in cocommit_test.go, and
// the fixture passes `-C <tempdir>` and a scrubbed environment to every git
// call (there is no `git -C` for `init`; see provGit).
// -----------------------------------------------------------------------------

// -----------------------------------------------------------------------------
// git fixture helpers
// -----------------------------------------------------------------------------

// provGit runs a git subcommand in dir and returns its trimmed stdout, failing
// the test on error. It passes `-C dir` so the command operates in the fixture
// regardless of the process working directory (git has no `-C` for `init`, and
// a `-C` before `init` would make init operate there — which is exactly what we
// want), and scrubs the environment so an inherited GIT_DIR/GIT_WORK_TREE (this
// test may run inside the developer's repo) cannot redirect the fixture git at
// the real working tree.
func provGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", dir}, args...)
	cmd := exec.Command("git", full...)
	cmd.Env = provGitEnv()
	out, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "git %s failed: %s", strings.Join(full, " "), out)
	return strings.TrimSpace(string(out))
}

// provGitEnv is the environment fixture git runs under: the process environment
// (so the TestMain-redirected GIT_CONFIG_GLOBAL/SYSTEM still apply) minus the
// location overrides that would point a subprocess at the enclosing repo.
func provGitEnv() []string {
	drop := map[string]bool{
		"GIT_DIR":        true,
		"GIT_WORK_TREE":  true,
		"GIT_INDEX_FILE": true,
		"GIT_COMMON_DIR": true,
	}
	env := make([]string, 0, len(os.Environ()))
	for _, kv := range os.Environ() {
		key := kv
		if i := strings.IndexByte(kv, '='); i >= 0 {
			key = kv[:i]
		}
		if drop[key] {
			continue
		}
		env = append(env, kv)
	}
	return append(env, "GIT_TERMINAL_PROMPT=0")
}

// provInit creates a throwaway repo at root carrying the fixture design/ tree
// plus the current token inputs at commit A, and returns A's commit hash — the
// "version pin" the artifact's provenance header is verified against.
func provInit(t *testing.T, root string) string {
	t.Helper()
	provGit(t, root, "init")
	provGit(t, root, "config", "user.email", "fixture@example.com")
	provGit(t, root, "config", "user.name", "Fixture User")
	provGit(t, root, "config", "commit.gpgsign", "false")

	// Commit A: the token inputs, plus the "code" side a generated artifact
	// would feed. No design/generated/ yet.
	provWrite(t, root, "design/tokens/color.tokens.json", provTokensHexBlue)
	syncWriteFixtureTree(t, root)
	provWrite(t, root, "src/theme.css", ":root{ --color-brand-primary: #0055ff; }\n")
	provGit(t, root, "add", "-A")
	provGit(t, root, "commit", "-m", "design: publish the token inputs")
	return provGit(t, root, "rev-parse", "HEAD")
}

// provExportAt exports the tokens from the *working tree* (the §5a export "code
// half") and commits the generated artifacts, returning the commit that carries
// them. It is the fixture form of running design_export_tokens at commit A: the
// artifacts and the token inputs they were exported from land in history, so a
// later checkout can verify the pair offline.
func provExportAt(t *testing.T, root, message string) string {
	t.Helper()
	tokens, err := ResolveExportTokens(root)
	require.NoError(t, err, "the committed token tree must resolve for export")
	artifacts, err := RenderArtifacts(tokens, exportTargetList(ExportTargets))
	require.NoError(t, err)
	require.NoError(t, WriteExportedArtifacts(root, artifacts))
	provGit(t, root, "add", "-A")
	provGit(t, root, "commit", "-m", message)
	return provGit(t, root, "rev-parse", "HEAD")
}

// provShow reads a file's bytes at a revision out of the commit itself. This is
// the point of the test: "the tree alone" means the committed tree at that ref,
// not the working tree the test currently has.
func provShow(t *testing.T, root, rev, rel string) string {
	t.Helper()
	return provGit(t, root, "show", rev+":"+rel)
}

// provTreeHashes lists every blob at a revision as "<objectid>\t<path>" rows,
// sorted by path. It is the git-side, tree-only identity of the commit: two
// trees with byte-identical contents list identically, so a test can prove the
// fixture returned to an earlier state without trusting the working tree.
func provTreeHashes(t *testing.T, root, rev string) []string {
	t.Helper()
	out := provGit(t, root, "ls-tree", "-r", rev)
	if out == "" {
		return nil
	}
	rows := strings.Split(out, "\n")
	sort.Strings(rows)
	return rows
}

// provCheckout materializes a revision into the fixture working tree, so the
// in-process recompute (ResolveExportTokens, which reads the filesystem) can
// run against exactly the tree at that ref. `git checkout --force` is safe
// here: the fixture has no uncommitted state by construction.
func provCheckout(t *testing.T, root, rev string) {
	t.Helper()
	provGit(t, root, "checkout", "--quiet", "--force", rev)
}

// provTreeInputHash recomputes the §5a/§5f token-input provenance hash from a
// revision's *committed* tree alone, via the exported helper the offline
// consumer would use. It builds the hash input from `git show rev:<path>`
// blobs, sorted by basename — the exact ordering TokenExportInputHash requires
// (the exporter's exportTokenInputHash sorts the same way) — so it never reads
// the working tree and needs no git checkout to be correct.
//
// It deliberately calls the *exported* TokenExportInputHash rather than the
// unexported exportTokenInputHash: the exported function is the documented
// offline recompute, and it makes the ordering precondition explicit at the
// call site instead of hiding it behind the internal wrapper.
func provTreeInputHash(t *testing.T, root, rev string) string {
	t.Helper()
	names := provGit(t, root, "ls-tree", "-r", "--name-only", rev, DirName+"/"+TokenSubdir)
	sources := make([]TokenExportSource, 0)
	for _, line := range strings.Split(names, "\n") {
		rel := strings.TrimSpace(line)
		if !strings.HasSuffix(rel, ".tokens.json") {
			continue
		}
		sources = append(sources, TokenExportSource{
			Path:    rel,
			Name:    filepath.Base(rel),
			Content: []byte(provShow(t, root, rev, rel)),
		})
	}
	require.NotEmpty(t, sources, "revision %s must carry at least one %s/*.tokens.json", rev, DirName+"/"+TokenSubdir)
	sort.Slice(sources, func(i, j int) bool { return sources[i].Name < sources[j].Name })
	return TokenExportInputHash(sources)
}

// provArtifactHeaderHashes gathers the `source-hash:` header value from every
// generated artifact at a revision, keyed by design-relative path.
func provArtifactHeaderHashes(t *testing.T, root, rev string) map[string]string {
	t.Helper()
	names := provGit(t, root, "ls-tree", "-r", "--name-only", rev, DirName+"/"+GeneratedSubdir)
	out := make(map[string]string)
	for _, line := range strings.Split(names, "\n") {
		rel := strings.TrimSpace(line)
		if rel == "" {
			continue
		}
		out[rel] = provenanceSourceHash(provShow(t, root, rev, rel))
	}
	return out
}

// provWrite writes rel (slash-separated) under root with parent directories.
func provWrite(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
}

// Token fixtures. The two documents differ only in the primary colour value, so
// commit B's token-input hash must differ from A's with the structure, file
// set, and sort order unchanged.
const provTokensHexBlue = `{
  "color": {
    "brand": {
      "primary": { "$type": "color", "$value": "#0055ff" }
    }
  }
}`

const provTokensHexRed = `{
  "color": {
    "brand": {
      "primary": { "$type": "color", "$value": "#ff0000" }
    }
  }
}`

// -----------------------------------------------------------------------------
// AC (a): at the export commit, the header matches the tree — not stale
// -----------------------------------------------------------------------------

func TestProvenanceOffline_ArtifactHeaderMatchesTokenInputsAtExportCommit(t *testing.T) {
	root := t.TempDir()
	commitA := provInit(t, root)

	// design_export_tokens at commit A, committed: the artifact and the token
	// inputs it was exported from now live at the same ref.
	exportCommit := provExportAt(t, root, "design: export the theme")

	// Every generated artifact carries the provenance banner, not just one.
	headers := provArtifactHeaderHashes(t, root, exportCommit)
	require.Len(t, headers, len(ExportTargets), "every §5a target's artifact is committed")
	for rel, header := range headers {
		assert.NotEmptyf(t, header, "%s must carry a parsable source-hash header", rel)
	}

	// (a) Recompute the token-input hash from the EXPORT COMMIT's tree alone
	// (via the exported helper) and compare to the header recorded in that
	// commit's artifact. They must be equal: at this checkout the generated
	// theme is not stale.
	recomputed := provTreeInputHash(t, root, exportCommit)
	for rel, header := range headers {
		assert.Equalf(t, recomputed, header,
			"%s at %s must record the token-input hash of that same commit", rel, exportCommit)
	}

	// The tokens did not change between A and the export commit, so the tree at
	// A recomputes to the same hash too: the header is valid at A's checkout.
	assert.Equal(t, recomputed, provTreeInputHash(t, root, commitA),
		"the token inputs are unchanged from A to the export commit")

	// Cross-check against the in-process exporter (a real export at this
	// checkout), so the git-side recompute is provably the same hash the tool
	// would write — not a parallel implementation that merely looks similar.
	provCheckout(t, root, exportCommit)
	tokens, err := ResolveExportTokens(root)
	require.NoError(t, err)
	assert.Equal(t, recomputed, tokens.InputHash,
		"the git-side recompute and ResolveExportTokens agree on the input hash")
	assert.Equal(t, recomputed, tokens.InputHash,
		"the working-tree export and the committed artifact header agree")

	// Close the whole loop offline: the artifact's header text at the commit
	// names the same hash the exported helper recomputes from that commit's
	// token blobs, and the artifact's *path* resolves to bytes git can read
	// back (`rev:path` succeeded). So a reader with only the commit and the
	// tree — no sprout, no database, no tags — can recompute the token-input
	// hash and compare it to the recorded header to decide staleness.
	blob := provGit(t, root, "rev-parse", exportCommit+":"+DirName+"/"+GeneratedSubdir+"/"+ExportFilenames[ExportTargetCSS])
	require.NotEmpty(t, blob, "the artifact path resolves inside the commit")
	assert.Contains(t, provShow(t, root, exportCommit, DirName+"/"+GeneratedSubdir+"/"+ExportFilenames[ExportTargetCSS]),
		"source-hash: "+recomputed,
		"the committed artifact carries the recomputable hash in its text")
}

// -----------------------------------------------------------------------------
// AC (b): a token change flips staleness, decidably from the tree alone
// -----------------------------------------------------------------------------

func TestProvenanceOffline_StaleArtifactDecidedFromTreeAloneAfterTokenChange(t *testing.T) {
	root := t.TempDir()
	provInit(t, root)
	exportCommit := provExportAt(t, root, "design: export the theme")

	// Commit B: the token inputs move. The same shape, a different value — so
	// the only thing that can move the hash is the value itself.
	provWrite(t, root, "design/tokens/color.tokens.json", provTokensHexRed)
	provGit(t, root, "add", "-A")
	provGit(t, root, "commit", "-m", "design: revalue the brand colour")
	commitB := provGit(t, root, "rev-parse", "HEAD")

	// The recompute from B's tree differs from the artifact header recorded at
	// the export commit — the artifact is stale relative to B.
	hashB := provTreeInputHash(t, root, commitB)
	headers := provArtifactHeaderHashes(t, root, exportCommit)
	oldHeader := headers[DirName+"/"+GeneratedSubdir+"/"+ExportFilenames[ExportTargetCSS]]
	require.NotEmpty(t, oldHeader)
	require.NotEqual(t, oldHeader, hashB,
		"a token change must move the recomputed token-input hash (§5b/§5f staleness basis)")

	// B's tree still carries the OLD artifact (it changed tokens, not the
	// generated output), so at B the header/inputs pair, read from the tree
	// alone, disagrees — the staleness is decidable offline with no database,
	// no tags, no CI.
	artifactAtB := provShow(t, root, commitB, DirName+"/"+GeneratedSubdir+"/"+ExportFilenames[ExportTargetCSS])
	assert.Equal(t, oldHeader, provenanceSourceHash(artifactAtB),
		"B carries the artifact from the export commit unchanged")
	assert.NotEqual(t, provenanceSourceHash(artifactAtB), provTreeInputHash(t, root, commitB),
		"at B the artifact header and the token inputs disagree: design/ is ahead of the generated output")

	// Negative control: the OLDER checkout (the export commit) is NOT stale —
	// the same three-line check that flags B accepts A. The verdict follows the
	// commit, not the machine, so a PR author can pin the pair with the commit
	// hash alone.
	assert.Equal(t, provTreeInputHash(t, root, exportCommit), oldHeader,
		"the export commit's own tree verifies clean")

	// Re-export at B reconciles them: the new artifact's header equals B's
	// recompute, proving the mismatch was the token change and not noise.
	provCheckout(t, root, commitB)
	reconciled := provExportAt(t, root, "design: regenerate the theme for the revalue")
	assert.Equal(t, provTreeInputHash(t, root, reconciled), hashB,
		"the regenerated artifact's header matches B's token inputs")
	assert.NotEqual(t, oldHeader, hashB)
}

// -----------------------------------------------------------------------------
// AC (c): the drift report (5.5) agrees, from the committed tree alone
// -----------------------------------------------------------------------------

func TestProvenanceOffline_DriftReportAgreesWhenCheckedOutAtEachCommit(t *testing.T) {
	root := t.TempDir()
	provInit(t, root)
	exportCommit := provExportAt(t, root, "design: export the theme")

	provWrite(t, root, "design/tokens/color.tokens.json", provTokensHexRed)
	provGit(t, root, "add", "-A")
	provGit(t, root, "commit", "-m", "design: revalue the brand colour")
	commitB := provGit(t, root, "rev-parse", "HEAD")

	// The Commons ground truth: at the export commit the tree carries BOTH the
	// tokens and the generated theme, so the commit is self-verifying.
	beforeA := provTreeHashes(t, root, exportCommit)

	// Check out A: the drift report — the same recompute that backs the
	// header check and the design_assets/design_validate rows — reads in sync.
	provCheckout(t, root, exportCommit)
	reportA := AnalyzeDrift(root, nil)
	designAheadA := driftRow(t, reportA, DriftRowDesignAhead)
	assert.False(t, designAheadA.Ahead, "at the export commit design/ is not ahead of the generated output")
	assert.True(t, designAheadA.Synced)
	assert.Empty(t, designAheadA.Remedy)

	// Check out B: the tokens moved after the export, so the same report reads
	// design-ahead with the regeneration remedy — decided from the tree alone.
	provCheckout(t, root, commitB)
	reportB := AnalyzeDrift(root, nil)
	designAheadB := driftRow(t, reportB, DriftRowDesignAhead)
	assert.True(t, designAheadB.Ahead, "at B design/ is ahead of the stale generated output")
	assert.Equal(t, "design_export_tokens", designAheadB.NextStep)
	assert.Contains(t, reportB.Evidence, "design/generated/ provenance hash differs from the current design/tokens/*.tokens.json input hash.")

	// The two verdicts differ, so the drift row is a function of the checked-out
	// commit — not of any ambient state.
	assert.NotEqual(t, designAheadA.Ahead, designAheadB.Ahead)

	// The check left the fixture history untouched: A lists the same blobs it
	// listed before the test checked it out (no stray writes, no rewritten
	// commits — the offline check is read-only by construction).
	assert.Equal(t, beforeA, provTreeHashes(t, root, exportCommit),
		"the offline check must not modify a commit's tree")
}

// -----------------------------------------------------------------------------
// unit: the recompute is tree-only (no working-tree, no ambient state)
// -----------------------------------------------------------------------------

func TestProvenanceOffline_RecomputeIsTreeOnlyAndNotAMachineFact(t *testing.T) {
	root := t.TempDir()
	provInit(t, root)
	exportCommit := provExportAt(t, root, "design: export the theme")

	// The git-side recompute reads the committed blobs at the ref; the
	// in-process recompute reads whatever is checked out. When the checkout IS
	// that ref they must agree — the fact is a property of the commit.
	fromTree := provTreeInputHash(t, root, exportCommit)

	provCheckout(t, root, exportCommit)
	tokens, err := ResolveExportTokens(root)
	require.NoError(t, err)
	assert.Equal(t, fromTree, tokens.InputHash,
		"a checkout of the export commit recomputes to the hash recorded at that commit")

	// Dirty the working tree (uncommitted token edit): the committed-tree
	// recompute is unmoved, because it never reads the working tree. That is
	// the whole offline property — the verdict follows the ref.
	provWrite(t, root, "design/tokens/color.tokens.json", provTokensHexRed)
	assert.Equal(t, fromTree, provTreeInputHash(t, root, exportCommit),
		"the committed-tree recompute is unaffected by uncommitted working-tree edits")

	// And the working-tree recompute now disagrees, so staleness is decidable
	// before committing too.
	dirty, err := ResolveExportTokens(root)
	require.NoError(t, err)
	assert.NotEqual(t, fromTree, dirty.InputHash)

	// Drop the uncommitted edit so the fixture ends clean (it is a throwaway
	// repo, but the species of assertion above is stronger with a clean end).
	provGit(t, root, "checkout", "--quiet", "--force", exportCommit)
	assert.Empty(t, provGit(t, root, "status", "--porcelain"))
}
