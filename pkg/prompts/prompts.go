package prompts

import (
	"fmt"
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

// --- LLM Message Builders ---

func getBaseCodeMessageSystemMessage() string {
	return "Provide the complete code. Each file must be in a separate fenced code block, starting with with the language, a space, then `# <filename>` ON THE SAME LINE above the code., and ending with '```END'.\n" +
		"For Example: '```python # myfile.py\n<replace-with-file-contents>\n```END', or '```html # myfile.html\n<replace-with-file-contents>\n```END', or '```javascript # myfile.js\n<replace-with-file-contents>\n```END'\n\n" +
		"The syntax of the code blocks must exactly match these instructions and the code must be complete. " +
		"ONLY make the changes that are necessary to satisfy the instructions. " +
		"Do not include any additional text, explanations, or comments outside the code blocks. " +
		"Update all files that are necessary to fulfill the requirements and any code that is affected by the changes. " +
		"Do not include any file paths in the code blocks. " +
		"Do not include any other text or explanations outside the code blocks. " +
		"Ensure that the code is syntactically correct and follows best practices for the specified language. "
}

// GetBaseCodeGenSystemMessage returns the core system prompt for code generation.
func GetBaseCodeGenSystemMessage() string {
	return "You are an assistant that can generate updated code based on provided instructions. " +
		getBaseCodeMessageSystemMessage()
}

// BuildCodeMessages constructs the messages for the LLM to generate code.
func BuildCodeMessages(code, instructions, filename string, interactive bool) []Message {
	var messages []Message

	systemPrompt := GetBaseCodeGenSystemMessage() // Use the base message

	if interactive {
		systemPrompt = "You are an assistant that can generate updated code based on provided instructions. You have two response options:\n\n" +
			"1.  **Generate Code:** If you have enough information and all context files, provide the complete code. " +
			getBaseCodeMessageSystemMessage() +
			"\n\n" +
			"2.  **Request Context:** *do not make guesses* If you need more information, respond *only* with a JSON array of context requests with no other text. The required format:\n" +
			"    `{\"context_requests\":[{ \"type\": \"TYPE\", \"query\": \"QUERY\" }]}`\n" +
			"    -   `type`: Can be `search` (web search), `user_prompt` (ask the user a question), `file` (request file content, needs to be a filename, otherwise ask the user), or `shell` (request a shell command execution).\n" +
			"    -   `query`: The search term, question, file path, or command.\n\n" +
			"    If the user's instructions refer to a file but its contents have not been provided, you *MUST* request the file's contents using the `file` type.\n\n" +
			"    If a user has requested that you update a file but it is not included, you *MUST* ask the user for the file name and then request the file contents using the `file` type.\n\n" +
			" Do not generate code until you have all the necessary context. If you do not have enough information, ask for it using the context request format.\n" +
			"After your context request is fulfilled, you will be prompted again to generate the code. Do not continue asking for context; generate the code as soon as you have enough information.\n"
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

// BuildOrchestrationPlanMessages constructs the messages for the LLM to generate a high-level orchestration plan.
func BuildOrchestrationPlanMessages(overallTask, workspaceContext string) []Message {
	systemPrompt := `You are an expert software developer. Your task is to break down a complex development task into a list of high-level, actionable requirements.
Each requirement should describe a distinct, logical step towards completing the overall task.
Your response MUST be a JSON object with a single key "requirements" which is an array of objects, each with an "instruction" key.
Do not include any filepaths in these high-level instructions. File-specific changes will be determined in a later step.
Do not include any other text or explanation outside the JSON.

Example JSON format:
{
  "requirements": [
    {
      "instruction": "Implement user authentication, including signup and login."
    },
    {
      "instruction": "Develop a new API endpoint for managing user profiles."
    },
    {
      "instruction": "Integrate a payment gateway for subscription management."
    }
  ]
}

Consider the provided workspace context to understand the project structure and existing code.
`
	userPrompt := fmt.Sprintf("Overall task: \"%s\"\n\nWorkspace Context:\n%s", overallTask, workspaceContext)

	return []Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}
}

// BuildChangesForRequirementMessages constructs the messages for the LLM to generate file-specific changes for a high-level requirement.
func BuildChangesForRequirementMessages(requirementInstruction, workspaceContext string, interactive bool) []Message {
	var systemPrompt string

	if interactive {
		systemPrompt = "You are an expert software developer. Your task is to break down a high-level development requirement into a list of specific, file-level changes. You have two response options:\n\n" +
			"1.  **Generate Changes:** If you have enough information and all context files, provide the complete list of changes. For each change, you must provide the 'filepath' and a detailed 'instruction' for what needs to be done in that file. If a file needs to be created, specify its full path. If a file needs to be deleted, specify its full path and an instruction like \"Delete this file.\"\n" +
			"    Your response MUST be a JSON object with a single key \"changes\" which is an array of objects, each with \"filepath\" and \"instruction\" keys. Do not include any other text or explanation outside the JSON.\n\n" +
			"    Example JSON format:\n" +
			"    {\n" +
			"      \"changes\": [\n" +
			"        {\n" +
			"          \"filepath\": \"src/main.go\",\n" +
			"          \"instruction\": \"Add a new function 'calculateSum' that takes two integers and returns their sum.\"\n" +
			"        },\n" +
			"        {\n" +
			"          \"filepath\": \"tests/main_test.go\",\n" +
			"          \"instruction\": \"Write a unit test for the 'calculateSum' function in 'src/main.go'.\"\n" +
			"        },\n" +
			"        {\n" +
			"          \"filepath\": \"docs/api.md\",\n" +
			"          \"instruction\": \"Update the API documentation to include details about the new 'calculateSum' function.\"\n" +
			"        }\n" +
			"      ]\n" +
			"    }\n\n" +
			"2.  **Request Context:** *do not make guesses* If you need more information, respond *only* with a JSON array of context requests with no other text. The required format:\n" +
			"    `{\"context_requests\":[{ \"type\": \"TYPE\", \"query\": \"QUERY\" }]}`\n" +
			"    -   `type`: Can be `search` (web search), `user_prompt` (ask the user a question), `file` (request file content, needs to be a filename, otherwise ask the user), or `shell` (request a shell command execution).\n" +
			"    -   `query`: The search term, question, file path, or command.\n\n" +
			"    If the user's instructions refer to a file but its contents have not been provided, you *MUST* request the file's contents using the `file` type.\n\n" +
			"    If a user has requested that you update a file but it is not included, you *MUST* ask the user for the file name and then request the file contents using the `file` type.\n\n" +
			"After your context request is fulfilled, you will be prompted again to generate the changes. Do not continue asking for context; generate the changes as soon as you have enough information.\n" +
			"Do not generate changes until you have all the necessary context. If you do not have enough information, ask for it using the context request format.\n" +
			"Consider the provided workspace context to understand the project structure and existing code.\n"
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

// --- User Interaction Prompts ---

// EnterLLMProvider prompts the user to enter their preferred LLM provider.
func EnterLLMProvider(defaultProvider string) string {
	return fmt.Sprintf("Enter your preferred LLM provider (e.g., anthropic, openai, gemini, ollama) (default: %s): ", defaultProvider)
}

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
