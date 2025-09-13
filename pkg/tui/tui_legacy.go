package tui

import (
	// "context" // commented out - used in the commented functions
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/agent"
	agent_api "github.com/alantheprice/ledit/pkg/agent_api"
	commands "github.com/alantheprice/ledit/pkg/agent_commands"
	"github.com/alantheprice/ledit/pkg/ui"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"path/filepath"
)

// Basic model scaffold: header, body, footer with ticking clock
type model struct {
	start         time.Time
	width         int
	height        int
	logs          []string
	progress      ui.ProgressSnapshotEvent
	streaming     bool
	interruptChan chan string
	// simple prompt state
	awaitingPrompt bool
	promptID       string
	promptText     string
	promptYesNo    bool
	promptDefault  bool
	promptInput    string
	promptContext  string
	// summary
	baseModel   string
	totalTokens int
	totalCost   float64
	// logs pane controls
	logsCollapsed     bool
	progressCollapsed bool
	vp                viewport.Model
	// prompt viewport for long prompts/code context
	promptVP viewport.Model
	// interactive mode
	interactiveMode bool
	textInput       textinput.Model
	focusedInput    bool
	commandHistory  []string
	historyIndex    int    // Current position in command history (-1 means not browsing history)
	originalInput   string // Store original input when browsing history
	// paste detection
	lastInputTime  time.Time
	pasteBuffer    string
	inPasteMode    bool
	pasteThreshold time.Duration // Time threshold to detect paste
	// command suggestions
	showCommandSuggestions bool
	commandSuggestions     []string
	// agent and command registry
	agent           *agent.Agent
	commandRegistry *commands.CommandRegistry
}

type tickMsg time.Time

func initialModel() model {
	m := model{start: time.Now(), logs: make([]string, 0, 256)}
	m.vp = viewport.New(80, 20)       // Default size, will be updated on window resize
	m.promptVP = viewport.New(80, 10) // Default size, will be updated on window resize

	// Enable auto-scroll by default by ensuring viewport starts at bottom
	m.vp.GotoBottom()

	// Default logs collapsed; allow override via env
	if v := strings.ToLower(strings.TrimSpace(os.Getenv("LEDIT_LOGS_COLLAPSED"))); v == "0" || v == "false" || v == "no" {
		m.logsCollapsed = false
	} else {
		m.logsCollapsed = true
	}
	return m
}

func initialInteractiveModel() model {
	m := initialModel()
	m.interactiveMode = true
	m.focusedInput = true

	// In interactive mode, expand logs by default so user can see agent progress
	m.logsCollapsed = false

	// Initialize textinput for agent prompts
	ti := textinput.New()
	ti.Placeholder = "Enter request or /help for commands..."
	ti.Focus()
	ti.CharLimit = 2000
	ti.Width = 80 // Start with reasonable width, will be adjusted for full width
	m.textInput = ti

	// Initialize history navigation
	m.historyIndex = -1 // Not browsing history initially
	m.originalInput = ""

	// Initialize paste detection
	m.pasteThreshold = 50 * time.Millisecond // Characters arriving faster than this are likely paste
	m.lastInputTime = time.Now()
	m.inPasteMode = false

	// Ensure auto-scroll is enabled from the start in interactive mode
	m.vp.GotoBottom()

	// Initialize interrupt channel
	m.interruptChan = make(chan string, 1)

	// Initialize agent and command registry
	// Set environment variables for system
	os.Setenv("LEDIT_FROM_AGENT", "1")
	os.Setenv("LEDIT_SKIP_PROMPT", "1")
	os.Setenv("LEDIT_USING_CODER", "1")

	var err error
	m.agent, err = agent.NewAgent()
	if err != nil {
		ui.Logf("❌ Failed to initialize agent: %v", err)
	} else {
		// Set up stats update callback to publish progress events
		m.agent.SetStatsUpdateCallback(func(totalTokens int, totalCost float64) {
			// Publish progress event with token/cost updates
			modelName := m.agent.GetModel()
			ui.PublishProgressWithTokens(0, 0, totalTokens, totalCost, modelName, nil)
		})

		// Initialize model info
		m.baseModel = m.agent.GetModel()
	}

	m.commandRegistry = commands.NewCommandRegistry()

	return m
}

func (m model) Init() tea.Cmd {
	// Set up TUI output redirection so all fmt.Print* calls go to the logs viewport
	ui.SetDefaultSink(ui.TuiSink{})

	return tea.Batch(
		tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) }),
		subscribeEvents(),
	)
}

func subscribeEvents() tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(t time.Time) tea.Msg {
		// Check for events with longer polling interval to reduce interference
		select {
		case ev := <-ui.Events():
			// Return any event we receive
			return ev
		default:
			// No events available, continue polling
		}
		return subscribeEventsMsg{}
	})
}

// Message type for continuing event subscription
type subscribeEventsMsg struct{}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.vp.Width = max(0, m.width-2)
		// height is set later based on reserved rows
		return m, nil
	case tea.KeyMsg:
		// Prompt-aware handling first (modal blocks normal keys)
		if m.awaitingPrompt {
			if m.promptYesNo {
				// Single-key quick responses
				switch msg.String() {
				case "y", "Y":
					ui.SubmitPromptResponse(m.promptID, "yes", true)
					m.awaitingPrompt = false
					m.promptInput = ""
					return m, nil
				case "n", "N":
					ui.SubmitPromptResponse(m.promptID, "no", false)
					m.awaitingPrompt = false
					m.promptInput = ""
					return m, nil
				case "enter":
					ui.SubmitPromptResponse(m.promptID, "", m.promptDefault)
					m.awaitingPrompt = false
					m.promptInput = ""
					return m, nil
				case "esc":
					ui.SubmitPromptResponse(m.promptID, "", m.promptDefault)
					m.awaitingPrompt = false
					m.promptInput = ""
					return m, nil
				}
				// Typed input workflow
				switch msg.Type {
				case tea.KeyRunes:
					m.promptInput += strings.ToLower(string(msg.Runes))
					return m, nil
				case tea.KeyBackspace, tea.KeyCtrlH:
					if len(m.promptInput) > 0 {
						m.promptInput = m.promptInput[:len(m.promptInput)-1]
					}
					return m, nil
				case tea.KeyCtrlU: // clear line
					m.promptInput = ""
					return m, nil
				case tea.KeyCtrlK: // clear to end of line (same as clear in single-line)
					m.promptInput = ""
					return m, nil
				case tea.KeyCtrlW: // delete last word
					trimmed := strings.TrimRight(m.promptInput, " \t")
					i := len(trimmed) - 1
					for i >= 0 && trimmed[i] != ' ' && trimmed[i] != '\t' {
						i--
					}
					if i < 0 {
						m.promptInput = ""
					} else {
						m.promptInput = strings.TrimRight(trimmed[:i], " \t")
					}
					return m, nil
				case tea.KeyEnter:
					in := strings.TrimSpace(strings.ToLower(m.promptInput))
					switch in {
					case "y", "yes":
						ui.SubmitPromptResponse(m.promptID, "yes", true)
						m.awaitingPrompt = false
						m.promptInput = ""
						return m, nil
					case "n", "no":
						ui.SubmitPromptResponse(m.promptID, "no", false)
						m.awaitingPrompt = false
						m.promptInput = ""
						return m, nil
					case "":
						ui.SubmitPromptResponse(m.promptID, "", m.promptDefault)
						m.awaitingPrompt = false
						m.promptInput = ""
						return m, nil
					default:
						ui.Log("Please type 'yes' or 'no', or press Enter for default")
						return m, nil
					}
				}
				// consume all other keys while modal is active
				return m, nil
			}
		}
		// Interactive mode key handling
		if m.interactiveMode && m.focusedInput {
			switch msg.String() {
			case "up":
				// Navigate up in command history
				if len(m.commandHistory) > 0 {
					// If not currently browsing history, save the original input
					if m.historyIndex == -1 {
						m.originalInput = m.textInput.Value()
						m.historyIndex = len(m.commandHistory) - 1
					} else if m.historyIndex > 0 {
						m.historyIndex--
					}

					// Set the historical command
					if m.historyIndex >= 0 && m.historyIndex < len(m.commandHistory) {
						m.textInput.SetValue(m.commandHistory[m.historyIndex])
						m.textInput.SetCursor(len(m.commandHistory[m.historyIndex]))
					}
				}
				return m, nil
			case "down":
				// Navigate down in command history
				if len(m.commandHistory) > 0 && m.historyIndex >= 0 {
					m.historyIndex++

					// If we've gone past the end, restore original input
					if m.historyIndex >= len(m.commandHistory) {
						m.textInput.SetValue(m.originalInput)
						m.textInput.SetCursor(len(m.originalInput))
						m.historyIndex = -1
						m.originalInput = ""
					} else {
						// Set the historical command
						m.textInput.SetValue(m.commandHistory[m.historyIndex])
						m.textInput.SetCursor(len(m.commandHistory[m.historyIndex]))
					}
				}
				return m, nil
			case "enter":
				input := m.textInput.Value()

				// Check for backslash continuation
				if strings.HasSuffix(strings.TrimSpace(input), "\\") {
					// Remove trailing backslash and add newline
					trimmed := strings.TrimSpace(input)
					newValue := trimmed[:len(trimmed)-1] + "\n"
					m.textInput.SetValue(newValue)
					m.textInput.SetCursor(len(newValue))
					ui.Log("↩️  Continue on next line...")
					return m, nil
				}

				input = strings.TrimSpace(input)
				if input != "" {
					// Check for slash commands first
					if strings.HasPrefix(input, "/") && !strings.Contains(input, "\n") {
						handled, newModel, cmd := m.handleSlashCommand(input)
						if handled {
							m.textInput.SetValue("")
							if newModel != nil {
								return *newModel, cmd
							}
							return m, cmd
						}
					}

					// Check if input contains file paths
					processedInput := m.processFileReferences(input)

					// Regular agent command execution
					go func() {
						m.executeAgentRequest(processedInput)
					}()
					ui.Logf("🎯 Executing: %s", strings.ReplaceAll(processedInput, "\n", " ↩️ "))

					// Add to command history
					m.commandHistory = append(m.commandHistory, input)
					if len(m.commandHistory) > 50 {
						m.commandHistory = m.commandHistory[len(m.commandHistory)-50:]
					}

					// Reset history navigation state
					m.historyIndex = -1
					m.originalInput = ""

					m.textInput.SetValue("")
				}
				return m, nil
			case "esc":
				// In interactive mode, clear the current input instead of unfocusing
				if m.interactiveMode {
					m.textInput.SetValue("")
					m.textInput.SetCursor(0)
					// Reset history navigation
					m.historyIndex = -1
					m.originalInput = ""
					return m, nil
				}
				// For non-interactive mode, unfocus as before
				m.focusedInput = false
				m.textInput.Blur()
				return m, nil
			case "ctrl+c":
				return m, tea.Quit
			case "tab":
				// In interactive mode with focused input, tab should pass through to text input
				// This prevents the general tab handler from interfering
				var cmd tea.Cmd
				m.textInput, cmd = m.textInput.Update(msg)
				return m, cmd
			default:
				// Reset history navigation when user starts typing
				if m.historyIndex != -1 {
					m.historyIndex = -1
					m.originalInput = ""
				}

				// Paste detection - check if input is coming in too fast
				now := time.Now()
				if msg.Type == tea.KeyRunes {
					// Check time since last input
					if now.Sub(m.lastInputTime) < m.pasteThreshold {
						// Likely paste - accumulate in paste buffer
						if !m.inPasteMode {
							m.inPasteMode = true
							m.pasteBuffer = m.textInput.Value() // Save current content
						}
						m.pasteBuffer += string(msg.Runes)
					} else {
						// Normal typing speed
						if m.inPasteMode {
							// Paste ended, update text input with full paste
							m.textInput.SetValue(m.pasteBuffer)
							m.textInput.SetCursor(len(m.pasteBuffer))
							m.inPasteMode = false
							m.pasteBuffer = ""
						}
					}
					m.lastInputTime = now
				}

				// Pass through to text input
				var cmd tea.Cmd
				m.textInput, cmd = m.textInput.Update(msg)

				// Check if we should show command suggestions
				currentValue := m.textInput.Value()
				if currentValue == "/" && m.commandRegistry != nil {
					m.showCommandSuggestions = true
					// Get commands from registry
					m.commandSuggestions = []string{}
					for _, cmd := range m.commandRegistry.ListCommands() {
						m.commandSuggestions = append(m.commandSuggestions,
							fmt.Sprintf("/%s - %s", cmd.Name(), cmd.Description()))
					}
					// Add TUI-specific commands
					m.commandSuggestions = append(m.commandSuggestions,
						"/logs - Toggle logs view",
						"/show - Show/expand logs",
						"/hide - Hide/collapse logs",
						"/progress - Toggle progress view",
						"/history - Show command history",
						"/paste - Paste mode instructions",
						"/clear - Clear logs",
					)
				} else if !strings.HasPrefix(currentValue, "/") || currentValue == "" {
					m.showCommandSuggestions = false
				}

				return m, cmd
			}
		}

		// General key handling (when not in prompt)
		switch msg.String() {
		case "i":
			if m.interactiveMode && !m.focusedInput {
				// Focus input
				m.focusedInput = true
				m.textInput.Focus()
				return m, nil
			}
		case "q", "Q":
			if !m.interactiveMode || !m.focusedInput {
				return m, tea.Quit
			}
		case "ctrl+c":
			return m, tea.Quit
		case "esc":
			if !m.interactiveMode || !m.focusedInput {
				return m, tea.Quit
			}
		case "l", "L":
			m.logsCollapsed = !m.logsCollapsed
			return m, nil
		case "p", "P":
			m.progressCollapsed = !m.progressCollapsed
			return m, nil
		case "ctrl+l": // clear logs
			m.logs = m.logs[:0]
			m.vp.SetContent("")
			return m, nil
		case "tab":
			// Skip if we're in interactive mode with focused input
			// (already handled in the interactive section)
			if m.interactiveMode && m.focusedInput {
				return m, nil
			}

			if m.interactiveMode {
				// In interactive mode, tab should not unfocus the input
				// Instead, it can be used for autocomplete or just ignored
				// Keep input focused for better UX
				if !m.focusedInput {
					m.focusedInput = true
					m.textInput.Focus()
				}
				// TODO: Could add autocomplete functionality here
			} else {
				// In non-interactive mode, preserve the toggle behavior
				if m.focusedInput {
					m.focusedInput = false
					m.textInput.Blur()
				} else {
					m.focusedInput = true
					m.textInput.Focus()
				}
			}
			return m, nil
		case "up", "k":
			if !m.logsCollapsed {
				m.vp.LineUp(1)
			}
			return m, nil
		case "down", "j":
			if !m.logsCollapsed {
				m.vp.LineDown(1)
			}
			return m, nil
		// PageUp/PageDown, arrows, mouse will be handled by viewport.Update below
		case "home":
			if !m.logsCollapsed {
				m.vp.GotoTop()
			}
			return m, nil
		case "end":
			if !m.logsCollapsed {
				m.vp.GotoBottom()
			}
			return m, nil
		}
		// Pass through to viewport for mouse and other keys
		if !m.logsCollapsed {
			var cmd tea.Cmd
			m.vp, cmd = m.vp.Update(msg)
			return m, cmd
		}
		return m, nil
	case ui.LogEvent:
		// Check if user was at bottom before adding new content
		wasAtBottom := m.vp.AtBottom()

		m.logs = append(m.logs, msg.Text)
		if len(m.logs) > 500 {
			m.logs = m.logs[len(m.logs)-500:]
		}
		m.vp.SetContent(strings.Join(m.logs, "\n"))

		// Smart auto-scroll: only scroll to bottom if user was already at bottom
		// This prevents interrupting users who are reading earlier logs
		if wasAtBottom {
			m.vp.GotoBottom()
		}
		return m, nil
	case ui.ProgressSnapshotEvent:
		m.progress = msg
		if msg.BaseModel != "" {
			m.baseModel = msg.BaseModel
		}
		if msg.TotalTokens > 0 {
			m.totalTokens = msg.TotalTokens
		}
		if msg.TotalCost > 0 {
			m.totalCost = msg.TotalCost
		}
		return m, nil
	case ui.ModelInfoEvent:
		if strings.TrimSpace(msg.Name) != "" {
			m.baseModel = msg.Name
		}
		return m, nil
	case ui.StreamStartedEvent:
		m.streaming = true
		return m, nil
	case ui.StreamEndedEvent:
		m.streaming = false
		return m, nil
	case ui.PromptRequestEvent:
		m.awaitingPrompt = true
		m.promptID = msg.ID
		m.promptText = msg.Prompt
		m.promptContext = msg.Context
		m.promptYesNo = msg.RequireYesNo
		m.promptDefault = msg.DefaultYes
		m.promptInput = ""
		// initialize prompt viewport content
		pvContent := m.promptText
		if strings.TrimSpace(m.promptContext) != "" {
			pvContent = m.promptText + "\n\n" + m.promptContext
		}
		m.promptVP.SetContent(pvContent)
		return m, nil
	case ui.StatusEvent:
		// Update a concise shimmery status line by publishing as a log substitute
		// but we keep it in header/body by storing in baseModel suffix
		if s := strings.TrimSpace(msg.Text); s != "" {
			// Show transiently via logs pane top
			m.logs = append([]string{"… " + s}, m.logs...)
			if len(m.logs) > 500 {
				m.logs = m.logs[:500]
			}
		}
		return m, nil
	case subscribeEventsMsg:
		// Continue polling for events
		return m, subscribeEvents()
	case tickMsg:
		return m, tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
	default:
		return m, nil
	}
}

func (m model) View() string {
	if m.interactiveMode {
		return m.renderInteractiveView()
	}
	return m.renderStandardView()
}

// renderInteractiveView provides a streamlined console-like interface
func (m model) renderInteractiveView() string {
	// Calculate layout dimensions
	headerHeight := 2
	footerHeight := 1
	inputHeight := 3 // Input box with border
	progressHeight := 0
	if !m.progressCollapsed && m.renderProgress() != "" {
		progressHeight = countLines(m.renderProgress()) + 1
	}

	// Account for command suggestions if shown
	suggestionsHeight := 0
	if m.showCommandSuggestions && len(m.commandSuggestions) > 0 {
		suggestionsHeight = len(m.commandSuggestions) + 3 // +3 for border and title
	}

	// Available space for scrolling logs
	logsHeight := m.height - headerHeight - footerHeight - inputHeight - progressHeight - suggestionsHeight
	if logsHeight < 5 {
		logsHeight = 5 // Minimum viable logs area
	}

	// Configure viewport for logs
	m.vp.Width = m.width - 2
	m.vp.Height = logsHeight

	// Build the UI components
	header := m.renderStreamlinedHeader()
	progress := ""
	if !m.progressCollapsed {
		if pr := m.renderProgress(); pr != "" {
			progress = pr + "\n"
		}
	}

	// Logs area - always visible in interactive mode
	logsContent := m.vp.View()
	if len(m.logs) == 0 {
		logsContent = lipgloss.NewStyle().
			Faint(true).
			Align(lipgloss.Center).
			Width(m.vp.Width).
			Height(m.vp.Height).
			Render("🤖 Agent ready - enter a request below")
	}

	// Command suggestions if active
	commandSuggestionsView := ""
	if m.showCommandSuggestions && len(m.commandSuggestions) > 0 {
		suggestionLines := []string{"📋 Available commands:"}
		for _, cmd := range m.commandSuggestions {
			suggestionLines = append(suggestionLines, "  "+cmd)
		}
		commandSuggestionsView = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("3")). // Yellow
			Padding(0, 1).
			Width(m.width - 2).
			Render(strings.Join(suggestionLines, "\n"))
	}

	// Input area at bottom
	inputArea := m.renderInputArea()

	// Footer with help
	footer := m.renderStreamlinedFooter()

	// Assemble the complete view
	components := []string{header, progress, logsContent}
	if commandSuggestionsView != "" {
		components = append(components, commandSuggestionsView)
	}
	components = append(components, inputArea, footer)

	return lipgloss.JoinVertical(lipgloss.Left, components...)
}

// renderStandardView keeps the original non-interactive layout
func (m model) renderStandardView() string {
	header := m.renderHeader()
	// Progress section (collapsible)
	prog := ""
	if !m.progressCollapsed {
		if pr := m.renderProgress(); pr != "" {
			prog = pr + "\n"
		}
	}
	// Compute logs viewport height
	reserved := 1 + 2 + 1 // header + spacing + footer
	if !m.progressCollapsed && m.renderProgress() != "" {
		reserved += countLines(m.renderProgress()) + 1
	}

	availableLogLines := m.height - reserved
	if availableLogLines < 3 {
		availableLogLines = 3
	}
	m.vp.Width = max(0, m.width-2)
	m.vp.Height = max(1, availableLogLines)
	logsView := "[logs collapsed]"
	if !m.logsCollapsed {
		logsView = m.vp.View()
	}

	body := lipgloss.NewStyle().Margin(1, 1).Render(fmt.Sprintf("Width: %d  Height: %d\n\n%sLogs | Progress\n%s", m.width, m.height, prog, logsView))

	// Interactive input box
	inputBox := ""
	if m.interactiveMode && m.width > 20 { // Only show input box if terminal is wide enough
		// Use a reasonable fixed width to avoid wrapping issues
		// Dynamic width seems to cause layout problems, so use responsive but stable sizing
		var textInputWidth int
		if m.width > 100 {
			textInputWidth = 80 // Wide terminal
		} else if m.width > 60 {
			textInputWidth = m.width - 20 // Medium terminal
		} else {
			textInputWidth = 40 // Narrow terminal
		}

		// Set textinput width
		m.textInput.Width = textInputWidth

		if m.focusedInput {
			// Clean input box without prefix - let textinput determine its own width
			inputBox = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				Padding(0, 1).
				Render(m.textInput.View())
		} else {
			// Show preview when unfocused
			currentValue := strings.TrimSpace(m.textInput.Value())
			preview := "Press 'i' to input"
			if currentValue != "" {
				maxPreviewLen := textInputWidth - 10
				if len(currentValue) > maxPreviewLen {
					currentValue = currentValue[:maxPreviewLen-3] + "..."
				}
				preview = fmt.Sprintf("Current: %s", currentValue)
			}
			inputBox = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				Padding(0, 1).
				Faint(true).
				Render(preview)
		}
	} else if m.interactiveMode {
		// Terminal too narrow for input box
		inputBox = lipgloss.NewStyle().Faint(true).Render("Terminal too narrow for input - resize or use command line")
	}

	footer := ""

	// Add scroll indicator if user is not at bottom (auto-scroll disabled)
	scrollIndicator := ""
	if !m.logsCollapsed && !m.vp.AtBottom() {
		scrollIndicator = " | 📜 Auto-scroll OFF (Press 'End' to resume)"
	}

	if m.interactiveMode {
		// Show focused state and add Tab info
		focusState := ""
		if m.focusedInput {
			focusState = "INPUT FOCUSED"
		} else {
			focusState = "LOGS FOCUSED"
		}

		historyInfo := ""
		if len(m.commandHistory) > 0 {
			historyInfo = fmt.Sprintf(" | History: %d", len(m.commandHistory))
		}

		footerText := fmt.Sprintf("%s | Enter: Execute | ↑/↓: History | Esc: Clear | Ctrl+C: Exit | /help: Commands%s%s",
			focusState, historyInfo, scrollIndicator)

		// Progressive truncation for narrow terminals
		if len(footerText) > m.width-4 {
			footerText = fmt.Sprintf("%s | Enter: Execute | ↑/↓: History | Esc: Clear%s%s",
				focusState, historyInfo, scrollIndicator)
		}
		if len(footerText) > m.width-4 {
			footerText = fmt.Sprintf("%s | Tab | Enter | ↑/↓%s", focusState, scrollIndicator)
		}
		if len(footerText) > m.width-4 {
			footerText = focusState + scrollIndicator
		}
		footer = lipgloss.NewStyle().Faint(true).Padding(0, 1).MaxWidth(m.width).Render(footerText)
	} else {
		footerText := "Press q to quit | © Ledit" + scrollIndicator
		footer = lipgloss.NewStyle().Faint(true).Padding(0, 1).MaxWidth(m.width).Render(footerText)
	}

	var base string
	if m.interactiveMode && inputBox != "" {
		base = lipgloss.JoinVertical(lipgloss.Left, header, body, inputBox, footer)
	} else {
		base = lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
	}
	if m.awaitingPrompt {
		// Overlay modal with scrollable prompt content + input/choices
		pvWidth := max(40, m.width-10)
		pvHeight := max(8, min(16, m.height-6))
		m.promptVP.Width = pvWidth - 4
		m.promptVP.Height = pvHeight - 5
		m.promptVP.SetContent(m.promptText)
		def := "no"
		if m.promptDefault {
			def = "yes"
		}
		help := "Type y/n then Enter (ESC cancels to default: " + def + ")"
		content := m.promptVP.View() + "\n"
		if m.promptYesNo {
			content += "[" + strings.ToUpper(def) + "] [Y]es / [N]o\n"
		} else {
			content += "> " + m.promptInput + "\n"
			help = "Type your response and press Enter (ESC cancels)"
		}
		content += help
		box := lipgloss.NewStyle().Padding(1, 2).Border(lipgloss.RoundedBorder()).Width(pvWidth).Height(pvHeight).Render(content)
		overlay := lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
		return base + "\n" + overlay
	}
	return base
}

// renderStreamlinedHeader provides a clean header for interactive mode
func (m model) renderStreamlinedHeader() string {
	elapsed := time.Since(m.start).Truncate(time.Second)

	// Get agent info if available
	agentInfo := "🤖 Agent Ready"
	if m.streaming {
		agentInfo = "🤖 Agent Processing..."
	}

	// Build header with essential info
	parts := []string{agentInfo}

	// Add model info if available
	if m.baseModel != "" {
		parts = append(parts, fmt.Sprintf("Model: %s", m.baseModel))
	}

	// Add token/cost info if available
	if m.totalTokens > 0 {
		parts = append(parts, fmt.Sprintf("Tokens: %s", formatNumber(m.totalTokens)))
	}
	if m.totalCost > 0 {
		parts = append(parts, fmt.Sprintf("Cost: $%.4f", m.totalCost))
	}

	// Add elapsed time and log count
	parts = append(parts, fmt.Sprintf("%v", elapsed))
	parts = append(parts, fmt.Sprintf("Logs: %d", len(m.logs)))

	headerContent := strings.Join(parts, " • ")

	// Trim to width if needed
	if m.width > 0 && len(headerContent) > m.width-4 {
		// Prioritize showing agent status, tokens, and cost
		parts = []string{agentInfo}
		if m.totalTokens > 0 {
			parts = append(parts, fmt.Sprintf("%s tok", formatNumber(m.totalTokens)))
		}
		if m.totalCost > 0 {
			parts = append(parts, fmt.Sprintf("$%.4f", m.totalCost))
		}
		headerContent = strings.Join(parts, " • ")
	}

	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("6")). // Cyan
		Padding(0, 1).
		Render(headerContent)
}

// renderInputArea provides the bottom input area for interactive mode
func (m model) renderInputArea() string {
	if m.width < 20 {
		return lipgloss.NewStyle().
			Faint(true).
			Render("Terminal too narrow - resize to use input")
	}

	// Set appropriate width for text input
	inputWidth := m.width - 6 // Account for borders and padding
	if inputWidth > 100 {
		inputWidth = 100 // Max width for usability
	}
	m.textInput.Width = inputWidth

	// Render based on focus state
	if m.focusedInput {
		return lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("6")). // Cyan when focused
			Padding(0, 1).
			Width(m.width - 2).
			Render(m.textInput.View())
	} else {
		// Show unfocused state with current value preview
		currentValue := strings.TrimSpace(m.textInput.Value())
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
			Width(m.width - 2).
			Faint(true).
			Render(preview)
	}
}

// renderStreamlinedFooter provides helpful shortcuts
func (m model) renderStreamlinedFooter() string {
	shortcuts := "Enter: Execute • ↑/↓: History • Esc: Clear • /help: Commands • Ctrl+C: Exit"

	if len(m.commandHistory) > 0 {
		shortcuts = fmt.Sprintf("History: %d • %s", len(m.commandHistory), shortcuts)
	}

	// Add scroll indicator if needed
	if !m.vp.AtBottom() {
		shortcuts += " • 📜 Auto-scroll OFF (End: resume)"
	}

	// Truncate if too long
	if len(shortcuts) > m.width-4 {
		shortcuts = "Enter: Execute • ↑/↓: History • /help • Ctrl+C: Exit"
	}
	if len(shortcuts) > m.width-4 {
		shortcuts = "Enter • ↑/↓ • /help • Ctrl+C"
	}

	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("8")). // Gray
		Padding(0, 1).
		Render(shortcuts)
}

func (m model) renderHeader() string {
	// Build a single-line status header that adapts to width
	streaming := ""
	if m.streaming {
		streaming = " [streaming…]"
	}

	var parts []string
	if m.interactiveMode {
		// Interactive mode header
		parts = []string{
			"🤖",
			fmt.Sprintf("Model: %s", func() string {
				if m.baseModel != "" {
					return m.baseModel
				}
				return "default"
			}()),
		}
		if m.totalTokens > 0 {
			parts = append(parts, fmt.Sprintf("Tokens: %s", formatNumber(m.totalTokens)))
		}
		if m.totalCost > 0 {
			parts = append(parts, fmt.Sprintf("Cost: $%.4f", m.totalCost))
		}
		// Removed uptime - not needed in UI
	} else {
		// Standard mode header
		parts = []string{
			fmt.Sprintf("Model: %s", m.baseModel),
			fmt.Sprintf("Tokens: %d", m.totalTokens),
			fmt.Sprintf("Cost: $%.4f", m.totalCost),
			// Removed uptime from standard mode too
		}
	}

	// Status indicators
	if m.awaitingPrompt {
		parts = append(parts, "🔄 Awaiting input…")
	} else if len(m.logs) > 0 && strings.HasPrefix(m.logs[0], "… ") {
		parts = append(parts, strings.TrimPrefix(m.logs[0], "… "))
	}

	line := strings.Join(parts, " | ") + streaming
	// trim to width
	if m.width > 0 {
		line = trimToWidth(line, m.width-2)
	}

	headerStyle := lipgloss.NewStyle().Bold(true).Padding(0, 1)
	if m.interactiveMode {
		headerStyle = headerStyle.Foreground(lipgloss.Color("#00ADD8")) // Go blue
	}
	return headerStyle.Render(line)
}

// formatNumber formats large numbers with comma separators
func formatNumber(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	if n < 1000000 {
		return fmt.Sprintf("%.1fK", float64(n)/1000)
	}
	return fmt.Sprintf("%.1fM", float64(n)/1000000)
}

func trimToWidth(s string, w int) string {
	if w <= 0 || len(s) <= w {
		return s
	}
	if w <= 1 {
		return s[:w]
	}
	// leave room for ellipsis
	return s[:w-1] + "…"
}

func (m model) renderProgress() string {
	if m.progress.Total == 0 {
		return ""
	}
	out := fmt.Sprintf("📊 Progress: %d/%d steps completed\n", m.progress.Completed, m.progress.Total)
	out += fmt.Sprintf("%-24s %-12s %-22s %8s %10s\n", "Agent", "Status", "Current Step", "Tokens", "Cost($)")
	out += strings.Repeat("-", 80) + "\n"
	for _, r := range m.progress.Rows {
		out += fmt.Sprintf("%-24s %-12s %-22s %8d %10.4f\n", r.Name, r.Status, r.Step, r.Tokens, r.Cost)
	}
	return out
}

func countLines(s string) int {
	if s == "" {
		return 0
	}
	// ensure consistent counting regardless of trailing newline
	c := strings.Count(s, "\n")
	if strings.HasSuffix(s, "\n") {
		return c
	}
	return c + 1
}

// LEGACY: These functions are now in entry.go
// // Run starts the TUI program with sane options.
// func Run() error {
// 	p := tea.NewProgram(initialModel(), tea.WithContext(context.Background()), tea.WithAltScreen())
// 	_, err := p.Run()
// 	// On exit, restore default sink to stdout so subsequent output isn't lost
// 	ui.UseStdoutSink()
// 	return err
// }

// // RunInteractiveAgent starts the TUI in interactive agent mode
// func RunInteractiveAgent() error {
// 	p := tea.NewProgram(initialInteractiveModel(), tea.WithContext(context.Background()), tea.WithAltScreen())
// 	_, err := p.Run()
// 	// On exit, restore default sink to stdout so subsequent output isn't lost
// 	ui.UseStdoutSink()
// 	return err
// }

// // RunMinimalTest runs a minimal TUI for testing input responsiveness
// func RunMinimalTest() error {
// 	m := initialInteractiveModel()
// 	// Clear all complex logic for testing
// 	m.logs = []string{"Minimal TUI test - type something and press enter"}

// 	p := tea.NewProgram(m, tea.WithContext(context.Background()), tea.WithAltScreen())
// 	_, err := p.Run()
// 	ui.UseStdoutSink()
// 	return err
// }

// handleSlashCommand processes slash commands and returns (handled, newModel, cmd)
func (m model) handleSlashCommand(input string) (bool, *model, tea.Cmd) {
	if m.agent == nil || m.commandRegistry == nil {
		ui.Log("❌ Agent not initialized")
		return true, nil, nil
	}

	// If user typed just "/" show the command selector
	if input == "/" {
		// Convert registry commands to our Command interface
		cmds := m.commandRegistry.ListCommands()
		commandList := make([]Command, len(cmds))
		for i, cmd := range cmds {
			commandList[i] = cmd
		}

		selectedCmd, err := ShowCommandDropdown(commandList)
		if err != nil {
			// Selection was cancelled
			return true, nil, nil
		}
		input = selectedCmd
	}

	// Handle special cases for /models - show selector
	if input == "/models" || input == "/models select" {
		return m.handleModelsSelector()
	}

	// Handle TUI-specific commands that need special behavior
	switch input {
	case "/paste":
		ui.Log(`📋 Paste Mode Instructions:

For multi-line input or pasting code:
1. Type your text normally - the TUI detects fast input as paste
2. Use backslash (\) at the end of a line to continue on the next line
3. Press Enter when done

The TUI automatically handles paste detection and buffering.`)
		return true, nil, nil

	case "/quit", "/exit", "/q":
		ui.Log("👋 Goodbye!")
		return true, nil, tea.Quit

	case "/clear", "/c":
		// Create new model with cleared logs
		newM := m
		newM.logs = newM.logs[:0]
		newM.vp.SetContent("")
		ui.Log("📋 Logs cleared")
		return true, &newM, nil

	case "/logs", "/l":
		newM := m
		newM.logsCollapsed = !newM.logsCollapsed
		ui.Logf("📋 Logs %s", func() string {
			if newM.logsCollapsed {
				return "collapsed"
			}
			return "expanded"
		}())
		return true, &newM, nil

	case "/show", "/showlogs":
		newM := m
		if newM.logsCollapsed {
			newM.logsCollapsed = false
			ui.Log("📋 Logs expanded")
		} else {
			ui.Log("📋 Logs are already visible")
		}
		return true, &newM, nil

	case "/hide", "/hidelogs":
		newM := m
		if !newM.logsCollapsed {
			newM.logsCollapsed = true
			ui.Log("📋 Logs collapsed")
		} else {
			ui.Log("📋 Logs are already hidden")
		}
		return true, &newM, nil

	case "/progress", "/p":
		newM := m
		newM.progressCollapsed = !newM.progressCollapsed
		ui.Logf("📊 Progress %s", func() string {
			if newM.progressCollapsed {
				return "collapsed"
			}
			return "expanded"
		}())
		return true, &newM, nil

	case "/history", "/hist":
		if len(m.commandHistory) == 0 {
			ui.Log("📋 No command history yet")
		} else {
			historyText := "📋 Command History (recent first):\n"
			// Show last 10 commands
			start := len(m.commandHistory) - 10
			if start < 0 {
				start = 0
			}
			for i := len(m.commandHistory) - 1; i >= start; i-- {
				historyText += fmt.Sprintf("  %d. %s\n", len(m.commandHistory)-i, m.commandHistory[i])
			}
			ui.Log(strings.TrimSuffix(historyText, "\n"))
		}
		return true, nil, nil
	}

	// Use CommandRegistry for all other slash commands
	err := m.commandRegistry.Execute(input, m.agent)
	if err != nil {
		ui.Logf("❌ Command error: %v", err)
		ui.Logf("💡 Type '/help' to see available commands")
	}

	return true, nil, nil
}

// processFileReferences checks for file paths in input and prepends file content markers
func (m model) processFileReferences(input string) string {
	// Look for common file path patterns
	words := strings.Fields(input)
	var hasFiles bool
	var filePaths []string

	for _, word := range words {
		// Remove quotes if present
		cleaned := strings.Trim(word, `"'`)

		// Check if it looks like a file path
		if strings.Contains(cleaned, "/") || strings.Contains(cleaned, "\\") {
			// Check if file exists
			if info, err := os.Stat(cleaned); err == nil && !info.IsDir() {
				hasFiles = true
				filePaths = append(filePaths, cleaned)
			}
		}

		// Also check for common file extensions without path
		if strings.Contains(cleaned, ".") {
			ext := filepath.Ext(cleaned)
			// Common code file extensions
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

	// If we found files, prepend them with file markers
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

// executeAgentRequest executes an agent request using the persistent agent
func (m *model) executeAgentRequest(request string) {
	if m.agent == nil {
		ui.Logf("❌ Agent not initialized")
		return
	}

	ui.Logf("🚀 Starting agent execution: %s", request)
	ui.PublishStatus("Executing with agent system...")

	// Execute using agent system
	ui.Logf("🔄 Processing with workflow...")
	ui.Logf("💡 Phase-based approach: UNDERSTAND → EXPLORE → IMPLEMENT → VERIFY")

	response, err := m.agent.ProcessQueryWithContinuity(request)
	if err != nil {
		ui.Logf("❌ Coder agent execution failed: %v", err)
		ui.PublishStatus("Coder agent execution failed")
	} else {
		ui.Logf("✅ Coder agent completed successfully")
		ui.Logf("🎯 Result: %s", response)
		ui.PublishStatus("Coder agent execution completed")

		// Show comprehensive cost and token summary
		m.agent.PrintConciseSummary()
	}
}

// handleModelsSelector shows the model selector in TUI mode
func (m model) handleModelsSelector() (bool, *model, tea.Cmd) {
	if m.agent == nil {
		ui.Log("❌ Agent not initialized")
		return true, nil, nil
	}

	// Get current provider from agent
	clientType := m.agent.GetProviderType()

	// Get available models for the current provider
	models, err := agent_api.GetModelsForProvider(clientType)
	if err != nil {
		ui.Logf("❌ Failed to list models: %v", err)
		return true, nil, nil
	}

	if len(models) == 0 {
		ui.Logf("No models available for %s", agent_api.GetProviderName(clientType))
		return true, nil, nil
	}

	// Convert to ModelItem format for the dropdown
	items := make([]ModelItem, len(models))
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

		items[i] = ModelItem{
			ID:          model.ID,
			Display:     fmt.Sprintf("%s (%s)", model.ID, model.Provider),
			Description: fmt.Sprintf("%s | Context: %d", cost, model.ContextLength),
		}
	}

	// Show the dropdown selector
	selected, err := ShowModelDropdown(items)
	if err != nil {
		ui.Log("Model selection cancelled")
		return true, nil, nil
	}

	// Execute the models command with the selected model
	cmdStr := fmt.Sprintf("/models %s", selected)
	return m.handleSlashCommand(cmdStr)
}

// UILogger implements utils.Logger interface to output to UI
type UILogger struct{}

func (ul *UILogger) LogError(err error) {
	ui.Logf("❌ %v", err)
}

func (ul *UILogger) LogProcessStep(message string) {
	ui.Log(message)
}

func (ul *UILogger) LogUserInteraction(message string) {
	ui.Log(message)
}

func (ul *UILogger) Logf(format string, args ...interface{}) {
	ui.Logf(format, args...)
}
