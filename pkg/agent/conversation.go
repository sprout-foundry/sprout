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

// ProcessQueryWithContinuity processes a query with continuity from previous actions
func (a *Agent) ProcessQueryWithContinuity(userQuery string) (string, error) {
	// SP-108: Re-enable auto-resume when the user sends a manual message.
	if userQuery != "" {
		a.EnableWakeupIfDisabled()
	}
	// Drain pending background-task notifications and prepend them.
	if notifications := a.DrainNotifications(); len(notifications) > 0 {
		wakeupMsg := FormatWakeupBatch(notifications)
		if userQuery != "" {
			userQuery = wakeupMsg + "\n\n" + userQuery
		} else {
			userQuery = wakeupMsg
		}
	}
	// Ensure changes are committed even if there are unexpected errors or early termination
	defer func() {
		// Only commit if we have changes and they haven't been committed yet
		if a.IsChangeTrackingEnabled() && a.GetChangeCount() > 0 {
			a.Logger().Debug("DEFER: Attempting to commit %d tracked changes\n", a.GetChangeCount())
			// Check if changes are already committed by trying to commit (it's safe due to committed flag)
			if commitErr := a.CommitChanges("Session cleanup - ensuring changes are not lost"); commitErr != nil {
				a.Logger().Debug("Warning: Failed to commit tracked changes during cleanup: %v\n", commitErr)
			} else {
				a.Logger().Debug("DEFER: Successfully committed tracked changes during cleanup\n")
			}
		} else {
			a.Logger().Debug("DEFER: No changes to commit (enabled: %v, count: %d)\n", a.IsChangeTrackingEnabled(), a.GetChangeCount())
		}

		// Auto-save memory state after every successful turn
		a.autoSaveState()
		a.Logger().Debug("DEFER: Auto-saved memory state\n")
	}()

	// Load previous state if available
	if a.state.GetPreviousSummary() != "" {
		// Inject the summary as a one-shot system supplement so it is attributed to
		// the system (not the user) and does not consume the user input budget.
		a.setPendingSystemSupplement(fmt.Sprintf(
			"## Context From Previous Session\n\n%s\n\nNote: The user cannot see the previous session's responses. Build upon that work but present your response as if it's the first time addressing this topic.",
			a.state.GetPreviousSummary()))
	}

	// Process the user's actual query, with or without previous context.
	return a.ProcessQuery(userQuery)
}

// getOptimizedToolDefinitions returns tool definitions optimized based on conversation context
func (a *Agent) getOptimizedToolDefinitions(messages []api.Message) []api.Tool {
	// Start with standard tools. Pulls from the canonical registry
	// (pkg/agent/tool_registrations.go) via BuildToolDefinitions —
	// the same registry seedRegistry uses, so the LLM and this
	// optimisation path stay in sync.
	tools := BuildToolDefinitions()

	// Filter out run_subagent and run_parallel_subagents when
	// the agent is not allowed to spawn subagents (depth limit or NO_SUBAGENTS env).
	if !a.CanSpawnSubagents() {
		filtered := make([]api.Tool, 0, len(tools))
		for _, tool := range tools {
			// Skip run_subagent and run_parallel_subagents
			if tool.Function.Name == "run_subagent" || tool.Function.Name == "run_parallel_subagents" {
				continue
			}
			filtered = append(filtered, tool)
		}
		tools = filtered
	}

	// Add MCP tools if available
	mcpTools := a.getMCPTools()
	if mcpTools != nil {
		tools = append(tools, mcpTools...)
	}

	// For custom providers, apply tool filtering only when tool_calls is explicitly configured.
	// Always preserve skill and memory tools regardless of the allowlist — these are
	// lightweight context tools that should never be hidden from the model.
	if customProvider, ok := a.getCurrentCustomProvider(); ok {
		if len(customProvider.ToolCalls) > 0 {
			allowedToolSet := makeAllowedToolSet(customProvider.ToolCalls)
			// Always include skill and memory tools so models can discover and use them
			for _, t := range alwaysIncludedTools {
				allowedToolSet[t] = struct{}{}
			}
			tools = filterToolsByName(tools, allowedToolSet)
		}
	}

	// Apply active persona tool filter (used for direct /persona and subagent persona runs).
	if personaAllowlist := a.getActivePersonaToolAllowlist(); len(personaAllowlist) > 0 {
		tools = filterToolsByName(tools, makeAllowedToolSet(personaAllowlist))
	}

	// Vision models retain access to analyze_image_content and analyze_ui_screenshot tools
	// even when direct multimodal images are present. This allows the agent to:
	// - Analyze images from URLs or file paths mentioned in the conversation
	// - Use specialized analysis modes (OCR, frontend analysis, etc.)
	// - Get viewport-adjusted analysis for HTML files
	// Direct multimodal images and tool-based analysis are complementary, not mutually exclusive.

	// Future: Could optimize by analyzing conversation context
	// and only returning relevant tools
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

// alwaysIncludedTools are tools that must always be available to models regardless
// of custom provider tool_calls filtering. These are lightweight context tools
// for skill discovery, memory, and self-management that should never be hidden.
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

// ClearConversationHistory clears the conversation history
func (a *Agent) ClearConversationHistory() {
	// Keep messages empty; system prompt is added during prepareMessages
	a.state.SetMessages([]api.Message{})
	a.clearTurnCheckpoints()
	a.state.SetCurrentIteration(0)
	a.state.SetPreviousSummary("")

	a.Logger().Debug("[clean] Conversation history cleared\n")
}

// SetConversationOptimization enables or disables conversation optimization
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

// GetOptimizationStats returns optimization statistics
func (a *Agent) GetOptimizationStats() map[string]interface{} {
	if a.state.GetOptimizer() != nil {
		return a.state.GetOptimizer().GetOptimizationStats()
	}
	return map[string]interface{}{
		"enabled": false,
		"message": "Optimizer not initialized",
	}
}

// maxTotalImagePayloadBytes is the maximum combined size of all images sent in a
// single query (20 MB).  Individual images are capped by console.MaxPastedImageSize.
const maxTotalImagePayloadBytes = 20 * 1024 * 1024

// pastedImagePlaceholderRe matches the placeholder inserted by the console
// when a user pastes an image.  ONLY this pattern is considered safe to load
// and send as multimodal content — arbitrary file paths in user text are ignored.
var pastedImagePlaceholderRe = regexp.MustCompile(`Pasted image saved to disk: (\S+)`)

// visionEmbedMaxEdgePx caps the longest edge of embedded images for multimodal
// LLM input.  Anthropic recommends ≤1568px for the best cost/quality trade-off.
const visionEmbedMaxEdgePx = 1568

// resizeImageForVisionEmbed caps the long edge of the decoded image at
// visionEmbedMaxEdgePx using bilinear resampling, re-encoding as JPEG
// at quality 85. Returns the input unchanged if the long edge is
// already within the cap or the bytes cannot be decoded by stdlib.
func resizeImageForVisionEmbed(data []byte) ([]byte, error) {
	// Fast path: check config without full decode.
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		// Format not supported by stdlib (e.g., webp/avif) — pass through.
		return data, nil
	}
	_ = format // format is already handled by OptimizeImageData upstream

	longEdge := cfg.Width
	if cfg.Height > longEdge {
		longEdge = cfg.Height
	}
	if longEdge <= visionEmbedMaxEdgePx {
		return data, nil
	}

	// Decode the full image.
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return data, nil
	}

	// Calculate new dimensions preserving aspect ratio.
	scale := float64(visionEmbedMaxEdgePx) / float64(longEdge)
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
// If the primary model supports vision it returns the image data as multimodal
// content so the model can see the images directly.  Otherwise it falls back
// to the existing OCR pipeline which converts images to text descriptions.
func (a *Agent) processImagesInQuery(query string) ([]api.ImageData, string, error) {
	// Skip if no client is available
	if a.client == nil {
		return nil, query, nil
	}

	// Multimodal path: if the active client supports *conversational* vision
	// (chat models that handle inline image content), embed pasted images
	// as multimodal content and strip the placeholder text. OCR-only models
	// (glm-ocr etc.) flow through the tool path below.
	if c := a.getClient(); c != nil && a.effectiveConversationalVision(c) {
		return a.processImagesAsMultimodal(query)
	}

	// Non-multimodal / OCR-only path: rewrite the query to instruct the
	// model to call analyze_image_content for each pasted image path.
	// processImagesViaOCR runs the actual vision analysis synchronously and
	// inlines the resulting text descriptions so non-vision chat models
	// can answer questions about pasted images.
	paths := extractPastedImagePaths(query)
	if len(paths) == 0 {
		return nil, query, nil
	}

	if c := a.getClient(); c != nil && a.effectiveVisionSupport() {
		// OCR-only model: keep placeholder text AND run the OCR tool inline
		// so the chat model gets text descriptions of the images.
		enhancedQuery, err := a.processImagesViaOCR(query)
		if err != nil {
			a.Logger().Debug("[WARN] OCR fallback failed: %v\n", err)
			return nil, query, nil
		}
		return nil, enhancedQuery, nil
	}

	// Plain non-vision model: hand it the OCR-tool prompt so it can call
	// analyze_image_content to read images itself.
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

// processImagesAsMultimodal extracts pasted-image references from the query,
// reads each file, and returns the image data for multimodal embedding.
func (a *Agent) processImagesAsMultimodal(query string) ([]api.ImageData, string, error) {
	cwd := a.currentWorkspaceRoot()

	var images []api.ImageData
	totalBytes := 0

	// Run the regex once: it serves as both the "any matches?" check and
	// the source of file paths for processing.
	uniqueMatches := pastedImagePlaceholderRe.FindAllStringSubmatchIndex(query, -1)
	if len(uniqueMatches) == 0 {
		return nil, query, nil
	}

	// Build replacement map so we can rewrite the query in a single pass.
	type placeholderInfo struct {
		fullMatch string
		filePath  string
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

	// Rewrite the query once, replacing every occurrence of each placeholder.
	// SP-103-B4: when there are multiple images, label each one with a numeric
	// hint ("[image 1 of 3: foo.png]") so the model can refer to them in
	// follow-up answers ("see image 2"). For single-image queries we keep
	// the simpler "[image: foo.png]" form to avoid implying there's more.
	cleanedQuery := query
	multi := len(placeholders) > 1
	for i, ph := range placeholders {
		fileName := filepath.Base(ph.filePath)
		var replacement string
		if multi {
			replacement = fmt.Sprintf("[image %d of %d: %s]", i+1, len(placeholders), fileName)
		} else {
			replacement = fmt.Sprintf("[image: %s]", fileName)
		}
		cleanedQuery = strings.ReplaceAll(cleanedQuery, ph.fullMatch, replacement)
	}

	// Load image files.
	expectedDir := filepath.Join(cwd, console.PastedImageDirName)
	for _, ph := range placeholders {
		filePath := ph.filePath

		// Resolve all paths to absolute for containment checking.
		if !filepath.IsAbs(filePath) {
			filePath = filepath.Join(cwd, filePath)
		}

		// Defense-in-depth: only read files under the pasted-images directory.
		// This prevents reading arbitrary files if an LLM were to produce text
		// matching the placeholder pattern.
		relToExpected, err := filepath.Rel(expectedDir, filePath)
		if err != nil || strings.HasPrefix(relToExpected, "..") {
			a.Logger().Debug("[WARN] Skipping image %s: not in pasted images directory\n", filePath)
			continue
		}

		imgData, imgSize, err := readImageAsImageData(filePath)
		if err != nil {
			a.Logger().Debug("[WARN] Skipping image %s: %v\n", filePath, err)
			continue
		}

		// Enforce per-image size cap (should already be enforced by console, but be safe).
		if imgSize > console.MaxPastedImageSize {
			a.Logger().Debug("[WARN] Skipping image %s: exceeds per-image size cap (%d > %d)\n",
				filePath, imgSize, console.MaxPastedImageSize)
			continue
		}

		// Enforce total payload cap.
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

	return images, cleanedQuery, nil
}

// processImagesViaOCR uses the existing VisionProcessor to convert images to
// text descriptions and embed them in the query.
func (a *Agent) processImagesViaOCR(query string) (string, error) {
	// Check if vision processing is available
	if !tools.HasVisionCapability() {
		// No vision capability available, return original query
		return query, nil
	}

	// Resolve via unified deterministic chain:
	// active provider vision -> explicit custom fallback -> global list -> local Ollama.
	processor, err := tools.NewVisionProcessorWithProvider(a.debug, a.getClientType())
	if err != nil {
		return query, agenterrors.NewAgent("conversation", "failed to create vision processor", err)
	}

	// Process any images found in the text
	enhancedQuery, analyses, err := processor.ProcessImagesInText(a.InterruptCtx(), query)
	if err != nil {
		return query, agenterrors.NewAgent("conversation", "failed to process images", err)
	}

	// If images were processed, log the enhancement
	if len(analyses) > 0 {
		a.Logger().Debug("[img] Processed %d image(s) and enhanced query with vision analysis\n", len(analyses))
		for _, analysis := range analyses {
			a.Logger().Debug("  - %s: %s\n", analysis.ImagePath, analysis.Description[:min(100, len(analysis.Description))])
		}
	}

	return enhancedQuery, nil
}

// readImageAsImageData reads an image file from disk, validates it, detects
// the MIME type from magic bytes, optimizes the image for vision models, and
// returns base64-encoded ImageData with the byte length of the (possibly
// optimized) image data.
func readImageAsImageData(filePath string) (api.ImageData, int, error) {
	// Check size before reading to avoid loading huge files into memory.
	stat, err := os.Stat(filePath)
	if err != nil {
		return api.ImageData{}, 0, agenterrors.NewAgent("conversation", "failed to stat file", err)
	}
	if stat.Size() > console.MaxPastedImageSize {
		return api.ImageData{}, 0, fmt.Errorf("image too large (%d bytes)", stat.Size())
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return api.ImageData{}, 0, agenterrors.NewAgent("conversation", "failed to read file", err)
	}

	// Validate it is actually an image by checking magic bytes.
	_, mimeType := console.DetectImageMagic(data)
	if mimeType == "" {
		return api.ImageData{}, 0, agenterrors.NewInvalidInputError("unrecognised image format", nil)
	}

	// Optimize to cap dimensions at 4096px and compress for context efficiency.
	optimized, optMime, optErr := tools.OptimizeImageData(filePath, data)
	if optErr == nil && len(optimized) > 0 {
		mimeType = optMime
		data = optimized
	}

	// Pre-resize for vision embedding: cap long edge at 1568px using
	// bilinear resampling for better visual quality. Runs after
	// OptimizeImageData so we don't double-resize small images.
	//
	// NOTE: OptimizeImageData already performs a 4096px nearest-neighbor
	// resize for oversized images. For very large inputs (>4096px on the
	// long edge), the chained steps (nearest-neighbor at 4096px → bilinear
	// at 1568px) may compound artifacts. This is a known limitation; future
	// work could unify both passes into a single bilinear resize.
	resized, resizeErr := resizeImageForVisionEmbed(data)
	if resizeErr == nil && len(resized) > 0 {
		// Check if the image was actually resized (different bytes).
		// If the image was already small enough, resizeImageForVisionEmbed
		// returns the original bytes; we detect this via byte comparison
		// to preserve the original encoding for small images.
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
