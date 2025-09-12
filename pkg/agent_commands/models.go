package commands

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/alantheprice/ledit/pkg/agent"
	api "github.com/alantheprice/ledit/pkg/agent_api"
	"golang.org/x/term"
)

// ModelsCommand implements the /models slash command
type ModelsCommand struct{}

// Name returns the command name
func (m *ModelsCommand) Name() string {
	return "models"
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

	return fmt.Errorf("usage: /models [select|<model_id>]")
}

// listModels displays all available models for the current provider
func (m *ModelsCommand) listModels(chatAgent *agent.Agent) error {
	// Get current provider from agent, not environment
	clientType := chatAgent.GetProviderType()
	providerName := api.GetProviderName(clientType)

	fmt.Printf("\n📋 Available Models (%s):\n", providerName)
	fmt.Println("====================")

	models, err := api.GetModelsForProvider(clientType)
	if err != nil {
		return fmt.Errorf("failed to get available models: %w", err)
	}

	if len(models) == 0 {
		fmt.Printf("No models available for %s.\n", providerName)
		fmt.Println()
		fmt.Println("💡 Tip: Use '/provider select' to switch to a different provider")
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
				fmt.Printf("   🛠️  Supports tools: %s\n", strings.Join(model.Tags, ", "))
			} else {
				fmt.Printf("   Features: %s\n", strings.Join(model.Tags, ", "))
			}
		}
		fmt.Println()
	}

	// Display featured models section
	if len(featuredIndices) > 0 {
		fmt.Println("⭐ Featured Models (Popular & High Performance):")
		fmt.Println("================================================")
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
	fmt.Println("  /models select          - Interactive model selection (current provider)")
	fmt.Println("  /models <model_id>      - Set model directly")
	fmt.Println("  /models                 - Show this list")
	fmt.Println("  /provider select        - Switch providers first, then select models")

	return nil
}

// findFeaturedModels identifies indices of featured models using provider-specific featured models
func (m *ModelsCommand) findFeaturedModels(models []api.ModelInfo, clientType api.ClientType) []int {
	// Get provider-specific featured models
	featuredModelNames := api.GetFeaturedModelsForProvider(clientType)

	if len(featuredModelNames) == 0 {
		return []int{}
	}

	var featured []int
	featuredSet := make(map[string]bool)

	// Convert featured model names to set for O(1) lookup
	for _, name := range featuredModelNames {
		featuredSet[strings.ToLower(name)] = true
	}

	// Find matching models
	for i, model := range models {
		if featuredSet[strings.ToLower(model.ID)] {
			featured = append(featured, i)
		}
	}

	return featured
}

// selectModel allows interactive model selection from the current provider with search functionality
func (m *ModelsCommand) selectModel(chatAgent *agent.Agent) error {
	// Get current provider from agent, not environment
	clientType := chatAgent.GetProviderType()
	providerName := api.GetProviderName(clientType)

	models, err := api.GetModelsForProvider(clientType)
	if err != nil {
		return fmt.Errorf("failed to get available models: %w", err)
	}

	if len(models) == 0 {
		fmt.Printf("No models available for %s.\n", providerName)
		fmt.Println()
		fmt.Println("💡 Tip: Use '/provider select' to switch to a different provider with available models")
		return nil
	}

	// Sort models alphabetically by model ID
	sort.Slice(models, func(i, j int) bool {
		return models[i].ID < models[j].ID
	})

	// Identify featured models
	featuredIndices := m.findFeaturedModels(models, clientType)

	// Use interactive search selection
	return m.selectModelWithSearch(models, featuredIndices, clientType, providerName, chatAgent)
}

// selectModelWithSearch provides interactive model selection with shell-style autocomplete
func (m *ModelsCommand) selectModelWithSearch(models []api.ModelInfo, featuredIndices []int, clientType api.ClientType, providerName string, chatAgent *agent.Agent) error {
	fmt.Printf("\n🔍 Model Selection (%s) - Shell-style autocomplete:\n", providerName)
	fmt.Println("====================")
	fmt.Println("• Start typing to filter models (e.g., 'open', 'anthrop', 'deep')")
	fmt.Println("• System auto-completes common prefixes automatically")
	fmt.Println("• Press Enter when you see your desired model")
	fmt.Println("• Commands: 'quit' to exit, 'list' for all models")
	fmt.Printf("• %d total models available\n", len(models))
	fmt.Println()

	// Create a simple autocomplete interface
	return m.interactiveAutocomplete(models, featuredIndices, providerName, chatAgent)
}

// interactiveAutocomplete provides live search with arrow key navigation
func (m *ModelsCommand) interactiveAutocomplete(models []api.ModelInfo, featuredIndices []int, providerName string, chatAgent *agent.Agent) error {
	// Use a simpler live search interface for now
	return m.liveSearchInterface(models, featuredIndices, chatAgent)
}

// liveSearchInterface provides real-time filtering with arrow key navigation
func (m *ModelsCommand) liveSearchInterface(models []api.ModelInfo, featuredIndices []int, chatAgent *agent.Agent) error {
	// Check if we're in a terminal
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		// Fallback to line-based input for non-terminal environments
		return m.fallbackLineBasedInterface(models, featuredIndices, chatAgent)
	}

	// Save original terminal state
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return m.fallbackLineBasedInterface(models, featuredIndices, chatAgent)
	}
	defer term.Restore(int(os.Stdin.Fd()), oldState)

	currentInput := ""
	selectedIndex := 0
	buffer := make([]byte, 1)

	// Initial display
	m.displayRealTimeSearch(currentInput, models, featuredIndices, selectedIndex)

	for {
		// Read single character
		n, err := os.Stdin.Read(buffer)
		if err != nil || n == 0 {
			fmt.Printf("\nModel selection cancelled.\n")
			return nil
		}

		char := buffer[0]

		switch char {
		case 3: // Ctrl+C
			fmt.Printf("\nModel selection cancelled.\n")
			return nil
		case 13: // Enter
			matches := m.getCurrentMatches(currentInput, models)
			if len(matches) > selectedIndex {
				selectedModel := matches[selectedIndex]
				fmt.Printf("\n✅ Selected: %s\n", selectedModel.ID)
				return m.setModel(selectedModel.ID, chatAgent)
			}
		case 27: // Escape sequence (arrow keys)
			// Read the next two characters for arrow key detection
			seq := make([]byte, 2)
			os.Stdin.Read(seq)
			if seq[0] == 91 { // '['
				matches := m.getCurrentMatches(currentInput, models)
				switch seq[1] {
				case 65: // Up arrow
					if selectedIndex > 0 {
						selectedIndex--
					}
				case 66: // Down arrow
					if selectedIndex < len(matches)-1 {
						selectedIndex++
					}
				}
			}
		case 127, 8: // Backspace/Delete
			if len(currentInput) > 0 {
				currentInput = currentInput[:len(currentInput)-1]
				selectedIndex = 0 // Reset selection when search changes
			}
		default:
			// Regular character input
			if char >= 32 && char <= 126 { // Printable ASCII
				currentInput += string(char)
				selectedIndex = 0 // Reset selection when search changes
			}
		}

		// Update display
		m.displayRealTimeSearch(currentInput, models, featuredIndices, selectedIndex)
	}
}

// fallbackLineBasedInterface provides the old line-based interface for non-terminal environments
func (m *ModelsCommand) fallbackLineBasedInterface(models []api.ModelInfo, featuredIndices []int, chatAgent *agent.Agent) error {
	reader := bufio.NewReader(os.Stdin)
	currentInput := ""

	for {
		// Clear screen area and redraw
		m.clearAndRedraw(currentInput, models, featuredIndices)

		fmt.Printf("\nType to filter (current: '%s'): ", currentInput)

		// Read a line of input
		input, err := reader.ReadString('\n')
		if err != nil {
			fmt.Printf("\nModel selection cancelled.\n")
			return nil
		}

		input = strings.TrimSpace(input)

		// Handle special commands
		if input == "quit" || input == "exit" || input == "q" {
			fmt.Println("Model selection cancelled.")
			return nil
		}

		if input == "clear" || input == "c" {
			currentInput = ""
			continue
		}

		// Handle numbered selection from current filtered results
		if num, err := strconv.Atoi(input); err == nil {
			matches := m.getCurrentMatches(currentInput, models)
			if num >= 1 && num <= len(matches) {
				selectedModel := matches[num-1]
				fmt.Printf("✅ Selected: %s\n", selectedModel.ID)
				return m.setModel(selectedModel.ID, chatAgent)
			}
		}

		// If input is provided, replace current search
		if input != "" {
			currentInput = input
		}

		// Check for exact match
		if exactModel := m.findExactModel(models, currentInput); exactModel != nil {
			fmt.Printf("✅ Exact match found: %s\n", exactModel.ID)
			return m.setModel(exactModel.ID, chatAgent)
		}
	}
}

// clearAndRedraw clears the display area and shows current search results
func (m *ModelsCommand) clearAndRedraw(currentInput string, models []api.ModelInfo, featuredIndices []int) {
	// Clear screen (simple version)
	fmt.Print("\033[2J\033[H")

	fmt.Printf("🔍 Live Model Search - %d total models\n", len(models))
	fmt.Println("===========================================")
	fmt.Println("Commands: 'quit'/'q' to exit, 'clear'/'c' to clear search, 'list'/'l' for all")
	fmt.Println()

	if currentInput == "" {
		// Show featured models when no input
		if len(featuredIndices) > 0 {
			fmt.Println("⭐ Featured Models:")
			for i, idx := range featuredIndices {
				if i >= 5 {
					break // Show only first 5 featured
				}
				model := models[idx]
				fmt.Printf("%d. %s", i+1, model.ID)
				m.showModelInfo(model)
			}
			fmt.Printf("\n💡 Start typing to search all %d models...\n", len(models))
		}
	} else {
		// Show search results
		matches := m.getCurrentMatches(currentInput, models)

		if len(matches) == 0 {
			fmt.Printf("❌ No models found matching '%s'\n", currentInput)
			m.suggestAlternatives(models, currentInput)
		} else {
			fmt.Printf("🔍 %d matches for '%s':\n", len(matches), currentInput)

			maxShow := 10
			if len(matches) < maxShow {
				maxShow = len(matches)
			}

			for i := 0; i < maxShow; i++ {
				model := matches[i]
				fmt.Printf("%d. %s", i+1, model.ID)
				m.showModelInfo(model)
			}

			if len(matches) > maxShow {
				fmt.Printf("\n... and %d more matches (type number 1-%d to select, or continue typing to narrow down)\n", len(matches)-maxShow, maxShow)
			} else {
				fmt.Printf("\nType number 1-%d to select, or continue typing to refine search\n", len(matches))
			}
		}
	}
}

// getCurrentMatches gets the current filtered matches
func (m *ModelsCommand) getCurrentMatches(input string, models []api.ModelInfo) []api.ModelInfo {
	if input == "" {
		return models
	}
	return m.fuzzySearchModels(models, input)
}

// showModelInfo displays concise model information inline
func (m *ModelsCommand) showModelInfo(model api.ModelInfo) {
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

// showAllModels displays all available models in a compact format
func (m *ModelsCommand) showAllModels(models []api.ModelInfo, featuredIndices []int) {
	fmt.Println("\n📋 All Available Models:")
	fmt.Println("========================")

	// Show featured models first
	if len(featuredIndices) > 0 {
		fmt.Println("⭐ Featured Models:")
		for _, idx := range featuredIndices {
			model := models[idx]
			fmt.Printf("  %s", model.ID)
			m.showModelInfo(model)
		}
		fmt.Println()
	}

	fmt.Println("All Models:")
	for _, model := range models {
		fmt.Printf("  %s", model.ID)
		m.showModelInfo(model)
	}
	fmt.Println()
}

// findCommonPrefix finds the longest common prefix among matches that extends the current input
func (m *ModelsCommand) findCommonPrefix(matches []api.ModelInfo, input string) string {
	if len(matches) == 0 {
		return ""
	}

	// Find the longest common prefix among all matches
	prefix := matches[0].ID
	for _, match := range matches[1:] {
		prefix = m.commonPrefix(prefix, match.ID)
	}

	// Only return if it's meaningfully longer than current input
	if len(prefix) > len(input)+1 && strings.HasPrefix(strings.ToLower(prefix), strings.ToLower(input)) {
		// Find a good stopping point (like after a slash, dash, or word)
		goodStopChars := []rune{'/', '-', '_', '.'}
		bestPrefix := prefix

		for i := len(input); i < len(prefix); i++ {
			char := rune(prefix[i])
			for _, stopChar := range goodStopChars {
				if char == stopChar && i < len(prefix)-1 {
					// Found a good stopping point
					goodPrefix := prefix[:i+1]
					if len(goodPrefix) > len(input) {
						bestPrefix = goodPrefix
						break
					}
				}
			}
		}

		return bestPrefix
	}

	return ""
}

// commonPrefix returns the common prefix of two strings
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

// suggestAlternatives suggests similar models when no matches found
func (m *ModelsCommand) suggestAlternatives(models []api.ModelInfo, input string) {
	fmt.Println("💡 Did you mean:")

	// Show featured models as suggestions
	suggestions := []string{"gpt", "claude", "deepseek", "qwen", "gemini", "codestral"}
	for _, suggestion := range suggestions {
		if strings.ToLower(suggestion) != strings.ToLower(input) {
			matches := m.fuzzySearchModels(models, suggestion)
			if len(matches) > 0 {
				fmt.Printf("  '%s' (%d models)\n", suggestion, len(matches))
			}
		}
	}
}

// displayAllModels shows the full model list with selection
func (m *ModelsCommand) displayAllModels(models []api.ModelInfo, featuredIndices []int, chatAgent *agent.Agent) error {
	fmt.Println("\n📋 All Available Models:")
	fmt.Println("========================")

	// Show featured models first if available
	if len(featuredIndices) > 0 {
		fmt.Println("⭐ Featured Models:")
		for _, idx := range featuredIndices {
			model := models[idx]
			fmt.Printf("%d. %s", idx+1, model.ID)
			if model.InputCost > 0 && model.OutputCost > 0 {
				fmt.Printf(" - $%.3f/$%.3f per M tokens", model.InputCost, model.OutputCost)
			} else if model.Provider == "Ollama (Local)" {
				fmt.Printf(" - FREE")
			}
			fmt.Println()
		}
		fmt.Println()
	}

	// Show all models
	fmt.Println("All Models:")
	for i, model := range models {
		fmt.Printf("%d. %s\n", i+1, model.ID)
	}

	fmt.Print("\nEnter model number: ")
	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("error reading input: %w", err)
	}

	input = strings.TrimSpace(input)
	selection, err := strconv.Atoi(input)
	if err != nil || selection < 1 || selection > len(models) {
		return fmt.Errorf("invalid selection: must be a number between 1 and %d", len(models))
	}

	selectedModel := models[selection-1]
	return m.setModel(selectedModel.ID, chatAgent)
}

// findExactModel looks for exact model ID matches
func (m *ModelsCommand) findExactModel(models []api.ModelInfo, query string) *api.ModelInfo {
	query = strings.ToLower(query)
	for i := range models {
		if strings.ToLower(models[i].ID) == query {
			return &models[i]
		}
	}
	return nil
}

// fuzzySearchModels performs fuzzy search on model IDs and descriptions
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

	// Sort by score (higher is better)
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	// Return top matches
	var results []api.ModelInfo
	for _, candidate := range candidates {
		results = append(results, candidate.model)
	}

	// Limit to top 10 matches for usability
	if len(results) > 10 {
		results = results[:10]
	}

	return results
}

// calculateFuzzyScore calculates a fuzzy matching score for a model against a query
func (m *ModelsCommand) calculateFuzzyScore(model api.ModelInfo, query string) int {
	modelID := strings.ToLower(model.ID)
	description := strings.ToLower(model.Description)

	score := 0

	// Exact substring match in ID gets highest score
	if strings.Contains(modelID, query) {
		score += 100
		// Bonus if it's at the start
		if strings.HasPrefix(modelID, query) {
			score += 50
		}
	}

	// Partial matches in ID
	queryWords := strings.Fields(query)
	for _, word := range queryWords {
		if strings.Contains(modelID, word) {
			score += 30
		}
		if strings.Contains(description, word) {
			score += 10
		}
	}

	// Character similarity (simple version)
	for _, char := range query {
		if strings.ContainsRune(modelID, char) {
			score += 1
		}
	}

	return score
}

// setModel sets the specified model for the agent
func (m *ModelsCommand) setModel(modelID string, chatAgent *agent.Agent) error {
	// Let the agent handle provider determination and switching automatically
	err := chatAgent.SetModel(modelID)
	if err != nil {
		return fmt.Errorf("failed to set model: %w", err)
	}

	// Get the final provider and model info after successful switch
	finalProvider := chatAgent.GetProviderType()
	finalModel := chatAgent.GetModel()

	fmt.Printf("✅ Model set to: %s\n", finalModel)
	fmt.Printf("🏢 Provider: %s\n", api.GetProviderName(finalProvider))

	return nil
}

// displayRealTimeSearch shows the real-time search interface with highlighted selection
func (m *ModelsCommand) displayRealTimeSearch(currentInput string, models []api.ModelInfo, featuredIndices []int, selectedIndex int) {
	// Clear screen
	fmt.Print("\033[2J\033[H")

	fmt.Printf("🔍 Real-time Model Search - %d total models\n", len(models))
	fmt.Println("===========================================")
	fmt.Printf("Search: %s_\n", currentInput) // Show cursor with underscore
	fmt.Println("↑↓ Navigate, Enter to select, Ctrl+C to cancel, Backspace to delete")
	fmt.Println()

	matches := m.getCurrentMatches(currentInput, models)

	if len(matches) == 0 {
		fmt.Printf("❌ No models found matching '%s'\n", currentInput)
		if currentInput != "" {
			m.suggestAlternatives(models, currentInput)
		}
		return
	}

	fmt.Printf("📋 %d matches:\n", len(matches))
	fmt.Println()

	// Show up to 10 matches
	maxShow := 10
	if len(matches) < maxShow {
		maxShow = len(matches)
	}

	for i := 0; i < maxShow; i++ {
		model := matches[i]

		// Highlight selected item
		if i == selectedIndex {
			fmt.Printf("▶ ")
		} else {
			fmt.Printf("  ")
		}

		fmt.Printf("%s", model.ID)
		m.showModelInfo(model)
	}

	if len(matches) > maxShow {
		fmt.Printf("\n... and %d more matches (type to narrow down)\n", len(matches)-maxShow)
	}

	// Show instructions at bottom
	fmt.Println()
	if len(matches) > 0 && selectedIndex < len(matches) {
		fmt.Printf("Selected: %s", matches[selectedIndex].ID)
	}
}
