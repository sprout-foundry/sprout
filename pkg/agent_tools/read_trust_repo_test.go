package tools

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/injection"
	"github.com/stretchr/testify/require"
)

func writeTrustFile(t *testing.T, path string) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("package x\n"), 0o644))
	return path
}

// linkWorktree lays out a linked worktree of the repository at repoRoot the
// way `git worktree add` does: a .git file naming a private gitdir under the
// main repository, whose commondir points back at the main .git.
func linkWorktree(t *testing.T, repoRoot, worktree string) {
	t.Helper()
	name := filepath.Base(worktree)
	private := filepath.Join(repoRoot, ".git", "worktrees", name)
	require.NoError(t, os.MkdirAll(private, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(private, "commondir"), []byte("../..\n"), 0o644))
	require.NoError(t, os.MkdirAll(worktree, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(worktree, ".git"), []byte("gitdir: "+private+"\n"), 0o644))
}

// Reading two sibling directories of one clone asks once: the question is
// about the repository, not the directory the first file happened to be in.
func TestResolveReadTrust_OneQuestionPerClone(t *testing.T) {
	env, root := trustTestEnv(t)
	asker := &recordingAskUser{answer: trustThirdPartyOption}
	env.AskUser = asker
	clone := filepath.Join(root, "clones", "acme")
	require.NoError(t, os.MkdirAll(filepath.Join(clone, ".git"), 0o755))

	resolveReadTrust(env, writeTrustFile(t, filepath.Join(clone, "pkg", "a", "a.go")))
	resolveReadTrust(env, writeTrustFile(t, filepath.Join(clone, "cmd", "b", "b.go")))
	require.Equal(t, 1, asker.calls())
	require.Contains(t, asker.lastQuestion(), "clones/acme`")
}

// A /task worktree is the workspace's own repository checked out again under
// .sprout/tasks. Its files are ours; nothing is asked.
func TestResolveReadTrust_TaskWorktreeIsOurs(t *testing.T) {
	env, root := trustTestEnv(t)
	asker := &recordingAskUser{answer: trustThirdPartyOption}
	env.AskUser = asker
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".git"), 0o755))
	wt := filepath.Join(root, ".sprout", "tasks", "fix-login")
	linkWorktree(t, root, wt)

	got := resolveReadTrust(env, writeTrustFile(t, filepath.Join(wt, "auth", "login.go")))
	require.Equal(t, injection.TrustFirstParty, got)
	require.Zero(t, asker.calls())
}

// The same repository checked out as a worktree outside the workspace is
// still our code.
func TestResolveReadTrust_OwnWorktreeOutsideWorkspaceIsOurs(t *testing.T) {
	env, root := trustTestEnv(t)
	asker := &recordingAskUser{answer: trustThirdPartyOption}
	env.AskUser = asker
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".git"), 0o755))
	wt := filepath.Join(t.TempDir(), "elsewhere-wt")
	linkWorktree(t, root, wt)

	got := resolveReadTrust(env, writeTrustFile(t, filepath.Join(wt, "main.go")))
	require.Equal(t, injection.TrustFirstParty, got)
	require.Zero(t, asker.calls())
}

// A file outside the workspace and outside any repository — a temp file, a
// cache, a config file — is framed without a question nobody can sensibly
// answer.
func TestResolveReadTrust_OutsideAnyRepoIsNotAsked(t *testing.T) {
	env, _ := trustTestEnv(t)
	asker := &recordingAskUser{answer: trustOursOption}
	env.AskUser = asker

	got := resolveReadTrust(env, writeTrustFile(t, filepath.Join(t.TempDir(), "out", "build.log")))
	require.Equal(t, injection.TrustThirdParty, got)
	require.Zero(t, asker.calls())
}

// A clone outside the workspace is still the ambiguous case: asked once, about
// the clone.
func TestResolveReadTrust_ForeignCloneOutsideIsAskedOnce(t *testing.T) {
	env, _ := trustTestEnv(t)
	asker := &recordingAskUser{answer: trustThirdPartyOption}
	env.AskUser = asker
	clone := filepath.Join(t.TempDir(), "lib")
	require.NoError(t, os.MkdirAll(filepath.Join(clone, ".git"), 0o755))

	resolveReadTrust(env, writeTrustFile(t, filepath.Join(clone, "a", "x.go")))
	resolveReadTrust(env, writeTrustFile(t, filepath.Join(clone, "b", "y.go")))
	require.Equal(t, 1, asker.calls())
}

// blockingAskUser holds every question until released, so concurrent reads
// really overlap.
type blockingAskUser struct {
	recordingAskUser
	release chan struct{}
}

func (b *blockingAskUser) Ask(ctx context.Context, req AskUserRequest) (string, error) {
	answer, err := b.recordingAskUser.Ask(ctx, req)
	<-b.release
	return answer, err
}

// Parallel reads of one clone raise one question, and every reader gets its
// answer.
func TestResolveReadTrust_ConcurrentReadsShareOneQuestion(t *testing.T) {
	env, root := trustTestEnv(t)
	asker := &blockingAskUser{recordingAskUser: recordingAskUser{answer: trustOursOption}, release: make(chan struct{})}
	env.AskUser = asker
	clone := filepath.Join(root, "vendor-ish", "acme")
	require.NoError(t, os.MkdirAll(filepath.Join(clone, ".git"), 0o755))

	var wg sync.WaitGroup
	results := make([]injection.TrustClass, 5)
	for i := range results {
		path := writeTrustFile(t, filepath.Join(clone, "pkg", string(rune('a'+i)), "f.go"))
		wg.Add(1)
		go func(i int, path string) {
			defer wg.Done()
			results[i] = resolveReadTrust(env, path)
		}(i, path)
	}
	deadline := time.Now().Add(2 * time.Second)
	for asker.calls() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond) // let the other readers reach the question
	close(asker.release)
	wg.Wait()

	require.Equal(t, 1, asker.calls(), "one question for the clone, however many reads raced to it")
	for i, got := range results {
		require.Equal(t, injection.TrustFirstParty, got, "reader %d", i)
	}
}
