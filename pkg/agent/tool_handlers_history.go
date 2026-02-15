package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	tools "github.com/alantheprice/ledit/pkg/agent_tools"
)

// Tool handler implementations for history operations

func handleViewHistory(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	limit := 10
	if v, ok := args["limit"].(int); ok {
		limit = v
	} else if v, ok := args["limit"].(float64); ok {
		limit = int(v)
	}

	fileFilter := ""
	if v, ok := args["file_filter"].(string); ok {
		fileFilter = strings.TrimSpace(v)
	}

	var sincePtr *time.Time
	sinceDisplay := ""
	if raw, ok := args["since"].(string); ok && strings.TrimSpace(raw) != "" {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(raw))
		if err != nil {
			return "", fmt.Errorf("invalid time format for 'since': %s. Use ISO 8601 format like '2024-01-01T10:00:00Z'", raw)
		}
		sincePtr = &parsed
		sinceDisplay = parsed.Format(time.RFC3339)
	}

	showContent := false
	if v, ok := args["show_content"].(bool); ok {
		showContent = v
	}

	logParts := []string{fmt.Sprintf("limit=%d", limit)}
	if fileFilter != "" {
		logParts = append(logParts, fmt.Sprintf("file~%s", fileFilter))
	}
	if sincePtr != nil {
		logParts = append(logParts, fmt.Sprintf("since=%s", sinceDisplay))
	}
	if showContent {
		logParts = append(logParts, "with_content")
	}

	a.debugLog("Executing view_history with limit=%d, file_filter=%q, since=%s, show_content=%v\n", limit, fileFilter, sinceDisplay, showContent)

	res, err := tools.ViewHistory(limit, fileFilter, sincePtr, showContent)
	if err != nil {
		return "", err
	}

	a.debugLog("view_history metadata: %+v\n", res.Metadata)
	return res.Output, nil
}

func handleRollbackChanges(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	revisionID := ""
	if v, ok := args["revision_id"].(string); ok {
		revisionID = strings.TrimSpace(v)
	}

	filePath := ""
	if v, ok := args["file_path"].(string); ok {
		filePath = strings.TrimSpace(v)
	}

	confirm := false
	if v, ok := args["confirm"].(bool); ok {
		confirm = v
	}

	a.debugLog("Executing rollback_changes with revision_id=%q, file_path=%q, confirm=%v\n", revisionID, filePath, confirm)

	res, err := tools.RollbackChanges(revisionID, filePath, confirm)
	if err != nil {
		return "", err
	}

	a.debugLog("rollback_changes success=%v metadata=%+v\n", res.Success, res.Metadata)
	return res.Output, nil
}
