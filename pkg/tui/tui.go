package tui

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Basic model scaffold: header, body, footer with ticking clock
type model struct {
	start  time.Time
	width  int
	height int
}

type tickMsg time.Time

func initialModel() model {
	return model{start: time.Now()}
}

func (m model) Init() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
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
	body := lipgloss.NewStyle().Margin(1, 1).Render(fmt.Sprintf("Uptime: %s\nWidth: %d  Height: %d\n\nPress q to quit.", uptime, m.width, m.height))
	footer := lipgloss.NewStyle().Faint(true).Padding(0, 1).Render("© Ledit")

	vertical := lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
	return vertical
}

// Run starts the TUI program with sane options.
func Run() error {
	p := tea.NewProgram(initialModel(), tea.WithContext(context.Background()), tea.WithAltScreen())
	_, err := p.Run()
	return err
}
