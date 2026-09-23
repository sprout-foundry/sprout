//go:build !js

package design

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/internal/testgit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// -----------------------------------------------------------------------------
// SP-140-5 §5f — Git: one commit carries both sides
//
// The co-commit rule is *baked-in process*, so it gets a git-fixture test, not
// just prose: on a throwaway repo this test drives the skill-driven loop end to
// end — a dev change touches the implementation and `design_sync`'s apply half
// (PlanSyncApply) produces the `design/` adoption — then stages both sides
// together, exactly as the skill's co-commit rule prescribes.
//
// The acceptance criteria asserted here are the two §5f clauses that only a
// real repository can prove:
//
//   - Co-commit: the loop yields a SINGLE commit carrying both the code change
//     and the design/ adoption (one commit, containing both files).
//   - Revert: `git revert` of that commit removes BOTH sides at once, so
//     history never desyncs to a half-applied loop.
//
// The fixture repo is a t.TempDir() with its own `git init`; the developer's
// real working tree is never touched (git's config lookup is redirected to the
// throwaway identity by TestMain below).
// -----------------------------------------------------------------------------

func TestMain(m *testing.M) {
	// The fixture-repo tests exec real git subprocesses; redirect their
	// config so they can never read or write the developer's real
	// ~/.gitconfig (the package under test itself never shells out to git —
	// the subprocesses here are the fixture).
	testgit.Configure()
	os.Exit(m.Run())
}

// -----------------------------------------------------------------------------
// git fixture helpers
// -----------------------------------------------------------------------------

// coCommitGit runs a git subcommand in dir and returns its trimmed stdout,
// failing the test on error. No global config is read: TestMain pinned
// GIT_CONFIG_GLOBAL/SYSTEM at throwaway files carrying the test identity.
func coCommitGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "git %s failed: %s", strings.Join(args, " "), out)
	return strings.TrimSpace(string(out))
}

// coCommitTrackedNames returns the repo-tracked files at HEAD, slash-separated.
// `git ls-tree -r --name-only HEAD` reads the commit itself, so it answers
// "which of these paths does this commit carry?" independent of the working
// tree's current state.
func coCommitTrackedNames(t *testing.T, dir string) []string {
	t.Helper()
	out := coCommitGit(t, dir, "ls-tree", "-r", "--name-only", "HEAD")
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// coCommitInit creates a throwaway repo at dir with a single base commit
// carrying the fixture design/ tree and the pre-change implementation file.
// The caller mutates the working tree from there.
func coCommitInit(t *testing.T, dir string) {
	t.Helper()
	coCommitGit(t, dir, "init")
	// Pin a repo-local identity as well as the redirected global one: the
	// suite's own workflow test treats a subprocess repo-local config write
	// (inside its temp workspace) as sanctioned, and this keeps the fixture
	// independent of testgit's sandbox files.
	coCommitGit(t, dir, "config", "user.email", "fixture@example.com")
	coCommitGit(t, dir, "config", "user.name", "Fixture User")
	coCommitGit(t, dir, "config", "commit.gpgsign", "false")

	syncWriteFixtureTree(t, dir)
	syncWrite(t, dir, "src/theme.css", ":root{ --color-brand-primary: #0055ff; }\n")
	coCommitGit(t, dir, "add", "-A")
	coCommitGit(t, dir, "commit", "-m", "chore: fixture base")
}

// coCommitApplySync runs the design_sync apply half over the loop's touched UI
// code and writes the plan's design/-confined adoption to disk — the fixture
// stand-in for `design_sync --apply`. It returns the design/ paths it adopted
// (the path set the co-commit must carry).
func coCommitApplySync(t *testing.T, root string, touched []SyncFileInput) []string {
	t.Helper()
	plan := syncApplyPlanFor(t, root, touched)
	require.NotEmpty(t, plan.Writes, "the fixture's dev change must produce a design/ adoption to co-commit")
	require.True(t, plan.IsConfinedToDesign(), "the adoption writes only under design/ (§5e)")

	adopted := make([]string, 0, len(plan.Writes))
	for _, w := range plan.Writes {
		require.NotEmpty(t, w.Content, "an adoption write carries its new bytes")
		syncWrite(t, root, w.Path, string(w.Content))
		adopted = append(adopted, w.Path)
	}
	return adopted
}

// coCommitLoopChange is the loop's dev-side delta: the implementation revalues
// its theme var, so the code file and its DTCG source disagree until design_sync
// adopts the change.
const coCommitDevCSS = ":root{ --color-brand-primary: #ff0000; }\n"

// -----------------------------------------------------------------------------
// AC: one commit carries both the code change and the design/ adoption
// -----------------------------------------------------------------------------

func TestCoCommit_LoopProducesSingleCommitCarryingCodeAndDesign(t *testing.T) {
	root := t.TempDir()
	coCommitInit(t, root)

	// Dev turn: the implementation changes.
	codePath := "src/theme.css"
	syncWrite(t, root, codePath, coCommitDevCSS)

	// The UI-affecting turn ends with design_sync: the semantic layer adopts
	// the revalue.
	adopted := coCommitApplySync(t, root, []SyncFileInput{
		{Path: codePath, Content: []byte(coCommitDevCSS)},
	})
	require.Equal(t, []string{"design/tokens/color.tokens.json"}, adopted,
		"the loop's adoption is the DTCG token source")

	// Co-commit: the dev change and its design/ adoption are staged and
	// committed TOGETHER — one commit, not two. The skill's design-led
	// iterations use the `design:` Conventional-Commit type.
	coCommitGit(t, root, "add", "--", codePath, "design/tokens/color.tokens.json")
	msg := "design: adopt the theme revalue into the token source"
	coCommitGit(t, root, "commit", "-m", msg)

	// (a) The loop produced exactly ONE commit beyond the fixture base.
	require.Equal(t, 2, coCommitCount(t, root), "the loop produces one co-commit, not a code commit + a design commit")

	// (a) That single commit carries BOTH files. The base commit already
	// tracks both paths (the fixture tree ships design/tokens/color.tokens.json),
	// so an ls-tree presence check alone would not be falsifiable — the test
	// must prove the *content of that commit* carries both changes.
	names := coCommitTrackedNames(t, root)
	assert.Contains(t, names, codePath, "the co-commit carries the code change")
	assert.Contains(t, names, "design/tokens/color.tokens.json", "the same commit carries the design/ adoption")

	// (a) …and it carries both *changes*, in one commit, not an empty merge:
	// the commit itself touched both files.
	changed := coCommitCommitFiles(t, root, "HEAD")
	assert.ElementsMatch(t, []string{codePath, "design/tokens/color.tokens.json"}, changed,
		"one commit touched both the implementation file and the design/ file")

	// (a) Both *sides* of the loop are present at HEAD, in one commit: the code
	// change and the design/ adoption, each with the post-loop value. This is
	// the falsifiable form of "one commit carries both" — if either side had
	// been left out of the commit (or committed separately) this fails.
	codeAtHead := coCommitGit(t, root, "show", "HEAD:src/theme.css")
	designAtHead := coCommitGit(t, root, "show", "HEAD:design/tokens/color.tokens.json")
	require.Contains(t, codeAtHead, "#ff0000", "the co-commit carries the code change")
	require.Contains(t, designAtHead, "#ff0000", "the co-commit carries the design/ adoption")

	// The commit reads as design-led history.
	assert.True(t, strings.HasPrefix(coCommitGit(t, root, "log", "-1", "--pretty=%s"), "design:"),
		"a design-led iteration uses the design: commit type")

	// The working tree is clean: nothing was left half-staged for a second
	// commit to pick up.
	assert.Empty(t, coCommitGit(t, root, "status", "--porcelain"),
		"after the co-commit nothing is left uncommitted — there is no second commit to make")
}

// -----------------------------------------------------------------------------
// AC: reverting the co-commit removes both sides
// -----------------------------------------------------------------------------

func TestCoCommit_RevertRemovesBothCodeAndDesignAdoption(t *testing.T) {
	root := t.TempDir()
	coCommitInit(t, root)

	codePath := "src/theme.css"
	designPath := "design/tokens/color.tokens.json"

	// The base state both sides revert to.
	codeBefore := coCommitRead(t, root, codePath)
	designBefore := coCommitRead(t, root, designPath)
	require.Contains(t, codeBefore, "#0055ff", "the base code carries the pre-change value")
	require.Contains(t, designBefore, "#0055ff", "the base token source carries the pre-change value")

	// Loop: dev change + design_sync adoption, co-committed.
	syncWrite(t, root, codePath, coCommitDevCSS)
	coCommitApplySync(t, root, []SyncFileInput{{Path: codePath, Content: []byte(coCommitDevCSS)}})
	coCommitGit(t, root, "add", "--", codePath, designPath)
	coCommitGit(t, root, "commit", "-m", "design: adopt the theme revalue into the token source")

	require.Contains(t, coCommitRead(t, root, codePath), "#ff0000", "the co-commit carries the code change")
	require.Contains(t, coCommitRead(t, root, designPath), "#ff0000", "the co-commit carries the design/ adoption")

	// Revert the co-commit. `git revert HEAD` on a single-parent commit needs
	// no `-m`.
	coCommitGit(t, root, "revert", "--no-edit", "HEAD")

	// (b) Both sides are gone: the files are back to their base bytes.
	codeAfter := coCommitRead(t, root, codePath)
	designAfter := coCommitRead(t, root, designPath)
	assert.NotContains(t, codeAfter, "#ff0000", "the code change is reverted")
	assert.NotContains(t, designAfter, "#ff0000", "the design/ adoption is reverted")
	assert.Equal(t, codeBefore, codeAfter, "the code file is byte-identical to the pre-loop state")
	assert.Equal(t, designBefore, designAfter, "the design/ file is byte-identical to the pre-loop state")

	// Reverting the co-commit leaves a single revert commit, the tree clean,
	// and no dangling half of the loop.
	assert.Equal(t, 3, coCommitCount(t, root), "one revert commit, not a revert pair")
	assert.Empty(t, coCommitGit(t, root, "status", "--porcelain"), "the reverting tree is clean")
	assert.ElementsMatch(t, []string{codePath, designPath}, coCommitCommitFiles(t, root, "HEAD"),
		"the revert itself touches both sides — it removes them together")
}

// -----------------------------------------------------------------------------
// unit: the co-commit is undoable as one unit (revert never desyncs)
// -----------------------------------------------------------------------------

func TestCoCommit_RevertIsASingleUnitOnTheLoopHistory(t *testing.T) {
	root := t.TempDir()
	coCommitInit(t, root)

	codePath := "src/theme.css"
	designPath := "design/tokens/color.tokens.json"
	base := coCommitGit(t, root, "rev-parse", "HEAD")

	syncWrite(t, root, codePath, coCommitDevCSS)
	coCommitApplySync(t, root, []SyncFileInput{{Path: codePath, Content: []byte(coCommitDevCSS)}})
	coCommitGit(t, root, "add", "--", codePath, designPath)
	coCommitGit(t, root, "commit", "-m", "feat: consume the refreshed theme (+ design/ adoption)")

	// A dev-led iteration keeps feat: and still co-commits the adoption.
	assert.True(t, strings.HasPrefix(coCommitGit(t, root, "log", "-1", "--pretty=%s"), "feat:"),
		"a dev-led iteration keeps the feat: type")

	// Revert as one unit, then descend to the base and assert the loop's two
	// sides are consistent there too: neither file carries the change.
	coCommitGit(t, root, "revert", "--no-edit", "HEAD")
	coCommitGit(t, root, "checkout", "--quiet", base, "--", codePath, designPath)

	code := coCommitRead(t, root, codePath)
	design := coCommitRead(t, root, designPath)
	assert.NotContains(t, code, "#ff0000", "no code-ahead delta survives the revert")
	assert.NotContains(t, design, "#ff0000", "no design-ahead delta survives the revert")
	assert.Contains(t, code, "#0055ff")
	assert.Contains(t, design, "#0055ff")

	// Restore the fixture to a clean HEAD so the temp repo ends in a
	// well-defined state (belt-and-braces for the leak check: this is the
	// fixture repo, never the developer's tree).
	coCommitGit(t, root, "checkout", "--quiet", "HEAD", "--", codePath, designPath)
}

// -----------------------------------------------------------------------------
// helpers
// -----------------------------------------------------------------------------

func coCommitCount(t *testing.T, dir string) int {
	t.Helper()
	out := coCommitGit(t, dir, "rev-list", "--count", "HEAD")
	n := 0
	for _, r := range out {
		if r < '0' || r > '9' {
			t.Fatalf("unexpected rev-list --count output %q", out)
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func coCommitCommitFiles(t *testing.T, dir, rev string) []string {
	t.Helper()
	out := coCommitGit(t, dir, "show", "--pretty=format:", "--name-only", rev)
	var files []string
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			files = append(files, line)
		}
	}
	return files
}

func coCommitRead(t *testing.T, root, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	require.NoErrorf(t, err, "read %s", rel)
	return string(data)
}
