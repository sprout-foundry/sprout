package tools

import (
	"context"
	"errors"
	"fmt"
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

// filesystemGateDenialReasonKey is the context key under which a
// FilesystemGate can store a human-readable reason when it denies a
// request. withFilesystemApproval reads it on denial and prefers it
// over the generic ErrWriteOutsideWorkingDirectory /
// ErrOutsideWorkingDirectory sentinels so the user sees the
// workflow-specific reason (e.g. "write blocked: declared read_only").
// The key is unexported to keep the contract between the gate and
// the helper internal to this package — external gate
// implementations use the WithFilesystemGateDenialReason setter.
type filesystemGateDenialReasonKey struct{}

// WithFilesystemGateDenialReason returns a child context carrying a
// human-readable denial reason. FilesystemGate implementations call
// this when they deny a request with extra context the caller
// should surface to the user (SP-128-1f: a workflow declared the
// path as read_only; the gate refuses the write with a specific
// message instead of the generic off-workspace sentinel). The
// reason is a single sentence; withFilesystemApproval wraps it as
// the new error returned to the caller.
func WithFilesystemGateDenialReason(ctx context.Context, reason string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, filesystemGateDenialReasonKey{}, reason)
}

// FilesystemGateDenialReasonFromContext returns the denial reason
// stored on ctx, or "" if none. Internal — used by
// withFilesystemApproval.
func FilesystemGateDenialReasonFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	reason, _ := ctx.Value(filesystemGateDenialReasonKey{}).(string)
	return reason
}

// DenialReasonForTest is an exported alias of
// FilesystemGateDenialReasonFromContext used by tests in other
// packages that need to assert the gate stored a reason on the
// returned ctx. Production callers should consume the reason
// indirectly via withFilesystemApproval (which returns it as the
// error string on denial). The name carries the "ForTest" suffix
// to discourage drift into production call sites.
//
// Lives in the same package as the unexported context key so the
// type assertion remains valid.
func DenialReasonForTest(ctx context.Context) string {
	return FilesystemGateDenialReasonFromContext(ctx)
}

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
//
// On denial: if the gate stored a denial reason on the returned
// context via WithFilesystemGateDenialReason (SP-128-1f: workflow
// declared the path as read_only), the helper wraps that reason as
// the returned error so the user sees a workflow-specific message
// rather than the generic off-workspace sentinel. When the gate
// returns no reason, the original filesystem error is preserved.
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
		if reason := FilesystemGateDenialReasonFromContext(newCtx); reason != "" {
			// Return the workflow-specific reason instead of the
			// generic off-workspace sentinel so the user (and the
			// model seeing the tool result) sees a clear message
			// — "declared read_only in allowed_paths" — rather
			// than a generic "file write outside working
			// directory" that requires them to puzzle out why a
			// session-allowed folder is still being refused.
			// We still wrap the original sentinel via %w so
			// downstream errors.Is checks (e.g. the subagent
			// stderr parser scanning for "outside working
			// directory") keep working. The test
			// TestWithFilesystemApproval_DenialReasonSurfacesAsError
			// asserts the replacement behavior; see
			// TestWithFilesystemApproval_DenyWithoutReasonPreservesOriginal
			// for the generic-sentinel path when no reason is set.
			return zero, fmt.Errorf("%s: %w", reason, err)
		}
		return result, err
	}
	result, err = op(newCtx)
	if err != nil {
		return zero, err
	}
	return result, nil
}