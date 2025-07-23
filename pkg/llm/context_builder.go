package llm

import (
	"bufio"
	"encoding/json"
	"fmt"
	"ledit/pkg/config"
	"ledit/pkg/prompts" // Import the new prompts package
	"os"
	"os/exec"
	"strings"
	"time"
)

// --- Message Structs ---

type ContextRequest struct {
	Type  string `json:"type"`
	Query string `json:"query"`
}

type ContextResponse struct {
	ContextRequests []ContextRequest `json:"context_requests"`
}

func handleContextRequest(reqs []ContextRequest, cfg *config.Config) (string, error) {
	var responses []string
	for _, req := range reqs {
		fmt.Printf(prompts.LLMContextRequest(req.Type, req.Query)) // Use prompt
		switch req.Type {
		case "search":
			responses = append(responses, "Web search is not yet implemented.")
		case "user_prompt":
			fmt.Printf(prompts.LLMUserQuestion(req.Query)) // Use prompt
			reader := bufio.NewReader(os.Stdin)
			answer, err := reader.ReadString('\n')
			if err != nil {
				return "", fmt.Errorf("failed to read user input: %w", err)
			}
			responses = append(responses, fmt.Sprintf("The user responded: %s", strings.TrimSpace(answer)))
		case "file":
			fmt.Printf(prompts.LLMFileRequest(req.Query)) // Use prompt
			content, err := os.ReadFile(req.Query)
			if err != nil {
				return "", fmt.Errorf("failed to read file '%s': %w", req.Query, err)
			}
			responses = append(responses, fmt.Sprintf("Here is the content of the file `%s`:\n\n%s", req.Query, string(content)))
		case "shell":
			fmt.Printf(prompts.LLMShellCommandRequest(req.Query)) // Use prompt
			fmt.Println(prompts.LLMShellWarning())                // Use prompt
			fmt.Print(prompts.LLMShellConfirmation())             // Use prompt
			reader := bufio.NewReader(os.Stdin)
			confirm, _ := reader.ReadString('\n')
			if strings.TrimSpace(strings.ToLower(confirm)) != "y" {
				responses = append(responses, "User denied execution of shell command.")
				continue
			}

			cmd := exec.Command("sh", "-c", req.Query)
			output, err := cmd.CombinedOutput()
			if err != nil {
				responses = append(responses, fmt.Sprintf("Shell command failed with error: %v\nOutput:\n%s", err, string(output)))
			} else {
				responses = append(responses, fmt.Sprintf("The shell command `%s` produced the following output:\n\n%s", req.Query, string(output)))
			}
		default:
			return "", fmt.Errorf("unknown context request type: %s", req.Type)
		}
	}
	return strings.Join(responses, "\n"), nil
}

func GetLLMCodeResponse(cfg *config.Config, code, instructions, filename string) (string, string, error) {
	modelName := cfg.EditingModel
	fmt.Printf(prompts.UsingModel(modelName)) // Use prompt

	messages := prompts.BuildCodeMessages(code, instructions, filename, cfg.Interactive)

	if !cfg.Interactive {
		_, response, err := GetLLMResponse(modelName, messages, filename, cfg, 6*time.Minute)
		if err != nil {
			return modelName, "", err
		}
		return modelName, response, nil
	}

	const maxContextRequests = 5
	contextRequestCount := 0

	for {
		if contextRequestCount >= maxContextRequests {
			fmt.Println(prompts.LLMMaxContextRequestsReached()) // Use prompt
			messages[0] = prompts.GetCodeGenMessages()[0]       // Reset to code generation message
		}

		// Default timeout for code generation is 6 minutes
		_, response, err := GetLLMResponse(modelName, messages, filename, cfg, 6*time.Minute)
		if err != nil {
			return modelName, "", err
		}

		// Try to extract JSON from response (handles both raw JSON and code block JSON)
		var jsonStr string
		if strings.Contains(response, "```json") {
			// Handle code block JSON
			parts := strings.Split(response, "```json")
			if len(parts) > 1 {
				jsonPart := parts[1]
				end := strings.Index(jsonPart, "```")
				if end > 0 {
					jsonStr = strings.TrimSpace(jsonPart[:end])
				} else {
					jsonStr = strings.TrimSpace(jsonPart)
				}
			}
		} else if strings.Contains(response, "context_requests") {
			// Handle raw JSON
			jsonStr = response
		}

		if jsonStr != "" {
			// Clean up the JSON string by removing any remaining backticks or whitespace
			jsonStr = strings.TrimSpace(jsonStr)
			jsonStr = strings.Trim(jsonStr, "`")

			var contextResponse ContextResponse
			if err := json.Unmarshal([]byte(jsonStr), &contextResponse); err != nil {
				fmt.Printf(prompts.LLMContextParseError(err, response)) // Use prompt
				return modelName, response, nil                         // Return the raw response if parsing fails
			}
			if len(contextResponse.ContextRequests) == 0 {
				fmt.Println(prompts.LLMNoContextRequests()) // Use prompt
				return modelName, response, nil              // No context requests, return the response
			}
			fmt.Printf(prompts.LLMContextRequestsFound(len(contextResponse.ContextRequests))) // Use prompt
			contextRequestCount++
			additionalContext, err := handleContextRequest(contextResponse.ContextRequests, cfg)
			if err != nil {
				fmt.Printf(prompts.LLMContextRequestError(err)) // Use prompt
				messages = append(messages, prompts.Message{
					Role:    "assistant",
					Content: response,
				}, prompts.Message{
					Role:    "user",
					Content: fmt.Sprintf("There was an error handling your request: %v. Please try a different request or generate the code.", err),
				})
			} else {
				fmt.Printf(prompts.LLMAddingContext(additionalContext)) // Use prompt
				messages = append(messages, prompts.Message{
					Role:    "assistant",
					Content: response,
				}, prompts.Message{
					Role:    "user",
					Content: additionalContext,
				})
			}
			continue // Loop to ask LLM again with new context
		} else {
			return modelName, response, nil // No context requests, return the response
		}
	}
}