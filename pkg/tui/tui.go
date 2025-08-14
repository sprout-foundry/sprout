package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/ui"
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
	// summary
	baseModel   string
	totalTokens int
	totalCost   float64
	// logs pane controls
	logsCollapsed bool
	logsOffset    int // number of lines scrolled up from bottom (0 means stick to bottom)
}

type tickMsg time.Time

func initialModel() model {
	return model{start: time.Now(), logs: make([]string, 0, 256)}
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
		return m, nil
	case ui.LogEvent:
		m.logs = append(m.logs, fmt.Sprintf("%s", msg.Text))
		if len(m.logs) > 500 {
			m.logs = m.logs[len(m.logs)-500:]
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
		m.promptYesNo = msg.RequireYesNo
		m.promptDefault = msg.DefaultYes
		return m, subscribeEvents()
	case tickMsg:
		return m, tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "Q", "esc", "ctrl+c":
			return m, tea.Quit
		case "l", "L":
			m.logsCollapsed = !m.logsCollapsed
			return m, nil
		case "up", "k":
			if !m.logsCollapsed && len(m.logs) > 0 {
				if m.logsOffset < len(m.logs)-1 {
					m.logsOffset++
				}
			}
			return m, nil
		case "down", "j":
			if !m.logsCollapsed && m.logsOffset > 0 {
				m.logsOffset--
			}
			return m, nil
		case "pgup":
			if !m.logsCollapsed && len(m.logs) > 0 {
				m.logsOffset += 10
				if m.logsOffset > len(m.logs)-1 {
					m.logsOffset = len(m.logs) - 1
				}
			}
			return m, nil
		case "pgdown":
			if !m.logsCollapsed && m.logsOffset > 0 {
				m.logsOffset -= 10
				if m.logsOffset < 0 {
					m.logsOffset = 0
				}
			}
			return m, nil
		case "home":
			if !m.logsCollapsed {
				m.logsOffset = len(m.logs) - 1
			}
			return m, nil
		case "end":
			if !m.logsCollapsed {
				m.logsOffset = 0
			}
			return m, nil
		case "y", "Y":
			if m.awaitingPrompt && m.promptYesNo {
				ui.SubmitPromptResponse(m.promptID, "yes", true)
				m.awaitingPrompt = false
				return m, subscribeEvents()
			}
		case "n", "N":
			if m.awaitingPrompt && m.promptYesNo {
				ui.SubmitPromptResponse(m.promptID, "no", false)
				m.awaitingPrompt = false
				return m, subscribeEvents()
			}
		case "enter":
			if m.awaitingPrompt && m.promptYesNo {
				ui.SubmitPromptResponse(m.promptID, "", m.promptDefault)
				m.awaitingPrompt = false
				return m, subscribeEvents()
			}
		}
		return m, nil
	default:
		return m, nil
	}
}

func (m model) View() string {
	hdr := "Ledit UI"
	if m.streaming {
		hdr += "  [streaming…]"
	}
	header := lipgloss.NewStyle().Bold(true).Padding(0, 1).Render(hdr)
	uptime := time.Since(m.start).Round(time.Second)
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
	// header(1) + summary(1) + spacing(2) + footer(1)
	reserved := 1 + 1 + 2 + 1 + progLines + promptLines
	availableLogLines := m.height - reserved
	if availableLogLines < 1 {
		availableLogLines = 1
	}
	// Optional collapsible logs
	logs := ""
	if !m.logsCollapsed && len(m.logs) > 0 {
		// compute starting index honoring scroll offset
		start := len(m.logs) - availableLogLines - m.logsOffset
		if start < 0 {
			start = 0
		}
		end := start + availableLogLines
		if end > len(m.logs) {
			end = len(m.logs)
		}
		for i := start; i < end; i++ {
			logs += m.logs[i] + "\n"
		}
	}
	prompt := ""
	if m.awaitingPrompt {
		if m.promptYesNo {
			def := "no"
			if m.promptDefault {
				def = "yes"
			}
			prompt = fmt.Sprintf("\nPrompt: %s [y/n] (default %s)\n", m.promptText, def)
		} else {
			prompt = fmt.Sprintf("\nPrompt: %s\n", m.promptText)
		}
	}
	summary := fmt.Sprintf("Model: %s | Tokens: %d | Cost: $%.4f | Uptime: %s", m.baseModel, m.totalTokens, m.totalCost, uptime)
	body := lipgloss.NewStyle().Margin(1, 1).Render(fmt.Sprintf("%s\nWidth: %d  Height: %d\n\n%s%s\nLogs (l to toggle, arrows/PgUp/PgDn to scroll):\n%s\nPress q to quit.", summary, m.width, m.height, m.renderProgress(), prompt, logs))
	footer := lipgloss.NewStyle().Faint(true).Padding(0, 1).Render("© Ledit")

	vertical := lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
	return vertical
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
