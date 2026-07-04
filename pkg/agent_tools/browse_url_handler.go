package tools

import (
	"context"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/events"
)

type browseURLHandler struct{}

func (h *browseURLHandler) Name() string { return "browse_url" }

func (h *browseURLHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "browse_url",
		Description: "Open a URL in a headless browser for debugging, scraping, or UI verification. Supports screenshots, DOM capture, interaction steps, and assertions. Activate the 'browse-debugging' skill for detailed patterns.",
		Required:    []string{"url"},
		Parameters: []ParameterDef{
			{Name: "url", Type: "string", Required: true, Description: "URL to browse (works with localhost)"},
			{Name: "action", Type: "string", Description: "Output: screenshot, dom, text (default), or inspect"},
			{Name: "steps", Type: "array", Description: "Interaction steps (click, fill, wait, navigate, eval, etc.)"},
			{Name: "viewport_width", Type: "integer", Description: "Browser width in px (default 1280)"},
			{Name: "viewport_height", Type: "integer", Description: "Browser height in px (default 720)"},
			{Name: "wait_for_selector", Type: "string", Description: "CSS selector to wait for before capture"},
			{Name: "wait_timeout_ms", Type: "integer", Description: "Selector wait timeout in ms (default 10000)"},
			{Name: "persist_session", Type: "boolean", Description: "Keep browser alive and return session_id"},
			{Name: "session_id", Type: "string", Description: "Reuse a persistent browser session"},
			{Name: "close_session", Type: "boolean", Description: "Close the persistent session"},
			{Name: "screenshot_path", Type: "string", Description: "File path for action=screenshot"},
			{Name: "include_console", Type: "boolean", Description: "Include console messages in inspect output"},
			{Name: "capture_network", Type: "boolean", Description: "Include network requests in inspect"},
			{Name: "capture_cookies", Type: "boolean", Description: "Include cookies in inspect output"},
			{Name: "capture_storage", Type: "boolean", Description: "Include localStorage/sessionStorage in inspect"},
			{Name: "capture_dom", Type: "boolean", Description: "Include rendered DOM in inspect output"},
			{Name: "capture_text", Type: "boolean", Description: "Include visible text in inspect output"},
			{Name: "capture_selectors", Type: "array", Description: "CSS selectors to capture text/html/attrs from"},
			{Name: "response_max_chars", Type: "integer", Description: "Per-field truncation limit in inspect"},
			{Name: "user_agent", Type: "string", Description: "Override User-Agent string"},
			{Name: "cookies", Type: "object", Description: "Pre-navigation cookies as name=value pairs"},
			{Name: "headers", Type: "object", Description: "Custom HTTP headers for every request"},
			{Name: "allow_file_url", Type: "boolean", Description: "Enable file:// URL navigation (opt-in)"},
		},
	}
}

func (h *browseURLHandler) Validate(args map[string]any) error {
	_, err := extractString(args, "url")
	return err
}

func (h *browseURLHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	toolName := h.Name()
	var hadError bool
	if env.EventBus != nil {
		env.EventBus.Publish(events.EventTypeToolStart, map[string]any{
			"tool":   toolName,
			"params": args,
		})
		defer func() {
			env.EventBus.Publish(events.EventTypeToolEnd, map[string]any{
				"tool":  toolName,
				"error": hadError,
			})
		}()
	}

	url, err := extractString(args, "url")
	if err != nil {
		hadError = true
		return ToolResult{Output: err.Error(), IsError: true}, nil
	}

	// Check if browser is available
	if env.WebBrowser == nil {
		hadError = true
		errMsg := "browser not available: WebBrowser is not configured in this environment"
		return ToolResult{Output: errMsg, IsError: true}, nil
	}

	// Validate action-specific requirements (mirrors legacy handler)
	if action, ok := args["action"].(string); ok && strings.ToLower(action) == "screenshot" {
		if sp, ok := args["screenshot_path"].(string); !ok || sp == "" {
			hadError = true
			errMsg := "screenshot_path is required for action=screenshot"
			return ToolResult{Output: errMsg, IsError: true}, nil
		}
	}

	// Build opts map from args (everything except url)
	opts := make(map[string]any)
	for k, v := range args {
		if k != "url" {
			opts[k] = v
		}
	}

	result, err := env.WebBrowser.BrowseURL(ctx, url, opts)
	if err != nil {
		hadError = true
		return ToolResult{Output: err.Error(), IsError: true}, nil
	}

	toolResult := ToolResult{Output: result}

	// Best-effort multimodal attachment for screenshots — see
	// browse_url_handler_image_nonjs.go for the !js implementation.
	// In WASM mode the stub returns immediately (vision APIs unavailable).
	attachScreenshotIfRequested(ctx, env, args, &toolResult)

	return toolResult, nil
}

func (h *browseURLHandler) Aliases() []string         { return nil }
func (h *browseURLHandler) Timeout() time.Duration    { return 0 }
func (h *browseURLHandler) MaxResultSize() int        { return 0 }
func (h *browseURLHandler) SafeForParallel() bool     { return false }
func (h *browseURLHandler) Interactive() bool         { return false }
