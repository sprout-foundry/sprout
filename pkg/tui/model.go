package tui

import (
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
)

// Model represents the TUI application model
type Model struct {
	state        *AppState
	viewRenderer *ViewRenderer
	dropdown     *InlineDropdown // For inline dropdowns
	dropdownMode DropdownType    // Current dropdown being shown
}

// DropdownType represents the type of dropdown being shown
type DropdownType int

const (
	DropdownNone DropdownType = iota
	DropdownCommands
	DropdownModels
)

// NewModel creates a new TUI model
func NewModel(interactive bool) Model {
	state := &AppState{
		StartTime:          time.Now(),
		Logs:               make([]string, 0, 256),
		LogsViewport:       viewport.New(DefaultViewWidth, DefaultViewHeight),
		PromptViewport:     viewport.New(DefaultViewWidth, 10),
		InteractiveMode:    interactive,
		FocusedInput:       interactive,
		HistoryIndex:       -1,
		PasteThreshold:     PasteThreshold,
		LastInputTime:      time.Now(),
		InterruptChan:      make(chan string, 1),
		CommandHistory:     make([]string, 0),
		CommandSuggestions: make([]string, 0),
	}

	// Enable auto-scroll by default
	state.LogsViewport.GotoBottom()

	// Initialize viewport with empty content to ensure it's ready
	state.LogsViewport.SetContent("")

	// Set logs collapsed based on mode and env
	if interactive {
		state.LogsCollapsed = false
	} else {
		if v := strings.ToLower(strings.TrimSpace(os.Getenv("LEDIT_LOGS_COLLAPSED"))); v == "0" || v == "false" || v == "no" {
			state.LogsCollapsed = false
		} else {
			state.LogsCollapsed = true
		}
	}

	// Initialize text input for interactive mode
	if interactive {
		ti := textinput.New()
		ti.Placeholder = "Enter request or /help for commands..."
		ti.Focus()
		ti.CharLimit = 2000
		ti.Width = 80
		state.TextInput = ti

		// Set environment variables
		os.Setenv("LEDIT_FROM_AGENT", "1")
		os.Setenv("LEDIT_SKIP_PROMPT", "1")
		os.Setenv("LEDIT_USING_CODER", "1")

		// Initialize agent
		agent, err := agent.NewAgent()
		if err != nil {
			ui.Logf("❌ Failed to initialize agent: %v", err)
		} else {
			state.Agent = agent
			state.Agent.SetStatsUpdateCallback(func(totalTokens int, totalCost float64) {
				modelName := state.Agent.GetModel()
				ui.PublishProgressWithTokens(0, 0, totalTokens, totalCost, modelName, nil)
				// Also update state directly
				state.TotalTokens = totalTokens
				state.TotalCost = totalCost
			})
			state.BaseModel = state.Agent.GetModel()
			// Get provider name
			clientType := state.Agent.GetProviderType()
			state.Provider = agent_api.GetProviderName(clientType)
		}

		state.CommandRegistry = commands.NewCommandRegistry()
	}

	return Model{
		state:        state,
		viewRenderer: NewViewRenderer(state),
		dropdownMode: DropdownNone,
	}
}

// Init initializes the model
func (m Model) Init() tea.Cmd {
	ui.SetDefaultSink(ui.TuiSink{})

	// Test log to verify logging is working
	ui.Log("🚀 TUI initialized and ready")
	ui.Log("📝 Type '/' to see available commands")

	return tea.Batch(
		tea.Tick(time.Second, func(t time.Time) tea.Msg { return TickMsg(t) }),
		subscribeEvents(),
	)
}

// Update handles messages
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Handle dropdown mode first
	if m.dropdownMode != DropdownNone && m.dropdown != nil {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			done, cancelled := m.dropdown.Update(msg.String())
			if done {
				if !cancelled {
					// Handle selection
					switch m.dropdownMode {
					case DropdownCommands:
						if cmd := m.dropdown.GetSelected(); cmd != nil {
							m.handleSlashCommand(cmd.(string))
						}
					case DropdownModels:
						if modelID := m.dropdown.GetSelected(); modelID != nil {
							m.handleSlashCommand(fmt.Sprintf("/models %s", modelID.(string)))
						}
					}
				}
				// Clear dropdown
				m.dropdown = nil
				m.dropdownMode = DropdownNone
			}
			return m, nil
		}
	}

	// Normal update handling
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.state.Width = msg.Width
		m.state.Height = msg.Height
		m.state.LogsViewport.Width = max(0, m.state.Width-2)
		return m, nil

	case tea.KeyMsg:
		// Always clear suggestions if we just executed a command and the input is empty
		if m.state.JustExecutedCommand && m.state.TextInput.Value() == "" {
			m.state.ShowCommandSuggestions = false
			m.state.CommandSuggestions = nil
		}
		return m.handleKeyMsg(msg)

	case ui.LogEvent:
		// Debug: Track that we received a log event
		m = m.handleLogEvent(msg)
		return m, nil

	case ui.ProgressSnapshotEvent:
		m.state.Progress = msg
		if msg.BaseModel != "" {
			m.state.BaseModel = msg.BaseModel
		}
		if msg.TotalTokens > 0 {
			m.state.TotalTokens = msg.TotalTokens
		}
		if msg.TotalCost > 0 {
			m.state.TotalCost = msg.TotalCost
		}
		return m, nil

	case ui.ModelInfoEvent:
		if strings.TrimSpace(msg.Name) != "" {
			m.state.BaseModel = msg.Name
		}
		return m, nil

	case ui.StreamStartedEvent:
		m.state.Streaming = true
		return m, nil

	case ui.StreamEndedEvent:
		m.state.Streaming = false
		return m, nil

	case ui.PromptRequestEvent:
		m.state.AwaitingPrompt = true
		m.state.PromptID = msg.ID
		m.state.PromptText = msg.Prompt
		m.state.PromptContext = msg.Context
		m.state.PromptYesNo = msg.RequireYesNo
		m.state.PromptDefault = msg.DefaultYes
		m.state.PromptInput = ""
		// Initialize prompt viewport content
		pvContent := m.state.PromptText
		if strings.TrimSpace(m.state.PromptContext) != "" {
			pvContent = m.state.PromptText + "\n\n" + m.state.PromptContext
		}
		m.state.PromptViewport.SetContent(pvContent)
		return m, nil

	case ui.StatusEvent:
		if s := strings.TrimSpace(msg.Text); s != "" {
			m.state.Logs = append([]string{"… " + s}, m.state.Logs...)
			if len(m.state.Logs) > 500 {
				m.state.Logs = m.state.Logs[:500]
			}
		}
		return m, nil

	case ui.PromptResponseEvent:
		// Handle prompt response if needed
		return m, nil

	case SubscribeEventsMsg:
		return m, subscribeEvents()

	case TickMsg:
		return m, tea.Tick(time.Second, func(t time.Time) tea.Msg { return TickMsg(t) })

	default:
		// Pass through to viewport
		if !m.state.LogsCollapsed {
			var cmd tea.Cmd
			m.state.LogsViewport, cmd = m.state.LogsViewport.Update(msg)
			return m, cmd
		}
		return m, nil
	}
}

// View renders the model
func (m Model) View() string {
	// Show dropdown if active
	if m.dropdownMode != DropdownNone && m.dropdown != nil {
		// Use 80% of width and height, max 100x30
		dropdownWidth := min(100, int(float64(m.state.Width)*0.8))
		dropdownHeight := min(30, int(float64(m.state.Height)*0.8))

		dropdownView := m.dropdown.View(dropdownWidth, dropdownHeight)

		// Center the dropdown
		return lipgloss.Place(m.state.Width, m.state.Height,
			lipgloss.Center, lipgloss.Center, dropdownView)
	}

	// Show prompt modal if waiting
	if m.state.AwaitingPrompt {
		return m.renderPromptModal()
	}

	// Normal view
	return m.viewRenderer.Render()
}

// handleKeyMsg handles keyboard input
func (m Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Handle prompt input first
	if m.state.AwaitingPrompt {
		return m.handlePromptKey(msg), nil
	}

	// Interactive mode key handling
	if m.state.InteractiveMode && m.state.FocusedInput {
		return m.handleInteractiveKey(msg)
	}

	// General key handling
	return m.handleGeneralKey(msg)
}

// handleLogEvent handles log events
func (m Model) handleLogEvent(msg ui.LogEvent) Model {
	wasAtBottom := m.state.LogsViewport.AtBottom()

	m.state.Logs = append(m.state.Logs, msg.Text)
	if len(m.state.Logs) > 500 {
		m.state.Logs = m.state.Logs[len(m.state.Logs)-500:]
	}
	m.state.LogsViewport.SetContent(strings.Join(m.state.Logs, "\n"))

	// Smart auto-scroll
	if wasAtBottom {
		m.state.LogsViewport.GotoBottom()
	}

	// Clear command suggestions whenever we log something after executing a command
	if m.state.JustExecutedCommand {
		m.state.ShowCommandSuggestions = false
		m.state.CommandSuggestions = nil
	}

	return m
}
