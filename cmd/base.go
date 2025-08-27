package cmd

import (
	"log"

	"github.com/alantheprice/ledit/pkg/config"
	tuiPkg "github.com/alantheprice/ledit/pkg/tui"
	uiPkg "github.com/alantheprice/ledit/pkg/ui"
	"github.com/alantheprice/ledit/pkg/utils"
	"github.com/spf13/cobra"
)

// CommandConfig represents the common configuration shared across commands
type CommandConfig struct {
	SkipPrompt bool
	Model      string
	DryRun     bool
	Logger     *utils.Logger
	Config     *config.Config
}

// BaseCommand provides common functionality for all CLI commands
type BaseCommand struct {
	cmd   *cobra.Command
	cfg   *CommandConfig
	flags CommandFlags
}

// CommandFlags defines common flags used across commands
type CommandFlags struct {
	SkipPrompt *bool
	Model      *string
	DryRun     *bool
}

// NewBaseCommand creates a new base command with common functionality
func NewBaseCommand(use, short, long string) *BaseCommand {
	base := &BaseCommand{
		cmd: &cobra.Command{
			Use:   use,
			Short: short,
			Long:  long,
		},
		cfg: &CommandConfig{},
	}

	// Initialize common flags
	base.flags.SkipPrompt = base.cmd.Flags().Bool("skip-prompt", false, "Skip user confirmation prompts")
	base.flags.Model = base.cmd.Flags().StringP("model", "m", "", "LLM model to use")
	base.flags.DryRun = base.cmd.Flags().Bool("dry-run", false, "Run in simulation mode")

	return base
}

// GetCommand returns the underlying cobra command
func (b *BaseCommand) GetCommand() *cobra.Command {
	return b.cmd
}

// Initialize sets up common command infrastructure
func (b *BaseCommand) Initialize() error {
	// Load configuration
	cfg, err := config.LoadOrInitConfig(*b.flags.SkipPrompt)
	if err != nil {
		return err
	}

	// Override model if specified
	if *b.flags.Model != "" {
		cfg.EditingModel = *b.flags.Model
		cfg.OrchestrationModel = *b.flags.Model
		cfg.WorkspaceModel = *b.flags.Model
	}

	// Setup UI if enabled
	if uiPkg.IsUIActive() {
		uiPkg.SetDefaultSink(uiPkg.TuiSink{})
		go func() { _ = tuiPkg.Run() }()
	}

	// Create logger
	logger := utils.GetLogger(*b.flags.SkipPrompt)

	// Update command config
	b.cfg = &CommandConfig{
		SkipPrompt: *b.flags.SkipPrompt,
		Model:      *b.flags.Model,
		DryRun:     *b.flags.DryRun,
		Logger:     logger,
		Config:     cfg,
	}

	return nil
}

// AddCustomFlag adds a custom flag to the command
func (b *BaseCommand) AddCustomFlag(name, shorthand, defaultValue, description string) *string {
	return b.cmd.Flags().StringP(name, shorthand, defaultValue, description)
}

// SetRunFunc sets the command's run function with common initialization
func (b *BaseCommand) SetRunFunc(fn func(*CommandConfig, []string) error) {
	b.cmd.Run = func(cmd *cobra.Command, args []string) {
		if err := b.Initialize(); err != nil {
			log.Printf("Error: failed to initialize command: %v", err)
			return
		}

		if err := fn(b.cfg, args); err != nil {
			log.Printf("Error: command execution failed: %v", err)
		}
	}
}
