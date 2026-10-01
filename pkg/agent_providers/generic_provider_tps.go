package providers

import (
	"strings"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// generic_provider_tps.go — vision-capability reporting and the
// token-per-second (TPS) throughput stats, plus the MaxTokensHinter /
// ReasoningHistoryReplayer interfaces. Split out of generic_provider.go.
// (Distinct from generic_provider_vision.go, which holds the vision
// request-path methods.)

// VisionCapabilities returns the per-model vision limits (SP-140 Phase 1):
// the provider's capability table, overlaid with the current model's
// model_info.vision_limits entry. A nil config returns safe defaults.
//
// Resolution order per field: model override → provider table →
// VisionCapabilitiesDefault() (applied by callers via
// VisionCapabilitiesOrDefault). Unspecified override fields (zero) keep
// the provider-table value.
func (p *GenericProvider) VisionCapabilities() api.VisionCapabilities {
	if p.config == nil {
		return api.VisionCapabilitiesDefault()
	}
	caps := p.providerVisionTable()

	p.mu.RLock()
	currentModel := strings.TrimSpace(p.model)
	p.mu.RUnlock()
	if currentModel == "" {
		currentModel = strings.TrimSpace(p.config.Defaults.Model)
	}
	if mi := p.config.GetModelInfo(currentModel); mi != nil && mi.VisionLimits != nil {
		v := mi.VisionLimits
		if v.MaxImageBytes > 0 {
			caps.MaxImageBytes = v.MaxImageBytes
		}
		if v.MaxImageCount > 0 {
			caps.MaxImageCount = v.MaxImageCount
		}
		if v.MaxImageDimension > 0 {
			caps.MaxImageDimension = v.MaxImageDimension
		}
	}
	return caps
}

// providerVisionTable returns the per-provider vision limits. The caller
// must ensure p.config is non-nil (VisionCapabilities guards it).
func (p *GenericProvider) providerVisionTable() api.VisionCapabilities {
	switch p.config.Name {
	case "anthropic":
		return api.VisionCapabilities{
			MaxImageBytes:     5_000_000,
			MaxImageCount:     20,
			MaxImageDimension: 1568,
		}
	case "openai":
		return api.VisionCapabilities{
			MaxImageBytes:     20_000_000,
			MaxImageCount:     10,
			MaxImageDimension: 2048,
			DetailTiers:       []string{"low", "high", "auto"},
		}
	case "gemini":
		return api.VisionCapabilities{
			MaxImageBytes:     20_000_000,
			MaxImageCount:     10,
			MaxImageDimension: 3072,
		}
	default:
		return api.VisionCapabilities{
			MaxImageBytes:     20_000_000,
			MaxImageCount:     500,
			MaxImageDimension: 2048,
			DetailTiers:       []string{"low", "high", "auto"},
		}
	}
}

// TPS tracking methods (no-op placeholders)
func (p *GenericProvider) GetLastTPS() float64 {
	return 0.0
}

func (p *GenericProvider) GetAverageTPS() float64 {
	return 0.0
}

func (p *GenericProvider) GetTPSStats() map[string]float64 {
	return map[string]float64{}
}

func (p *GenericProvider) ResetTPSStats() {
}

// MaxTokensHinter lets callers pass a pre-computed max_tokens to the provider.
type MaxTokensHinter interface {
	SetMaxTokensHint(tokens int)
}

// ReasoningHistoryReplayer reports whether the provider replays historical
// assistant ReasoningContent on the wire (flat reasoning_content_field or
// structured reasoning_details). Token estimation that runs against the
// wire-visible view must exclude reasoning mass when this returns false —
// the provider never bills or sees those tokens.
type ReasoningHistoryReplayer interface {
	ReplaysReasoningHistory() bool
}

// ReplaysReasoningHistory reports whether converted assistant messages
// carry historical reasoning content on requests. False when the provider
// config neither sets a reasoning_content_field nor preserves structured
// reasoning_details.
func (p *GenericProvider) ReplaysReasoningHistory() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.config.Conversion.ReasoningContentField != "" || p.config.Conversion.PreserveReasoningDetails
}

// SetMaxTokensHint sets a pre-computed max_tokens override (0 to clear).
func (p *GenericProvider) SetMaxTokensHint(tokens int) {
	p.maxTokensHintMu.Lock()
	p.maxTokensHint = tokens
	p.maxTokensHintMu.Unlock()
}

// --- Functions split into separate files ---
//
// generic_provider_http_errors.go:
//   maxProviderErrorBodyPreview, formatProviderHTTPError, formatResponseHeaders,
//   summarizeProviderHTTPError, extractProviderJSONErrorMessage,
//   extractProviderJSONErrorField, looksLikeProviderHTMLErrorPage,
//   summarizeProviderHTMLErrorPage, extractProviderHTMLTitle,
//   limitProviderErrorText, modelInfoHasVisionTag
//
// generic_provider_request.go:
//   buildChatRequest, applyReasoningEffort, applyDisableThinking,
//   ensureModel, applyModelSpecificSettings, shouldRetryWithMaxCompletionTokens,
//   rewriteMaxTokensToMaxCompletionTokens, buildHTTPRequest, buildHTTPRequestCtx
//
// generic_provider_streaming.go:
//   SendChatRequestStream, handleStreamingResponse
//
// generic_provider_vision.go:
//   SupportsVision, GetVisionModel, SendVisionRequest,
//   buildMultiModalContent, buildImageURL
//
// generic_provider_messages.go:
//   convertMessages, shouldSkipReasoningContentHistory, convertToolCalls,
//   getModelCompletionLimit
//
// generic_provider_retry.go:
//   tryMaxCompletionTokensRetry
