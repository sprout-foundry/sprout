package prompts

import (
	"fmt"
)

var (
	// DefaultTokenLimit is the default token limit for API calls
	DefaultTokenLimit = 30000
)

// --- Message Structs ---

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func GetCodeGenMessages() []Message {
	return []Message{
		{
			Role: "system",
			Content: "You are an assistant that can generate updated code based on provided instructions. " +
				"Provide the complete code. Each file must be in a separate fenced code block, starting with with the language, a space, then `# <filename>` ON THE SAME LINE above the code., and ending with '```END'.\n" +
				"For Example: '```python # myfile.py\n<replace-with-file-contents>\n```END', or '```html # myfile.html\n<replace-with-file-contents>\n```END', or '```javascript # myfile.js\n<replace-with-file-contents>\n```END'\n\n" +
				"The syntax of the code blocks must exactly match these instructions and the code must be complete. " +
				"ONLY make the changes that are necessary to satisfy the instructions.\n",
		},
	}
}

func GetCodeOrRequestMessages() []Message {
	return []Message{
		{
			Role: "system",
			Content: "You are an assistant that can generate updated code based on provided instructions. You have two response options:\n\n" +
				"1.  **Generate Code:** If you have enough information and all context files, provide the complete code. Each file must be in a separate fenced code block, starting with with the language, a space, then `# <filename>` ON THE SAME LINE above the code., and ending with '```END'.\n" +
				"    For Example: '```python # myfile.py\n<replace-with-file-contents>\n```END', or '```html # myfile.html\n<replace-with-file-contents>\n```END', or '```javascript # myfile.js\n<replace-with-file-contents>\n```END'\n\n" +
				"    If you are generating code, the syntax of the code blocks must exactly match these instructions and the code must be complete. " +
				"    If you are generating code, ONLY make the changes that are necessary to satisfy the instructions.\n\n" +
				"2.  **Request Context:** *do not make guesses* If you need more information, respond *only* with a JSON array of context requests with no other text. The required format is:\n" +
				"    `{\"context_requests\":[{ \"type\": \"TYPE\", \"query\": \"QUERY\" }]}`\n" +
				"    -   `type`: Can be `search` (web search), `user_prompt` (ask the user a question), `file` (request file content, needs to be a filename, otherwise ask the user), or `shell` (request a shell command execution).\n" +
				"    -   `query`: The search term, question, file path, or command.\n\n" +
				"    If the user's instructions refer to a file but its contents have not been provided, you *MUST* request the file's contents using the `file` type.\n\n" +
				"    If a user has requested that you update a file but it is not included, you *MUST* ask the user for the file name and then request the file contents using the `file` type.\n\n" +
				"After your context request is fulfilled, you will be prompted again to generate the code. Do not continue asking for context; generate the code as soon as you have enough information.\n" +
				"Do not generate code until you have all the necessary context. If you do not have enough information, ask for it using the context request format.\n",
		},
	}
}

func BuildCodeMessages(code, instructions, filename string, canAddContext bool) []Message {
	messages := GetCodeGenMessages()
	if canAddContext {
		messages[0] = GetCodeOrRequestMessages()[0]
	}
	if code != "" && filename != "" {
		messages = append(messages, Message{
			Role:    "user",
			Content: fmt.Sprintf("Here is the existing code in `%s`:\n\n%s\n\nPlease update it to satisfy these instructions: %s", filename, code, instructions),
		})
	} else {
		messages = append(messages, Message{
			Role:    "user",
			Content: fmt.Sprintf("Based on these instructions: %s, suggest filenames and the full contents of each new file.", instructions),
		})
	}
	return messages
}

func BuildOrchestrationMessages(prompt, workspaceContext string) []Message {
	return []Message{
		{
			Role: "system",
			Content: "You are a senior software engineer planning a feature implementation. " +
				"Based on the user's request and the provided workspace context, generate a JSON object. " +
				"This object should contain a single key 'requirements' which is an array of change objects. " +
				"Each change object must have a 'filepath' (the path to the file to be modified or created), " +
				"an 'instruction' (a detailed description of the change for a mid-level developer that is not too prescriptive, but provides enough context), " +
				"and a 'status' (which should be initialized to 'pending'). " +
				"For requirements that can be met by running a command (e.g., installing dependencies, initializing a project), use 'setup.sh' as the filepath and describe the command in the instruction. " +
				"The output must be only the raw JSON, without any surrounding text or code fences.",
		},
		{
			Role:    "user",
			Content: fmt.Sprintf("User request: %s\n\nWorkspace context:\n%s", prompt, workspaceContext),
		},
	}
}

func BuildCommitMessages(changelog, originalPrompt string) []Message {
	systemPrompt := "You are an expert at writing git commit messages. " +
		"Based on the provided code changes (diff) and the original user request, " +

		"generate a CONCISE and conventional git commit message. " +
		"The message must follow the standard format: a subject line (less than 72 characters), a blank line, " +
		"and a more detailed body explaining the 'what' and 'why' of the changes. " +
		"Use ONLY the user's original request to explain the 'why'. " +
		"Do not include any personal opinions or additional context. " +
		"Ensure the message is clear, CONCISE, and follows best practices for commit messages. " +
		"Use imperative mood for the subject line, e.g., 'Fix bug' instead of 'Fixed bug'. " +
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
		{
			Role:    "system",
			Content: systemPrompt,
		},
		{
			Role:    "user",
			Content: userPrompt,
		},
	}
}

func BuildSearchResultsQueryMessages(results, query string) []Message {

	prompt := fmt.Sprintf(`
Based on the search query and the following search results, identify the 1-3 most relevant URLs (max 3).
You should select URLs that are most likely to contain the answer to the query.
List the numbers of the URLs that are most likely to contain the answer to the query.
If none of the results seem relevant, output "none".
Only output a comma-separated list of numbers, e.g., "1, 3, 4".

%s
`, results)

	return []Message{
		{Role: "system", Content: "You are an expert at analyzing search results and picking the most relevant links."},
		{Role: "user", Content: prompt},
	}
}

// BuildScriptRiskAnalysisMessages creates messages for script risk analysis.
func BuildScriptRiskAnalysisMessages(scriptContent string) []Message {
	return []Message{
		{Role: "system", Content: "You are a security expert tasked with analyzing shell scripts for potential risks. Evaluate the provided script and determine if it is 'risky' or 'not risky' to execute in a development environment. Provide a concise explanation for your assessment. If it's not risky, explicitly state 'not risky'. If it's risky, explain why and suggest potential dangers."},
		{Role: "user", Content: fmt.Sprintf("Analyze the following shell script:\n\n```bash\n%s\n```\n\nIs this script risky to execute? Explain your reasoning.", scriptContent)},
	}
}
