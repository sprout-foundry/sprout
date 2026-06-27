package tools

import (
	"context"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/events"
)

type browseURLHandler struct{}

func (h *browseURLHandler) Name() string { return "browse_url" }

func (h *browseURLHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "browse_url",
		Description: "Open a URL in a headless browser. Use this directly for localhost app debugging, JS-rendered scraping, and web UI verification when you need rendered state or when Playwright/MCP is unavailable. Supports screenshots, rendered DOM/text capture, persistent browser sessions across tool calls, navigation and interaction steps, assertions, selector inspection, browser console/error capture, network request summaries including CORS signals, cookies/storage snapshots, and responsive testing via custom viewport sizes.",
		Required:    []string{"url"},
		Parameters: []ParameterDef{
			{Name: "url", Type: "string", Required: true, Description: "URL to browse — works with localhost URLs for testing local apps"},
			{Name: "action", Type: "string", Description: "What to do: 'screenshot' (save PNG), 'dom' (return rendered HTML), 'text' (return visible text, default), or 'inspect' (return structured JSON with page state and diagnostics)"},
			{Name: "steps", Type: "array", Description: "Optional interaction steps. Each step object supports action=wait_for|wait_for_text|assert_selector|assert_text|assert_title|assert_url|click|hover|type|fill|press|sleep|scroll_to|navigate|reload|back|forward|eval plus selector/value/key/millis/script/expect fields as needed"},
			{Name: "viewport_width", Type: "integer", Description: "Browser viewport width in pixels (default: 1280)"},
			{Name: "viewport_height", Type: "integer", Description: "Browser viewport height in pixels (default: 720)"},
			{Name: "wait_for_selector", Type: "string", Description: "Optional CSS selector to wait for before capturing output or running steps"},
			{Name: "wait_timeout_ms", Type: "integer", Description: "Optional selector wait timeout in milliseconds (default: 10000)"},
			{Name: "persist_session", Type: "boolean", Description: "Keep the browser page alive after this call and return a session_id in inspect output"},
			{Name: "session_id", Type: "string", Description: "Reuse a persistent built-in browser session across multiple browse_url calls for iterative debugging"},
			{Name: "close_session", Type: "boolean", Description: "Close the referenced persistent session after this call completes"},
			{Name: "screenshot_path", Type: "string", Description: "File path to save screenshot (required when action=screenshot, e.g. /tmp/sprout_examples/screenshot.png)"},
			{Name: "include_console", Type: "boolean", Description: "Include browser console messages and page errors in inspect output"},
			{Name: "capture_network", Type: "boolean", Description: "Include fetch/XHR network request summaries in inspect output"},
			{Name: "capture_cookies", Type: "boolean", Description: "Include document.cookie-visible cookies in inspect output"},
			{Name: "capture_storage", Type: "boolean", Description: "Include localStorage and sessionStorage snapshots in inspect output"},
			{Name: "capture_dom", Type: "boolean", Description: "Include rendered DOM in inspect output"},
			{Name: "capture_text", Type: "boolean", Description: "Include visible text in inspect output"},
			{Name: "capture_selectors", Type: "array", Description: "Optional list of CSS selectors to capture after interactions (text/html/value/basic attrs)"},
			{Name: "response_max_chars", Type: "integer", Description: "Optional per-field truncation limit for inspect output"},
			{Name: "user_agent", Type: "string", Description: "Override the browser User-Agent string"},
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

	return ToolResult{Output: result}, nil
}
