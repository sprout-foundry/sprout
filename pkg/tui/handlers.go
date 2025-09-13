package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	agent_api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/ui"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// handlePromptKey handles keyboard input during prompts
func (m Model) handlePromptKey(msg tea.KeyMsg) Model {
	if m.state.PromptYesNo {
		// Single-key quick responses
		switch msg.String() {
		case "y", "Y":
			ui.SubmitPromptResponse(m.state.PromptID, "yes", true)
			m.state.AwaitingPrompt = false
			m.state.PromptInput = ""
			return m
		case "n", "N":
			ui.SubmitPromptResponse(m.state.PromptID, "no", false)
			m.state.AwaitingPrompt = false
			m.state.PromptInput = ""
			return m
		case "enter":
			ui.SubmitPromptResponse(m.state.PromptID, "", m.state.PromptDefault)
			m.state.AwaitingPrompt = false
			m.state.PromptInput = ""
			return m
		case "esc":
			ui.SubmitPromptResponse(m.state.PromptID, "", m.state.PromptDefault)
			m.state.AwaitingPrompt = false
			m.state.PromptInput = ""
			return m
		}
	}
	return m
}

// handleInteractiveKey handles keyboard input in interactive mode
func (m Model) handleInteractiveKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up":
		// Navigate up in command history
		if len(m.state.CommandHistory) > 0 {
			if m.state.HistoryIndex == -1 {
				m.state.OriginalInput = m.state.TextInput.Value()
				m.state.HistoryIndex = len(m.state.CommandHistory) - 1
			} else if m.state.HistoryIndex > 0 {
				m.state.HistoryIndex--
			}

			if m.state.HistoryIndex >= 0 && m.state.HistoryIndex < len(m.state.CommandHistory) {
				m.state.TextInput.SetValue(m.state.CommandHistory[m.state.HistoryIndex])
				m.state.TextInput.SetCursor(len(m.state.CommandHistory[m.state.HistoryIndex]))
			}
		}
		return m, nil

	case "down":
		// Navigate down in command history
		if len(m.state.CommandHistory) > 0 && m.state.HistoryIndex >= 0 {
			m.state.HistoryIndex++

			if m.state.HistoryIndex >= len(m.state.CommandHistory) {
				m.state.TextInput.SetValue(m.state.OriginalInput)
				m.state.TextInput.SetCursor(len(m.state.OriginalInput))
				m.state.HistoryIndex = -1
				m.state.OriginalInput = ""
			} else {
				m.state.TextInput.SetValue(m.state.CommandHistory[m.state.HistoryIndex])
				m.state.TextInput.SetCursor(len(m.state.CommandHistory[m.state.HistoryIndex]))
			}
		}
		return m, nil

	case "enter":
		input := m.state.TextInput.Value()

		// Check for backslash continuation
		if strings.HasSuffix(strings.TrimSpace(input), "\\") {
			trimmed := strings.TrimSpace(input)
			newValue := trimmed[:len(trimmed)-1] + "\n"
			m.state.TextInput.SetValue(newValue)
			m.state.TextInput.SetCursor(len(newValue))
			ui.Log("↩️  Continue on next line...")
			return m, nil
		}

		input = strings.TrimSpace(input)
		if input != "" {
			// Check for slash commands
			if strings.HasPrefix(input, "/") && !strings.Contains(input, "\n") {
				// Create a new model with cleared suggestions
				newModel := m
				newModel.state.ShowCommandSuggestions = false
				newModel.state.CommandSuggestions = nil
				newModel.state.JustExecutedCommand = true
				newModel.state.TextInput.SetValue("")

				if input == "/" {
					// Show command dropdown
					newModel.showCommandDropdown()
				} else {
					// Execute the command
					newModel.handleSlashCommand(input)
				}
				return newModel, nil
			}

			// Process file references and execute
			processedInput := m.processFileReferences(input)
			go m.executeAgentRequest(processedInput)

			ui.Logf("🎯 Executing: %s", strings.ReplaceAll(processedInput, "\n", " ↩️ "))

			// Add to history
			m.state.CommandHistory = append(m.state.CommandHistory, input)
			if len(m.state.CommandHistory) > MaxCommandHistory {
				m.state.CommandHistory = m.state.CommandHistory[len(m.state.CommandHistory)-MaxCommandHistory:]
			}

			// Reset
			m.state.HistoryIndex = -1
			m.state.OriginalInput = ""
			m.state.TextInput.SetValue("")
			// Clear command suggestions
			m.state.ShowCommandSuggestions = false
			m.state.CommandSuggestions = []string{}
		}
		return m, nil

	case "esc":
		m.state.TextInput.SetValue("")
		m.state.TextInput.SetCursor(0)
		m.state.HistoryIndex = -1
		m.state.OriginalInput = ""
		return m, nil

	case "ctrl+c":
		return m, tea.Quit

	case "tab":
		// Pass through to text input
		var cmd tea.Cmd
		m.state.TextInput, cmd = m.state.TextInput.Update(msg)
		return m, cmd

	default:
		// Reset history navigation when typing
		if m.state.HistoryIndex != -1 {
			m.state.HistoryIndex = -1
			m.state.OriginalInput = ""
		}

		// Update text input first
		var cmd tea.Cmd
		m.state.TextInput, cmd = m.state.TextInput.Update(msg)

		// Get current input value
		currentValue := m.state.TextInput.Value()

		// Check if we should show suggestions
		// Only show when exactly "/" is typed and we're not in the middle of executing
		if currentValue == "/" && m.state.CommandRegistry != nil && !m.state.JustExecutedCommand {
			// Show suggestions
			m.state.ShowCommandSuggestions = true
			m.state.CommandSuggestions = []string{}
			for _, c := range m.state.CommandRegistry.ListCommands() {
				m.state.CommandSuggestions = append(m.state.CommandSuggestions,
					fmt.Sprintf("/%s - %s", c.Name(), c.Description()))
			}
			// Add TUI-specific commands
			m.state.CommandSuggestions = append(m.state.CommandSuggestions,
				"/logs - Toggle logs view",
				"/clear - Clear logs",
				"/history - Show command history",
			)
		} else if currentValue == "" || !strings.HasPrefix(currentValue, "/") {
			// Clear suggestions when input is empty or doesn't start with "/"
			m.state.ShowCommandSuggestions = false
			m.state.CommandSuggestions = nil
			m.state.JustExecutedCommand = false
		}

		return m, cmd
	}
}

// handleGeneralKey handles general keyboard input
func (m Model) handleGeneralKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "Q":
		if !m.state.InteractiveMode || !m.state.FocusedInput {
			return m, tea.Quit
		}
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		if !m.state.InteractiveMode || !m.state.FocusedInput {
			return m, tea.Quit
		}
	case "l", "L":
		m.state.LogsCollapsed = !m.state.LogsCollapsed
		return m, nil
	case "p", "P":
		m.state.ProgressCollapsed = !m.state.ProgressCollapsed
		return m, nil
	case "ctrl+l":
		m.state.Logs = m.state.Logs[:0]
		m.state.LogsViewport.SetContent("")
		return m, nil
	case "i":
		if m.state.InteractiveMode && !m.state.FocusedInput {
			m.state.FocusedInput = true
			m.state.TextInput.Focus()
			return m, nil
		}
	}

	// Pass through to viewport
	if !m.state.LogsCollapsed {
		var cmd tea.Cmd
		m.state.LogsViewport, cmd = m.state.LogsViewport.Update(msg)
		return m, cmd
	}
	return m, nil
}

// handleSlashCommand handles slash commands
func (m *Model) handleSlashCommand(input string) {
	// Clear suggestions immediately and persistently
	m.state.ShowCommandSuggestions = false
	m.state.CommandSuggestions = nil
	m.state.TextInput.SetValue("") // Ensure input is cleared

	ui.Logf("🎮 handleSlashCommand called with: %s", input)

	if m.state.Agent == nil || m.state.CommandRegistry == nil {
		ui.Log("❌ Agent not initialized")
		return
	}

	// Handle special cases
	switch input {
	case "/models":
		m.showModelDropdown()
		return

	case "/clear", "/c":
		m.state.Logs = m.state.Logs[:0]
		m.state.LogsViewport.SetContent("")
		ui.Log("📋 Logs cleared")
		return

	case "/logs", "/l":
		m.state.LogsCollapsed = !m.state.LogsCollapsed
		ui.Logf("📋 Logs %s", func() string {
			if m.state.LogsCollapsed {
				return "collapsed"
			}
			return "expanded"
		}())
		return

	case "/history", "/hist":
		if len(m.state.CommandHistory) == 0 {
			ui.Log("📋 No command history yet")
		} else {
			historyText := "📋 Command History (recent first):\n"
			start := len(m.state.CommandHistory) - 10
			if start < 0 {
				start = 0
			}
			for i := len(m.state.CommandHistory) - 1; i >= start; i-- {
				historyText += fmt.Sprintf("  %d. %s\n", len(m.state.CommandHistory)-i, m.state.CommandHistory[i])
			}
			ui.Log(strings.TrimSuffix(historyText, "\n"))
		}
		return

	case "/quit", "/exit", "/q":
		ui.Log("👋 Goodbye!")
		// Exit will be handled by the caller
		return
	}

	// Use CommandRegistry for other commands
	ui.Logf("🔍 Executing command: %s", input)
	err := m.state.CommandRegistry.Execute(input, m.state.Agent)
	if err != nil {
		ui.Logf("❌ Command error: %v", err)
		ui.Logf("💡 Type '/help' to see available commands")
	} else {
		ui.Logf("✅ Command completed: %s", input)
		// Update model/provider info in case it changed
		if m.state.Agent != nil {
			m.state.BaseModel = m.state.Agent.GetModel()
			clientType := m.state.Agent.GetProviderType()
			m.state.Provider = agent_api.GetProviderName(clientType)
		}
	}
}

// showCommandDropdown shows the command selection dropdown
func (m *Model) showCommandDropdown() {
	if m.state.CommandRegistry == nil {
		return
	}

	items := []dropdownItem{}
	for _, cmd := range m.state.CommandRegistry.ListCommands() {
		items = append(items, dropdownItem{
			title:       fmt.Sprintf("/%s", cmd.Name()),
			description: cmd.Description(),
			value:       "/" + cmd.Name(),
		})
	}

	// Add TUI-specific commands
	items = append(items,
		dropdownItem{title: "/logs", description: "Toggle logs view", value: "/logs"},
		dropdownItem{title: "/clear", description: "Clear logs", value: "/clear"},
		dropdownItem{title: "/history", description: "Show command history", value: "/history"},
		dropdownItem{title: "/quit", description: "Exit the TUI", value: "/quit"},
	)

	m.dropdown = NewInlineDropdown("Select a command:", items)
	m.dropdownMode = DropdownCommands
}

// showModelDropdown shows the model selection dropdown
func (m *Model) showModelDropdown() {
	if m.state.Agent == nil {
		ui.Log("❌ Agent not initialized")
		return
	}

	// Get current provider
	clientType := m.state.Agent.GetProviderType()

	// Get available models
	models, err := agent_api.GetModelsForProvider(clientType)
	if err != nil {
		ui.Logf("❌ Failed to list models: %v", err)
		return
	}

	if len(models) == 0 {
		ui.Logf("No models available for %s", agent_api.GetProviderName(clientType))
		return
	}

	// Convert to dropdown items
	items := make([]dropdownItem, len(models))
	for i, model := range models {
		cost := ""
		if model.InputCost > 0 || model.OutputCost > 0 {
			cost = fmt.Sprintf("$%.4f/$%.4f", model.InputCost, model.OutputCost)
		} else if model.Cost > 0 {
			cost = fmt.Sprintf("$%.4f/1K", model.Cost)
		} else if model.Provider == "Ollama (Local)" {
			cost = "FREE (local)"
		} else {
			cost = "N/A"
		}

		items[i] = dropdownItem{
			title:       fmt.Sprintf("%s (%s)", model.ID, model.Provider),
			description: fmt.Sprintf("%s | Context: %d", cost, model.ContextLength),
			value:       model.ID,
		}
	}

	m.dropdown = NewInlineDropdown("Select a model:", items)
	m.dropdownMode = DropdownModels
}

// processFileReferences processes file references in input
func (m Model) processFileReferences(input string) string {
	words := strings.Fields(input)
	var hasFiles bool
	var filePaths []string

	for _, word := range words {
		cleaned := strings.Trim(word, `"'`)

		// Check if it looks like a file path
		if strings.Contains(cleaned, "/") || strings.Contains(cleaned, "\\") {
			if info, err := os.Stat(cleaned); err == nil && !info.IsDir() {
				hasFiles = true
				filePaths = append(filePaths, cleaned)
			}
		}

		// Check for common file extensions
		if strings.Contains(cleaned, ".") {
			ext := filepath.Ext(cleaned)
			commonExts := []string{".go", ".js", ".ts", ".py", ".java", ".c", ".cpp", ".rs", ".rb", ".php", ".swift", ".kt", ".scala", ".sh", ".yml", ".yaml", ".json", ".xml", ".html", ".css", ".md", ".txt"}
			for _, commonExt := range commonExts {
				if ext == commonExt {
					if info, err := os.Stat(cleaned); err == nil && !info.IsDir() {
						hasFiles = true
						filePaths = append(filePaths, cleaned)
					}
					break
				}
			}
		}
	}

	if hasFiles {
		var result strings.Builder
		result.WriteString(input)
		result.WriteString("\n\nReferenced files:\n")
		for _, path := range filePaths {
			result.WriteString(fmt.Sprintf("#%s\n", path))
		}
		ui.Logf("📁 Detected %d file reference(s) in input", len(filePaths))
		return result.String()
	}

	return input
}

// executeAgentRequest executes an agent request
func (m *Model) executeAgentRequest(request string) {
	if m.state.Agent == nil {
		ui.Logf("❌ Agent not initialized")
		return
	}

	ui.Logf("🚀 Starting agent execution: %s", request)
	ui.PublishStatus("Executing with agent system...")

	response, err := m.state.Agent.ProcessQueryWithContinuity(request)
	if err != nil {
		ui.Logf("❌ Agent execution failed: %v", err)
		ui.PublishStatus("Agent execution failed")
	} else {
		ui.Logf("✅ Agent completed successfully")
		ui.Logf("🎯 Result: %s", response)
		ui.PublishStatus("Agent execution completed")
		m.state.Agent.PrintConciseSummary()
	}
}

// renderPromptModal renders the prompt modal overlay
func (m Model) renderPromptModal() string {
	base := m.viewRenderer.Render()

	// Overlay modal with scrollable prompt content
	pvWidth := max(40, m.state.Width-10)
	pvHeight := max(8, min(16, m.state.Height-6))
	m.state.PromptViewport.Width = pvWidth - 4
	m.state.PromptViewport.Height = pvHeight - 5
	m.state.PromptViewport.SetContent(m.state.PromptText)

	def := "no"
	if m.state.PromptDefault {
		def = "yes"
	}
	help := "Type y/n then Enter (ESC cancels to default: " + def + ")"
	content := m.state.PromptViewport.View() + "\n"

	if m.state.PromptYesNo {
		content += "[" + strings.ToUpper(def) + "] [Y]es / [N]o\n"
	} else {
		content += "> " + m.state.PromptInput + "\n"
		help = "Type your response and press Enter (ESC cancels)"
	}
	content += help

	box := lipgloss.NewStyle().
		Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		Width(pvWidth).
		Height(pvHeight).
		Render(content)

	overlay := lipgloss.Place(m.state.Width, m.state.Height, lipgloss.Center, lipgloss.Center, box)
	return base + "\n" + overlay
}
