package agent

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	tools "github.com/alantheprice/ledit/pkg/agent_tools"
	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/alantheprice/ledit/pkg/console"
)

// ProcessQuery handles the main conversation loop with the LLM
func (a *Agent) ProcessQuery(userQuery string) (string, error) {
	handler := NewConversationHandler(a)
	return handler.ProcessQuery(userQuery)
}

// ProcessQueryWithContinuity processes a query with continuity from previous actions
func (a *Agent) ProcessQueryWithContinuity(userQuery string) (string, error) {
	// Ensure changes are committed even if there are unexpected errors or early termination
	defer func() {
		// Only commit if we have changes and they haven't been committed yet
		if a.IsChangeTrackingEnabled() && a.GetChangeCount() > 0 {
			a.debugLog("DEFER: Attempting to commit %d tracked changes\n", a.GetChangeCount())
			// Check if changes are already committed by trying to commit (it's safe due to committed flag)
			if commitErr := a.CommitChanges("Session cleanup - ensuring changes are not lost"); commitErr != nil {
				a.debugLog("Warning: Failed to commit tracked changes during cleanup: %v\n", commitErr)
			} else {
				a.debugLog("DEFER: Successfully committed tracked changes during cleanup\n")
			}
		} else {
			a.debugLog("DEFER: No changes to commit (enabled: %v, count: %d)\n", a.IsChangeTrackingEnabled(), a.GetChangeCount())
		}

		// Auto-save memory state after every successful turn
		a.autoSaveState()
		a.debugLog("DEFER: Auto-saved memory state\n")
	}()

	// Load previous state if available
	if a.previousSummary != "" {
		continuityPrompt := fmt.Sprintf(`
CONTEXT FROM PREVIOUS SESSION:
%s

CURRENT TASK:
%s

Note: The user cannot see the previous session's responses, so please provide a complete answer without referencing "previous responses" or "as mentioned before". If this task relates to the previous session, build upon that work but present your response as if it's the first time addressing this topic.`,
			a.previousSummary, userQuery)

		return a.ProcessQuery(continuityPrompt)
	}

	// No previous state, process normally
	return a.ProcessQuery(userQuery)
}

// getOptimizedToolDefinitions returns tool definitions optimized based on conversation context
func (a *Agent) getOptimizedToolDefinitions(messages []api.Message) []api.Tool {
	// Start with standard tools
	tools := api.GetToolDefinitions()

	// Filter out run_subagent and run_parallel_subagents when:
	// 1. Running as a subagent (prevents nested subagents)
	// 2. User explicitly disabled subagents via --no-subagents flag or LEDIT_NO_SUBAGENTS env
	noSubagents := os.Getenv("LEDIT_SUBAGENT") == "1" || os.Getenv("LEDIT_NO_SUBAGENTS") == "1"
	if noSubagents {
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
	if customProvider, ok := a.getCurrentCustomProvider(); ok {
		if len(customProvider.ToolCalls) > 0 {
			allowedToolSet := makeAllowedToolSet(customProvider.ToolCalls)
			tools = filterToolsByName(tools, allowedToolSet)
		}
	}

	// Apply active persona tool filter (used for direct /persona and subagent persona runs).
	if personaAllowlist := a.getActivePersonaToolAllowlist(); len(personaAllowlist) > 0 {
		tools = filterToolsByName(tools, makeAllowedToolSet(personaAllowlist))
	}

	if a.shouldUseDirectMultimodalImageReasoning(messages) {
		filtered := make([]api.Tool, 0, len(tools))
		for _, tool := range tools {
			switch tool.Function.Name {
			case "analyze_image_content", "analyze_ui_screenshot":
				continue
			default:
				filtered = append(filtered, tool)
			}
		}
		tools = filtered
	}

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

	provider, exists := config.CustomProviders[string(a.clientType)]
	if !exists {
		return nil, false
	}
	return &provider, true
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
	if a == nil || a.client == nil || !a.client.SupportsVision() {
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
	a.messages = []api.Message{}
	a.clearTurnCheckpoints()
	a.currentIteration = 0
	a.previousSummary = ""

	a.debugLog("[clean] Conversation history cleared\n")
}

// SetConversationOptimization enables or disables conversation optimization
func (a *Agent) SetConversationOptimization(enabled bool) {
	if a.optimizer != nil {
		a.optimizer.SetEnabled(enabled)
		if enabled {
			a.debugLog("[*] Conversation optimization enabled\n")
		} else {
			a.debugLog("[tool] Conversation optimization disabled\n")
		}
	}
}

// GetOptimizationStats returns optimization statistics
func (a *Agent) GetOptimizationStats() map[string]interface{} {
	if a.optimizer != nil {
		return a.optimizer.GetOptimizationStats()
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

// processImagesInQuery detects and processes images in user queries.
// If the primary model supports vision it returns the image data as multimodal
// content so the model can see the images directly.  Otherwise it falls back
// to the existing OCR pipeline which converts images to text descriptions.
func (a *Agent) processImagesInQuery(query string) ([]api.ImageData, string, error) {
	// Skip if no client is available
	if a.client == nil {
		return nil, query, nil
	}

	// Multimodal path: if the active client reports vision capability, send
	// pasted images as direct image payloads and strip placeholder text.
	if a.client.SupportsVision() {
		return a.processImagesAsMultimodal(query)
	}

	// Non-multimodal path: keep the original text placeholder in the prompt so
	// the model can choose OCR/image-analysis tools.
	return nil, query, nil
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
	cwd, err := os.Getwd()
	if err != nil {
		a.debugLog("[WARN] Failed to get working directory for image resolution: %v\n", err)
		return nil, query, nil
	}

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
	cleanedQuery := query
	for _, ph := range placeholders {
		fileName := filepath.Base(ph.filePath)
		replacement := fmt.Sprintf("[image: %s]", fileName)
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
			a.debugLog("[WARN] Skipping image %s: not in pasted images directory\n", filePath)
			continue
		}

		imgData, imgSize, err := readImageAsImageData(filePath)
		if err != nil {
			a.debugLog("[WARN] Skipping image %s: %v\n", filePath, err)
			continue
		}

		// Enforce per-image size cap (should already be enforced by console, but be safe).
		if imgSize > console.MaxPastedImageSize {
			a.debugLog("[WARN] Skipping image %s: exceeds per-image size cap (%d > %d)\n",
				filePath, imgSize, console.MaxPastedImageSize)
			continue
		}

		// Enforce total payload cap.
		if totalBytes+imgSize > maxTotalImagePayloadBytes {
			a.debugLog("[WARN] Skipping image %s: total payload would exceed cap (%d bytes)\n",
				filePath, maxTotalImagePayloadBytes)
			continue
		}

		totalBytes += imgSize
		images = append(images, imgData)
	}

	if len(images) > 0 {
		a.debugLog("[img] Attached %d image(s) as multimodal content (%d bytes)\n", len(images), totalBytes)
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
	processor, err := tools.NewVisionProcessorWithProvider(a.debug, a.clientType)
	if err != nil {
		return query, fmt.Errorf("failed to create vision processor: %w", err)
	}

	// Process any images found in the text
	enhancedQuery, analyses, err := processor.ProcessImagesInText(query)
	if err != nil {
		return query, fmt.Errorf("failed to process images: %w", err)
	}

	// If images were processed, log the enhancement
	if len(analyses) > 0 {
		a.debugLog("[img] Processed %d image(s) and enhanced query with vision analysis\n", len(analyses))
		for _, analysis := range analyses {
			a.debugLog("  - %s: %s\n", analysis.ImagePath, analysis.Description[:min(100, len(analysis.Description))])
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
		return api.ImageData{}, 0, fmt.Errorf("failed to stat file: %w", err)
	}
	if stat.Size() > console.MaxPastedImageSize {
		return api.ImageData{}, 0, fmt.Errorf("image too large (%d bytes)", stat.Size())
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return api.ImageData{}, 0, fmt.Errorf("failed to read file: %w", err)
	}

	// Validate it is actually an image by checking magic bytes.
	_, mimeType := console.DetectImageMagic(data)
	if mimeType == "" {
		return api.ImageData{}, 0, fmt.Errorf("unrecognised image format")
	}

	// Optimize to cap dimensions at 4096px and compress for context efficiency.
	optimized, optMime, optErr := tools.OptimizeImageData(filePath, data)
	if optErr == nil && len(optimized) > 0 {
		mimeType = optMime
		data = optimized
	}

	encoded := base64.StdEncoding.EncodeToString(data)
	return api.ImageData{
		Base64: encoded,
		Type:   mimeType,
	}, len(data), nil
}
