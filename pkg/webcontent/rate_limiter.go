package webcontent

import (
	"math/rand"
	"sync"
	"time"
)

// rateLimiter enforces a minimum interval between requests to the same domain,
// with jitter to avoid thundering herd patterns.
type rateLimiter struct {
	mu          sync.Mutex
	lastRequest map[string]time.Time
	minInterval time.Duration
	maxInterval time.Duration
	rng         *rand.Rand
}

// newRateLimiter creates a rate limiter with the given minimum and maximum
// intervals between requests to the same domain. Each request waits for a
// random duration between min and max, except the first request to a host
// which returns immediately.
func newRateLimiter(minInterval, maxInterval time.Duration) *rateLimiter {
	return &rateLimiter{
		lastRequest: make(map[string]time.Time),
		minInterval: minInterval,
		maxInterval: maxInterval,
		rng:         rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// wait blocks until the host-specific cooldown has elapsed.
// First request to a host returns immediately.
// Subsequent requests sleep for a random duration so that the total interval
// since the last request falls between minInterval and maxInterval.
func (r *rateLimiter) wait(host string) {
	r.mu.Lock()
	last, exists := r.lastRequest[host]
	if !exists {
		r.lastRequest[host] = time.Now()
		r.mu.Unlock()
		return
	}

	elapsed := time.Since(last)
	if elapsed >= r.maxInterval {
		r.lastRequest[host] = time.Now()
		r.mu.Unlock()
		return
	}

	// Sleep so total interval since last request is random in [min, max].
	sleepMin := r.minInterval - elapsed
	if sleepMin < 0 {
		sleepMin = 0
	}
	sleepMax := r.maxInterval - elapsed
	sleepFor := sleepMin + time.Duration(r.rng.Int63n(int64(sleepMax-sleepMin)))

	r.lastRequest[host] = time.Now().Add(sleepFor)
	r.mu.Unlock()
	time.Sleep(sleepFor)
}
