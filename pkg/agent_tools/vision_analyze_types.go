package tools

import (
	"strconv"

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
}

// ============================================================================
// Caching and Usage Tracking
// ============================================================================

// getVisionMaxReturnedTextChars returns the max text chars limit from env or default
func getVisionMaxReturnedTextChars() int {
	if raw := configuration.GetEnvSimple("VISION_MAX_TEXT_CHARS"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			return parsed
		}
	}
	return visionMaxReturnedTextChars
}
