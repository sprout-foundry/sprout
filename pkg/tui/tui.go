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
	start    time.Time
	width    int
	height   int
	logs     []string
	progress ui.ProgressSnapshotEvent
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
		return m, subscribeEvents()
	case tickMsg:
		return m, tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
	case tea.KeyMsg:
		if msg.String() == "q" || msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		return m, nil
	default:
		return m, nil
	}
}

func (m model) View() string {
	header := lipgloss.NewStyle().Bold(true).Padding(0, 1).Render("Ledit UI")
	uptime := time.Since(m.start).Round(time.Second)
	logs := ""
	for i := range m.logs {
		logs += m.logs[i] + "\n"
	}
	body := lipgloss.NewStyle().Margin(1, 1).Render(fmt.Sprintf("Uptime: %s\nWidth: %d  Height: %d\n\n%s\nLogs:\n%s\nPress q to quit.", uptime, m.width, m.height, m.renderProgress(), logs))
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

// Run starts the TUI program with sane options.
func Run() error {
	p := tea.NewProgram(initialModel(), tea.WithContext(context.Background()), tea.WithAltScreen())
	_, err := p.Run()
	return err
}
