package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/alantheprice/ledit/pkg/webcontent"
)

func handleBrowseURL(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	// Extract url (required)
	url, err := convertToString(args["url"], "url")
	if err != nil {
		return "", fmt.Errorf("failed to convert url parameter: %w", err)
	}

	// Extract action (optional, default "text")
	action := "text"
	if v, ok := args["action"].(string); ok && v != "" {
		action = strings.ToLower(v)
	}

	opts := webcontent.BrowseOptions{
		Ctx:    ctx,
		Action: action,
	}

	// Extract viewport dimensions
	if v, ok := args["viewport_width"].(float64); ok {
		opts.ViewportWidth = int(v)
	}
	if v, ok := args["viewport_height"].(float64); ok {
		opts.ViewportHeight = int(v)
	}

	// Extract user agent
	if v, ok := args["user_agent"].(string); ok {
		opts.UserAgent = v
	}

	// Extract screenshot path
	if v, ok := args["screenshot_path"].(string); ok {
		opts.ScreenshotPath = v
	}
	if v, ok := args["session_id"].(string); ok {
		opts.SessionID = v
	}
	if v, ok := args["persist_session"].(bool); ok {
		opts.PersistSession = v
	}
	if v, ok := args["close_session"].(bool); ok {
		opts.CloseSession = v
	}
	if v, ok := args["wait_for_selector"].(string); ok {
		opts.WaitForSelector = v
	}
	if v, ok := args["wait_timeout_ms"].(float64); ok {
		opts.WaitTimeoutMs = int(v)
	}
	if v, ok := args["capture_dom"].(bool); ok {
		opts.CaptureDOM = v
	}
	if v, ok := args["capture_text"].(bool); ok {
		opts.CaptureText = v
	}
	if v, ok := args["include_console"].(bool); ok {
		opts.IncludeConsole = v
	}
	if v, ok := args["capture_network"].(bool); ok {
		opts.CaptureNetwork = v
	}
	if v, ok := args["capture_storage"].(bool); ok {
		opts.CaptureStorage = v
	}
	if v, ok := args["capture_cookies"].(bool); ok {
		opts.CaptureCookies = v
	}
	if v, ok := args["response_max_chars"].(float64); ok {
		opts.ResponseMaxChars = int(v)
	}
	if rawSelectors, ok := args["capture_selectors"].([]interface{}); ok {
		opts.CaptureSelectors = make([]string, 0, len(rawSelectors))
		for _, raw := range rawSelectors {
			if selector, ok := raw.(string); ok && strings.TrimSpace(selector) != "" {
				opts.CaptureSelectors = append(opts.CaptureSelectors, selector)
			}
		}
	}
	if rawSteps, ok := args["steps"].([]interface{}); ok {
		steps, err := parseBrowseSteps(rawSteps)
		if err != nil {
			return "", fmt.Errorf("failed to parse browse steps: %w", err)
		}
		opts.Steps = steps
	}

	// Validate screenshot_path for screenshot action
	if action == "screenshot" && opts.ScreenshotPath == "" {
		return "", fmt.Errorf("screenshot_path is required for action=screenshot")
	}
	if opts.ScreenshotPath != "" {
		if _, err := filepath.Abs(opts.ScreenshotPath); err != nil {
			return "", fmt.Errorf("invalid screenshot_path: %w", err)
		}
	}

	a.debugLog("Browsing URL: %s action=%s viewport=%dx%d\n", url, action, opts.ViewportWidth, opts.ViewportHeight)

	result, err := webcontent.BrowseURL(url, opts)
	if err != nil {
		return "", fmt.Errorf("failed to browse URL %s (action=%s): %w", url, action, err)
	}

	return result, nil
}

func parseBrowseSteps(rawSteps []interface{}) ([]webcontent.BrowseStep, error) {
	steps := make([]webcontent.BrowseStep, 0, len(rawSteps))
	for idx, rawStep := range rawSteps {
		stepMap, ok := rawStep.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("browse_url steps[%d] must be an object", idx)
		}
		encoded, err := json.Marshal(stepMap)
		if err != nil {
			return nil, fmt.Errorf("browse_url steps[%d] marshal failed: %w", idx, err)
		}
		var step webcontent.BrowseStep
		if err := json.Unmarshal(encoded, &step); err != nil {
			return nil, fmt.Errorf("browse_url steps[%d] parse failed: %w", idx, err)
		}
		if strings.TrimSpace(step.Action) == "" {
			return nil, fmt.Errorf("browse_url steps[%d] requires action", idx)
		}
		steps = append(steps, step)
	}
	return steps, nil
}
