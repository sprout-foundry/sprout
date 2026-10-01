//go:build !js

package tools

import (
	"fmt"
)

const (
	// inlineImagePassthroughBytes: payloads at or under this size attach
	// raw — no decode/re-encode (avoids CPU and format churn on the common
	// small-image case).
	inlineImagePassthroughBytes = 1 << 20
	// inlineImageMaxReadBytes: files larger than this are never read — no
	// optimization can make a 100MB file fit the per-image cap, and reading
	// it would only spike memory.
	inlineImageMaxReadBytes = 100 << 20
	// inlineImagePayloadCapBytes: hard post-optimization cap for one inline
	// image (raw bytes; ~1.33x in base64). Sized with maxLiveRequestImages
	// (pkg/agent) so N images plus prompt text stay under a conservative
	// 10MB server body limit.
	inlineImagePayloadCapBytes = 2 << 20
)

// prepareInlineAttachmentPayload prepares raw image bytes for inline
// multimodal attachment. Payloads at or under inlineImagePassthroughBytes
// pass through untouched; larger ones run through OptimizeImageData
// (dimension + JPEG quality cascade) so the wire payload stays within the
// per-image cap. This is what keeps bulk image turns (a turn that reads a
// dozen screenshots) from growing an HTTP body that outgrows the provider
// server's body limit (413).
//
// A non-nil err means the image cannot be represented within the cap —
// an oversized format the stdlib cannot decode (webp/avif), or still over
// after the optimization cascade. Callers degrade to their text-only
// result (analysis output, summary, or an error note), never to a raw
// oversized payload.
func prepareInlineAttachmentPayload(path string, data []byte, mimeType string) ([]byte, string, error) {
	if len(data) > inlineImageMaxReadBytes {
		return nil, "", fmt.Errorf("image %d bytes exceeds the %d MB read cap for inline attachment", len(data), inlineImageMaxReadBytes>>20)
	}
	if len(data) <= inlineImagePassthroughBytes {
		return data, mimeType, nil
	}

	optimized, optMime, optErr := OptimizeImageData(path, data)
	if optErr != nil || len(optimized) == 0 {
		optimized, optMime = data, mimeType
	}
	if len(optimized) > inlineImagePayloadCapBytes {
		return nil, "", fmt.Errorf("image %d bytes exceeds the %d MB inline attachment cap after optimization", len(optimized), inlineImagePayloadCapBytes>>20)
	}
	return optimized, optMime, nil
}
