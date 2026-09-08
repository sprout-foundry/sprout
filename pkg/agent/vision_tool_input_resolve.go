// Package agent: attached-image fallback resolution for vision tool inputs.
// Moved here from tool_handlers_analysis.go when the dead legacy handlers
// were removed (SP-137 B4). The interface-based handlers in pkg/agent_tools
// are the only execution path; this helper is used by the WebUI bridge to
// resolve image references to the latest attached user image when the
// referenced file is missing.
package agent

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/console"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

func resolveVisionToolInputPath(a *Agent, imagePath string) (string, func(), bool) {
	trimmed := strings.TrimSpace(imagePath)
	if trimmed == "" {
		return imagePath, func() {}, false
	}

	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return imagePath, func() {}, false
	}

	if localFileExists(trimmed) {
		return imagePath, func() {}, false
	}

	img, ok := latestAttachedUserImage(a)
	if !ok {
		return imagePath, func() {}, false
	}

	resolvedPath, cleanup, err := materializeImageDataForVisionTool(img)
	if err != nil {
		if a != nil {
			a.Logger().Debug("[WARN] Failed to materialize attached image fallback for %s: %v\n", imagePath, err)
		}
		return imagePath, func() {}, false
	}

	return resolvedPath, cleanup, true
}

func localFileExists(path string) bool {
	info, err := os.Stat(path)
	if err == nil {
		return !info.IsDir()
	}

	if filepath.IsAbs(path) {
		return false
	}

	absPath, absErr := filepath.Abs(path)
	if absErr != nil {
		return false
	}
	info, err = os.Stat(absPath)
	return err == nil && !info.IsDir()
}

func latestAttachedUserImage(a *Agent) (api.ImageData, bool) {
	if a == nil || a.state == nil {
		return api.ImageData{}, false
	}

	for i := len(a.state.GetMessages()) - 1; i >= 0; i-- {
		msg := a.state.GetMessages()[i]
		if msg.Role != "user" || len(msg.Images) == 0 {
			continue
		}
		return msg.Images[0], true
	}

	return api.ImageData{}, false
}

func materializeImageDataForVisionTool(img api.ImageData) (string, func(), error) {
	if url := strings.TrimSpace(img.URL); url != "" {
		return url, func() {}, nil
	}

	encoded := strings.TrimSpace(img.Base64)
	if encoded == "" {
		return "", nil, agenterrors.NewInvalidInputError("attached image has no URL or base64 data", nil)
	}

	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", nil, agenterrors.NewTool("analysis", "failed to decode attached image data", err)
	}

	ext, _ := console.DetectImageMagic(data)
	if ext == "" {
		ext = extensionForImageMIME(img.Type)
	}
	if ext == "" {
		ext = ".png"
	}

	tempFile, err := os.CreateTemp("", "sprout-attached-image-*"+ext)
	if err != nil {
		return "", nil, agenterrors.NewTool("analysis", "failed to create temp image file", err)
	}
	tempPath := tempFile.Name()
	if _, err := tempFile.Write(data); err != nil {
		tempFile.Close()
		_ = os.Remove(tempPath)
		return "", nil, agenterrors.NewTool("analysis", "failed to write temp image file", err)
	}
	if err := tempFile.Close(); err != nil {
		_ = os.Remove(tempPath)
		return "", nil, agenterrors.NewTool("analysis", "failed to close temp image file", err)
	}

	return tempPath, func() {
		_ = os.Remove(tempPath)
	}, nil
}

func extensionForImageMIME(mimeType string) string {
	switch strings.ToLower(strings.TrimSpace(mimeType)) {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "image/bmp":
		return ".bmp"
	case "image/avif":
		return ".avif"
	default:
		return ""
	}
}
