package tui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/agent"
	"github.com/alantheprice/ledit/pkg/ui"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Basic model scaffold: header, body, footer with ticking clock
type model struct {
	start     time.Time
	width     int
	height    int
	logs      []string
	progress  ui.ProgressSnapshotEvent
	streaming bool
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

	// Ensure auto-scroll is enabled from the start in interactive mode
	m.vp.GotoBottom()

	return m
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) }),
		subscribeEvents(),
	)
}

func subscribeEvents() tea.Cmd {
	return func() tea.Msg {
		for ev := range ui.Events() {
			switch e := ev.(type) {
			case ui.LogEvent:
				return e
			case ui.ProgressSnapshotEvent:
				return e
			case ui.StreamStartedEvent:
				return e
			case ui.StreamEndedEvent:
				return e
			case ui.PromptRequestEvent:
				return e
			case ui.PromptResponseEvent:
				return e
			}
		}
		return nil
	}
}

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
					return m, subscribeEvents()
				case "n", "N":
					ui.SubmitPromptResponse(m.promptID, "no", false)
					m.awaitingPrompt = false
					m.promptInput = ""
					return m, subscribeEvents()
				case "enter":
					ui.SubmitPromptResponse(m.promptID, "", m.promptDefault)
					m.awaitingPrompt = false
					m.promptInput = ""
					return m, subscribeEvents()
				case "esc":
					ui.SubmitPromptResponse(m.promptID, "", m.promptDefault)
					m.awaitingPrompt = false
					m.promptInput = ""
					return m, subscribeEvents()
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
						return m, subscribeEvents()
					case "n", "no":
						ui.SubmitPromptResponse(m.promptID, "no", false)
						m.awaitingPrompt = false
						m.promptInput = ""
						return m, subscribeEvents()
					case "":
						ui.SubmitPromptResponse(m.promptID, "", m.promptDefault)
						m.awaitingPrompt = false
						m.promptInput = ""
						return m, subscribeEvents()
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
				input := strings.TrimSpace(m.textInput.Value())
				if input != "" {
					// Check for slash commands first
					if strings.HasPrefix(input, "/") {
						handled, newModel, cmd := m.handleSlashCommand(input)
						if handled {
							m.textInput.SetValue("")
							if newModel != nil {
								return *newModel, cmd
							}
							return m, cmd
						}
					}

					// Regular agent command execution
					go executeAgentRequest(input)
					ui.Logf("🎯 Executing: %s", input)

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
				// Unfocus input when focused
				m.focusedInput = false
				m.textInput.Blur()
				return m, nil
			case "ctrl+c":
				return m, tea.Quit
			default:
				// Reset history navigation when user starts typing
				if m.historyIndex != -1 {
					m.historyIndex = -1
					m.originalInput = ""
				}

				// Pass through to text input
				var cmd tea.Cmd
				m.textInput, cmd = m.textInput.Update(msg)
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
		case "q", "Q", "esc", "ctrl+c":
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
			if m.interactiveMode {
				// Toggle input focus with tab
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
		return m, subscribeEvents()
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
		return m, subscribeEvents()
	case ui.ModelInfoEvent:
		if strings.TrimSpace(msg.Name) != "" {
			m.baseModel = msg.Name
		}
		return m, subscribeEvents()
	case ui.StreamStartedEvent:
		m.streaming = true
		return m, subscribeEvents()
	case ui.StreamEndedEvent:
		m.streaming = false
		return m, subscribeEvents()
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
		return m, subscribeEvents()
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
		return m, subscribeEvents()
	case tickMsg:
		return m, tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
	default:
		return m, nil
	}
}

func (m model) View() string {
	header := m.renderHeader()
	// Progress section (collapsible)
	prog := ""
	if !m.progressCollapsed {
		if pr := m.renderProgress(); pr != "" {
			prog = pr + "\n"
		}
	}
	// Compute logs viewport height; prompt renders as overlay, so exclude from height calc
	reserved := 1 + 2 + 1 // header + spacing + footer
	if !m.progressCollapsed && m.renderProgress() != "" {
		reserved += countLines(m.renderProgress()) + 1
	}

	// In interactive mode, reserve space for input box
	if m.interactiveMode {
		reserved += 4 // input box height
	}

	availableLogLines := m.height - reserved
	if availableLogLines < 3 {
		availableLogLines = 3 // Minimum space for logs
	}
	m.vp.Width = max(0, m.width-2)
	m.vp.Height = max(1, availableLogLines)
	logsView := "[logs collapsed]"
	if !m.logsCollapsed {
		logsView = m.vp.View()
	}
	// Clean body layout for interactive mode
	var body string
	if m.interactiveMode {
		// Enhanced header with status indicators
		logStatus := "expanded"
		if m.logsCollapsed {
			logStatus = "collapsed"
		}

		autoScrollStatus := "ON"
		if !m.logsCollapsed && !m.vp.AtBottom() {
			autoScrollStatus = "OFF"
		}

		sectionHeader := fmt.Sprintf("📋 Agent Logs (%s) • Auto-scroll: %s • Entries: %d",
			logStatus, autoScrollStatus, len(m.logs))

		body = lipgloss.NewStyle().Margin(1, 1).Render(fmt.Sprintf("%s%s\n%s", prog, sectionHeader, logsView))
	} else {
		// Keep original layout for non-interactive mode
		body = lipgloss.NewStyle().Margin(1, 1).Render(fmt.Sprintf("Width: %d  Height: %d\n\n%sLogs | Progress\n%s", m.width, m.height, prog, logsView))
	}

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

		footerText := fmt.Sprintf("%s | Tab: Switch Focus | Enter: Execute | ↑/↓: History | /help: Commands%s%s",
			focusState, historyInfo, scrollIndicator)

		// Progressive truncation for narrow terminals
		if len(footerText) > m.width-4 {
			footerText = fmt.Sprintf("%s | Tab: Focus | Enter: Execute | ↑/↓: History%s%s",
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

// Run starts the TUI program with sane options.
func Run() error {
	p := tea.NewProgram(initialModel(), tea.WithContext(context.Background()), tea.WithAltScreen())
	_, err := p.Run()
	// On exit, restore default sink to stdout so subsequent output isn't lost
	ui.UseStdoutSink()
	return err
}

// RunInteractiveAgent starts the TUI in interactive agent mode
func RunInteractiveAgent() error {
	p := tea.NewProgram(initialInteractiveModel(), tea.WithContext(context.Background()), tea.WithAltScreen())
	_, err := p.Run()
	// On exit, restore default sink to stdout so subsequent output isn't lost
	ui.UseStdoutSink()
	return err
}

// handleSlashCommand processes slash commands and returns (handled, newModel, cmd)
func (m model) handleSlashCommand(input string) (bool, *model, tea.Cmd) {
	parts := strings.Fields(input)
	if len(parts) == 0 {
		return false, nil, nil
	}

	command := strings.ToLower(parts[0])
	args := parts[1:]

	switch command {
	case "/help", "/h":
		helpText := `🚀 Slash Commands:

Agent Commands:
  /help, /h              Show this help message
  /quit, /q, /exit       Quit the interactive agent
  /clear, /c             Clear the logs
  /status, /s            Show current agent status
  /model [name]          Show or set the current model
  /logs                  Toggle logs collapse/expand
  /show, /showlogs       Show/expand logs (force visible)
  /hide, /hidelogs       Hide/collapse logs  
  /progress              Toggle progress collapse/expand
  /history, /hist        Show command history
  /workspace, /ws        Show workspace information
  /config                Show current configuration

Navigation:
  Enter                  Execute agent command or slash command
  Tab                    Switch focus (input ↔ logs)
  ESC                    Unfocus input (ESC again to quit)
  Ctrl+C                 Quit immediately
  i                      Focus input (when unfocused)
  l                      Toggle logs collapse/expand
  p                      Toggle progress collapse/expand
  Ctrl+L                 Clear logs

History (when input is focused):
  ↑                      Previous command in history
  ↓                      Next command in history (or return to current input)

Scrolling (when logs are visible):
  ↑/k                    Scroll up one line
  ↓/j                    Scroll down one line
  Home                   Go to top of logs
  End                    Go to bottom (resume auto-scroll)
  PgUp/PgDn             Scroll by page
  Mouse wheel            Scroll up/down

Note: Auto-scroll is disabled when you scroll up to read earlier logs.
Press 'End' or scroll to bottom to resume auto-scroll for new messages.

Examples:
  Add error handling to main.go
  /show                              # Expand logs to see output
  /model deepinfra:deepseek-ai/DeepSeek-V3.1
  /clear
  Fix the bug in auth.go
  /hide                              # Hide logs for more space
  /status`
		ui.Log(helpText)
		return true, nil, nil

	case "/quit", "/q", "/exit":
		ui.Log("👋 Goodbye!")
		return true, nil, tea.Quit

	case "/clear", "/c":
		// Create new model with cleared logs
		newM := m
		newM.logs = newM.logs[:0]
		newM.vp.SetContent("")
		ui.Log("📋 Logs cleared")
		return true, &newM, nil

	case "/status", "/s":
		statusMsg := fmt.Sprintf(`📊 Agent Status:
• Model: %s
• Total Tokens: %s
• Total Cost: $%.4f
• Logs: %d entries (%s)
• Interactive Mode: Active`,
			func() string {
				if m.baseModel != "" {
					return m.baseModel
				}
				return "default"
			}(),
			formatNumber(m.totalTokens),
			m.totalCost,
			len(m.logs),
			func() string {
				if m.logsCollapsed {
					return "collapsed"
				}
				return "expanded"
			}())
		ui.Log(statusMsg)
		return true, nil, nil

	case "/model", "/m":
		if len(args) == 0 {
			ui.Logf("📋 Current model: %s", func() string {
				if m.baseModel != "" {
					return m.baseModel
				}
				return "default"
			}())
		} else {
			modelName := strings.Join(args, " ")
			ui.Logf("📋 Model setting changed to: %s (will apply to next agent execution)", modelName)
			// Note: Actual model change would need to be implemented in the agent execution
		}
		return true, nil, nil

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

	case "/workspace", "/ws":
		ui.Log(`📁 Workspace Information:
• Working Directory: ` + func() string {
			if wd, err := os.Getwd(); err == nil {
				return wd
			}
			return "unknown"
		}() + `
• Git Repository: ` + func() string {
			if _, err := os.Stat(".git"); err == nil {
				return "✓ Git repo detected"
			}
			return "✗ Not a git repo"
		}() + `
• Ledit Config: ` + func() string {
			if _, err := os.Stat(".ledit"); err == nil {
				return "✓ .ledit directory exists"
			}
			return "✗ No .ledit directory"
		}())
		return true, nil, nil

	case "/config":
		autoScrollStatus := "Smart (auto when at bottom)"
		if !m.vp.AtBottom() {
			autoScrollStatus = "Disabled (user scrolled up)"
		}
		ui.Log(`⚙️  Current Configuration:
• Interactive Mode: Active
• Auto-scroll Logs: ` + autoScrollStatus + `
• Command History: ` + fmt.Sprintf("%d commands stored", len(m.commandHistory)) + `
• Log Retention: 500 entries max
• Model: ` + func() string {
			if m.baseModel != "" {
				return m.baseModel
			}
			return "default (from config)"
		}())
		return true, nil, nil

	default:
		ui.Logf("❌ Unknown slash command: %s. Type /help for available commands.", command)
		return true, nil, nil
	}
}

// executeAgentRequest executes an agent request asynchronously
func executeAgentRequest(request string) {
	ui.Logf("🚀 Starting agent execution: %s", request)
	ui.PublishStatus("Executing agent request...")

	// Set environment variables for agent execution
	os.Setenv("LEDIT_FROM_AGENT", "1")
	os.Setenv("LEDIT_SKIP_PROMPT", "1")

	// Execute the agent request
	ui.Logf("⚙️  Agent analyzing request and creating execution plan...")
	err := agent.RunSimplifiedAgent(request, true, "") // Use default model

	if err != nil {
		ui.Logf("❌ Agent execution failed: %v", err)
		ui.PublishStatus("Agent execution failed")
	} else {
		ui.Logf("✅ Agent request completed successfully")
		ui.PublishStatus("Agent execution completed")
	}
}
