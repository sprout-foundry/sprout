package commands

import (
	"fmt"
	"sort"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/console"
)

// SetupCommand implements the /setup slash command, which displays a
// human-readable summary of the current Sprout configuration.
type SetupCommand struct{}

// Name returns the command name.
func (s *SetupCommand) Name() string { return "setup" }

// Description returns the command description.
func (s *SetupCommand) Description() string {
	return "Show current configuration summary with status and warnings"
}

// Execute runs the /setup command.
func (s *SetupCommand) Execute(args []string, chatAgent *agent.Agent) error {
	mgr := chatAgent.GetConfigManager()
	if mgr == nil {
		return fmt.Errorf("configuration manager not available")
	}

	cfg := mgr.GetConfig()
	if cfg == nil {
		return fmt.Errorf("configuration not loaded")
	}

	fmt.Println()
	console.GlyphInfo.Print("Current Configuration")
	fmt.Println()

	// Provider & Model
	s.printSection("Provider & Model", []keyValue{
		{"Provider", s.providerDisplay(cfg)},
		{"Model", s.modelDisplay(cfg, chatAgent)},
		{"Reasoning Effort", s.nonEmpty(cfg.ReasoningEffort, "(auto)")},
		{"Disable Thinking", fmt.Sprintf("%v", cfg.DisableThinking)},
	})

	// Subagents
	s.printSection("Subagents", []keyValue{
		{"Provider", s.nonEmpty(cfg.SubagentProvider, "(inherits provider)")},
		{"Model", s.nonEmpty(cfg.SubagentModel, "(inherits provider default)")},
		{"Max Parallel", fmt.Sprintf("%d", cfg.GetSubagentMaxParallel())},
		{"Parallel Enabled", fmt.Sprintf("%v", cfg.GetSubagentParallelEnabled())},
		{"Max Depth", fmt.Sprintf("%d", cfg.GetSubagentMaxDepth())},
	})

	// Commit & Review
	s.printSection("Commit & Review", []keyValue{
		{"Commit Provider", s.nonEmpty(cfg.CommitProvider, "(inherits provider)")},
		{"Commit Model", s.nonEmpty(cfg.CommitModel, "(inherits provider default)")},
		{"Review Provider", s.nonEmpty(cfg.ReviewProvider, "(inherits provider)")},
		{"Review Model", s.nonEmpty(cfg.ReviewModel, "(inherits provider default)")},
		{"Self-Review Gate", s.nonEmpty(cfg.SelfReviewGateMode, "off")},
	})

	// Security
	s.printSection("Security", []keyValue{
		{"Risk Profile", s.nonEmpty(cfg.RiskProfile, "default")},
	})

	// Skills
	s.printSkills(cfg)

	// MCP
	mcpCount := len(cfg.MCP.Servers)
	s.printSection("MCP", []keyValue{
		{"Servers Configured", fmt.Sprintf("%d", mcpCount)},
	})

	// Embedding
	if cfg.EmbeddingIndex != nil {
		s.printSection("Embedding Index", []keyValue{
			{"Enabled", fmt.Sprintf("%v", cfg.EmbeddingIndex.Enabled)},
		})
	}

	// Warnings
	s.printWarnings(cfg, mgr, chatAgent)

	fmt.Println()
	return nil
}

type keyValue struct {
	key   string
	value string
}

func (s *SetupCommand) printSection(title string, rows []keyValue) {
	console.GlyphDim.Printf("── %s ", title)
	fmt.Println()
	for _, row := range rows {
		fmt.Printf("  %-22s %s\n", row.key+":", row.value)
	}
	fmt.Println()
}

func (s *SetupCommand) printSkills(cfg *configuration.Config) {
	if len(cfg.Skills) == 0 {
		s.printSection("Skills", []keyValue{{"Active", "none"}})
		return
	}

	var enabled, disabled []string
	for id, skill := range cfg.Skills {
		if skill.Enabled {
			enabled = append(enabled, id)
		} else {
			disabled = append(disabled, id)
		}
	}
	sort.Strings(enabled)
	sort.Strings(disabled)

	rows := []keyValue{
		{"Active", fmt.Sprintf("%d", len(enabled))},
		{"List", strings.Join(enabled, ", ")},
	}
	if len(disabled) > 0 {
		rows = append(rows, keyValue{"Disabled", strings.Join(disabled, ", ")})
	}
	s.printSection("Skills", rows)
}

func (s *SetupCommand) printWarnings(cfg *configuration.Config, mgr *configuration.Manager, chatAgent *agent.Agent) {
	var warnings []string

	// Check for missing credentials on active provider
	if cfg.LastUsedProvider != "" && !configuration.HasProviderAuth(strings.ToLower(cfg.LastUsedProvider)) {
		// Check if it's a provider that doesn't need keys (e.g., ollama-local)
		pt := chatAgent.GetProviderType()
		if string(pt) != "ollama-local" {
			warnings = append(warnings, fmt.Sprintf("No credentials for active provider %q", cfg.LastUsedProvider))
		}
	}

	// Check subagent provider credentials
	if cfg.SubagentProvider != "" && !configuration.HasProviderAuth(strings.ToLower(cfg.SubagentProvider)) {
		warnings = append(warnings, fmt.Sprintf("No credentials for subagent provider %q", cfg.SubagentProvider))
	}

	if len(warnings) == 0 {
		console.GlyphSuccess.Print("No configuration warnings")
		fmt.Println()
		return
	}

	fmt.Println()
	console.GlyphWarning.Print("Warnings")
	fmt.Println()
	for _, w := range warnings {
		fmt.Printf("  ⚠  %s\n", w)
	}
}

func (s *SetupCommand) providerDisplay(cfg *configuration.Config) string {
	if cfg.LastUsedProvider == "" {
		return "(not set)"
	}
	return cfg.LastUsedProvider
}

func (s *SetupCommand) modelDisplay(cfg *configuration.Config, chatAgent *agent.Agent) string {
	model := chatAgent.GetModel()
	if model == "" {
		return "(not set)"
	}
	return model
}

func (s *SetupCommand) nonEmpty(val, fallback string) string {
	if val == "" {
		return fallback
	}
	return val
}
