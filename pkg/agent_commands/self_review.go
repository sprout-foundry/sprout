package commands

import (
	"fmt"
	"strings"

	"github.com/alantheprice/ledit/pkg/agent"
	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/alantheprice/ledit/pkg/spec"
	"github.com/alantheprice/ledit/pkg/utils"
)

// SelfReviewCommand implements the /self-review slash command.
type SelfReviewCommand struct{}

func (c *SelfReviewCommand) Name() string {
	return "self-review"
}

func (c *SelfReviewCommand) Description() string {
	return "Run canonical-spec scope validation against the current or specified revision"
}

func (c *SelfReviewCommand) Execute(args []string, chatAgent *agent.Agent) error {
	if chatAgent == nil {
		return fmt.Errorf("agent is not initialized")
	}

	revisionID := ""
	if len(args) > 0 {
		revisionID = strings.TrimSpace(args[0])
	}
	if revisionID == "" {
		revisionID = strings.TrimSpace(chatAgent.GetRevisionID())
	}

	cfg := chatAgent.GetConfigManager().GetConfig()
	if cfg == nil {
		var err error
		cfg, err = configuration.Load()
		if err != nil {
			return fmt.Errorf("failed to load configuration: %w", err)
		}
	}

	logger := utils.GetLogger(true)
	result, err := spec.ReviewTrackedChanges(revisionID, cfg, logger)
	if err != nil {
		return fmt.Errorf("self-review failed: %w", err)
	}

	fmt.Print("\n## Self-Review\n")
	fmt.Printf("Revision: %s\n", result.RevisionID)
	if result.SpecResult != nil {
		fmt.Printf("Spec confidence: %.0f%%\n", result.SpecResult.Confidence*100)
	}
	if result.ScopeResult != nil && result.ScopeResult.InScope {
		fmt.Println("Scope status: IN_SCOPE")
	} else {
		fmt.Println("Scope status: OUT_OF_SCOPE")
		if result.ScopeResult != nil {
			fmt.Printf("Summary: %s\n", result.ScopeResult.Summary)
		}
	}
	fmt.Printf("Files changed: %d | Total changes: %d\n\n", result.FilesChanged, result.TotalChanges)

	return nil
}
