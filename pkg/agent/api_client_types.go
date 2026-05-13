package agent

import "fmt"

// RateLimitExceededError indicates repeated rate limit failures even after retries.
// This type is referenced by the scripted test client and must be available
// outside of the (now-deleted) APIClient file.
type RateLimitExceededError struct {
	Attempts  int
	LastError error
}

func (e *RateLimitExceededError) Error() string {
	if e.LastError == nil {
		return fmt.Sprintf("rate limit exceeded after %d attempt(s)", e.Attempts)
	}
	return fmt.Sprintf("rate limit exceeded after %d attempt(s): %v", e.Attempts, e.LastError)
}

func (e *RateLimitExceededError) Unwrap() error {
	return e.LastError
}
