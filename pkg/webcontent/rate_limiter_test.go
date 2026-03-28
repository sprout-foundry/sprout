package webcontent

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRateLimiter_FirstCallReturnsImmediately(t *testing.T) {
	rl := newRateLimiter(50*time.Millisecond, 100*time.Millisecond)
	start := time.Now()
	rl.wait("example.com")
	elapsed := time.Since(start)
	assert.Less(t, elapsed, 30*time.Millisecond, "first call should return immediately")
}

func TestRateLimiter_SecondCallBlocks(t *testing.T) {
	minD, maxD := 50*time.Millisecond, 100*time.Millisecond
	rl := newRateLimiter(minD, maxD)
	rl.wait("example.com")

	start := time.Now()
	rl.wait("example.com")
	elapsed := time.Since(start)
	assert.GreaterOrEqual(t, elapsed, minD-5*time.Millisecond,
		"second call should block for at least the min interval")
	assert.Less(t, elapsed, maxD+50*time.Millisecond,
		"second call should not block much beyond the max interval")
}

func TestRateLimiter_DifferentHostsDontBlock(t *testing.T) {
	rl := newRateLimiter(50*time.Millisecond, 100*time.Millisecond)
	rl.wait("host1.com")

	start := time.Now()
	rl.wait("host2.com")
	elapsed := time.Since(start)
	assert.Less(t, elapsed, 30*time.Millisecond,
		"different host should not be rate-limited by the first host")
}

func TestRateLimiter_SleepAfterElapsedWithinMin(t *testing.T) {
	minD, maxD := 100*time.Millisecond, 200*time.Millisecond
	rl := newRateLimiter(minD, maxD)
	rl.wait("example.com")
	time.Sleep(80 * time.Millisecond) // elapsed is less than min

	start := time.Now()
	rl.wait("example.com")
	elapsed := time.Since(start)
	// Should sleep for the remainder of the min interval (at least ~20ms)
	assert.GreaterOrEqual(t, elapsed, 10*time.Millisecond,
		"should still sleep to reach min interval")
}

func TestRateLimiter_SleepAfterElapsedBetweenMinMax(t *testing.T) {
	minD, maxD := 100*time.Millisecond, 200*time.Millisecond
	rl := newRateLimiter(minD, maxD)
	rl.wait("example.com")
	time.Sleep(150 * time.Millisecond) // elapsed is between min and max

	start := time.Now()
	rl.wait("example.com")
	elapsed := time.Since(start)
	// Should return immediately or near-immediately, no sleep needed.
	// Use 100ms to tolerate CI scheduling jitter (observed ~50ms overhead).
	assert.Less(t, elapsed, 100*time.Millisecond,
		"should not need to sleep since elapsed is within range")
}

func TestRateLimiter_NoSleepAfterMaxElapsed(t *testing.T) {
	minD, maxD := 50*time.Millisecond, 100*time.Millisecond
	rl := newRateLimiter(minD, maxD)
	rl.wait("example.com")
	time.Sleep(120 * time.Millisecond) // elapsed exceeds max

	start := time.Now()
	rl.wait("example.com")
	elapsed := time.Since(start)
	assert.Less(t, elapsed, 30*time.Millisecond,
		"should return immediately when enough time has passed")
}

func TestRateLimiter_ConcurrentSafety(t *testing.T) {
	rl := newRateLimiter(10*time.Millisecond, 15*time.Millisecond)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(host string) {
			defer wg.Done()
			rl.wait(host)
		}(string(rune('a'+i)) + ".example.com")
	}
	wg.Wait() // should not deadlock or panic
}

func TestRateLimiter_JitterVarying(t *testing.T) {
	// Call the same host multiple times and verify delays aren't all identical.
	minD, maxD := 20*time.Millisecond, 60*time.Millisecond
	rl := newRateLimiter(minD, maxD)

	var delays []time.Duration
	for i := 0; i < 5; i++ {
		start := time.Now()
		rl.wait("jitter.example.com")
		if i > 0 { // skip first call (no delay)
			delays = append(delays, time.Since(start))
		}
	}

	// With jitter, delays should vary — check at least two are different.
	// (Probabilistic test but extremely unlikely to fail with genuine jitter.)
	hasVariation := false
	for i := 1; i < len(delays); i++ {
		diff := delays[i] - delays[i-1]
		if diff < 0 {
			diff = -diff
		}
		if diff > 1*time.Millisecond {
			hasVariation = true
		}
	}
	assert.True(t, hasVariation, "delays should vary with jitter, got: %v", delays)
}
