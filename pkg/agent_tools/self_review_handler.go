package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/events"
)

type selfReviewHandler struct{}

func (h *selfReviewHandler) Name() string { return "self_review" }

func (h *selfReviewHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "self_review",
		Description: "Review the agent's own work against a canonical specification extracted from the conversation to detect scope creep and ensure alignment with user requirements",
		Required: []string{},
		Parameters: []ParameterDef{
			{Name: "revision_id", Type: "string", Description: "Optional revision ID to review (defaults to current/most recent revision)"},
		},
	}
}

func (h *selfReviewHandler) Validate(args map[string]any) error {
	if args == nil || len(args) == 0 {
		return fmt.Errorf("arguments must not be nil or empty")
	}
	return nil
}

func (h *selfReviewHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	toolName := h.Name()
	if env.EventBus != nil {
		env.EventBus.Publish(events.EventTypeToolStart, map[string]any{
			"tool":   toolName,
			"params": args,
		})
		defer func() {
			env.EventBus.Publish(events.EventTypeToolEnd, map[string]any{
				"tool":  toolName,
				"error": false,
			})
		}()
	}

	revisionID, _ := extractString(args, "revision_id")

	// Find workspace root
	workspaceRoot := env.WorkspaceRoot
	if workspaceRoot == "" {
		workspaceRoot, _ = os.Getwd()
	}

	revisionDir := filepath.Join(workspaceRoot, ".sprout", "revisions")
	var revisionPath string

	if revisionID != "" {
		revisionPath = filepath.Join(revisionDir, revisionID)
		if _, err := os.Stat(revisionPath); os.IsNotExist(err) {
			return ToolResult{Output: fmt.Sprintf("Revision %q not found", revisionID), IsError: true}, nil
		}
	} else {
		// Find most recent revision
		entries, err := os.ReadDir(revisionDir)
		if err != nil {
			return ToolResult{Output: "No revisions found"}, nil
		}
		if len(entries) == 0 {
			return ToolResult{Output: "No revisions found"}, nil
		}
		// Sort by name (timestamp-based names sort chronologically)
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].Name() < entries[j].Name()
		})
		last := entries[len(entries)-1]
		revisionPath = filepath.Join(revisionDir, last.Name())
	}

	// Read the revision manifest
	manifestPath := filepath.Join(revisionPath, "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		// Try revision.txt
		revisionFile := filepath.Join(revisionPath, "revision.txt")
		data, err = os.ReadFile(revisionFile)
		if err != nil {
			return ToolResult{Output: fmt.Sprintf("No revision manifest found at %s or %s", manifestPath, revisionFile), IsError: true}, nil
		}
	}

	// Format output
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Self-review of revision:\n\n"))
	sb.WriteString(string(data))
	sb.WriteString("\n\nReview complete. Check for alignment with user requirements, scope creep, and quality.")

	return ToolResult{Output: sb.String()}, nil
}
