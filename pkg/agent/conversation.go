package agent

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/console"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"golang.org/x/image/draw"
)

// ProcessQuery handles the main conversation loop with the LLM
func (a *Agent) ProcessQuery(userQuery string) (string, error) {
	return a.processQueryWithSeed(userQuery)
}

func (a *Agent) ProcessQueryWithContinuity(userQuery string) (string, error) {
	if userQuery != "" {
		a.EnableWakeupIfDisabled()
	}
	if notifications := a.DrainNotifications(); len(notifications) > 0 {
		wakeupMsg := FormatWakeupBatch(notifications)
		if userQuery != "" {
			userQuery = wakeupMsg + "\n\n" + userQuery
		} else {
			userQuery = wakeupMsg
		}
	}
	// Commit any uncommitted changes and auto-save state on exit.
	defer func() {
		if a.IsChangeTrackingEnabled() && a.GetChangeCount() > 0 {
			a.Logger().Debug("DEFER: Attempting to commit %d tracked changes\n", a.GetChangeCount())
			if commitErr := a.CommitChanges("Session cleanup - ensuring changes are not lost"); commitErr != nil {
				a.Logger().Debug("Warning: Failed to commit tracked changes during cleanup: %v\n", commitErr)
			} else {
				a.Logger().Debug("DEFER: Successfully committed tracked changes during cleanup\n")
			}
		} else {
			a.Logger().Debug("DEFER: No changes to commit (enabled: %v, count: %d)\n", a.IsChangeTrackingEnabled(), a.GetChangeCount())
		}

		a.autoSaveState()
		a.Logger().Debug("DEFER: Auto-saved memory state\n")
	}()

	if a.state.GetPreviousSummary() != "" {
		a.setPendingSystemSupplement(fmt.Sprintf(
			"## Context From Previous Session\n\n%s\n\nNote: The user cannot see the previous session's responses. Build upon that work but present your response as if it's the first time addressing this topic.",
			a.state.GetPreviousSummary()))
	}

	return a.ProcessQuery(userQuery)
}

func (a *Agent) getOptimizedToolDefinitions(messages []api.Message) []api.Tool {
	tools := BuildToolDefinitionsForAgent(a)

	// LCM allowlist: applied first as the broadest narrowing pass.
	if allow := a.contextProfile.ToolAllowlist; len(allow) > 0 {
		tools = filterToolsByName(tools, makeAllowedToolSet(allow))
	}

	// Filter subagent tools by mode and depth.
	filtered := make([]api.Tool, 0, len(tools))
	for _, tool := range tools {
		if tool.Function.Name == "run_parallel_subagents" {
			if a.contextProfile.Mode == configuration.ContextModeLowContext || !a.CanSpawnSubagents() {
				continue
			}
		}
		if tool.Function.Name == "run_subagent" && !a.CanSpawnSubagents() {
			continue
		}
		filtered = append(filtered, tool)
	}
	tools = filtered

	if mcpTools := a.getMCPTools(); mcpTools != nil {
		tools = append(tools, mcpTools...)
	}

	// Custom provider tool filtering preserves skill and memory tools.
	if customProvider, ok := a.getCurrentCustomProvider(); ok {
		if len(customProvider.ToolCalls) > 0 {
			allowedToolSet := makeAllowedToolSet(customProvider.ToolCalls)
			for _, t := range alwaysIncludedTools {
				allowedToolSet[t] = struct{}{}
			}
			tools = filterToolsByName(tools, allowedToolSet)
		}
	}

	// Persona tool filter skipped in LCM mode (allowlist is final).
	if personaAllowlist := a.getActivePersonaToolAllowlist(); len(personaAllowlist) > 0 &&
		len(a.contextProfile.ToolAllowlist) == 0 {
		tools = filterToolsByName(tools, makeAllowedToolSet(personaAllowlist))
	}

	return tools
}

func (a *Agent) getCurrentCustomProvider() (*configuration.CustomProviderConfig, bool) {
	if a.configManager == nil {
		return nil, false
	}
	config := a.configManager.GetConfig()
	if config == nil || config.CustomProviders == nil {
		return nil, false
	}

	provider, exists := config.CustomProviders[string(a.getClientType())]
	if !exists {
		return nil, false
	}
	return &provider, true
}

// alwaysIncludedTools are always available regardless of custom provider filtering.
var alwaysIncludedTools = []string{
	"list_skills",
	"activate_skill",
	"manage_memory",
	"TodoWrite",
	"TodoRead",
}

func makeAllowedToolSet(toolNames []string) map[string]struct{} {
	toolSet := make(map[string]struct{}, len(toolNames))
	for _, name := range toolNames {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		toolSet[trimmed] = struct{}{}
	}
	return toolSet
}

func filterToolsByName(tools []api.Tool, allowed map[string]struct{}) []api.Tool {
	filtered := make([]api.Tool, 0, len(tools))
	for _, tool := range tools {
		if _, ok := allowed[tool.Function.Name]; !ok {
			continue
		}
		filtered = append(filtered, tool)
	}
	return filtered
}

func (a *Agent) shouldUseDirectMultimodalImageReasoning(messages []api.Message) bool {
	if a == nil || a.client == nil {
		return false
	}
	if !a.effectiveVisionSupport() {
		return false
	}

	for _, msg := range messages {
		if msg.Role != "user" || len(msg.Images) == 0 {
			continue
		}
		return true
	}

	return false
}

func (a *Agent) ClearConversationHistory() {
	a.state.SetMessages([]api.Message{})
	a.clearTurnCheckpoints()
	a.state.SetCurrentIteration(0)
	a.state.SetPreviousSummary("")

	a.Logger().Debug("[clean] Conversation history cleared\n")
}

func (a *Agent) SetConversationOptimization(enabled bool) {
	if a.state.GetOptimizer() != nil {
		a.state.GetOptimizer().SetEnabled(enabled)
		if enabled {
			a.Logger().Debug("[*] Conversation optimization enabled\n")
		} else {
			a.Logger().Debug("[tool] Conversation optimization disabled\n")
		}
	}
}

func (a *Agent) GetOptimizationStats() map[string]interface{} {
	if a.state.GetOptimizer() != nil {
		return a.state.GetOptimizer().GetOptimizationStats()
	}
	return map[string]interface{}{
		"enabled": false,
		"message": "Optimizer not initialized",
	}
}

// Fallback max combined image payload (20 MB) when VisionCapabilities are unavailable.
const maxTotalImagePayloadBytesDefault = 20 * 1024 * 1024

// Matches the placeholder inserted by the console when a user pastes an image.
var pastedImagePlaceholderRe = regexp.MustCompile(`Pasted image saved to disk: (\S+)`)

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

	if c := a.getClient(); c != nil && a.effectiveConversationalVision(c) {
		return a.processImagesAsMultimodal(query)
	}

	paths := extractPastedImagePaths(query)
	if len(paths) == 0 {
		return nil, query, nil
	}

	if c := a.getClient(); c != nil && a.effectiveVisionSupport() {
		enhancedQuery, err := a.processImagesViaOCR(query)
		if err != nil {
			a.Logger().Debug("[WARN] OCR fallback failed: %v\n", err)
			return nil, query, nil
		}
		return nil, enhancedQuery, nil
	}

	return nil, a.buildNonVisionImageToolPrompt(query, paths), nil
}

// effectiveConversationalVision reports whether the model is suitable for
// inline multimodal chat messages, consulting probe ground truth when
// available. If the probe says the model has no vision, we skip the
// conversational path regardless of config flags.
func (a *Agent) effectiveConversationalVision(c api.ClientInterface) bool {
	if probe := a.probeVisionResult(); probe != nil && !*probe {
		return false
	}
	return supportsConversationalVision(c)
}

// supportsConversationalVision reports whether the client's vision capability
// is suitable for inline multimodal chat. Falls back to true when the client
// doesn't implement SupportsConversationalVision (older or non-Ollama clients).
func supportsConversationalVision(c api.ClientInterface) bool {
	if typed, ok := c.(interface{ SupportsConversationalVision() bool }); ok {
		return typed.SupportsConversationalVision()
	}
	return c.SupportsVision()
}

func extractPastedImagePaths(query string) []string {
	uniqueMatches := pastedImagePlaceholderRe.FindAllStringSubmatchIndex(query, -1)
	if len(uniqueMatches) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(uniqueMatches))
	paths := make([]string, 0, len(uniqueMatches))
	for _, loc := range uniqueMatches {
		filePath := strings.TrimSpace(query[loc[2]:loc[3]])
		if filePath == "" {
			continue
		}
		if _, exists := seen[filePath]; exists {
			continue
		}
		seen[filePath] = struct{}{}
		paths = append(paths, filePath)
	}
	return paths
}

func (a *Agent) buildNonVisionImageToolPrompt(query string, paths []string) string {
	var b strings.Builder
	b.WriteString("OCR Trigger Policy (MANDATORY): The active model is non-multimodal. ")
	b.WriteString("Before answering, call analyze_image_content for each pasted image path below. ")
	b.WriteString("Use analysis_mode=\"ocr\" first, then run additional image analysis as needed.\n")
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

	uniqueMatches := pastedImagePlaceholderRe.FindAllStringSubmatchIndex(query, -1)
	if len(uniqueMatches) == 0 {
		return nil, query, nil
	}

	var placeholders []placeholderInfo
	seen := make(map[string]struct{}, len(uniqueMatches))
	for _, loc := range uniqueMatches {
		fullMatch := query[loc[0]:loc[1]]
		filePath := query[loc[2]:loc[3]]
		if _, exists := seen[filePath]; exists {
			continue
		}
		seen[filePath] = struct{}{}
		placeholders = append(placeholders, placeholderInfo{fullMatch: fullMatch, filePath: filePath})
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

	processor, err := tools.NewVisionProcessorWithProvider(a.debug, a.getClientType())
	if err != nil {
		return query, agenterrors.NewAgent("conversation", "failed to create vision processor", err)
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

	result := BatchSplit(sizes, caps)

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

	cleanedQuery += "\n\n## Additional Image Analysis (OCR fallback)\n"
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
