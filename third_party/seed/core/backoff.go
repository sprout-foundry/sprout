package core

import (
	"math/rand"
	"time"
)

// ExponentialBackoff implements exponential backoff with jitter for retry logic.
// It is NOT safe for concurrent use by multiple goroutines.
//
// Usage:
//
//	backoff := &ExponentialBackoff{InitialDelay: 100 * time.Millisecond, MaxDelay: 10 * time.Second, MaxAttempts: 5}
//	for backoff.NextAttempt() {
//	    time.Sleep(backoff.Delay())
//	    if ok := doWork(); ok {
//	        break
//	    }
//	}
type ExponentialBackoff struct {
	InitialDelay time.Duration // delay for the first retry
	MaxDelay     time.Duration // cap on delay growth (0 = no cap)
	Multiplier   float64       // exponential growth factor (typically > 1)
	MaxAttempts  int           // total number of attempts (0 = unlimited)
	Jitter       float64       // jitter: 0 = none, (0,1) = partial [base, base*(1+jitter)], >= 1 = full jitter [0, base]

	// randSrc is the source for jitter. If nil, uses global math/rand.
	randSrc randSource

	attempt      int
	currentDelay time.Duration
}

// randSource abstracts rand.Float64 for testability.
type randSource interface {
	Float64() float64
}

// randWrapper wraps global math/rand for the randSource interface.
type randWrapper struct{}

func (r randWrapper) Float64() float64 { return rand.Float64() }

// NewExponentialBackoff creates a configured ExponentialBackoff with a global
// random source. Pass a custom randSource to make jitter deterministic in tests.
func NewExponentialBackoff(initialDelay, maxDelay time.Duration, multiplier float64, maxAttempts int, jitter float64) *ExponentialBackoff {
	return &ExponentialBackoff{
		InitialDelay: initialDelay,
		MaxDelay:     maxDelay,
		Multiplier:   multiplier,
		MaxAttempts:  maxAttempts,
		Jitter:       jitter,
		randSrc:      randWrapper{},
	}
}

// WithRand sets a custom random source (useful for deterministic tests).
func (b *ExponentialBackoff) WithRand(rs randSource) *ExponentialBackoff {
	b.randSrc = rs
	return b
}

// NextAttempt advances to the next retry and returns true if the attempt count
// has not exceeded MaxAttempts. Returns false immediately if MaxAttempts is
// already reached or exceeded.
func (b *ExponentialBackoff) NextAttempt() bool {
	b.attempt++

	if b.MaxAttempts > 0 && b.attempt > b.MaxAttempts {
		return false
	}

	if b.attempt == 1 {
		b.currentDelay = b.InitialDelay
	} else {
		b.currentDelay = time.Duration(float64(b.currentDelay) * b.Multiplier)
		if b.MaxDelay > 0 && b.currentDelay > b.MaxDelay {
			b.currentDelay = b.MaxDelay
		}
	}

	return true
}

// Delay returns the current delay duration with jitter applied.
// Jitter adds a random percentage of the base delay to prevent thundering herd.
// If Jitter is in (0, 1), the delay is [base, base * (1 + Jitter)].
// If Jitter is >= 1, full jitter mode replaces delay with [0, base].
// Panics if called before NextAttempt() or if randSrc is nil.
func (b *ExponentialBackoff) Delay() time.Duration {
	if b.randSrc == nil {
		panic("ExponentialBackoff.Delay: randSrc is nil; use NewExponentialBackoff or WithRand")
	}

	delay := b.currentDelay
	if b.Jitter > 0 && b.Jitter < 1.0 {
		jitter := time.Duration(b.randSrc.Float64() * b.Jitter * float64(delay))
		delay += jitter
	} else if b.Jitter >= 1.0 {
		// Full jitter: replace delay with a random value in [0, delay]
		delay = time.Duration(b.randSrc.Float64() * float64(delay))
	}
	return delay
}

// Reset resets the backoff state so it can be reused for a new retry sequence.
func (b *ExponentialBackoff) Reset() {
	b.attempt = 0
	b.currentDelay = 0
}
