//go:build !js

package webui

import (
	"log"
	"sync"
	"time"
)

// logRateMinInterval is the minimum gap between two log emissions for the
// same key. Chosen so a periodically-polled-but-unreachable provider (e.g.
// a misconfigured ollama-local host) logs once when it first fails, stays
// silent through routine polling, and re-surfaces after enough time has
// passed that a fresh log line is genuinely informative.
const logRateMinInterval = 5 * time.Minute

var (
	logRateMu       sync.Mutex
	logRateLastSeen = map[string]time.Time{}
)

// logRateLimitedf wraps log.Printf and emits at most one line per key per
// logRateMinInterval. Use for chatty repeating failures (e.g. periodic
// model-discovery that hits an unreachable provider) where the first
// occurrence is useful but the 100th is just noise.
//
// Pick a stable key per logical event, e.g. "model_discovery_fail:ollama-local".
// Different keys are independent — limiting one does not silence the other.
func logRateLimitedf(key string, format string, args ...any) {
	now := time.Now()
	logRateMu.Lock()
	last, seen := logRateLastSeen[key]
	if seen && now.Sub(last) < logRateMinInterval {
		logRateMu.Unlock()
		return
	}
	logRateLastSeen[key] = now
	logRateMu.Unlock()
	log.Printf(format, args...)
}
