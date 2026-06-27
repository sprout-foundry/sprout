package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/webcontent"
)

// browserAdapter adapts the webcontent.BrowseURL function to the WebBrowser interface.
type browserAdapter struct{}

// buildBrowseOptions converts a flexible map[string]any options map into
// webcontent.BrowseOptions. Field extraction mirrors the legacy handler in
// pkg/agent/tool_handlers_browse.go for conformance.
func buildBrowseOptions(opts map[string]any) (webcontent.BrowseOptions, error) {
	if opts == nil {
		opts = make(map[string]any)
	}
	// Extract action (optional, default "text")
	action := "text"
	if v, ok := opts["action"].(string); ok && v != "" {
		action = strings.ToLower(v)
	}

	browseOpts := webcontent.BrowseOptions{
		Action: action,
	}

	// Extract viewport dimensions
	if v, ok := opts["viewport_width"].(float64); ok {
		browseOpts.ViewportWidth = int(v)
	}
	if v, ok := opts["viewport_height"].(float64); ok {
		browseOpts.ViewportHeight = int(v)
	}

	// Extract user agent
	if v, ok := opts["user_agent"].(string); ok {
		browseOpts.UserAgent = v
	}

	// Extract screenshot path
	if v, ok := opts["screenshot_path"].(string); ok {
		browseOpts.ScreenshotPath = v
	}
	if v, ok := opts["session_id"].(string); ok {
		browseOpts.SessionID = v
	}
	if v, ok := opts["persist_session"].(bool); ok {
		browseOpts.PersistSession = v
	}
	if v, ok := opts["close_session"].(bool); ok {
		browseOpts.CloseSession = v
	}
	if v, ok := opts["wait_for_selector"].(string); ok {
		browseOpts.WaitForSelector = v
	}
	if v, ok := opts["wait_timeout_ms"].(float64); ok {
		browseOpts.WaitTimeoutMs = int(v)
	}
	if v, ok := opts["capture_dom"].(bool); ok {
		browseOpts.CaptureDOM = v
	}
	if v, ok := opts["capture_text"].(bool); ok {
		browseOpts.CaptureText = v
	}
	if v, ok := opts["include_console"].(bool); ok {
		browseOpts.IncludeConsole = v
	}
	if v, ok := opts["capture_network"].(bool); ok {
		browseOpts.CaptureNetwork = v
	}
	if v, ok := opts["capture_storage"].(bool); ok {
		browseOpts.CaptureStorage = v
	}
	if v, ok := opts["capture_cookies"].(bool); ok {
		browseOpts.CaptureCookies = v
	}
	if v, ok := opts["response_max_chars"].(float64); ok {
		browseOpts.ResponseMaxChars = int(v)
	}

	// Extract capture_selectors ([]interface{} → []string)
	if rawSelectors, ok := opts["capture_selectors"].([]interface{}); ok {
		browseOpts.CaptureSelectors = make([]string, 0, len(rawSelectors))
		for _, raw := range rawSelectors {
			if selector, ok := raw.(string); ok && strings.TrimSpace(selector) != "" {
				browseOpts.CaptureSelectors = append(browseOpts.CaptureSelectors, selector)
			}
		}
	}

	// Extract steps ([]interface{} → []webcontent.BrowseStep via JSON marshal/unmarshal)
	if rawSteps, ok := opts["steps"].([]interface{}); ok {
		steps, err := parseBrowseSteps(rawSteps)
		if err != nil {
			return browseOpts, fmt.Errorf("failed to parse browse steps: %w", err)
		}
		browseOpts.Steps = steps
	}

	return browseOpts, nil
}

// BrowseURL converts a flexible map[string]any options map into webcontent.BrowseOptions
// then delegates to webcontent.BrowseURL. Field extraction mirrors the legacy handler
// in pkg/agent/tool_handlers_browse.go for conformance.
func (a *browserAdapter) BrowseURL(ctx context.Context, url string, opts map[string]any) (string, error) {
	browseOpts, err := buildBrowseOptions(opts)
	if err != nil {
		return "", err
	}
	browseOpts.Ctx = ctx
	return webcontent.BrowseURL(url, browseOpts)
}

// parseBrowseSteps converts []interface{} to []webcontent.BrowseStep via JSON
// marshal/unmarshal, mirroring the legacy handler's logic.
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

// NewBrowserAdapter creates a browser adapter instance.
func NewBrowserAdapter() WebBrowser {
	return &browserAdapter{}
}
