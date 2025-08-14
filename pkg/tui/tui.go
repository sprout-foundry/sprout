package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/ui"
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
	// summary
	baseModel   string
	totalTokens int
	totalCost   float64
	// logs pane controls
	logsCollapsed bool
	vp            viewport.Model
}

type tickMsg time.Time

func initialModel() model {
	m := model{start: time.Now(), logs: make([]string, 0, 256)}
	m.vp = viewport.Model{}
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
		// Prompt-aware handling first
		if m.awaitingPrompt && m.promptYesNo {
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
		}
		// General key handling
		switch msg.String() {
		case "q", "Q", "esc", "ctrl+c":
			return m, tea.Quit
		case "l", "L":
			m.logsCollapsed = !m.logsCollapsed
			return m, nil
		case "ctrl+l": // clear logs
			m.logs = m.logs[:0]
			m.vp.SetContent("")
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
		m.logs = append(m.logs, fmt.Sprintf("%s", msg.Text))
		if len(m.logs) > 500 {
			m.logs = m.logs[len(m.logs)-500:]
		}
		m.vp.SetContent(strings.Join(m.logs, "\n"))
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
		m.promptYesNo = msg.RequireYesNo
		m.promptDefault = msg.DefaultYes
		return m, subscribeEvents()
	case tickMsg:
		return m, tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
	default:
		return m, nil
	}
}

func (m model) View() string {
	header := m.renderHeader()
	// Compute how many log lines can fit given the current terminal height
	progLines := countLines(m.renderProgress())
	promptLines := 0
	if m.awaitingPrompt {
		if m.promptYesNo {
			promptLines = 1
		} else {
			promptLines = 2
		}
	}
	// header(1) + spacing(2) + footer(1)
	reserved := 1 + 2 + 1 + progLines + promptLines
	availableLogLines := m.height - reserved
	if availableLogLines < 1 {
		availableLogLines = 1
	}
	// update viewport dims and content
	m.vp.Width = max(0, m.width-2)
	m.vp.Height = max(1, availableLogLines)
	m.vp.SetContent(strings.Join(m.logs, "\n"))
	prompt := ""
	if m.awaitingPrompt {
		if m.promptYesNo {
			def := "no"
			if m.promptDefault {
				def = "yes"
			}
			if m.promptInput != "" {
				prompt = fmt.Sprintf("\nPrompt: %s [y/n] (default %s) > %s\n", m.promptText, def, m.promptInput)
			} else {
				prompt = fmt.Sprintf("\nPrompt: %s [y/n] (default %s)\n", m.promptText, def)
			}
		} else {
			prompt = fmt.Sprintf("\nPrompt: %s\n", m.promptText)
		}
	}
	logsView := "[logs collapsed]"
	if !m.logsCollapsed {
		logsView = m.vp.View()
	}
	body := lipgloss.NewStyle().Margin(1, 1).Render(fmt.Sprintf("Width: %d  Height: %d\n\n%s%s\nLogs (l to toggle, arrows/PgUp/PgDn, mouse; ctrl+l clear):\n%s\nPress q to quit.", m.width, m.height, m.renderProgress(), prompt, logsView))
	footer := lipgloss.NewStyle().Faint(true).Padding(0, 1).Render("© Ledit")

	vertical := lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
	return vertical
}

func (m model) renderHeader() string {
	// Build a single-line status header that adapts to width
	streaming := ""
	if m.streaming {
		streaming = " [streaming…]"
	}
	uptime := time.Since(m.start).Round(time.Second)
	parts := []string{
		fmt.Sprintf("Model: %s", m.baseModel),
		fmt.Sprintf("Tokens: %d", m.totalTokens),
		fmt.Sprintf("Cost: $%.4f", m.totalCost),
		fmt.Sprintf("Uptime: %s", uptime),
	}
	line := strings.Join(parts, " | ") + streaming
	// trim to width
	if m.width > 0 {
		line = trimToWidth(line, m.width-2)
	}
	return lipgloss.NewStyle().Bold(true).Padding(0, 1).Render(line)
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
