package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ViewRenderer handles rendering different views
type ViewRenderer struct {
	state *AppState
}

// NewViewRenderer creates a new view renderer
func NewViewRenderer(state *AppState) *ViewRenderer {
	return &ViewRenderer{state: state}
}

// Render returns the appropriate view based on mode
func (vr *ViewRenderer) Render() string {
	if vr.state.InteractiveMode {
		return vr.renderInteractiveView()
	}
	return vr.renderStandardView()
}

// renderInteractiveView provides a streamlined console-like interface
func (vr *ViewRenderer) renderInteractiveView() string {
	// Calculate layout dimensions
	layout := vr.calculateLayout()

	// Configure viewport for logs (account for no border now)
	vr.state.LogsViewport.Width = vr.state.Width
	vr.state.LogsViewport.Height = layout.logsHeight

	// Build UI components
	header := vr.renderStreamlinedHeader()
	progress := vr.renderProgress()
	logs := vr.renderLogs()

	// Main content area - show logs by default
	mainContent := logs

	// Only show suggestions if ALL conditions are met:
	// 1. ShowCommandSuggestions is true
	// 2. We have suggestions to show
	// 3. We haven't just executed a command
	// 4. The text input contains exactly "/"
	if vr.state.ShowCommandSuggestions &&
		len(vr.state.CommandSuggestions) > 0 &&
		!vr.state.JustExecutedCommand &&
		vr.state.TextInput.Value() == "/" {
		// When showing suggestions, combine logs and suggestions
		suggestionBox := vr.renderCommandSuggestions()
		mainContent = lipgloss.JoinVertical(lipgloss.Left, logs, suggestionBox)
	}

	inputArea := vr.renderInputArea()
	footer := vr.renderStreamlinedFooter()

	// Build final components list
	components := []string{header}

	if progress != "" {
		components = append(components, progress)
	}
	components = append(components, mainContent, inputArea, footer)

	// Filter out empty components
	var nonEmptyComponents []string
	for _, comp := range components {
		if comp != "" {
			nonEmptyComponents = append(nonEmptyComponents, comp)
		}
	}

	return lipgloss.JoinVertical(lipgloss.Left, nonEmptyComponents...)
}

// renderStandardView keeps the original non-interactive layout
func (vr *ViewRenderer) renderStandardView() string {
	header := vr.renderHeader()
	prog := ""
	if !vr.state.ProgressCollapsed {
		if pr := vr.renderProgress(); pr != "" {
			prog = pr + "\n"
		}
	}

	// Compute logs viewport height
	reserved := 4 // header + footer + spacing
	if prog != "" {
		reserved += countLines(prog) + 1
	}

	availableLogLines := vr.state.Height - reserved
	if availableLogLines < 3 {
		availableLogLines = 3
	}

	vr.state.LogsViewport.Width = max(0, vr.state.Width-2)
	vr.state.LogsViewport.Height = max(1, availableLogLines)

	logsView := "[logs collapsed]"
	if !vr.state.LogsCollapsed {
		logsView = vr.state.LogsViewport.View()
	}

	body := lipgloss.NewStyle().
		Margin(1, 1).
		Render(fmt.Sprintf("%sLogs:\n%s", prog, logsView))

	footer := vr.renderFooter()

	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
}

// Layout calculations
type layoutDimensions struct {
	headerHeight      int
	footerHeight      int
	inputHeight       int
	progressHeight    int
	suggestionsHeight int
	logsHeight        int
}

func (vr *ViewRenderer) calculateLayout() layoutDimensions {
	layout := layoutDimensions{
		headerHeight: 1, // Just the header line
		footerHeight: 1,
		inputHeight:  3, // Input box with border
	}

	// Progress height
	if !vr.state.ProgressCollapsed && vr.renderProgress() != "" {
		layout.progressHeight = countLines(vr.renderProgress()) + 1
	}

	// Command suggestions height
	if vr.state.ShowCommandSuggestions && len(vr.state.CommandSuggestions) > 0 {
		// Limit suggestions height to prevent taking all space
		suggestionsCount := len(vr.state.CommandSuggestions)
		if suggestionsCount > 10 {
			suggestionsCount = 10 // Show max 10 suggestions
		}
		layout.suggestionsHeight = suggestionsCount + 3
	}

	// Calculate remaining space for logs
	layout.logsHeight = vr.state.Height - layout.headerHeight - layout.footerHeight -
		layout.inputHeight - layout.progressHeight - layout.suggestionsHeight

	if layout.logsHeight < MinLogsHeight {
		layout.logsHeight = MinLogsHeight
	}

	return layout
}

// Component rendering methods
func (vr *ViewRenderer) renderStreamlinedHeader() string {
	elapsed := vr.state.StartTime.Format("15:04:05")

	// Get agent info
	agentInfo := "🤖 Agent Ready"
	if vr.state.Streaming {
		agentInfo = "🤖 Agent Processing..."
	}

	// Build header parts
	parts := []string{agentInfo}

	// Add provider and model info
	if vr.state.Provider != "" {
		parts = append(parts, fmt.Sprintf("Provider: %s", vr.state.Provider))
	}
	if vr.state.BaseModel != "" {
		parts = append(parts, fmt.Sprintf("Model: %s", vr.state.BaseModel))
	}

	// Add token/cost info
	if vr.state.TotalTokens > 0 {
		parts = append(parts, fmt.Sprintf("Tokens: %s", formatNumber(vr.state.TotalTokens)))
	}
	if vr.state.TotalCost > 0 {
		parts = append(parts, fmt.Sprintf("Cost: $%.4f", vr.state.TotalCost))
	}

	// Add time and log count
	parts = append(parts, elapsed)
	parts = append(parts, fmt.Sprintf("Logs: %d", len(vr.state.Logs)))

	headerContent := strings.Join(parts, " • ")

	// Trim to width if needed
	if vr.state.Width > 0 && len(headerContent) > vr.state.Width-4 {
		// Prioritize showing agent status, tokens, and cost
		parts = []string{agentInfo}
		if vr.state.TotalTokens > 0 {
			parts = append(parts, fmt.Sprintf("%s tok", formatNumber(vr.state.TotalTokens)))
		}
		if vr.state.TotalCost > 0 {
			parts = append(parts, fmt.Sprintf("$%.4f", vr.state.TotalCost))
		}
		headerContent = strings.Join(parts, " • ")
	}

	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("6")). // Cyan
		Padding(0, 1).
		MaxWidth(vr.state.Width).
		Render(headerContent)
}

func (vr *ViewRenderer) renderHeader() string {
	// Standard header for non-interactive mode
	streaming := ""
	if vr.state.Streaming {
		streaming = " [streaming…]"
	}

	var parts []string
	parts = []string{
		fmt.Sprintf("Model: %s", vr.state.BaseModel),
		fmt.Sprintf("Tokens: %d", vr.state.TotalTokens),
		fmt.Sprintf("Cost: $%.4f", vr.state.TotalCost),
	}

	line := strings.Join(parts, " | ") + streaming
	if vr.state.Width > 0 {
		line = trimToWidth(line, vr.state.Width-2)
	}

	return lipgloss.NewStyle().
		Bold(true).
		Padding(0, 1).
		Render(line)
}

func (vr *ViewRenderer) renderProgress() string {
	if vr.state.ProgressCollapsed || vr.state.Progress.Total == 0 {
		return ""
	}

	out := fmt.Sprintf("📊 Progress: %d/%d steps completed\n",
		vr.state.Progress.Completed, vr.state.Progress.Total)
	out += fmt.Sprintf("%-24s %-12s %-22s %8s %10s\n",
		"Agent", "Status", "Current Step", "Tokens", "Cost($)")
	out += strings.Repeat("-", 80) + "\n"

	for _, r := range vr.state.Progress.Rows {
		out += fmt.Sprintf("%-24s %-12s %-22s %8d %10.4f\n",
			r.Name, r.Status, r.Step, r.Tokens, r.Cost)
	}

	return out
}

func (vr *ViewRenderer) renderLogs() string {
	// Ensure viewport has correct dimensions
	if vr.state.LogsViewport.Width <= 0 || vr.state.LogsViewport.Height <= 0 {
		// Return a minimal placeholder if dimensions are invalid
		return lipgloss.NewStyle().
			Width(80).
			Height(5).
			Align(lipgloss.Center).
			Faint(true).
			Render("🤖 Agent initializing...")
	}

	// Show actual logs without border (border was causing layout issues)
	logsContent := vr.state.LogsViewport.View()

	// If no logs, show placeholder
	if len(vr.state.Logs) == 0 {
		logsContent = lipgloss.NewStyle().
			Faint(true).
			Align(lipgloss.Center).
			Width(vr.state.LogsViewport.Width).
			Height(vr.state.LogsViewport.Height).
			Render("🤖 Agent ready - enter a request below")
	}

	return logsContent
}

func (vr *ViewRenderer) renderCommandSuggestions() string {
	suggestionLines := []string{"📋 Available commands:"}

	// Show max 10 suggestions to prevent overflow
	maxSuggestions := 10
	for i, cmd := range vr.state.CommandSuggestions {
		if i >= maxSuggestions {
			suggestionLines = append(suggestionLines, "  ... and more. Type to filter.")
			break
		}
		suggestionLines = append(suggestionLines, "  "+cmd)
	}

	// Ensure width doesn't exceed terminal width
	boxWidth := vr.state.Width - 4
	if boxWidth < 40 {
		boxWidth = 40
	}

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("3")). // Yellow
		Padding(0, 1).
		Width(boxWidth).
		MaxWidth(vr.state.Width - 2).
		Render(strings.Join(suggestionLines, "\n"))
}

func (vr *ViewRenderer) renderInputArea() string {
	if vr.state.Width < 20 {
		return lipgloss.NewStyle().
			Faint(true).
			Render("Terminal too narrow - resize to use input")
	}

	// Set appropriate width for text input
	inputWidth := vr.state.Width - 6
	if inputWidth > 100 {
		inputWidth = 100
	}
	vr.state.TextInput.Width = inputWidth

	// Render based on focus state
	if vr.state.FocusedInput {
		return lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("6")). // Cyan when focused
			Padding(0, 1).
			Width(vr.state.Width - 2).
			Render(vr.state.TextInput.View())
	}

	// Unfocused state
	currentValue := strings.TrimSpace(vr.state.TextInput.Value())
	preview := "Press 'i' to focus input"
	if currentValue != "" {
		maxLen := inputWidth - 20
		if len(currentValue) > maxLen {
			currentValue = currentValue[:maxLen-3] + "..."
		}
		preview = fmt.Sprintf("Current: %s (Press 'i' to edit)", currentValue)
	}

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("8")). // Gray when unfocused
		Padding(0, 1).
		Width(vr.state.Width - 2).
		Faint(true).
		Render(preview)
}

func (vr *ViewRenderer) renderStreamlinedFooter() string {
	shortcuts := "Enter: Execute • ↑/↓: History • Esc: Clear • /help: Commands • Ctrl+C: Exit"

	if len(vr.state.CommandHistory) > 0 {
		shortcuts = fmt.Sprintf("History: %d • %s", len(vr.state.CommandHistory), shortcuts)
	}

	// Add scroll indicator if needed
	if !vr.state.LogsViewport.AtBottom() {
		shortcuts += " • 📜 Auto-scroll OFF (End: resume)"
	}

	// Truncate if too long
	if len(shortcuts) > vr.state.Width-4 {
		shortcuts = "Enter: Execute • ↑/↓: History • /help • Ctrl+C: Exit"
	}
	if len(shortcuts) > vr.state.Width-4 {
		shortcuts = "Enter • ↑/↓ • /help • Ctrl+C"
	}

	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("8")). // Gray
		Padding(0, 1).
		Render(shortcuts)
}

func (vr *ViewRenderer) renderFooter() string {
	footerText := "Press q to quit | © Ledit"

	// Add scroll indicator if not at bottom
	if !vr.state.LogsCollapsed && !vr.state.LogsViewport.AtBottom() {
		footerText += " | 📜 Auto-scroll OFF (Press 'End' to resume)"
	}

	return lipgloss.NewStyle().
		Faint(true).
		Padding(0, 1).
		MaxWidth(vr.state.Width).
		Render(footerText)
}

// Helper functions are defined in tui.go
