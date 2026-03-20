package agent

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/alantheprice/ledit/pkg/webcontent"
)

func handleBrowseURL(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	// Extract url (required)
	url, err := convertToString(args["url"], "url")
	if err != nil {
		return "", err
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
		return "", err
	}

	return result, nil
}
