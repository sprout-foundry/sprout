package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

// registerPreviewPortHandler implements ToolHandler for register_preview_port.
// Called by the agent when it starts a web server or dev server inside a
// workspace, so the user gets a browser-accessible preview URL.
type registerPreviewPortHandler struct{}

func (h *registerPreviewPortHandler) Name() string { return "register_preview_port" }

func (h *registerPreviewPortHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name: "register_preview_port",
		Description: "Register a port for browser preview access. Call this when you start a web server, " +
			"dev server, or any HTTP service inside the workspace. Returns a preview URL the user can open. " +
			"Only call this AFTER the server is running and listening on a port.",
		Required: []string{"port"},
		Parameters: []ParameterDef{
			{Name: "port", Type: "integer", Required: true,
				Description: "The port number the server is listening on (1024-65535)"},
			{Name: "label", Type: "string",
				Description: "Human-readable label for this service (e.g., 'Next.js dev server', 'API server')"},
		},
	}
}

func (h *registerPreviewPortHandler) Validate(args map[string]any) error {
	port, ok := args["port"]
	if !ok {
		return fmt.Errorf("register_preview_port: 'port' is required")
	}
	portNum, ok := toInt(port)
	if !ok || portNum < 1024 || portNum > 65535 {
		return fmt.Errorf("register_preview_port: port must be between 1024 and 65535")
	}
	return nil
}

func (h *registerPreviewPortHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	port, _ := toInt(args["port"])
	label, _ := extractString(args, "label")
	if label == "" {
		label = "dev server"
	}

	workspaceID := os.Getenv("WORKSPACE_ID")
	if workspaceID == "" {
		return ToolResult{
			Output: "WORKSPACE_ID not set — preview port registration is only available in platform workspaces.",
		}, nil
	}

	platformURL := os.Getenv("PLATFORM_API_URL")
	if platformURL == "" {
		platformURL = "http://172.17.0.1:8080" // Default Docker bridge gateway
	}

	body, _ := json.Marshal(map[string]interface{}{
		"port":  port,
		"label": label,
	})

	client := &http.Client{Timeout: 5 * time.Second}
	url := fmt.Sprintf("%s/workspace/%s/ports", platformURL, workspaceID)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return ToolResult{Output: fmt.Sprintf("Failed to create request: %v", err)}, nil
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		// Not in a platform workspace or platform API unreachable —
		// not an error, just inform the agent.
		return ToolResult{
			Output: fmt.Sprintf("Preview port registration unavailable (platform API not reachable). The server is running on port %d.", port),
		}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return ToolResult{
			Output: fmt.Sprintf("Preview port registration failed (HTTP %d). The server is running on port %d.", resp.StatusCode, port),
		}, nil
	}

	var result struct {
		PreviewURL string `json:"preview_url"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&result)

	return ToolResult{
		Output: fmt.Sprintf("Preview URL: %s\nThe user can open this in their browser to see the %s (port %d).",
			result.PreviewURL, label, port),
	}, nil
}

// Aliases, Timeout, MaxResultSize, SafeForParallel, Interactive — defaults
func (h *registerPreviewPortHandler) Aliases() []string      { return nil }
func (h *registerPreviewPortHandler) Timeout() time.Duration { return 10 * time.Second }
func (h *registerPreviewPortHandler) MaxResultSize() int     { return 1024 }
func (h *registerPreviewPortHandler) SafeForParallel() bool  { return true }
func (h *registerPreviewPortHandler) Interactive() bool      { return false }

// toInt extracts an integer from an interface{} that may be float64 or int.
func toInt(v interface{}) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	}
	return 0, false
}
