//go:build js

package tools

import (
	"fmt"
)

const (
	inlineImagePassthroughBytes = 1 << 20
	inlineImageMaxReadBytes     = 100 << 20
	inlineImagePayloadCapBytes  = 2 << 20
)

// prepareInlineAttachmentPayload is the WASM build stub: the optimization
// pipeline (OptimizeImageData) is unavailable under GOOS=js, so payloads
// pass through up to the cap and oversized images degrade to the caller's
// text-only result rather than shipping an oversized body.
func prepareInlineAttachmentPayload(_ string, data []byte, mimeType string) ([]byte, string, error) {
	if len(data) > inlineImageMaxReadBytes {
		return nil, "", fmt.Errorf("image %d bytes exceeds the %d MB read cap for inline attachment", len(data), inlineImageMaxReadBytes>>20)
	}
	if len(data) > inlineImagePayloadCapBytes {
		return nil, "", fmt.Errorf("image %d bytes exceeds the %d MB inline attachment cap", len(data), inlineImagePayloadCapBytes>>20)
	}
	return data, mimeType, nil
}
