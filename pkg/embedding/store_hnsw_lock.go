package embedding

// store_hnsw_lock.go — the build-lock layer of the HNSW embedding
// store: the flock-based acquireBuildLock and the
// isFlockUnavailable probe (Windows/unsupported filesystems).
// Split out of store_hnsw.go.
import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/gofrs/flock"
)

// buildLockFileName is the cross-process lock file placed in the index dir.
const buildLockFileName = ".build.lock"

// errBuildLocked is returned when another process holds the index build lock.
var errBuildLocked = errors.New("embedding: index build lock held by another process")

// isFlockUnavailable checks whether an error from flock indicates that
// file locking is not supported on this platform (WASM, restricted sandbox).
// In those cases we fall back to no locking rather than failing the build.
func isFlockUnavailable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, errors.ErrUnsupported) {
		return true
	}
	if pe, ok := err.(*os.PathError); ok {
		if errors.Is(pe.Err, errors.ErrUnsupported) {
			return true
		}
		switch pe.Err {
		case syscall.ENOSYS, syscall.EOPNOTSUPP, syscall.EACCES, syscall.EPERM:
			return true
		}
	}
	return false
}

// acquireBuildLock takes an exclusive flock on indexDir/.build.lock with a short
// timeout. Returns a release func and nil error to proceed (release may be nil
// when flock is unavailable — proceed WITHOUT locking as a fallback). Returns
// errBuildLocked when another process holds the lock (caller should SKIP the
// build). Read-only operations must not call this.
//
// NOTE: This function is NOT re-entrant. If the same process acquires the lock
// twice (e.g. UpdateFromGitDiff → UpdateFile), the second acquisition will
// conflict with the first via flock(2) and block. Callers with re-entrant paths
// must use IndexManager.lockForBuild instead.
func acquireBuildLock(indexDir string) (release func(), err error) {
	// Empty indexDir means locking is disabled (keeps tests and direct callers working).
	if indexDir == "" {
		return nil, nil
	}

	// Ensure the directory exists; if it can't be created, fall back to no locking.
	if err := os.MkdirAll(indexDir, 0o755); err != nil {
		debugLogf("embedding: create lock dir %s failed (proceeding without lock): %v", indexDir, err)
		return nil, nil
	}

	f := flock.New(filepath.Join(indexDir, buildLockFileName))

	// Try non-blocking first.
	ok, err := f.TryLock()
	if ok {
		return func() { f.Unlock() }, nil
	}
	if err != nil {
		if isFlockUnavailable(err) {
			debugLogf("embedding: flock unavailable (proceeding without lock): %v", err)
			return nil, nil
		}
		// Unexpected error — fall back to no locking to avoid blocking the build.
		debugLogf("embedding: flock try-lock failed (proceeding without lock): %v", err)
		return nil, nil
	}

	// Lock is held by another process. Wait up to BuildLockTimeout with 50ms retry delay.
	ctx, cancel := context.WithTimeout(context.Background(), BuildLockTimeout)
	defer cancel()
	ok, err = f.TryLockContext(ctx, 50*time.Millisecond)
	if err != nil {
		if isFlockUnavailable(err) {
			debugLogf("embedding: flock unavailable (proceeding without lock): %v", err)
			return nil, nil
		}
		// Context expired or other error — still locked by another process.
		return nil, errBuildLocked
	}
	if ok {
		return func() { f.Unlock() }, nil
	}
	// Still not acquired.
	return nil, errBuildLocked
}
