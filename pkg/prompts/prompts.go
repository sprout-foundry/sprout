package prompts

import (
	"fmt"
	"os"
	"strings" // Added for getLanguageFromFilename
)

var (
	DefaultTokenLimit = 42096 // Default token limit for LLM requests
)

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

func LoadPromptFromFile(filename string) (string, error) {
	content, err := os.ReadFile(filename)
	if err != nil {
		return "", fmt.Errorf("failed to read prompt file %s: %w", filename, err)
	}
	return string(content), nil
}

func GetBaseCodeGenSystemMessage() string {
	content, err := LoadPromptFromFile("prompts/base_code_editing.txt")
	if err != nil {
		// Fallback to hardcoded string if file reading fails
		return "You MUST provide the COMPLETE and FULL file contents for each file you modify. Each file must be in a separate fenced code block, starting with the language, a space, then `# <filename>` ON THE SAME LINE above the code, and ending with '```END'.\n" +
			"For Example: '```python # myfile.py\n<replace-with-file-contents>\n```END', or '```html # myfile.html\n<replace-with-file-contents>\n```END', or '```javascript # myfile.js\n<replace-with-file-contents>\n```END'\n\n" +
			"CRITICAL REQUIREMENTS:\n" +
			"- You MUST include the ENTIRE file content from beginning to end\n" +
			"- NEVER truncate or abbreviate any part of the file\n" +
			"- Include ALL imports, functions, classes, and code - both modified AND unmodified sections\n" +
			"- The code blocks must contain the complete, full, working file that can be saved and executed\n" +
			"- Make only the specific changes requested, but include ALL surrounding code\n" +
			"- Do NOT add new features, refactor unrelated code, or reformat existing code unless explicitly requested\n" +
			"- Strive for the most minimal and targeted changes necessary to fulfill the request\n" +
			"- Do not regenerate or reflow unchanged sections of code unless absolutely necessary for correctness\n\n" +
			"CODE MODIFICATION BEST PRACTICES:\n" +
			"- PREFER modifying existing functions/methods over creating new ones when possible\n" +
			"- Before adding new functionality, analyze existing code to identify modification opportunities\n" +
			"- Follow DRY principles: Look for existing functions that perform similar tasks and extend them rather than duplicate\n" +
			"- When modifying existing functions, preserve the original function signature unless specifically requested to change it\n" +
			"- Only create new functions when the requested functionality is genuinely distinct from existing code\n\n" +
			"The syntax of the code blocks must exactly match these instructions.\n" +
			"Do not include any additional text, explanations, or comments outside the code blocks.\n" +
			"Update only the files that are necessary to fulfill the requirements.\n" +
			"If a specific filename is provided, focus your edits primarily on that file. Only create or modify other files if it is an absolute, unavoidable dependency for the requested change to work.\n" +
			"The filename must only appear in the header of the code block (e.g., ````python # myfile.py`). Do not include file paths within the code content itself.\n" +
			"Do not include any other text or explanations outside the code blocks.\n" +
			"Ensure that the code is syntactically correct and follows best practices for the specified language.\n"
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
	var messages []Message

	systemPrompt := GetBaseCodeGenSystemMessage() // Use the base message

	if interactive {
		systemPrompt = "You are an assistant that can generate updated code based on provided instructions. You have access to tools for gathering additional information when needed:\n\n" +
			"1.  **Generate Code:** If you have enough information and all context files, provide the complete code. " +
			GetBaseCodeGenSystemMessage() +
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
		messages = append(messages, Message{Role: "user", Content: fmt.Sprintf("Here is the current content of `%s`:\n\n```%s\n%s\n```\n\nInstructions: %s", filename, getLanguageFromFilename(filename), code, instructions)})
	} else {
		messages = append(messages, Message{Role: "user", Content: fmt.Sprintf("Instructions: %s", instructions)})
	}
	return messages
}

// BuildScriptRiskAnalysisMessages constructs the messages for the LLM to analyze script risk.
func BuildScriptRiskAnalysisMessages(scriptContent string) []Message {
	systemPrompt := `You are an expert in shell script security analysis. Your task is to review a provided shell script and determine if it poses any significant security risks (e.g., deleting critical files, installing untrusted software, exposing sensitive information).
Respond with a concise analysis, stating whether the script is "not risky" or detailing any identified risks.
Do not execute the script. Focus solely on static analysis.
`
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
	systemPrompt := `You are an expert at formulating precise web search queries.
Your task is to generate a single, highly effective search query based on the provided context, which includes an error message and relevant code/instruction.
The query should be designed to find solutions or relevant information to resolve the error or understand the context better.
Respond with ONLY the search query string. Do not include any other text, explanations, or conversational filler.
Example: "golang http server connection refused"
`
	userPrompt := fmt.Sprintf("Generate a search query based on the following context:\n\n%s", context)

	return []Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}
}

// BuildSearchResultsQueryMessages constructs messages for the LLM to select relevant URLs from search results.
func BuildSearchResultsQueryMessages(searchResultsContext, originalQuery string) []Message {
	systemPrompt := `You are an expert at identifying the most relevant URLs from a list of search results based on an original search query.
Your task is to review the provided search query and the list of search results (including URL, title, and description).
From the search results, identify the 1-3 most relevant URLs that are most likely to contain the information needed to answer the original query.
Respond with ONLY a comma-separated list of the result numbers (e.g., "1,3,5").
If no results are relevant, respond with "none".
Do not include any other text, explanations, or conversational filler.
`
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
	systemPrompt := `You are an expert at defining clear and concise project goals.
Your task is to analyze the provided workspace summary and infer the overall goals, key features, target audience, and technical vision for the project.
Your response MUST be a JSON object with the following keys:
- "overall_goal": A concise statement of the project's main objective.
- "key_features": A paragraph describing the most important functionalities or capabilities.
- "target_audience": Who the project is intended for.
- "technical_vision": The high-level technical approach or philosophy.
Do not include any other text or explanation outside the JSON.

Example JSON format:
{
  "overall_goal": "Develop a secure and scalable e-commerce platform.",
  "key_features": "Product catalog, Shopping cart, Payment processing, User authentication",
  "target_audience": "Small to medium-sized businesses selling online.",
  "technical_vision": "Microservices architecture with cloud-native deployment."
}

`
	userPrompt := fmt.Sprintf("Based on the following workspace summary, generate the project goals:\n\n%s", workspaceSummary)

	return []Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}
}

// BuildCodeReviewMessages constructs the messages for the LLM to review code changes.
func BuildCodeReviewMessages(combinedDiff, originalPrompt, processedInstructions string) []Message {
	systemPrompt := `You are an expert code reviewer. Your task is to review a combined diff representing the ENTIRE changeset across one or more files against the original user prompt.
Analyze the changes HOLISTICALLY across files for correctness, security, cross-file consistency, and adherence to best practices.

CRITICAL: Consider the whole changeset together. Do NOT request a change that already exists in any file within the provided diff. If a requirement is satisfied in another file, acknowledge it and avoid redundant recommendations.

OPTIONAL PER-FILE RECOMMENDATIONS: You may include file-specific suggestions, but the overall status MUST reflect the entire changeset.

Your response MUST be a JSON object with the following keys:
- "status": Either "approved", "needs_revision", or "rejected".
- "feedback": A concise explanation of your review decision.
- "instructions": (Only required if status is "needs_revision" or "rejected") Detailed instructions for what needs to be fixed or improved (these can reference multiple files).
- "new_prompt": (Only required if status is "rejected") A more detailed prompt that addresses the issues found.
- "file_recommendations": (Optional) An array of objects with keys {"filepath", "recommendation"} for file-scoped pointers.

Example JSON format for approval:
{
  "status": "approved",
  "feedback": "The changes correctly implement the requested feature and follow best practices."
}

Example JSON format for revision:
{
  "status": "needs_revision",
  "feedback": "The implementation has a potential security vulnerability in the authentication logic.",
  "instructions": "Review the authentication function in src/auth.go and ensure proper input validation is implemented."
}

Example JSON format for rejection:
{
  "status": "rejected",
  "feedback": "The changes do not address the core requirements and introduce several bugs.",
  "new_prompt": "Please implement a proper user authentication system with secure password handling and session management."
}
`
	var userPromptBuilder strings.Builder
	// Pull the workspace context out of the processed instructions if available
	// the start of the workspace context is marked by: --- Full content from workspace ---
	// the end of the workspace context is marked by: --- End of full content from workspace ---
	workspaceContext := extractWorkspaceContext(processedInstructions)

	if workspaceContext != "" {
		userPromptBuilder.WriteString("--- Workspace Context ---\n")
		userPromptBuilder.WriteString(workspaceContext)
		userPromptBuilder.WriteString("\n--- End Workspace Context ---\n\n")
	}

	userPromptBuilder.WriteString(fmt.Sprintf(
		"Original user prompt:\n\"%s\"\n\nCode changes (diff):\n```diff\n%s\n```\n\nPlease review these changes and provide your assessment.",
		originalPrompt,
		combinedDiff,
	))

	return []Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPromptBuilder.String()},
	}
}

func extractWorkspaceContext(processedInstructions string) string {
	// Pull the workspace context out of the processed instructions if available
	// the start of the workspace context is marked by: --- Full content from workspace ---
	// the end of the workspace context is marked by: --- End of full content from workspace ---
	startMarker := "--- Full content from workspace ---"
	endMarker := "--- End of full content from workspace ---"
	startIndex := strings.Index(processedInstructions, startMarker)
	endIndex := strings.Index(processedInstructions, endMarker)

	if startIndex != -1 && endIndex != -1 && startIndex < endIndex {
		return processedInstructions[startIndex+len(startMarker) : endIndex]
	}
	return ""
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
