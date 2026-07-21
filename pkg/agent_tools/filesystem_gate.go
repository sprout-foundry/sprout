package tools

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	"github.com/sprout-foundry/sprout/pkg/filesystem"
)

// resolveCanonicalTimeout caps the symlink eval used to compute the
// resolved target shown in the approval dialog. Mirrors
// pkg/filesystem/filesystem.go:symlinkTimeout — network mounts
// (NFS, SMB, cloud) can hang indefinitely on EvalSymlinks, and the
// approval flow must not freeze the agent waiting on one.
const resolveCanonicalTimeout = 3 * time.Second

const filesystemGateContextKey = "filesystem_gate"

// WithFilesystemGate returns a child context carrying the supplied
// FilesystemGate. File-touching helpers (ReadFile, WriteFile, EditFile,
// ReadFileWithRange, ListDirectory, ProcessPDFForTextOnly) extract the
// gate via FilesystemGateFromContext and route off-workspace errors
// through it before propagating the failure to the caller.
//
// A nil gate is a no-op (returns ctx unchanged) — handlers constructed
// without an agent (unit tests, internal machinery) keep their
// historical hard-error semantics.
func WithFilesystemGate(ctx context.Context, gate FilesystemGate) context.Context {
	if gate == nil {
		return ctx
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, filesystemGateContextKey, gate)
}

// FilesystemGateFromContext returns the FilesystemGate previously
// stored with WithFilesystemGate, or nil if none. The returned value
// must be type-asserted to FilesystemGate; helpers do that assertion
// exactly once.
func FilesystemGateFromContext(ctx context.Context) FilesystemGate {
	if ctx == nil {
		return nil
	}
	gate, _ := ctx.Value(filesystemGateContextKey).(FilesystemGate)
	return gate
}

// WithFilesystemGateFromEnv copies the gate from ToolEnv onto ctx so
// downstream helpers (ReadFile, WriteFile, EditFile, …) can extract
// it via FilesystemGateFromContext. Without this wiring, helpers on
// the live seed dispatch path bypass the gate and the user gets a
// hard error instead of the approval dialog.
func WithFilesystemGateFromEnv(ctx context.Context, env ToolEnv) context.Context {
	if env.FilesystemGate == nil {
		return ctx
	}
	return WithFilesystemGate(ctx, env.FilesystemGate)
}

// resolveCanonicalForDisplay performs a symlink-only resolution of
// filePath and returns the absolute canonical target. It does NOT
// perform a workspace check — that's the resolver's job, which is
// what triggered the gate in the first place. The point is purely
// to give the approval dialog the actual filesystem target so the
// user can verify a symlink `workspace/link` pointing to
// `/etc/passwd` is not silently approved under a benign-looking
// display string.
//
// Bounded by resolveCanonicalTimeout to match the resolver's own
// symlink timeout (pkg/filesystem.evalSymlinksWithTimeout). On
// timeout, returns "" — callers fall back to the user-supplied path
// for display rather than hanging the agent on an unresponsive
// network mount.
func resolveCanonicalForDisplay(filePath string) string {
	if filePath == "" {
		return ""
	}
	abs, err := filepath.Abs(filePath)
	if err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), resolveCanonicalTimeout)
	defer cancel()
	type result struct {
		path string
		err  error
	}
	done := make(chan result, 1)
	go func() {
		resolved, err := filepath.EvalSymlinks(abs)
		done <- result{resolved, err}
	}()
	select {
	case res := <-done:
		if res.err != nil {
			return ""
		}
		return res.path
	case <-ctx.Done():
		return ""
	}
}

// withFilesystemApproval runs a retryable file operation and, if it
// fails with an off-workspace error, consults the active gate. On
// approval, retries once with the bypass-wrapped context the gate
// returns; on retry failure, returns the retry error alongside the
// zero value of T.
//
// `op` MUST be idempotent under retry — the helper may invoke it
// twice (initial attempt + retry after approval). Only the second
// call uses the bypass-wrapped context.
//
// Non-filesystem errors (anything other than the two filesystem
// sentinels) propagate unchanged without consulting the gate.
func withFilesystemApproval[T any](
	ctx context.Context,
	gate FilesystemGate,
	toolName, filePath string,
	op func(ctx context.Context) (T, error),
) (T, error) {
	var zero T
	result, err := op(ctx)
	if err == nil || gate == nil {
		return result, err
	}
	if !errors.Is(err, filesystem.ErrOutsideWorkingDirectory) && !errors.Is(err, filesystem.ErrWriteOutsideWorkingDirectory) {
		return result, err
	}

	resolved := resolveCanonicalForDisplay(filePath)
	newCtx, approved := gate.RequestPathApproval(ctx, toolName, filePath, resolved, err)
	if !approved {
		return result, err
	}
	result, err = op(newCtx)
	if err != nil {
		return zero, err
	}
	return result, nil
}