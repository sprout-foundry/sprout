package tools

import (
	"strconv"
	"sync"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/utils"
)

const (
	visionMaxReturnedTextChars = 20000 // Raised from 12000 to 20000 for better PDF/doc coverage
)

// Error codes for vision analysis and remote operations
const (
	ErrCodeRemoteFetchFailed   = "REMOTE_FETCH_FAILED"
	ErrCodeOCRNoTextDetected   = "OCR_NO_TEXT_DETECTED"
	ErrCodeVisionNotAvailable  = "VISION_NOT_AVAILABLE"
	ErrCodeVisionRequestFailed = "VISION_REQUEST_FAILED"
	ErrCodeInvalidResponse     = "INVALID_RESPONSE"
)

// VisionUsageInfo contains token usage and cost information from vision model calls
type VisionUsageInfo struct {
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	EstimatedCost    float64 `json:"estimated_cost"`
}

// VisionProcessor handles image analysis using vision-capable models
type VisionProcessor struct {
	visionClient api.ClientInterface
	logger       *utils.Logger
	debug        bool
	usage        *VisionUsageInfo // Per-session usage tracking (SP-103-A4)
}

// ============================================================================
// Caching and Usage Tracking
// ============================================================================

// visionLastUsageMirror provides thread-safe access to the most recent
// vision usage info across all VisionProcessor sessions.
type visionLastUsageMirror struct {
	mu    sync.RWMutex
	usage *VisionUsageInfo
}

var lastUsageMirror = &visionLastUsageMirror{}

// recordVisionUsage writes usage info to both the per-session processor
// and the cross-session global mirror. Call this from any place that
// previously wrote to the package-global lastVisionUsage.
//
// If vp is nil (e.g., cache hit before a processor is created), only the
// global mirror is updated. If usage is nil, nothing happens.
func recordVisionUsage(vp *VisionProcessor, usage *VisionUsageInfo) {
	if vp != nil {
		vp.usage = usage
	}
	if usage == nil {
		return
	}
	lastUsageMirror.mu.Lock()
	lastUsageMirror.usage = usage
	lastUsageMirror.mu.Unlock()
}

// GetLastVisionUsage returns the usage information from the most recent
// vision model call across all sessions. Thread-safe.
func GetLastVisionUsage() *VisionUsageInfo {
	lastUsageMirror.mu.RLock()
	defer lastUsageMirror.mu.RUnlock()
	return lastUsageMirror.usage
}

// ClearLastVisionUsage clears the stored vision usage information.
// Thread-safe.
func ClearLastVisionUsage() {
	lastUsageMirror.mu.Lock()
	lastUsageMirror.usage = nil
	lastUsageMirror.mu.Unlock()
}

// LastUsage returns the per-session usage info for this VisionProcessor.
// Returns nil if no vision call has been made with this processor yet.
func (vp *VisionProcessor) LastUsage() *VisionUsageInfo {
	return vp.usage
}

// getVisionMaxReturnedTextChars returns the max text chars limit from env or default
func getVisionMaxReturnedTextChars() int {
	if raw := configuration.GetEnvSimple("VISION_MAX_TEXT_CHARS"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			return parsed
		}
	}
	return visionMaxReturnedTextChars
}

// GetVisionCacheStats returns statistics about vision result caching
func GetVisionCacheStats() map[string]interface{} {
	visionLRU.mu.Lock()
	defer visionLRU.mu.Unlock()

	stats := make(map[string]interface{})
	stats["cached_results"] = int(visionLRU.stats.Size.Load())
	stats["hits"] = int(visionLRU.stats.Hits.Load())
	stats["misses"] = int(visionLRU.stats.Misses.Load())
	stats["evictions"] = int(visionLRU.stats.Evictions.Load())
	stats["insertions"] = int(visionLRU.stats.Insertions.Load())
	stats["capacity"] = visionLRU.capacity

	// Compute estimated savings from cached usage info
	totalSavedCost := 0.0
	for _, e := range visionLRU.entries {
		if e.usage != nil {
			totalSavedCost += e.usage.EstimatedCost
		}
	}
	stats["estimated_savings"] = totalSavedCost

	return stats
}
