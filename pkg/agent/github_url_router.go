package agent

import (
	"context"
	"fmt"

	"github.com/alantheprice/ledit/pkg/webcontent"
)

// githubMCPToolMapping maps GitHubURLInfo.Type to the corresponding GitHub MCP tool name.
var githubMCPToolMapping = map[string]string{
	"issue":        "get_issue",
	"pull_request": "get_pull_request",
	"repo":         "search_repositories",
	"actions_run":  "actions_get",
	"discussion":   "get_discussion",
	"commit":       "get_commit",
	"release":      "get_release_by_tag",
}

// tryRouteGitHubToMCP attempts to use the GitHub MCP server to fetch GitHub URL content.
// Returns (content, true, nil) if successfully routed to MCP.
// Returns ("", false, nil) if MCP is not available or URL is not a GitHub resource type that MCP handles.
// Returns ("", false, nil) if MCP routing was attempted but failed (graceful fallback).
func (a *Agent) tryRouteGitHubToMCP(ctx context.Context, rawURL string) (string, bool, error) {
	if a.mcpManager == nil {
		return "", false, nil
	}

	info := webcontent.ParseGitHubURL(rawURL)
	if info.Type == "unknown" {
		return "", false, nil
	}

	// Only route resource types that have MCP tool mappings.
	// Types like "file", "directory", and "gist" are either handled by
	// the raw-GitHub rewrite or are not well-supported by the MCP tools.
	mcpTool, ok := githubMCPToolMapping[info.Type]
	if !ok {
		return "", false, nil
	}

	// Release URLs without a tag (numeric ID) have no matching MCP tool.
	// get_release_by_tag requires a tag name; /releases/123 falls through.
	if info.Type == "release" && info.Ref == "" {
		return "", false, nil
	}

	// Check if the GitHub MCP server is available and running.
	server, exists := a.mcpManager.GetServer("github")
	if !exists || !server.IsRunning() {
		a.debugLog("GitHub MCP server not available, falling back to normal fetch\n")
		return "", false, nil
	}

	// Build MCP tool arguments based on resource type.
	args, err := buildGitHubMCPArgs(info)
	if err != nil {
		a.debugLog("Failed to build GitHub MCP args: %v\n", err)
		return "", false, nil
	}

	// Call the MCP tool.
	result, callErr := a.mcpManager.CallTool(ctx, "github", mcpTool, args)
	if callErr != nil {
		a.debugLog("GitHub MCP CallTool failed: %v, falling back to normal fetch\n", callErr)
		return "", false, nil
	}

	// If the MCP result itself indicates an error, fall through gracefully.
	if result != nil && result.IsError {
		a.debugLog("GitHub MCP returned error result, falling back to normal fetch\n")
		return "", false, nil
	}

	content := formatMCPResult(result)
	return content, true, nil
}

// buildGitHubMCPArgs constructs the arguments map for a GitHub MCP tool call
// based on the parsed GitHub URL info.
func buildGitHubMCPArgs(info webcontent.GitHubURLInfo) (map[string]interface{}, error) {
	switch info.Type {
	case "issue":
		return map[string]interface{}{
			"owner":        info.Owner,
			"repo":         info.Repo,
			"issue_number": float64(info.Number),
		}, nil

	case "pull_request":
		return map[string]interface{}{
			"owner":       info.Owner,
			"repo":        info.Repo,
			"pull_number": float64(info.Number),
		}, nil

	case "repo":
		return map[string]interface{}{
			"q": fmt.Sprintf("repo:%s/%s", info.Owner, info.Repo),
		}, nil

	case "actions_run":
		return map[string]interface{}{
			"owner":       info.Owner,
			"repo":        info.Repo,
			"method":      "get_workflow_run",
			"resource_id": fmt.Sprintf("%d", info.Number),
		}, nil

	case "discussion":
		return map[string]interface{}{
			"owner":             info.Owner,
			"repo":              info.Repo,
			"discussion_number": float64(info.Number),
		}, nil

	case "commit":
		return map[string]interface{}{
			"owner": info.Owner,
			"repo":  info.Repo,
			"sha":   info.Ref,
		}, nil

	case "release":
		return map[string]interface{}{
			"owner": info.Owner,
			"repo":  info.Repo,
			"tag":   info.Ref,
		}, nil

	default:
		return nil, fmt.Errorf("unsupported GitHub URL type: %s", info.Type)
	}
}
