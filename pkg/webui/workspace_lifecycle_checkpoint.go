//go:build !js

package webui

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// wipRefPrefix namespaces prepare-stop checkpoints; refs/sprout/wip/latest
// always points at the newest primary checkpoint.
const (
	wipRefPrefix = "refs/sprout/wip/"
	wipRefLatest = wipRefPrefix + "latest"
	emptyTreeSHA = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"
)

// checkpointWorkingTree snapshots root's uncommitted work (tracked changes
// and untracked, non-ignored files) as a commit on refs/sprout/wip/<ts>,
// parented on HEAD when there is one. It never touches HEAD, the index or
// the working tree: the snapshot is staged into a temporary copy of the
// index. Returns nil without error when root is not a git work tree or has
// nothing uncommitted. updateLatest also moves refs/sprout/wip/latest.
func checkpointWorkingTree(ctx context.Context, root string, ts int64, updateLatest bool, reason string) (*wipCheckpoint, error) {
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		return nil, nil
	}
	top, err := checkpointGit(ctx, root, nil, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, nil
	}
	indexPath, err := checkpointGit(ctx, top, nil, "rev-parse", "--git-path", "index")
	if err != nil {
		return nil, fmt.Errorf("locate index: %w", err)
	}
	if !filepath.IsAbs(indexPath) {
		indexPath = filepath.Join(top, indexPath)
	}
	head, headErr := checkpointGit(ctx, top, nil, "rev-parse", "--verify", "-q", "HEAD")
	hasHead := headErr == nil && head != ""

	tmp, err := os.CreateTemp("", "sprout-wip-index-*")
	if err != nil {
		return nil, fmt.Errorf("create temp index: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	copied, err := copyIndexInto(indexPath, tmp)
	_ = tmp.Close()
	if err != nil {
		return nil, fmt.Errorf("copy index: %w", err)
	}
	if !copied {
		// git treats an empty file as a corrupt index; start from none.
		_ = os.Remove(tmpPath)
	}

	env := []string{
		"GIT_INDEX_FILE=" + tmpPath,
		"GIT_AUTHOR_NAME=Sprout",
		"GIT_AUTHOR_EMAIL=sprout@localhost",
		"GIT_COMMITTER_NAME=Sprout",
		"GIT_COMMITTER_EMAIL=sprout@localhost",
	}
	if !copied && hasHead {
		if _, err := checkpointGit(ctx, top, env, "read-tree", "HEAD"); err != nil {
			return nil, fmt.Errorf("seed temp index: %w", err)
		}
	}
	if _, err := checkpointGit(ctx, top, env, "add", "-A", "--", "."); err != nil {
		return nil, fmt.Errorf("stage working tree: %w", err)
	}
	tree, err := checkpointGit(ctx, top, env, "write-tree")
	if err != nil {
		return nil, fmt.Errorf("write tree: %w", err)
	}
	baseTree := emptyTreeSHA
	if hasHead {
		if baseTree, err = checkpointGit(ctx, top, nil, "rev-parse", "HEAD^{tree}"); err != nil {
			return nil, fmt.Errorf("read HEAD tree: %w", err)
		}
	}
	if tree == baseTree {
		return nil, nil
	}

	args := []string{"commit-tree", tree, "-m", "sprout: checkpoint uncommitted work before workspace stop (" + reason + ")"}
	if hasHead {
		args = append(args, "-p", head)
	}
	sha, err := checkpointGit(ctx, top, env, args...)
	if err != nil {
		return nil, fmt.Errorf("commit checkpoint: %w", err)
	}
	ref := wipRefPrefix + strconv.FormatInt(ts, 10)
	if !updateLatest {
		// Secondary roots in the same repository must not collide with the
		// primary's ref.
		ref += "-" + sha[:12]
	}
	if _, err := checkpointGit(ctx, top, nil, "update-ref", ref, sha); err != nil {
		return nil, fmt.Errorf("write %s: %w", ref, err)
	}
	if updateLatest {
		if _, err := checkpointGit(ctx, top, nil, "update-ref", wipRefLatest, sha); err != nil {
			return nil, fmt.Errorf("write %s: %w", wipRefLatest, err)
		}
	}
	return &wipCheckpoint{Root: top, WipRef: ref, WipSHA: sha}, nil
}

// copyIndexInto copies the repository's index file into dst. It reports
// false (and no error) when the repository has no index yet.
func copyIndexInto(indexPath string, dst *os.File) (bool, error) {
	src, err := os.Open(filepath.Clean(indexPath))
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer src.Close()
	if _, err := io.Copy(dst, src); err != nil {
		return false, err
	}
	return true, nil
}

// checkpointGit runs git in dir with optional extra environment and returns its
// trimmed stdout.
func checkpointGit(ctx context.Context, dir string, env []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0")
	cmd.Env = append(cmd.Env, env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return "", err
		}
		return "", fmt.Errorf("%w: %s", err, msg)
	}
	return strings.TrimSpace(stdout.String()), nil
}
