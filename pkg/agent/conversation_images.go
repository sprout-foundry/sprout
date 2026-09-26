package agent

// conversation_images.go — the image-processing layer of the query
// pipeline: pasted-image extraction, the resize + payload-cap
// plumbing, the multimodal / OCR / delegation strategies, and the
// placeholder batch-splitting helpers. Split out of conversation.go.
import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/console"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"golang.org/x/image/draw"
)

// Fallback max combined image payload (20 MB) when VisionCapabilities are unavailable.
const maxTotalImagePayloadBytesDefault = 20 * 1024 * 1024

// Matches the placeholder inserted by the console when a user pastes an image.
// Legacy paste placeholder regex lives in pkg/console (legacyPastedImageRe);
// parsing goes through console.ParsePastedImagePlaceholders (SP-140).

// Fallback longest-edge cap for embedded images (1568px, per Anthropic recommendation).
const visionEmbedMaxEdgePxDefault = 1568

// resizeImageForVisionEmbed caps the long edge at maxEdgePx using bilinear resampling.
func resizeImageForVisionEmbed(data []byte, maxEdgePx int) ([]byte, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return data, nil // unsupported format, pass through
	}
	_ = format

	longEdge := cfg.Width
	if cfg.Height > longEdge {
		longEdge = cfg.Height
	}
	if longEdge <= maxEdgePx {
		return data, nil
	}

	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return data, nil
	}

	scale := float64(maxEdgePx) / float64(longEdge)
	newW := int(float64(cfg.Width)*scale + 0.5)
	newH := int(float64(cfg.Height)*scale + 0.5)
	if newW < 1 {
		newW = 1
	}
	if newH < 1 {
		newH = 1
	}

	// Resize with bilinear interpolation.
	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	resample := draw.BiLinear
	resample.Scale(dst, dst.Rect, img, img.Bounds(), draw.Over, nil)

	// Re-encode as JPEG at quality 85.
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 85}); err != nil {
		return data, agenterrors.NewAgent("conversation", "jpeg encode after resize", err)
	}
	return buf.Bytes(), nil
}

// processImagesInQuery detects and processes images in user queries.
func (a *Agent) processImagesInQuery(query string) ([]api.ImageData, string, error) {
	if a.client == nil {
		return nil, query, nil
	}

	if c := a.getClient(); c != nil && api.ResolveVisionCapability(c).AcceptsImages {
		return a.processImagesAsMultimodal(query)
	}

	paths := extractPastedImagePaths(query)
	if len(paths) == 0 {
		return nil, query, nil
	}

	// Non-vision primary: delegate to a vision model when one exists;
	// otherwise fall back to steering the model at analyze_image_content.
	enhanced, delegated := a.processImagesByDelegation(query, paths)
	if delegated {
		a.Logger().Debug("[img] Delegated %d pasted image(s) to a vision model\n", len(paths))
	}
	return nil, enhanced, nil
}

func extractPastedImagePaths(query string) []string {
	return console.ParsePastedImagePlaceholders(query)
}

// processImagesByDelegation is the inline chat path's delegation rung
// (SP-140 Phase 3): the primary model cannot see, so pasted images are
// described by the best available vision model and the structured,
// provenance-labeled descriptions are injected into the query. Falls back
// to the analyze-tool prompt when no delegation client exists — the model
// can still help itself via analyze_image_content.
func (a *Agent) processImagesByDelegation(query string, paths []string) (string, bool) {
	if !tools.HasVisionCapability() {
		return a.buildNonVisionImageToolPrompt(query, paths), false
	}

	var b strings.Builder
	delegated := 0
	for i, path := range paths {
		delegate, err := tools.DelegateImageDescriptions(a.InterruptCtx(), nil, path)
		if err != nil {
			a.Logger().Debug("[WARN] image delegation failed for %s: %v\n", path, err)
			b.WriteString(fmt.Sprintf("[image %d of %d: %s — description unavailable]\n", i+1, len(paths), filepath.Base(path)))
			continue
		}
		delegated++
		b.WriteString(fmt.Sprintf("[image %d of %d: %s — described via %s/%s]\n%s\n\n",
			i+1, len(paths), filepath.Base(path), delegate.Provider, delegate.Model, delegate.Description))
	}

	if delegated == 0 {
		return a.buildNonVisionImageToolPrompt(query, paths), false
	}

	b.WriteString("\nOriginal user request (images above were described by a separate vision model; call analyze_image_content for higher-fidelity analysis):\n")
	b.WriteString(query)
	return b.String(), true
}

// buildNonVisionImageToolPrompt steers a non-multimodal model to the
// analyze_image_content tool. Kept as the final fallback when delegation
// itself is unavailable (no vision client at all).
func (a *Agent) buildNonVisionImageToolPrompt(query string, paths []string) string {
	var b strings.Builder
	b.WriteString("Image Analysis Policy: The active model is non-multimodal. ")
	b.WriteString("Before answering, call analyze_image_content for each pasted image path below. ")
	b.WriteString("Use the default mode (general) first — it describes layout, text, and UI structure. ")
	b.WriteString("Only follow with analysis_mode=\"ocr\" if you need verbatim text extraction.\n")
	b.WriteString("Pasted image paths:\n")
	for _, path := range paths {
		b.WriteString("- ")
		b.WriteString(path)
		b.WriteString("\n")
	}
	b.WriteString("\nOriginal user request:\n")
	b.WriteString(query)
	return b.String()
}

// processImagesAsMultimodal extracts pasted-image references and returns image data for multimodal embedding.
func (a *Agent) processImagesAsMultimodal(query string) ([]api.ImageData, string, error) {
	cwd := a.currentWorkspaceRoot()

	caps := api.VisionCapabilitiesDefault()
	if c := a.getClient(); c != nil {
		caps = api.VisionCapabilitiesOrDefault(c.VisionCapabilities())
	}
	maxEdgePx := caps.MaxImageDimension
	maxImageBytes := caps.MaxImageBytes
	maxImageCount := caps.MaxImageCount
	maxTotalImagePayloadBytes := maxImageBytes * maxImageCount
	if maxTotalImagePayloadBytes < maxTotalImagePayloadBytesDefault {
		maxTotalImagePayloadBytes = maxTotalImagePayloadBytesDefault
	}

	var images []api.ImageData
	totalBytes := 0

	imagePaths := console.ParsePastedImagePlaceholders(query)
	if len(imagePaths) == 0 {
		return nil, query, nil
	}

	var placeholders []placeholderInfo
	for _, filePath := range imagePaths {
		placeholders = append(placeholders, placeholderInfo{
			fullMatch: console.PastedImagePlaceholder(filePath),
			filePath:  filePath,
		})
	}

	inlinePlaceholders, overflowPlaceholders := a.splitPlaceholdersWithBatchSplit(placeholders, caps, maxImageCount, maxTotalImagePayloadBytes)

	// Rewrite the query, labeling images numerically for multi-image queries.
	cleanedQuery := query
	totalImages := len(placeholders)
	multi := totalImages > 1
	for i, ph := range inlinePlaceholders {
		fileName := filepath.Base(ph.filePath)
		var replacement string
		if multi {
			replacement = fmt.Sprintf("[image %d of %d: %s]", i+1, totalImages, fileName)
		} else {
			replacement = fmt.Sprintf("[image: %s]", fileName)
		}
		cleanedQuery = strings.ReplaceAll(cleanedQuery, ph.fullMatch, replacement)
	}
	for i, ph := range overflowPlaceholders {
		fileName := filepath.Base(ph.filePath)
		idx := len(inlinePlaceholders) + i + 1
		cleanedQuery = strings.ReplaceAll(cleanedQuery, ph.fullMatch,
			fmt.Sprintf("[image %d of %d: %s]", idx, totalImages, fileName))
	}

	// Load inline image files, enforcing directory containment and size caps.
	expectedDir := filepath.Join(cwd, console.PastedImageDirName)
	for _, ph := range inlinePlaceholders {
		filePath := ph.filePath

		if !filepath.IsAbs(filePath) {
			filePath = filepath.Join(cwd, filePath)
		}

		relToExpected, err := filepath.Rel(expectedDir, filePath)
		if err != nil || strings.HasPrefix(relToExpected, "..") {
			a.Logger().Debug("[WARN] Skipping image %s: not in pasted images directory\n", filePath)
			continue
		}

		imgData, imgSize, err := readImageAsImageData(filePath, maxEdgePx)
		if err != nil {
			a.Logger().Debug("[WARN] Skipping image %s: %v\n", filePath, err)
			continue
		}

		perImageCap := maxImageBytes
		if perImageCap <= 0 {
			perImageCap = console.MaxPastedImageSize
		}
		if imgSize > perImageCap {
			a.Logger().Debug("[WARN] Skipping image %s: exceeds per-image size cap (%d > %d)\n",
				filePath, imgSize, perImageCap)
			continue
		}

		if totalBytes+imgSize > maxTotalImagePayloadBytes {
			a.Logger().Debug("[WARN] Skipping image %s: total payload would exceed cap (%d bytes)\n",
				filePath, maxTotalImagePayloadBytes)
			continue
		}

		totalBytes += imgSize
		images = append(images, imgData)
	}

	if len(images) > 0 {
		a.Logger().Debug("[img] Attached %d image(s) as multimodal content (%d bytes)\n", len(images), totalBytes)
	}

	if len(overflowPlaceholders) > 0 {
		cleanedQuery = a.appendOCRFallback(cleanedQuery, overflowPlaceholders)
	}

	return images, cleanedQuery, nil
}

// processImagesViaOCR converts images to text descriptions via the VisionProcessor.
func (a *Agent) processImagesViaOCR(query string) (string, error) {
	if !tools.HasVisionCapability() {
		return query, nil
	}

	// The agent's cached processor (agent_accessors.go): one resolution per
	// provider swap, not one per call — and the same tier the design tools
	// receive via env.VisionProcessor, so every consumer agrees.
	processor := a.GetVisionProcessor()
	if processor == nil {
		return query, agenterrors.NewAgent("conversation", "failed to create vision processor", nil)
	}

	enhancedQuery, analyses, err := processor.ProcessImagesInText(a.InterruptCtx(), query)
	if err != nil {
		return query, agenterrors.NewAgent("conversation", "failed to process images", err)
	}

	if len(analyses) > 0 {
		a.Logger().Debug("[img] Processed %d image(s) and enhanced query with vision analysis\n", len(analyses))
		for _, analysis := range analyses {
			a.Logger().Debug("  - %s: %s\n", analysis.ImagePath, analysis.Description[:min(100, len(analysis.Description))])
		}
	}

	return enhancedQuery, nil
}

type placeholderInfo struct {
	fullMatch string
	filePath  string
}

// splitPlaceholdersWithBatchSplit splits images into inline and overflow lists using byte-aware BatchSplit.
func (a *Agent) splitPlaceholdersWithBatchSplit(placeholders []placeholderInfo, caps api.VisionCapabilities, maxImageCount int, maxTotalImagePayloadBytes int) (inline, overflow []placeholderInfo) {
	if len(placeholders) == 0 {
		return placeholders, nil
	}

	sizes := make([]int, len(placeholders))
	for i, ph := range placeholders {
		stat, err := os.Stat(ph.filePath)
		if err != nil {
			sizes[i] = 0
			continue
		}
		sizes[i] = int(stat.Size())
	}

	result := tools.BatchSplit(sizes, caps)

	inline = make([]placeholderInfo, 0, len(result.InlineIndices))
	overflow = make([]placeholderInfo, 0, len(result.OverflowIndices))

	for _, idx := range result.InlineIndices {
		if idx >= 0 && idx < len(placeholders) {
			inline = append(inline, placeholders[idx])
		}
	}
	for _, idx := range result.OverflowIndices {
		if idx >= 0 && idx < len(placeholders) {
			overflow = append(overflow, placeholders[idx])
		}
	}

	if len(overflow) > 0 {
		a.Logger().Debug("[WARN] Query has %d images, but provider %s supports at most %d inline with ~%d bytes total; %d will be processed via OCR fallback\n",
			len(placeholders), a.getClientType(), maxImageCount, maxTotalImagePayloadBytes, len(overflow))
	}

	return inline, overflow
}

// appendOCRFallback processes overflow images through OCR and appends the descriptions.
func (a *Agent) appendOCRFallback(cleanedQuery string, overflowPlaceholders []placeholderInfo) string {
	if len(overflowPlaceholders) == 0 {
		return cleanedQuery
	}

	var ocrBuilder strings.Builder
	ocrBuilder.WriteString("Please analyze the following images:\n")
	for _, ph := range overflowPlaceholders {
		ocrBuilder.WriteString(ph.filePath)
		ocrBuilder.WriteString("\n")
	}
	ocrQuery := ocrBuilder.String()

	enhanced, err := a.processImagesViaOCR(ocrQuery)
	if err != nil {
		a.Logger().Debug("[WARN] OCR fallback for %d overflow image(s) failed: %v\n",
			len(overflowPlaceholders), err)
		for _, ph := range overflowPlaceholders {
			cleanedQuery += fmt.Sprintf("\n[OCR analysis unavailable for %s]", filepath.Base(ph.filePath))
		}
		return cleanedQuery
	}

	a.Logger().Debug("[img] OCR fallback processed %d overflow image(s)\n", len(overflowPlaceholders))

	cleanedQuery += "\n\n## Additional Image Analysis (OCR fallback — descriptions via remote vision model)\n"
	cleanedQuery += enhanced

	return cleanedQuery
}

// readImageAsImageData reads, validates, optimizes, and base64-encodes an image for vision embedding.
func readImageAsImageData(filePath string, maxEdgePx int) (api.ImageData, int, error) {
	stat, err := os.Stat(filePath)
	if err != nil {
		return api.ImageData{}, 0, agenterrors.NewAgent("conversation", "failed to stat file", err)
	}
	if stat.Size() > console.MaxPastedImageSize {
		return api.ImageData{}, 0, agenterrors.NewInvalidInputError(fmt.Sprintf("image too large (%d bytes)", stat.Size()), nil)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return api.ImageData{}, 0, agenterrors.NewAgent("conversation", "failed to read file", err)
	}

	_, mimeType := console.DetectImageMagic(data)
	if mimeType == "" {
		return api.ImageData{}, 0, agenterrors.NewInvalidInputError("unrecognised image format", nil)
	}

	optimized, optMime, optErr := tools.OptimizeImageData(filePath, data)
	if optErr == nil && len(optimized) > 0 {
		mimeType = optMime
		data = optimized
	}

	// Resize to cap long edge. NOTE: chaining nearest-neighbor (OptimizeImageData) then bilinear
	// may compound artifacts for very large inputs (>4096px).
	resized, resizeErr := resizeImageForVisionEmbed(data, maxEdgePx)
	if resizeErr == nil && len(resized) > 0 {
		if !bytes.Equal(resized, data) {
			data = resized
			mimeType = "image/jpeg"
		}
	}

	encoded := base64.StdEncoding.EncodeToString(data)
	return api.ImageData{
		Base64: encoded,
		Type:   mimeType,
	}, len(data), nil
}
