package prompts

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	promptassets "github.com/alantheprice/ledit/prompts"
)

var (
	DefaultTokenLimit = 100000 // Default token limit for LLM requests
)

// PromptManager handles prompt loading with user overrides and hash tracking
type PromptManager struct {
	userPromptsDir string
	baselinePath   string
	baseline       map[string]string // filename -> baseline hash
}

// PromptInfo contains information about a prompt
type PromptInfo struct {
	Content   string
	Hash      string
	IsUserMod bool
}

// NewPromptManager creates a new prompt manager
func NewPromptManager() *PromptManager {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return &PromptManager{
		userPromptsDir: filepath.Join(home, ".ledit", "prompts"),
		baselinePath:   filepath.Join(home, ".ledit", "prompts", ".baseline_hashes.json"),
		baseline:       map[string]string{},
	}
}

// Initialize creates the prompts directory and copies embedded prompts if needed
func (pm *PromptManager) Initialize() error {
	// Create .ledit/prompts directory
	if err := os.MkdirAll(pm.userPromptsDir, 0755); err != nil {
		return fmt.Errorf("failed to create prompts directory: %w", err)
	}

	// Load baseline if present (ignore error if not present)
	_ = pm.loadBaseline()

	// List embedded prompts
	entries, err := promptassets.FS.ReadDir(".")
	if err != nil {
		return fmt.Errorf("failed to read embedded prompts: %w", err)
	}

	// For each embedded prompt, install/update if appropriate
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()

		embeddedContent, err := promptassets.FS.ReadFile(name)
		if err != nil {
			return fmt.Errorf("failed to read embedded prompt %s: %w", name, err)
		}
		embeddedHash := pm.calculateHash(string(embeddedContent))

		userPath := filepath.Join(pm.userPromptsDir, name)
		userContent, uerr := os.ReadFile(userPath)

		if uerr != nil {
			// No user file: install embedded and set baseline to embedded
			if err := os.WriteFile(userPath, embeddedContent, 0644); err != nil {
				return fmt.Errorf("failed to write prompt %s: %w", userPath, err)
			}
			pm.baseline[name] = embeddedHash
			continue
		}

		userHash := pm.calculateHash(string(userContent))
		prior := pm.baseline[name]
		if prior == "" {
			// Pre-existing user file without baseline; treat as user-modified
			pm.baseline[name] = userHash
			continue
		}

		if userHash == prior {
			// Not modified by user → overwrite with new embedded and advance baseline
			if err := os.WriteFile(userPath, embeddedContent, 0644); err != nil {
				return fmt.Errorf("failed to update prompt %s: %w", userPath, err)
			}
			pm.baseline[name] = embeddedHash
		} else {
			// Modified by user → keep user content; lock baseline to user's current hash
			pm.baseline[name] = userHash
		}
	}

	// Save baseline
	if err := pm.saveBaseline(); err != nil {
		return err
	}

	return nil
}

// Refresh updates user prompt files from embedded assets.
// If force is true, overwrite all user files and reset baseline to embedded.
// If false, only overwrite files that are unmodified relative to baseline; keep user-modified files.
func (pm *PromptManager) Refresh(force bool) error {
	if err := os.MkdirAll(pm.userPromptsDir, 0755); err != nil {
		return fmt.Errorf("failed to create prompts directory: %w", err)
	}

	// Load baseline if present
	_ = pm.loadBaseline()

	entries, err := promptassets.FS.ReadDir(".")
	if err != nil {
		return fmt.Errorf("failed to read embedded prompts: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		embeddedContent, err := promptassets.FS.ReadFile(name)
		if err != nil {
			return fmt.Errorf("failed to read embedded prompt %s: %w", name, err)
		}
		embeddedHash := pm.calculateHash(string(embeddedContent))

		userPath := filepath.Join(pm.userPromptsDir, name)
		userContent, readErr := os.ReadFile(userPath)

		if force {
			if err := os.WriteFile(userPath, embeddedContent, 0644); err != nil {
				return fmt.Errorf("failed to write prompt %s: %w", userPath, err)
			}
			pm.baseline[name] = embeddedHash
			continue
		}

		if readErr != nil {
			// Not present: write embedded and set baseline
			if err := os.WriteFile(userPath, embeddedContent, 0644); err != nil {
				return fmt.Errorf("failed to write prompt %s: %w", userPath, err)
			}
			pm.baseline[name] = embeddedHash
			continue
		}

		userHash := pm.calculateHash(string(userContent))
		prior := pm.baseline[name]

		if prior == "" {
			// Unknown baseline (pre-existing local file). Treat as user-modified; keep user's version
			pm.baseline[name] = userHash
			continue
		}

		if userHash == prior {
			// Unmodified by user → overwrite with new embedded and advance baseline
			if err := os.WriteFile(userPath, embeddedContent, 0644); err != nil {
				return fmt.Errorf("failed to update prompt %s: %w", userPath, err)
			}
			pm.baseline[name] = embeddedHash
		} else {
			// Modified by user → keep user's version; update baseline to user's current hash
			pm.baseline[name] = userHash
		}
	}

	return pm.saveBaseline()
}

// loadBaseline loads the baseline hash map from disk
func (pm *PromptManager) loadBaseline() error {
	data, err := os.ReadFile(pm.baselinePath)
	if err != nil {
		return err
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	pm.baseline = m
	return nil
}

// saveBaseline persists the baseline hash map to disk
func (pm *PromptManager) saveBaseline() error {
	if err := os.MkdirAll(filepath.Dir(pm.baselinePath), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(pm.baseline, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(pm.baselinePath, data, 0644)
}

// LoadPrompt loads a prompt, preferring user version if it exists and hasn't been modified
func (pm *PromptManager) LoadPrompt(filename string) (string, error) {
	// Try to load from user directory first
	userPath := filepath.Join(pm.userPromptsDir, filename)
	if content, err := pm.loadFromUserDirectory(userPath); err == nil {
		return content, nil
	}

	// Fall back to embedded prompt
	return pm.loadFromEmbedded(filename)
}

// loadFromUserDirectory loads a prompt from the user directory if it exists and is valid
func (pm *PromptManager) loadFromUserDirectory(userPath string) (string, error) {
	content, err := os.ReadFile(userPath)
	if err != nil {
		return "", err
	}

	return string(content), nil
}

// loadFromEmbedded loads a prompt from the embedded filesystem
func (pm *PromptManager) loadFromEmbedded(filename string) (string, error) {
	content, err := promptassets.FS.ReadFile(filename)
	if err != nil {
		return "", fmt.Errorf("failed to read embedded prompt %s: %w", filename, err)
	}
	return string(content), nil
}

// GetPromptInfo returns information about a prompt including its hash and modification status
func (pm *PromptManager) GetPromptInfo(filename string) (*PromptInfo, error) {
	userPath := filepath.Join(pm.userPromptsDir, filename)

	// Load user version if it exists
	userContent, userErr := pm.loadFromUserDirectory(userPath)
	embeddedContent, embeddedErr := pm.loadFromEmbedded(filename)

	if embeddedErr != nil {
		return nil, fmt.Errorf("failed to load embedded prompt %s: %w", filename, embeddedErr)
	}

	// Calculate embedded hash
	embeddedHash := pm.calculateHash(embeddedContent)

	if userErr != nil {
		// No user version, return embedded info
		return &PromptInfo{
			Content:   embeddedContent,
			Hash:      embeddedHash,
			IsUserMod: false,
		}, nil
	}

	// Calculate user hash
	userHash := pm.calculateHash(userContent)

	// Check if user has modified the prompt
	isUserMod := userHash != embeddedHash

	return &PromptInfo{
		Content:   userContent,
		Hash:      userHash,
		IsUserMod: isUserMod,
	}, nil
}

// calculateHash calculates SHA256 hash of content
func (pm *PromptManager) calculateHash(content string) string {
	hash := sha256.Sum256([]byte(content))
	return hex.EncodeToString(hash[:])
}

// UpdateUserPrompt updates a user prompt and saves the new hash
func (pm *PromptManager) UpdateUserPrompt(filename, content string) error {
	userPath := filepath.Join(pm.userPromptsDir, filename)

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(userPath), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Write the new content
	if err := os.WriteFile(userPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write prompt file: %w", err)
	}

	return nil
}

// Message represents a single message in a chat-like conversation with the LLM.
type Message struct {
	Role       string      `json:"role"`
	Content    interface{} `json:"content"`                // Can be string or []ContentPart for multimodal
	ToolCallID *string     `json:"tool_call_id,omitempty"` // For tool response messages
}

// ContentPart represents a part of multimodal content (text or image)
type ContentPart struct {
	Type     string    `json:"type"`                // "text" or "image_url"
	Text     string    `json:"text,omitempty"`      // For text content
	ImageURL *ImageURL `json:"image_url,omitempty"` // For image content
}

// ImageURL represents an image URL with optional detail level
type ImageURL struct {
	URL    string `json:"url"`              // base64 encoded image or URL
	Detail string `json:"detail,omitempty"` // "low", "high", or "auto"
}

// CodeReviewResponse represents the structure of the LLM's code review response.
// This struct is used by the llm package to unmarshal the LLM's response.
// It is placed here for visibility to other packages that might need to know its structure,
// but its primary use is within the llm package.
type CodeReviewResponse struct {
	Status       string `json:"status"`
	Feedback     string `json:"feedback"`
	Instructions string `json:"instructions,omitempty"`
	NewPrompt    string `json:"new_prompt,omitempty"`
}

// --- LLM Message Builders ---

// LoadPromptFromFile loads a prompt from file (legacy function, kept for compatibility)
func LoadPromptFromFile(filename string) (string, error) {
	pm := GetPromptManager()
	return pm.LoadPrompt(filename)
}

// Global prompt manager instance
var globalPromptManager *PromptManager

// InitPromptManager initializes the global prompt manager and sets up user prompts directory
func InitPromptManager() error {
	globalPromptManager = NewPromptManager()
	return globalPromptManager.Initialize()
}

// GetPromptManager returns the global prompt manager
func GetPromptManager() *PromptManager {
	if globalPromptManager == nil {
		globalPromptManager = NewPromptManager()
	}
	return globalPromptManager
}

// mustLoadPrompt loads a prompt and exits on failure (prompts are embedded so this should not fail)
func mustLoadPrompt(filename string) string {
	pm := GetPromptManager()
	content, err := pm.LoadPrompt(filename)
	if err != nil {
		os.Exit(1)
	}
	return content
}

// GetBaseCodeGenSystemMessage returns the base system message for code generation
// This function now supports both legacy full-file format and new patch format
func GetBaseCodeGenSystemMessage() string {
	return GetBaseCodeGenSystemMessageWithFormat(false)
}

// GetBaseCodeGenSystemMessageWithFormat returns the base system message with format selection
func GetBaseCodeGenSystemMessageWithFormat(usePatchFormat bool) string {
	if usePatchFormat {
		return GetBaseCodePatchSystemMessage()
	}

	content, err := LoadPromptFromFile("base_code_editing.txt")
	if err != nil {
		// we need to exit, this is a critical error
		os.Exit(1)
	}
	return content
}

// GetQualityAwareCodeGenSystemMessage returns a quality-appropriate system message
func GetQualityAwareCodeGenSystemMessage(qualityLevel int, usePatchFormat bool) string {
	if usePatchFormat {
		return GetBaseCodePatchSystemMessage() // Patch format doesn't have quality variants yet
	}

	var promptFile string
	switch qualityLevel {
	case 2: // QualityProduction
		promptFile = "base_code_editing_quality_enhanced.txt"
	case 1: // QualityEnhanced
		promptFile = "base_code_editing_quality_enhanced.txt"
	default: // QualityStandard
		// Try optimized first, fallback to base
		content, err := LoadPromptFromFile("base_code_editing_optimized.txt")
		if err == nil {
			return content
		}
		promptFile = "base_code_editing.txt"
	}

	content, err := LoadPromptFromFile(promptFile)
	if err != nil {
		// Fallback to base if quality prompt doesn't exist
		content, fallbackErr := LoadPromptFromFile("base_code_editing.txt")
		if fallbackErr != nil {
			os.Exit(1)
		}
		return content
	}
	return content
}

// GetBaseCodePatchSystemMessage returns the system message for patch-based code editing
func GetBaseCodePatchSystemMessage() string {
	content, err := LoadPromptFromFile("base_code_editing_patch.txt")
	if err != nil {
		// we need to exit, this is a critical error
		os.Exit(1)
	}
	return content
}

// StripToolCallsIfPresent removes obvious tool_calls JSON blocks from a model response
// to ensure code-only handling when tools are disabled.
func StripToolCallsIfPresent(response string) string {
	// Quick heuristic: if it contains '"tool_calls"' or a top-level JSON with that key, strip everything between a recognizable block.
	// Keep it conservative to avoid removing valid code; we just drop the tool_calls stanza if present.
	if !strings.Contains(response, "\"tool_calls\"") {
		return response
	}
	// Remove minimal blocks that start with '{' containing tool_calls and end at the matching '}'
	// Fallback: remove lines that look like a tool_calls JSON block
	var out []string
	inBlock := false
	braceDepth := 0
	for _, line := range strings.Split(response, "\n") {
		if !inBlock && strings.Contains(line, "\"tool_calls\"") {
			inBlock = true
			// Count braces
			braceDepth += strings.Count(line, "{") - strings.Count(line, "}")
			continue
		}
		if inBlock {
			braceDepth += strings.Count(line, "{") - strings.Count(line, "}")
			if braceDepth <= 0 {
				inBlock = false
			}
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// BuildCodeMessages constructs the messages for the LLM to generate code.
func BuildCodeMessages(code, instructions, filename string, interactive bool) []Message {
	return BuildCodeMessagesWithFormat(code, instructions, filename, interactive, false)
}

// BuildCodeMessagesWithFormat constructs the messages with format selection (legacy or patch)
func BuildCodeMessagesWithFormat(code, instructions, filename string, interactive bool, usePatchFormat bool) []Message {
	var messages []Message

	systemPrompt := GetBaseCodeGenSystemMessageWithFormat(usePatchFormat) // Use the base message with format selection

	if interactive {
		// Force non-interactive behavior by overriding the system prompt completely
		systemPrompt = mustLoadPrompt("interactive_code_generation.txt")
		systemPrompt = strings.Replace(systemPrompt, "{INSTRUCTIONS}", instructions, 1)
	}

	// Inject dynamic guidance when a specific filename is targeted
	if filename != "" {
		systemPrompt = systemPrompt + "\nSINGLE-FILE TARGETING:\n" +
			"- A specific filename was provided (" + filename + "). Focus your edits primarily on that file.\n" +
			"- Only create or modify other files if absolutely necessary dependencies are required for the requested change to work.\n" +
			"MINIMALITY:\n" +
			"- Make the smallest possible changes to satisfy the request. Do not add unrelated features, refactors, or formatting changes.\n"
	}

	messages = append(messages, Message{Role: "system", Content: systemPrompt})

	if code != "" {
		if usePatchFormat {
			messages = append(messages, Message{Role: "user", Content: fmt.Sprintf("Here is the current content of `%s`:\n\n```\n%s\n```\n\nInstructions: %s", filename, code, instructions)})
		} else {
			messages = append(messages, Message{Role: "user", Content: fmt.Sprintf("Here is the current content of `%s`:\n\n```%s\n%s\n```\n\nInstructions: %s", filename, getLanguageFromFilename(filename), code, instructions)})
		}
	} else {
		messages = append(messages, Message{Role: "user", Content: fmt.Sprintf("Instructions: %s", instructions)})
	}
	return messages
}

// BuildPatchMessages constructs the messages for the LLM to generate patches.
func BuildPatchMessages(code, instructions, filename string, interactive bool) []Message {
	return BuildCodeMessagesWithFormat(code, instructions, filename, interactive, true)
}

// BuildScriptRiskAnalysisMessages constructs the messages for the LLM to analyze script risk.
func BuildScriptRiskAnalysisMessages(scriptContent string) []Message {
	systemPrompt := mustLoadPrompt("shell_risk_system.txt")
	userPrompt := fmt.Sprintf("Analyze the following shell script for security risks:\n\n```bash\n%s\n```", scriptContent)

	return []Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}
}

// BuildCommitMessages constructs the messages for the LLM to generate a commit message.
func BuildCommitMessages(changelog, originalPrompt string) []Message {
	systemPrompt := mustLoadPrompt("commit_message_system.txt")

	userPrompt := fmt.Sprintf(
		"Original user request:\n\"%s\"\n\nCode changes (diff):\n```diff\n%s\n```\n\nPlease generate the git commit message.",
		originalPrompt,
		changelog,
	)

	return []Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}
}

// BuildSearchQueryMessages constructs the messages for the LLM to generate search queries.
func BuildSearchQueryMessages(context string) []Message {
	systemPrompt := mustLoadPrompt("search_query_system.txt")
	userPrompt := fmt.Sprintf("Generate a search query based on the following context:\n\n%s", context)

	return []Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}
}

// BuildSearchResultsQueryMessages constructs messages for the LLM to select relevant URLs from search results.
func BuildSearchResultsQueryMessages(searchResultsContext, originalQuery string) []Message {
	systemPrompt := mustLoadPrompt("search_results_select_system.txt")
	userPrompt := fmt.Sprintf("Original Search Query: \"%s\"\n\nSearch Results:\n%s\n\nWhich result numbers are most relevant?", originalQuery, searchResultsContext)

	return []Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}
}

// RetryPromptWithDiff constructs a prompt for retrying with diff context.
func RetryPromptWithDiff(originalInstruction, filename, validationFailureContext, lastLLMResponse string) string {
	return fmt.Sprintf("The previous attempt to fulfill the instruction '%s' for file '%s' failed. "+
		"Validation failed with the following context:\n%s\n\n"+
		"Here was the last LLM response (diff):\n```diff\n%s\n```\n\n"+
		"Please provide the corrected code, taking into account the validation failure and the previous response.",
		originalInstruction, filename, validationFailureContext, lastLLMResponse)
}

// RetryPromptWithoutDiff constructs a prompt for retrying without diff context.
func RetryPromptWithoutDiff(originalInstruction, filename, validationFailureContext string) string {
	return fmt.Sprintf("The previous attempt to fulfill the instruction '%s' for file '%s' failed. "+
		"Validation failed with the following context:\n%s\n\n"+
		"Please provide the corrected code, taking into account the validation failure.",
		originalInstruction, filename, validationFailureContext)
}

// BuildProjectGoalsMessages constructs messages for the LLM to generate project goals.
func BuildProjectGoalsMessages(workspaceSummary string) []Message {
	systemPrompt := mustLoadPrompt("project_goals_system.txt")
	userPrompt := fmt.Sprintf("Based on the following workspace summary, generate the project goals:\n\n%s", workspaceSummary)

	return []Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}
}

// BuildCodeReviewMessages constructs the messages for the LLM to review code changes.
func BuildCodeReviewMessages(combinedDiff, originalPrompt, processedInstructions, fullFileContext string) []Message {
	systemPrompt := mustLoadPrompt("code_review_system.txt")

	userPrompt := fmt.Sprintf(
		"Original user prompt:\n\"%s\"\n\nCode changes (diff):\n```diff\n%s\n```\n\nFull file context:\n```go\n%s\n```\n\nPlease review these changes and provide your assessment. If you need to make changes, provide a patch_resolution field with the complete updated file content.",
		originalPrompt,
		combinedDiff,
		fullFileContext,
	)

	return []Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}
}

// --- User Interaction Prompts ---

// getLanguageFromFilename infers the programming language from the file extension.
func getLanguageFromFilename(filename string) string {
	if strings.HasSuffix(filename, ".go") {
		return "go"
	}
	if strings.HasSuffix(filename, ".py") {
		return "python"
	}
	if strings.HasSuffix(filename, ".js") || strings.HasSuffix(filename, ".ts") {
		return "javascript"
	}
	if strings.HasSuffix(filename, ".java") {
		return "java"
	}
	if strings.HasSuffix(filename, ".c") || strings.HasSuffix(filename, ".cpp") || strings.HasSuffix(filename, ".h") {
		return "c"
	}
	if strings.HasSuffix(filename, ".sh") {
		return "bash"
	}
	if strings.HasSuffix(filename, ".md") {
		return "markdown"
	}
	if strings.HasSuffix(filename, ".json") {
		return "json"
	}
	if strings.HasSuffix(filename, ".xml") {
		return "xml"
	}
	if strings.HasSuffix(filename, ".html") {
		return "html"
	}
	if strings.HasSuffix(filename, ".css") {
		return "css"
	}
	if strings.HasSuffix(filename, ".yaml") || strings.HasSuffix(filename, ".yml") {
		return "yaml"
	}
	if strings.HasSuffix(filename, ".sql") {
		return "sql"
	}
	if strings.HasSuffix(filename, ".rb") {
		return "ruby"
	}
	if strings.HasSuffix(filename, ".php") {
		return "php"
	}
	if strings.HasSuffix(filename, ".rs") {
		return "rust"
	}
	if strings.HasSuffix(filename, ".swift") {
		return "swift"
	}
	if strings.HasSuffix(filename, ".kt") {
		return "kotlin"
	}
	if strings.HasSuffix(filename, ".cs") {
		return "csharp"
	}
	return "" // Unknown language
}

// BuildProjectInsightsMessages constructs messages for the LLM to infer high-level insights.
func BuildProjectInsightsMessages(workspaceOverview string) []Message {
	systemPrompt := mustLoadPrompt("project_insights_system.txt")
	userPrompt := fmt.Sprintf("Based on the following workspace overview, infer the project insights as a compact JSON object.\n\n%s", workspaceOverview)

	return []Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}
}
