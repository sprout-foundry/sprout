package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/webcontent"
)

type browseURLHandler struct{}

func (h *browseURLHandler) Name() string { return "browse_url" }

// stepsParameterDescription builds the `steps` parameter description from the
// canonical action list, so the vocabulary the model reads cannot drift from
// the vocabulary the parser accepts. The api.ToolParameters wire type has no
// `enum` field, so this description is the only place the model sees the
// verbs on that path — it must name them all.
func stepsParameterDescription() string {
	return "Interaction steps. Each step's action must be one of: " +
		strings.Join(webcontent.BrowseStepActions(), ", ") + ". " +
		"There is NO wait step (use wait_for / wait_for_text, or sleep for a fixed delay) and NO screenshot step " +
		`(use the top-level action:"screenshot" with screenshot_path, or the step screenshot_selector). ` +
		`Text expectations go in "expect", never "text". ` +
		`Step fields: selector (CSS selector), value (fill value or navigate URL), key (press key, e.g. Enter), ` +
		`millis (milliseconds: sleep duration, or scroll_to y-offset), script (JavaScript for eval / wait_for_function), ` +
		`expect (expected text for wait_for_text / assert_text / assert_title / assert_url), screenshot_path (for screenshot_selector).`
}

func (h *browseURLHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "browse_url",
		Description: "Open a URL in a headless browser for debugging, scraping, or UI verification. Supports screenshots, DOM capture, interaction steps, and assertions. Activate the 'browse-debugging' skill for detailed patterns.",
		Required:    []string{"url"},
		Parameters: []ParameterDef{
			{Name: "url", Type: "string", Required: true, Description: "URL to browse (works with localhost)"},
			{Name: "action", Type: "string", Description: "Output: screenshot, dom, text (default), inspect, or interact", Enum: webcontent.BrowseActions()},
			{Name: "steps", Type: "array", Description: stepsParameterDescription(),
				Items: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"action": map[string]any{
							"type": "string",
							"enum": webcontent.BrowseStepActions(),
							"description": "Step action. One of: assert_selector, assert_text, assert_title, assert_url, back, click, eval, fill, forward, hover, navigate, press, reload, screenshot_selector, scroll_to, sleep, type, wait_for, wait_for_function, wait_for_text. " +
								"There is NO wait step (use wait_for / wait_for_text, or sleep for a fixed delay) and NO screenshot step (use the top-level action:\"screenshot\" with screenshot_path, or the step screenshot_selector).",
						},
						"selector":        map[string]any{"type": "string", "description": "CSS selector targeting the element for this step"},
						"value":           map[string]any{"type": "string", "description": "Value to fill or URL to navigate to"},
						"key":             map[string]any{"type": "string", "description": "Key to press (e.g. Enter, Tab, Control+A)"},
						"millis":          map[string]any{"type": "integer", "description": "Wait duration in milliseconds"},
						"script":          map[string]any{"type": "string", "description": "JavaScript to evaluate (action=eval)"},
						"expect":          map[string]any{"type": "string", "description": "Expected text/condition to wait for"},
						"screenshot_path": map[string]any{"type": "string", "description": "File path for action=screenshot_selector"},
					},
					"required": []any{"action"},
				}},
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
			{Name: "capture_selectors", Type: "array", Description: "CSS selectors to capture text/html/attrs from", Items: map[string]any{"type": "string"}},
			{Name: "response_max_chars", Type: "integer", Description: "Per-field truncation limit in inspect"},
			{Name: "user_agent", Type: "string", Description: "Override User-Agent string"},
			{Name: "cookies", Type: "object", Description: "Pre-navigation cookies as name=value pairs"},
			{Name: "headers", Type: "object", Description: "Custom HTTP headers for every request"},
			{Name: "allow_file_url", Type: "boolean", Description: "Enable file:// URL navigation (opt-in)"},
		},
	}
}

func (h *browseURLHandler) Validate(args map[string]any) error {
	if _, err := extractString(args, "url"); err != nil {
		return agenterrors.NewInvalidInputError(err.Error(), err)
	}
	return nil
}

func (h *browseURLHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	url, err := extractString(args, "url")
	if err != nil {
		// Invalid input, not a transient fault: return it typed so the tool
		// executor classifies it as non-retryable instead of re-running the
		// same bad call.
		return ToolResult{Output: err.Error(), IsError: true}, agenterrors.NewInvalidInputError(err.Error(), err)
	}

	// Pre-flight validate the top-level action before any browser work: an
	// unknown action must cost a message, not a Chromium launch.
	rawAction := extractStringOrEmpty(args, "action")
	action, ok := webcontent.NormalizeBrowseAction(rawAction)
	if !ok {
		msg := fmt.Sprintf("unknown action: %q (valid: %s)", rawAction, strings.Join(webcontent.BrowseActions(), ", "))
		return ToolResult{Output: msg, IsError: true}, agenterrors.NewInvalidInputError(msg, nil)
	}

	// Check if browser is available
	if env.WebBrowser == nil {
		errMsg := "browser not available: WebBrowser is not configured in this environment"
		// A missing browser is a property of the build/environment, not a
		// momentary fault: retrying cannot conjure one.
		return ToolResult{Output: errMsg, IsError: true}, agenterrors.NewPermanentError(errMsg, nil)
	}

	// Validate action-specific requirements (mirrors legacy handler)
	if action == "screenshot" {
		if sp, ok := args["screenshot_path"].(string); !ok || sp == "" {
			errMsg := "screenshot_path is required for action=screenshot"
			return ToolResult{Output: errMsg, IsError: true}, agenterrors.NewInvalidInputError(errMsg, nil)
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
		// Return the error as well as flagging the result: the tool executor
		// classifies the returned error (IsInvalidInput, IsTransient, ...), and
		// a stringified-only failure is misread as an untyped error worth
		// retrying. The classification mirrors classifyBrowseError in
		// pkg/agent/tool_handlers_browse.go so a transient navigation fault
		// stays retryable. Output and the error text are kept identical on
		// purpose — the executor renders err.Error(), while IsError/Output
		// remain populated for callers that branch on the result.
		classified := classifyBrowseFailure(err, url, action)
		return ToolResult{Output: classified.Error(), IsError: true}, classified
	}

	toolResult := ToolResult{Output: result}

	// Best-effort multimodal attachment for screenshots — see
	// browse_url_handler_image_nonjs.go for the !js implementation.
	// In WASM mode the stub returns immediately (vision APIs unavailable).
	attachScreenshotIfRequested(ctx, env, args, &toolResult)

	return toolResult, nil
}

// classifyBrowseFailure maps a browse failure onto the agent error taxonomy so
// the executor retries a transient network fault (connection reset, timeout,
// empty response) and does not retry a deterministic one. It mirrors
// classifyBrowseError in pkg/agent/tool_handlers_browse.go, keeping the two
// browse entry points consistent.
func classifyBrowseFailure(err error, url, action string) error {
	msg := fmt.Sprintf("failed to browse URL %s (action=%s)", url, action)
	if webcontent.IsTransientNavigationError(err) {
		return agenterrors.NewNetwork(msg, err)
	}
	return agenterrors.Wrapf(err, "%s", msg)
}

func (h *browseURLHandler) Aliases() []string      { return nil }
func (h *browseURLHandler) Timeout() time.Duration { return 0 }
func (h *browseURLHandler) MaxResultSize() int     { return 0 }
func (h *browseURLHandler) SafeForParallel() bool  { return false }
func (h *browseURLHandler) Interactive() bool      { return false }
