package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	DeepInfraURL = "https://api.deepinfra.com/v1/openai/chat/completions"
	DefaultModel = "deepseek-ai/DeepSeek-V3.1"
	
	// Model types for different use cases
	AgentModel = "deepseek-ai/DeepSeek-V3.1" // Primary agent model
	CoderModel = "qwen/Qwen3-Coder-480B-A35B-Instruct-Turbo" // Coding-specific model
	FastModel  = "openai/gpt-oss-20b" // Fast model for commits and simple tasks
	
	// Local models (all use the same model for local inference)
	LocalModel = "gpt-oss:20b"
)

// IsGPTOSSModel checks if a model uses the GPT-OSS family and requires harmony syntax
func IsGPTOSSModel(model string) bool {
	return strings.HasPrefix(model, "openai/gpt-oss")
}

type ImageData struct {
	URL    string `json:"url,omitempty"`    // URL to image
	Base64 string `json:"base64,omitempty"` // Base64 encoded image data
	Type   string `json:"type,omitempty"`   // MIME type (image/jpeg, image/png, etc.)
}

type Message struct {
	Role             string      `json:"role"`
	Content          string      `json:"content"`
	ReasoningContent string      `json:"reasoning_content,omitempty"`
	Images           []ImageData `json:"images,omitempty"` // Support for multiple images
}

type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type Choice struct {
	Index   int `json:"index"`
	Message struct {
		Role             string      `json:"role"`
		Content          string      `json:"content"`
		ReasoningContent string      `json:"reasoning_content,omitempty"`
		Images           []ImageData `json:"images,omitempty"`
		ToolCalls        []ToolCall  `json:"tool_calls,omitempty"`
	} `json:"message"`
	FinishReason string `json:"finish_reason"`
}

type ChatResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   struct {
		PromptTokens     int     `json:"prompt_tokens"`
		CompletionTokens int     `json:"completion_tokens"`
		TotalTokens      int     `json:"total_tokens"`
		EstimatedCost    float64 `json:"estimated_cost"`
		PromptTokensDetails struct {
			CachedTokens     int `json:"cached_tokens"`
			CacheWriteTokens *int `json:"cache_write_tokens"`
		} `json:"prompt_tokens_details,omitempty"`
	} `json:"usage"`
}

type Tool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string      `json:"name"`
		Description string      `json:"description"`
		Parameters  interface{} `json:"parameters"`
	} `json:"function"`
}

type ChatRequest struct {
	Model      string    `json:"model"`
	Messages   []Message `json:"messages"`
	Tools      []Tool    `json:"tools,omitempty"`
	ToolChoice string    `json:"tool_choice,omitempty"`
	MaxTokens  int       `json:"max_tokens,omitempty"`
	Reasoning  string    `json:"reasoning,omitempty"`
}

type Client struct {
	httpClient *http.Client
	apiToken   string
	debug      bool
	model      string
}

func NewClient() (*Client, error) {
	return NewClientWithModel(DefaultModel)
}

func NewClientWithModel(model string) (*Client, error) {
	token := os.Getenv("DEEPINFRA_API_KEY")
	if token == "" {
		return nil, fmt.Errorf("DEEPINFRA_API_KEY environment variable not set")
	}

	// Use default model if none specified
	if model == "" {
		model = DefaultModel
	}

	return &Client{
		httpClient: &http.Client{
			Timeout: 300 * time.Second, // Increased from 120s to 300s for complex reasoning tasks
		},
		apiToken: token,
		debug:    false, // Will be set later via SetDebug
		model:    model,
	}, nil
}

func (c *Client) SendChatRequest(req ChatRequest) (*ChatResponse, error) {
	var finalReq ChatRequest
	
	// Use harmony format only for GPT-OSS models
	if IsGPTOSSModel(req.Model) {
		// Convert to ENHANCED harmony format
		var formatter *HarmonyFormatter
		if req.Reasoning != "" {
			formatter = NewHarmonyFormatterWithReasoning(req.Reasoning)
		} else {
			formatter = NewHarmonyFormatter()
		}
		
		// Configure harmony options based on request
		opts := &HarmonyOptions{
			ReasoningLevel: req.Reasoning,
			EnableAnalysis: false, // Disable analysis channel to reduce excessive reasoning
		}
		if opts.ReasoningLevel == "" {
			opts.ReasoningLevel = "medium" // Reduced from "high" to "medium"
		}
		
		harmonyText := formatter.FormatMessagesForCompletion(req.Messages, req.Tools, opts)

		// Create a single message with harmony-formatted text
		finalReq = ChatRequest{
			Model:     req.Model,
			Messages:  []Message{{Role: "user", Content: harmonyText}},
			MaxTokens: req.MaxTokens,
			Reasoning: req.Reasoning,
			// Note: Don't include Tools in harmony format - they're embedded in the text
		}
	} else {
		// Use standard OpenAI format for other models
		finalReq = req
	}

	reqBody, err := json.Marshal(finalReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequest("POST", DeepInfraURL, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiToken)

	// Log the request for debugging
	if c.debug {
		log.Printf("DeepInfra Request URL: %s", DeepInfraURL)
		log.Printf("DeepInfra Request Headers: %v", httpReq.Header)
		log.Printf("DeepInfra Request Body: %s", string(reqBody))
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Log the response for debugging
	respBody, _ := io.ReadAll(resp.Body)
	if c.debug {
		log.Printf("DeepInfra Response Status: %s", resp.Status)
		log.Printf("DeepInfra Response Headers: %v", resp.Header)
		log.Printf("DeepInfra Response Body: %s", string(respBody))
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var chatResp ChatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	// Post-process harmony responses
	if IsGPTOSSModel(req.Model) {
		formatter := NewHarmonyFormatter()
		// Strip return token from responses before returning to agent
		for i, choice := range chatResp.Choices {
			chatResp.Choices[i].Message.Content = formatter.StripReturnToken(choice.Message.Content)
		}
	}

	return &chatResp, nil
}

func (c *Client) GetModel() string {
	return c.model
}

func GetToolDefinitions() []Tool {
	// Added ask_user tool for user clarification interactions
	// This tool simply returns a prompt string that the agent can display to the user.
	// It does not perform I/O in this non‑interactive environment.
	// The implementation is defined in tools/ask_user.go.

	return []Tool{
		{
			Type: "function",
			Function: struct {
				Name        string      `json:"name"`
				Description string      `json:"description"`
				Parameters  interface{} `json:"parameters"`
			}{
				Name:        "shell_command",
				Description: "Execute shell commands to explore directory structure, search files, run programs",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"command": map[string]interface{}{
							"type":        "string",
							"description": "Shell command to execute",
						},
					},
					"required": []string{"command"},
				},
			},
		},
		{
			Type: "function",
			Function: struct {
				Name        string      `json:"name"`
				Description string      `json:"description"`
				Parameters  interface{} `json:"parameters"`
			}{
				Name:        "read_file",
				Description: "Read contents of a specific file",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"file_path": map[string]interface{}{
							"type":        "string",
							"description": "Path to file to read",
						},
					},
					"required": []string{"file_path"},
				},
			},
		},
		{
			Type: "function",
			Function: struct {
				Name        string      `json:"name"`
				Description string      `json:"description"`
				Parameters  interface{} `json:"parameters"`
			}{
				Name:        "edit_file",
				Description: "Edit existing file by replacing old string with new string",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"file_path": map[string]interface{}{
							"type":        "string",
							"description": "Path to file to edit",
						},
						"old_string": map[string]interface{}{
							"type":        "string",
							"description": "Exact string to replace",
						},
						"new_string": map[string]interface{}{
							"type":        "string",
							"description": "New string to replace with",
						},
					},
					"required": []string{"file_path", "old_string", "new_string"},
				},
			},
		},
		{
			Type: "function",
			Function: struct {
				Name        string      `json:"name"`
				Description string      `json:"description"`
				Parameters  interface{} `json:"parameters"`
			}{
				Name:        "write_file",
				Description: "Write content to a new file or overwrite existing file",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"file_path": map[string]interface{}{
							"type":        "string",
							"description": "Path to file to write",
						},
						"content": map[string]interface{}{
							"type":        "string",
							"description": "Content to write to file",
						},
					},
					"required": []string{"file_path", "content"},
				},
			},
		},
		{
			Type: "function",
			Function: struct {
				Name        string      `json:"name"`
				Description string      `json:"description"`
				Parameters  interface{} `json:"parameters"`
			}{
				Name:        "add_todo",
				Description: "Add a new todo item to track task progress",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"title": map[string]interface{}{
							"type":        "string",
							"description": "Brief title of the todo item",
						},
						"description": map[string]interface{}{
							"type":        "string",
							"description": "Optional detailed description",
						},
						"priority": map[string]interface{}{
							"type":        "string",
							"description": "Priority level: high, medium, low",
						},
					},
					"required": []string{"title"},
				},
			},
		},
		{
			Type: "function",
			Function: struct {
				Name        string      `json:"name"`
				Description string      `json:"description"`
				Parameters  interface{} `json:"parameters"`
			}{
				Name:        "update_todo_status",
				Description: "Update the status of a todo item",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"id": map[string]interface{}{
							"type":        "string",
							"description": "ID of the todo item to update",
						},
						"status": map[string]interface{}{
							"type":        "string",
							"description": "New status: pending, in_progress, completed, cancelled",
						},
					},
					"required": []string{"id", "status"},
				},
			},
		},
		{
			Type: "function",
			Function: struct {
				Name        string      `json:"name"`
				Description string      `json:"description"`
				Parameters  interface{} `json:"parameters"`
			}{
				Name:        "list_todos",
				Description: "List all current todos with their status",
				Parameters: map[string]interface{}{
					"type":       "object",
					"properties": map[string]interface{}{},
					"required":   []string{},
				},
			},
		},
		{
			Type: "function",
			Function: struct {
				Name        string      `json:"name"`
				Description string      `json:"description"`
				Parameters  interface{} `json:"parameters"`
			}{
				Name:        "add_bulk_todos",
				Description: "Add multiple todo items at once for better efficiency",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"todos": map[string]interface{}{
							"type": "array",
							"items": map[string]interface{}{
								"type": "object",
								"properties": map[string]interface{}{
									"title": map[string]interface{}{
										"type":        "string",
										"description": "Brief title of the todo item",
									},
									"description": map[string]interface{}{
										"type":        "string",
										"description": "Optional detailed description",
									},
									"priority": map[string]interface{}{
										"type":        "string",
										"description": "Priority level: high, medium, low",
									},
								},
								"required": []string{"title"},
							},
							"description": "Array of todo items to add",
						},
					},
					"required": []string{"todos"},
				},
			},
		},
		{
			Type: "function",
			Function: struct {
				Name        string      `json:"name"`
				Description string      `json:"description"`
				Parameters  interface{} `json:"parameters"`
			}{
				Name:        "auto_complete_todos",
				Description: "Automatically complete todos based on context (e.g., after successful build)",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"context": map[string]interface{}{
							"type":        "string",
							"description": "Context trigger: build_success, test_success, file_written",
						},
					},
					"required": []string{"context"},
				},
			},
		},
		{
			Type: "function",
			Function: struct {
				Name        string      `json:"name"`
				Description string      `json:"description"`
				Parameters  interface{} `json:"parameters"`
			}{
				Name:        "get_next_todo",
				Description: "Get the next logical todo to work on based on priority and current state",
				Parameters: map[string]interface{}{
					"type":       "object",
					"properties": map[string]interface{}{},
					"required":   []string{},
				},
			},
		},
		{
			Type: "function",
			Function: struct {
				Name        string      `json:"name"`
				Description string      `json:"description"`
				Parameters  interface{} `json:"parameters"`
			}{
				Name:        "analyze_ui_screenshot",
				Description: "Comprehensive analysis of UI screenshots, mockups, and web designs. Extracts colors, layout, components, styling, and generates implementation guidance for frontend development. Use this for ANY React/Vue/Angular app creation, website building, or UI design implementation. Uses optimized prompts for maximum caching efficiency.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"image_path": map[string]interface{}{
							"type":        "string",
							"description": "Path to UI screenshot, mockup, or design file",
						},
					},
					"required": []string{"image_path"},
				},
			},
		},
		{
			Type: "function",
			Function: struct {
				Name        string      `json:"name"`
				Description string      `json:"description"`
				Parameters  interface{} `json:"parameters"`
			}{
				Name:        "analyze_image_content",
				Description: "General image analysis for text extraction, code screenshots, diagrams, and non-UI content. Use only for document text extraction, reading code from screenshots, or analyzing non-UI visual content.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"image_path": map[string]interface{}{
							"type":        "string",
							"description": "Path to image file containing text, code, or general content",
						},
						"analysis_prompt": map[string]interface{}{
							"type":        "string",
							"description": "Optional specific prompt for content extraction (extract text, read code, analyze diagram, etc.)",
						},
					},
					"required": []string{"image_path"},
				},
			},
		},
	}
}
