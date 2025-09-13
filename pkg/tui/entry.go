package tui

import (
	"context"

	"github.com/alantheprice/ledit/pkg/ui"
	tea "github.com/charmbracelet/bubbletea"
)

// Run starts the TUI program in standard mode
func Run() error {
	m := NewModel(false)
	p := tea.NewProgram(m, tea.WithContext(context.Background()), tea.WithAltScreen())
	_, err := p.Run()
	// Restore default sink on exit
	ui.UseStdoutSink()
	return err
}

// RunInteractiveAgent starts the TUI in interactive agent mode
func RunInteractiveAgent() error {
	m := NewModel(true)
	p := tea.NewProgram(m, tea.WithContext(context.Background()), tea.WithAltScreen())
	_, err := p.Run()
	// Restore default sink on exit
	ui.UseStdoutSink()
	return err
}

// RunMinimalTest runs a minimal TUI for testing input responsiveness
func RunMinimalTest() error {
	m := NewModel(true)
	// Clear logs for minimal test
	m.state.Logs = []string{"Minimal TUI test - type something and press enter"}

	p := tea.NewProgram(m, tea.WithContext(context.Background()), tea.WithAltScreen())
	_, err := p.Run()
	ui.UseStdoutSink()
	return err
}
