package commands

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/console"
)

// ModelsCommand implements the /model slash command
type ModelsCommand struct{}

// Name returns the command name
func (m *ModelsCommand) Name() string {
	return "model"
}

// Description returns the command description
func (m *ModelsCommand) Description() string {
	return "List available models and select which model to use"
}

// Execute runs the models command
func (m *ModelsCommand) Execute(args []string, chatAgent *agent.Agent) error {
	// If no arguments, list available models
	if len(args) == 0 {
		return m.listModels(chatAgent)
	}

	// If arguments provided, handle model selection
	if len(args) == 1 {
		if args[0] == "select" {
			return m.selectModel(chatAgent)
		} else {
			// Direct model selection by ID
			return m.setModel(args[0], chatAgent)
		}
	}

	return errors.New("usage: /model [select|<model_id>]")
}

// listModels displays all available models for the current provider
func (m *ModelsCommand) listModels(chatAgent *agent.Agent) error {
	// Get current provider from agent, not environment
	clientType := chatAgent.GetProviderType()
	providerName := api.GetProviderName(clientType)

	fmt.Println()
	console.GlyphInfo.Printf("Available Models (%s):", providerName)

	models, err := api.GetModelsForProvider(clientType)
	if err != nil {
		return fmt.Errorf("failed to get available models: %w", err)
	}

	if len(models) == 0 {
		fmt.Printf("No models available for %s.\n", providerName)
		fmt.Println()
		console.GlyphInfo.Print("Tip: Use '/provider select' to switch to a different provider")
		return nil
	}

	// Sort models alphabetically by model ID
	sort.Slice(models, func(i, j int) bool {
		return models[i].ID < models[j].ID
	})

	// Identify featured models
	featuredIndices := m.findFeaturedModels(models, clientType)

	// Display all models
	for i, model := range models {
		fmt.Printf("%d. %s\n", i+1, model.ID)
		if model.Description != "" {
			fmt.Printf("   Description: %s\n", model.Description)
		}
		if model.Size != "" {
			fmt.Printf("   Size: %s\n", model.Size)
		}
		if model.InputCost > 0 || model.OutputCost > 0 {
			if model.InputCost > 0 && model.OutputCost > 0 {
				fmt.Printf("   Cost: $%.3f/M input, $%.3f/M output tokens\n", model.InputCost, model.OutputCost)
			} else if model.Cost > 0 {
				// Fallback to legacy format
				fmt.Printf("   Cost: ~$%.2f/M tokens\n", model.Cost)
			}
		} else if model.Provider == "Ollama (Local)" {
			fmt.Printf("   Cost: FREE (local)\n")
		} else {
			fmt.Printf("   Cost: N/A\n")
		}
		if model.ContextLength > 0 {
			fmt.Printf("   Context: %d tokens\n", model.ContextLength)
		}
		if len(model.EligibleRoles) > 0 {
			fmt.Printf("   Eligible for: %s\n", strings.Join(model.EligibleRoles, ", "))
		}
		if len(model.RecommendedRoles) > 0 {
			fmt.Printf("   %sRecommended for: %s (passed capability probe)\n",
				console.GlyphSuccess.Prefix(), strings.Join(model.RecommendedRoles, ", "))
		}
		for _, w := range model.Warnings {
			fmt.Printf("   %s%s\n", console.GlyphWarning.Prefix(), w)
		}
		if len(model.Tags) > 0 {
			// Highlight tool support
			hasTools := false
			for _, tag := range model.Tags {
				if tag == "tools" || tag == "tool_choice" {
					hasTools = true
					break
				}
			}
			if hasTools {
				fmt.Printf("   %sSupports tools: %s\n", console.GlyphSuccess.Prefix(), strings.Join(model.Tags, ", "))
			} else {
				fmt.Printf("   Features: %s\n", strings.Join(model.Tags, ", "))
			}
		}
		fmt.Println()
	}

	// Display featured models section
	if len(featuredIndices) > 0 {
		console.GlyphAction.Print("Featured Models (Popular & High Performance):")
		for _, idx := range featuredIndices {
			model := models[idx]
			fmt.Printf("%d. %s", idx+1, model.ID)
			if model.InputCost > 0 && model.OutputCost > 0 {
				fmt.Printf(" - $%.3f/$%.3f per M tokens", model.InputCost, model.OutputCost)
			} else if model.Cost > 0 {
				fmt.Printf(" - ~$%.2f/M tokens", model.Cost)
			} else if model.Provider == "Ollama (Local)" {
				fmt.Printf(" - FREE")
			} else {
				fmt.Printf(" - N/A")
			}
			if model.ContextLength > 0 {
				fmt.Printf(" - %dK context", model.ContextLength/1000)
			}
			fmt.Println()
		}
		fmt.Println()
	}

	fmt.Println("Usage:")
	fmt.Println("  /model select          - Interactive model selection (current provider)")
	fmt.Println("  /model <model_id>      - Set model directly")
	fmt.Println("  /model                 - Show this list")
	fmt.Println("  /provider select        - Switch providers first, then select models")

	return nil
}

// findFeaturedModels identifies indices of featured models
// Now that we've removed the featured models concept, this returns an empty list
func (m *ModelsCommand) findFeaturedModels(models []api.ModelInfo, clientType api.ClientType) []int {
	// Featured models concept has been removed - all models are treated equally
	return []int{}
}

// selectModel drives the interactive model picker. Uses the unified
// SelectList primitive with Searchable=true so users can type to
// filter and arrow-key through matches without re-typing — the prior
// line-based fallback required typing 'clear' between searches and
// truncated OpenRouter's 200+ models to 10 visible (SP-057 Phase 4).
func (m *ModelsCommand) selectModel(chatAgent *agent.Agent) error {
	clientType := chatAgent.GetProviderType()
	providerName := api.GetProviderName(clientType)

	models, err := api.GetModelsForProvider(clientType)
	if err != nil {
		return fmt.Errorf("failed to get available models: %w", err)
	}

	if len(models) == 0 {
		fmt.Printf("No models available for %s.\n", providerName)
		fmt.Println()
		console.GlyphInfo.Print("Tip: Use '/provider select' to switch to a different provider with available models")
		return nil
	}

	// Sort models alphabetically by model ID so the picker has a
	// stable order across runs.
	sort.Slice(models, func(i, j int) bool {
		return models[i].ID < models[j].ID
	})

	current := chatAgent.GetModel()
	items := make([]console.SelectItem, 0, len(models))
	for _, model := range models {
		detail := modelDetailString(model)
		if model.ID == current {
			detail = "current · " + detail
		}
		items = append(items, console.SelectItem{
			Label:  model.ID,
			Detail: detail,
			Value:  model.ID,
		})
	}

	picker := console.NewSelectList(console.SelectListOptions{
		Title:      fmt.Sprintf("Select model (%s)", providerName),
		Items:      items,
		Searchable: true,
		PageSize:   12,
	})
	chosen, ok, err := picker.Run(context.Background())
	if err != nil {
		return fmt.Errorf("model picker: %w", err)
	}
	if !ok || chosen == "" {
		fmt.Println("Model selection cancelled.")
		return nil
	}
	return m.setModel(chosen, chatAgent)
}

// modelDetailString renders the right-aligned detail column for the
// model picker: pricing tier + context length when available. Falls
// back to "FREE" for local Ollama and "N/A" when nothing is known.
func modelDetailString(model api.ModelInfo) string {
	parts := []string{}

	// Pricing
	switch {
	case model.InputCost > 0 && model.OutputCost > 0:
		parts = append(parts, fmt.Sprintf("$%.2f/$%.2f", model.InputCost, model.OutputCost))
	case model.Cost > 0:
		parts = append(parts, fmt.Sprintf("$%.2f/M", model.Cost))
	case strings.Contains(model.Provider, "Ollama"):
		parts = append(parts, "FREE")
	}

	// Context length
	if model.ContextLength > 0 {
		parts = append(parts, fmt.Sprintf("%dK", model.ContextLength/1000))
	}

	// Agentic role: probe-backed recommendation (★) wins over the
	// deterministic eligibility pre-filter; a warning (e.g. small context)
	// shows a ⚠ marker (full text appears in the `/model` list).
	if len(model.RecommendedRoles) > 0 {
		parts = append(parts, "★"+strings.Join(model.RecommendedRoles, "+"))
	} else if len(model.EligibleRoles) > 0 {
		parts = append(parts, strings.Join(model.EligibleRoles, "+"))
	}
	if len(model.Warnings) > 0 {
		parts = append(parts, "⚠")
	}

	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " · ")
}

// Legacy line-based / arrow-key model search interfaces removed in
// SP-057 Phase 4. The unified SelectList picker (pkg/console/select_list.go)
// handles both TTY and non-TTY paths, type-to-filter, and arrow-key
// navigation with no row cap. See selectModel above for the new flow.

// getCurrentMatches returns the input-filtered subset of models. Kept
// as a utility (used by the test suite and potentially by future
// pickers that want richer scoring than SelectList's substring filter).
func (m *ModelsCommand) getCurrentMatches(input string, models []api.ModelInfo) []api.ModelInfo {
	if input == "" {
		return models
	}
	return m.fuzzySearchModels(models, input)
}

// findExactModel looks for exact (case-insensitive) model ID matches.
func (m *ModelsCommand) findExactModel(models []api.ModelInfo, query string) *api.ModelInfo {
	query = strings.ToLower(query)
	for i := range models {
		if strings.ToLower(models[i].ID) == query {
			return &models[i]
		}
	}
	return nil
}

// fuzzySearchModels performs fuzzy search on model IDs and descriptions.
// Returns up to 10 ranked matches.
func (m *ModelsCommand) fuzzySearchModels(models []api.ModelInfo, query string) []api.ModelInfo {
	query = strings.ToLower(query)

	type scoredModel struct {
		model api.ModelInfo
		score int
	}

	var candidates []scoredModel
	for _, model := range models {
		score := m.calculateFuzzyScore(model, query)
		if score > 0 {
			candidates = append(candidates, scoredModel{model: model, score: score})
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	var results []api.ModelInfo
	for _, candidate := range candidates {
		results = append(results, candidate.model)
	}
	if len(results) > 10 {
		results = results[:10]
	}
	return results
}

// calculateFuzzyScore scores a model against a query. Higher = better
// match. Substring match in ID is worth 100; prefix match adds 50;
// provider/model split queries get 80; per-word matches in
// ID/description add smaller increments.
func (m *ModelsCommand) calculateFuzzyScore(model api.ModelInfo, query string) int {
	modelID := strings.ToLower(model.ID)
	description := strings.ToLower(model.Description)

	score := 0
	if strings.Contains(modelID, query) {
		score += 100
		if strings.HasPrefix(modelID, query) {
			score += 50
		}
	}
	if strings.Contains(query, "/") {
		parts := strings.Split(query, "/")
		if len(parts) == 2 {
			provider := parts[0]
			modelPart := parts[1]
			if strings.Contains(modelID, provider) && strings.Contains(modelID, modelPart) {
				score += 80
			}
		}
	} else {
		queryWords := strings.Fields(query)
		for _, word := range queryWords {
			if len(word) >= 3 {
				if strings.Contains(modelID, word) {
					score += 30
				}
				if strings.Contains(description, word) {
					score += 10
				}
			}
		}
	}
	return score
}

// commonPrefix returns the case-insensitive common prefix of two strings.
func (m *ModelsCommand) commonPrefix(a, b string) string {
	minLen := len(a)
	if len(b) < minLen {
		minLen = len(b)
	}
	for i := 0; i < minLen; i++ {
		if strings.ToLower(string(a[i])) != strings.ToLower(string(b[i])) {
			return a[:i]
		}
	}
	return a[:minLen]
}

// findCommonPrefix returns the longest common prefix among the matches
// that extends the current input. Returns "" when no useful prefix
// exists. Used by autocomplete-style tests.
func (m *ModelsCommand) findCommonPrefix(matches []api.ModelInfo, input string) string {
	if len(matches) == 0 {
		return ""
	}
	prefix := matches[0].ID
	for _, match := range matches[1:] {
		prefix = m.commonPrefix(prefix, match.ID)
	}
	if len(prefix) > len(input)+1 && strings.HasPrefix(strings.ToLower(prefix), strings.ToLower(input)) {
		goodStopChars := []rune{'/', '-', '_', '.'}
		bestPrefix := prefix
		for i := len(input); i < len(prefix); i++ {
			ch := rune(prefix[i])
			for _, stop := range goodStopChars {
				if ch == stop && i < len(prefix)-1 {
					good := prefix[:i+1]
					if len(good) > len(input) {
						bestPrefix = good
						break
					}
				}
			}
		}
		return bestPrefix
	}
	return ""
}

// setModel sets the specified model for the agent (persisted for CLI use)
func (m *ModelsCommand) setModel(modelID string, chatAgent *agent.Agent) error {
	// Let the agent handle provider determination and switching automatically
	err := chatAgent.SetModelPersisted(modelID)
	if err != nil {
		return fmt.Errorf("failed to set model: %w", err)
	}

	// Get the final provider and model info after successful switch
	finalProvider := chatAgent.GetProviderType()
	finalModel := chatAgent.GetModel()

	fmt.Printf("Model set to: %s\n", finalModel)
	fmt.Printf("Provider: %s\n", api.GetProviderName(finalProvider))
	if note := chatAgent.ConsumePendingStrictSwitchNotice(); note != "" {
		fmt.Printf("\n%s\n", note)
	}

	// Publish model info event for UI
	agent.PublishModel(finalModel)

	return nil
}
