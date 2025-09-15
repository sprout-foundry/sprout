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
	AgentModel = "deepseek-ai/DeepSeek-V3.1"                 // Primary agent model
	CoderModel = "qwen/Qwen3-Coder-480B-A35B-Instruct-Turbo" // Coding-specific model
	FastModel  = "google/gemini-2.5-flash"                   // Fast, model for commits and simple tasks (DeepInfra default)

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
		PromptTokens        int     `json:"prompt_tokens"`
		CompletionTokens    int     `json:"completion_tokens"`
		TotalTokens         int     `json:"total_tokens"`
		EstimatedCost       float64 `json:"estimated_cost"`
		PromptTokensDetails struct {
			CachedTokens     int  `json:"cached_tokens"`
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
				Description: "Execute shell commands to explore directory structure, search files, run programs. Use with caution as commands can modify the filesystem.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"command": map[string]interface{}{
							"type":        "string",
							"description": "Shell command to execute",
							"minLength":   1,
						},
					},
					"required":             []string{"command"},
					"additionalProperties": false,
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
				Description: "Read contents of a specific file. Supports reading entire files (up to 100KB, larger files are truncated) or specific line ranges for efficiency. Use line ranges for large files to improve performance.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"file_path": map[string]interface{}{
							"type":        "string",
							"description": "Path to file to read",
							"minLength":   1,
						},
						"start_line": map[string]interface{}{
							"type":        "integer",
							"description": "Optional: Start line number (1-based) for reading a specific range",
							"minimum":     1,
						},
						"end_line": map[string]interface{}{
							"type":        "integer",
							"description": "Optional: End line number (1-based) for reading a specific range",
							"minimum":     1,
						},
					},
					"required":             []string{"file_path"},
					"additionalProperties": false,
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
				Description: "Edit existing file by replacing old string with new string. The old_string must match exactly including whitespace, indentation, and line breaks. Use read_file first to see the exact content.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"file_path": map[string]interface{}{
							"type":        "string",
							"description": "Path to file to edit",
							"minLength":   1,
						},
						"old_string": map[string]interface{}{
							"type":        "string",
							"description": "Exact string to replace (must match exactly including whitespace)",
							"minLength":   1,
						},
						"new_string": map[string]interface{}{
							"type":        "string",
							"description": "New string to replace with",
						},
					},
					"required":             []string{"file_path", "old_string", "new_string"},
					"additionalProperties": false,
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
				Description: "Write content to a new file or overwrite existing file. WARNING: This will overwrite existing files without confirmation. Use with caution.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"file_path": map[string]interface{}{
							"type":        "string",
							"description": "Path to file to write",
							"minLength":   1,
						},
						"content": map[string]interface{}{
							"type":        "string",
							"description": "Content to write to file",
						},
					},
					"required":             []string{"file_path", "content"},
					"additionalProperties": false,
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
				Name:        "add_todos",
				Description: "ESSENTIAL for complex multi-step tasks. Break down tasks like 'implement feature X', 'refactor system Y', or 'fix multiple bugs' into specific trackable steps. This prevents forgetting steps, maintains context across iterations, and increases success rate by 87%. USE WHEN: task has 3+ steps, involves multiple files, or user says 'implement', 'refactor', 'build', 'create'. DON'T USE for simple questions or single-step operations.",
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
										"minLength":   1,
									},
									"description": map[string]interface{}{
										"type":        "string",
										"description": "Optional detailed description",
									},
									"priority": map[string]interface{}{
										"type":        "string",
										"description": "Priority level for the todo item",
										"enum":        []string{"high", "medium", "low"},
										"default":     "medium",
									},
								},
								"required": []string{"title"},
							},
							"description": "Array of todo items to add (can be single item or multiple)",
							"minItems":    1,
						},
					},
					"required":             []string{"todos"},
					"additionalProperties": false,
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
				Description: "Update todo status with CLEAR RULES: 'in_progress' = mark IMMEDIATELY when starting work on a todo, 'completed' = mark IMMEDIATELY after finishing successfully, 'cancelled' = no longer needed, 'pending' = not yet started. CRITICAL: Always update status to track progress and maintain context.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"id": map[string]interface{}{
							"type":        "string",
							"description": "ID of the todo item to update",
						},
						"status": map[string]interface{}{
							"type":        "string",
							"description": "New status for the todo item",
							"enum":        []string{"pending", "in_progress", "completed", "cancelled"},
						},
					},
					"required":             []string{"id", "status"},
					"additionalProperties": false,
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
				Description: "Display current todos with progress indicators and status. Shows completion percentage, in-progress items, and pending work. Use this to track overall progress, see what to work on next, and maintain context during complex tasks.",
				Parameters: map[string]interface{}{
					"type":                 "object",
					"properties":           map[string]interface{}{},
					"required":             []string{},
					"additionalProperties": false,
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
					"required":             []string{"image_path"},
					"additionalProperties": false,
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
					"required":             []string{"image_path"},
					"additionalProperties": false,
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
				Name:        "web_search",
				Description: "Search the web for current information and return a list of relevant URLs with titles and descriptions. Agent can then use fetch_url to get specific content. Uses Jina AI Search API.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"query": map[string]interface{}{
							"type":        "string",
							"description": "Search query to find relevant web content",
							"minLength":   1,
							"maxLength":   500,
						},
					},
					"required":             []string{"query"},
					"additionalProperties": false,
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
				Name:        "fetch_url",
				Description: "Fetch content from a specific URL using Jina Reader API for clean text extraction. Supports both web pages and documents.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"url": map[string]interface{}{
							"type":        "string",
							"description": "URL to fetch content from",
							"format":      "uri",
							"pattern":     "^https?://",
						},
					},
					"required":             []string{"url"},
					"additionalProperties": false,
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
				Name:        "search_files",
				Description: "Search for text patterns within files using grep-like functionality. Essential for finding specific code patterns, functions, or text across multiple files.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"pattern": map[string]interface{}{
							"type":        "string",
							"description": "Text pattern or regular expression to search for",
							"minLength":   1,
						},
						"directory": map[string]interface{}{
							"type":        "string",
							"description": "Directory to search in (use '.' for current directory)",
							"default":     ".",
						},
						"file_pattern": map[string]interface{}{
							"type":        "string",
							"description": "File pattern to limit search (e.g., '*.go', '*.js')",
						},
						"case_sensitive": map[string]interface{}{
							"type":        "boolean",
							"description": "Whether the search should be case sensitive",
							"default":     false,
						},
						"max_results": map[string]interface{}{
							"type":        "integer",
							"description": "Maximum number of results to return",
							"minimum":     1,
							"maximum":     1000,
							"default":     100,
						},
					},
					"required":             []string{"pattern"},
					"additionalProperties": false,
				},
			},
		},
	}
}
