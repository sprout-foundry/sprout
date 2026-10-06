package tools

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/filesystem"
)

// buildImageAttachment reads a local image file and returns a ToolResult
// carrying its inline data-URI (reads up to inlineImageMaxReadBytes,
// attachments bounded by inlineImagePayloadCapBytes via
// prepareInlineAttachmentPayload). Failure degrades to a text-only
// result — analysis output is still valuable.
func buildImageAttachment(ctx context.Context, imagePath string) ToolResult {
	cleanPath, err := filesystem.SafeResolvePathWithBypass(ctx, imagePath)
	if err != nil {
		return ToolResult{}
	}
	info, err := os.Stat(cleanPath)
	if err != nil || info.IsDir() || info.Size() > inlineImageMaxReadBytes {
		return ToolResult{}
	}
	data, err := os.ReadFile(cleanPath)
	if err != nil {
		return ToolResult{}
	}
	_, mimeType := console.DetectImageMagic(data)
	if mimeType == "" {
		mimeType = imageExtensions[strings.ToLower(filepath.Ext(cleanPath))]
	}
	if mimeType == "" {
		return ToolResult{}
	}
	payload, payloadMime, prepErr := prepareInlineAttachmentPayload(cleanPath, data, mimeType)
	if prepErr != nil {
		return ToolResult{}
	}
	return ToolResult{
		Images: []ImageData{{
			URI:      fmt.Sprintf("data:%s;base64,%s", payloadMime, base64.StdEncoding.EncodeToString(payload)),
			MIMEType: payloadMime,
		}},
	}
}
