package tools

import (
	"sync/atomic"
)

// VisionMetrics holds in-memory counters for vision pipeline observability.
// These are surfaced via GetVisionMetrics() and emitted to the OpenTelemetry
// metrics sink when one is configured. They are deliberately atomic-only
// (no mutex) — counters can be incremented in tight loops without lock
// contention.
//
// SP-103-C4: metrics + observability for vision image tokens.
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
}

// globalVisionMetrics is the package-level metrics instance. It is updated
// from hot paths; readers should call GetVisionMetrics() to take a stable
// snapshot.
var globalVisionMetrics VisionMetrics

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

// VisionMetricsSnapshot is a stable-by-value snapshot of the metrics state.
type VisionMetricsSnapshot struct {
	ImageTokensTotal       int64 `json:"vision_image_tokens_total"`
	ImageTokensCachedTotal int64 `json:"vision_image_tokens_cached_total"`
	EmbedCallsTotal        int64 `json:"vision_embed_calls_total"`
	OCRCallsTotal          int64 `json:"vision_ocr_calls_total"`
	ResizeEvents           int64 `json:"vision_resize_events"`
	CacheHits              int64 `json:"vision_cache_hits"`
	CacheMisses            int64 `json:"vision_cache_misses"`
}

// GetVisionMetrics returns a stable snapshot of the current vision metrics.
func GetVisionMetrics() VisionMetricsSnapshot {
	return VisionMetricsSnapshot{
		ImageTokensTotal:       globalVisionMetrics.ImageTokensTotal.Load(),
		ImageTokensCachedTotal: globalVisionMetrics.ImageTokensCachedTotal.Load(),
		EmbedCallsTotal:        globalVisionMetrics.EmbedCallsTotal.Load(),
		OCRCallsTotal:          globalVisionMetrics.OCRCallsTotal.Load(),
		ResizeEvents:           globalVisionMetrics.ResizeEvents.Load(),
		CacheHits:              globalVisionMetrics.CacheHits.Load(),
		CacheMisses:            globalVisionMetrics.CacheMisses.Load(),
	}
}
