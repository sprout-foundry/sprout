package credentials

import (
	"context"
	"errors"
	"time"

	"github.com/gofrs/flock"
)

// lockRetryDelay is how often a contended lock is re-polled. flock's
// Try*LockContext takes a retry delay, not a timeout: passing the timeout
// there made every contended caller sleep the full duration before its
// first retry and, with a Background context, wait forever.
const lockRetryDelay = 25 * time.Millisecond

// tryLock takes lock (shared or exclusive) within timeout. It reports
// false with a nil error when the timeout elapses.
func tryLock(lock *flock.Flock, shared bool, timeout time.Duration) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	try := lock.TryLockContext
	if shared {
		try = lock.TryRLockContext
	}
	locked, err := try(ctx, lockRetryDelay)
	if errors.Is(err, context.DeadlineExceeded) {
		return false, nil
	}
	return locked, err
}
