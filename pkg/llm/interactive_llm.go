package llm

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/prompts"
)

// ContextHandler is a function type that defines how context requests are handled.
// It takes a slice of ContextRequest and returns a string response and an error.
type ContextHandler func([]ContextRequest, *config.Config) (string, error)

// ContextRequest represents a request for additional context from the LLM.
type ContextRequest struct {
	Type  string `json:"type"`
	Query string `json:"query"`
}

// ContextResponse represents the LLM's response containing context requests.
type ContextResponse struct {
	ContextRequests []ContextRequest `json:"context_requests"`
}

// CallLLMWithInteractiveContext handles interactive LLM calls, processing context requests, and retrying the LLM call.
func CallLLMWithInteractiveContext(
	modelName string,
	initialMessages []prompts.Message,
	filename string,
	cfg *config.Config,
	timeout time.Duration,
	contextHandler ContextHandler, // This is the key: it takes a handler function
) (string, error) {
	currentMessages := initialMessages
	maxRetries := 5 // Limit the number of interactive turns

	for i := 0; i < maxRetries; i++ {
		// Call the main LLM response function (which is in api.go, same package)
		_, response, err := GetLLMResponse(modelName, currentMessages, filename, cfg, timeout)
		if err != nil {
			return "", fmt.Errorf("LLM call failed: %w", err)
		}

		// Check if the response contains a context request
		if strings.Contains(response, "<CONTEXT_REQUEST>") && strings.Contains(response, "</CONTEXT_REQUEST>") {
			contextRequestBlock := extractContextRequestBlock(response)
			if contextRequestBlock == "" {
				return "", fmt.Errorf("LLM returned malformed context request block")
			}

			var contextResp ContextResponse
			if err := json.Unmarshal([]byte(contextRequestBlock), &contextResp); err != nil {
				return "", fmt.Errorf("failed to unmarshal context request: %w", err)
			}

			if len(contextResp.ContextRequests) > 0 {
				// Handle the context requests using the provided handler
				contextContent, err := contextHandler(contextResp.ContextRequests, cfg)
				if err != nil {
					return "", fmt.Errorf("failed to handle context request: %w", err)
				}

				// Append the context content as a new message from the user
				currentMessages = append(currentMessages, prompts.Message{
					Role:    "user",
					Content: fmt.Sprintf("<CONTEXT_RESPONSE>\n%s\n</CONTEXT_RESPONSE>", contextContent),
				})
				// Continue the loop to send the updated messages to the LLM
				continue
			}
		}

		// If no context request, or if all requests were handled, return the response
		return response, nil
	}

	return "", fmt.Errorf("max interactive LLM retries reached (%d)", maxRetries)
}

// extractContextRequestBlock extracts the content within <CONTEXT_REQUEST> tags.
func extractContextRequestBlock(response string) string {
	startTag := "<CONTEXT_REQUEST>"
	endTag := "</CONTEXT_REQUEST>"

	startIndex := strings.Index(response, startTag)
	if startIndex == -1 {
		return ""
	}
	startIndex += len(startTag)

	endIndex := strings.Index(response[startIndex:], endTag)
	if endIndex == -1 {
		return ""
	}
	endIndex += startIndex

	return strings.TrimSpace(response[startIndex:endIndex])
}

// Removed the placeholder GetLLMResponse function as it's implemented in api.go
// and can be called directly within the same package.
