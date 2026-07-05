package commands

import (
	"fmt"
	"os"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/console"
)

// InfoCommand implements the /info slash command — a one-shot overview
// of the agent's current state: model, provider, context, cost, persona,
// embedding index, and subagent config.
type InfoCommand struct{}

// Name returns the command name
func (c *InfoCommand) Name() string {
	return "info"
}

// Description returns the command description
func (c *InfoCommand) Description() string {
	return "Quick overview of live agent state (model, context, cost, persona)"
}

// Usage returns the detailed help text shown by `/help info`.
func (c *InfoCommand) Usage() string {
	return strings.Join([]string{
		"/info   Quick one-shot overview of live agent state.",
		"",
		"Shows model, provider, context tokens (used/limit/%), cost, workspace,",
		"persona, embedding index status, and subagent provider/model.",
		"Use /status for detailed runtime status or /setup for persisted config.",
		"",
		"Flags:",
		"  --json   Output the same data as a JSON object",
	}, "\n")
}

// Execute renders the agent state overview
func (c *InfoCommand) Execute(args []string, chatAgent *agent.Agent) error {
	if chatAgent == nil {
		fmt.Println(console.GlyphInfo.Prefix() + "No agent state available.")
		return nil
	}

	// Model & provider
	model := chatAgent.GetModel()
	provider := chatAgent.GetProvider()
	if model == "" {
		model = "(unknown)"
	}
	if provider == "" {
		provider = "(unknown)"
	}

	// Context tokens
	used, limit := chatAgent.GetContextTokens()
	pct := 0.0
	if limit > 0 {
		pct = float64(used) / float64(limit) * 100
	}

	// Cost
	totalCost := chatAgent.GetTotalCost()

	// Workspace
	workspace := chatAgent.GetWorkspaceRoot()
	if workspace == "" {
		workspace = "(none)"
	}

	// Persona
	persona := chatAgent.GetActivePersona()
	if persona == "" {
		persona = "none"
	}

	// Embeddings
	embeddingEnabled := chatAgent.IsEmbeddingIndexEnabled()
	embedCount := 0
	if mgr := chatAgent.GetEmbeddingManager(); mgr != nil {
		embedCount = mgr.IndexSize()
	}
	embedStatus := "disabled"
	if embeddingEnabled {
		embedStatus = "enabled"
	}

	// Subagent config
	cfg := chatAgent.GetConfig()
	subagentProvider := "(unknown)"
	subagentModel := "(unknown)"
	if cfg != nil {
		subagentProvider = cfg.GetSubagentProvider()
		subagentModel = cfg.GetSubagentModel()
	}

	fmt.Fprintln(os.Stdout)
	fmt.Fprintf(os.Stdout, "Agent: %s (%s)\n", model, provider)
	fmt.Fprintf(os.Stdout, "Context: %d/%d tokens (%.1f%%)\n", used, limit, pct)
	fmt.Fprintf(os.Stdout, "Cost: $%.6f\n", totalCost)
	fmt.Fprintf(os.Stdout, "Workspace: %s\n", workspace)
	fmt.Fprintf(os.Stdout, "Persona: %s\n", persona)
	fmt.Fprintf(os.Stdout, "Embeddings: %s (%d records)\n", embedStatus, embedCount)
	fmt.Fprintf(os.Stdout, "Subagent provider: %s model: %s\n", subagentProvider, subagentModel)
	fmt.Fprintln(os.Stdout)

	return nil
}

// infoJSONPayload is the JSON representation produced by /info --json.
type infoJSONPayload struct {
	Model            string  `json:"model"`
	Provider         string  `json:"provider"`
	ContextUsed      int     `json:"context_used"`
	ContextLimit     int     `json:"context_limit"`
	ContextPct       float64 `json:"context_pct"`
	Cost             float64 `json:"cost"`
	Workspace        string  `json:"workspace"`
	Persona          string  `json:"persona"`
	EmbeddingEnabled bool    `json:"embedding_enabled"`
	EmbeddingRecords int     `json:"embedding_records"`
	SubagentProvider string  `json:"subagent_provider"`
	SubagentModel    string  `json:"subagent_model"`
}

// ExecuteWithJSONOutput emits the agent state overview as JSON.
func (c *InfoCommand) ExecuteWithJSONOutput(args []string, chatAgent *agent.Agent, ctx *CommandContext) error {
	if chatAgent == nil {
		return WriteJSONToOutput(infoJSONPayload{})
	}

	model := chatAgent.GetModel()
	provider := chatAgent.GetProvider()
	if model == "" {
		model = "(unknown)"
	}
	if provider == "" {
		provider = "(unknown)"
	}

	used, limit := chatAgent.GetContextTokens()
	pct := 0.0
	if limit > 0 {
		pct = float64(used) / float64(limit) * 100
	}

	workspace := chatAgent.GetWorkspaceRoot()
	if workspace == "" {
		workspace = "(none)"
	}

	persona := chatAgent.GetActivePersona()
	if persona == "" {
		persona = "none"
	}

	embeddingEnabled := chatAgent.IsEmbeddingIndexEnabled()
	embedCount := 0
	if mgr := chatAgent.GetEmbeddingManager(); mgr != nil {
		embedCount = mgr.IndexSize()
	}

	subagentProvider := "(unknown)"
	subagentModel := "(unknown)"
	if cfg := chatAgent.GetConfig(); cfg != nil {
		subagentProvider = cfg.GetSubagentProvider()
		subagentModel = cfg.GetSubagentModel()
	}

	return WriteJSONToOutput(infoJSONPayload{
		Model:            model,
		Provider:         provider,
		ContextUsed:      used,
		ContextLimit:     limit,
		ContextPct:       pct,
		Cost:             chatAgent.GetTotalCost(),
		Workspace:        workspace,
		Persona:          persona,
		EmbeddingEnabled: embeddingEnabled,
		EmbeddingRecords: embedCount,
		SubagentProvider: subagentProvider,
		SubagentModel:    subagentModel,
	})
}
