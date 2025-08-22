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
	Role    string      `json:"role"`
	Content interface{} `json:"content"` // Can be string or []ContentPart for multimodal
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
		systemPrompt = "You are an assistant that can generate updated code based on provided instructions. You have access to tools for gathering additional information when needed:\n\n" +
			"1.  **Generate Code:** If you have enough information and all context files, provide the complete code. " +
			GetBaseCodeGenSystemMessageWithFormat(usePatchFormat) +
			"\n\n" +
			"2.  **Use Tools When Needed:** If you need more information, you can use the available tools:\n" +
			"    - **read_file**: Read files to understand existing implementations before making changes\n" +
			"    - **workspace_context**:\n" +
			"        - action=search_embeddings: find semantically relevant files via embeddings\n" +
			"        - action=search_keywords: find files containing exact keywords/symbols (grep-like)\n" +
			"    - **run_shell_command**: Execute shell commands\n" +
			"    - **ask_user**: Ask the user for clarification\n\n" +
			"    Tools will be automatically executed and results provided to you.\n\n" +
			"    If the user's instructions refer to a file but its contents have not been provided, you *MUST* read the file using the read_file tool.\n\n" +
			"    If a user has requested that you update a file but it is not included, you *MUST* ask the user for the file name and then read the file using the read_file tool.\n\n" +
			" Do not generate code until you have all the necessary context. Use both embeddings and keyword search to find relevant files, then read_file the top candidates before editing.\n" +
			"After tools provide you with information, generate the code based on all available context.\n"
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
	systemPrompt := "You are an expert at writing git commit messages. " +
		"Based on the provided code changes (diff) and the original user request, " +
		"generate a CONCISE and conventional git commit message. " +
		"The message must follow the standard format: a concise first line (less than 72 characters), a blank line, " +
		"and a succinct description explaining the 'what' and 'why' of the changes. " +
		"Use ONLY the user's original request to explain the 'why'. " +
		"Do not include any personal opinions or additional context. " +
		"Ensure the message is clear, CONCISE, and follows best practices for commit messages. " +
		"Use imperative mood for the first line, e.g., 'Fix bug' instead of 'Fixed bug'. " +
		"Do not include any reference to the commit message generation process, or the user message itself. " +
		"Avoid any references to the 'user request or prompt' in the commit message. " +
		"Do not include any additional information that is not directly related to the code changes. " +
		"Do not include the '```' or '```git' markdown fences in your response. Your output should be only the raw text of the commit message."

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

// BuildChangesForRequirementMessages constructs the messages for the LLM to generate file-specific changes for a high-level requirement.
func BuildChangesForRequirementMessages(requirementInstruction, workspaceContext string, interactive bool) []Message {
	var systemPrompt string

	if interactive {
		systemPrompt = "You are an expert software developer. Your task is to break down a high-level development requirement into a list of specific, file-level changes with strong preference for modifying existing code over creating new code.\n\n" +
			"WORKSPACE CONTEXT PROVIDED:\n" +
			"You are provided with a MINIMAL workspace context containing only:\n" +
			"- File summaries (what each file does)\n" +
			"- Public function exports (available functions/methods)\n" +
			"- Basic project structure\n" +
			"- NO full file contents are provided initially\n\n" +
			"CRITICAL ANALYSIS APPROACH:\n" +
			"1. FIRST: Analyze the minimal context to identify which files likely contain relevant functionality\n" +
			"2. SECOND: Use the read_file tool to load ONLY the specific files you need to understand\n" +
			"3. THIRD: Focus on the exact problem - make changes to solve the specific issue\n" +
			// "4. AVOID: Reading multiple files unless absolutely necessary for your analysis\n\n" +
			"FUNCTION TARGETING:\n" +
			"- Look for functions whose purpose matches the problem domain (e.g., 'GetCodeReview' for code review issues)\n" +
			"- Use the 'exports' information to identify candidate functions without reading full files\n" +
			"MINIMAL CHANGE TARGETING:\n" +
			"- Make the SMALLEST change that solves the specific problem described\n" +
			"- If the issue can be fixed with a 1-2 line change, do NOT suggest additional modifications\n" +
			"- Only propose related changes if they are REQUIRED for your primary fix to function\n" +
			"- Ask yourself: 'Will my primary fix work without these additional changes?' If yes, omit them\n" +
			"- Focus on the exact problem statement rather than general improvements\n\n" +
			"RESPONSE OPTIONS:\n" +
			"1.  **Use Tools to Analyze:** When you need to understand specific implementations:\n" +
			"    - **read_file**: Read specific files to understand current implementations\n" +
			"    - Use this ONLY when the minimal context suggests a file is relevant\n" +
			// "    - Read the minimal number of files needed to understand the problem\n\n" +
			"2.  **Generate Changes:** After you understand the exact change needed, provide the complete list of changes.\n" +
			"    Your response MUST be a JSON object with a single key \"changes\" which is an array of objects, each with \"filepath\" and \"instruction\" keys.\n\n" +
			"    PREFER SINGLE CHANGE (most common and correct approach):\n" +
			"    {\n" +
			"      \"changes\": [\n" +
			"        {\n" +
			"          \"filepath\": " + "`" + "pkg/llm/api.go" + "`" + ",\n" +
			"          \"instruction\": " + "`" + "In the GetCodeReview function, change 'modelName := cfg.OrchestrationModel' to 'modelName := cfg.EditingModel' on line 266." + "`" + "\n" +
			"        }\n" +
			"      ]\n" +
			"    }\n\n" +
			"    Use multi-change responses ONLY when genuinely necessary:\n" +
			"    {\n" +
			"      \"changes\": [\n" +
			"        {\n" +
			"          \"filepath\": " + "`" + "src/main.go" + "`" + ",\n" +
			"          \"instruction\": " + "`" + "Modify the existing calculateTotal function to also handle sum calculations by adding a new parameter 'operation' and extending the logic." + "`" + "\n" +
			"        }\n" +
			"      ]\n" +
			"    }\n\n"
		// "REMEMBER: The minimal context approach means you must actively choose which files to read. Don't read files unless the exports or summary clearly indicate relevance to your specific problem.\n"
	} else {
		systemPrompt = `You are an expert software developer. Your task is to break down a high-level development requirement into a list of specific, file-level changes.
For each change, you must provide the 'filepath' and a detailed 'instruction' for what needs to be done in that file.
If a file needs to be created, specify its full path.
If a file needs to be deleted, specify its full path and an instruction like "Delete this file."
Your response MUST be a JSON object with a single key "changes" which is an array of objects, each with "filepath" and "instruction" keys.
Do not include any other text or explanation outside the JSON.

Example JSON format:
{
  "changes": [
    {
      "filepath": "src/main.go",
      "instruction": "Add a new function 'calculateSum' that takes two integers and returns their sum."
    },
    {
      "filepath": "tests/main_test.go",
      "instruction": "Write a unit test for the 'calculateSum' function in 'src/main.go'."
    },
    {
      "filepath": "docs/api.md",
      "instruction": "Update the API documentation to include details about the new 'calculateSum' function."
    }
  ]
}

Consider the provided workspace context to understand the project structure and existing code.
`
	}

	userPrompt := fmt.Sprintf("High-level requirement: \"%s\"\n\nWorkspace Context:\n%s", requirementInstruction, workspaceContext)

	return []Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}
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
