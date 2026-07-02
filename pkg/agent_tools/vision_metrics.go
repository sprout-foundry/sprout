package tools

import (
	"sync"
	"sync/atomic"
	"time"
)

// VisionMetrics holds in-memory counters for vision pipeline observability.
// These are surfaced via GetVisionMetrics() and emitted to the OpenTelemetry
// metrics sink when one is configured. The hot-path counters are atomic-only
// (no mutex) — they can be incremented in tight loops without lock contention.
// The failure-by-reason map uses a sync.RWMutex because (a) writes are rare
// (only on failures) and (b) we need to iterate the map for snapshots. A
// sync.Map would work but adds per-entry allocation overhead for no benefit
// at our write volume.
//
// SP-103-C4: metrics + observability for vision image tokens.
// VISION-5: structured vision metrics (failure-by-reason, retry count,
// OCR fallback rate, latency by phase).
type VisionMetrics struct {
	// ImageTokensTotal is the cumulative count of input prompt image tokens
	// billed across all vision calls (including cached reads).
	ImageTokensTotal atomic.Int64

	// ImageTokensCachedTotal is the cumulative count of cached image
	// tokens — a subset of ImageTokensTotal that hit the cache and so
	// cost only the discounted cached rate.
	ImageTokensCachedTotal atomic.Int64

	// EmbedCallsTotal counts every call to embed a multimodal image into
	// a chat message (processImagesAsMultimodal result).
	EmbedCallsTotal atomic.Int64

	// OCRCallsTotal counts calls to the OCR-via-tool path.
	OCRCallsTotal atomic.Int64

	// ResizeEvents counts every image we resized down to the 1568px cap
	// before embedding (SP-103-B2).
	ResizeEvents atomic.Int64

	// CacheHits and CacheMisses mirror the cache stats on a separate
	// atomic surface so callers can poll metrics cheaply.
	CacheHits   atomic.Int64
	CacheMisses atomic.Int64

	// --- VISION-5: structured metrics ---

	// RetryCount tracks the total number of retry attempts across all
	// vision calls (i.e., the number of times DoVisionRetry re-entered
	// the loop after a failed attempt).
	RetryCount atomic.Int64

	// OCRFallbackTotal counts the number of times OCR fallback was attempted.
	OCRFallbackTotal atomic.Int64

	// OCRFallbackSuccess counts the number of times OCR fallback returned
	// a successful result. The fallback success rate is
	// OCRFallbackSuccess / OCRFallbackTotal.
	OCRFallbackSuccess atomic.Int64

	// LatencyRequestMS accumulates wall-clock time (ms) spent in the
	// provider's SendVisionRequest call (per attempt, not including retries).
	LatencyRequestMS atomic.Int64

	// LatencyRetrySleepMS accumulates wall-clock time (ms) spent sleeping
	// between retry attempts.
	LatencyRetrySleepMS atomic.Int64

	// LatencyFallbackMS accumulates wall-clock time (ms) spent in the
	// OCR fallback path (from entry to result).
	LatencyFallbackMS atomic.Int64

	// LatencyParseMS accumulates wall-clock time (ms) spent parsing the
	// provider response into a VisionAnalysis struct.
	LatencyParseMS atomic.Int64

	// FailuresByReason maps a classified failure reason string to the
	// cumulative count of failures with that reason. Protected by mu.
	// Reasons include: "http_5xx", "http_429", "http_4xx",
	// "context_cancel", "network", "timeout", "invalid_response",
	// "ocr_no_text", "unknown".
	mu               sync.RWMutex
	FailuresByReason map[string]int64

	// --- VISION-4: batch metrics ---

	// BatchAttempts counts the number of times batched vision analysis was
	// attempted (N>1 images sent together in one provider call).
	BatchAttempts atomic.Int64

	// BatchHits counts the number of times a batched result was served
	// from the cache without a provider call.
	BatchHits atomic.Int64

	// BatchMisses counts the number of times a batched result was NOT in
	// the cache and required a provider call.
	BatchMisses atomic.Int64

	// BatchPartialFailures counts the number of times a batched provider
	// call returned but one or more per-image sections were missing/failed,
	// requiring per-image fallback processing.
	BatchPartialFailures atomic.Int64
}

// globalVisionMetrics is the package-level metrics instance. It is updated
// from hot paths; readers should call GetVisionMetrics() to take a stable
// snapshot.
var globalVisionMetrics = VisionMetrics{
	FailuresByReason: make(map[string]int64),
}

// IncVisionImageTokens adds delta to the image-tokens counter.
// deltaCached is the portion of delta that was served from cache (so the
// discounted-rate bucket is updated separately).
func IncVisionImageTokens(delta int, deltaCached int) {
	if delta > 0 {
		globalVisionMetrics.ImageTokensTotal.Add(int64(delta))
	}
	if deltaCached > 0 {
		globalVisionMetrics.ImageTokensCachedTotal.Add(int64(deltaCached))
	}
}

// IncVisionEmbedCall bumps the embed-call counter by 1.
func IncVisionEmbedCall() {
	globalVisionMetrics.EmbedCallsTotal.Add(1)
}

// IncVisionOCRCall bumps the OCR-call counter by 1.
func IncVisionOCRCall() {
	globalVisionMetrics.OCRCallsTotal.Add(1)
}

// IncVisionResizeEvent records that we resized one image down to embed.
func IncVisionResizeEvent() {
	globalVisionMetrics.ResizeEvents.Add(1)
}

// IncVisionCacheHit/Miss track cache outcomes for metrics consumers that
// only watch the metrics surface (not VisionCacheStats).
func IncVisionCacheHit() {
	globalVisionMetrics.CacheHits.Add(1)
}

func IncVisionCacheMiss() {
	globalVisionMetrics.CacheMisses.Add(1)
}

// --- VISION-5: structured metrics helpers ---

// IncVisionFailure classifies err into a reason bucket and increments the
// corresponding counter. Reason buckets:
//
//	"http_5xx"         — HTTP 5xx errors
//	"http_429"         — HTTP 429 Too Many Requests
//	"http_4xx"         — Other HTTP 4xx errors
//	"context_cancel"   — context.Canceled or context.DeadlineExceeded
//	"network"          — net.Error (timeout or temporary)
//	"timeout"          — syscall.ETIMEDOUT
//	"invalid_response" — empty or unparseable provider response
//	"ocr_no_text"      — OCR fallback returned no text
//	"unknown"          — everything else
func IncVisionFailure(reason string) {
	globalVisionMetrics.mu.Lock()
	globalVisionMetrics.FailuresByReason[reason]++
	globalVisionMetrics.mu.Unlock()
}

// IncVisionRetry bumps the retry counter by 1. Called each time DoVisionRetry
// loops back for another attempt.
func IncVisionRetry() {
	globalVisionMetrics.RetryCount.Add(1)
}

// IncVisionFallbackTotal bumps the OCR-fallback attempt counter.
func IncVisionFallbackTotal() {
	globalVisionMetrics.OCRFallbackTotal.Add(1)
}

// IncVisionFallbackSuccess bumps the OCR-fallback success counter.
func IncVisionFallbackSuccess() {
	globalVisionMetrics.OCRFallbackSuccess.Add(1)
}

// AddVisionLatencyRequest accumulates wall-clock time spent in the provider
// SendVisionRequest call.
func AddVisionLatencyRequest(d time.Duration) {
	globalVisionMetrics.LatencyRequestMS.Add(d.Milliseconds())
}

// AddVisionLatencyRetrySleep accumulates wall-clock time spent sleeping
// between retry attempts.
func AddVisionLatencyRetrySleep(d time.Duration) {
	globalVisionMetrics.LatencyRetrySleepMS.Add(d.Milliseconds())
}

// AddVisionLatencyFallback accumulates wall-clock time spent in the OCR
// fallback path.
func AddVisionLatencyFallback(d time.Duration) {
	globalVisionMetrics.LatencyFallbackMS.Add(d.Milliseconds())
}

// AddVisionLatencyParse accumulates wall-clock time spent parsing the
// provider response.
func AddVisionLatencyParse(d time.Duration) {
	globalVisionMetrics.LatencyParseMS.Add(d.Milliseconds())
}

// --- VISION-4: batch metrics helpers ---

// IncVisionBatchAttempt bumps the batch attempt counter.
func IncVisionBatchAttempt() {
	globalVisionMetrics.BatchAttempts.Add(1)
}

// IncVisionBatchHit bumps the batch cache-hit counter.
func IncVisionBatchHit() {
	globalVisionMetrics.BatchHits.Add(1)
}

// IncVisionBatchMiss bumps the batch cache-miss counter.
func IncVisionBatchMiss() {
	globalVisionMetrics.BatchMisses.Add(1)
}

// IncVisionBatchPartialFailure bumps the batch partial-failure counter.
func IncVisionBatchPartialFailure() {
	globalVisionMetrics.BatchPartialFailures.Add(1)
}

// VisionMetricsSnapshot is a stable-by-value snapshot of the metrics state.
type VisionMetricsSnapshot struct {
	ImageTokensTotal       int64            `json:"vision_image_tokens_total"`
	ImageTokensCachedTotal int64            `json:"vision_image_tokens_cached_total"`
	EmbedCallsTotal        int64            `json:"vision_embed_calls_total"`
	OCRCallsTotal          int64            `json:"vision_ocr_calls_total"`
	ResizeEvents           int64            `json:"vision_resize_events"`
	CacheHits              int64            `json:"vision_cache_hits"`
	CacheMisses            int64            `json:"vision_cache_misses"`
	// VISION-5: structured metrics
	RetryCount          int64            `json:"vision_retry_count"`
	OCRFallbackTotal    int64            `json:"vision_ocr_fallback_total"`
	OCRFallbackSuccess  int64            `json:"vision_ocr_fallback_success"`
	LatencyRequestMS    int64            `json:"vision_latency_request_ms"`
	LatencyRetrySleepMS int64            `json:"vision_latency_retry_sleep_ms"`
	LatencyFallbackMS   int64            `json:"vision_latency_fallback_ms"`
	LatencyParseMS      int64            `json:"vision_latency_parse_ms"`
	FailuresByReason    map[string]int64 `json:"vision_failures_by_reason"`
	// VISION-4: batch metrics
	BatchAttempts        int64 `json:"vision_batch_attempts"`
	BatchHits            int64 `json:"vision_batch_hits"`
	BatchMisses          int64 `json:"vision_batch_misses"`
	BatchPartialFailures int64 `json:"vision_batch_partial_failures"`
}

// GetVisionMetrics returns a stable snapshot of the current vision metrics.
func GetVisionMetrics() VisionMetricsSnapshot {
	globalVisionMetrics.mu.RLock()
	failuresCopy := make(map[string]int64, len(globalVisionMetrics.FailuresByReason))
	for k, v := range globalVisionMetrics.FailuresByReason {
		failuresCopy[k] = v
	}
	globalVisionMetrics.mu.RUnlock()

	return VisionMetricsSnapshot{
		ImageTokensTotal:       globalVisionMetrics.ImageTokensTotal.Load(),
		ImageTokensCachedTotal: globalVisionMetrics.ImageTokensCachedTotal.Load(),
		EmbedCallsTotal:        globalVisionMetrics.EmbedCallsTotal.Load(),
		OCRCallsTotal:          globalVisionMetrics.OCRCallsTotal.Load(),
		ResizeEvents:           globalVisionMetrics.ResizeEvents.Load(),
		CacheHits:              globalVisionMetrics.CacheHits.Load(),
		CacheMisses:            globalVisionMetrics.CacheMisses.Load(),
		RetryCount:             globalVisionMetrics.RetryCount.Load(),
		OCRFallbackTotal:       globalVisionMetrics.OCRFallbackTotal.Load(),
		OCRFallbackSuccess:     globalVisionMetrics.OCRFallbackSuccess.Load(),
		LatencyRequestMS:       globalVisionMetrics.LatencyRequestMS.Load(),
		LatencyRetrySleepMS:    globalVisionMetrics.LatencyRetrySleepMS.Load(),
		LatencyFallbackMS:      globalVisionMetrics.LatencyFallbackMS.Load(),
		LatencyParseMS:         globalVisionMetrics.LatencyParseMS.Load(),
		FailuresByReason:       failuresCopy,
		BatchAttempts:          globalVisionMetrics.BatchAttempts.Load(),
		BatchHits:              globalVisionMetrics.BatchHits.Load(),
		BatchMisses:            globalVisionMetrics.BatchMisses.Load(),
		BatchPartialFailures:   globalVisionMetrics.BatchPartialFailures.Load(),
	}
}
