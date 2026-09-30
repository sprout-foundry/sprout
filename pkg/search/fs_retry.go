package search

import "time"

// retryTransientFS retries op while it fails with an error the OS reports
// only because another handle has the file open. Windows refuses to rename
// over, or open, a file that a concurrent reader or writer holds; POSIX
// never does, so there op runs exactly once.
func retryTransientFS(op func() error) error {
	const attempts = 50
	var err error
	for i := 0; i < attempts; i++ {
		if err = op(); err == nil || !isTransientShareError(err) {
			return err
		}
		time.Sleep(10 * time.Millisecond)
	}
	return err
}
