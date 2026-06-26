package utils

import (
	"context"
	"sync"
	"time"
)

// TokenBucket implements a thread-safe token bucket rate limiter.
// Tokens are added to the bucket at a constant rate, up to a maximum burst capacity.
// Requests must acquire a token from the bucket before proceeding.
//
// The algorithm:
// - The bucket starts with 'burst' tokens.
// - Tokens are added at 'rate' tokens per second.
// - The bucket never exceeds 'burst' tokens.
// - A request waits until at least one token is available, then consumes one.
//
// This implementation uses time.After for efficient waiting, avoiding busy-waiting.
// The nextReservation field is used to track when the next reserved token becomes
// available, preventing TOCTOU races when multiple goroutines wait concurrently.
type TokenBucket struct {
	rate            float64    // tokens per second
	burst           int        // maximum tokens in the bucket
	tokens          float64    // current token count
	lastRefill      time.Time  // last time tokens were added
	nextReservation time.Time  // when the next reserved token will be available
	mu              sync.Mutex // protects all fields
}

// NewTokenBucket creates a new token bucket with the specified rate and burst capacity.
// rate: tokens per second (e.g., 1.0 = 1 token per second)
// burst: maximum number of tokens the bucket can hold
//
// If rate <= 0 or burst <= 0, the bucket allows unlimited access.
func NewTokenBucket(rate float64, burst int) *TokenBucket {
	now := time.Now()
	return &TokenBucket{
		rate:            rate,
		burst:           burst,
		tokens:          float64(burst),
		lastRefill:      now,
		nextReservation: now, // No pending reservations initially
	}
}

// Wait blocks until a token is available, then consumes one.
// Returns an error if the context is canceled before a token becomes available.
// If the bucket was configured with rate <= 0 or burst <= 0, Wait returns immediately.
func (tb *TokenBucket) Wait(ctx context.Context) error {
	// Unlimited access configured
	if tb.rate <= 0 || tb.burst <= 0 {
		return nil
	}

	tb.mu.Lock()

	// Refill tokens based on elapsed time
	tb.refill()

	now := time.Now()

	// If nextReservation is in the future, someone is already waiting, so we join the queue
	if !tb.nextReservation.Before(now) {
		// Join the reservation queue
		reservationTime := tb.nextReservation
		tb.nextReservation = reservationTime.Add(time.Duration(float64(time.Second) / tb.rate))
		tb.mu.Unlock()

		waitDuration := reservationTime.Sub(now)
		if err := tb.waitForDuration(ctx, waitDuration); err != nil {
			// Context canceled - abandon reservation slot
			return err
		}
		return nil
	}

	// No one is waiting, check if we have tokens available
	if tb.tokens >= 1 {
		tb.tokens--
		tb.mu.Unlock()
		return nil
	}

	// No tokens available and no one waiting - we're the first to wait
	// Calculate when the next token will be available
	deficit := 1.0 - tb.tokens
	waitTime := time.Duration(deficit / float64(tb.rate) * float64(time.Second))
	tb.nextReservation = now.Add(waitTime)

	// Claim our reservation slot
	reservationTime := tb.nextReservation
	tb.nextReservation = reservationTime.Add(time.Duration(float64(time.Second) / tb.rate))

	tb.mu.Unlock()

	// Wait until our reservation slot is available
	waitDuration := reservationTime.Sub(now)
	if err := tb.waitForDuration(ctx, waitDuration); err != nil {
		// Context canceled - abandon reservation slot
		return err
	}

	return nil
}

// TryWait attempts to acquire a token without blocking.
// Returns true if a token was acquired, false if no token is available.
// If the bucket was configured with rate <= 0 or burst <= 0, TryWait returns true immediately.
func (tb *TokenBucket) TryWait() bool {
	// Unlimited access configured
	if tb.rate <= 0 || tb.burst <= 0 {
		return true
	}

	tb.mu.Lock()
	defer tb.mu.Unlock()

	// Refill tokens based on elapsed time
	tb.refill()

	// Check if token is available
	if tb.tokens >= 1 {
		tb.tokens--
		return true
	}

	return false
}

// UpdateRate dynamically updates the rate and burst capacity.
// This is safe to call while other goroutines are using Wait/TryWait.
// Note: If there are pending reservations, they will be honored at the old rate.
// To immediately apply the new rate, you may want to reset the limiter.
func (tb *TokenBucket) UpdateRate(rate float64, burst int) {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	tb.rate = rate
	tb.burst = burst

	// Refill tokens based on elapsed time before potentially changing capacity
	tb.refill()

	// Ensure tokens don't exceed new burst capacity
	if tb.tokens > float64(burst) {
		tb.tokens = float64(burst)
	}

	// Reset nextReservation to allow the new rate to take effect immediately
	// This is a trade-off: pending reservations at the old rate will be invalidated
	tb.nextReservation = time.Now()
}

// refill adds tokens to the bucket based on elapsed time since last refill.
// Caller must hold tb.mu.
func (tb *TokenBucket) refill() {
	if tb.rate <= 0 {
		return
	}

	now := time.Now()
	elapsed := now.Sub(tb.lastRefill)

	// Calculate tokens to add: rate * elapsed_seconds
	tokensToAdd := tb.rate * elapsed.Seconds()

	// Add tokens, capped at burst
	tb.tokens += tokensToAdd
	if tb.tokens > float64(tb.burst) {
		tb.tokens = float64(tb.burst)
	}

	tb.lastRefill = now
}

// waitForDuration waits for the specified duration, respecting context cancellation.
func (tb *TokenBucket) waitForDuration(ctx context.Context, duration time.Duration) error {
	if duration <= 0 {
		return nil
	}

	// Use time.After for each wait to avoid timer sharing issues
	// This is less efficient than reusing a timer but is simpler and thread-safe
	select {
	case <-time.After(duration):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// GetRate returns the current rate (tokens per second).
func (tb *TokenBucket) GetRate() float64 {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	return tb.rate
}

// GetBurst returns the current burst capacity.
func (tb *TokenBucket) GetBurst() int {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	return tb.burst
}

// GetAvailableTokens returns the approximate number of tokens currently available.
// This is useful for debugging and monitoring.
func (tb *TokenBucket) GetAvailableTokens() float64 {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.refill()
	return tb.tokens
}

// Refund returns a previously consumed token to the bucket.
// This is useful when a rate-limited operation fails before using the capacity.
func (tb *TokenBucket) Refund() {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.refill()
	if tb.tokens < float64(tb.burst) {
		tb.tokens++
	}
}
