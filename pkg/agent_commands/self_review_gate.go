package commands

import (
	"fmt"
	"strings"

	"github.com/alantheprice/ledit/pkg/agent"
	"github.com/alantheprice/ledit/pkg/configuration"
)

// SelfReviewGateCommand implements the /self-review-gate slash command.
type SelfReviewGateCommand struct{}

func (c *SelfReviewGateCommand) Name() string {
	return "self-review-gate"
}

func (c *SelfReviewGateCommand) Description() string {
	return "Show or set self-review gate mode: off, code, always"
}

func (c *SelfReviewGateCommand) Execute(args []string, chatAgent *agent.Agent) error {
	if chatAgent == nil {
		return fmt.Errorf("agent is not initialized")
	}

	configManager := chatAgent.GetConfigManager()
	cfg := configManager.GetConfig()
	if cfg == nil {
		return fmt.Errorf("configuration is not initialized")
	}

	if len(args) == 0 {
		fmt.Printf("\nSelf-review gate mode: %s\n", cfg.GetSelfReviewGateMode())
		fmt.Println("Usage: /self-review-gate <off|code|always>")
		fmt.Println("  off: disable automatic self-review gate")
		fmt.Println("  code: run gate only when code/config files changed")
		fmt.Println("  always: run gate for any tracked change")
		return nil
	}
	if len(args) > 1 {
		return fmt.Errorf("usage: /self-review-gate <off|code|always>")
	}

	modeInput := strings.TrimSpace(args[0])
	if err := cfg.SetSelfReviewGateMode(modeInput); err != nil {
		return err
	}
	if err := configManager.SaveConfig(); err != nil {
		return fmt.Errorf("failed to save configuration: %w", err)
	}

	mode := cfg.GetSelfReviewGateMode()
	fmt.Printf("✅ Self-review gate mode set to: %s\n", mode)
	switch mode {
	case configuration.SelfReviewGateModeOff:
		fmt.Println("Automatic self-review gate is disabled.")
	case configuration.SelfReviewGateModeAlways:
		fmt.Println("Automatic self-review gate will run for any tracked changes.")
	default:
		fmt.Println("Automatic self-review gate will run only for code/config file changes.")
	}

	return nil
}
